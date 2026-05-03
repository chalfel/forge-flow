# forge-flow

A Go orchestrator for coding agents — built around OpenAI's [Symphony spec](https://github.com/openai/symphony/blob/main/SPEC.md) and extended with first-class support for **GitHub Issues**, **Claude Code**, **repo-local skills**, and a **captain** agent that turns demands into tickets.

> Manage tickets, not sessions. The orchestrator polls your tracker, claims eligible tickets, prepares isolated workspaces, runs your coding agent, and reports back through the tracker itself. You stay in the loop without watching individual sessions.

## Why

Coding agents have outgrown the single-session UX. Most engineers run two or three agents in parallel and hit a hard ceiling — not on model capability, but on their own attention and context-switching cost. forge-flow moves you up one level: instead of supervising sessions, you write tickets and review outcomes.

The architecture is intentionally minimal:

- A **scheduler** (poll loop, claim state machine, retry/backoff)
- A **`WORKFLOW.md`** at the repo root (YAML config + markdown prompt template)
- A **tracker adapter** (Linear, GitHub Issues)
- An **agent runner** (Codex, Claude Code, or any CLI agent that accepts a prompt on stdin)

That's it. The full daemon is a single Go binary.

## Quick start

```bash
# Build
go build -o symphony ./cmd/symphony

# Validate your workflow file
./symphony validate WORKFLOW.md

# Dry-run with in-memory stubs (no tracker / no agent execution)
./symphony run --stub --once WORKFLOW.md

# Production run (Linear or GitHub, real Claude Code or Codex)
export LINEAR_API_KEY=lin_...    # or GITHUB_TOKEN=ghp_...
./symphony run --http :8080 WORKFLOW.md
```

Then create a ticket in your tracker with the configured active state — Symphony picks it up on the next 30-second poll, prepares a workspace, runs your agent, and surfaces progress on the dashboard at <http://localhost:8080>.

## `WORKFLOW.md`

Two parts: YAML front matter (config) and a markdown prompt body (rendered on every turn).

```yaml
---
tracker:
  kind: linear                       # or "github"
  project_slug: SYM                  # for linear; use `repo: owner/name` for github
  api_key: $LINEAR_API_KEY
  active_states: [Todo, In Progress] # tickets in these states are eligible
  terminal_states: [Done, Cancelled]
polling:
  interval_ms: 30000
workspace:
  root: ~/symphony_workspaces
hooks:
  after_create: |
    git clone git@github.com:you/your-repo.git .
  before_run: |
    npm install
  after_run: |
    echo "attempt finished"
  timeout_ms: 60000
agent:
  kind: claude_code                  # or "codex"
  max_concurrent_agents: 3
  max_turns: 20
  max_retry_backoff_ms: 300000
codex:
  command: codex app-server
claude_code:
  command: claude --print --permission-mode acceptEdits
captain:
  watch_label: needs-planning        # tickets with this label go to the captain
---
# Standard operating procedure

You are working on issue **{{ issue.identifier }} — {{ issue.title }}** (attempt {{ attempt }}).

## Skills you can invoke
{{ skills }}

## Plan
1. Read the issue description and any linked context.
2. Identify the smallest viable change that resolves the ticket end-to-end.
3. Write or update tests that fail without the change.

## Validate
- Run the full test suite and lint.
- For UI changes, capture a Playwright video and attach it to the ticket.

## Done criteria
- Tests green, lint clean, behaviour matches the ticket.
- Move the ticket to **In Review** with a short summary.
```

The prompt body supports placeholders: `{{ issue.identifier }}`, `{{ issue.title }}`, `{{ issue.description }}`, `{{ issue.labels }}`, `{{ attempt }}`, `{{ skills }}`. Unknown placeholders are left intact so typos surface.

The file is **hot-reloaded** — edit it while Symphony is running and changes (prompt body, polling cadence, concurrency, hooks, agent command) take effect within 2 seconds. Invalid edits are rejected and the previous valid configuration is retained.

## Subcommands

```
symphony validate <WORKFLOW.md>          parse + validate; exit non-zero on errors
symphony print    <WORKFLOW.md>          show resolved config + rendered placeholders
symphony run      [flags] <WORKFLOW.md>  daemon loop (real or --stub)
symphony captain  [flags] <WORKFLOW.md>  plan a demand into tickets and create them
symphony version
```

`symphony run` flags:

| flag             | effect                                                           |
| ---------------- | ---------------------------------------------------------------- |
| `--stub`         | in-memory tracker / agent / workspace (dry-run)                  |
| `--once`         | run a single tick and exit                                       |
| `--http <addr>`  | serve `/api/v1/{state,refresh,<id>}` + dashboard at the address  |

