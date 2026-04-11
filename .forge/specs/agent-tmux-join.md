# Agent tmux join — one-click join active agent session
<!-- status: todo -->
<!-- priority: medium -->
<!-- created: 2026-04-11 -->
<!-- branch: feat/agent-tmux-join -->

**What exists:** The board shows active agents in the status strip but there's no way to jump into their tmux session without manually knowing the session/pane name.

**What's missing:** Session and pane tracking in the run state, surfaced on the agent strip and inbox items so the operator can join with one click (copy command now, embedded terminal later).

**Demo:** Operator sees an agent pulsing in the agent strip, clicks it, and gets a popover with the exact `tmux attach-session -t forge-watch` command (or a button if the board is running in a native shell context). Clicking "Copy" copies the command. Future: clicking "Join" opens an embedded xterm.js terminal attached to the pane.

## Expected behavior
- `watchedTask` struct gains `SessionName string` (already has `paneID`) and persists both in run state.
- `AgentStatus` gains `Session` and `PaneID` fields populated from the watch daemon's state.
- Clicking an agent in the strip shows a popover: session name, pane ID, and a "Copy attach command" button.
- Inbox items for `need_info` also show the copy-attach button because the operator often wants to join the agent to respond.
- CLI helper: `forge run attach --task "Task Name"` resolves the pane and runs `tmux attach`.

## Test cases

### Scenario: Operator joins an active agent
Given the watch daemon is running and has spawned agents
When the operator clicks an agent in the board strip
Then a popover shows the tmux attach command for that specific session
And clicking Copy copies it to the clipboard

### Scenario: Agent session info persists across board refreshes
Given a watch run has spawned an agent in session "forge-watch"
When the board refreshes via SSE
Then the agent strip still shows the correct session name

## Validation plan
- Unit test for `buildAgentAttachCommand(session, paneID)`.
- Integration test: simulate a watch run writing session info to a run record, load board, assert `Agents[0].Session` is populated.
- Manual smoke: run `forge run watch` + open board + click an agent.

### Task: Persist session and pane info in run state
<!-- status: todo -->
<!-- parallelizable: no -->
<!-- deps: none -->
<!-- repo: forge-flow -->
<!-- touches: internal/forge/run_watch.go, internal/forge/run_execute.go, internal/forge/board_activity.go -->
<!-- conflict-risk: medium -->

**Done when:**
- `TaskAttempt` struct gains `Session` and `PaneID` fields.
- Watch daemon writes session/pane when spawning a task and updates the run record.
- `loadAgentStatuses` reads these fields and populates `AgentStatus.Session` / `.PaneID`.

**Validation:**
- Covers: Scenario "Agent session info persists across board refreshes"

### Task: Agent strip popover with copy-attach command
<!-- status: todo -->
<!-- parallelizable: yes -->
<!-- deps: Persist session and pane info in run state -->
<!-- repo: forge-flow -->
<!-- touches: internal/forge/board_web.go -->
<!-- conflict-risk: low -->

**Done when:**
- Clicking an agent in the strip opens a popover anchored to the pulse dot.
- Popover shows session name, pane ID, and a "Copy attach command" button.
- Copy button uses `navigator.clipboard.writeText()`.
- Popover closes on Escape or outside click.

**Validation:**
- Covers: Scenario "Operator joins an active agent"

### Task: CLI command forge run attach
<!-- status: todo -->
<!-- parallelizable: yes -->
<!-- deps: Persist session and pane info in run state -->
<!-- repo: forge-flow -->
<!-- touches: cmd/forge/main.go -->
<!-- conflict-risk: low -->

**Done when:**
- `forge run attach --task "Task Name"` resolves the session from the latest run.
- Executes `tmux attach-session -t <session>` replacing the current process.
- Error if task not found or not currently running.

**Validation:**
- Covers: Scenario "Operator joins an active agent"
