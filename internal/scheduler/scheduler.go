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
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
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
	cfg       atomic.Pointer[config.Workflow]
	tracker   tracker.Tracker
	agent     agent.Agent
	workspace workspace.Manager
	store     *Store
	backoff   atomic.Pointer[Backoff]
	clock     Clock
	log       *slog.Logger

	completions chan completion
	refresh     chan struct{}
	cfgChanged  chan struct{}
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
	s := &Scheduler{
		tracker:     opts.Tracker,
		agent:       opts.Agent,
		workspace:   opts.Workspace,
		store:       NewStore(),
		clock:       opts.Clock,
		log:         opts.Logger,
		completions: make(chan completion, 64),
		refresh:     make(chan struct{}, 1),
		cfgChanged:  make(chan struct{}, 1),
	}
	s.cfg.Store(opts.Config)
	bo := DefaultBackoff(opts.Config.Agent.MaxRetryBackoffMs)
	s.backoff.Store(&bo)
	return s
}

// SetConfig atomically swaps the workflow. Most fields (prompt body,
// active/terminal states, hooks, concurrency, agent command) take effect on
// the next Tick. Polling cadence is honoured by the Run loop, which
// recreates its ticker when notified via cfgChanged.
func (s *Scheduler) SetConfig(wf *config.Workflow) {
	s.cfg.Store(wf)
	bo := DefaultBackoff(wf.Agent.MaxRetryBackoffMs)
	s.backoff.Store(&bo)
	select {
	case s.cfgChanged <- struct{}{}:
	default:
	}
}

func (s *Scheduler) currentCfg() *config.Workflow { return s.cfg.Load() }
func (s *Scheduler) currentBackoff() Backoff      { return *s.backoff.Load() }

// Refresh requests an immediate tick. Multiple concurrent calls collapse to
// a single tick because the channel has capacity 1. Used by the
// observability HTTP API and operator CLIs.
func (s *Scheduler) Refresh() {
	select {
	case s.refresh <- struct{}{}:
	default:
	}
}

// Config exposes the workflow for read-only observability surfaces. Returns
// the currently active workflow after any dynamic reload.
func (s *Scheduler) Config() *config.Workflow { return s.currentCfg() }

// Run drives the scheduler until ctx is canceled. Each tick polls the tracker
// and dispatches eligible issues; completions arrive asynchronously on the
// channel. Run returns nil on graceful shutdown after all in-flight runs
// finish.
func (s *Scheduler) Run(ctx context.Context) error {
	ticker := time.NewTicker(s.tickInterval())
	defer func() { ticker.Stop() }()

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
		case <-s.refresh:
			if err := s.Tick(ctx); err != nil && !errors.Is(err, context.Canceled) {
				s.log.Error("refresh tick", "err", err)
			}
		case <-s.cfgChanged:
			ticker.Stop()
			ticker = time.NewTicker(s.tickInterval())
			s.log.Info("workflow reloaded; ticker restarted", "interval_ms", s.currentCfg().Polling.IntervalMs)
		case <-ticker.C:
			if err := s.Tick(ctx); err != nil && !errors.Is(err, context.Canceled) {
				s.log.Error("tick", "err", err)
			}
		}
	}
}

func (s *Scheduler) tickInterval() time.Duration {
	return time.Duration(s.currentCfg().Polling.IntervalMs) * time.Millisecond
}

