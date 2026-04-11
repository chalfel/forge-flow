# Inbox actions — make attention items actionable from the board
<!-- status: todo -->
<!-- priority: high -->
<!-- created: 2026-04-11 -->
<!-- branch: feat/inbox-actions -->

**What exists:** The sidebar shows inbox items (need_info, review, failed, blocked, memory) but they're read-only. The operator has to drop to the terminal to respond.

**What's missing:** Acting on inbox items directly from the board. Respond to need_info, approve/reject reviews, promote memories, mark blockers resolved — all without leaving the board.

**Demo:** Operator sees a need_info item in the sidebar, clicks it, a panel opens with the question and a textarea. Operator types an answer, clicks "Send to agent", and the answer is written to the agent's events file. Agent picks it up on the next poll. Same flow for approving reviews, promoting memories, and unblocking tasks.

## Expected behavior
- Clicking an inbox item opens an action panel (slide-in or modal) with the item detail and the appropriate action UI.
- `need_info` → textarea + "Send to agent" button. POSTs to `/api/inbox/respond` which appends an `operator_response` event to the agent's events file.
- `review` → "Approve" and "Request changes" buttons. Approve marks task `done` and merges the PR. Request changes opens a textarea for feedback and marks the task `changes_requested`.
- `memory` → "Promote to KB" and "Dismiss". Promote moves the entry from `inbox.md` to `kb/memory.md`. Dismiss removes it from inbox.md.
- `blocked` → shows the blocking deps; "Mark dep resolved" button for each.
- `failed` → "Retry", "View history", "Open spec" buttons.
- All actions are optimistic: UI updates immediately, server call in background, rollback on error.

## Test cases

### Scenario: Operator responds to a need_info event
Given an agent emitted a need_info event
When the operator clicks the inbox item and sends a response
Then an `operator_response` event is appended to the agent's events file
And the sidebar removes the item on the next refresh

### Scenario: Operator approves a review task
Given a task in `in_review` status
When the operator clicks Approve
Then the task status becomes `done`
And the PR (if present) is marked ready to merge

### Scenario: Operator promotes a proposed memory
Given a proposed memory in inbox.md
When the operator clicks Promote
Then the entry is moved from inbox.md to kb/memory.md
And the inbox reflects the change

## Validation plan
- Unit tests for each action handler (`HandleNeedInfoResponse`, `HandleReviewApproval`, `PromoteMemory`, etc.).
- Integration tests hitting the real HTTP endpoints with tmpdir-backed workspace.
- Manual smoke test: trigger each action in the UI and verify filesystem state.

### Task: Action panel scaffolding and routing
<!-- status: todo -->
<!-- parallelizable: no -->
<!-- deps: none -->
<!-- repo: forge-flow -->
<!-- touches: internal/forge/board_actions.go, internal/forge/board_web.go -->
<!-- conflict-risk: low -->

**Done when:**
- New file `internal/forge/board_actions.go` with a dispatcher `HandleInboxAction` that routes to per-type handlers (initially stubbed).
- Route `/api/inbox/action` with POST body `{type, payload}` dispatching to the right handler.
- Action panel HTML template with slide-in animation.
- Clicking an inbox item opens the panel via htmx + hyperscript or vanilla JS.

**Validation:**
- Covers: Scenario "Operator responds to a need_info event"

### Task: need_info response handler
<!-- status: todo -->
<!-- parallelizable: no -->
<!-- deps: Action panel scaffolding and routing -->
<!-- repo: forge-flow -->
<!-- touches: internal/forge/board_actions_need_info.go, internal/forge/events.go -->
<!-- conflict-risk: low -->

**Done when:**
- `HandleNeedInfoResponse` appends an event with type `operator_response` to the agent's events file.
- Harness protocol section updated so agents know to watch for `operator_response` events.
- UI textarea posts to `/api/inbox/action` with type `need_info_response`.

**Validation:**
- Covers: Scenario "Operator responds to a need_info event"

### Task: Review approve and request changes handler
<!-- status: todo -->
<!-- parallelizable: no -->
<!-- deps: need_info response handler -->
<!-- repo: forge-flow -->
<!-- touches: internal/forge/board_actions_review.go, internal/forge/spec_update.go -->
<!-- conflict-risk: medium -->

**Done when:**
- `HandleReviewApproval` sets the task status to `done` in the spec file.
- `HandleRequestChanges` sets status to `changes_requested` and writes structured feedback.
- UI has Approve + Request changes buttons with textarea for feedback.

**Validation:**
- Covers: Scenario "Operator approves a review task"

### Task: Memory promotion handler
<!-- status: todo -->
<!-- parallelizable: no -->
<!-- deps: Review approve and request changes handler -->
<!-- repo: forge-flow -->
<!-- touches: internal/forge/board_actions_memory.go, internal/forge/memory.go -->
<!-- conflict-risk: low -->

**Done when:**
- `PromoteProposedMemory(entry)` removes the entry from inbox.md and appends it to kb/memory.md.
- Action panel shows Promote + Dismiss buttons for memory items.

**Validation:**
- Covers: Scenario "Operator promotes a proposed memory"
