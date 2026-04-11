package forge

import (
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSaveAndLoadDecisionForTask(t *testing.T) {
	fixedTime := time.Date(2026, 4, 10, 12, 0, 0, 0, time.UTC)
	origTimeNow := timeNow
	timeNow = func() time.Time { return fixedTime }
	defer func() { timeNow = origTimeNow }()

	svc, webRepo := setupRunTestProject(t)

	d := Decision{
		SpecFile:     "spec1.md",
		TaskName:     "Task A",
		Choice:       "Use Zod for validation",
		Rationale:    "Better TypeScript inference than yup",
		Alternatives: []string{"yup", "joi"},
		RunID:        "run-1",
	}
	err := svc.SaveDecision(webRepo, d)
	if err != nil {
		t.Fatalf("SaveDecision: %v", err)
	}

	loaded, err := svc.LoadDecisionsForTask(webRepo, "spec1.md", "Task A")
	if err != nil {
		t.Fatalf("LoadDecisionsForTask: %v", err)
	}
	if len(loaded) != 1 {
		t.Fatalf("expected 1 decision, got %d", len(loaded))
	}
	if loaded[0].Choice != "Use Zod for validation" {
		t.Fatalf("unexpected choice: %s", loaded[0].Choice)
	}
	if loaded[0].RecordedAt == "" {
		t.Fatal("expected RecordedAt to be auto-set")
	}
	if len(loaded[0].Alternatives) != 2 {
		t.Fatalf("expected 2 alternatives, got %d", len(loaded[0].Alternatives))
	}
}

func TestSaveMultipleDecisionsForSameTask(t *testing.T) {
	svc, webRepo := setupRunTestProject(t)

	for i, choice := range []string{"first choice", "second choice", "third choice"} {
		_ = svc.SaveDecision(webRepo, Decision{
			SpecFile: "spec1.md",
			TaskName: "Task A",
			Choice:   choice,
			RunID:    "run-" + string(rune('a'+i)),
		})
	}

	loaded, _ := svc.LoadDecisionsForTask(webRepo, "spec1.md", "Task A")
	if len(loaded) != 3 {
		t.Fatalf("expected 3 decisions, got %d", len(loaded))
	}
	if loaded[0].Choice != "first choice" || loaded[2].Choice != "third choice" {
		t.Fatal("decisions not preserved in order")
	}
}

func TestLoadDecisionsForSpecAcrossTasks(t *testing.T) {
	svc, webRepo := setupRunTestProject(t)

	_ = svc.SaveDecision(webRepo, Decision{SpecFile: "spec1.md", TaskName: "Task A", Choice: "choice A1"})
	_ = svc.SaveDecision(webRepo, Decision{SpecFile: "spec1.md", TaskName: "Task A", Choice: "choice A2"})
	_ = svc.SaveDecision(webRepo, Decision{SpecFile: "spec1.md", TaskName: "Task B", Choice: "choice B1"})
	_ = svc.SaveDecision(webRepo, Decision{SpecFile: "other.md", TaskName: "Unrelated", Choice: "noise"})

	decisions, err := svc.LoadDecisionsForSpec(webRepo, "spec1.md")
	if err != nil {
		t.Fatalf("LoadDecisionsForSpec: %v", err)
	}
	if len(decisions) != 3 {
		t.Fatalf("expected 3 decisions from spec1, got %d", len(decisions))
	}

	// Verify "noise" from other spec is NOT included
	for _, d := range decisions {
		if d.Choice == "noise" {
			t.Fatal("other spec's decision leaked in")
		}
	}
}

func TestLoadDecisionsForSpecMissingReturnsEmpty(t *testing.T) {
	svc, webRepo := setupRunTestProject(t)

	decisions, err := svc.LoadDecisionsForSpec(webRepo, "nonexistent.md")
	if err != nil {
		t.Fatalf("should not error: %v", err)
	}
	if len(decisions) != 0 {
		t.Fatalf("expected empty, got %d", len(decisions))
	}
}

func TestListAllDecisionsEmpty(t *testing.T) {
	svc, webRepo := setupRunTestProject(t)

	result, err := svc.ListAllDecisions(webRepo)
	if err != nil {
		t.Fatalf("ListAllDecisions: %v", err)
	}
	if len(result) != 0 {
		t.Fatalf("expected empty, got %d specs", len(result))
	}
}

func TestListAllDecisionsAcrossSpecs(t *testing.T) {
	svc, webRepo := setupRunTestProject(t)

	_ = svc.SaveDecision(webRepo, Decision{SpecFile: "spec1.md", TaskName: "Task A", Choice: "A"})
	_ = svc.SaveDecision(webRepo, Decision{SpecFile: "spec2.md", TaskName: "Task B", Choice: "B"})
	_ = svc.SaveDecision(webRepo, Decision{SpecFile: "spec2.md", TaskName: "Task C", Choice: "C"})

	result, err := svc.ListAllDecisions(webRepo)
	if err != nil {
		t.Fatalf("ListAllDecisions: %v", err)
	}
	if len(result) != 2 {
		t.Fatalf("expected 2 specs, got %d", len(result))
	}

	spec2Key := slugifySpecFile("spec2.md")
	if len(result[spec2Key]) != 2 {
		t.Fatalf("expected 2 decisions from spec2, got %d", len(result[spec2Key]))
	}
}

func TestDecisionsPathStructure(t *testing.T) {
	ctx := ResolvedContext{RepoLocalForge: "/repo/.forge"}
	path := decisionsPath(ctx, "2026-04-10-foo.md", "Do the Thing!")
	expected := filepath.Join("/repo/.forge", "history", "decisions", "2026-04-10-foo", "do-the-thing.jsonl")
	if path != expected {
		t.Errorf("expected %q, got %q", expected, path)
	}
	if !strings.HasSuffix(path, ".jsonl") {
		t.Error("expected .jsonl suffix")
	}
}
