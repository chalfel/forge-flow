package forge

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// TaskAttemptRecord is one line in the attempts jsonl file.
// Represents a single execution attempt against a specific task.
type TaskAttemptRecord struct {
	Attempt         int              `json:"attempt"`
	RunID           string           `json:"runId"`
	Agent           string           `json:"agent"`
	SpecFile        string           `json:"specFile"`
	TaskName        string           `json:"taskName"`
	StartedAt       string           `json:"startedAt"`
	EndedAt         string           `json:"endedAt,omitempty"`
	Status          string           `json:"status"` // running|succeeded|failed|changes_requested|done|skipped
	Error           string           `json:"error,omitempty"`
	Commits         []string         `json:"commits,omitempty"`
	PR              string           `json:"pr,omitempty"`
	ReviewFeedback  []FeedbackItem   `json:"reviewFeedback,omitempty"`
}

// FeedbackItem is a single structured review comment.
type FeedbackItem struct {
	Kind string `json:"kind"` // bug|style|missing|suggestion|question
	Text string `json:"text"`
	File string `json:"file,omitempty"`
	Line int    `json:"line,omitempty"`
}

// StructuredFeedback is the JSON persisted alongside a markdown review.
type StructuredFeedback struct {
	ReviewedAt       string         `json:"reviewedAt"`
	Reviewer         string         `json:"reviewer,omitempty"`
	Status           string         `json:"status"` // changes_requested|done
	Items            []FeedbackItem `json:"items"`
	OriginalMarkdown string         `json:"originalMarkdown,omitempty"`
}

// historyRoot returns {repoLocalForge}/history or falls back to project root.
func historyRoot(ctx ResolvedContext) string {
	if ctx.RepoLocalForge != "" {
		return filepath.Join(ctx.RepoLocalForge, "history")
	}
	return filepath.Join(ctx.ProjectRoot, "history")
}

// attemptsPath returns the path for a task's attempts jsonl file.
func attemptsPath(ctx ResolvedContext, specFile, taskName string) string {
	specSlug := slugifySpecFile(specFile)
	taskSlug := slugify(taskName)
	return filepath.Join(historyRoot(ctx), "attempts", specSlug, taskSlug+".jsonl")
}

// feedbackPath returns the path for a task's structured feedback json file.
func feedbackPath(ctx ResolvedContext, specFile, taskName string) string {
	specSlug := slugifySpecFile(specFile)
	taskSlug := slugify(taskName)
	return filepath.Join(historyRoot(ctx), "feedback", specSlug, taskSlug+".json")
}

// slugifySpecFile strips .md extension and slugifies the filename.
func slugifySpecFile(specFile string) string {
	base := filepath.Base(specFile)
	base = strings.TrimSuffix(base, ".md")
	return slugify(base)
}

// AppendAttempt writes a new attempt entry to the task's jsonl file.
// The attempt number is auto-computed from existing entries.
func (s *Service) AppendAttempt(cwd string, record TaskAttemptRecord) error {
	ctx, err := s.ResolveContext(cwd)
	if err != nil {
		return err
	}
	return s.appendAttemptWithCtx(ctx, record)
}

// appendAttemptWithCtx is the internal helper that skips ResolveContext
// so callers that already have a ResolvedContext can reuse it.
func (s *Service) appendAttemptWithCtx(ctx ResolvedContext, record TaskAttemptRecord) error {
	path := attemptsPath(ctx, record.SpecFile, record.TaskName)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("creating attempts dir: %w", err)
	}

	// Auto-compute attempt number if zero.
	if record.Attempt == 0 {
		existing, _ := loadAttemptsFromFile(path)
		record.Attempt = len(existing) + 1
	}
	if record.StartedAt == "" {
		record.StartedAt = timeNow().UTC().Format(time.RFC3339)
	}
	if record.Agent == "" {
		record.Agent = "claude"
	}

	data, err := json.Marshal(record)
	if err != nil {
		return err
	}

	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("opening attempts file: %w", err)
	}
	defer f.Close()

	if _, err := f.Write(append(data, '\n')); err != nil {
		return fmt.Errorf("writing attempt: %w", err)
	}
	return nil
}

