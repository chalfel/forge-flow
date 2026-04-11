package forge

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestUpdateMetadataInLinesReplacesExisting(t *testing.T) {
	lines := strings.Split(`# My Spec
<!-- status: todo -->

### Task: Build the thing
<!-- status: todo -->
<!-- parallelizable: yes -->
<!-- deps: none -->

**Done when:**
- It works.
`, "\n")

	result, err := updateMetadataInLines(lines, "Build the thing", map[string]string{
		"status": "in_progress",
	})
	if err != nil {
		t.Fatalf("updateMetadataInLines: %v", err)
	}

	joined := strings.Join(result, "\n")
	if !strings.Contains(joined, "<!-- status: in_progress -->") {
		t.Fatalf("expected status to be updated to in_progress, got:\n%s", joined)
	}
	if strings.Contains(joined, "<!-- status: todo -->") {
		// The spec-level status: todo should remain, but the task-level should be updated
		// Count occurrences
		count := strings.Count(joined, "<!-- status: todo -->")
		if count != 1 {
			t.Fatalf("expected exactly 1 remaining 'status: todo' (spec-level), got %d", count)
		}
	}
}

func TestUpdateMetadataInLinesInsertsNew(t *testing.T) {
	lines := strings.Split(`# My Spec
<!-- status: todo -->

### Task: Build the thing
<!-- status: todo -->
<!-- parallelizable: yes -->

**Done when:**
- It works.
`, "\n")

	result, err := updateMetadataInLines(lines, "Build the thing", map[string]string{
		"pr": "https://github.com/org/repo/pull/42",
	})
	if err != nil {
		t.Fatalf("updateMetadataInLines: %v", err)
	}

	joined := strings.Join(result, "\n")
	if !strings.Contains(joined, "<!-- pr: https://github.com/org/repo/pull/42 -->") {
		t.Fatalf("expected pr metadata to be inserted, got:\n%s", joined)
	}
}

func TestUpdateMetadataInLinesMultipleUpdates(t *testing.T) {
	lines := strings.Split(`# My Spec
<!-- status: todo -->

### Task: Build the thing
<!-- status: todo -->
<!-- parallelizable: yes -->
<!-- deps: none -->

**Done when:**
- It works.
`, "\n")

	result, err := updateMetadataInLines(lines, "Build the thing", map[string]string{
		"status": "in_review",
		"pr":     "https://github.com/org/repo/pull/99",
	})
	if err != nil {
		t.Fatalf("updateMetadataInLines: %v", err)
	}

	joined := strings.Join(result, "\n")
	if !strings.Contains(joined, "<!-- status: in_review -->") {
		t.Fatalf("expected status update, got:\n%s", joined)
	}
	if !strings.Contains(joined, "<!-- pr: https://github.com/org/repo/pull/99 -->") {
		t.Fatalf("expected pr insertion, got:\n%s", joined)
	}
}

func TestUpdateMetadataInLinesMultipleTasks(t *testing.T) {
	lines := strings.Split(`# My Spec
<!-- status: todo -->

### Task: First task
<!-- status: todo -->
<!-- parallelizable: yes -->
<!-- deps: none -->

**Done when:**
- First is done.

### Task: Second task
<!-- status: todo -->
<!-- parallelizable: no -->
<!-- deps: First task -->

**Done when:**
- Second is done.
`, "\n")

	result, err := updateMetadataInLines(lines, "Second task", map[string]string{
		"status": "in_progress",
	})
	if err != nil {
		t.Fatalf("updateMetadataInLines: %v", err)
	}

	joined := strings.Join(result, "\n")

	// First task should remain todo
	firstIdx := strings.Index(joined, "### Task: First task")
	secondIdx := strings.Index(joined, "### Task: Second task")
	firstSection := joined[firstIdx:secondIdx]
	if !strings.Contains(firstSection, "<!-- status: todo -->") {
		t.Fatalf("first task status should remain todo")
	}

	secondSection := joined[secondIdx:]
	if !strings.Contains(secondSection, "<!-- status: in_progress -->") {
		t.Fatalf("second task status should be in_progress, got:\n%s", secondSection)
	}
}

func TestUpdateMetadataInLinesTaskNotFound(t *testing.T) {
	lines := strings.Split(`# My Spec

### Task: Existing task
<!-- status: todo -->
`, "\n")

	_, err := updateMetadataInLines(lines, "Nonexistent task", map[string]string{
		"status": "done",
	})
	if err == nil {
		t.Fatalf("expected error for nonexistent task")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Fatalf("expected 'not found' error, got: %v", err)
	}
}

func TestUpdateMetadataInLinesCaseInsensitive(t *testing.T) {
	lines := strings.Split(`# My Spec

### Task: Add run execution models
<!-- status: todo -->
`, "\n")

	result, err := updateMetadataInLines(lines, "add run execution models", map[string]string{
		"status": "done",
	})
	if err != nil {
		t.Fatalf("updateMetadataInLines: %v", err)
	}

	joined := strings.Join(result, "\n")
	if !strings.Contains(joined, "<!-- status: done -->") {
		t.Fatalf("case-insensitive match should work, got:\n%s", joined)
	}
}

func TestUpdateTaskMetadataFileRoundtrip(t *testing.T) {
	dir := t.TempDir()
	specPath := filepath.Join(dir, "spec.md")
	content := `# Test Spec
<!-- status: todo -->

### Task: Build feature
<!-- status: todo -->
<!-- parallelizable: yes -->
<!-- deps: none -->

**Done when:**
- Feature is built.
`
	if err := os.WriteFile(specPath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	err := UpdateTaskMetadata(specPath, "Build feature", map[string]string{
		"status": "in_review",
		"pr":     "https://github.com/test/repo/pull/1",
	})
	if err != nil {
		t.Fatalf("UpdateTaskMetadata: %v", err)
	}

	data, err := os.ReadFile(specPath)
	if err != nil {
		t.Fatal(err)
	}
	result := string(data)
	if !strings.Contains(result, "<!-- status: in_review -->") {
		t.Fatalf("expected status update in file")
	}
	if !strings.Contains(result, "<!-- pr: https://github.com/test/repo/pull/1 -->") {
		t.Fatalf("expected pr insertion in file")
	}
}

func TestUpdateMetadataEmptyUpdates(t *testing.T) {
	lines := strings.Split(`### Task: Noop
<!-- status: todo -->
`, "\n")

	result, err := updateMetadataInLines(lines, "Noop", map[string]string{})
	if err != nil {
		t.Fatalf("updateMetadataInLines: %v", err)
	}
	if strings.Join(result, "\n") != strings.Join(lines, "\n") {
		t.Fatalf("empty updates should not change content")
	}
}
