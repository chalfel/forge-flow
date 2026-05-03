// Package agent abstracts coding-agent runners. The scheduler launches one
// agent per dispatched attempt and waits for the run to terminate. Adapters
// (codex, claudecode, stub) own the wire protocol with their respective CLI.
package agent

import (
	"context"

	"github.com/chalfel/forge-flow/internal/domain"
)

type RunRequest struct {
	Issue     domain.Issue
	Attempt   int
	Workspace domain.Workspace
	Prompt    string
}

type RunResult struct {
	Status       domain.AttemptStatus
	Err          error
	InputTokens  int
	OutputTokens int
	// Output is the captured stdout+stderr from the agent. The scheduler
	// does not inspect this; the captain reads it to parse a JSON ticket
	// plan from the agent's reply.
	Output string
}

type Agent interface {
	// Run executes a single attempt. Implementations must respect ctx for
	// cancellation and surface terminal status (Succeeded/Failed/TimedOut/
	// Stalled) on RunResult. Transport errors go on Err and trigger retry.
	Run(ctx context.Context, req RunRequest) RunResult
}
