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

// TestReconcile_CancelsRunningWhenTrackerMovesIssueToTerminal exercises the
// SPEC.md reconciliation requirement: each tick refreshes the tracker view
// of every Running issue and cancels any whose state has fallen out of the
// configured active set.
func TestReconcile_CancelsRunningWhenTrackerMovesIssueToTerminal(t *testing.T) {
	wf := &config.Workflow{
		Polling:    config.Polling{IntervalMs: 1000},
		Agent:      config.Agent{MaxConcurrentAgents: 3, MaxRetryBackoffMs: 60_000},
		Tracker:    config.Tracker{ActiveStates: []string{"Todo"}, TerminalStates: []string{"Done"}},
		PromptBody: "{{ issue.identifier }}",
	}
	tr := trackerstub.New()
	ag := agentstub.New()

	// Block the agent indefinitely so the issue stays in Running state until
	// the reconcile cancels it.
	released := make(chan struct{})
	cancelObserved := make(chan struct{}, 1)
	delayEntered := make(chan struct{}, 1)
	ag.SetDelay(func(req agent.RunRequest) {
		select {
		case delayEntered <- struct{}{}:
		default:
		}
		<-released
		cancelObserved <- struct{}{}
	})

	clock := &fixedClock{t: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
	s := New(Options{
		Config:    wf,
		Tracker:   tr,
		Agent:     ag,
		Workspace: workspace.NewStub(),
		Logger:    slog.New(slog.NewTextHandler(io.Discard, nil)),
		Clock:     clock,
	})

	tr.Set(domain.Issue{ID: "1", Identifier: "ABC-1", Title: "x", State: "Todo"})

	// Tick 1: dispatches and the issue enters Running.
	if err := s.Tick(context.Background()); err != nil {
		t.Fatalf("tick: %v", err)
	}
	// Wait until the dispatch goroutine has actually called the agent
	// (delay entered). Otherwise reconcile would see no running agent.
	select {
	case <-delayEntered:
	case <-time.After(2 * time.Second):
		t.Fatal("dispatch never reached agent.Run")
	}

	// Operator moves the ticket on the tracker out of active states.
	tr.Set(domain.Issue{ID: "1", Identifier: "ABC-1", Title: "x", State: "Done"})

	// Tick 2: reconcile must cancel the dispatched run.
	_ = s.Tick(context.Background())

	// Now release the agent so it returns. The dispatch goroutine should
	// observe the cancellation and post a CanceledByReconcile completion.
	close(released)
	select {
	case <-cancelObserved:
	case <-time.After(2 * time.Second):
		t.Fatal("agent never returned after cancel")
	}
	s.WaitForIdle()

	// Tick 3: drains the cancellation completion → backoff retry queued.
	_ = s.Tick(context.Background())
	st, _ := s.store.State("1")
	if st != RetryQueued && st != Released {
		t.Fatalf("expected RetryQueued or Released, got %s", st)
	}
}
