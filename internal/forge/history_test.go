package forge

import (
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestAppendAttemptAutoIncrementsNumber(t *testing.T) {
	fixedTime := time.Date(2026, 4, 10, 12, 0, 0, 0, time.UTC)
	origTimeNow := timeNow
	timeNow = func() time.Time { return fixedTime }
	defer func() { timeNow = origTimeNow }()

	svc, webRepo := setupRunTestProject(t)

	// First attempt — no attempt number passed, should be 1
	err := svc.AppendAttempt(webRepo, TaskAttemptRecord{
		RunID:    "run-1",
		SpecFile: "spec1.md",
		TaskName: "Build thing",
		Status:   "running",
	})
	if err != nil {
		t.Fatalf("AppendAttempt 1: %v", err)
	}

	// Second attempt — should be 2
	err = svc.AppendAttempt(webRepo, TaskAttemptRecord{
		RunID:    "run-2",
		SpecFile: "spec1.md",
		TaskName: "Build thing",
		Status:   "running",
	})
	if err != nil {
		t.Fatalf("AppendAttempt 2: %v", err)
	}

	records, err := svc.LoadAttempts(webRepo, "spec1.md", "Build thing")
	if err != nil {
		t.Fatalf("LoadAttempts: %v", err)
	}
	if len(records) != 2 {
		t.Fatalf("expected 2 attempts, got %d", len(records))
	}
	if records[0].Attempt != 1 {
		t.Fatalf("expected first attempt=1, got %d", records[0].Attempt)
	}
	if records[1].Attempt != 2 {
		t.Fatalf("expected second attempt=2, got %d", records[1].Attempt)
	}
}

func TestAppendAttemptSetsDefaults(t *testing.T) {
	fixedTime := time.Date(2026, 4, 10, 14, 30, 0, 0, time.UTC)
	origTimeNow := timeNow
	timeNow = func() time.Time { return fixedTime }
	defer func() { timeNow = origTimeNow }()

	svc, webRepo := setupRunTestProject(t)

	err := svc.AppendAttempt(webRepo, TaskAttemptRecord{
		RunID:    "run-1",
		SpecFile: "spec1.md",
		TaskName: "My task",
		Status:   "running",
	})
	if err != nil {
		t.Fatalf("AppendAttempt: %v", err)
	}

	records, _ := svc.LoadAttempts(webRepo, "spec1.md", "My task")
	if len(records) != 1 {
		t.Fatalf("expected 1 attempt, got %d", len(records))
	}
	if records[0].Agent != "claude" {
		t.Fatalf("expected default agent=claude, got %s", records[0].Agent)
	}
	if records[0].StartedAt == "" {
		t.Fatal("expected StartedAt to be set")
	}
}

func TestUpdateLatestAttemptSetsEndedAt(t *testing.T) {
	callCount := 0
	times := []time.Time{
		time.Date(2026, 4, 10, 10, 0, 0, 0, time.UTC),
		time.Date(2026, 4, 10, 10, 5, 0, 0, time.UTC),
	}
	origTimeNow := timeNow
	timeNow = func() time.Time {
		t := times[callCount]
		if callCount < len(times)-1 {
			callCount++
		}
		return t
	}
	defer func() { timeNow = origTimeNow }()

	svc, webRepo := setupRunTestProject(t)

	_ = svc.AppendAttempt(webRepo, TaskAttemptRecord{
		RunID: "run-1", SpecFile: "spec1.md", TaskName: "T", Status: "running",
	})

	err := svc.UpdateLatestAttempt(webRepo, "spec1.md", "T", func(r *TaskAttemptRecord) {
		r.Status = "succeeded"
		r.PR = "https://github.com/test/repo/pull/1"
	})
	if err != nil {
		t.Fatalf("UpdateLatestAttempt: %v", err)
	}

	records, _ := svc.LoadAttempts(webRepo, "spec1.md", "T")
	if len(records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(records))
	}
	if records[0].Status != "succeeded" {
		t.Fatalf("expected status=succeeded, got %s", records[0].Status)
	}
	if records[0].PR == "" {
		t.Fatal("expected PR to be set")
	}
	if records[0].EndedAt == "" {
		t.Fatal("expected EndedAt to be auto-set")
	}
}

func TestUpdateLatestAttemptPreservesHistory(t *testing.T) {
	svc, webRepo := setupRunTestProject(t)

	// Append 3 attempts
	for i := 1; i <= 3; i++ {
		_ = svc.AppendAttempt(webRepo, TaskAttemptRecord{
			RunID: "run-1", SpecFile: "spec.md", TaskName: "Task", Status: "running",
		})
	}

	// Update the latest (3rd) only
	_ = svc.UpdateLatestAttempt(webRepo, "spec.md", "Task", func(r *TaskAttemptRecord) {
		r.Status = "done"
	})

	records, _ := svc.LoadAttempts(webRepo, "spec.md", "Task")
	if len(records) != 3 {
		t.Fatalf("expected 3 records preserved, got %d", len(records))
	}
	if records[0].Status != "running" || records[1].Status != "running" {
		t.Fatal("first two attempts should be untouched")
	}
	if records[2].Status != "done" {
		t.Fatalf("latest should be done, got %s", records[2].Status)
	}
}

