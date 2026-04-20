package forge

import (
	"encoding/json"
	"fmt"
	"html/template"
	"net"
	"net/http"
	"strings"
	"time"
)

// ServeBoardWeb starts an HTTP server serving the board UI.
func (s *Service) ServeBoardWeb(cwd string, port int) error {
	mux := http.NewServeMux()

	// effectiveCwd returns the cwd to use for this request. Reads the
	// forge_project cookie; if valid, resolves to the project's first repo
	// path. Otherwise falls back to the startup cwd.
	effectiveCwd := func(r *http.Request) string {
		c, err := r.Cookie("forge_project")
		if err != nil || c.Value == "" {
			return cwd
		}
		parts := strings.SplitN(c.Value, "/", 2)
		if len(parts) != 2 {
			return cwd
		}
		path, err := s.CwdForProject(parts[0], parts[1])
		if err != nil {
			return cwd
		}
		return path
	}

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		board, err := s.Board(effectiveCwd(r))
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_ = boardHTMLTemplate.Execute(w, board)
	})

	mux.HandleFunc("/api/board", func(w http.ResponseWriter, r *http.Request) {
		board, err := s.Board(effectiveCwd(r))
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(board)
	})

	mux.HandleFunc("/partials/board", func(w http.ResponseWriter, r *http.Request) {
		board, err := s.Board(effectiveCwd(r))
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_ = boardPartialTemplate.Execute(w, board)
	})

	mux.HandleFunc("/spec/", func(w http.ResponseWriter, r *http.Request) {
		specFile := r.URL.Path[len("/spec/"):]
		if specFile == "" {
			http.Error(w, "missing spec file", 400)
			return
		}
		detail, err := s.SpecDetail(effectiveCwd(r), specFile)
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_ = specDetailTemplate.Execute(w, detail)
	})

	mux.HandleFunc("/api/spec/", func(w http.ResponseWriter, r *http.Request) {
		specFile := r.URL.Path[len("/api/spec/"):]
		if specFile == "" {
			http.Error(w, "missing spec file", 400)
			return
		}
		detail, err := s.SpecDetail(effectiveCwd(r), specFile)
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(detail)
	})

	mux.HandleFunc("/api/events/", func(w http.ResponseWriter, r *http.Request) {
		runID := r.URL.Path[len("/api/events/"):]
		if runID == "" {
			http.Error(w, "missing run ID", 400)
			return
		}
		events, err := s.LoadRunEvents(effectiveCwd(r), runID)
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(events)
	})

	mux.HandleFunc("/api/feed", func(w http.ResponseWriter, r *http.Request) {
		board, err := s.Board(effectiveCwd(r))
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(board.Feed)
	})

	mux.HandleFunc("/api/inbox", func(w http.ResponseWriter, r *http.Request) {
		board, err := s.Board(effectiveCwd(r))
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(board.Inbox)
	})

	mux.HandleFunc("/partials/sidebar", func(w http.ResponseWriter, r *http.Request) {
		board, err := s.Board(effectiveCwd(r))
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_ = sidebarPartialTemplate.Execute(w, board)
	})

	// Dashboard (roadmap milestone view + Claude analysis).
	mux.HandleFunc("/dashboard", func(w http.ResponseWriter, r *http.Request) {
		view, err := s.Dashboard(effectiveCwd(r))
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_ = dashboardHTMLTemplate.Execute(w, view)
	})

	mux.HandleFunc("/api/dashboard", func(w http.ResponseWriter, r *http.Request) {
		view, err := s.Dashboard(effectiveCwd(r))
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(view)
	})

	mux.HandleFunc("/api/analyze", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", 405)
			return
		}
		analysis, err := s.RunProjectAnalysis(effectiveCwd(r))
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(analysis)
	})

	// Project picker endpoints.
	mux.HandleFunc("/api/projects", func(w http.ResponseWriter, r *http.Request) {
		projects, err := s.ListAllProjects()
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(projects)
	})

	mux.HandleFunc("/project/select", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", 405)
			return
		}
		var payload struct {
			Workspace string `json:"workspace"`
			ProjectID string `json:"projectId"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			http.Error(w, "invalid payload", 400)
			return
		}
		if payload.Workspace == "" || payload.ProjectID == "" {
			http.Error(w, "missing workspace or projectId", 400)
			return
		}
		// Validate the project exists and has a linked repo.
		if _, err := s.CwdForProject(payload.Workspace, payload.ProjectID); err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		http.SetCookie(w, &http.Cookie{
			Name:     "forge_project",
			Value:    payload.Workspace + "/" + payload.ProjectID,
			Path:     "/",
			HttpOnly: true,
			SameSite: http.SameSiteLaxMode,
			MaxAge:   60 * 60 * 24 * 365, // 1 year
		})
		w.WriteHeader(http.StatusNoContent)
	})

	// SSE endpoint — pushes board state every 2s for real-time updates.
	// Clients connect via EventSource; htmx polling is the fallback.
	mux.HandleFunc("/events/stream", func(w http.ResponseWriter, r *http.Request) {
		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "streaming not supported", 500)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")

		ctx := r.Context()
		ticker := time.NewTicker(2 * time.Second)
		defer ticker.Stop()

		reqCwd := effectiveCwd(r)

		// Send initial state immediately.
		sendBoardSSE(w, flusher, s, reqCwd)

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				sendBoardSSE(w, flusher, s, reqCwd)
			}
		}
	})

	addr := fmt.Sprintf("127.0.0.1:%d", port)
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("starting server: %w", err)
	}

	fmt.Printf("Forge Board running at http://%s\n", addr)

	// Wrap handler to disable WriteTimeout for long-lived endpoints. SSE is
	// streaming indefinitely; /api/analyze shells out to the claude CLI which
	// commonly takes 15-30s, longer than the default 10s write timeout.
	longLived := map[string]bool{
		"/events/stream": true,
		"/api/analyze":   true,
	}
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if longLived[r.URL.Path] {
			if rc := http.NewResponseController(w); rc != nil {
				_ = rc.SetWriteDeadline(time.Time{})
			}
		}
		mux.ServeHTTP(w, r)
	})

	server := &http.Server{
		Handler:      handler,
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 10 * time.Second,
		IdleTimeout:  60 * time.Second,
	}
	return server.Serve(listener)
}

// sendBoardSSE writes a single SSE "board" event with the current board state
// as JSON. The JS client uses this to trigger partial refreshes.
func sendBoardSSE(w http.ResponseWriter, flusher http.Flusher, s *Service, cwd string) {
	board, err := s.Board(cwd)
	if err != nil {
		fmt.Fprintf(w, "event: error\ndata: %s\n\n", err.Error())
		flusher.Flush()
		return
	}

	// Send JSON state for JS consumers.
	data, _ := json.Marshal(board)
	fmt.Fprintf(w, "event: board\ndata: %s\n\n", data)
	flusher.Flush()
}

var funcMap = template.FuncMap{
	"progressWidth": func(pct float64) string {
		return fmt.Sprintf("%.0f%%", pct)
	},
	"laneAccent": func(name string) string {
		m := map[string]string{
			"Ready":       "#8b5cf6",
			"In Progress": "#3b82f6",
			"In Review":   "#f59e0b",
			"Blocked":     "#ef4444",
			"Done":        "#22c55e",
		}
		if c, ok := m[name]; ok {
			return c
		}
		return "#6b7280"
	},
	"inboxIcon": func(typ string) template.HTML {
		icons := map[string]string{
			"need_info": `<path d="M6 3a3 3 0 0 0-3 3v.5h1V6a2 2 0 1 1 4 0c0 .8-.5 1.2-1 1.6-.6.5-1 1-1 1.9h1c0-.5.3-.9.8-1.3.6-.5 1.2-1.1 1.2-2.2a3 3 0 0 0-3-3zM5.5 10.5a.75.75 0 1 1 1.5 0 .75.75 0 0 1-1.5 0z`,
			"review":    `<path d="M3 2.5a.5.5 0 0 1 .5-.5h5a.5.5 0 0 1 .354.146l2.5 2.5A.5.5 0 0 1 11.5 5v6.5a.5.5 0 0 1-.5.5H3.5a.5.5 0 0 1-.5-.5v-9zM5 6h4M5 8h4" fill="none" stroke="currentColor" stroke-width="1"/>`,
			"blocked":   `<circle cx="6" cy="6" r="5" fill="none" stroke="currentColor" stroke-width="1.2"/><path d="M4 4l4 4M8 4l-4 4" stroke="currentColor" stroke-width="1.2" stroke-linecap="round"/>`,
			"failed":    `<circle cx="6" cy="6" r="5" fill="none" stroke="currentColor" stroke-width="1.2"/><path d="M6 4v3M6 8.5v.5" stroke="currentColor" stroke-width="1.2" stroke-linecap="round"/>`,
			"memory":    `<path d="M3 3h6v8H3zM5 3V1M7 3V1M3 5.5h6M3 8h6" fill="none" stroke="currentColor" stroke-width="1"/>`,
		}
		svg := icons[typ]
		if svg == "" {
			svg = `<circle cx="6" cy="6" r="5" fill="none" stroke="currentColor" stroke-width="1.2"/>`
		}
		return template.HTML(fmt.Sprintf(`<svg width="12" height="12" viewBox="0 0 12 12" fill="none" class="flex-shrink-0">%s</svg>`, svg))
	},
	"inboxColor": func(urgency string) string {
		m := map[string]string{"high": "#ef4444", "medium": "#f59e0b", "low": "#6b7280"}
		if c, ok := m[urgency]; ok {
			return c
		}
		return "#6b7280"
	},
	"feedIcon": func(typ string) template.HTML {
		colors := map[string]string{
			"progress":        "#3b82f6",
			"decision":        "#8b5cf6",
			"blocked":         "#ef4444",
			"need_info":       "#f59e0b",
			"handoff":         "#22c55e",
			"proposed_memory": "#6b7280",
		}
		c := colors[typ]
		if c == "" {
			c = "#6b7280"
		}
		return template.HTML(fmt.Sprintf(`<svg width="6" height="6" viewBox="0 0 6 6" class="flex-shrink-0 mt-1.5"><circle cx="3" cy="3" r="3" fill="%s"/></svg>`, c))
	},
	"feedLabel": func(typ string) string {
		m := map[string]string{
			"progress":        "progress",
			"decision":        "decided",
			"blocked":         "blocked",
			"need_info":       "needs info",
			"handoff":         "handoff",
			"proposed_memory": "memory",
		}
		if l, ok := m[typ]; ok {
			return l
		}
		return typ
	},
	"focusColor": func(action string) string {
		m := map[string]string{
			"respond": "#ef4444", "review": "#f59e0b", "triage": "#ef4444",
			"unblock": "#f59e0b", "approve": "#22c55e", "check": "#6b7280",
		}
		if c, ok := m[action]; ok {
			return c
		}
		return "#8b5cf6"
	},
	"focusVerb": func(action string) string {
		m := map[string]string{
			"respond": "Respond", "review": "Review", "triage": "Triage",
			"unblock": "Unblock", "approve": "Approve", "check": "Check",
		}
		if v, ok := m[action]; ok {
			return v
		}
		return "Act"
	},
	"agentPulse": func(status string) template.HTML {
		if status == "in_progress" {
			return template.HTML(`<span class="relative flex h-2 w-2"><span class="animate-ping absolute inline-flex h-full w-full rounded-full bg-blue-400 opacity-75"></span><span class="relative inline-flex rounded-full h-2 w-2 bg-blue-500"></span></span>`)
		}
		return template.HTML(`<span class="inline-flex rounded-full h-2 w-2 bg-amber-500"></span>`)
	},
	"laneIcon": func(name string) template.HTML {
		icons := map[string]string{
			"Ready":       `<circle cx="6" cy="6" r="5" fill="none" stroke="#8b5cf6" stroke-width="1.5"/><circle cx="6" cy="6" r="2" fill="#8b5cf6"/>`,
			"In Progress": `<circle cx="6" cy="6" r="5" fill="none" stroke="#3b82f6" stroke-width="1.5"/><path d="M6 1 A5 5 0 0 1 11 6" fill="none" stroke="#3b82f6" stroke-width="1.5" stroke-linecap="round"/>`,
			"In Review":   `<circle cx="6" cy="6" r="5" fill="none" stroke="#f59e0b" stroke-width="1.5"/><path d="M4 6h4M6 4v4" stroke="#f59e0b" stroke-width="1.5" stroke-linecap="round"/>`,
			"Blocked":     `<circle cx="6" cy="6" r="5" fill="none" stroke="#ef4444" stroke-width="1.5"/><path d="M4 4l4 4M8 4l-4 4" stroke="#ef4444" stroke-width="1.5" stroke-linecap="round"/>`,
			"Done":        `<circle cx="6" cy="6" r="5" fill="#22c55e" stroke="none"/><path d="M4 6l1.5 1.5L8 5" stroke="white" stroke-width="1.5" fill="none" stroke-linecap="round" stroke-linejoin="round"/>`,
		}
		svg := icons[name]
		if svg == "" {
			svg = `<circle cx="6" cy="6" r="5" fill="none" stroke="#6b7280" stroke-width="1.5"/>`
		}
		return template.HTML(fmt.Sprintf(`<svg width="12" height="12" viewBox="0 0 12 12">%s</svg>`, svg))
	},
	"taskDot": func(status string) template.HTML {
		colors := map[string]string{
			"todo": "#6b7280", "in_progress": "#3b82f6", "in_review": "#f59e0b",
			"changes_requested": "#ef4444", "done": "#22c55e",
			"ready": "#8b5cf6", "blocked": "#ef4444",
		}
		c := colors[status]
		if c == "" {
			c = "#6b7280"
		}
		return template.HTML(fmt.Sprintf(`<svg width="8" height="8" viewBox="0 0 8 8"><circle cx="4" cy="4" r="3" fill="%s"/></svg>`, c))
	},
	"runStatusColor": func(status RunStatus) string {
		switch status {
		case RunStatusSucceeded:
			return "#22c55e"
		case RunStatusFailed:
			return "#ef4444"
		default:
			return "#6b7280"
		}
	},
}

