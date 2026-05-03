package scheduler

import (
	"testing"
	"time"

	"github.com/chalfel/forge-flow/internal/domain"
)

func TestEligible_OK(t *testing.T) {
	store := NewStore()
	issue := domain.Issue{ID: "1", Identifier: "ABC-1", Title: "x"}
	if got := eligible(issue, store, []string{"Done"}); !got.OK {
		t.Fatalf("expected eligible, got %+v", got)
	}
}

func TestEligible_MissingFields(t *testing.T) {
	store := NewStore()
	cases := []domain.Issue{
		{Identifier: "ABC-1", Title: "x"},     // no ID
		{ID: "1", Title: "x"},                  // no identifier
		{ID: "1", Identifier: "ABC-1"},         // no title
	}
	for _, c := range cases {
		if got := eligible(c, store, nil); got.OK {
			t.Errorf("expected NOT eligible: %+v", c)
		}
	}
}

func TestEligible_AlreadyInFlight(t *testing.T) {
	store := NewStore()
	store.TryClaim("1")
	store.MarkRunning("1", time.Now(), func() {}, "")
	issue := domain.Issue{ID: "1", Identifier: "ABC-1", Title: "x"}
	if got := eligible(issue, store, nil); got.OK {
		t.Fatalf("expected NOT eligible while running")
	}
}

func TestEligible_BlockedByNonTerminal(t *testing.T) {
	store := NewStore()
	issue := domain.Issue{
		ID:         "1",
		Identifier: "ABC-1",
		Title:      "x",
		BlockedBy:  []string{"Todo"},
	}
	got := eligible(issue, store, []string{"Done", "Cancelled"})
	if got.OK {
		t.Fatalf("expected NOT eligible when blocked by non-terminal")
	}
}

func TestEligible_BlockedByTerminalAllowed(t *testing.T) {
	store := NewStore()
	issue := domain.Issue{
		ID:         "1",
		Identifier: "ABC-1",
		Title:      "x",
		BlockedBy:  []string{"Done"},
	}
	got := eligible(issue, store, []string{"Done"})
	if !got.OK {
		t.Fatalf("expected eligible when all blockers terminal, got %+v", got)
	}
}
