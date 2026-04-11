# Run history detail — drill into any run's events, decisions, and attempts
<!-- status: todo -->
<!-- priority: medium -->
<!-- created: 2026-04-11 -->
<!-- branch: feat/run-history-detail -->

**What exists:** The board shows only the latest run as a summary line. Past runs are invisible. Events, decisions, and attempts live in JSONL files that only the CLI can inspect.

**What's missing:** A dedicated detail page for any run — timeline of events, decisions made, tasks attempted, outcomes. Filters by task and event type. Linkable URL per run.

**Demo:** Operator clicks the "last run" summary in the board header. A run detail page opens at `/run/{runId}` showing a timeline of all events across all tasks, grouped by task with collapse/expand. Decisions appear inline with their rationale. Failed tasks show their error. A filter bar lets the operator hide progress events to focus on decisions and blockers.

## Expected behavior
- `/run/{runId}` page loads the full run record + events + decisions + attempts for that run.
- Timeline view: events sorted newest-first, grouped by task, with type-colored pills.
- Decision cards inline in the timeline showing choice, rationale, alternatives.
- Failed tasks show expanded error detail with a "Retry" button.
- Filter bar with checkboxes per event type (progress, decision, blocked, need_info, handoff).
- URL query params persist filter state: `/run/{runId}?hide=progress`.
- "Recent runs" list on the board header expands to show the last 10 runs as a dropdown with links.

## Test cases

### Scenario: Operator opens a run detail page
Given a run with multiple tasks and events
When the operator navigates to /run/{runId}
Then they see a timeline with events grouped by task

### Scenario: Operator filters out progress events
Given a run detail page is open
When the operator unchecks "progress" in the filter bar
Then only decisions, blockers, and need_info events are visible

### Scenario: Operator retries a failed task from the run detail
Given a run with a failed task
When the operator clicks Retry on that task
Then a new attempt is started with the same spec + task args

## Validation plan
- Unit tests for the event-loading logic (aggregate events across tasks, group, sort, filter).
- Integration test: create a fake run with events and decisions, hit `/run/{runId}`, assert the response contains all tasks.
- Manual smoke: open a real run detail page.

### Task: Run detail data aggregator
<!-- status: todo -->
<!-- parallelizable: no -->
<!-- deps: none -->
<!-- repo: forge-flow -->
<!-- touches: internal/forge/run_detail.go -->
<!-- conflict-risk: low -->

**Done when:**
- New file `internal/forge/run_detail.go` with `Service.RunDetail(cwd, runID) (RunDetailView, error)`.
- `RunDetailView` aggregates: run record, all events grouped by task, decisions per task, attempt history.
- Events sorted newest-first per task; tasks ordered by first-event-timestamp.
- Support for event-type filtering via a filter argument.

**Validation:**
- Covers: Scenario "Operator opens a run detail page"

### Task: Run detail web page and route
<!-- status: todo -->
<!-- parallelizable: no -->
<!-- deps: Run detail data aggregator -->
<!-- repo: forge-flow -->
<!-- touches: internal/forge/board_web.go -->
<!-- conflict-risk: medium -->

**Done when:**
- Route `/run/{runId}` serves an HTML template using `RunDetailView`.
- Timeline UI with per-task sections, event pills, decision cards.
- Filter bar with URL query state (`?hide=progress,handoff`).
- Recent runs dropdown in the board header linking to this route.
- Back link to the board.

**Validation:**
- Covers: Scenario "Operator opens a run detail page"
- Covers: Scenario "Operator filters out progress events"

### Task: Retry failed task action
<!-- status: todo -->
<!-- parallelizable: no -->
<!-- deps: Run detail web page and route -->
<!-- repo: forge-flow -->
<!-- touches: internal/forge/run_detail_retry.go, internal/forge/board_web.go -->
<!-- conflict-risk: low -->

**Done when:**
- New handler `HandleRetryTask(runID, taskName)` triggers a fresh attempt for the task.
- Retry button on failed task rows in the run detail page.
- Updates the spec task status back to `todo` and spawns an agent if watch is running.

**Validation:**
- Covers: Scenario "Operator retries a failed task from the run detail"
