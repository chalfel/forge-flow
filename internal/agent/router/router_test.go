package router

import (
	"context"
	"testing"

	"github.com/chalfel/forge-flow/internal/agent"
	"github.com/chalfel/forge-flow/internal/domain"
)

type tagAgent struct{ tag string }

func (a *tagAgent) Run(_ context.Context, _ agent.RunRequest) agent.RunResult {
	return agent.RunResult{Status: domain.StatusSucceeded, Output: a.tag}
}

func TestRouter_FallbackWhenNoLabelMatches(t *testing.T) {
	fb := &tagAgent{tag: "fallback"}
	cap := &tagAgent{tag: "captain"}
	r := New(fb, Rule{Label: "needs-planning", Agent: cap})
	res := r.Run(context.Background(), agent.RunRequest{
		Issue: domain.Issue{Labels: []string{"bug", "frontend"}},
	})
	if res.Output != "fallback" {
		t.Fatalf("want fallback, got %q", res.Output)
	}
}

func TestRouter_RoutesByLabel(t *testing.T) {
	fb := &tagAgent{tag: "fallback"}
	cap := &tagAgent{tag: "captain"}
	r := New(fb, Rule{Label: "needs-planning", Agent: cap})
	res := r.Run(context.Background(), agent.RunRequest{
		Issue: domain.Issue{Labels: []string{"bug", "needs-planning"}},
	})
	if res.Output != "captain" {
		t.Fatalf("want captain, got %q", res.Output)
	}
}

func TestRouter_LabelMatchIsCaseInsensitive(t *testing.T) {
	fb := &tagAgent{tag: "fallback"}
	cap := &tagAgent{tag: "captain"}
	r := New(fb, Rule{Label: "Needs-Planning", Agent: cap})
	res := r.Run(context.Background(), agent.RunRequest{
		Issue: domain.Issue{Labels: []string{"NEEDS-PLANNING"}},
	})
	if res.Output != "captain" {
		t.Fatalf("expected case-insensitive match, got %q", res.Output)
	}
}

func TestRouter_FirstRuleWins(t *testing.T) {
	fb := &tagAgent{tag: "fallback"}
	a := &tagAgent{tag: "first"}
	b := &tagAgent{tag: "second"}
	r := New(fb, Rule{Label: "L", Agent: a}, Rule{Label: "L", Agent: b})
	res := r.Run(context.Background(), agent.RunRequest{
		Issue: domain.Issue{Labels: []string{"L"}},
	})
	if res.Output != "first" {
		t.Fatalf("expected first rule to win, got %q", res.Output)
	}
}
