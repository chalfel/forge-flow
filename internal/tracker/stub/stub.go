// Package stub is an in-memory tracker for tests and the `symphony run --stub`
// dry-run mode. Issues are pushed by tests; the scheduler treats it like any
// other tracker.
package stub

import (
	"context"
	"fmt"
	"sync"

	"github.com/chalfel/forge-flow/internal/domain"
)

type Tracker struct {
	mu      sync.Mutex
	issues  map[string]domain.Issue
	created []domain.IssueDraft
	nextN   int
}

func New() *Tracker {
	return &Tracker{issues: make(map[string]domain.Issue)}
}

// CreateIssue records the draft and synthesises an Issue with a fresh ID so
// the captain integration tests can assert on what was written without
// touching a real tracker.
func (t *Tracker) CreateIssue(_ context.Context, draft domain.IssueDraft) (*domain.Issue, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.created = append(t.created, draft)
	t.nextN++
	id := fmt.Sprintf("stub-%d", t.nextN)
	is := domain.Issue{
		ID:          id,
		Identifier:  fmt.Sprintf("STUB-%d", t.nextN),
		Title:       draft.Title,
		Description: draft.Description,
		Priority:    draft.Priority,
		Labels:      draft.Labels,
	}
	t.issues[id] = is
	return &is, nil
}

// Created returns the drafts that have been written via CreateIssue.
func (t *Tracker) Created() []domain.IssueDraft {
	t.mu.Lock()
	defer t.mu.Unlock()
	out := make([]domain.IssueDraft, len(t.created))
	copy(out, t.created)
	return out
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
