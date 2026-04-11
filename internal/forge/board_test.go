package forge

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestBuildBoardSpecLaneGrouping(t *testing.T) {
	plan := RunPlan{
		ProjectID: "test", ProjectName: "Test", LintValid: true,
		// Spec 1: has ready tasks → lane = Ready
		Safe: []RunPlanTask{
			{Task: "Task A", File: "spec1.md", SpecTitle: "Spec Ready", Repo: "core", Status: "todo", Parallelizable: "yes"},
		},
		// Spec 2: has active tasks → lane = In Progress
		Active: []RunPlanTask{
			{Task: "Task B", File: "spec2.md", SpecTitle: "Spec Active", Repo: "core", Status: "in_progress"},
		},
		// Spec 3: all done → lane = Done
		Done: []RunPlanTask{
			{Task: "Task C", File: "spec3.md", SpecTitle: "Spec Done", Repo: "core", Status: "done"},
			{Task: "Task D", File: "spec3.md", SpecTitle: "Spec Done", Repo: "core", Status: "done"},
		},
		// Spec 4: all blocked → lane = Blocked
		Blocked: []RunPlanTask{
			{Task: "Task E", File: "spec4.md", SpecTitle: "Spec Blocked", Repo: "core", Status: "todo", Reasons: []string{"waiting on: X"}},
		},
	}

	board := buildBoard(plan, nil)

	laneSpecs := map[string]int{}
	for _, lane := range board.Lanes {
		laneSpecs[lane.Name] = lane.Count
	}

	if laneSpecs["Ready"] != 1 {
		t.Fatalf("Ready: expected 1 spec, got %d", laneSpecs["Ready"])
	}
	if laneSpecs["In Progress"] != 1 {
		t.Fatalf("In Progress: expected 1 spec, got %d", laneSpecs["In Progress"])
	}
	if laneSpecs["Done"] != 1 {
		t.Fatalf("Done: expected 1 spec, got %d", laneSpecs["Done"])
	}
	if laneSpecs["Blocked"] != 1 {
		t.Fatalf("Blocked: expected 1 spec, got %d", laneSpecs["Blocked"])
	}
}

func TestBuildBoardSpecProgress(t *testing.T) {
	plan := RunPlan{
		ProjectID: "test", ProjectName: "Test", LintValid: true,
		Safe: []RunPlanTask{
			{Task: "Remaining", File: "spec.md", SpecTitle: "My Spec", Status: "todo"},
		},
		Done: []RunPlanTask{
			{Task: "Done 1", File: "spec.md", SpecTitle: "My Spec", Status: "done"},
			{Task: "Done 2", File: "spec.md", SpecTitle: "My Spec", Status: "done"},
		},
	}

	board := buildBoard(plan, nil)

	// Find the spec in Ready lane
	var spec *BoardSpec
	for _, lane := range board.Lanes {
		for i := range lane.Specs {
			if lane.Specs[i].Title == "My Spec" {
				spec = &lane.Specs[i]
			}
		}
	}
	if spec == nil {
		t.Fatal("expected to find My Spec")
	}
	if spec.TotalTasks != 3 {
		t.Fatalf("expected 3 total tasks, got %d", spec.TotalTasks)
	}
	if spec.DoneTasks != 2 {
		t.Fatalf("expected 2 done tasks, got %d", spec.DoneTasks)
	}
	if spec.ProgressPct != 67 {
		t.Fatalf("expected 67%% progress, got %.0f%%", spec.ProgressPct)
	}
}

func TestBuildBoardIncludesRunHistory(t *testing.T) {
	plan := RunPlan{ProjectID: "test", ProjectName: "Test", LintValid: true}
	runs := []RunRecord{
		{
			RunID: "run-20260409-120000", Status: RunStatusSucceeded,
			StartedAt: "2026-04-09T12:00:00Z",
			Summary:   RunSummary{Total: 5, Succeeded: 3, Skipped: 2},
		},
	}

	board := buildBoard(plan, runs)
	if board.LastRun == nil {
		t.Fatal("expected last run info")
	}
	if board.LastRun.RunID != "run-20260409-120000" {
		t.Fatalf("expected run ID, got %s", board.LastRun.RunID)
	}
}

func TestBuildBoardNoRunHistory(t *testing.T) {
	plan := RunPlan{ProjectID: "test", ProjectName: "Test", LintValid: true}
	board := buildBoard(plan, nil)
	if board.LastRun != nil {
		t.Fatal("expected no last run info")
	}
}