var boardHTMLTemplate = template.Must(template.New("board").Funcs(funcMap).Parse(boardFullHTML))
var boardPartialTemplate = template.Must(template.New("partial").Funcs(funcMap).Parse(boardPartialHTML))
var sidebarPartialTemplate = template.Must(template.New("sidebar").Funcs(funcMap).Parse(sidebarPartialHTML))
var dashboardHTMLTemplate = template.Must(template.New("dashboard").Funcs(funcMap).Parse(dashboardFullHTML))

const boardFullHTML = `<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="UTF-8">
  <meta name="viewport" content="width=device-width, initial-scale=1.0">
  <title>Forge Board{{if .ProjectName}} — {{.ProjectName}}{{end}}</title>
  <script src="https://cdn.tailwindcss.com"></script>
  <script src="https://unpkg.com/htmx.org@2.0.4"></script>
  <link rel="preconnect" href="https://fonts.googleapis.com">
  <link href="https://fonts.googleapis.com/css2?family=Inter:wght@400;500;600&family=JetBrains+Mono:wght@400;500&display=swap" rel="stylesheet">
  <script>
    tailwind.config = {
      theme: {
        extend: {
          fontFamily: { sans: ['Inter', 'system-ui', 'sans-serif'], mono: ['JetBrains Mono', 'monospace'] },
          colors: {
            surface: { 0: '#0a0a0a', 1: '#111111', 2: '#191919', 3: '#222222', 4: '#2a2a2a' },
            border: { subtle: 'rgba(255,255,255,0.06)', medium: 'rgba(255,255,255,0.1)' },
            txt: { primary: 'rgba(255,255,255,0.92)', secondary: 'rgba(255,255,255,0.55)', tertiary: 'rgba(255,255,255,0.35)' },
            accent: { purple: '#8b5cf6', blue: '#3b82f6', amber: '#f59e0b', red: '#ef4444', green: '#22c55e' }
          }
        }
      }
    }
  </script>
  <style>
    body { background: #0a0a0a; color: rgba(255,255,255,0.92); }
    * { scrollbar-width: thin; scrollbar-color: rgba(255,255,255,0.1) transparent; }
    .spec-card { transition: background 0.15s ease; }
    .spec-card:hover { background: #222222 !important; }
    .lane-col { min-width: 280px; max-width: 320px; }
    #sidebar-content { max-height: calc(100vh - 120px); overflow-y: auto; }
    @keyframes focus-glow { 0%,100% { opacity: 0.6; } 50% { opacity: 1; } }
    .focus-glow { animation: focus-glow 2s ease-in-out infinite; }
  </style>
</head>
<body class="min-h-screen font-sans antialiased">
  <div id="project-picker-menu" class="hidden fixed z-50 bg-surface-1 border border-border-medium rounded-lg shadow-2xl min-w-[260px] max-h-[400px] overflow-y-auto">
    <div class="px-3 py-2 text-[11px] font-mono text-txt-tertiary uppercase tracking-wider border-b border-border-subtle">Projects</div>
    <div id="project-picker-list" class="py-1">
      <div class="px-3 py-2 text-[12px] text-txt-tertiary">Loading...</div>
    </div>
  </div>
  <div id="board-content" hx-get="/partials/board" hx-trigger="every 5s" hx-swap="innerHTML">
` + boardPartialHTML + `
  </div>
  <script>
  // SSE upgrade: if EventSource is available, connect to /events/stream
  // and disable htmx polling. Falls back to htmx polling on disconnect.
  (function() {
    var boardEl = document.getElementById('board-content');
    var sseActive = false;
    var retryDelay = 1000;

    function connectSSE() {
      if (!window.EventSource) return;
      var es = new EventSource('/events/stream');

      es.addEventListener('board', function(e) {
        if (!sseActive) {
          // Disable htmx polling since SSE is working.
          boardEl.removeAttribute('hx-get');
          boardEl.removeAttribute('hx-trigger');
          boardEl.removeAttribute('hx-swap');
          if (window.htmx) htmx.process(boardEl);
          sseActive = true;
          retryDelay = 1000;
        }
        // Refresh the board partial via htmx swap.
        fetch('/partials/board').then(function(r) { return r.text(); }).then(function(html) {
          boardEl.innerHTML = html;
        });
        // Refresh sidebar independently.
        var sidebar = document.getElementById('sidebar-content');
        if (sidebar) {
          fetch('/partials/sidebar').then(function(r) { return r.text(); }).then(function(html) {
            sidebar.innerHTML = html;
          });
        }
      });

      es.onerror = function() {
        es.close();
        sseActive = false;
        // Re-enable htmx polling as fallback.
        boardEl.setAttribute('hx-get', '/partials/board');
        boardEl.setAttribute('hx-trigger', 'every 5s');
        boardEl.setAttribute('hx-swap', 'innerHTML');
        if (window.htmx) htmx.process(boardEl);
        // Retry SSE with backoff.
        setTimeout(connectSSE, retryDelay);
        retryDelay = Math.min(retryDelay * 2, 30000);
      };
    }

    connectSSE();
  })();

  // Highlight the active nav link based on URL path.
  (function() {
    var path = window.location.pathname;
    var active = path === '/dashboard' ? 'dashboard' : 'board';
    document.querySelectorAll('.nav-link').forEach(function(el) {
      if (el.getAttribute('data-nav') === active) {
        el.classList.add('text-txt-primary', 'bg-surface-2');
        el.classList.remove('text-txt-secondary');
      }
    });
  })();

  // Project picker dropdown — fetches /api/projects, renders the list,
  // and posts the selection to /project/select before reloading.
  (function() {
    function openPicker() {
      var btn = document.getElementById('project-picker-btn');
      var menu = document.getElementById('project-picker-menu');
      if (!btn || !menu) return;

      var rect = btn.getBoundingClientRect();
      menu.style.top = (rect.bottom + 6) + 'px';
      menu.style.left = rect.left + 'px';
      menu.classList.remove('hidden');

      fetch('/api/projects').then(function(r) { return r.json(); }).then(function(projects) {
        var list = document.getElementById('project-picker-list');
        if (!projects || projects.length === 0) {
          list.innerHTML = '<div class="px-3 py-2 text-[12px] text-txt-tertiary">No projects found</div>';
          return;
        }
        var byWorkspace = {};
        projects.forEach(function(p) {
          if (!byWorkspace[p.workspace]) byWorkspace[p.workspace] = [];
          byWorkspace[p.workspace].push(p);
        });
        var html = '';
        Object.keys(byWorkspace).sort().forEach(function(ws) {
          html += '<div class="px-3 pt-2 pb-1 text-[10px] font-mono text-txt-tertiary uppercase">' + ws + '</div>';
          byWorkspace[ws].forEach(function(p) {
            html += '<button class="project-item w-full text-left px-3 py-2 text-[13px] text-txt-primary hover:bg-surface-2 transition-colors flex items-center gap-2" data-ws="' + p.workspace + '" data-id="' + p.projectId + '">';
            html += '<span class="flex-1 truncate">' + p.projectName + '</span>';
            html += '<span class="text-[10px] font-mono text-txt-tertiary">' + p.projectId + '</span>';
            html += '</button>';
          });
        });
        list.innerHTML = html;
        list.querySelectorAll('.project-item').forEach(function(btn) {
          btn.addEventListener('click', function() {
            var ws = btn.getAttribute('data-ws');
            var id = btn.getAttribute('data-id');
            fetch('/project/select', {
              method: 'POST',
              headers: { 'Content-Type': 'application/json' },
              body: JSON.stringify({ workspace: ws, projectId: id })
            }).then(function(r) {
              if (r.ok) { window.location.reload(); }
              else { r.text().then(function(t) { alert('Failed to switch project: ' + t); }); }
            });
          });
        });
      });
    }

    function closePicker() {
      var menu = document.getElementById('project-picker-menu');
      if (menu) menu.classList.add('hidden');
    }

    document.addEventListener('click', function(e) {
      var btn = e.target.closest('#project-picker-btn');
      var menu = document.getElementById('project-picker-menu');
      if (btn) {
        e.stopPropagation();
        if (menu && !menu.classList.contains('hidden')) closePicker();
        else openPicker();
        return;
      }
      if (menu && !menu.classList.contains('hidden') && !e.target.closest('#project-picker-menu')) {
        closePicker();
      }
    });

    document.addEventListener('keydown', function(e) {
      if (e.key === 'Escape') closePicker();
    });
  })();

  // Keyboard shortcuts for rapid operator navigation.
  (function() {
    var selectedIdx = -1;
    var mode = 'board'; // board|inbox

    function getInboxItems() {
      var sidebar = document.getElementById('sidebar-content');
      if (!sidebar) return [];
      return sidebar.querySelectorAll('[data-inbox-item]');
    }

    function getSpecCards() {
      return document.querySelectorAll('.spec-card');
    }

    function clearSelection() {
      document.querySelectorAll('.kbd-selected').forEach(function(el) {
        el.classList.remove('kbd-selected');
        el.style.outline = '';
      });
    }

    function selectItem(items, idx) {
      clearSelection();
      if (idx < 0 || idx >= items.length) return;
      var el = items[idx];
      el.classList.add('kbd-selected');
      el.style.outline = '2px solid #8b5cf6';
      el.style.outlineOffset = '1px';
      el.scrollIntoView({ block: 'nearest', behavior: 'smooth' });
    }

    document.addEventListener('keydown', function(e) {
      // Ignore if typing in an input.
      if (e.target.tagName === 'INPUT' || e.target.tagName === 'TEXTAREA') return;

      var items = mode === 'inbox' ? getInboxItems() : getSpecCards();

      switch(e.key) {
        case 'j': // next
          e.preventDefault();
          selectedIdx = Math.min(selectedIdx + 1, items.length - 1);
          selectItem(items, selectedIdx);
          break;
        case 'k': // prev
          e.preventDefault();
          selectedIdx = Math.max(selectedIdx - 1, 0);
          selectItem(items, selectedIdx);
          break;
        case 'Enter': // open
          if (selectedIdx >= 0 && selectedIdx < items.length) {
            var el = items[selectedIdx];
            if (el.href) window.location = el.href;
            else if (el.querySelector('a')) el.querySelector('a').click();
          }
          break;
        case 'i': // toggle inbox mode
          e.preventDefault();
          mode = mode === 'inbox' ? 'board' : 'inbox';
          selectedIdx = -1;
          clearSelection();
          break;
        case 'r': // refresh
          e.preventDefault();
          fetch('/partials/board').then(function(r) { return r.text(); }).then(function(html) {
            document.getElementById('board-content').innerHTML = html;
          });
          break;
        case 'Escape':
          selectedIdx = -1;
          mode = 'board';
          clearSelection();
          break;
      }
    });
  })();
  </script>
</body>
</html>`

