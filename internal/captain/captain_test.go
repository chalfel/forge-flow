package captain

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"

	"github.com/chalfel/forge-flow/internal/agent"
	"github.com/chalfel/forge-flow/internal/config"
	"github.com/chalfel/forge-flow/internal/domain"
	trackerstub "github.com/chalfel/forge-flow/internal/tracker/stub"
)

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// fixedAgent returns the same RunResult on every call; lets tests inject the
// expected captain output deterministically.
type fixedAgent struct{ res agent.RunResult }

func (f *fixedAgent) Run(_ context.Context, _ agent.RunRequest) agent.RunResult { return f.res }

func TestParseTickets_HappyPath(t *testing.T) {
	out := "thinking...\n```json\n" +
		`{"tickets":[{"title":"A","description":"do A","priority":2,"labels":["Todo"]},
		             {"title":"B","description":"do B","priority":3,"labels":["Todo","frontend"]}]}` +
		"\n```\n"
	got, err := parseTickets(out)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 drafts, got %d", len(got))
	}
	if got[0].Title != "A" || got[0].Priority != 2 {
		t.Errorf("first draft wrong: %+v", got[0])
	}
	if len(got[1].Labels) != 2 {
		t.Errorf("labels wrong: %v", got[1].Labels)
	}
}

func TestParseTickets_PicksLastFencedBlock(t *testing.T) {
	out := "Schema example:\n```json\n{\"tickets\": []}\n```\n\n" +
		"Now the actual plan:\n```json\n" +
		`{"tickets":[{"title":"Real","description":"x"}]}` +
		"\n```\n"
	got, err := parseTickets(out)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Title != "Real" {
		t.Fatalf("expected to pick last block, got %+v", got)
	}
}

func TestParseTickets_NoBlock(t *testing.T) {
	if _, err := parseTickets("no code block here"); err == nil {
		t.Fatal("expected error")
	}
}

func TestParseTickets_EmptyArray(t *testing.T) {
	out := "```json\n{\"tickets\": []}\n```"
	if _, err := parseTickets(out); err == nil || !strings.Contains(err.Error(), "empty") {
		t.Fatalf("expected empty error, got %v", err)
	}
}

func TestParseTickets_MissingTitle(t *testing.T) {
	out := "```json\n{\"tickets\": [{\"description\":\"x\"}]}\n```"
	if _, err := parseTickets(out); err == nil || !strings.Contains(err.Error(), "title") {
		t.Fatalf("expected title error, got %v", err)
	}
}

func TestPlan_AndCreate_EndToEnd(t *testing.T) {
	wf := &config.Workflow{
		Tracker: config.Tracker{Kind: config.TrackerLinear, ProjectSlug: "X", ActiveStates: []string{"Todo"}},
		Agent:   config.Agent{Kind: "claude_code"},
	}
	ag := &fixedAgent{res: agent.RunResult{
		Status: domain.StatusSucceeded,
		Output: "plan ok\n```json\n" +
			`{"tickets":[{"title":"Add dark mode","description":"why+what+ac","priority":1,"labels":["Todo"]}]}` +
			"\n```",
	}}
	tr := trackerstub.New()
	cap, err := New(Options{Config: wf, Agent: ag, Writer: tr, Logger: discardLogger()})
	if err != nil {
		t.Fatal(err)
	}
	drafts, _, err := cap.Plan(context.Background(), "We want dark mode", "")
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	if len(drafts) != 1 || drafts[0].Title != "Add dark mode" {
		t.Fatalf("drafts wrong: %+v", drafts)
	}
	res, err := cap.Create(context.Background(), drafts)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if len(res.Created) != 1 {
		t.Fatalf("expected 1 created, got %d", len(res.Created))
	}
	written := tr.Created()
	if len(written) != 1 || written[0].Title != "Add dark mode" || len(written[0].Labels) != 1 {
		t.Fatalf("tracker did not record draft correctly: %+v", written)
	}
}

func TestPlan_AgentError(t *testing.T) {
	ag := &fixedAgent{res: agent.RunResult{Status: domain.StatusFailed, Err: errors.New("boom"), Output: "..."}}
	cap, err := New(Options{
		Config: &config.Workflow{},
		Agent:  ag,
		Writer: trackerstub.New(),
		Logger: discardLogger(),
	})
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = cap.Plan(context.Background(), "x", "")
	if err == nil || !strings.Contains(err.Error(), "captain agent") {
		t.Fatalf("expected agent error, got %v", err)
	}
}

func TestNew_RejectsMissingDeps(t *testing.T) {
	if _, err := New(Options{}); err == nil {
		t.Fatal("expected error for empty options")
	}
	if _, err := New(Options{Config: &config.Workflow{}}); err == nil {
		t.Fatal("expected error for missing agent")
	}
	if _, err := New(Options{Config: &config.Workflow{}, Agent: &fixedAgent{}}); err == nil {
		t.Fatal("expected error for missing writer")
	}
}

func TestBuildPrompt_SubstitutesActiveStates(t *testing.T) {
	wf := &config.Workflow{
		Tracker: config.Tracker{ActiveStates: []string{"Todo", "In Progress"}},
	}
	c, _ := New(Options{Config: wf, Agent: &fixedAgent{}, Writer: trackerstub.New(), Logger: discardLogger()})
	prompt := c.buildPrompt("dark mode")
	if !strings.Contains(prompt, "Todo, In Progress") {
		t.Errorf("active states not injected: %q", prompt)
	}
	if !strings.Contains(prompt, "dark mode") {
		t.Errorf("demand not injected: %q", prompt)
	}
}
