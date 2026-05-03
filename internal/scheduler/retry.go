package scheduler

import "time"

// Backoff computes the next retry delay. Per SPEC.md: a clean attempt
// schedules a 1-second continuation; failures use exponential backoff capped
// at MaxBackoff (workflow.agent.max_retry_backoff_ms).
type Backoff struct {
	BaseFailure time.Duration
	MaxBackoff  time.Duration
	Continuation time.Duration
}

func DefaultBackoff(maxBackoffMs int) Backoff {
	return Backoff{
		BaseFailure:  2 * time.Second,
		MaxBackoff:   time.Duration(maxBackoffMs) * time.Millisecond,
		Continuation: 1 * time.Second,
	}
}

// NextDelay returns the delay before the next attempt. `attempt` is the
// just-finished attempt number (0 for first dispatch). `failed` flips the
// curve from continuation to exponential backoff.
func (b Backoff) NextDelay(attempt int, failed bool) time.Duration {
	if !failed {
		return b.Continuation
	}
	d := b.BaseFailure
	for i := 0; i < attempt; i++ {
		d *= 2
		if d >= b.MaxBackoff {
			return b.MaxBackoff
		}
	}
	if d > b.MaxBackoff {
		return b.MaxBackoff
	}
	return d
}
