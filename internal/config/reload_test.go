package config

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func writeWorkflow(t *testing.T, dir, body string) string {
	t.Helper()
	p := filepath.Join(dir, "WORKFLOW.md")
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	return p
}

func TestWatcher_TriggersOnMtimeChange(t *testing.T) {
	t.Setenv("LINEAR_API_KEY", "lin_test")
	dir := t.TempDir()
	p := writeWorkflow(t, dir, `---
tracker: { kind: linear, project_slug: A, api_key: $LINEAR_API_KEY, active_states: [Todo] }
codex: { command: codex }
---
v1`)

	w := NewWatcher(WatcherOptions{
		Path:     p,
		Interval: 50 * time.Millisecond,
		Logger:   slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var (
		mu       sync.Mutex
		received []*Workflow
	)
	go w.Run(ctx, func(wf *Workflow) {
		mu.Lock()
		received = append(received, wf)
		mu.Unlock()
	})

	// Bump mtime explicitly to avoid 1s mtime-resolution flakes on some FS.
	time.Sleep(100 * time.Millisecond)
	future := time.Now().Add(2 * time.Second)
	_ = os.Chtimes(p, future, future)
	_ = os.WriteFile(p, []byte(`---
tracker: { kind: linear, project_slug: A, api_key: $LINEAR_API_KEY, active_states: [Todo] }
codex: { command: codex }
---
v2`), 0o644)
	_ = os.Chtimes(p, future, future)

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		n := len(received)
		mu.Unlock()
		if n >= 1 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(received) == 0 {
		t.Fatal("expected at least one reload")
	}
	if got := received[len(received)-1].PromptBody; got != "v2" {
		t.Fatalf("last reload prompt body = %q, want v2", got)
	}
}

func TestWatcher_InvalidReloadKept(t *testing.T) {
	t.Setenv("LINEAR_API_KEY", "lin_test")
	dir := t.TempDir()
	p := writeWorkflow(t, dir, `---
tracker: { kind: linear, project_slug: A, api_key: $LINEAR_API_KEY, active_states: [Todo] }
codex: { command: codex }
---
v1`)

	w := NewWatcher(WatcherOptions{
		Path:     p,
		Interval: 50 * time.Millisecond,
		Logger:   slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var calls int
	var mu sync.Mutex
	go w.Run(ctx, func(_ *Workflow) {
		mu.Lock()
		calls++
		mu.Unlock()
	})

	// Replace with invalid (missing project_slug + api_key) — Validate should fail.
	time.Sleep(100 * time.Millisecond)
	future := time.Now().Add(2 * time.Second)
	_ = os.WriteFile(p, []byte(`---
tracker: { kind: linear, active_states: [Todo] }
codex: { command: codex }
---
broken`), 0o644)
	_ = os.Chtimes(p, future, future)

	time.Sleep(300 * time.Millisecond)
	mu.Lock()
	defer mu.Unlock()
	if calls != 0 {
		t.Fatalf("invalid reload should not invoke callback, got %d calls", calls)
	}
}
