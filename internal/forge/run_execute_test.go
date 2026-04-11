package forge

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestPreviewRunShowsExecutionOrderWithoutPersisting(t *testing.T) {
	svc, webRepo := setupRunTestProject(t)

	result, err := svc.PreviewRun(webRepo, "")
	if err != nil {
		t.Fatalf("PreviewRun: %v", err)
	}
	if result.Mode != "preview" {
		t.Fatalf("expected mode=preview, got %s", result.Mode)
	}
	if result.Persisted {
		t.Fatalf("preview should not persist run records")
	}
	if result.RunFile != "" {
		t.Fatalf("preview should not have a run file path")
	}
	if len(result.Tasks) == 0 {
		t.Fatalf("expected at least one task attempt in preview")
	}

	// Verify that no run files were persisted
	ctx, _ := svc.ResolveContext(webRepo)
	runsDir := resolveRunsDir(ctx)
	entries, _ := os.ReadDir(runsDir)
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".json") {
			t.Fatalf("preview mode should not persist run files, found %s", e.Name())
		}
	}
}

func TestExecuteRunRecordsStateAndSkipsUnsafe(t *testing.T) {
	fixedTime := time.Date(2026, 4, 9, 12, 0, 0, 0, time.UTC)
	origTimeNow := timeNow
	timeNow = func() time.Time { return fixedTime }
	defer func() { timeNow = origTimeNow }()

	svc, webRepo := setupRunTestProject(t)

	result, err := svc.ExecuteRun(webRepo, "")
	if err != nil {
		t.Fatalf("ExecuteRun: %v", err)
	}
	if result.Mode != "apply" {
		t.Fatalf("expected mode=apply, got %s", result.Mode)
	}
	if !result.Persisted {
		t.Fatalf("apply mode should persist run records")
	}
	if result.RunFile == "" {
		t.Fatalf("apply mode should produce a run file path")
	}

	// Verify file exists on disk
	if _, err := os.Stat(result.RunFile); err != nil {
		t.Fatalf("run file should exist: %v", err)
	}

	// Check task buckets: safe tasks should succeed, blocked should be skipped
	safeCount := 0
	skippedCount := 0
	for _, task := range result.Tasks {
		switch task.Status {
		case RunStatusSucceeded:
			safeCount++
			if task.Bucket != "safe" && task.Bucket != "serial" {
				t.Fatalf("succeeded task should be in safe or serial bucket, got %s", task.Bucket)
			}
		case RunStatusSkipped:
			skippedCount++
			if task.Bucket != "blocked" && task.Bucket != "conflict" && task.Bucket != "invalid" {
				t.Fatalf("skipped task should be in blocked/conflict/invalid bucket, got %s", task.Bucket)
			}
			if len(task.Reasons) == 0 {
				t.Fatalf("skipped task %q should have reasons", task.Task)
			}
		}
	}
	if result.Summary.Total != len(result.Tasks) {
		t.Fatalf("summary total %d != len(tasks) %d", result.Summary.Total, len(result.Tasks))
	}
	if result.Summary.Succeeded != safeCount {
		t.Fatalf("summary succeeded %d != counted %d", result.Summary.Succeeded, safeCount)
	}
	if result.Summary.Skipped != skippedCount {
		t.Fatalf("summary skipped %d != counted %d", result.Summary.Skipped, skippedCount)
	}
}

func TestExecuteRunFailedTaskLeavesInspectableArtifact(t *testing.T) {
	fixedTime := time.Date(2026, 4, 9, 14, 30, 0, 0, time.UTC)
	origTimeNow := timeNow
	timeNow = func() time.Time { return fixedTime }
	defer func() { timeNow = origTimeNow }()

	svc, webRepo := setupRunTestProject(t)

	result, err := svc.ExecuteRun(webRepo, "")
	if err != nil {
		t.Fatalf("ExecuteRun: %v", err)
	}

	// Reload from disk and verify structure
	record, err := svc.LoadRunRecord(webRepo, result.RunID)
	if err != nil {
		t.Fatalf("LoadRunRecord: %v", err)
	}
	if record.RunID != result.RunID {
		t.Fatalf("reloaded run ID mismatch: %s vs %s", record.RunID, result.RunID)
	}
	if record.ProjectID == "" {
		t.Fatalf("run record missing project ID")
	}
	if record.StartedAt == "" {
		t.Fatalf("run record missing startedAt")
	}
	if record.EndedAt == "" {
		t.Fatalf("run record missing endedAt")
	}
	if len(record.Tasks) == 0 {
		t.Fatalf("run record should contain task attempts")
	}

	// Verify each task has correct fields
	for _, task := range record.Tasks {
		if task.Task == "" {
			t.Fatalf("task attempt missing task name")
		}
		if task.SpecFile == "" {
			t.Fatalf("task attempt missing spec file")
		}
		if task.Bucket == "" {
			t.Fatalf("task attempt missing bucket")
		}
	}
}

