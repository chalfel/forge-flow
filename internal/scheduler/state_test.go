package scheduler

import (
	"testing"
	"time"
)

func TestTryClaim_NewIssue(t *testing.T) {
	s := NewStore()
	if !s.TryClaim("a") {
		t.Fatal("expected first claim to succeed")
	}
	if s.TryClaim("a") {
		t.Fatal("expected second claim on Claimed entry to fail")
	}
}

func TestTryClaim_AfterRelease(t *testing.T) {
	s := NewStore()
	s.TryClaim("a")
	s.Release("a")
	if !s.TryClaim("a") {
		t.Fatal("expected re-claim after Release to succeed")
	}
}

func TestPromoteDueRetries(t *testing.T) {
	s := NewStore()
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	s.ScheduleRetry("a", 1, now.Add(-time.Second), "boom")
	s.ScheduleRetry("b", 1, now.Add(time.Minute), "boom")
	promoted := s.PromoteDueRetries(now)
	if len(promoted) != 1 || promoted[0] != "a" {
		t.Fatalf("expected [a] promoted, got %v", promoted)
	}
	st, _ := s.State("a")
	if st != Unclaimed {
		t.Fatalf("a should be Unclaimed (ready for re-dispatch), got %s", st)
	}
	st, _ = s.State("b")
	if st != RetryQueued {
		t.Fatalf("b should remain RetryQueued, got %s", st)
	}
}

func TestRunningCount(t *testing.T) {
	s := NewStore()
	s.TryClaim("a")
	s.TryClaim("b")
	s.MarkRunning("a", time.Now(), func() {}, "ss-a")
	if got := s.RunningCount(); got != 1 {
		t.Fatalf("expected 1 running, got %d", got)
	}
	s.MarkRunning("b", time.Now(), func() {}, "ss-b")
	if got := s.RunningCount(); got != 2 {
		t.Fatalf("expected 2 running, got %d", got)
	}
	s.Release("a")
	if got := s.RunningCount(); got != 1 {
		t.Fatalf("expected 1 running after release, got %d", got)
	}
}

func TestReleaseClearsAttempt(t *testing.T) {
	s := NewStore()
	s.ScheduleRetry("a", 5, time.Now(), "boom")
	if s.Attempt("a") != 5 {
		t.Fatalf("setup: expected attempt 5")
	}
	s.Release("a")
	if s.Attempt("a") != 0 {
		t.Fatalf("expected Release to clear attempt, got %d", s.Attempt("a"))
	}
}
