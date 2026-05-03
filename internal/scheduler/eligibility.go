package scheduler

import (
	"strings"

	"github.com/chalfel/forge-flow/internal/domain"
)

// Eligibility returns whether an issue can be dispatched right now and, when
// not, a short reason for observability.
type Eligibility struct {
	OK     bool
	Reason string
}

func eligible(issue domain.Issue, store *Store, terminalStates []string) Eligibility {
	if strings.TrimSpace(issue.ID) == "" || strings.TrimSpace(issue.Identifier) == "" {
		return Eligibility{Reason: "missing id or identifier"}
	}
	if strings.TrimSpace(issue.Title) == "" {
		return Eligibility{Reason: "missing title"}
	}
	state, _ := store.State(issue.ID)
	switch state {
	case Claimed, Running:
		return Eligibility{Reason: "already in flight"}
	case RetryQueued:
		return Eligibility{Reason: "retry pending"}
	}
	for _, b := range issue.BlockedBy {
		if !isTerminal(b, terminalStates) {
			return Eligibility{Reason: "blocked by non-terminal issue"}
		}
	}
	return Eligibility{OK: true}
}

func isTerminal(state string, terminal []string) bool {
	for _, t := range terminal {
		if strings.EqualFold(t, state) {
			return true
		}
	}
	return false
}
