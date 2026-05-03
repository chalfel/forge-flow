// Package scheduler implements the Symphony orchestration loop: poll a
// tracker, claim eligible issues, prepare a workspace, render the prompt,
// run an agent, and route terminal results to either Released or RetryQueued.
//
// The loop is built around a single Tick function so tests can drive it
// deterministically. Run wraps Tick with a ticker plus a completion channel
// so production usage stays goroutine-driven.
package scheduler

import (
	"context"
	"errors"
	"log/slog"
	"sort"
	"sync"
	"time"

	"github.com/chalfel/forge-flow/internal/agent"
	"github.com/chalfel/forge-flow/internal/config"
	"github.com/chalfel/forge-flow/internal/domain"
	"github.com/chalfel/forge-flow/internal/tracker"
	"github.com/chalfel/forge-flow/internal/workspace"
)

// Clock isolates time.Now so tests advance time without sleeping.
type Clock interface {
	Now() time.Time
}

type realClock struct{}

func (realClock) Now() time.Time { return time.Now() }

type Scheduler struct {
	cfg       *config.Workflow
	tracker   tracker.Tracker
	agent     agent.Agent
	workspace workspace.Manager
	store     *Store
	backoff   Backoff
	clock     Clock
	log       *slog.Logger

	completions chan completion
	wg          sync.WaitGroup
}

type completion struct {
	issue   domain.Issue
	attempt int
	result  agent.RunResult
}

type Options struct {
	Config    *config.Workflow
	Tracker   tracker.Tracker
	Agent     agent.Agent
	Workspace workspace.Manager
	Logger    *slog.Logger
	Clock     Clock
}

func New(opts Options) *Scheduler {
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}
	if opts.Clock == nil {
		opts.Clock = realClock{}
	}
	return &Scheduler{
		cfg:         opts.Config,
		tracker:     opts.Tracker,
		agent:       opts.Agent,
		workspace:   opts.Workspace,
		store:       NewStore(),
		backoff:     DefaultBackoff(opts.Config.Agent.MaxRetryBackoffMs),
		clock:       opts.Clock,
		log:         opts.Logger,
		completions: make(chan completion, 64),
	}
}

// Run drives the scheduler until ctx is canceled. Each tick polls the tracker
// and dispatches eligible issues; completions arrive asynchronously on the
// channel. Run returns nil on graceful shutdown after all in-flight runs
// finish.
func (s *Scheduler) Run(ctx context.Context) error {
	interval := time.Duration(s.cfg.Polling.IntervalMs) * time.Millisecond
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	if err := s.Tick(ctx); err != nil && !errors.Is(err, context.Canceled) {
		s.log.Error("initial tick", "err", err)
	}
	for {
		select {
		case <-ctx.Done():
			s.wg.Wait()
			return nil
		case c := <-s.completions:
			s.handleCompletion(c)
		case <-ticker.C:
			if err := s.Tick(ctx); err != nil && !errors.Is(err, context.Canceled) {
				s.log.Error("tick", "err", err)
			}
		}
	}
}

// Tick performs one polling iteration synchronously: drain ready completions,
// promote due retries, fetch candidates, sort, dispatch up to concurrency.
// It is exported so tests can step the loop without timing dependencies.
func (s *Scheduler) Tick(ctx context.Context) error {
	s.drainCompletions()

	now := s.clock.Now()
	if promoted := s.store.PromoteDueRetries(now); len(promoted) > 0 {
		s.log.Info("retries promoted", "count", len(promoted))
	}

	issues, err := s.tracker.FetchCandidates(ctx, s.cfg.Tracker.ActiveStates)
	if err != nil {
		return err
	}
	sortByPriorityThenCreated(issues)

	slots := s.cfg.Agent.MaxConcurrentAgents - s.store.RunningCount()
	for _, issue := range issues {
		if slots <= 0 {
			break
		}
		eli := eligible(issue, s.store, s.cfg.Tracker.TerminalStates)
		if !eli.OK {
			s.log.Debug("ineligible", "issue", issue.Identifier, "reason", eli.Reason)
			continue
		}
		if !s.store.TryClaim(issue.ID) {
			continue
		}
		s.dispatch(ctx, issue)
		slots--
	}
	return nil
}