// UpdateLatestAttempt rewrites the last attempt entry with updated fields
// (typically to set EndedAt, Status, Error after a task completes).
func (s *Service) UpdateLatestAttempt(cwd, specFile, taskName string, updater func(*TaskAttemptRecord)) error {
	ctx, err := s.ResolveContext(cwd)
	if err != nil {
		return err
	}
	return s.updateLatestAttemptWithCtx(ctx, specFile, taskName, updater)
}

func (s *Service) updateLatestAttemptWithCtx(ctx ResolvedContext, specFile, taskName string, updater func(*TaskAttemptRecord)) error {
	path := attemptsPath(ctx, specFile, taskName)

	records, err := loadAttemptsFromFile(path)
	if err != nil {
		return err
	}
	if len(records) == 0 {
		return fmt.Errorf("no attempts found for task %q", taskName)
	}

	last := &records[len(records)-1]
	updater(last)
	if last.EndedAt == "" {
		last.EndedAt = timeNow().UTC().Format(time.RFC3339)
	}

	// Rewrite the file.
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	for _, r := range records {
		data, _ := json.Marshal(r)
		if _, err := f.Write(append(data, '\n')); err != nil {
			return err
		}
	}
	return nil
}

// LoadAttempts returns all recorded attempts for a task, oldest first.
func (s *Service) LoadAttempts(cwd, specFile, taskName string) ([]TaskAttemptRecord, error) {
	ctx, err := s.ResolveContext(cwd)
	if err != nil {
		return nil, err
	}
	return s.loadAttemptsWithCtx(ctx, specFile, taskName)
}

func (s *Service) loadAttemptsWithCtx(ctx ResolvedContext, specFile, taskName string) ([]TaskAttemptRecord, error) {
	return loadAttemptsFromFile(attemptsPath(ctx, specFile, taskName))
}

func loadAttemptsFromFile(path string) ([]TaskAttemptRecord, error) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer f.Close()

	var records []TaskAttemptRecord
	scanner := bufio.NewScanner(f)
	// Allow large lines (up to 1MB) in case attempts include long error messages.
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var rec TaskAttemptRecord
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			continue // skip malformed lines
		}
		records = append(records, rec)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return records, nil
}

// WriteFeedback persists structured review feedback for a task.
func (s *Service) WriteFeedback(cwd, specFile, taskName string, fb StructuredFeedback) error {
	ctx, err := s.ResolveContext(cwd)
	if err != nil {
		return err
	}
	if fb.ReviewedAt == "" {
		fb.ReviewedAt = timeNow().UTC().Format(time.RFC3339)
	}
	path := feedbackPath(ctx, specFile, taskName)
	return writeJSON(path, fb)
}

// LoadFeedback returns the most recent structured feedback for a task.
// Returns nil, nil if no feedback file exists.
func (s *Service) LoadFeedback(cwd, specFile, taskName string) (*StructuredFeedback, error) {
	ctx, err := s.ResolveContext(cwd)
	if err != nil {
		return nil, err
	}
	return s.loadFeedbackWithCtx(ctx, specFile, taskName)
}

func (s *Service) loadFeedbackWithCtx(ctx ResolvedContext, specFile, taskName string) (*StructuredFeedback, error) {
	path := feedbackPath(ctx, specFile, taskName)
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return nil, nil
	}
	var fb StructuredFeedback
	if err := readJSON(path, &fb); err != nil {
		return nil, err
	}
	return &fb, nil
}

// ListTaskHistory returns all tasks that have recorded attempts,
// grouped by spec file. Useful for the 'forge history list' command.
func (s *Service) ListTaskHistory(cwd string) (map[string][]string, error) {
	ctx, err := s.ResolveContext(cwd)
	if err != nil {
		return nil, err
	}
	root := filepath.Join(historyRoot(ctx), "attempts")
	result := map[string][]string{}

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
			taskSlug := strings.TrimSuffix(tf.Name(), ".jsonl")
			result[specDir.Name()] = append(result[specDir.Name()], taskSlug)
		}
	}
	return result, nil
}
