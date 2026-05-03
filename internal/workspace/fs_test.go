package workspace

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/chalfel/forge-flow/internal/config"
	"github.com/chalfel/forge-flow/internal/domain"
)

func newFS(t *testing.T, hooks config.Hooks) *FS {
	t.Helper()
	root := t.TempDir()
	fs, err := NewFS(FSOptions{
		Root:   root,
		Hooks:  hooks,
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	if err != nil {
		t.Fatalf("new fs: %v", err)
	}
	return fs
}

func TestSanitizeKey(t *testing.T) {
	cases := map[string]string{
		"ABC-7":         "ABC-7",
		"foo/bar baz":   "foo_bar_baz",
		"":              "_",
		"o/r#42":        "o_r_42",
		"weird*name!":   "weird_name_",
	}
	for in, want := range cases {
		if got := SanitizeKey(in); got != want {
			t.Errorf("SanitizeKey(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestPrepare_CreatesDirAndRunsAfterCreate(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "marker")
	fs := newFS(t, config.Hooks{
		AfterCreate: "touch " + marker,
		TimeoutMs:   5000,
	})
	ws, err := fs.Prepare(context.Background(), domain.Issue{ID: "1", Identifier: "ABC-7"})
	if err != nil {
		t.Fatalf("prepare: %v", err)
	}
	if !ws.CreatedNow {
		t.Fatal("CreatedNow should be true on first prepare")
	}
	if !strings.HasSuffix(ws.Path, "/ABC-7") {
		t.Errorf("workspace path wrong: %q", ws.Path)
	}
	if _, err := os.Stat(marker); err != nil {
		t.Fatalf("after_create did not run: %v", err)
	}
}

func TestPrepare_ExistingDirSkipsAfterCreate(t *testing.T) {
	created := 0
	hooks := config.Hooks{TimeoutMs: 5000}
	fs := newFS(t, hooks)

	// First call creates the dir.
	ws, err := fs.Prepare(context.Background(), domain.Issue{ID: "1", Identifier: "ABC-7"})
	if err != nil {
		t.Fatal(err)
	}
	if !ws.CreatedNow {
		t.Fatal("expected CreatedNow on first call")
	}
	created++

	// Second call should NOT mark CreatedNow (after_create would not re-run).
	ws2, err := fs.Prepare(context.Background(), domain.Issue{ID: "1", Identifier: "ABC-7"})
	if err != nil {
		t.Fatal(err)
	}
	if ws2.CreatedNow {
		t.Fatal("expected CreatedNow=false on second call")
	}
	if ws2.Path != ws.Path {
		t.Errorf("path changed between calls: %q vs %q", ws.Path, ws2.Path)
	}
	_ = created
}

func TestPrepare_AfterCreateFailureCleansUp(t *testing.T) {
	fs := newFS(t, config.Hooks{
		AfterCreate: "exit 1",
		TimeoutMs:   5000,
	})
	_, err := fs.Prepare(context.Background(), domain.Issue{ID: "1", Identifier: "ABC-7"})
	if err == nil {
		t.Fatal("expected error from failing after_create")
	}
	// Directory should have been removed so the next attempt can re-try cleanly.
	candidates, _ := filepath.Glob(filepath.Join(fs.root, "ABC-7"))
	if len(candidates) != 0 {
		t.Fatalf("expected workspace dir to be removed after hook failure, got %v", candidates)
	}
}

func TestPrepare_BeforeRunFailureSurfaces(t *testing.T) {
	fs := newFS(t, config.Hooks{
		BeforeRun: "exit 2",
		TimeoutMs: 5000,
	})
	_, err := fs.Prepare(context.Background(), domain.Issue{ID: "1", Identifier: "ABC-7"})
	if err == nil || !strings.Contains(err.Error(), "before_run") {
		t.Fatalf("expected before_run failure, got %v", err)
	}
}

func TestPrepare_RejectsKeyEscapingRoot(t *testing.T) {
	// Sanitiser converts slashes and dots to "_", so direct ".." cannot
	// escape, but we still verify the containment guard. We reach in via the
	// internal helper to simulate a malformed path.
	fs := newFS(t, config.Hooks{})
	bad := filepath.Join(fs.root, "..", "elsewhere")
	if err := fs.ensureContained(bad); err == nil {
		t.Fatal("expected containment failure for parent-relative path")
	}
}

func TestCleanup_RunsAfterRun(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "after-run-marker")
	fs := newFS(t, config.Hooks{
		AfterRun:  "touch " + marker,
		TimeoutMs: 5000,
	})
	ws, err := fs.Prepare(context.Background(), domain.Issue{ID: "1", Identifier: "ABC-7"})
	if err != nil {
		t.Fatal(err)
	}
	if err := fs.Cleanup(context.Background(), ws); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(marker); err != nil {
		t.Fatalf("after_run did not run: %v", err)
	}
}

func TestPruneOrphans_RemovesUnknownDirsAndKeepsActive(t *testing.T) {
	fs := newFS(t, config.Hooks{TimeoutMs: 5000})
	// Create three workspaces.
	for _, id := range []string{"ABC-1", "ABC-2", "ABC-3"} {
		if _, err := fs.Prepare(context.Background(), domain.Issue{ID: id, Identifier: id}); err != nil {
			t.Fatal(err)
		}
	}
	pruned, err := fs.PruneOrphans(context.Background(), []string{"ABC-1"})
	if err != nil {
		t.Fatal(err)
	}
	if pruned != 2 {
		t.Fatalf("expected 2 pruned, got %d", pruned)
	}
	// ABC-1 must still exist.
	if _, err := os.Stat(filepath.Join(fs.root, "ABC-1")); err != nil {
		t.Errorf("active workspace missing: %v", err)
	}
	// ABC-2 and ABC-3 must be gone.
	for _, gone := range []string{"ABC-2", "ABC-3"} {
		if _, err := os.Stat(filepath.Join(fs.root, gone)); !os.IsNotExist(err) {
			t.Errorf("orphan %s should be removed, err=%v", gone, err)
		}
	}
}

func TestPruneOrphans_HonoursBeforeRemoveHook(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "before-remove-marker")
	fs := newFS(t, config.Hooks{
		BeforeRemove: "touch " + marker,
		TimeoutMs:    5000,
	})
	if _, err := fs.Prepare(context.Background(), domain.Issue{ID: "1", Identifier: "ABC-1"}); err != nil {
		t.Fatal(err)
	}
	if _, err := fs.PruneOrphans(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(marker); err != nil {
		t.Fatalf("before_remove did not run during prune: %v", err)
	}
}

func TestRemove_RunsBeforeRemoveAndDeletes(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "before-remove-marker")
	fs := newFS(t, config.Hooks{
		BeforeRemove: "touch " + marker,
		TimeoutMs:    5000,
	})
	ws, err := fs.Prepare(context.Background(), domain.Issue{ID: "1", Identifier: "ABC-7"})
	if err != nil {
		t.Fatal(err)
	}
	if err := fs.Remove(context.Background(), ws); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(marker); err != nil {
		t.Fatalf("before_remove did not run: %v", err)
	}
	if _, err := os.Stat(ws.Path); !os.IsNotExist(err) {
		t.Fatalf("expected workspace dir removed, got err=%v", err)
	}
}
