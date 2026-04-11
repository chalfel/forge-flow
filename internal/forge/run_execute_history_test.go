package forge

import (
	"strings"
	"testing"
)

func TestBuildClaudeCommandWithHistoryInjectsPastAttempts(t *testing.T) {
	task := RunPlanTask{
		Task:      "Add login button",
		SpecTitle: "User can sign in",
		Repo:      "web",
	}
	pastAttempts := []TaskAttemptRecord{
		{
			Attempt:   1,
			RunID:     "run-20260409-100000",
			StartedAt: "2026-04-09T10:00:00Z",
			Status:    "failed",
			Error:     "Build failed: undefined TypeScript type",
		},
		{
			Attempt:   2,
			RunID:     "run-20260409-120000",
			StartedAt: "2026-04-09T12:00:00Z",
			Status:    "changes_requested",
			ReviewFeedback: []FeedbackItem{
				{Kind: "bug", Text: "Link points to /register", File: "app/page.tsx", Line: 42},
				{Kind: "style", Text: "Use existing Button component"},
			},
		},
	}

	cmd := BuildClaudeCommandWithHistory("/tmp/wt", "/project", task, "", "main", pastAttempts, nil)

	// Must mention past attempts section
	if !strings.Contains(cmd, "Previous attempts on this task") {
		t.Fatal("expected past attempts section in prompt")
	}
	// Must show attempt numbers and statuses
	if !strings.Contains(cmd, "Attempt 1") || !strings.Contains(cmd, "FAILED") {
		t.Fatal("expected attempt 1 failed entry")
	}
	if !strings.Contains(cmd, "Attempt 2") || !strings.Contains(cmd, "CHANGES_REQUESTED") {
		t.Fatal("expected attempt 2 changes_requested entry")
	}
	// Must show the error from attempt 1
	if !strings.Contains(cmd, "undefined TypeScript type") {
		t.Fatal("expected error message from attempt 1")
	}
	// Must show feedback from attempt 2
	if !strings.Contains(cmd, "BUG") || !strings.Contains(cmd, "Link points to /register") {
		t.Fatal("expected bug feedback from attempt 2")
	}
	if !strings.Contains(cmd, "app/page.tsx:42") {
		t.Fatal("expected file:line reference in feedback")
	}
	// Must have the guidance to not repeat
	if !strings.Contains(cmd, "Do not repeat the same mistakes") {
		t.Fatal("expected guidance to avoid repeating mistakes")
	}
}

func TestBuildClaudeCommandWithHistoryInjectsLatestFeedback(t *testing.T) {
	task := RunPlanTask{
		Task:      "Fix the nav",
		SpecTitle: "Header polish",
	}
	fb := &StructuredFeedback{
		Status: "changes_requested",
		Items: []FeedbackItem{
			{Kind: "missing", Text: "Logo alt text"},
			{Kind: "bug", Text: "Menu collapses wrong on mobile", File: "header.tsx"},
		},
	}

	cmd := BuildClaudeCommandWithHistory("/tmp/wt", "/project", task, "", "main", nil, fb)

	if !strings.Contains(cmd, "Latest review feedback (must be addressed)") {
		t.Fatal("expected latest feedback section")
	}
	if !strings.Contains(cmd, "MISSING") || !strings.Contains(cmd, "Logo alt text") {
		t.Fatal("expected missing feedback item")
	}
	if !strings.Contains(cmd, "header.tsx") {
		t.Fatal("expected file reference in feedback")
	}
}

func TestBuildClaudeCommandWithHistoryEmptyHistory(t *testing.T) {
	task := RunPlanTask{Task: "First run", SpecTitle: "New spec"}

	cmd := BuildClaudeCommandWithHistory("/tmp/wt", "/project", task, "", "main", nil, nil)

	// Without history, should NOT include the sections
	if strings.Contains(cmd, "Previous attempts on this task") {
		t.Fatal("should not include attempts section when empty")
	}
	if strings.Contains(cmd, "Latest review feedback") {
		t.Fatal("should not include feedback section when empty")
	}
	// But base instructions still there
	if !strings.Contains(cmd, "Implement the task") {
		t.Fatal("base instructions missing")
	}
}

func TestRunIDFromStartTime(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"2026-04-10T12:30:45Z", "run-20260410-123045"},
		{"2026-04-10T00:00:00Z", "run-20260410-000000"},
	}
	for _, tt := range tests {
		got := runIDFromStartTime(tt.input)
		if got != tt.want {
			t.Errorf("runIDFromStartTime(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}

	// Invalid input should fall back to generateRunID format
	got := runIDFromStartTime("not-a-timestamp")
	if !strings.HasPrefix(got, "run-") {
		t.Errorf("fallback should still have run- prefix, got %q", got)
	}
}
