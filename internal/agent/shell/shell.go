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
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os/exec"
	"strings"
	"time"

	"github.com/chalfel/forge-flow/internal/agent"
	"github.com/chalfel/forge-flow/internal/config"
	"github.com/chalfel/forge-flow/internal/domain"
)

type Runner struct {
	command       string
	turnTimeout   time.Duration
	log           *slog.Logger
}

type Options struct {
	Command       string
	TurnTimeoutMs int
	Logger        *slog.Logger
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
	return &Runner{command: opts.Command, turnTimeout: turn, log: logger}, nil
}

// FromAgentCommand is a convenience constructor for callers that already
// have a config.AgentCommand (codex / claude_code blocks).
func FromAgentCommand(cmd config.AgentCommand, logger *slog.Logger) (*Runner, error) {
	return New(Options{
		Command:       cmd.Command,
		TurnTimeoutMs: cmd.TurnTimeoutMs,
		Logger:        logger,
	})
}

// Run launches the configured command via `bash -lc`, with cwd set to the
// workspace path. The rendered prompt is piped to stdin. A non-zero exit
// surfaces as StatusFailed; a context-deadline kill surfaces as
// StatusTimedOut. Combined stdout+stderr are logged.
func (r *Runner) Run(ctx context.Context, req agent.RunRequest) agent.RunResult {
	runCtx, cancel := context.WithTimeout(ctx, r.turnTimeout)
	defer cancel()

	cmd := exec.CommandContext(runCtx, "bash", "-lc", r.command)
	cmd.Dir = req.Workspace.Path
	cmd.Stdin = strings.NewReader(req.Prompt)
	var combined bytes.Buffer
	cmd.Stdout = &combined
	cmd.Stderr = &combined

	r.log.Info("agent starting",
		"issue", req.Issue.Identifier,
		"attempt", req.Attempt,
		"cwd", req.Workspace.Path,
	)
	start := time.Now()
	err := cmd.Run()
	elapsed := time.Since(start)

	output := combined.String()
	if errors.Is(runCtx.Err(), context.DeadlineExceeded) {
		r.log.Warn("agent turn timed out",
			"issue", req.Issue.Identifier,
			"elapsed_ms", elapsed.Milliseconds(),
			"output", truncate(combined.Bytes()),
		)
		return agent.RunResult{Status: domain.StatusTimedOut, Err: fmt.Errorf("turn timeout after %s", r.turnTimeout), Output: output}
	}

	if err != nil {
		r.log.Warn("agent failed",
			"issue", req.Issue.Identifier,
			"err", err,
			"elapsed_ms", elapsed.Milliseconds(),
			"output", truncate(combined.Bytes()),
		)
		return agent.RunResult{Status: domain.StatusFailed, Err: err, Output: output}
	}

	r.log.Info("agent succeeded",
		"issue", req.Issue.Identifier,
		"elapsed_ms", elapsed.Milliseconds(),
	)
	return agent.RunResult{Status: domain.StatusSucceeded, Output: output}
}

func truncate(b []byte) string {
	const limit = 2048
	if len(b) <= limit {
		return string(b)
	}
	return string(b[:limit]) + "...[truncated]"
}
