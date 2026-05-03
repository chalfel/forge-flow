// Package stub is an in-memory tracker for tests and the `symphony run --stub`
// dry-run mode. Issues are pushed by tests; the scheduler treats it like any
// other tracker.
package stub

import (
	"context"
	"sync"

	"github.com/chalfel/forge-flow/internal/domain"
)

type Tracker struct {
	mu     sync.Mutex
	issues map[string]domain.Issue
}

func New() *Tracker {
	return &Tracker{issues: make(map[string]domain.Issue)}
}

func (t *Tracker) Set(issue domain.Issue) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.issues[issue.ID] = issue
}

func (t *Tracker) Remove(id string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	delete(t.issues, id)
}

func (t *Tracker) FetchCandidates(_ context.Context, activeStates []string) ([]domain.Issue, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	out := make([]domain.Issue, 0, len(t.issues))
	for _, i := range t.issues {
		if matchAny(i.State, activeStates) {
			out = append(out, i)
		}
	}
	return out, nil
}

func (t *Tracker) GetIssue(_ context.Context, id string) (*domain.Issue, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if i, ok := t.issues[id]; ok {
		return &i, nil
	}
	return nil, nil
}

func matchAny(s string, set []string) bool {
	for _, x := range set {
		if x == s {
			return true
		}
	}
	return false
}
