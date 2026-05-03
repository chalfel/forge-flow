// Package shell is a generic Agent runner that launches a shell command
// inside the workspace, pipes the rendered prompt to stdin, and maps the
// process exit code to an attempt status.
//
// Codex (Phase 6) and Claude Code (Phase 7) both compose on top of this:
// they only differ in the default command and which AgentCommand block in
// the workflow they read. Full Codex app-server stdio (thread management,
// token telemetry) can layer in later behind the same agent.Agent
// interface — this runner is the minimum that gets agents actually running
// against real tickets.
package shell

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/chalfel/forge-flow/internal/agent"
	"github.com/chalfel/forge-flow/internal/config"
	"github.com/chalfel/forge-flow/internal/domain"
)

type Runner struct {
	command      string
	turnTimeout  time.Duration
	stallTimeout time.Duration
	log          *slog.Logger
}

type Options struct {
	Command        string
	TurnTimeoutMs  int
	StallTimeoutMs int
	Logger         *slog.Logger
}

func New(opts Options) (*Runner, error) {
	if strings.TrimSpace(opts.Command) == "" {
		return nil, errors.New("shell agent: command is required")
	}
	logger := opts.Logger
	if logger == nil {
		logger = slog.Default()
	}
	turn := time.Duration(opts.TurnTimeoutMs) * time.Millisecond
	if turn <= 0 {
		turn = 60 * time.Minute
	}
	stall := time.Duration(opts.StallTimeoutMs) * time.Millisecond
	if stall <= 0 {
		stall = 5 * time.Minute
	}
	return &Runner{
		command:      opts.Command,
		turnTimeout:  turn,
		stallTimeout: stall,
		log:          logger,
	}, nil
}

// FromAgentCommand is a convenience constructor for callers that already
// have a config.AgentCommand (codex / claude_code blocks).
func FromAgentCommand(cmd config.AgentCommand, logger *slog.Logger) (*Runner, error) {
	return New(Options{
		Command:        cmd.Command,
		TurnTimeoutMs:  cmd.TurnTimeoutMs,
		StallTimeoutMs: cmd.StallTimeoutMs,
		Logger:         logger,
	})
}

// Run launches the configured command via `bash -lc`, with cwd set to the
// workspace path. The rendered prompt is piped to stdin. The runner enforces
// two distinct timeouts:
//   - turn_timeout_ms: total wall-clock budget. Exceeding it maps to
//     StatusTimedOut.
//   - stall_timeout_ms: the longest gap allowed between bytes on stdout/
//     stderr. Exceeding it kills the process and maps to StatusStalled —
//     used to catch agents that hang silently without exiting.
//
// Exit zero → StatusSucceeded; non-zero exit (with no timeout) →
// StatusFailed.
func (r *Runner) Run(ctx context.Context, req agent.RunRequest) agent.RunResult {
	runCtx, cancel := context.WithTimeout(ctx, r.turnTimeout)
	defer cancel()

	cmd := exec.CommandContext(runCtx, "bash", "-lc", r.command)
	cmd.Dir = req.Workspace.Path
	cmd.Stdin = strings.NewReader(req.Prompt)

	tracker := newActivityTracker()
	cmd.Stdout = tracker
	cmd.Stderr = tracker

	r.log.Info("agent starting",
		"issue", req.Issue.Identifier,
		"attempt", req.Attempt,
		"session_id", req.SessionID,
		"cwd", req.Workspace.Path,
		"stall_timeout_ms", r.stallTimeout.Milliseconds(),
		"turn_timeout_ms", r.turnTimeout.Milliseconds(),
	)
	start := time.Now()
	if err := cmd.Start(); err != nil {
		return agent.RunResult{Status: domain.StatusFailed, Err: fmt.Errorf("start: %w", err), Output: tracker.String()}
	}

	// Stall watchdog: every (stall/4) check whether the process has produced
	// any output recently. When idle for `stall`, kill the process; the wait
	// below sees the killed exit and we return StatusStalled.
	stalledFlag := &atomic.Bool{}
	watchdogDone := make(chan struct{})
	go r.runStallWatchdog(runCtx, cmd, tracker, stalledFlag, watchdogDone)

	err := cmd.Wait()
	close(watchdogDone)
	elapsed := time.Since(start)
	output := tracker.String()

	if stalledFlag.Load() {
		r.log.Warn("agent stalled",
			"issue", req.Issue.Identifier,
			"session_id", req.SessionID,
			"elapsed_ms", elapsed.Milliseconds(),
			"output", truncate(output),
		)
		return agent.RunResult{Status: domain.StatusStalled, Err: fmt.Errorf("no output for %s", r.stallTimeout), Output: output}
	}
	if errors.Is(runCtx.Err(), context.DeadlineExceeded) {
		r.log.Warn("agent turn timed out",
			"issue", req.Issue.Identifier,
			"session_id", req.SessionID,
			"elapsed_ms", elapsed.Milliseconds(),
			"output", truncate(output),
		)
		return agent.RunResult{Status: domain.StatusTimedOut, Err: fmt.Errorf("turn timeout after %s", r.turnTimeout), Output: output}
	}
	if err != nil {
		r.log.Warn("agent failed",
			"issue", req.Issue.Identifier,
			"session_id", req.SessionID,
			"err", err,
			"elapsed_ms", elapsed.Milliseconds(),
			"output", truncate(output),
		)
		return agent.RunResult{Status: domain.StatusFailed, Err: err, Output: output}
	}
	r.log.Info("agent succeeded",
		"issue", req.Issue.Identifier,
		"session_id", req.SessionID,
		"elapsed_ms", elapsed.Milliseconds(),
	)
	return agent.RunResult{Status: domain.StatusSucceeded, Output: output}
}

func (r *Runner) runStallWatchdog(ctx context.Context, cmd *exec.Cmd, tr *activityTracker, stalled *atomic.Bool, done chan struct{}) {
	tick := r.stallTimeout / 4
	if tick <= 0 {
		tick = time.Second
	}
	t := time.NewTicker(tick)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-done:
			return
		case <-t.C:
			if time.Since(tr.LastActivity()) >= r.stallTimeout {
				stalled.Store(true)
				if cmd.Process != nil {
					_ = cmd.Process.Kill()
				}
				return
			}
		}
	}
}

// activityTracker is an io.Writer that records the time of the most recent
// write so the stall watchdog can ask "did anything happen recently?". The
// underlying buffer captures bytes for inclusion in RunResult.Output.
type activityTracker struct {
	mu     sync.Mutex
	buf    []byte
	last   atomic.Int64 // unix nanos
}

func newActivityTracker() *activityTracker {
	a := &activityTracker{}
	a.last.Store(time.Now().UnixNano())
	return a
}

func (a *activityTracker) Write(p []byte) (int, error) {
	a.mu.Lock()
	a.buf = append(a.buf, p...)
	a.mu.Unlock()
	a.last.Store(time.Now().UnixNano())
	return len(p), nil
}

func (a *activityTracker) LastActivity() time.Time {
	return time.Unix(0, a.last.Load())
}

func (a *activityTracker) String() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return string(a.buf)
}

func truncate(s string) string {
	const limit = 2048
	if len(s) <= limit {
		return s
	}
	return s[:limit] + "...[truncated]"
}

// satisfy unused import detection (io is used implicitly via cmd.Stdout/Stderr).
var _ io.Writer = (*activityTracker)(nil)
