package forge

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Decision is a single architectural choice recorded during task execution.
// Agents emit decision events via the runtime protocol; Forge persists them
// so sibling tasks on the same spec can see why previous choices were made.
type Decision struct {
	SpecFile     string   `json:"specFile"`
	TaskName     string   `json:"taskName"`
	Choice       string   `json:"choice"`
	Rationale    string   `json:"rationale,omitempty"`
	Alternatives []string `json:"alternatives,omitempty"`
	RecordedAt   string   `json:"recordedAt"`
	RunID        string   `json:"runId,omitempty"`
}

// decisionsPath returns the path where a task's decisions are stored.
// Layout: {historyRoot}/decisions/{specSlug}/{taskSlug}.jsonl
func decisionsPath(ctx ResolvedContext, specFile, taskName string) string {
	specSlug := slugifySpecFile(specFile)
	taskSlug := slugify(taskName)
	return filepath.Join(historyRoot(ctx), "decisions", specSlug, taskSlug+".jsonl")
}

// SaveDecision persists a decision to disk. It appends to the task's jsonl
// file, creating parent directories if needed. The RecordedAt field is
// auto-set if empty.
func (s *Service) SaveDecision(cwd string, d Decision) error {
	ctx, err := s.ResolveContext(cwd)
	if err != nil {
		return err
	}
	return s.saveDecisionWithCtx(ctx, d)
}

func (s *Service) saveDecisionWithCtx(ctx ResolvedContext, d Decision) error {
	if d.RecordedAt == "" {
		d.RecordedAt = timeNow().UTC().Format(time.RFC3339)
	}
	path := decisionsPath(ctx, d.SpecFile, d.TaskName)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.Marshal(d)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.Write(append(data, '\n'))
	return err
}

// LoadDecisionsForTask returns all decisions recorded for a single task
// on a specific spec file, oldest first.
func (s *Service) LoadDecisionsForTask(cwd, specFile, taskName string) ([]Decision, error) {
	ctx, err := s.ResolveContext(cwd)
	if err != nil {
		return nil, err
	}
	return s.loadDecisionsForTaskWithCtx(ctx, specFile, taskName)
}

func (s *Service) loadDecisionsForTaskWithCtx(ctx ResolvedContext, specFile, taskName string) ([]Decision, error) {
	return loadDecisionsFromFile(decisionsPath(ctx, specFile, taskName))
}

// LoadDecisionsForSpec returns all decisions recorded across every task
// of a given spec file. Useful for injecting sibling context into a new
// task's harness prompt.
func (s *Service) LoadDecisionsForSpec(cwd, specFile string) ([]Decision, error) {
	ctx, err := s.ResolveContext(cwd)
	if err != nil {
		return nil, err
	}
	return s.loadDecisionsForSpecWithCtx(ctx, specFile)
}

func (s *Service) loadDecisionsForSpecWithCtx(ctx ResolvedContext, specFile string) ([]Decision, error) {
	specSlug := slugifySpecFile(specFile)
	dir := filepath.Join(historyRoot(ctx), "decisions", specSlug)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var all []Decision
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".jsonl") {
			continue
		}
		decisions, err := loadDecisionsFromFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			continue
		}
		all = append(all, decisions...)
	}
	return all, nil
}

// ListAllDecisions walks the decisions directory and returns every
// recorded decision across the project, grouped by spec file.
func (s *Service) ListAllDecisions(cwd string) (map[string][]Decision, error) {
	ctx, err := s.ResolveContext(cwd)
	if err != nil {
		return nil, err
	}
	root := filepath.Join(historyRoot(ctx), "decisions")
	result := map[string][]Decision{}

	if _, err := os.Stat(root); os.IsNotExist(err) {
		return result, nil
	}

	specDirs, err := os.ReadDir(root)
	if err != nil {
		return nil, err
	}
	for _, specDir := range specDirs {
		if !specDir.IsDir() {
			continue
		}
		taskFiles, err := os.ReadDir(filepath.Join(root, specDir.Name()))
		if err != nil {
			continue
		}
		for _, tf := range taskFiles {
			if !strings.HasSuffix(tf.Name(), ".jsonl") {
				continue
			}
			decisions, err := loadDecisionsFromFile(filepath.Join(root, specDir.Name(), tf.Name()))
			if err != nil {
				continue
			}
			result[specDir.Name()] = append(result[specDir.Name()], decisions...)
		}
	}
	return result, nil
}

func loadDecisionsFromFile(path string) ([]Decision, error) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer f.Close()

	var decisions []Decision
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var d Decision
		if err := json.Unmarshal([]byte(line), &d); err != nil {
			continue
		}
		decisions = append(decisions, d)
	}
	return decisions, scanner.Err()
}