`symphony captain` flags:

| flag                   | effect                                              |
| ---------------------- | --------------------------------------------------- |
| `--demand "..."`       | high-level demand text                              |
| `--demand-file <path>` | read demand from a file                             |
| `--dry-run`            | plan only — print the proposed tickets, do not write |

If neither flag is set, the demand is read from stdin (when piped).

## Trackers

Both adapters implement the same `Tracker` interface. Switching is a one-line config change.

### Linear (`tracker.kind: linear`)

GraphQL adapter. Filters by `project.slugId` and state name. Implements:

- `FetchCandidates` (read)
- `GetIssue` (read)
- `CreateIssue` (write — used by the captain)

The API key resolves from `$LINEAR_API_KEY` (recommended) or any `$VAR` reference in `tracker.api_key`.

### GitHub Issues (`tracker.kind: github`)

REST adapter. Because GitHub Issues only has `open` / `closed` natively, the state machine is overlaid with **labels**: an issue is in state "Todo" iff it carries a label "Todo". The workflow's `active_states` drive both the filter and the derived state.

- Issue identifiers are `owner/repo#N`.
- PRs are filtered out automatically (the same endpoint returns them).
- Priority is derived from `priority:high`, `p:1`, etc. labels.

## Agents

Both Codex and Claude Code use the same generic shell runner — they only differ in the configured command and the YAML block consumed (`codex:` vs `claude_code:`). The runner:

- Launches `bash -lc <command>` with `cwd = workspace path`
- Pipes the rendered prompt to **stdin**
- Captures combined `stdout` + `stderr`
- Enforces `turn_timeout_ms` via `context.WithTimeout`
- Maps exit zero → `Succeeded`, non-zero → `Failed`, deadline → `TimedOut`

Any CLI agent that accepts a prompt on stdin works out of the box.

## Skills

Repo-local agent capabilities live at `<repo>/.symphony/skills/<name>/`:

```
.symphony/skills/
  grafana/
    SKILL.md          # description + usage instructions
    fetch-logs.sh     # invocation script
  playwright-video/
    SKILL.md
    record-start.sh
    record-stop.sh
```

The `{{ skills }}` placeholder in your prompt body renders an inventory the agent can invoke without spending tokens to discover them. Skills are discovered per-attempt from the workspace, so you can ship them inside the repo and version them with normal pull requests.

## Captain

Two modes:

**One-shot CLI** — refine a demand into tickets and create them:

```bash
symphony captain \
  --demand "We want dark mode across the marketing pages and dashboard" \
  WORKFLOW.md
# planned 4 ticket(s):
#   1. [Todo] Add ThemeProvider to root layout
#   2. [Todo] Migrate hardcoded colors in /marketing/*.tsx to tokens
#   3. [Todo] Add toggle to dashboard header
#   4. [Todo] Persist preference in localStorage
# created 4 ticket(s):
#   - SYM-101 — https://linear.app/.../SYM-101
#   ...
```

**Watch-label** — the daemon routes any ticket carrying `captain.watch_label` to the captain instead of the worker:

```yaml
captain:
  watch_label: needs-planning
```

Workflow: file a high-level ticket on Linear/GitHub with the `needs-planning` label → the next poll dispatches the captain → captain plans + writes the children → the parent is added to the in-memory skip set so it is not re-dispatched while Symphony is running.

The captain runs the same agent CLI as the worker (or override via `captain.command`). It expects the agent's output to contain a fenced JSON code block matching:

```json
{
  "tickets": [
    {
      "title": "...",
      "description": "...",
      "priority": 2,
      "labels": ["Todo"]
    }
  ]
}
```

The default planning prompt is baked in; override with `captain.prompt_path` for project-specific tone.

## Observability

When `--http <addr>` is set, the daemon exposes:

| endpoint            | method | result                                                 |
| ------------------- | ------ | ------------------------------------------------------ |
| `/`                 | GET    | minimal HTML dashboard (zero JS, monospace, refresh)   |
| `/api/v1/state`     | GET    | runtime summary (tracker, agent, concurrency, entries) |
| `/api/v1/<issueID>` | GET    | per-issue snapshot                                     |
| `/api/v1/refresh`   | POST   | queue an immediate scheduler tick                      |

Token telemetry and rate-limit fields are placeholders today (the shell runner does not parse them yet); the JSON contract is stable so future runners can fill them in without breaking clients.

## Architecture