const sidebarPartialHTML = `
{{if .Inbox}}
<div class="mb-4">
  <div class="flex items-center gap-2 px-1 mb-2">
    <svg width="12" height="12" viewBox="0 0 12 12" fill="none"><circle cx="6" cy="6" r="5" fill="none" stroke="#f59e0b" stroke-width="1.5"/><path d="M6 4v3M6 8.5v.5" stroke="#f59e0b" stroke-width="1.5" stroke-linecap="round"/></svg>
    <span class="text-[13px] font-medium text-txt-secondary">Needs Attention</span>
    <span class="text-[11px] font-mono px-1.5 py-0.5 rounded-full bg-amber-500/10 text-amber-400">{{len .Inbox}}</span>
  </div>
  <div class="space-y-1">
    {{range .Inbox}}
    <div class="rounded-lg border border-border-subtle bg-surface-1 px-3 py-2.5 hover:bg-surface-2 transition-colors cursor-pointer" data-inbox-item>
      <div class="flex items-start gap-2">
        <div style="color: {{inboxColor .Urgency}}" class="mt-0.5">{{inboxIcon .Type}}</div>
        <div class="flex-1 min-w-0">
          <div class="flex items-center gap-2">
            <span class="text-[12px] font-medium text-txt-primary">{{.Title}}</span>
            {{if .Urgency}}<span class="text-[10px] font-mono px-1 py-0.5 rounded" style="color: {{inboxColor .Urgency}}; background: {{inboxColor .Urgency}}15">{{.Urgency}}</span>{{end}}
          </div>
          {{if .Detail}}<p class="text-[11px] text-txt-tertiary mt-1 truncate">{{.Detail}}</p>{{end}}
          {{if .TaskName}}<p class="text-[10px] font-mono text-txt-tertiary mt-1">{{.TaskName}}</p>{{end}}
        </div>
      </div>
    </div>
    {{end}}
  </div>
</div>
{{end}}

{{if .Feed}}
<div>
  <div class="flex items-center gap-2 px-1 mb-2">
    <svg width="12" height="12" viewBox="0 0 12 12" fill="none"><circle cx="6" cy="6" r="2" fill="#3b82f6"/><circle cx="6" cy="6" r="5" fill="none" stroke="#3b82f6" stroke-width="1" opacity="0.4"/></svg>
    <span class="text-[13px] font-medium text-txt-secondary">Activity</span>
  </div>
  <div class="space-y-0.5">
    {{range .Feed}}
    <div class="flex items-start gap-2 px-2 py-1.5 rounded hover:bg-surface-2 transition-colors">
      {{feedIcon .Type}}
      <div class="flex-1 min-w-0">
        <p class="text-[11px] text-txt-secondary leading-snug truncate">{{.Message}}</p>
        <div class="flex items-center gap-2 mt-0.5">
          <span class="text-[10px] font-mono text-txt-tertiary">{{feedLabel .Type}}</span>
          {{if .Ago}}<span class="text-[10px] font-mono text-txt-tertiary">{{.Ago}}</span>{{end}}
        </div>
      </div>
    </div>
    {{end}}
  </div>
</div>
{{end}}

{{if and (not .Inbox) (not .Feed)}}
<div class="flex flex-col items-center justify-center py-12 text-center">
  <svg width="24" height="24" viewBox="0 0 24 24" fill="none" class="text-txt-tertiary mb-3">
    <circle cx="12" cy="12" r="10" stroke="currentColor" stroke-width="1.5"/>
    <path d="M8 12h8M12 8v8" stroke="currentColor" stroke-width="1.5" stroke-linecap="round"/>
  </svg>
  <p class="text-[13px] text-txt-tertiary">No activity yet</p>
  <p class="text-[11px] text-txt-tertiary mt-1">Run a spec to see agents working</p>
</div>
{{end}}
`