func TestLoadAttemptsMissingFileReturnsNil(t *testing.T) {
	svc, webRepo := setupRunTestProject(t)

	records, err := svc.LoadAttempts(webRepo, "nonexistent.md", "No task")
	if err != nil {
		t.Fatalf("should not error on missing file: %v", err)
	}
	if len(records) != 0 {
		t.Fatalf("expected empty, got %d", len(records))
	}
}

func TestWriteFeedbackAndLoad(t *testing.T) {
	svc, webRepo := setupRunTestProject(t)

	fb := StructuredFeedback{
		Status:   "changes_requested",
		Reviewer: "reviewer-agent",
		Items: []FeedbackItem{
			{Kind: "bug", Text: "Missing error handling", File: "main.go", Line: 42},
			{Kind: "style", Text: "Use existing helper"},
		},
		OriginalMarkdown: "**Review feedback:**\n- Missing error handling\n- Use existing helper",
	}

	err := svc.WriteFeedback(webRepo, "spec.md", "My task", fb)
	if err != nil {
		t.Fatalf("WriteFeedback: %v", err)
	}

	loaded, err := svc.LoadFeedback(webRepo, "spec.md", "My task")
	if err != nil {
		t.Fatalf("LoadFeedback: %v", err)
	}
	if loaded == nil {
		t.Fatal("expected feedback to be loaded")
	}
	if loaded.Status != "changes_requested" {
		t.Fatalf("expected status=changes_requested, got %s", loaded.Status)
	}
	if len(loaded.Items) != 2 {
		t.Fatalf("expected 2 items, got %d", len(loaded.Items))
	}
	if loaded.Items[0].Kind != "bug" {
		t.Fatalf("expected first item kind=bug, got %s", loaded.Items[0].Kind)
	}
	if loaded.Items[0].File != "main.go" {
		t.Fatalf("expected file=main.go, got %s", loaded.Items[0].File)
	}
	if loaded.ReviewedAt == "" {
		t.Fatal("expected ReviewedAt to be auto-set")
	}
}

func TestLoadFeedbackMissingReturnsNil(t *testing.T) {
	svc, webRepo := setupRunTestProject(t)

	fb, err := svc.LoadFeedback(webRepo, "nonexistent.md", "No task")
	if err != nil {
		t.Fatalf("should not error: %v", err)
	}
	if fb != nil {
		t.Fatal("expected nil for missing file")
	}
}

func TestListTaskHistoryEmpty(t *testing.T) {
	svc, webRepo := setupRunTestProject(t)

	result, err := svc.ListTaskHistory(webRepo)
	if err != nil {
		t.Fatalf("ListTaskHistory: %v", err)
	}
	if len(result) != 0 {
		t.Fatalf("expected empty, got %d specs", len(result))
	}
}

func TestListTaskHistoryWithEntries(t *testing.T) {
	svc, webRepo := setupRunTestProject(t)

	_ = svc.AppendAttempt(webRepo, TaskAttemptRecord{
		RunID: "r1", SpecFile: "spec1.md", TaskName: "Task A", Status: "running",
	})
	_ = svc.AppendAttempt(webRepo, TaskAttemptRecord{
		RunID: "r1", SpecFile: "spec1.md", TaskName: "Task B", Status: "running",
	})
	_ = svc.AppendAttempt(webRepo, TaskAttemptRecord{
		RunID: "r2", SpecFile: "spec2.md", TaskName: "Task C", Status: "running",
	})

	result, err := svc.ListTaskHistory(webRepo)
	if err != nil {
		t.Fatalf("ListTaskHistory: %v", err)
	}

	// Should have 2 spec directories
	if len(result) != 2 {
		t.Fatalf("expected 2 specs in history, got %d", len(result))
	}

	// spec1 should have 2 tasks
	spec1Slug := slugifySpecFile("spec1.md")
	if len(result[spec1Slug]) != 2 {
		t.Fatalf("expected 2 tasks for spec1, got %d", len(result[spec1Slug]))
	}
}

func TestSlugifySpecFile(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"2026-04-10-user-can-login.md", "2026-04-10-user-can-login"},
		{"path/to/spec-file.md", "spec-file"},
		{"SPEC.MD", "spec-md"},
	}
	for _, tt := range tests {
		got := slugifySpecFile(tt.input)
		if got != tt.want {
			t.Errorf("slugifySpecFile(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestAttemptsPathStructure(t *testing.T) {
	ctx := ResolvedContext{
		RepoLocalForge: "/repo/.forge",
	}
	path := attemptsPath(ctx, "2026-04-10-foo.md", "Do the Thing!")
	expected := filepath.Join("/repo/.forge", "history", "attempts", "2026-04-10-foo", "do-the-thing")
	if !strings.HasPrefix(path, expected) {
		t.Errorf("expected path prefix %q, got %q", expected, path)
	}
	if !strings.HasSuffix(path, ".jsonl") {
		t.Errorf("expected .jsonl suffix, got %q", path)
	}
}
