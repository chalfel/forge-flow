# Multi-project board — project picker in header
<!-- status: done -->
<!-- priority: high -->
<!-- created: 2026-04-11 -->
<!-- branch: feat/multi-project-board -->

**What exists:** The board web is pinned to whatever `cwd` is passed to `forge board --web` at startup. All endpoints resolve context from that single cwd. No way to switch projects without restarting the server.

**What's missing:** A project picker in the header that lists all projects across all workspaces. Clicking a project switches the active board without restart. The selection persists via cookie.

**Demo:** Operator runs `forge board --web` from any repo. Board opens showing the current project. Operator clicks the project name in the header → dropdown appears with every project in every workspace → clicks another project → board reloads showing that project's specs, inbox, feed. Cookie keeps the choice across page reloads.

## Expected behavior
- Header "project name" becomes a clickable dropdown button.
- Dropdown lists all projects across all workspaces, grouped by workspace.
- Clicking a project sets the `forge_project` cookie (value: `{workspace}/{projectId}`) and reloads.
- All board endpoints read the cookie on each request, resolve the project's repo path, and use that as the effective cwd.
- If the cookie is missing or invalid, fall back to the startup cwd.
- `GET /api/projects` returns JSON list of all projects for the dropdown.
- Board gracefully handles projects with no linked repos (shows "no repos linked" state).

## Test cases

### Scenario: Operator switches to another project
Given two projects exist in the main workspace
When the operator opens the board and picks the second project from the dropdown
Then the board reloads showing the second project's specs
And the cookie forge_project is set to the second project

### Scenario: Operator reloads with an existing selection
Given the forge_project cookie is set to a valid project
When the operator opens the board
Then the board shows that project without needing the dropdown

### Scenario: Invalid cookie falls back to cwd
Given the forge_project cookie points to a nonexistent project
When the operator opens the board
Then the board falls back to the startup cwd's project

## Validation plan
- Unit test for `Service.ListAllProjects()` with a tmpdir workspace containing multiple projects.
- Unit test for `Service.CwdForProject(workspace, projectID)` returning the first linked repo path.
- HTTP integration test: set cookie → hit `/api/board` → assert project in response.
- Manual smoke: create two projects, switch between them in the UI.

### Task: Backend project listing and cwd resolution
<!-- status: done -->
<!-- parallelizable: no -->
<!-- deps: none -->
<!-- repo: forge-flow -->
<!-- touches: internal/forge/projects_list.go -->
<!-- conflict-risk: low -->

**Done when:**
- New file `internal/forge/projects_list.go` with `Service.ListAllProjects() ([]ProjectSummary, error)` that iterates all workspaces.
- `Service.CwdForProject(workspaceID, projectID string) (string, error)` that returns the first linked repo path (or the project root if no repos linked).
- Returns stable sort order (workspace asc, project asc).

**Validation:**
- Covers: Scenario "Operator switches to another project"

### Task: Project picker middleware and endpoints
<!-- status: done -->
<!-- parallelizable: no -->
<!-- deps: Backend project listing and cwd resolution -->
<!-- repo: forge-flow -->
<!-- touches: internal/forge/board_web.go -->
<!-- conflict-risk: medium -->

**Done when:**
- `GET /api/projects` returns all projects as JSON.
- Middleware reads the `forge_project` cookie on every board request and resolves the effective cwd.
- Invalid or missing cookie → fall back to the startup cwd.
- `POST /project/select` accepts `{workspace, projectId}`, sets the cookie, and returns 204.
- Header dropdown in the board template with project list and click handler that posts to `/project/select` and reloads.

**Validation:**
- Covers: Scenario "Operator reloads with an existing selection"
- Covers: Scenario "Invalid cookie falls back to cwd"
