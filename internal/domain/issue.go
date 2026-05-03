package domain

import "time"

// Issue is the normalized tracker record. Adapters (Linear, GitHub) map their
// native types into this shape so the scheduler stays tracker-agnostic.
type Issue struct {
	ID          string
	Identifier  string
	Title       string
	Description string
	Priority    int
	State       string
	BranchName  string
	URL         string
	Labels      []string
	BlockedBy   []string
	CreatedAt   time.Time
	UpdatedAt   time.Time
}
