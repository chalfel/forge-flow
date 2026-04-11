# Architecture — Forge Flow

## Stack
- **Language:** Go 1.23
- **CLI framework:** stdlib `flag` + charmbracelet/huh for interactive prompts
- **TUI:** charmbracelet/bubbletea + lipgloss (used selectively)
- **Web UI:** stdlib `net/http` + `html/template` + Tailwind (CDN) + htmx + SSE
- **Persistence:** file-based (JSON/JSONL/Markdown) — no database
- **Session orchestration:** tmux + git worktrees
- **Agent runtime:** claude CLI (shelled out with composed system prompts)

## Module layout
- `cmd/forge/main.go` — CLI entry point. Routes subcommands to handlers.
- `internal/forge/` — the whole service. Not exposed as a library.
  - `forge.go` — core `Service` + `ResolvedContext` model
  - `specs.go`, `spec_format.go`, `spec_generate.go`, `spec_update.go` — spec parsing, linting, generation
  - `run_execute.go`, `run_watch.go` — run planner + executor + watch daemon
  - `harness.go` — composes layered system prompts for agents
  - `worktree.go`, `tmux.go` — session orchestration primitives
  - `board.go`, `board_web.go`, `board_activity.go`, `board_spec_detail.go` — board + web UI
  - `events.go`, `decisions.go`, `history.go`, `memory.go` — agent protocol + persistence
  - `mcp.go` — MCP 2024-11-05 server (stdio)
  - `project_create.go`, `project_add.go`, `project_topology.go`, `stackpacks.go` — scaffolding
  - `dev.go`, `dev_interactive.go` — dev session config generation
- `stacks/` — embedded stack packs (blank, nextjs-saas). Embedded via `go:embed`.

## Data model
The workspace lives at `~/.forge/workspaces/{workspace}/projects/{projectId}/`:
- `project.json` — identity + topology + linked repos
- `config.md` — shared agent config
- `kb/` — architecture, business, roadmap, repos, memory
- `specs/` — capability specs (markdown with HTML-comment metadata)
- `reports/` — generated status reports
- `inbox.md` — capture + proposed memories queue

Each linked repo has `.forge/project.json` + runtime state:
- `.forge/runs/{runId}.json` — run records
- `.forge/runtime/{runId}/{taskSlug}.events.jsonl` — agent event stream
- `.forge/history/attempts/{specSlug}/{taskSlug}.jsonl` — attempt history
- `.forge/history/decisions/{specSlug}/{taskSlug}.jsonl` — decision log

## Board web architecture
- Single HTTP server on `127.0.0.1:{port}` (default 4242)
- Endpoints: `/`, `/partials/board`, `/partials/sidebar`, `/spec/{file}`, `/api/*`, `/events/stream` (SSE)
- Two polling layers: htmx for the kanban (5s), htmx for sidebar (3s)
- SSE upgrade: JS client connects to `/events/stream` and disables htmx polling when active. Falls back to htmx on disconnect with exponential backoff.
- Long-lived SSE handler has its write deadline cleared (default `WriteTimeout: 10s` would kill the connection).

## Agent protocol
Agents write events to `$FORGE_EVENTS_FILE` (JSONL, one event per line):
- `progress` — status update
- `decision` — architectural choice (persisted to decisions log)
- `blocked` — blocker encountered
- `need_info` — agent needs operator input
- `handoff` — work is ready for another agent/role
- `proposed_memory` — append to inbox.md for human review

The watch daemon and board ingest these events to drive the activity feed and operator inbox.

## Constraints
- **No external process managers.** tmux + worktrees only. No docker, no k8s.
- **No database.** File-based everything. JSONL for append-only logs, JSON for records, markdown for human-editable content.
- **No web framework.** stdlib net/http. Tailwind + htmx via CDN.
- **Tests must hit real tmpdirs.** Don't mock the filesystem. Integration tests create real worktrees where needed.
- **Parallel scaffolding rule.** When a spec creates a new module, the first task is a serial blocker that lays down the skeleton. Parallel tasks declare it as a dep.
- **Board is an agentic surface, not a PM tool.** Every UI element answers "does the operator need to do something here?" Prefer aggregation over scattered status pages.
