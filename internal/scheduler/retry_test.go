package scheduler

import (
	"testing"
	"time"
)

func TestBackoff_Continuation(t *testing.T) {
	b := DefaultBackoff(300_000)
	if got := b.NextDelay(0, false); got != time.Second {
		t.Fatalf("continuation should be 1s, got %v", got)
	}
}

func TestBackoff_ExponentialFailure(t *testing.T) {
	b := DefaultBackoff(60_000)
	cases := []struct {
		attempt int
		want    time.Duration
	}{
		{0, 2 * time.Second},
		{1, 4 * time.Second},
		{2, 8 * time.Second},
		{3, 16 * time.Second},
	}
	for _, c := range cases {
		if got := b.NextDelay(c.attempt, true); got != c.want {
			t.Errorf("attempt=%d: want %v, got %v", c.attempt, c.want, got)
		}
	}
}

func TestBackoff_CappedAtMax(t *testing.T) {
	b := DefaultBackoff(10_000) // 10s cap
	got := b.NextDelay(20, true)
	if got != 10*time.Second {
		t.Fatalf("expected cap at 10s, got %v", got)
	}
}
