package forge

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestListProposedMemoriesEmpty(t *testing.T) {
	svc, webRepo := setupRunTestProject(t)

	memories, err := svc.ListProposedMemories(webRepo)
	if err != nil {
		t.Fatalf("ListProposedMemories: %v", err)
	}
	// setupRunTestProject doesn't customize inbox.md so default content
	// has the empty section — should return 0 items.
	if len(memories) != 0 {
		t.Fatalf("expected 0 proposed memories, got %d", len(memories))
	}
}

func TestLoadProposedMemoriesFromFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "inbox.md")
	content := `# Inbox

Capture rough ideas here.

## Proposed memory
- [run-1] Supabase migrations must regenerate types after
- [run-2] Use gh pr view --json for PR URLs

## Other section
- this should not be included
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	memories, err := loadProposedMemoriesFromFile(path)
	if err != nil {
		t.Fatalf("loadProposedMemoriesFromFile: %v", err)
	}
	if len(memories) != 2 {
		t.Fatalf("expected 2 memories, got %d", len(memories))
	}
	if !strings.Contains(memories[0].Text, "Supabase migrations") {
		t.Fatalf("unexpected first entry: %q", memories[0].Text)
	}
	if !strings.Contains(memories[1].Text, "gh pr view") {
		t.Fatalf("unexpected second entry: %q", memories[1].Text)
	}
}

func TestLoadProposedMemoriesIgnoresOtherSections(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "inbox.md")
	content := `# Inbox

## Random thoughts
- this is not a memory
- neither is this

## Proposed memory
- actual memory 1

## Something else
- not a memory either
`
	_ = os.WriteFile(path, []byte(content), 0o644)

	memories, _ := loadProposedMemoriesFromFile(path)
	if len(memories) != 1 {
		t.Fatalf("expected 1 memory, got %d", len(memories))
	}
	if memories[0].Text != "actual memory 1" {
		t.Fatalf("unexpected memory: %q", memories[0].Text)
	}
}

func TestAppendProposedMemoryToExistingSection(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "inbox.md")
	content := `# Inbox

## Proposed memory
- existing memory

## Other
- unrelated
`
	_ = os.WriteFile(path, []byte(content), 0o644)

	err := appendProposedMemoryToFile(path, "new memory from agent")
	if err != nil {
		t.Fatalf("append: %v", err)
	}

	memories, _ := loadProposedMemoriesFromFile(path)
	if len(memories) != 2 {
		t.Fatalf("expected 2 memories, got %d", len(memories))
	}
	if memories[1].Text != "new memory from agent" {
		t.Fatalf("expected new entry, got %q", memories[1].Text)
	}

	// Verify other section still intact
	data, _ := os.ReadFile(path)
	if !strings.Contains(string(data), "## Other") {
		t.Fatal("other section was damaged")
	}
}

func TestAppendProposedMemoryCreatesSection(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "inbox.md")
	content := `# Inbox

Capture ideas here.
`
	_ = os.WriteFile(path, []byte(content), 0o644)

	err := appendProposedMemoryToFile(path, "first proposal")
	if err != nil {
		t.Fatalf("append: %v", err)
	}

	memories, _ := loadProposedMemoriesFromFile(path)
	if len(memories) != 1 {
		t.Fatalf("expected 1 memory, got %d", len(memories))
	}
	if memories[0].Text != "first proposal" {
		t.Fatalf("unexpected text: %q", memories[0].Text)
	}
}
