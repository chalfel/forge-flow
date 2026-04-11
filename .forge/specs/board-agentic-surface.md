# Board agentic surface
<!-- status: done -->
<!-- priority: high -->
<!-- created: 2026-04-11 -->
<!-- branch: feat/board-activity-feed-inbox -->

**What exists:** Static kanban board with lanes (Ready, In Progress, In Review, Blocked, Done) and last-run summary.

**What's missing:** The board shows state but doesn't tell the operator what to do. No activity heartbeat, no attention aggregation, no real-time updates.

**Demo:** Operator opens `forge board --web`. Within 5 seconds they see a colored Focus banner ("Respond: Agent needs info on auth provider"), an inbox pill with count, a sidebar of attention items, an agent status strip pulsing in real time, and an activity feed of agent events — all updating via SSE with htmx fallback. Press `j`/`k` to navigate, `Enter` to drill in, `i` to switch to inbox mode.

## Expected behavior
- Focus banner appears when inbox has items, hidden when empty.
- Sidebar shows inbox (top) and activity feed (bottom), polling every 3s via htmx, upgrading to SSE push when available.
- Agent status strip shows pulsing dots for `in_progress`, solid for `in_review`, with latest event snippet and relative time.
- Keyboard shortcuts: `j`/`k` navigate, `Enter` open, `i` toggle inbox mode, `r` refresh, `Escape` clear.
- SSE endpoint streams every 2s; client falls back to htmx polling on disconnect with exponential backoff up to 30s.
- Header shows "N needs attention" pill in amber when inbox is non-empty.

## Test cases

### Scenario: Operator opens the board with a failing task
Given a run with a failed task
When the operator opens the board
Then they see the Focus banner saying "Triage: Task failed" within the first screen

### Scenario: Agent emits a need_info event
Given an active run with an agent that wrote a need_info event
When the board refreshes
Then the inbox shows the need_info item with high urgency and the Focus banner points to it

### Scenario: SSE connection drops
Given the board is connected via SSE
When the server restarts
Then the client falls back to htmx polling within 5s and reconnects SSE with backoff

## Validation plan
- Unit tests for `deriveFocus`, `loadInbox`, `loadActivityFeed`, `loadAgentStatuses`, `relativeTime`.
- Integration test: `TestBoardIncludesFocusAndAgents` runs a real `ExecuteRun` and asserts the Board struct has Focus/Inbox/Feed fields populated.
- Manual smoke: `forge board --web` with a project that has blocked + in_review + proposed memories.

### Task: Activity feed + operator inbox data layer
<!-- status: done -->
<!-- parallelizable: no -->
<!-- deps: none -->
<!-- repo: forge-flow -->
<!-- touches: internal/forge/board_activity.go, internal/forge/board.go -->
<!-- conflict-risk: low -->

**Done when:**
- `ActivityItem` and `InboxItem` types exist.
- `loadActivityFeed` returns reverse-chronological events from recent runs, capped at 30.
- `loadInbox` returns urgency-sorted items from need_info, review, failed, blocked, memory sources.
- `Board` struct has `Feed` and `Inbox` fields populated by `Service.Board()`.

**Validation:**
- Covers: Scenario "Agent emits a need_info event"

### Task: Focus mode and agent status derivation
<!-- status: done -->
<!-- parallelizable: no -->
<!-- deps: Activity feed + operator inbox data layer -->
<!-- repo: forge-flow -->
<!-- touches: internal/forge/board_activity.go -->
<!-- conflict-risk: low -->

**Done when:**
- `deriveFocus` picks top-urgency inbox item and maps it to a verb (respond/review/triage/unblock/approve).
- `loadAgentStatuses` returns active tasks with their latest event, loading events once to avoid N+1.
- `Board.Focus` and `Board.Agents` are populated.

**Validation:**
- Covers: Scenario "Operator opens the board with a failing task"

### Task: Board web UI sidebar focus and agent strip
<!-- status: done -->
<!-- parallelizable: no -->
<!-- deps: Focus mode and agent status derivation -->
<!-- repo: forge-flow -->
<!-- touches: internal/forge/board_web.go -->
<!-- conflict-risk: medium -->

**Done when:**
- Sidebar template renders inbox + activity feed.
- Focus banner renders with colored pill (respond=red, review=amber, etc.) and suggested command.
- Agent strip renders pulsing dots per active task.
- Header shows amber "needs attention" pill.
- New endpoints: `/api/feed`, `/api/inbox`, `/partials/sidebar`.

**Validation:**
- Covers: Scenario "Agent emits a need_info event"

### Task: SSE real-time push with htmx fallback
<!-- status: done -->
<!-- parallelizable: no -->
<!-- deps: Board web UI sidebar focus and agent strip -->
<!-- repo: forge-flow -->
<!-- touches: internal/forge/board_web.go -->
<!-- conflict-risk: low -->

**Done when:**
- `/events/stream` SSE endpoint pushes board state every 2s.
- JS client upgrades from htmx polling to SSE on connect.
- On SSE error, falls back to htmx polling and retries with exponential backoff.
- Server wraps the handler to clear write deadline for `/events/stream` so long-lived connections don't hit the 10s WriteTimeout.

**Validation:**
- Covers: Scenario "SSE connection drops"

### Task: Keyboard navigation
<!-- status: done -->
<!-- parallelizable: no -->
<!-- deps: SSE real-time push with htmx fallback -->
<!-- repo: forge-flow -->
<!-- touches: internal/forge/board_web.go -->
<!-- conflict-risk: low -->

**Done when:**
- `j`/`k` navigate spec cards (default) or inbox items (after `i`).
- `Enter` opens the selected item (spec detail or inbox action).
- `r` refreshes the board.
- `Escape` clears selection.
- Selection shows a purple outline with smooth scroll-into-view.

**Validation:**
- Covers: Scenario "Operator opens the board with a failing task"