```
forge-flow/
  cmd/symphony/                CLI entry point
  internal/
    config/                    WORKFLOW.md parser, validator, hot-reload watcher
    domain/                    Issue, Workspace, RunAttempt, IssueDraft, etc.
    scheduler/                 poll loop, claim state machine, retry/backoff,
                               eligibility, prompt renderer, skip set
    tracker/                   read/write interface
      linear/                  GraphQL adapter
      github/                  REST adapter (labels-as-state)
      stub/                    in-memory adapter for tests + --stub mode
    agent/                     Run interface
      shell/                   generic shell runner (codex + claude_code)
      router/                  label-based dispatch
      stub/                    in-memory queue agent for tests
    workspace/                 lifecycle interface
      fs.go                    real filesystem manager (hooks + path containment)
      stub.go                  no-op manager for tests
    skills/                    .symphony/skills/<name>/ discovery + render
    captain/                   plan + write tickets, agent.Agent adapter
    observability/             HTTP API + dashboard
```

The scheduler depends on interfaces only — every concrete adapter is swappable. New trackers, agents, or workspace strategies plug in without touching the core loop.

## Testing

```bash
go test ./...
```

Coverage targets the orchestration core: claim state machine, retry/backoff, eligibility, prompt rendering, skip-set safety, dispatch concurrency cap, dispatch idempotence, retry round-trip, tracker adapters via `httptest`, captain plan + create, router routing rules, workspace hooks (after_create / before_run / after_run / before_remove + path containment), config hot-reload (mtime + invalid-keep-previous), HTTP endpoints. No external services required.

## State machines (reference)

Issue claim state inside the scheduler:

```
Unclaimed → Claimed → Running → ┬→ Released
                                ├→ RetryQueued → Unclaimed (after due)
                                └→ Skip set    (captain decomposed)
```

Run-attempt status (subset of SPEC.md):

```
preparing_workspace → building_prompt → launching_agent_process →
  initializing_session → streaming_turn → finishing →
    {succeeded | failed | timed_out | stalled | canceled_by_reconciliation | decomposed}
```

## Spec compliance

forge-flow is a Go implementation of the Symphony [SPEC.md](https://github.com/openai/symphony/blob/main/SPEC.md). Every requirement labelled "must" in the spec is enforced:

| spec requirement                                 | status |
| ------------------------------------------------ | ------ |
| WORKFLOW.md parser (front matter + body)         | ✅     |
| `$VAR` env-var resolution + path normalisation   | ✅     |
| Startup config validation (kind, key, command)   | ✅     |
| Dynamic reload (invalid keeps previous valid)    | ✅     |
| Claim state machine (Unclaimed → Released)       | ✅     |
| Per-tick reconciliation of running issues        | ✅     |
| Sort by priority then created_at                 | ✅     |
| Eligibility (fields, blockers, concurrency)      | ✅     |
| Continuation retry (1s) + exponential backoff    | ✅     |
| Workspace path containment (root-relative)       | ✅     |
| Workspace key sanitised to `[A-Za-z0-9._-]`      | ✅     |
| Hook lifecycle: after_create / before_run / …    | ✅     |
| Hook timeout (default 60s)                       | ✅     |
| Read / turn / stall timeouts (independent)       | ✅     |
| Agent cwd validated before launch                | ✅     |
| `session_id` in structured logs                  | ✅     |
| Snapshot API (state, refresh, per-issue)         | ✅     |
| HTTP dashboard at `/`                            | ✅     |
| Tracker fetch-failure tolerance (skip + retry)   | ✅     |
| Startup terminal cleanup (orphan workspaces)     | ✅     |

Documented extensions beyond the spec:

- **GitHub Issues** tracker (state overlaid via labels)
- **Claude Code** agent alongside Codex
- **Skills** convention (`.symphony/skills/<name>/`) and `{{ skills }}` prompt placeholder
- **Captain** subcommand + watch-label routing for demand → tickets

Optional spec items still on the roadmap:

- **Token / rate-limit telemetry** — `LiveSession` carries fields; the shell runner does not parse them yet (Codex app-server stdio JSON-RPC would supply them natively)
- **Persistent retry queue across restarts** — spec explicitly allows this to be skipped; tracker is the source of truth
- **`linear_graphql` agent tool** — optional capability; not implemented
- **Captain → tracker state transitions** — captain currently relies on the in-memory skip set + manual label removal to prevent re-dispatch of decomposed parents

## Status

Active development. The PR linked in the repo description tracks the initial multi-phase build-out (parser → scheduler → adapters → workspace → agents → observability → reload → skills → captain). Breaking changes are possible while the v0 surface stabilises.

## License

This project is in early development; license to be added before the first tagged release.