func TestSpecLaneDetermination(t *testing.T) {
	tests := []struct {
		name     string
		spec     BoardSpec
		wantLane string
	}{
		{"all done", BoardSpec{TotalTasks: 3, DoneTasks: 3}, "Done"},
		{"has active", BoardSpec{TotalTasks: 3, DoneTasks: 1, ActiveTasks: 1, ReadyTasks: 1, Tasks: []BoardTask{{Bucket: "active", Status: "in_progress"}}}, "In Progress"},
		{"has ready", BoardSpec{TotalTasks: 3, DoneTasks: 1, ReadyTasks: 2}, "Ready"},
		{"all blocked", BoardSpec{TotalTasks: 2, BlockedTasks: 2}, "Blocked"},
		{"all in review", BoardSpec{TotalTasks: 2, ActiveTasks: 2, Tasks: []BoardTask{{Bucket: "active", Status: "in_review"}, {Bucket: "active", Status: "in_review"}}}, "In Review"},
	}

	for _, tt := range tests {
		got := specLane(tt.spec)
		if got != tt.wantLane {
			t.Errorf("%s: expected lane %q, got %q", tt.name, tt.wantLane, got)
		}
	}
}

func TestFormatBoardTerminal(t *testing.T) {
	board := Board{
		ProjectName: "Forge",
		Lanes: []BoardLane{
			{Name: "Ready", Count: 1, Specs: []BoardSpec{
				{Title: "User can login", TotalTasks: 3, DoneTasks: 1, ProgressPct: 33},
			}},
			{Name: "Done", Count: 1, Specs: []BoardSpec{
				{Title: "User can register", TotalTasks: 2, DoneTasks: 2, ProgressPct: 100},
			}},
		},
		Summary: BoardSummary{TotalSpecs: 2, TotalTasks: 5, Done: 3, ProgressPct: 60},
	}

	output := FormatBoard(board)
	if !strings.Contains(output, "Forge Board") {
		t.Fatal("expected header")
	}
	if !strings.Contains(output, "User can login") {
		t.Fatal("expected spec title")
	}
	if !strings.Contains(output, "1/3 tasks") {
		t.Fatal("expected task count")
	}
	if !strings.Contains(output, "60% done") {
		t.Fatal("expected progress")
	}
}

func TestServiceBoardIntegration(t *testing.T) {
	fixedTime := time.Date(2026, 4, 9, 20, 0, 0, 0, time.UTC)
	origTimeNow := timeNow
	timeNow = func() time.Time { return fixedTime }
	defer func() { timeNow = origTimeNow }()

	svc, webRepo := setupRunTestProject(t)

	_, err := svc.ExecuteRun(webRepo, "")
	if err != nil {
		t.Fatalf("ExecuteRun: %v", err)
	}

	board, err := svc.Board(webRepo)
	if err != nil {
		t.Fatalf("Board: %v", err)
	}

	if board.ProjectName == "" {
		t.Fatal("expected project name")
	}
	if board.Summary.TotalSpecs == 0 {
		t.Fatal("expected specs on board")
	}
	if len(board.Lanes) != 5 {
		t.Fatalf("expected 5 lanes, got %d", len(board.Lanes))
	}
	if board.LastRun == nil {
		t.Fatal("expected last run info")
	}

	output := FormatBoard(board)
	if output == "" {
		t.Fatal("expected non-empty formatted output")
	}
}

func TestServiceBoardEmptyProject(t *testing.T) {
	home := t.TempDir()
	svc := &Service{ForgeHome: filepath.Join(home, ".forge")}
	if _, err := svc.InitWorkspace("main", "Main", ""); err != nil {
		t.Fatalf("InitWorkspace: %v", err)
	}
	if _, err := svc.InitProject("main", "empty", "Empty"); err != nil {
		t.Fatalf("InitProject: %v", err)
	}
	repo := filepath.Join(home, "code", "empty")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.LinkRepo("main", "empty", "core", "empty", "core", "", repo); err != nil {
		t.Fatalf("LinkRepo: %v", err)
	}

	board, err := svc.Board(repo)
	if err != nil {
		t.Fatalf("Board: %v", err)
	}
	if board.Summary.TotalTasks != 0 {
		t.Fatalf("expected 0 tasks, got %d", board.Summary.TotalTasks)
	}
}
