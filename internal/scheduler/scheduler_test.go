package scheduler

import (
	"context"
	"errors"
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

type fixedClock struct{ t time.Time }

func (c *fixedClock) Now() time.Time     { return c.t }
func (c *fixedClock) Advance(d time.Duration) { c.t = c.t.Add(d) }

func newTestScheduler(t *testing.T, maxConcurrent int) (*Scheduler, *trackerstub.Tracker, *agentstub.Agent, *fixedClock) {
	t.Helper()
	wf := &config.Workflow{
		Polling:    config.Polling{IntervalMs: 1000},
		Agent:      config.Agent{MaxConcurrentAgents: maxConcurrent, MaxRetryBackoffMs: 60_000},
		Tracker:    config.Tracker{ActiveStates: []string{"Todo"}, TerminalStates: []string{"Done"}},
		PromptBody: "{{ issue.identifier }}",
	}
	tr := trackerstub.New()
	ag := agentstub.New()
	clock := &fixedClock{t: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
	s := New(Options{
		Config:    wf,
		Tracker:   tr,
		Agent:     ag,
		Workspace: workspace.NewStub(),
		Logger:    slog.New(slog.NewTextHandler(io.Discard, nil)),
		Clock:     clock,
	})
	return s, tr, ag, clock
}

// waitFor spins until cond returns true or the deadline elapses. Used to wait
// for goroutine-driven completions without timing flakiness.
func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("timed out waiting for condition")
}

func TestTick_DispatchesAndSucceeds(t *testing.T) {
	s, tr, ag, _ := newTestScheduler(t, 3)
	tr.Set(domain.Issue{ID: "1", Identifier: "ABC-1", Title: "x", State: "Todo"})

	if err := s.Tick(context.Background()); err != nil {
		t.Fatalf("tick: %v", err)
	}
	s.WaitForIdle()
	if len(ag.Executed()) != 1 {
		t.Fatalf("expected 1 executed, got %d", len(ag.Executed()))
	}
	// In production the agent transitions the ticket out of active states
	// after success; stub mirrors that by removing the issue.
	tr.Remove("1")
	_ = s.Tick(context.Background())

	state, _ := s.store.State("1")
	if state != Released {
		t.Fatalf("expected Released after success, got %s", state)
	}
}

func TestTick_RespectsConcurrency(t *testing.T) {
	s, tr, ag, _ := newTestScheduler(t, 2)
	// Block the agent so dispatched runs stay Running.
	block := make(chan struct{})
	ag.SetDelay(func(_ agent.RunRequest) { <-block })

	for i := 1; i <= 5; i++ {
		tr.Set(domain.Issue{
			ID:         identifier(i),
			Identifier: identifier(i),
			Title:      "x",
			State:      "Todo",
			CreatedAt:  time.Now().Add(time.Duration(i) * time.Second),
		})
	}
	if err := s.Tick(context.Background()); err != nil {
		t.Fatalf("tick: %v", err)
	}
	waitFor(t, func() bool { return s.store.RunningCount() == 2 })

	// Tick again — must not exceed the cap even though more candidates exist.
	_ = s.Tick(context.Background())
	if got := s.store.RunningCount(); got != 2 {
		t.Fatalf("concurrency cap violated: %d running", got)
	}

	close(block)
	waitFor(t, func() bool { return len(ag.Executed()) >= 2 })
}

func TestTick_RetryOnFailure(t *testing.T) {
	s, tr, ag, clock := newTestScheduler(t, 1)
	tr.Set(domain.Issue{ID: "1", Identifier: "ABC-1", Title: "x", State: "Todo"})
	ag.Queue(agent.RunResult{Status: domain.StatusFailed, Err: errors.New("boom")})
	ag.Queue(agent.RunResult{Status: domain.StatusSucceeded})

	_ = s.Tick(context.Background())
	s.WaitForIdle()
	_ = s.Tick(context.Background())
	state, _ := s.store.State("1")
	if state != RetryQueued {
		t.Fatalf("expected RetryQueued after failure, got %s", state)
	}

	// Advance past the failure backoff and tick again.
	clock.Advance(10 * time.Second)
	_ = s.Tick(context.Background())
	s.WaitForIdle()
	if len(ag.Executed()) != 2 {
		t.Fatalf("expected 2 executions, got %d", len(ag.Executed()))
	}
	tr.Remove("1") // agent moved the ticket on success
	_ = s.Tick(context.Background())
	state, _ = s.store.State("1")
	if state != Released {
		t.Fatalf("expected Released after retry success, got %s", state)
	}
}

func TestTick_DoesNotRedispatchWhileRunning(t *testing.T) {
	s, tr, ag, _ := newTestScheduler(t, 5)
	block := make(chan struct{})
	ag.SetDelay(func(_ agent.RunRequest) { <-block })

	tr.Set(domain.Issue{ID: "1", Identifier: "ABC-1", Title: "x", State: "Todo"})

	_ = s.Tick(context.Background())
	// Wait for the goroutine to actually call the agent (the blocking point).
	waitFor(t, func() bool { return len(ag.Executed()) == 1 })

	_ = s.Tick(context.Background())
	if n := s.store.RunningCount(); n != 1 {
		t.Fatalf("expected 1 running after second tick, got %d", n)
	}
	if n := len(ag.Executed()); n != 1 {
		t.Fatalf("expected 1 executed (no duplicate dispatch), got %d", n)
	}
	close(block)
	s.WaitForIdle()
}

func TestSortByPriority(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	issues := []domain.Issue{
		{ID: "c", Priority: 2, CreatedAt: now},
		{ID: "a", Priority: 1, CreatedAt: now},
		{ID: "b", Priority: 1, CreatedAt: now.Add(time.Hour)},
	}
	sortByPriorityThenCreated(issues)
	got := []string{issues[0].ID, issues[1].ID, issues[2].ID}
	want := []string{"a", "b", "c"}
	for i, g := range got {
		if g != want[i] {
			t.Fatalf("sort wrong: got %v, want %v", got, want)
		}
	}
}

func identifier(i int) string {
	return string(rune('A'+i-1)) + "BC-" + string(rune('0'+i))
}
