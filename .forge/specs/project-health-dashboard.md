# Project health dashboard — roadmap-driven milestone view
<!-- status: todo -->
<!-- priority: high -->
<!-- created: 2026-04-11 -->
<!-- branch: feat/dashboard -->

**What exists:** The board shows real-time attention and active work but never zooms out. The operator can't answer "what milestone are we in? what's the next deliverable? how far from shipping it?"

**What's missing:** A dashboard that reads the roadmap KB doc, parses its sections (Now / Next / Later), correlates each bullet with spec statuses, and asks Claude "based on this roadmap and these specs, where are we and what ships next?" The whole view is organized around milestones, not generic KPIs.

**Demo:** Operator clicks "Dashboard" in the header. Sees three milestone cards (Now, Next, Later) stacked vertically. Each card shows the roadmap bullets with done/pending marks and a progress bar per section. Below the cards, a "Claude analysis" section with a "Run analysis" button. Clicks Run. 15 seconds later, Claude's narrative appears: "**Current milestone:** Now. **Progress:** 50% (1 of 2 shipped — Agentic board surface done; Meta-forge still active). **Blockers:** None. **Ships next:** Inbox actions (first spec in Next, scaffolding task ready)."

## Expected behavior
- `/dashboard` route renders a page organized around the roadmap.
- Parses `kb/roadmap.md` for `## Now`, `## Next`, `## Later` sections and their `- bullet` items.
- For each section, renders a card: title, items with done/pending state, progress bar.
- Done detection: bullet contains `(done ...)` substring OR the bullet text fuzzy-matches a spec whose status is `done`.
- Progress per section = done items / total items.
- Claude analysis card below the milestones with "Run analysis" button.
- Prompt sent to Claude: full roadmap text + spec summary (title + status + done/total tasks) + top inbox items.
- Analysis asks Claude to identify current milestone, progress, blockers, and the single most important next ship.
- Response cached at `.forge/runtime/analysis.json` with timestamp.
- Cache is displayed on page load; button triggers a fresh run.
- If roadmap.md is missing or has no sections, the page shows a helpful empty state telling the operator to write a roadmap.
- Nav link "Dashboard" added to the header next to the project picker.

## Test cases

### Scenario: Operator opens dashboard with a populated roadmap
Given the project has a kb/roadmap.md with Now / Next / Later sections and specs in various states
When the operator navigates to /dashboard
Then each milestone card renders with its bullets and progress bar
And the current milestone (first non-empty section) is highlighted

### Scenario: Operator runs an analysis
Given claude CLI is available
When the operator clicks Run on the dashboard
Then the server shells out to claude -p with the roadmap + spec state
And the response is persisted to .forge/runtime/analysis.json
And the page shows the rendered markdown

### Scenario: Roadmap has no sections
Given kb/roadmap.md is empty or missing section headings
When the operator opens the dashboard
Then an empty state prompts the operator to add Now / Next / Later sections

### Scenario: Operator reloads after a prior analysis
Given a cached analysis exists at .forge/runtime/analysis.json
When the operator opens the dashboard
Then the cached analysis is rendered with its timestamp without re-running Claude

### Scenario: Claude CLI is not installed
Given claude is not on PATH
When the operator clicks Run
Then the dashboard shows "Claude CLI not found" and the cached analysis (if any) remains visible

## Validation plan
- Unit tests for `parseRoadmapMilestones(content string)` returning sections + bullets.
- Unit tests for `matchBulletsToSpecs(bullets, specs)` done detection logic.
- Unit tests for the dashboard view aggregation with a fake workspace.
- Test for claude exec using a shellRun mock.
- Manual smoke: run on forge-flow project, verify milestones render and analysis runs.

### Task: Roadmap parser and milestone aggregator
<!-- status: todo -->
<!-- parallelizable: no -->
<!-- deps: none -->
<!-- repo: forge-flow -->
<!-- touches: internal/forge/dashboard.go, internal/forge/dashboard_test.go -->
<!-- conflict-risk: low -->

**Done when:**
- New file `internal/forge/dashboard.go` with:
  - `Milestone` type with Name, Bullets ([]MilestoneItem), DonePct, Current bool
  - `MilestoneItem` type with Text, Done bool, MatchedSpec string
  - `parseRoadmapMilestones(content)` returns `[]Milestone` from `## Now|Next|Later` sections
  - `matchBulletsToSpecs(bullets, specs)` marks bullets as done via substring/fuzzy match or `(done ...)` token
  - `Service.Dashboard(cwd)` aggregates roadmap + specs + inbox into `DashboardView`
  - `DashboardView` type with Milestones, CachedAnalysis, ProjectName
- First non-empty milestone marked as Current
- Tests cover parsing, matching, and aggregation

**Validation:**
- Covers: Scenario "Operator opens dashboard with a populated roadmap"
- Covers: Scenario "Roadmap has no sections"

### Task: Claude analysis runner and cache
<!-- status: todo -->
<!-- parallelizable: no -->
<!-- deps: Roadmap parser and milestone aggregator -->
<!-- repo: forge-flow -->
<!-- touches: internal/forge/dashboard_analysis.go, internal/forge/dashboard_analysis_test.go -->
<!-- conflict-risk: low -->

**Done when:**
- New file `internal/forge/dashboard_analysis.go` with:
  - `ProjectAnalysis` type (Markdown, GeneratedAt, Model)
  - `Service.RunProjectAnalysis(cwd)` composes prompt from roadmap + spec states + inbox, shells out to `claude -p`, persists to `.forge/runtime/analysis.json`
  - `Service.LoadCachedAnalysis(cwd)` reads the cache
  - Prompt is milestone-focused: "Based on this roadmap and these specs, identify current milestone, progress, blockers, and next ship"
- Uses existing `shellRun` for testability
- Returns a helpful error if claude CLI is missing

**Validation:**
- Covers: Scenario "Operator runs an analysis"
- Covers: Scenario "Claude CLI is not installed"

### Task: Dashboard web route UI and nav link
<!-- status: todo -->
<!-- parallelizable: no -->
<!-- deps: Claude analysis runner and cache -->
<!-- repo: forge-flow -->
<!-- touches: internal/forge/board_web.go -->
<!-- conflict-risk: medium -->

**Done when:**
- Route `/dashboard` renders an HTML page with:
  - Header (same as board) + nav link highlighted
  - Milestone cards (Now/Next/Later) with progress bars and item lists
  - "Claude analysis" card with Run button + rendered markdown (via marked.js from CDN)
  - Empty state if roadmap has no sections
- `POST /api/analyze` triggers `RunProjectAnalysis` and returns the result as JSON
- `GET /api/dashboard` returns `DashboardView` as JSON
- Header nav updated: Board | Dashboard links, active highlighted by URL path
- Loading spinner while analysis runs; error banner if exec fails

**Validation:**
- Covers: Scenario "Operator reloads after a prior analysis"
