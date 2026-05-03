// Package stub is a deterministic agent for tests and dry-run mode. It
// returns whatever RunResult was queued via Push, in FIFO order, defaulting
// to Succeeded.
package stub

import (
	"context"
	"sync"

	"github.com/chalfel/forge-flow/internal/agent"
	"github.com/chalfel/forge-flow/internal/domain"
)

type Agent struct {
	mu       sync.Mutex
	queued   []agent.RunResult
	executed []agent.RunRequest
	delay    func(req agent.RunRequest)
}

func New() *Agent { return &Agent{} }

// Queue pushes a canned response. When the queue is empty, Run returns a
// successful result so basic dry-run flows complete without setup.
func (a *Agent) Queue(r agent.RunResult) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.queued = append(a.queued, r)
}

// SetDelay installs a callback invoked synchronously inside Run, useful for
// tests that need to observe the Running state before completion fires.
func (a *Agent) SetDelay(f func(req agent.RunRequest)) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.delay = f
}

func (a *Agent) Executed() []agent.RunRequest {
	a.mu.Lock()
	defer a.mu.Unlock()
	out := make([]agent.RunRequest, len(a.executed))
	copy(out, a.executed)
	return out
}

func (a *Agent) Run(ctx context.Context, req agent.RunRequest) agent.RunResult {
	a.mu.Lock()
	a.executed = append(a.executed, req)
	var res agent.RunResult
	if len(a.queued) > 0 {
		res = a.queued[0]
		a.queued = a.queued[1:]
	} else {
		res = agent.RunResult{Status: domain.StatusSucceeded}
	}
	delay := a.delay
	a.mu.Unlock()
	if delay != nil {
		done := make(chan struct{})
		go func() {
			delay(req)
			close(done)
		}()
		select {
		case <-done:
		case <-ctx.Done():
			// Stop blocking on the user-supplied delay so the scheduler
			// can observe the cancellation. The goroutine running delay
			// remains; tests that rely on this path must arrange to
			// release it (e.g. via a shared channel) for clean shutdown.
			return agent.RunResult{Status: domain.StatusFailed, Err: ctx.Err()}
		}
	}
	return res
}
