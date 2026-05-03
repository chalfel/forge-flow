package scheduler

import (
	"strings"
	"testing"

	"github.com/chalfel/forge-flow/internal/domain"
)

func TestRenderPrompt_BasicSubstitution(t *testing.T) {
	body := "Issue {{ issue.identifier }}: {{ issue.title }} (attempt {{ attempt }})"
	out := renderPrompt(body, domain.Issue{
		ID:         "id1",
		Identifier: "ABC-7",
		Title:      "Add dark mode",
	}, 2)
	want := "Issue ABC-7: Add dark mode (attempt 2)"
	if out != want {
		t.Fatalf("want %q, got %q", want, out)
	}
}

func TestRenderPrompt_UnknownPlaceholderLeftIntact(t *testing.T) {
	body := "Hello {{ issue.title }} and {{ unknown.field }}"
	out := renderPrompt(body, domain.Issue{Title: "x"}, 0)
	if !strings.Contains(out, "{{ unknown.field }}") {
		t.Fatalf("expected unknown placeholder to remain, got %q", out)
	}
}

func TestRenderPrompt_LabelsJoined(t *testing.T) {
	body := "{{ issue.labels }}"
	out := renderPrompt(body, domain.Issue{Labels: []string{"bug", "urgent"}}, 0)
	if out != "bug,urgent" {
		t.Fatalf("got %q", out)
	}
}
