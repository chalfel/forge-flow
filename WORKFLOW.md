---
tracker:
  kind: linear
  project_slug: SYM
  api_key: $LINEAR_API_KEY
  active_states: [Todo, In Progress]
  terminal_states: [Done, Cancelled, Duplicate]
polling:
  interval_ms: 30000
workspace:
  root: ~/symphony_workspaces
hooks:
  after_create: |
    git clone git@github.com:chalfel/forge-flow.git .
  before_run: |
    go mod download
  after_run: |
    echo "attempt finished"
  timeout_ms: 60000
agent:
  kind: claude_code
  max_concurrent_agents: 3
  max_turns: 20
  max_retry_backoff_ms: 300000
codex:
  command: codex app-server
  turn_timeout_ms: 3600000
  read_timeout_ms: 5000
  stall_timeout_ms: 300000
claude_code:
  command: claude --print --permission-mode acceptEdits
  turn_timeout_ms: 3600000
  read_timeout_ms: 5000
  stall_timeout_ms: 300000
---
# Standard operating procedure

You are working on issue **{{ issue.identifier }} — {{ issue.title }}** (attempt {{ attempt }}).

## Plan
1. Read the issue description and any linked context.
2. Identify the smallest viable change that resolves the ticket end-to-end.
3. Write or update tests that fail without the change.

## Implement
- Make the change in the smallest blast radius possible.
- Keep diffs scoped to the ticket; do not refactor opportunistically.

## Validate
- Run the full test suite and lint.
- For UI changes, capture a Playwright video and attach it to the ticket.
- Re-read the diff. Anything that looks load-bearing but untested gets a test.

## Done criteria
- Tests green, lint clean, behaviour matches the ticket.
- Move ticket to **In Review** with a short summary of the change and the
  validation evidence. A human merges.
