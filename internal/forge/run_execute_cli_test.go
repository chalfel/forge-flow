package forge

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestPreviewRunDoesNotMutateState(t *testing.T) {
	svc, webRepo := setupRunTestProject(t)

	result, err := svc.PreviewRun(webRepo, "")
	if err != nil {
		t.Fatalf("PreviewRun: %v", err)
	}
	if result.Mode != "preview" {
		t.Fatalf("expected preview mode")
	}
	if result.Persisted {
		t.Fatalf("preview must not persist")
	}

	// Check that safe tasks show as pending (would-be-executed)
	hasPending := false
	for _, task := range result.Tasks {
		if task.Status == RunStatusPending {
			hasPending = true
		}
	}
	if !hasPending {
		t.Fatalf("preview should show safe tasks as pending")
	}

	// Check that blocked tasks show as skipped
	hasSkipped := false
	for _, task := range result.Tasks {
		if task.Status == RunStatusSkipped {
			hasSkipped = true
		}
	}
	if !hasSkipped {
		t.Fatalf("preview should show blocked/invalid tasks as skipped")
	}
}

func TestApplyRunRefusesBlockedAndInvalidTasks(t *testing.T) {
	fixedTime := time.Date(2026, 4, 9, 16, 0, 0, 0, time.UTC)
	origTimeNow := timeNow
	timeNow = func() time.Time { return fixedTime }
	defer func() { timeNow = origTimeNow }()

	svc, webRepo := setupRunTestProject(t)

	result, err := svc.ExecuteRun(webRepo, "")
	if err != nil {
		t.Fatalf("ExecuteRun: %v", err)
	}

	// "Depends on A" is blocked because its dependency is not done.
	// It must NOT appear as succeeded.
	for _, task := range result.Tasks {
		if task.Task == "Depends on A" {
			if task.Status == RunStatusSucceeded {
				t.Fatalf("blocked task 'Depends on A' should not succeed")
			}
			if task.Status != RunStatusSkipped {
				t.Fatalf("blocked task 'Depends on A' should be skipped, got %s", task.Status)
			}
			if task.Bucket != "blocked" {
				t.Fatalf("expected bucket=blocked for 'Depends on A', got %s", task.Bucket)
			}
		}
	}
}

func TestCLIOutputSeparatesRunnableSkippedAndFailed(t *testing.T) {
	svc, webRepo := setupRunTestProject(t)

	result, err := svc.PreviewRun(webRepo, "")
	if err != nil {
		t.Fatalf("PreviewRun: %v", err)
	}

	// Group by status
	statusGroups := map[RunStatus]int{}
	for _, task := range result.Tasks {
		statusGroups[task.Status]++
	}

	// There should be both pending (runnable) and skipped (blocked) tasks
	if statusGroups[RunStatusPending] == 0 {
		t.Fatalf("expected at least one pending (runnable) task")
	}
	if statusGroups[RunStatusSkipped] == 0 {
		t.Fatalf("expected at least one skipped task")
	}
}

func TestExecuteRunWithSpecFilter(t *testing.T) {
	fixedTime := time.Date(2026, 4, 9, 17, 0, 0, 0, time.UTC)
	origTimeNow := timeNow
	timeNow = func() time.Time { return fixedTime }
	defer func() { timeNow = origTimeNow }()

	svc, webRepo := setupRunTestProject(t)
	ctx, err := svc.ResolveContext(webRepo)
	if err != nil {
		t.Fatalf("ResolveContext: %v", err)
	}

	specFile := filepath.Join(ctx.SpecsDir, "test-run-spec.md")
	result, err := svc.ExecuteRun(webRepo, specFile)
	if err != nil {
		t.Fatalf("ExecuteRun with spec filter: %v", err)
	}
	if len(result.Tasks) == 0 {
		t.Fatalf("expected tasks in filtered run")
	}

	// All tasks should reference the filtered spec
	for _, task := range result.Tasks {
		if task.SpecFile == "" {
			t.Fatalf("task missing spec file reference")
		}
	}
}

func TestRunListAndShowRoundtrip(t *testing.T) {
	fixedTime := time.Date(2026, 4, 9, 18, 0, 0, 0, time.UTC)
	origTimeNow := timeNow
	timeNow = func() time.Time { return fixedTime }
	defer func() { timeNow = origTimeNow }()

	svc, webRepo := setupRunTestProject(t)

	execResult, err := svc.ExecuteRun(webRepo, "")
	if err != nil {
		t.Fatalf("ExecuteRun: %v", err)
	}

	// List should include the run
	records, err := svc.ListRunRecords(webRepo)
	if err != nil {
		t.Fatalf("ListRunRecords: %v", err)
	}
	found := false
	for _, r := range records {
		if r.RunID == execResult.RunID {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected run %s in list", execResult.RunID)
	}

	// Show should return matching record
	record, err := svc.LoadRunRecord(webRepo, execResult.RunID)
	if err != nil {
		t.Fatalf("LoadRunRecord: %v", err)
	}
	if record.RunID != execResult.RunID {
		t.Fatalf("run ID mismatch")
	}
	if record.Summary.Total != execResult.Summary.Total {
		t.Fatalf("summary total mismatch")
	}
}

func TestListRunRecordsEmptyProject(t *testing.T) {
	home := t.TempDir()
	svc := &Service{ForgeHome: filepath.Join(home, ".forge")}
	if _, err := svc.InitWorkspace("main", "Main", ""); err != nil {
		t.Fatalf("InitWorkspace: %v", err)
	}
	if _, err := svc.InitProject("main", "empty-project", "Empty"); err != nil {
		t.Fatalf("InitProject: %v", err)
	}
	repo := filepath.Join(home, "code", "empty-repo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.LinkRepo("main", "empty-project", "core", "empty-repo", "core", "", repo); err != nil {
		t.Fatalf("LinkRepo: %v", err)
	}

	records, err := svc.ListRunRecords(repo)
	if err != nil {
		t.Fatalf("ListRunRecords: %v", err)
	}
	if len(records) != 0 {
		t.Fatalf("expected 0 records for fresh project, got %d", len(records))
	}
}
