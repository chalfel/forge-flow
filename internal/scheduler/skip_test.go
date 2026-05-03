package scheduler

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/chalfel/forge-flow/internal/agent"
	agentstub "github.com/chalfel/forge-flow/internal/agent/stub"
	"github.com/chalfel/forge-flow/internal/config"
	"github.com/chalfel/forge-flow/internal/domain"
	trackerstub "github.com/chalfel/forge-flow/internal/tracker/stub"
	"github.com/chalfel/forge-flow/internal/workspace"
)

func TestSkip_PreventsRedispatch(t *testing.T) {
	wf := &config.Workflow{
		Polling:    config.Polling{IntervalMs: 1000},
		Agent:      config.Agent{MaxConcurrentAgents: 3, MaxRetryBackoffMs: 60_000},
		Tracker:    config.Tracker{ActiveStates: []string{"Todo"}, TerminalStates: []string{"Done"}},
		PromptBody: "{{ issue.identifier }}",
	}
	tr := trackerstub.New()
	ag := agentstub.New()
	ag.Queue(agent.RunResult{Status: domain.StatusDecomposed, Output: "decomposed"})

	clock := &fixedClock{t: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
	s := New(Options{
		Config:    wf,
		Tracker:   tr,
		Agent:     ag,
		Workspace: workspace.NewStub(),
		Logger:    slog.New(slog.NewTextHandler(io.Discard, nil)),
		Clock:     clock,
	})

	tr.Set(domain.Issue{ID: "parent", Identifier: "X-99", Title: "decompose me", State: "Todo"})

	// First tick: dispatches captain.
	if err := s.Tick(context.Background()); err != nil {
		t.Fatalf("tick: %v", err)
	}
	s.WaitForIdle()
	// Second tick: drains the StatusDecomposed completion → skip set.
	_ = s.Tick(context.Background())
	if !s.store.IsSkipped("parent") {
		t.Fatal("expected parent to be in skip set")
	}
	if got := len(ag.Executed()); got != 1 {
		t.Fatalf("expected 1 dispatch, got %d", got)
	}

	// Third tick: tracker still reports the parent in active states, but the
	// scheduler must NOT re-dispatch because IsSkipped(parent) is true.
	_ = s.Tick(context.Background())
	if got := len(ag.Executed()); got != 1 {
		t.Fatalf("expected dispatch count to stay at 1, got %d (skip set leaked)", got)
	}
}