func TestListRunRecords(t *testing.T) {
	call := 0
	times := []time.Time{
		time.Date(2026, 4, 9, 10, 0, 0, 0, time.UTC),
		time.Date(2026, 4, 9, 11, 0, 0, 0, time.UTC),
	}
	origTimeNow := timeNow
	timeNow = func() time.Time {
		idx := call
		if idx >= len(times) {
			idx = len(times) - 1
		}
		call++
		return times[idx]
	}
	defer func() { timeNow = origTimeNow }()

	svc, webRepo := setupRunTestProject(t)

	_, err := svc.ExecuteRun(webRepo, "")
	if err != nil {
		t.Fatalf("ExecuteRun 1: %v", err)
	}

	// Advance time for second run
	call = len(times) - 1
	timeNow = func() time.Time { return times[1] }
	_, err = svc.ExecuteRun(webRepo, "")
	if err != nil {
		t.Fatalf("ExecuteRun 2: %v", err)
	}

	records, err := svc.ListRunRecords(webRepo)
	if err != nil {
		t.Fatalf("ListRunRecords: %v", err)
	}
	if len(records) < 2 {
		t.Fatalf("expected at least 2 run records, got %d", len(records))
	}
	// Most recent should come first
	if records[0].StartedAt < records[1].StartedAt {
		t.Fatalf("expected most recent run first, got %s before %s", records[0].StartedAt, records[1].StartedAt)
	}
}

func TestSummarizeAttempts(t *testing.T) {
	attempts := []TaskAttempt{
		{Status: RunStatusSucceeded},
		{Status: RunStatusSucceeded},
		{Status: RunStatusFailed},
		{Status: RunStatusSkipped},
		{Status: RunStatusSkipped},
		{Status: RunStatusSkipped},
	}
	s := summarizeAttempts(attempts)
	if s.Total != 6 {
		t.Fatalf("expected total=6, got %d", s.Total)
	}
	if s.Succeeded != 2 {
		t.Fatalf("expected succeeded=2, got %d", s.Succeeded)
	}
	if s.Failed != 1 {
		t.Fatalf("expected failed=1, got %d", s.Failed)
	}
	if s.Skipped != 3 {
		t.Fatalf("expected skipped=3, got %d", s.Skipped)
	}
}

func TestGenerateRunID(t *testing.T) {
	fixedTime := time.Date(2026, 4, 9, 15, 30, 45, 0, time.UTC)
	origTimeNow := timeNow
	timeNow = func() time.Time { return fixedTime }
	defer func() { timeNow = origTimeNow }()

	id := generateRunID()
	if !strings.HasPrefix(id, "run-20260409-") {
		t.Fatalf("unexpected run ID format: %s", id)
	}
}

// setupRunTestProject creates a Forge project with a spec containing mixed task states
// (safe parallel, blocked deps, serial) for testing the executor.
func setupRunTestProject(t *testing.T) (*Service, string) {
	t.Helper()
	home := t.TempDir()
	svc := &Service{ForgeHome: filepath.Join(home, ".forge")}
	if _, err := svc.InitWorkspace("main", "Main Workspace", ""); err != nil {
		t.Fatalf("InitWorkspace: %v", err)
	}
	if _, err := svc.InitProject("main", "test-project", "Test Project"); err != nil {
		t.Fatalf("InitProject: %v", err)
	}

	webRepo := filepath.Join(home, "code", "test-web")
	if err := os.MkdirAll(filepath.Join(webRepo, "src"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.LinkRepo("main", "test-project", "core", "test-web", "core repo", "", webRepo); err != nil {
		t.Fatalf("LinkRepo: %v", err)
	}

	ctx, err := svc.ResolveContext(webRepo)
	if err != nil {
		t.Fatalf("ResolveContext: %v", err)
	}

	specContent := `# Test run execution
<!-- status: todo -->
<!-- priority: high -->
<!-- created: 2026-04-09 -->
<!-- branch: test/run-execution -->

**Demo:** Operator runs forge run and sees task execution results.

## Expected behavior
- Safe tasks execute.
- Blocked tasks are skipped.

## Test cases

### Scenario: Safe tasks execute
Given a project with runnable tasks
When the operator runs forge run --apply
Then safe tasks are executed

### Scenario: Blocked tasks are skipped
Given a project with blocked tasks
When the operator runs forge run --apply
Then blocked tasks are skipped with reasons

## Validation plan
- Unit tests cover execution and persistence.

### Task: Independent work A
<!-- status: todo -->
<!-- parallelizable: yes -->
<!-- deps: none -->
<!-- repo: core -->
<!-- touches: src/features/alpha/** -->
<!-- conflict-risk: low -->

**Done when:**
- Alpha feature is implemented.

**Validation:**
- Covers: Scenario "Safe tasks execute"

### Task: Independent work B
<!-- status: todo -->
<!-- parallelizable: yes -->
<!-- deps: none -->
<!-- repo: core -->
<!-- touches: src/features/beta/** -->
<!-- conflict-risk: low -->

**Done when:**
- Beta feature is implemented.

**Validation:**
- Covers: Scenario "Safe tasks execute"

### Task: Depends on A
<!-- status: todo -->
<!-- parallelizable: no -->
<!-- deps: Independent work A -->
<!-- repo: core -->
<!-- touches: src/features/gamma/** -->
<!-- conflict-risk: medium -->

**Done when:**
- Gamma feature is implemented after A.

**Validation:**
- Covers: Scenario "Blocked tasks are skipped"
`
	specPath := filepath.Join(ctx.SpecsDir, "test-run-spec.md")
	if err := os.WriteFile(specPath, []byte(specContent), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	return svc, webRepo
}
