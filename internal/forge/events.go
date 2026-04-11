package forge

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Event is a structured message emitted by an agent while working on a task.
// Agents write one JSON object per line to the events.jsonl file; Forge reads
// the file periodically to show real-time status and react to blockers.
type Event struct {
	Type      string          `json:"type"`                // progress|decision|blocked|need_info|handoff|proposed_memory
	Message   string          `json:"message"`
	Timestamp string          `json:"timestamp"`           // RFC3339
	Details   json.RawMessage `json:"details,omitempty"`   // type-specific payload
	// Derived fields populated by LoadEvents — not written by agents.
	TaskName string `json:"taskName,omitempty"`
	RunID    string `json:"runId,omitempty"`
}

// Event types.
const (
	EventTypeProgress       = "progress"
	EventTypeDecision       = "decision"
	EventTypeBlocked        = "blocked"
	EventTypeNeedInfo       = "need_info"
	EventTypeHandoff        = "handoff"
	EventTypeProposedMemory = "proposed_memory"
)

// EventsFilePath returns the canonical path for a task's events.jsonl file
// inside the runtime directory of a specific run.
func EventsFilePath(ctx ResolvedContext, runID, taskName string) string {
	base := ctx.RepoLocalForge
	if base == "" {
		base = ctx.ProjectRoot
	}
	return filepath.Join(base, "runtime", runID, slugify(taskName)+".events.jsonl")
}

// EnsureEventsFile creates the parent directory for an events file so
// the agent can write to it without creating directories itself.
func EnsureEventsFile(path string) error {
	return os.MkdirAll(filepath.Dir(path), 0o755)
}

// LoadEvents reads all events from a file. Returns nil if the file doesn't exist.
func LoadEvents(path string) ([]Event, error) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer f.Close()

	var events []Event
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var ev Event
		if err := json.Unmarshal([]byte(line), &ev); err != nil {
			continue // skip malformed lines
		}
		events = append(events, ev)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return events, nil
}

// eventTail watches a set of events files for new lines and calls the
// handler for each new event. It polls because file-based tail is cross-platform
// and doesn't need fsnotify. The poll interval is fine for a background watcher.
type eventTail struct {
	paths    map[string]int64 // path -> last byte offset consumed
	mu       sync.Mutex
	handler  func(path string, ev Event)
	interval time.Duration
}

func newEventTail(handler func(path string, ev Event), interval time.Duration) *eventTail {
	return &eventTail{
		paths:    map[string]int64{},
		handler:  handler,
		interval: interval,
	}
}

// Watch adds a file path to the tail watcher.
func (t *eventTail) Watch(path string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if _, ok := t.paths[path]; !ok {
		t.paths[path] = 0
	}
}

// Run polls all watched files for new content until ctx is cancelled.
func (t *eventTail) Run(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-time.After(t.interval):
		}
		t.poll()
	}
}

// poll reads new content from each watched file and emits events for
// any new lines since the last read.
func (t *eventTail) poll() {
	t.mu.Lock()
	paths := make([]string, 0, len(t.paths))
	for p := range t.paths {
		paths = append(paths, p)
	}
	t.mu.Unlock()

	for _, path := range paths {
		t.mu.Lock()
		offset := t.paths[path]
		t.mu.Unlock()

		f, err := os.Open(path)
		if err != nil {
			continue
		}
		info, err := f.Stat()
		if err != nil {
			f.Close()
			continue
		}
		size := info.Size()
		if size <= offset {
			f.Close()
			continue
		}
		if _, err := f.Seek(offset, 0); err != nil {
			f.Close()
			continue
		}

		scanner := bufio.NewScanner(f)
		scanner.Buffer(make([]byte, 64*1024), 1024*1024)
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if line == "" {
				continue
			}
			var ev Event
			if err := json.Unmarshal([]byte(line), &ev); err != nil {
				continue
			}
			t.handler(path, ev)
		}
		f.Close()

		t.mu.Lock()
		t.paths[path] = size
		t.mu.Unlock()
	}
}

// TailEvents is a convenience wrapper that tails a single file until ctx is cancelled,
// calling handler for each new event.
func TailEvents(ctx context.Context, path string, handler func(Event), interval time.Duration) error {
	if interval <= 0 {
		interval = 1 * time.Second
	}
	tail := newEventTail(func(_ string, ev Event) { handler(ev) }, interval)
	tail.Watch(path)
	tail.Run(ctx)
	return nil
}

// TailEventsFromOffset tails a file starting at a given byte offset.
// Useful when the caller has already processed the initial content.
func TailEventsFromOffset(ctx context.Context, path string, offset int64, handler func(Event), interval time.Duration) error {
	if interval <= 0 {
		interval = 1 * time.Second
	}
	tail := newEventTail(func(_ string, ev Event) { handler(ev) }, interval)
	tail.mu.Lock()
	tail.paths[path] = offset
	tail.mu.Unlock()
	tail.Run(ctx)
	return nil
}

// LoadRunEvents walks the runtime directory for a given run ID and
// returns all events from every task's events file, tagged with task name.
func (s *Service) LoadRunEvents(cwd, runID string) ([]Event, error) {
	ctx, err := s.ResolveContext(cwd)
	if err != nil {
		return nil, err
	}
	base := ctx.RepoLocalForge
	if base == "" {
		base = ctx.ProjectRoot
	}
	runDir := filepath.Join(base, "runtime", runID)

	entries, err := os.ReadDir(runDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var all []Event
	for _, entry := range entries {
		name := entry.Name()
		if !strings.HasSuffix(name, ".events.jsonl") {
			continue
		}
		taskSlug := strings.TrimSuffix(name, ".events.jsonl")
		events, err := LoadEvents(filepath.Join(runDir, name))
		if err != nil {
			continue
		}
		for i := range events {
			events[i].TaskName = taskSlug
			events[i].RunID = runID
		}
		all = append(all, events...)
	}
	return all, nil
}

// EmitEvent is a convenience for Forge itself to write events into an agent's
// events file (e.g., system notifications). Agents write events too but they
// typically use plain shell `echo ... >> $FORGE_EVENTS_FILE`.
func EmitEvent(path string, ev Event) error {
	if err := EnsureEventsFile(path); err != nil {
		return err
	}
	if ev.Timestamp == "" {
		ev.Timestamp = timeNow().UTC().Format(time.RFC3339)
	}
	data, err := json.Marshal(ev)
	if err != nil {
		return err
	}

	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("opening events file: %w", err)
	}
	defer f.Close()
	_, err = f.Write(append(data, '\n'))
	return err
}
