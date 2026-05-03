// Package observability serves the optional HTTP dashboard + JSON API
// described in SPEC.md. The endpoints are read-only except for /refresh,
// which queues an immediate scheduler tick.
package observability

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/chalfel/forge-flow/internal/scheduler"
)

type Server struct {
	sched   *scheduler.Scheduler
	started time.Time
	mux     *http.ServeMux
}

func NewServer(s *scheduler.Scheduler) *Server {
	srv := &Server{sched: s, started: time.Now()}
	mux := http.NewServeMux()
	mux.HandleFunc("/", srv.handleDashboard)
	mux.HandleFunc("/api/v1/state", srv.handleState)
	mux.HandleFunc("/api/v1/refresh", srv.handleRefresh)
	mux.HandleFunc("/api/v1/", srv.handleIssue) // catch-all for /api/v1/<identifier>
	srv.mux = mux
	return srv
}

func (s *Server) Handler() http.Handler { return s.mux }

func (s *Server) handleState(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	writeJSON(w, http.StatusOK, s.snapshot())
}

func (s *Server) handleRefresh(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	s.sched.Refresh()
	writeJSON(w, http.StatusAccepted, map[string]string{"status": "refresh queued"})
}

func (s *Server) handleIssue(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	id := strings.TrimPrefix(r.URL.Path, "/api/v1/")
	if id == "" || id == "state" || id == "refresh" {
		http.NotFound(w, r)
		return
	}
	for _, e := range s.sched.Snapshot() {
		if e.IssueID == id {
			writeJSON(w, http.StatusOK, e)
			return
		}
	}
	http.NotFound(w, r)
}

// Snapshot is the JSON shape returned by /api/v1/state. Token aggregates and
// rate-limit telemetry land here once the agent runners surface them; today
// they are zero-valued placeholders to keep the contract stable.
type Snapshot struct {
	Tracker        TrackerSummary             `json:"tracker"`
	Agent          AgentSummary               `json:"agent"`
	IntervalMs     int                        `json:"interval_ms"`
	UptimeSec      int64                      `json:"uptime_sec"`
	Concurrency    Concurrency                `json:"concurrency"`
	Entries        []scheduler.EntrySnapshot  `json:"entries"`
}

type TrackerSummary struct {
	Kind        string `json:"kind"`
	ProjectSlug string `json:"project_slug,omitempty"`
	Repo        string `json:"repo,omitempty"`
}

type AgentSummary struct {
	Kind    string `json:"kind"`
	Command string `json:"command"`
}

type Concurrency struct {
	Max     int `json:"max"`
	Running int `json:"running"`
	Queued  int `json:"retry_queued"`
}

func (s *Server) snapshot() Snapshot {
	cfg := s.sched.Config()
	entries := s.sched.Snapshot()
	running, queued := 0, 0
	for _, e := range entries {
		switch e.State {
		case scheduler.Running:
			running++
		case scheduler.RetryQueued:
			queued++
		}
	}
	return Snapshot{
		Tracker: TrackerSummary{
			Kind:        string(cfg.Tracker.Kind),
			ProjectSlug: cfg.Tracker.ProjectSlug,
			Repo:        cfg.Tracker.Repo,
		},
		Agent: AgentSummary{
			Kind:    cfg.Agent.Kind,
			Command: cfg.AgentCommandFor().Command,
		},
		IntervalMs:  cfg.Polling.IntervalMs,
		UptimeSec:   int64(time.Since(s.started).Seconds()),
		Concurrency: Concurrency{Max: cfg.Agent.MaxConcurrentAgents, Running: running, Queued: queued},
		Entries:     entries,
	}
}

func (s *Server) handleDashboard(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	snap := s.snapshot()
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprintf(w, `<!doctype html><meta charset=utf-8><title>symphony</title>
<style>body{font:14px ui-monospace,Menlo,monospace;padding:24px;max-width:900px;margin:0 auto}
h1{font-size:18px}table{width:100%%;border-collapse:collapse}td,th{padding:6px 8px;border-bottom:1px solid #eee;text-align:left}
.tag{display:inline-block;padding:2px 8px;border-radius:4px;background:#eee;font-size:12px}
.running{background:#dbeafe}.retry_queued{background:#fef3c7}.released{background:#d1fae5}.failed{background:#fee2e2}</style>
<h1>symphony · %s · %s</h1>
<p>uptime: %ds &nbsp; concurrency: %d/%d running, %d retry-queued &nbsp; interval: %dms</p>
<table><thead><tr><th>issue</th><th>state</th><th>attempt</th><th>error</th></tr></thead><tbody>`,
		snap.Tracker.Kind, snap.Agent.Kind, snap.UptimeSec,
		snap.Concurrency.Running, snap.Concurrency.Max, snap.Concurrency.Queued, snap.IntervalMs)
	for _, e := range snap.Entries {
		fmt.Fprintf(w, `<tr><td>%s</td><td><span class="tag %s">%s</span></td><td>%d</td><td>%s</td></tr>`,
			htmlEscape(e.IssueID), e.State, e.State, e.Attempt, htmlEscape(e.LastErr))
	}
	fmt.Fprint(w, `</tbody></table>
<form method="post" action="/api/v1/refresh"><button>refresh now</button></form>`)
}

func htmlEscape(s string) string {
	r := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;")
	return r.Replace(s)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