// dispatch launches the workspace prep + prompt render + agent run on a
// goroutine. The result lands on s.completions so the next Tick reconciles.
func (s *Scheduler) dispatch(ctx context.Context, issue domain.Issue) {
	attempt := s.store.Attempt(issue.ID)
	s.store.MarkRunning(issue.ID, s.clock.Now())
	s.log.Info("dispatch", "issue", issue.Identifier, "attempt", attempt)

	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		ws, err := s.workspace.Prepare(ctx, issue)
		if err != nil {
			s.send(ctx, completion{issue: issue, attempt: attempt, result: agent.RunResult{Status: domain.StatusFailed, Err: err}})
			return
		}
		prompt := renderPrompt(s.cfg.PromptBody, issue, attempt)
		res := s.agent.Run(ctx, agent.RunRequest{
			Issue:     issue,
			Attempt:   attempt,
			Workspace: ws,
			Prompt:    prompt,
		})
		_ = s.workspace.Cleanup(ctx, ws)
		s.send(ctx, completion{issue: issue, attempt: attempt, result: res})
	}()
}

// send delivers a completion without blocking past ctx cancellation.
func (s *Scheduler) send(ctx context.Context, c completion) {
	select {
	case s.completions <- c:
	case <-ctx.Done():
	}
}

// drainCompletions handles any results delivered between Run select cases or
// invoked synchronously by tests.
func (s *Scheduler) drainCompletions() {
	for {
		select {
		case c := <-s.completions:
			s.handleCompletion(c)
		default:
			return
		}
	}
}

func (s *Scheduler) handleCompletion(c completion) {
	failed := c.result.Err != nil ||
		c.result.Status == domain.StatusFailed ||
		c.result.Status == domain.StatusTimedOut ||
		c.result.Status == domain.StatusStalled

	if !failed && c.result.Status == domain.StatusSucceeded {
		s.store.Release(c.issue.ID)
		s.log.Info("succeeded", "issue", c.issue.Identifier, "attempt", c.attempt)
		return
	}

	delay := s.backoff.NextDelay(c.attempt, failed)
	dueAt := s.clock.Now().Add(delay)
	errMsg := ""
	if c.result.Err != nil {
		errMsg = c.result.Err.Error()
	}
	s.store.ScheduleRetry(c.issue.ID, c.attempt+1, dueAt, errMsg)
	s.log.Info("retry scheduled",
		"issue", c.issue.Identifier,
		"next_attempt", c.attempt+1,
		"due_in_ms", delay.Milliseconds(),
		"failed", failed,
		"err", errMsg,
	)
}

// CompleteForTest is the test hook that pushes a completion into the loop
// without spinning up a real goroutine. Production code uses dispatch.
func (s *Scheduler) CompleteForTest(issue domain.Issue, attempt int, result agent.RunResult) {
	s.completions <- completion{issue: issue, attempt: attempt, result: result}
}

// WaitForIdle blocks until all currently dispatched goroutines have finished.
// Tests use this to step the loop deterministically: Tick, WaitForIdle, Tick.
// Do not call concurrently with other dispatchers.
func (s *Scheduler) WaitForIdle() { s.wg.Wait() }

// Snapshot returns a copy of all entries for observability and tests.
func (s *Scheduler) Snapshot() []EntrySnapshot { return s.store.Snapshot() }

func sortByPriorityThenCreated(issues []domain.Issue) {
	sort.SliceStable(issues, func(i, j int) bool {
		if issues[i].Priority != issues[j].Priority {
			return issues[i].Priority < issues[j].Priority
		}
		return issues[i].CreatedAt.Before(issues[j].CreatedAt)
	})
}
