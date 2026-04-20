package forge

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseRoadmapMilestonesExtractsSections(t *testing.T) {
	content := `# Roadmap

## Now (shipping)
- Agentic board surface (done 2026-04-11)
- Meta-forge (in progress)

## Next
- Inbox actions
- Agent tmux join

## Later
- Native app (Tauri)

## Vision
Not a milestone section.
`
	milestones := parseRoadmapMilestones(content)

	if len(milestones) != 3 {
		t.Fatalf("expected 3 milestones, got %d", len(milestones))
	}
	names := []string{"Now", "Next", "Later"}
	for i, want := range names {
		if milestones[i].Name != want {
			t.Errorf("position %d: expected %s, got %s", i, want, milestones[i].Name)
		}
	}

	// Now has 2 bullets, first should be marked done from "(done ...)" token.
	if len(milestones[0].Bullets) != 2 {
		t.Fatalf("Now section: expected 2 bullets, got %d", len(milestones[0].Bullets))
	}
	if !milestones[0].Bullets[0].Done {
		t.Error("expected first Now bullet to be marked done from (done ...) token")
	}
	if milestones[0].Bullets[1].Done {
		t.Error("expected second Now bullet to be pending")
	}

	// Next has 2 bullets, none should be done.
	if len(milestones[1].Bullets) != 2 {
		t.Fatalf("Next section: expected 2 bullets, got %d", len(milestones[1].Bullets))
	}
}

func TestParseRoadmapMilestonesEmpty(t *testing.T) {
	milestones := parseRoadmapMilestones("")
	if len(milestones) != 0 {
		t.Errorf("expected no milestones from empty content, got %d", len(milestones))
	}
}

func TestParseRoadmapMilestonesNoSections(t *testing.T) {
	content := "# Roadmap\n\nJust some paragraphs, no section headings.\n"
	milestones := parseRoadmapMilestones(content)
	if len(milestones) != 0 {
		t.Errorf("expected no milestones when no section headings, got %d", len(milestones))
	}
}

func TestMatchBulletsToSpecs(t *testing.T) {
	milestones := []Milestone{
		{
			Name: "Now",
			Bullets: []MilestoneItem{
				{Text: "Agentic board surface"},
				{Text: "Meta-forge (in progress)"},
			},
		},
	}
	specs := []SpecSummary{
		{File: "board-agentic-surface.md", Title: "Board agentic surface", Status: "done", DoneTasks: 5, TotalTasks: 5},
		{File: "other.md", Title: "Unrelated spec", Status: "todo", DoneTasks: 0, TotalTasks: 3},
	}

	matchBulletsToSpecs(milestones, specs)

	first := milestones[0].Bullets[0]
	if first.MatchedSpec != "board-agentic-surface.md" {
		t.Errorf("expected match to board-agentic-surface.md, got %q", first.MatchedSpec)
	}
	if !first.Done {
		t.Error("expected bullet to be done since the matched spec is done")
	}
	if first.SpecProgress != 100 {
		t.Errorf("expected 100%% progress, got %d", first.SpecProgress)
	}

	// Second bullet should not match anything meaningful.
	second := milestones[0].Bullets[1]
	if second.MatchedSpec != "" {
		t.Errorf("did not expect a match for %q, got %s", second.Text, second.MatchedSpec)
	}
}

func TestComputeMilestoneProgress(t *testing.T) {
	milestones := []Milestone{
		{
			Name: "Now",
			Bullets: []MilestoneItem{
				{Text: "a", Done: true},
				{Text: "b", Done: true},
				{Text: "c", Done: false},
			},
		},
	}
	computeMilestoneProgress(milestones)
	if milestones[0].Total != 3 {
		t.Errorf("expected Total=3, got %d", milestones[0].Total)
	}
	if milestones[0].DoneCount != 2 {
		t.Errorf("expected DoneCount=2, got %d", milestones[0].DoneCount)
	}
	if milestones[0].DonePct != 66 {
		t.Errorf("expected DonePct=66, got %d", milestones[0].DonePct)
	}
}

func TestMarkCurrentMilestoneFirstIncomplete(t *testing.T) {
	milestones := []Milestone{
		{Name: "Now", Bullets: []MilestoneItem{{Done: true}}, DonePct: 100},
		{Name: "Next", Bullets: []MilestoneItem{{Done: false}}, DonePct: 0},
		{Name: "Later", Bullets: []MilestoneItem{{Done: false}}, DonePct: 0},
	}
	markCurrentMilestone(milestones)

	if milestones[0].Current {
		t.Error("Now should not be current when fully done")
	}
	if !milestones[1].Current {
		t.Error("Next should be current (first incomplete)")
	}
	if milestones[2].Current {
		t.Error("Later should not be current when Next is")
	}
}

func TestMarkCurrentMilestoneAllDone(t *testing.T) {
	milestones := []Milestone{
		{Name: "Now", Bullets: []MilestoneItem{{Done: true}}, DonePct: 100},
		{Name: "Next", Bullets: []MilestoneItem{{Done: true}}, DonePct: 100},
	}
	markCurrentMilestone(milestones)

	// When everything is done, last one is marked current.
	if !milestones[1].Current {
		t.Error("last milestone should be current when all are complete")
	}
}

