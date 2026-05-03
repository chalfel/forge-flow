package captain

import (
	"context"
	"strings"
	"testing"

	"github.com/chalfel/forge-flow/internal/agent"
	"github.com/chalfel/forge-flow/internal/config"
	"github.com/chalfel/forge-flow/internal/domain"
	trackerstub "github.com/chalfel/forge-flow/internal/tracker/stub"
)

func TestAgent_DecomposesIssueAndReturnsStatusDecomposed(t *testing.T) {
	wf := &config.Workflow{
		Tracker: config.Tracker{Kind: config.TrackerLinear, ProjectSlug: "X", ActiveStates: []string{"Todo"}},
	}
	planAgent := &fixedAgent{res: agent.RunResult{
		Status: domain.StatusSucceeded,
		Output: "thinking...\n```json\n" +
			`{"tickets":[{"title":"Step 1","description":"do step 1","labels":["Todo"]}, {"title":"Step 2","description":"do step 2","labels":["Todo"]}]}` +
			"\n```",
	}}
	tr := trackerstub.New()
	cap, err := New(Options{Config: wf, Agent: planAgent, Writer: tr, Logger: discardLogger()})
	if err != nil {
		t.Fatal(err)
	}
	a := NewAgent(cap)
	res := a.Run(context.Background(), agent.RunRequest{
		Issue: domain.Issue{
			ID:          "parent",
			Identifier:  "X-99",
			Title:       "Multi-step migration",
			Description: "Break this down",
			Labels:      []string{"needs-planning"},
		},
	})
	if res.Status != domain.StatusDecomposed {
		t.Fatalf("expected StatusDecomposed, got %s (err=%v)", res.Status, res.Err)
	}
	if !strings.Contains(res.Output, "captain decomposed parent into 2 ticket(s)") {
		t.Errorf("summary missing in output: %q", res.Output)
	}
	if got := len(tr.Created()); got != 2 {
		t.Errorf("expected 2 tickets written to tracker, got %d", got)
	}
}

func TestAgent_PlanFailureSurfacesAsFailedStatus(t *testing.T) {
	wf := &config.Workflow{}
	planAgent := &fixedAgent{res: agent.RunResult{
		Status: domain.StatusSucceeded,
		Output: "no JSON at all",
	}}
	cap, _ := New(Options{Config: wf, Agent: planAgent, Writer: trackerstub.New(), Logger: discardLogger()})
	a := NewAgent(cap)
	res := a.Run(context.Background(), agent.RunRequest{
		Issue: domain.Issue{Identifier: "X-1", Title: "x"},
	})
	if res.Status != domain.StatusFailed {
		t.Fatalf("expected Failed, got %s", res.Status)
	}
}

func TestBuildDemand_IncludesTitleDescriptionAndLabels(t *testing.T) {
	got := buildDemand(domain.Issue{
		Title:       "Add dark mode",
		Description: "Users want a toggle.",
		Labels:      []string{"needs-planning", "ux"},
	})
	for _, want := range []string{"Add dark mode", "Users want a toggle.", "needs-planning, ux"} {
		if !strings.Contains(got, want) {
			t.Errorf("demand missing %q in:\n%s", want, got)
		}
	}
}
