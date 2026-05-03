package scheduler

import (
	"sync"
	"time"
)

// ClaimState models the lifecycle of an issue inside the scheduler. The
// transitions are: Unclaimed → Claimed → Running → (RetryQueued | Released).
// RetryQueued returns to Claimed when its timer fires.
type ClaimState string

const (
	Unclaimed   ClaimState = "unclaimed"
	Claimed     ClaimState = "claimed"
	Running     ClaimState = "running"
	RetryQueued ClaimState = "retry_queued"
	Released    ClaimState = "released"
)

type entry struct {
	state     ClaimState
	attempt   int
	dueAt     time.Time
	lastErr   string
	startedAt time.Time
	// cancel terminates the dispatched goroutine for a Running issue. It is
	// installed by MarkRunning and invoked by CancelRunning during
	// reconciliation when the tracker reports the issue moved out of an
	// active state.
	cancel    func()
	sessionID string
}

// Store is the in-memory map of issue ID to claim state. Concurrent access is
// guarded by a single mutex; the scheduler does not hold the lock during
// agent execution, so contention stays low.
type Store struct {
	mu      sync.Mutex
	entries map[string]*entry
	skip    map[string]struct{} // permanent tombstone for issues consumed by captain
}

func NewStore() *Store {
	return &Store{
		entries: make(map[string]*entry),
		skip:    make(map[string]struct{}),
	}
}

// Skip marks an issue ID as permanently ineligible for dispatch. Used by the
// captain workflow: once a parent ticket is decomposed into children, we
// don't want the scheduler to re-dispatch the parent on the next tick (the
// label that triggered decomposition would otherwise re-claim it). The skip
// set is in-memory only — operators must remove the watch label or
// transition the parent state to make the change durable across restarts.
func (s *Store) Skip(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.skip[id] = struct{}{}
}

func (s *Store) IsSkipped(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.skip[id]
	return ok
}

// TryClaim atomically transitions an Unclaimed (or absent) issue to Claimed.
// Returns false when the issue is already in flight or scheduled for retry.
func (s *Store) TryClaim(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.entries[id]
	if !ok {
		s.entries[id] = &entry{state: Claimed}
		return true
	}
	if e.state == Unclaimed || e.state == Released {
		e.state = Claimed
		return true
	}
	return false
}

func (s *Store) MarkRunning(id string, now time.Time, cancel func(), sessionID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if e, ok := s.entries[id]; ok {
		e.state = Running
		e.startedAt = now
		e.cancel = cancel
		e.sessionID = sessionID
	}
}

// CancelRunning calls the stored cancel function (if any) so the dispatched
// goroutine unwinds. The state transition itself happens when the goroutine
// posts its completion (typically with StatusCanceledByReconcile).
func (s *Store) CancelRunning(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.entries[id]
	if !ok || e.state != Running || e.cancel == nil {
		return false
	}
	e.cancel()
	return true
}

// RunningIDs returns the IDs of issues currently Running. Used by the
// reconciliation pass to refresh tracker state.
func (s *Store) RunningIDs() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []string
	for id, e := range s.entries {
		if e.state == Running {
			out = append(out, id)
		}
	}
	return out
}

// SessionID returns the session id stored when MarkRunning was called.
func (s *Store) SessionID(id string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if e, ok := s.entries[id]; ok {
		return e.sessionID
	}
	return ""
}

// ScheduleRetry moves the issue to RetryQueued with a due time. The retry
// will be eligible for re-dispatch after `dueAt`.
func (s *Store) ScheduleRetry(id string, attempt int, dueAt time.Time, errMsg string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.entries[id]
	if !ok {
		e = &entry{}
		s.entries[id] = e
	}
	e.state = RetryQueued
	e.attempt = attempt
	e.dueAt = dueAt
	e.lastErr = errMsg
}

// Release transitions to Released and clears retry/error state. Called when
// a run reaches a terminal state with no continuation, or when reconciliation
// detects the tracker moved the issue to a terminal state.
func (s *Store) Release(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if e, ok := s.entries[id]; ok {
		e.state = Released
		e.dueAt = time.Time{}
		e.lastErr = ""
		e.attempt = 0
	}
}

func (s *Store) State(id string) (ClaimState, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.entries[id]
	if !ok {
		return Unclaimed, false
	}
	return e.state, true
}

// Attempt returns the next attempt number for an issue. First dispatch is 0;
// retries use 1, 2, 3...
func (s *Store) Attempt(id string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	if e, ok := s.entries[id]; ok {
		return e.attempt
	}
	return 0
}

// PromoteDueRetries moves any RetryQueued entries whose dueAt is in the past
// back to Unclaimed so the next dispatch picks them up via the standard
// eligible/TryClaim path. Attempt count is preserved on the entry.
func (s *Store) PromoteDueRetries(now time.Time) []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	var ids []string
	for id, e := range s.entries {
		if e.state == RetryQueued && !now.Before(e.dueAt) {
			e.state = Unclaimed
			ids = append(ids, id)
		}
	}
	return ids
}

// RunningCount counts issues currently in Running state. Used to enforce
// max_concurrent_agents.
func (s *Store) RunningCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	for _, e := range s.entries {
		if e.state == Running {
			n++
		}
	}
	return n
}

// Snapshot returns a copy of all entries for observability. Cheap because the
// dataset is bounded by tracker page size.
type EntrySnapshot struct {
	IssueID   string
	State     ClaimState
	Attempt   int
	DueAt     time.Time
	LastErr   string
	StartedAt time.Time
	SessionID string
}

func (s *Store) Snapshot() []EntrySnapshot {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]EntrySnapshot, 0, len(s.entries))
	for id, e := range s.entries {
		out = append(out, EntrySnapshot{
			IssueID:   id,
			State:     e.state,
			Attempt:   e.attempt,
			DueAt:     e.dueAt,
			LastErr:   e.lastErr,
			StartedAt: e.startedAt,
			SessionID: e.sessionID,
		})
	}
	return out
}