func TestComposeAnalysisPromptIncludesKeyContext(t *testing.T) {
	view := DashboardView{
		ProjectName: "Forge Flow",
		SpecSummaries: []SpecSummary{
			{Title: "Board agentic surface", Status: "done", DoneTasks: 5, TotalTasks: 5},
			{Title: "Inbox actions", Status: "todo", DoneTasks: 0, TotalTasks: 4},
		},
		Milestones: []Milestone{
			{
				Name: "Now", DoneCount: 1, Total: 2,
				Bullets: []MilestoneItem{
					{Text: "Agentic board surface", Done: true, MatchedSpec: "board-agentic-surface.md", SpecProgress: 100},
					{Text: "Meta-forge", Done: false},
				},
			},
		},
	}
	roadmap := "## Now\n- Agentic board surface\n- Meta-forge\n"
	board := Board{
		Inbox: []InboxItem{
			{Type: "blocked", Urgency: "medium", Title: "Task blocked", Detail: "waiting on scaffolding"},
		},
	}

	prompt := composeAnalysisPrompt(view, roadmap, board)

	mustContain := []string{
		"Forge Flow",
		"ROADMAP MILESTONE PROGRESS",
		"Board agentic surface",
		"Inbox actions",
		"## Now",
		"Task blocked",
		"Current milestone:",
		"Ships next:",
	}
	for _, s := range mustContain {
		if !strings.Contains(prompt, s) {
			t.Errorf("prompt missing expected content %q", s)
		}
	}
}

func TestLoadCachedAnalysisMissingReturnsNil(t *testing.T) {
	svc, webRepo := setupDashboardTestProject(t)
	cached, err := svc.LoadCachedAnalysis(webRepo)
	if err != nil {
		t.Fatalf("LoadCachedAnalysis: %v", err)
	}
	if cached != nil {
		t.Errorf("expected nil cache, got %+v", cached)
	}
}

func TestRunProjectAnalysisWithMockedClaude(t *testing.T) {
	origRun := shellRun
	shellRun = func(name string, args ...string) ([]byte, error) {
		if name != "claude" {
			t.Errorf("expected claude, got %s", name)
		}
		if len(args) < 2 || args[0] != "-p" {
			t.Errorf("expected -p prompt, got %v", args)
		}
		// Assert the prompt contains expected anchors.
		if !strings.Contains(args[1], "ROADMAP MILESTONE PROGRESS") {
			t.Errorf("prompt missing milestone anchor")
		}
		return []byte("**Current milestone:** Now\n\nProgress solid."), nil
	}
	defer func() { shellRun = origRun }()

	// Bypass EnsureClaudeAvailable by setting shellRun — but exec.LookPath still
	// runs. Skip the test if claude is not on PATH in this environment.
	if err := EnsureClaudeAvailable(); err != nil {
		t.Skip("claude CLI not available in test environment")
	}

	svc, webRepo := setupDashboardTestProject(t)
	analysis, err := svc.RunProjectAnalysis(webRepo)
	if err != nil {
		t.Fatalf("RunProjectAnalysis: %v", err)
	}
	if !strings.Contains(analysis.Markdown, "Current milestone") {
		t.Errorf("unexpected markdown: %q", analysis.Markdown)
	}

	// Cache should exist.
	cached, err := svc.LoadCachedAnalysis(webRepo)
	if err != nil {
		t.Fatalf("LoadCachedAnalysis: %v", err)
	}
	if cached == nil || cached.Markdown != analysis.Markdown {
		t.Error("cached analysis does not match returned analysis")
	}
}

func TestDashboardLoadsRoadmapAndSpecs(t *testing.T) {
	svc, webRepo := setupDashboardTestProject(t)

	view, err := svc.Dashboard(webRepo)
	if err != nil {
		t.Fatalf("Dashboard: %v", err)
	}
	if view.ProjectName == "" {
		t.Error("expected project name")
	}
	if !view.HasRoadmap {
		t.Error("expected HasRoadmap true")
	}
	if len(view.Milestones) == 0 {
		t.Fatal("expected milestones parsed from roadmap")
	}
	// Our setup adds a Now section with one bullet.
	if view.Milestones[0].Name != "Now" {
		t.Errorf("expected first milestone Now, got %s", view.Milestones[0].Name)
	}
}

// setupDashboardTestProject creates a workspace with a project that has a
// roadmap.md and one spec, suitable for dashboard tests.
func setupDashboardTestProject(t *testing.T) (*Service, string) {
	t.Helper()
	home := t.TempDir()
	svc := &Service{ForgeHome: filepath.Join(home, ".forge")}
	if _, err := svc.InitWorkspace("main", "Main", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.InitProject("main", "dash-test", "Dashboard Test"); err != nil {
		t.Fatal(err)
	}
	repo := filepath.Join(home, "code", "dash-test")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.LinkRepo("main", "dash-test", "core", "dash-test", "core", "", repo); err != nil {
		t.Fatal(err)
	}

	ctx, err := svc.ResolveContext(repo)
	if err != nil {
		t.Fatal(err)
	}

	// Write a minimal roadmap.
	roadmap := "# Roadmap\n\n## Now\n- Ship the demo\n\n## Next\n- Scale up\n"
	if err := os.WriteFile(filepath.Join(ctx.KBDir, "roadmap.md"), []byte(roadmap), 0o644); err != nil {
		t.Fatal(err)
	}

	// Write a minimal spec.
	spec := `# Ship the demo
<!-- status: in_progress -->

**Demo:** Operator sees a working demo.

## Expected behavior
- Demo runs.

## Test cases

### Scenario: Demo works
Given the app runs
When operator opens it
Then it works

## Validation plan
- Manual smoke test.

### Task: Build the demo
<!-- status: done -->
<!-- parallelizable: no -->
<!-- deps: none -->
<!-- repo: core -->
<!-- touches: src/** -->

**Done when:**
- Demo runs.

**Validation:**
- Covers: Scenario "Demo works"
`
	if err := os.WriteFile(filepath.Join(ctx.SpecsDir, "ship-the-demo.md"), []byte(spec), 0o644); err != nil {
		t.Fatal(err)
	}

	return svc, repo
}