const boardPartialHTML = `
<header class="border-b border-border-subtle">
  <div class="max-w-[1800px] mx-auto px-6 h-14 flex items-center justify-between">
    <div class="flex items-center gap-3">
      <svg width="20" height="20" viewBox="0 0 20 20" fill="none">
        <rect width="20" height="20" rx="5" fill="#8b5cf6"/>
        <path d="M6 10h8M10 6v8" stroke="white" stroke-width="1.5" stroke-linecap="round"/>
      </svg>
      <span class="text-[15px] font-semibold tracking-tight">Forge</span>
      {{if .ProjectName}}
      <span class="text-txt-tertiary mx-1">/</span>
      <button id="project-picker-btn" class="text-[15px] text-txt-secondary hover:text-txt-primary transition-colors flex items-center gap-1 cursor-pointer">
        <span>{{.ProjectName}}</span>
        <svg width="10" height="10" viewBox="0 0 10 10" class="text-txt-tertiary"><path d="M2 4l3 3 3-3" stroke="currentColor" stroke-width="1.5" fill="none" stroke-linecap="round" stroke-linejoin="round"/></svg>
      </button>
      {{end}}
      <nav class="flex items-center gap-1 ml-4">
        <a href="/" class="text-[13px] px-2 py-1 rounded text-txt-secondary hover:text-txt-primary hover:bg-surface-2 transition-colors nav-link" data-nav="board">Board</a>
        <a href="/dashboard" class="text-[13px] px-2 py-1 rounded text-txt-secondary hover:text-txt-primary hover:bg-surface-2 transition-colors nav-link" data-nav="dashboard">Dashboard</a>
      </nav>
    </div>
    <div class="flex items-center gap-6">
      {{if .Inbox}}<div class="flex items-center gap-1.5 text-[12px] font-mono px-2.5 py-1 rounded-full bg-amber-500/10 text-amber-400 border border-amber-500/20">
        <svg width="10" height="10" viewBox="0 0 12 12" fill="none"><circle cx="6" cy="6" r="5" fill="none" stroke="currentColor" stroke-width="1.5"/><path d="M6 4v3M6 8.5v.5" stroke="currentColor" stroke-width="1.5" stroke-linecap="round"/></svg>
        {{len .Inbox}} needs attention
      </div>{{end}}
      <div class="flex items-center gap-4 text-[13px] text-txt-secondary">
        <span><span class="text-txt-primary font-medium">{{.Summary.TotalSpecs}}</span> specs</span>
        <span><span class="text-txt-primary font-medium">{{.Summary.TotalTasks}}</span> tasks</span>
        <span><span class="text-accent-green font-medium">{{.Summary.Done}}</span> done</span>
      </div>
    </div>
  </div>
</header>

<div class="max-w-[1800px] mx-auto px-6 py-4">
  <div class="flex items-center gap-3">
    <div class="flex-1 h-1.5 bg-surface-3 rounded-full overflow-hidden">
      <div class="h-full bg-accent-purple rounded-full transition-all duration-700" style="width: {{progressWidth .Summary.ProgressPct}}"></div>
    </div>
    <span class="text-[12px] font-mono text-txt-tertiary">{{progressWidth .Summary.ProgressPct}}</span>
  </div>
</div>

{{if .LastRun}}
<div class="max-w-[1800px] mx-auto px-6 pb-3">
  <div class="flex items-center gap-2 text-[12px] font-mono text-txt-tertiary bg-surface-1 border border-border-subtle rounded-md px-3 py-2">
    <svg width="12" height="12" viewBox="0 0 12 12"><circle cx="6" cy="6" r="5" fill="none" stroke="{{runStatusColor .LastRun.Status}}" stroke-width="1.5"/></svg>
    <span>{{.LastRun.RunID}}</span>
    <span style="color: {{runStatusColor .LastRun.Status}}">{{.LastRun.Status}}</span>
    <span class="text-txt-tertiary ml-2">{{.LastRun.Summary.Succeeded}} ok  {{.LastRun.Summary.Skipped}} skip  {{.LastRun.Summary.Failed}} fail</span>
  </div>
</div>
{{end}}

{{if .Focus}}
<div class="max-w-[1800px] mx-auto px-6 pb-3">
  <div class="rounded-lg border px-4 py-3 flex items-center gap-4" style="border-color: {{focusColor .Focus.Action}}30; background: {{focusColor .Focus.Action}}08">
    <div class="flex-shrink-0">
      <span class="inline-flex items-center rounded-md px-2.5 py-1 text-[12px] font-semibold text-white" style="background: {{focusColor .Focus.Action}}">
        {{focusVerb .Focus.Action}}
      </span>
    </div>
    <div class="flex-1 min-w-0">
      <div class="flex items-center gap-2">
        <span class="text-[14px] font-medium text-txt-primary">{{.Focus.Title}}</span>
        {{if .Focus.TaskName}}<span class="text-[11px] font-mono text-txt-tertiary">{{.Focus.TaskName}}</span>{{end}}
      </div>
      {{if .Focus.Detail}}<p class="text-[12px] text-txt-secondary mt-0.5 truncate">{{.Focus.Detail}}</p>{{end}}
    </div>
    {{if .Focus.Command}}<code class="text-[11px] font-mono text-txt-tertiary bg-surface-3 rounded px-2 py-1 flex-shrink-0 hidden sm:block">{{.Focus.Command}}</code>{{end}}
  </div>
</div>
{{end}}

{{if .Agents}}
<div class="max-w-[1800px] mx-auto px-6 pb-3">
  <div class="flex items-center gap-3 overflow-x-auto">
    {{range .Agents}}
    <div class="flex items-center gap-2 bg-surface-1 border border-border-subtle rounded-lg px-3 py-2 flex-shrink-0">
      {{agentPulse .Status}}
      <div class="min-w-0">
        <div class="text-[12px] font-medium text-txt-primary truncate max-w-[200px]">{{.TaskName}}</div>
        {{if .LastEvent}}<div class="text-[10px] text-txt-tertiary truncate max-w-[200px]">{{.LastEvent}}</div>{{end}}
      </div>
      {{if .Ago}}<span class="text-[10px] font-mono text-txt-tertiary flex-shrink-0">{{.Ago}}</span>{{end}}
    </div>
    {{end}}
  </div>
</div>
{{end}}

<div class="max-w-[1800px] mx-auto px-6 py-2 flex gap-4 items-start pb-12">
  <div class="flex-1 flex gap-3 overflow-x-auto items-start min-w-0">
    {{range .Lanes}}
    {{$laneName := .Name}}
    {{$accent := laneAccent $laneName}}
    <div class="lane-col flex-shrink-0">
      <div class="flex items-center gap-2 px-1 mb-2 h-8">
        {{laneIcon $laneName}}
        <span class="text-[13px] font-medium text-txt-secondary">{{$laneName}}</span>
        <span class="text-[12px] font-mono text-txt-tertiary ml-1">{{.Count}}</span>
      </div>

      <div class="space-y-1">
        {{if not .Specs}}
        <div class="rounded-lg border border-border-subtle bg-surface-1 px-4 py-8 text-center">
          <p class="text-[13px] text-txt-tertiary">No specs</p>
        </div>
        {{end}}

        {{range .Specs}}
        <a href="/spec/{{.File}}" class="spec-card block rounded-lg border border-border-subtle bg-surface-1 px-4 py-3 cursor-pointer no-underline">
          <div class="flex items-start justify-between gap-2">
            <h3 class="text-[13px] font-medium leading-snug text-txt-primary">{{.Title}}</h3>
          </div>

          <div class="flex items-center gap-2 mt-2.5">
            <div class="flex-1 h-1 bg-surface-3 rounded-full overflow-hidden">
              <div class="h-full rounded-full transition-all duration-500" style="width: {{progressWidth .ProgressPct}}; background: {{$accent}}"></div>
            </div>
            <span class="text-[11px] font-mono text-txt-tertiary whitespace-nowrap">{{.DoneTasks}}/{{.TotalTasks}}</span>
          </div>

          <div class="mt-3 space-y-1.5">
            {{range .Tasks}}
            <div class="flex items-center gap-2 text-[12px] group">
              {{taskDot .Status}}
              <span class="truncate {{if eq .Status "done"}}text-txt-tertiary line-through{{else}}text-txt-secondary group-hover:text-txt-primary{{end}}">{{.Task}}</span>
              {{if .Repo}}<span class="ml-auto text-[11px] font-mono text-txt-tertiary border border-border-subtle rounded px-1.5 py-0.5 flex-shrink-0">{{.Repo}}</span>{{end}}
            </div>
            {{if .Reasons}}
            <div class="ml-4 text-[11px] text-txt-tertiary">
              {{range .Reasons}}<p>{{.}}</p>{{end}}
            </div>
            {{end}}
            {{end}}
          </div>
        </a>
        {{end}}
      </div>
    </div>
    {{end}}
  </div>

  <div class="w-[300px] flex-shrink-0 sticky top-4" id="sidebar-content" hx-get="/partials/sidebar" hx-trigger="every 3s" hx-swap="innerHTML">
    ` + sidebarPartialHTML + `
  </div>
</div>
`

