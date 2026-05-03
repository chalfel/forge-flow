package workspace

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/chalfel/forge-flow/internal/config"
	"github.com/chalfel/forge-flow/internal/domain"
)

// FS is the real filesystem Manager. It honours the four lifecycle hooks and
// the path-containment invariants from SPEC.md:
//   1. Workspace path must stay inside workspace root
//   2. Workspace key sanitised to [A-Za-z0-9._-]; other chars become "_"
//   3. Hooks run with cwd = workspace path, shell context, configurable timeout
type FS struct {
	root      string
	hooks     config.Hooks
	hookTimeout time.Duration
	log       *slog.Logger
}

type FSOptions struct {
	Root   string
	Hooks  config.Hooks
	Logger *slog.Logger
}

func NewFS(opts FSOptions) (*FS, error) {
	abs, err := filepath.Abs(opts.Root)
	if err != nil {
		return nil, fmt.Errorf("workspace: absolutize root: %w", err)
	}
	logger := opts.Logger
	if logger == nil {
		logger = slog.Default()
	}
	timeout := time.Duration(opts.Hooks.TimeoutMs) * time.Millisecond
	if timeout <= 0 {
		timeout = 60 * time.Second
	}
	return &FS{
		root:        abs,
		hooks:       opts.Hooks,
		hookTimeout: timeout,
		log:         logger,
	}, nil
}

var unsafeKey = regexp.MustCompile(`[^A-Za-z0-9._-]`)

// SanitizeKey is exported so tests and callers can compute the canonical key
// without going through Prepare.
func SanitizeKey(input string) string {
	if strings.TrimSpace(input) == "" {
		return "_"
	}
	return unsafeKey.ReplaceAllString(input, "_")
}

// Prepare creates the per-issue directory if absent (running after_create on
// first creation), then runs before_run. Failure of either hook surfaces as
// an error and the attempt is treated as failed.
func (f *FS) Prepare(ctx context.Context, issue domain.Issue) (domain.Workspace, error) {
	key := SanitizeKey(issue.Identifier)
	if key == "_" || key == "" {
		key = SanitizeKey(issue.ID)
	}
	path := filepath.Join(f.root, key)
	abs, err := filepath.Abs(path)
	if err != nil {
		return domain.Workspace{}, fmt.Errorf("workspace: absolutize: %w", err)
	}
	if err := f.ensureContained(abs); err != nil {
		return domain.Workspace{}, err
	}

	createdNow := false
	if _, statErr := os.Stat(abs); errors.Is(statErr, os.ErrNotExist) {
		if err := os.MkdirAll(abs, 0o755); err != nil {
			return domain.Workspace{}, fmt.Errorf("workspace: mkdir %s: %w", abs, err)
		}
		createdNow = true
		if err := f.runHook(ctx, "after_create", f.hooks.AfterCreate, abs); err != nil {
			// On after_create failure, remove the half-created directory so
			// the next attempt can re-run the hook from a clean slate.
			_ = os.RemoveAll(abs)
			return domain.Workspace{}, fmt.Errorf("workspace: after_create: %w", err)
		}
	} else if statErr != nil {
		return domain.Workspace{}, fmt.Errorf("workspace: stat %s: %w", abs, statErr)
	}

	if err := f.runHook(ctx, "before_run", f.hooks.BeforeRun, abs); err != nil {
		return domain.Workspace{}, fmt.Errorf("workspace: before_run: %w", err)
	}

	return domain.Workspace{Path: abs, WorkspaceKey: key, CreatedNow: createdNow}, nil
}

// Cleanup runs after_run. Per SPEC.md hook failures here are logged but do
// not fail the attempt — the agent's terminal status already determines that.
func (f *FS) Cleanup(ctx context.Context, ws domain.Workspace) error {
	if err := f.runHook(ctx, "after_run", f.hooks.AfterRun, ws.Path); err != nil {
		f.log.Warn("after_run hook failed", "path", ws.Path, "err", err)
	}
	return nil
}

// PruneOrphans removes per-issue subdirectories under the root whose
// sanitised keys are not in activeKeys. before_remove runs for each pruned
// dir (logged-as-skipped on failure). Returns the number of directories
// removed.
func (f *FS) PruneOrphans(ctx context.Context, activeKeys []string) (int, error) {
	keep := make(map[string]struct{}, len(activeKeys))
	for _, k := range activeKeys {
		keep[SanitizeKey(k)] = struct{}{}
	}
	entries, err := os.ReadDir(f.root)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return 0, nil
		}
		return 0, fmt.Errorf("workspace prune: read root: %w", err)
	}
	pruned := 0
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if _, ok := keep[e.Name()]; ok {
			continue
		}
		path := filepath.Join(f.root, e.Name())
		if err := f.runHook(ctx, "before_remove", f.hooks.BeforeRemove, path); err != nil {
			f.log.Warn("prune: before_remove failed; continuing", "path", path, "err", err)
		}
		if err := os.RemoveAll(path); err != nil {
			f.log.Warn("prune: remove failed; continuing", "path", path, "err", err)
			continue
		}
		f.log.Info("prune: removed orphan workspace", "path", path)
		pruned++
	}
	return pruned, nil
}

// Remove runs before_remove and deletes the directory. Not invoked
// automatically by the scheduler; available for operators who want to wipe
// workspaces between cycles.
func (f *FS) Remove(ctx context.Context, ws domain.Workspace) error {
	if err := f.ensureContained(ws.Path); err != nil {
		return err
	}
	if err := f.runHook(ctx, "before_remove", f.hooks.BeforeRemove, ws.Path); err != nil {
		f.log.Warn("before_remove hook failed", "path", ws.Path, "err", err)
	}
	return os.RemoveAll(ws.Path)
}

// ensureContained verifies that `path` lives inside the workspace root. We
// use filepath.Rel rather than HasPrefix so symlink-resolved equivalents
// don't slip through and so platform path separators are handled.
func (f *FS) ensureContained(path string) error {
	rel, err := filepath.Rel(f.root, path)
	if err != nil {
		return fmt.Errorf("workspace: path containment check: %w", err)
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return fmt.Errorf("workspace: path %q escapes root %q", path, f.root)
	}
	return nil
}

func (f *FS) runHook(ctx context.Context, name, script, cwd string) error {
	if strings.TrimSpace(script) == "" {
		return nil
	}
	hookCtx, cancel := context.WithTimeout(ctx, f.hookTimeout)
	defer cancel()
	cmd := exec.CommandContext(hookCtx, "bash", "-lc", script)
	cmd.Dir = cwd
	out, err := cmd.CombinedOutput()
	if err != nil {
		f.log.Error("hook failed", "hook", name, "cwd", cwd, "err", err, "output", truncateOutput(out))
		return err
	}
	f.log.Debug("hook ok", "hook", name, "cwd", cwd, "output", truncateOutput(out))
	return nil
}

func truncateOutput(b []byte) string {
	const limit = 1024
	if len(b) <= limit {
		return string(b)
	}
	return string(b[:limit]) + "...[truncated]"
}
