package forge

import (
	"encoding/json"
	"sync"
	"testing"
)

func TestHandleAgentEventPersistsDecision(t *testing.T) {
	svc, webRepo := setupRunTestProject(t)
	ctx, err := svc.ResolveContext(webRepo)
	if err != nil {
		t.Fatalf("ResolveContext: %v", err)
	}

	task := RunPlanTask{
		File: "test-spec.md",
		Task: "Implement something",
	}

	details, _ := json.Marshal(map[string]any{
		"rationale":    "better type inference",
		"alternatives": []string{"option A", "option B"},
	})

	ev := Event{
		Type:    EventTypeDecision,
		Message: "Use option Z",
		Details: details,
	}

	blocked := map[string]string{}
	var mu sync.Mutex
	svc.handleAgentEvent(ctx, task, ev, blocked, &mu)

	decisions, err := svc.LoadDecisionsForTask(webRepo, task.File, task.Task)
	if err != nil {
		t.Fatalf("LoadDecisionsForTask: %v", err)
	}
	if len(decisions) != 1 {
		t.Fatalf("expected 1 decision persisted, got %d", len(decisions))
	}
	d := decisions[0]
	if d.Choice != "Use option Z" {
		t.Fatalf("unexpected choice: %s", d.Choice)
	}
	if d.Rationale != "better type inference" {
		t.Fatalf("unexpected rationale: %s", d.Rationale)
	}
	if len(d.Alternatives) != 2 {
		t.Fatalf("expected 2 alternatives, got %d", len(d.Alternatives))
	}
}

func TestHandleAgentEventDecisionWithoutDetails(t *testing.T) {
	svc, webRepo := setupRunTestProject(t)
	ctx, _ := svc.ResolveContext(webRepo)

	task := RunPlanTask{File: "spec.md", Task: "Plain task"}
	ev := Event{Type: EventTypeDecision, Message: "Simple choice"}

	blocked := map[string]string{}
	var mu sync.Mutex
	svc.handleAgentEvent(ctx, task, ev, blocked, &mu)

	decisions, _ := svc.LoadDecisionsForTask(webRepo, task.File, task.Task)
	if len(decisions) != 1 {
		t.Fatalf("expected 1 decision, got %d", len(decisions))
	}
	if decisions[0].Rationale != "" {
		t.Fatalf("rationale should be empty, got %q", decisions[0].Rationale)
	}
}

func TestHandleAgentEventBlockedDoesNotPersistDecision(t *testing.T) {
	svc, webRepo := setupRunTestProject(t)
	ctx, _ := svc.ResolveContext(webRepo)

	task := RunPlanTask{File: "spec.md", Task: "Blocked task"}
	ev := Event{Type: EventTypeBlocked, Message: "cannot proceed"}

	blocked := map[string]string{}
	var mu sync.Mutex
	svc.handleAgentEvent(ctx, task, ev, blocked, &mu)

	decisions, _ := svc.LoadDecisionsForTask(webRepo, task.File, task.Task)
	if len(decisions) != 0 {
		t.Fatalf("blocked event should not persist decisions, got %d", len(decisions))
	}
	if blocked[task.Task] != "cannot proceed" {
		t.Fatalf("blocked reason not stashed: %v", blocked)
	}
}
