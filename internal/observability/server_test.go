package observability

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/chalfel/forge-flow/internal/agent"
	agentstub "github.com/chalfel/forge-flow/internal/agent/stub"
	"github.com/chalfel/forge-flow/internal/config"
	"github.com/chalfel/forge-flow/internal/domain"
	"github.com/chalfel/forge-flow/internal/scheduler"
	trackerstub "github.com/chalfel/forge-flow/internal/tracker/stub"
	"github.com/chalfel/forge-flow/internal/workspace"
)

func newSchedFixture(t *testing.T) (*scheduler.Scheduler, *trackerstub.Tracker, *agentstub.Agent) {
	t.Helper()
	wf := &config.Workflow{
		Tracker:    config.Tracker{Kind: config.TrackerLinear, ProjectSlug: "SYM", ActiveStates: []string{"Todo"}},
		Agent:      config.Agent{Kind: "claude_code", MaxConcurrentAgents: 3, MaxRetryBackoffMs: 60_000},
		ClaudeCode: config.AgentCommand{Command: "claude --print"},
		Polling:    config.Polling{IntervalMs: 30000},
		PromptBody: "{{ issue.identifier }}",
	}
	tr := trackerstub.New()
	ag := agentstub.New()
	s := scheduler.New(scheduler.Options{
		Config:    wf,
		Tracker:   tr,
		Agent:     ag,
		Workspace: workspace.NewStub(),
		Logger:    slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	return s, tr, ag
}

func TestStateEndpoint_ReturnsSnapshot(t *testing.T) {
	s, tr, ag := newSchedFixture(t)
	tr.Set(domain.Issue{ID: "demo", Identifier: "SYM-1", Title: "x", State: "Todo"})
	// Block the agent so the issue stays in Running state for the snapshot.
	block := make(chan struct{})
	ag.SetDelay(func(_ agent.RunRequest) { <-block })
	_ = s.Tick(context.Background())

	srv := NewServer(s)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/state", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status: %d", rec.Code)
	}
	var snap Snapshot
	if err := json.Unmarshal(rec.Body.Bytes(), &snap); err != nil {
		t.Fatal(err)
	}
	if snap.Tracker.Kind != "linear" || snap.Tracker.ProjectSlug != "SYM" {
		t.Errorf("tracker summary wrong: %+v", snap.Tracker)
	}
	if snap.Agent.Kind != "claude_code" || snap.Agent.Command == "" {
		t.Errorf("agent summary wrong: %+v", snap.Agent)
	}
	if snap.Concurrency.Max != 3 {
		t.Errorf("max concurrency wrong: %d", snap.Concurrency.Max)
	}
	close(block)
	s.WaitForIdle()
}

func TestRefreshEndpoint_QueuesTick(t *testing.T) {
	s, _, _ := newSchedFixture(t)
	srv := NewServer(s)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/v1/refresh", nil))
	if rec.Code != http.StatusAccepted {
		t.Fatalf("expected 202 Accepted, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "refresh queued") {
		t.Errorf("body %q", rec.Body.String())
	}
}

func TestRefreshEndpoint_RejectsGet(t *testing.T) {
	s, _, _ := newSchedFixture(t)
	srv := NewServer(s)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/refresh", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", rec.Code)
	}
}

func TestIssueEndpoint_FoundAndNotFound(t *testing.T) {
	s, tr, ag := newSchedFixture(t)
	tr.Set(domain.Issue{ID: "demo", Identifier: "SYM-1", Title: "x", State: "Todo"})
	block := make(chan struct{})
	ag.SetDelay(func(_ agent.RunRequest) { <-block })
	_ = s.Tick(context.Background())

	srv := NewServer(s)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/demo", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for known issue, got %d body=%s", rec.Code, rec.Body.String())
	}

	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/missing", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for unknown issue, got %d", rec.Code)
	}
	close(block)
	s.WaitForIdle()
}

func TestDashboard_ReturnsHTML(t *testing.T) {
	s, _, _ := newSchedFixture(t)
	srv := NewServer(s)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status: %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "<title>symphony</title>") {
		t.Errorf("unexpected body: %s", rec.Body.String())
	}
}
