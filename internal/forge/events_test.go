package forge

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestEmitEventAndLoad(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "events.jsonl")

	ev := Event{
		Type:    EventTypeProgress,
		Message: "Scaffolded types",
	}
	err := EmitEvent(path, ev)
	if err != nil {
		t.Fatalf("EmitEvent: %v", err)
	}

	events, err := LoadEvents(path)
	if err != nil {
		t.Fatalf("LoadEvents: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if events[0].Type != EventTypeProgress {
		t.Fatalf("unexpected type: %s", events[0].Type)
	}
	if events[0].Timestamp == "" {
		t.Fatal("timestamp should be auto-set")
	}
}

func TestEmitMultipleEvents(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "events.jsonl")

	_ = EmitEvent(path, Event{Type: EventTypeProgress, Message: "step 1"})
	_ = EmitEvent(path, Event{Type: EventTypeDecision, Message: "using Zod"})
	_ = EmitEvent(path, Event{Type: EventTypeBlocked, Message: "missing scaffold"})

	events, err := LoadEvents(path)
	if err != nil {
		t.Fatalf("LoadEvents: %v", err)
	}
	if len(events) != 3 {
		t.Fatalf("expected 3 events, got %d", len(events))
	}
	if events[2].Type != EventTypeBlocked {
		t.Fatalf("expected last event to be blocked, got %s", events[2].Type)
	}
}

func TestLoadEventsSkipsMalformedLines(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "events.jsonl")

	content := `{"type":"progress","message":"ok"}
not valid json
{"type":"blocked","message":"stuck"}
`
	_ = os.WriteFile(path, []byte(content), 0o644)

	events, _ := LoadEvents(path)
	if len(events) != 2 {
		t.Fatalf("expected 2 valid events, got %d", len(events))
	}
}

func TestLoadEventsMissingFile(t *testing.T) {
	events, err := LoadEvents("/nonexistent/path.jsonl")
	if err != nil {
		t.Fatalf("expected nil error for missing file, got %v", err)
	}
	if events != nil {
		t.Fatal("expected nil events")
	}
}

func TestEventsFilePath(t *testing.T) {
	ctx := ResolvedContext{RepoLocalForge: "/repo/.forge"}
	path := EventsFilePath(ctx, "run-123", "Build the thing!")
	expected := filepath.Join("/repo/.forge", "runtime", "run-123", "build-the-thing.events.jsonl")
	if path != expected {
		t.Errorf("expected %q, got %q", expected, path)
	}
}

func TestEventDetailsRoundtrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "events.jsonl")

	details, _ := json.Marshal(map[string]any{
		"alternatives": []string{"yup", "joi"},
		"rationale":    "Better TS inference",
	})
	ev := Event{
		Type:    EventTypeDecision,
		Message: "Using Zod",
		Details: details,
	}
	_ = EmitEvent(path, ev)

	events, _ := LoadEvents(path)
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if len(events[0].Details) == 0 {
		t.Fatal("details should be preserved")
	}
	var parsed map[string]any
	_ = json.Unmarshal(events[0].Details, &parsed)
	if parsed["rationale"] != "Better TS inference" {
		t.Fatal("details content lost in roundtrip")
	}
}

func TestTailEventsProcessesNewEntries(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "events.jsonl")

	// Write initial event
	_ = EmitEvent(path, Event{Type: EventTypeProgress, Message: "start"})

	var received []Event
	var mu sync.Mutex

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		_ = TailEvents(ctx, path, func(ev Event) {
			mu.Lock()
			received = append(received, ev)
			mu.Unlock()
		}, 20*time.Millisecond)
		close(done)
	}()

	// Wait for the initial event to be picked up
	time.Sleep(80 * time.Millisecond)

	// Append more events
	_ = EmitEvent(path, Event{Type: EventTypeDecision, Message: "decided"})
	_ = EmitEvent(path, Event{Type: EventTypeBlocked, Message: "stuck"})

	// Give the tail time to process
	time.Sleep(80 * time.Millisecond)
	cancel()
	<-done

	mu.Lock()
	got := len(received)
	mu.Unlock()

	if got < 3 {
		t.Fatalf("expected at least 3 events, got %d", got)
	}
}