const dashboardFullHTML = `<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="UTF-8">
  <meta name="viewport" content="width=device-width, initial-scale=1.0">
  <title>Dashboard{{if .ProjectName}} — {{.ProjectName}}{{end}} | Forge</title>
  <script src="https://cdn.tailwindcss.com"></script>
  <script src="https://cdn.jsdelivr.net/npm/marked/marked.min.js"></script>
  <link rel="preconnect" href="https://fonts.googleapis.com">
  <link href="https://fonts.googleapis.com/css2?family=Inter:wght@400;500;600;700&family=JetBrains+Mono:wght@400;500&display=swap" rel="stylesheet">
  <script>
    tailwind.config = {
      theme: {
        extend: {
          fontFamily: { sans: ['Inter', 'system-ui', 'sans-serif'], mono: ['JetBrains Mono', 'monospace'] },
          colors: {
            surface: { 0: '#0a0a0a', 1: '#111111', 2: '#191919', 3: '#222222', 4: '#2a2a2a' },
            border: { subtle: 'rgba(255,255,255,0.06)', medium: 'rgba(255,255,255,0.1)' },
            txt: { primary: 'rgba(255,255,255,0.92)', secondary: 'rgba(255,255,255,0.55)', tertiary: 'rgba(255,255,255,0.35)' },
            accent: { purple: '#8b5cf6', blue: '#3b82f6', amber: '#f59e0b', red: '#ef4444', green: '#22c55e' }
          }
        }
      }
    }
  </script>
  <style>
    body { background: #0a0a0a; color: rgba(255,255,255,0.92); }
    * { scrollbar-width: thin; scrollbar-color: rgba(255,255,255,0.1) transparent; }
    .prose h1 { font-size: 1.5rem; font-weight: 600; margin: 1.25rem 0 0.75rem; }
    .prose h2 { font-size: 1.15rem; font-weight: 600; margin: 1rem 0 0.5rem; color: rgba(255,255,255,0.92); }
    .prose h3 { font-size: 1rem; font-weight: 600; margin: 0.75rem 0 0.35rem; color: rgba(255,255,255,0.85); }
    .prose p { margin: 0.5rem 0; color: rgba(255,255,255,0.75); line-height: 1.6; }
    .prose strong { color: rgba(255,255,255,0.95); font-weight: 600; }
    .prose ul { margin: 0.5rem 0 0.75rem 1.25rem; list-style: disc; color: rgba(255,255,255,0.75); }
    .prose li { margin: 0.2rem 0; }
    .prose code { background: #222; padding: 1px 5px; border-radius: 3px; font-family: 'JetBrains Mono', monospace; font-size: 0.85em; color: #c4b5fd; }
    @keyframes spin { to { transform: rotate(360deg); } }
    .spinner { animation: spin 1s linear infinite; }
  </style>
</head>
<body class="min-h-screen font-sans antialiased">
  <div id="project-picker-menu" class="hidden fixed z-50 bg-surface-1 border border-border-medium rounded-lg shadow-2xl min-w-[260px] max-h-[400px] overflow-y-auto">
    <div class="px-3 py-2 text-[11px] font-mono text-txt-tertiary uppercase tracking-wider border-b border-border-subtle">Projects</div>
    <div id="project-picker-list" class="py-1">
      <div class="px-3 py-2 text-[12px] text-txt-tertiary">Loading...</div>
    </div>
  </div>

  <header class="border-b border-border-subtle">
    <div class="max-w-5xl mx-auto px-6 h-14 flex items-center gap-3">
      <svg width="20" height="20" viewBox="0 0 20 20" fill="none">
        <rect width="20" height="20" rx="5" fill="#8b5cf6"/>
        <path d="M6 10h8M10 6v8" stroke="white" stroke-width="1.5" stroke-linecap="round"/>
      </svg>
      <span class="text-[15px] font-semibold tracking-tight">Forge</span>
      {{if .ProjectName}}
      <span class="text-txt-tertiary mx-1">/</span>
      <button id="project-picker-btn" class="text-[15px] text-txt-secondary hover:text-txt-primary transition-colors flex items-center gap-1 cursor-pointer">
        <span>{{.ProjectName}}</span>
        <svg width="10" height="10" viewBox="0 0 10 10" class="text-txt-tertiary"><path d="M2 4l3 3 3-3" stroke="currentColor" stroke-width="1.5" fill="none" stroke-linecap="round" stroke-linejoin="round"/></svg>
      </button>
      {{end}}
      <nav class="flex items-center gap-1 ml-4">
        <a href="/" class="text-[13px] px-2 py-1 rounded text-txt-secondary hover:text-txt-primary hover:bg-surface-2 transition-colors nav-link" data-nav="board">Board</a>
        <a href="/dashboard" class="text-[13px] px-2 py-1 rounded text-txt-secondary hover:text-txt-primary hover:bg-surface-2 transition-colors nav-link" data-nav="dashboard">Dashboard</a>
      </nav>
    </div>
  </header>

  <main class="max-w-5xl mx-auto px-6 py-8">
    {{if not .HasRoadmap}}
    <div class="rounded-lg border border-border-subtle bg-surface-1 p-8 text-center">
      <svg width="40" height="40" viewBox="0 0 40 40" fill="none" class="mx-auto mb-4 text-txt-tertiary">
        <rect x="6" y="6" width="28" height="28" rx="3" stroke="currentColor" stroke-width="2"/>
        <path d="M12 14h16M12 20h12M12 26h10" stroke="currentColor" stroke-width="2" stroke-linecap="round"/>
      </svg>
      <h2 class="text-[16px] font-semibold text-txt-primary mb-2">No roadmap yet</h2>
      <p class="text-[13px] text-txt-secondary max-w-md mx-auto">Add a <code class="text-[12px] font-mono bg-surface-3 px-1.5 py-0.5 rounded">kb/roadmap.md</code> with <code class="text-[12px] font-mono bg-surface-3 px-1.5 py-0.5 rounded">## Now</code>, <code class="text-[12px] font-mono bg-surface-3 px-1.5 py-0.5 rounded">## Next</code>, and <code class="text-[12px] font-mono bg-surface-3 px-1.5 py-0.5 rounded">## Later</code> sections to see milestone progress here.</p>
    </div>
    {{else}}
    <div class="mb-6">
      <h1 class="text-2xl font-semibold tracking-tight mb-1">Roadmap progress</h1>
      <p class="text-[13px] text-txt-tertiary">{{.DoneSpecs}} of {{.TotalSpecs}} specs done</p>
    </div>

    <div class="space-y-4 mb-8">
      {{range .Milestones}}
      <div class="rounded-lg border bg-surface-1 p-5 {{if .Current}}border-accent-purple/40 ring-1 ring-accent-purple/20{{else}}border-border-subtle{{end}}">
        <div class="flex items-center justify-between mb-3">
          <div class="flex items-center gap-3">
            <h2 class="text-[15px] font-semibold text-txt-primary">{{.Name}}</h2>
            {{if .Current}}<span class="text-[10px] font-mono uppercase tracking-wider px-2 py-0.5 rounded-full bg-accent-purple/15 text-accent-purple">Current</span>{{end}}
          </div>
          <div class="text-[12px] font-mono text-txt-tertiary">{{.DoneCount}}/{{.Total}} · {{.DonePct}}%</div>
        </div>
        <div class="h-1 bg-surface-3 rounded-full overflow-hidden mb-4">
          <div class="h-full bg-accent-purple rounded-full transition-all duration-500" style="width: {{.DonePct}}%"></div>
        </div>
        <div class="space-y-1.5">
          {{range .Bullets}}
          <div class="flex items-start gap-2 text-[13px] {{if .Done}}text-txt-tertiary{{else}}text-txt-secondary{{end}}">
            {{if .Done}}
            <svg width="14" height="14" viewBox="0 0 14 14" class="mt-0.5 flex-shrink-0 text-accent-green"><circle cx="7" cy="7" r="6" fill="currentColor"/><path d="M4.5 7l2 2 3-4" stroke="#0a0a0a" stroke-width="1.5" fill="none" stroke-linecap="round" stroke-linejoin="round"/></svg>
            {{else}}
            <svg width="14" height="14" viewBox="0 0 14 14" class="mt-0.5 flex-shrink-0 text-txt-tertiary"><circle cx="7" cy="7" r="6" fill="none" stroke="currentColor" stroke-width="1.5"/></svg>
            {{end}}
            <span class="{{if .Done}}line-through{{end}}">{{.Text}}</span>
            {{if .MatchedSpec}}
            <a href="/spec/{{.MatchedSpec}}" class="text-[11px] font-mono text-txt-tertiary hover:text-accent-purple ml-1">{{.SpecProgress}}%</a>
            {{end}}
          </div>
          {{end}}
        </div>
      </div>
      {{end}}
    </div>
    {{end}}

    <div class="rounded-lg border border-border-subtle bg-surface-1 p-5">
      <div class="flex items-center justify-between mb-4">
        <div class="flex items-center gap-2">
          <svg width="16" height="16" viewBox="0 0 16 16" fill="none" class="text-accent-purple"><circle cx="8" cy="8" r="7" stroke="currentColor" stroke-width="1.5"/><path d="M8 4v4l2.5 2.5" stroke="currentColor" stroke-width="1.5" stroke-linecap="round"/></svg>
          <h2 class="text-[14px] font-semibold text-txt-primary">Claude analysis</h2>
          {{if .CachedAnalysis}}<span class="text-[11px] font-mono text-txt-tertiary" id="analysis-timestamp">{{.CachedAnalysis.GeneratedAt}}</span>{{end}}
        </div>
        <button id="run-analysis-btn" class="text-[12px] font-medium px-3 py-1.5 rounded bg-accent-purple hover:bg-accent-purple/80 text-white transition-colors flex items-center gap-1.5">
          <svg id="run-icon" width="12" height="12" viewBox="0 0 12 12" fill="none"><path d="M3 2l6 4-6 4V2z" fill="currentColor"/></svg>
          <svg id="run-spinner" width="12" height="12" viewBox="0 0 12 12" fill="none" class="hidden spinner"><circle cx="6" cy="6" r="4" stroke="currentColor" stroke-width="1.5" stroke-dasharray="6 12"/></svg>
          <span id="run-label">Run analysis</span>
        </button>
      </div>
      <div id="analysis-error" class="hidden mb-3 text-[12px] text-accent-red bg-accent-red/10 border border-accent-red/20 rounded px-3 py-2"></div>
      <div id="analysis-content" class="prose">
        {{if .CachedAnalysis}}
        <div id="analysis-markdown" data-markdown="{{.CachedAnalysis.Markdown}}"></div>
        {{else}}
        <p class="text-[13px] text-txt-tertiary italic">No analysis yet — click Run to generate one.</p>
        {{end}}
      </div>
    </div>
  </main>

  <script>
  // Highlight the active nav link.
  (function() {
    var active = window.location.pathname === '/dashboard' ? 'dashboard' : 'board';
    document.querySelectorAll('.nav-link').forEach(function(el) {
      if (el.getAttribute('data-nav') === active) {
        el.classList.add('text-txt-primary', 'bg-surface-2');
        el.classList.remove('text-txt-secondary');
      }
    });
  })();

  // Render cached markdown if present.
  (function() {
    var el = document.getElementById('analysis-markdown');
    if (el && el.dataset.markdown) {
      el.innerHTML = marked.parse(el.dataset.markdown);
    }
  })();

  // Run analysis button.
  (function() {
    var btn = document.getElementById('run-analysis-btn');
    if (!btn) return;
    var icon = document.getElementById('run-icon');
    var spinner = document.getElementById('run-spinner');
    var label = document.getElementById('run-label');
    var content = document.getElementById('analysis-content');
    var errEl = document.getElementById('analysis-error');
    var tsEl = document.getElementById('analysis-timestamp');

    btn.addEventListener('click', function() {
      btn.disabled = true;
      icon.classList.add('hidden');
      spinner.classList.remove('hidden');
      label.textContent = 'Analyzing...';
      errEl.classList.add('hidden');

      fetch('/api/analyze', { method: 'POST' })
        .then(function(r) {
          if (!r.ok) { return r.text().then(function(t) { throw new Error(t || 'Analysis failed'); }); }
          return r.json();
        })
        .then(function(analysis) {
          content.innerHTML = marked.parse(analysis.markdown);
          if (tsEl) tsEl.textContent = analysis.generatedAt;
          else {
            var ts = document.createElement('span');
            ts.id = 'analysis-timestamp';
            ts.className = 'text-[11px] font-mono text-txt-tertiary';
            ts.textContent = analysis.generatedAt;
            btn.parentElement.querySelector('.flex.items-center.gap-2').appendChild(ts);
          }
        })
        .catch(function(err) {
          errEl.textContent = err.message || 'Failed to run analysis';
          errEl.classList.remove('hidden');
        })
        .finally(function() {
          btn.disabled = false;
          icon.classList.remove('hidden');
          spinner.classList.add('hidden');
          label.textContent = 'Run analysis';
        });
    });
  })();

  // Project picker (shared with board).
  (function() {
    function openPicker() {
      var btn = document.getElementById('project-picker-btn');
      var menu = document.getElementById('project-picker-menu');
      if (!btn || !menu) return;
      var rect = btn.getBoundingClientRect();
      menu.style.top = (rect.bottom + 6) + 'px';
      menu.style.left = rect.left + 'px';
      menu.classList.remove('hidden');
      fetch('/api/projects').then(function(r) { return r.json(); }).then(function(projects) {
        var list = document.getElementById('project-picker-list');
        if (!projects || projects.length === 0) {
          list.innerHTML = '<div class="px-3 py-2 text-[12px] text-txt-tertiary">No projects found</div>';
          return;
        }
        var byWorkspace = {};
        projects.forEach(function(p) {
          if (!byWorkspace[p.workspace]) byWorkspace[p.workspace] = [];
          byWorkspace[p.workspace].push(p);
        });
        var html = '';
        Object.keys(byWorkspace).sort().forEach(function(ws) {
          html += '<div class="px-3 pt-2 pb-1 text-[10px] font-mono text-txt-tertiary uppercase">' + ws + '</div>';
          byWorkspace[ws].forEach(function(p) {
            html += '<button class="project-item w-full text-left px-3 py-2 text-[13px] text-txt-primary hover:bg-surface-2 transition-colors flex items-center gap-2" data-ws="' + p.workspace + '" data-id="' + p.projectId + '">';
            html += '<span class="flex-1 truncate">' + p.projectName + '</span>';
            html += '<span class="text-[10px] font-mono text-txt-tertiary">' + p.projectId + '</span>';
            html += '</button>';
          });
        });
        list.innerHTML = html;
        list.querySelectorAll('.project-item').forEach(function(btn) {
          btn.addEventListener('click', function() {
            fetch('/project/select', {
              method: 'POST',
              headers: { 'Content-Type': 'application/json' },
              body: JSON.stringify({ workspace: btn.getAttribute('data-ws'), projectId: btn.getAttribute('data-id') })
            }).then(function(r) { if (r.ok) window.location.reload(); });
          });
        });
      });
    }
    function closePicker() {
      var menu = document.getElementById('project-picker-menu');
      if (menu) menu.classList.add('hidden');
    }
    document.addEventListener('click', function(e) {
      var btn = e.target.closest('#project-picker-btn');
      var menu = document.getElementById('project-picker-menu');
      if (btn) { e.stopPropagation(); if (menu && !menu.classList.contains('hidden')) closePicker(); else openPicker(); return; }
      if (menu && !menu.classList.contains('hidden') && !e.target.closest('#project-picker-menu')) closePicker();
    });
    document.addEventListener('keydown', function(e) { if (e.key === 'Escape') closePicker(); });
  })();
  </script>
</body>
</html>`
