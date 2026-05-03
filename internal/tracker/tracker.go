// Package tracker abstracts external issue trackers. Adapters (linear,
// github, stub) implement the Tracker interface so the scheduler stays
// tracker-agnostic; switching trackers is a workflow.kind config change.
package tracker

import (
	"context"

	"github.com/chalfel/forge-flow/internal/domain"
)

// Tracker is the read surface the scheduler needs from an external system.
// Write-back operations (status transitions, comments, attachments) are
// modeled as optional capabilities on richer interfaces because not every
// tracker supports them in the same way.
type Tracker interface {
	// FetchCandidates returns issues currently in any of the active states.
	// Order is not guaranteed; the scheduler sorts.
	FetchCandidates(ctx context.Context, activeStates []string) ([]domain.Issue, error)
	// GetIssue refreshes a single issue. Used during reconciliation to
	// detect tracker-side state changes (terminal moves, manual retry).
	GetIssue(ctx context.Context, id string) (*domain.Issue, error)
}