// Tick performs one polling iteration synchronously: drain ready completions,
// reconcile running issues against the tracker (cancel any whose state moved
// out of active), promote due retries, fetch candidates, sort, dispatch up to
// concurrency. Exported so tests can step the loop without timing
// dependencies.
func (s *Scheduler) Tick(ctx context.Context) error {
	s.drainCompletions()
	s.reconcileRunning(ctx)

	now := s.clock.Now()
	if promoted := s.store.PromoteDueRetries(now); len(promoted) > 0 {
		s.log.Info("retries promoted", "count", len(promoted))
	}

	cfg := s.currentCfg()
	issues, err := s.tracker.FetchCandidates(ctx, cfg.Tracker.ActiveStates)
	if err != nil {
		return err
	}
	sortByPriorityThenCreated(issues)

	slots := cfg.Agent.MaxConcurrentAgents - s.store.RunningCount()
	for _, issue := range issues {
		if slots <= 0 {
			break
		}
		eli := eligible(issue, s.store, cfg.Tracker.TerminalStates)
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
// goroutine. The goroutine derives its context from a per-attempt cancel
// function so reconciliation can interrupt the run when the tracker moves
// the issue to a terminal state. The result lands on s.completions.
func (s *Scheduler) dispatch(ctx context.Context, issue domain.Issue) {
	attempt := s.store.Attempt(issue.ID)
	runCtx, cancel := context.WithCancel(ctx)
	sessionID := newSessionID()
	s.store.MarkRunning(issue.ID, s.clock.Now(), cancel, sessionID)
	s.log.Info("dispatch", "issue", issue.Identifier, "attempt", attempt, "session_id", sessionID)

	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		defer cancel()
		ws, err := s.workspace.Prepare(runCtx, issue)
		if err != nil {
			s.send(ctx, completion{issue: issue, attempt: attempt, result: agent.RunResult{Status: domain.StatusFailed, Err: err}})
			return
		}
		// SPEC.md mandatory baseline: validate cwd before launch. We only
		// enforce when the workflow declares a root; an empty root means
		// "no production constraint" and is used by the in-memory test
		// stubs. The FS workspace manager always sets a real root.
		if root := s.currentCfg().Workspace.Root; root != "" {
			if err := assertContained(root, ws.Path); err != nil {
				_ = s.workspace.Cleanup(ctx, ws)
				s.send(ctx, completion{issue: issue, attempt: attempt, result: agent.RunResult{Status: domain.StatusFailed, Err: err}})
				return
			}
		}
		prompt := renderPrompt(s.currentCfg().PromptBody, issue, attempt, ws.Path)
		res := s.agent.Run(runCtx, agent.RunRequest{
			Issue:     issue,
			Attempt:   attempt,
			Workspace: ws,
			Prompt:    prompt,
			SessionID: sessionID,
		})
		// If reconciliation cancelled us, override the agent's status so
		// the scheduler treats it as a reconciliation cancellation rather
		// than a generic failure.
		if errors.Is(runCtx.Err(), context.Canceled) && ctx.Err() == nil {
			res.Status = domain.StatusCanceledByReconcile
			if res.Err == nil {
				res.Err = context.Canceled
			}
		}
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

	if !failed && c.result.Status == domain.StatusDecomposed {
		s.store.Skip(c.issue.ID)
		s.store.Release(c.issue.ID)
		s.log.Info("decomposed by captain", "issue", c.issue.Identifier, "attempt", c.attempt)
		return
	}
	if !failed && c.result.Status == domain.StatusSucceeded {
		s.store.Release(c.issue.ID)
		s.log.Info("succeeded", "issue", c.issue.Identifier, "attempt", c.attempt)
		return
	}

	delay := s.currentBackoff().NextDelay(c.attempt, failed)
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

// reconcileRunning refreshes tracker state for every Running issue. If the
// tracker no longer reports the issue in any active state (or returns nil),
// the dispatched goroutine is cancelled — the operator manually moved the
// ticket on the tracker side and we should not waste compute completing it.
func (s *Scheduler) reconcileRunning(ctx context.Context) {
	ids := s.store.RunningIDs()
	if len(ids) == 0 {
		return
	}
	cfg := s.currentCfg()
	for _, id := range ids {
		issue, err := s.tracker.GetIssue(ctx, id)
		if err != nil {
			s.log.Warn("reconcile: get_issue failed; skipping", "issue_id", id, "err", err)
			continue
		}
		if issue == nil || !inSet(issue.State, cfg.Tracker.ActiveStates) {
			if s.store.CancelRunning(id) {
				s.log.Info("reconcile: cancelling run; tracker state out of active set",
					"issue_id", id,
					"tracker_state", trackerStateOrNone(issue))
			}
		}
	}
}

func inSet(s string, set []string) bool {
	for _, x := range set {
		if x == s {
			return true
		}
	}
	return false
}

func trackerStateOrNone(i *domain.Issue) string {
	if i == nil {
		return "<not found>"
	}
	return i.State
}

// assertContained verifies that path lies inside root. Mirrors the same
// invariant the FS workspace manager enforces; running it here too means a
// custom Manager cannot accidentally hand the agent a path outside the
// configured root.
func assertContained(root, path string) error {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return fmt.Errorf("workspace containment: %w", err)
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return fmt.Errorf("workspace path %q escapes root %q", path, root)
	}
	return nil
}

// newSessionID returns a short opaque identifier suitable for log
// correlation. The shell runner does not have a real Codex thread/turn
// session, so we mint one ourselves so structured logs include the
// `session_id` key required by the spec.
func newSessionID() string {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return fmt.Sprintf("ss-%d", time.Now().UnixNano())
	}
	return "ss-" + hex.EncodeToString(b[:])
}
