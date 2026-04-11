package forge

import (
	"strings"
	"testing"
)

func TestHarnessSiblingDecisionsInjected(t *testing.T) {
	h := &HarnessBuilder{
		Task: RunPlanTask{
			Task: "Task B",
			File: "spec1.md",
		},
		WorktreePath: "/tmp/wt",
		BaseBranch:   "main",
		SiblingDecisions: []Decision{
			{
				TaskName:     "Task A",
				Choice:       "Use Zod for validation",
				Rationale:    "Better TS inference",
				Alternatives: []string{"yup", "joi"},
			},
			{
				TaskName: "Task C",
				Choice:   "Postgres over MongoDB",
			},
		},
	}

	cmd := h.Build()

	if !strings.Contains(cmd, "Previous decisions on this spec") {
		t.Fatal("expected sibling decisions section header")
	}
	if !strings.Contains(cmd, "Use Zod for validation") {
		t.Fatal("expected Task A decision")
	}
	if !strings.Contains(cmd, "Better TS inference") {
		t.Fatal("expected rationale")
	}
	if !strings.Contains(cmd, "alternatives considered") {
		t.Fatal("expected alternatives label")
	}
	if !strings.Contains(cmd, "Postgres over MongoDB") {
		t.Fatal("expected Task C decision")
	}
}

func TestHarnessFiltersOwnDecisions(t *testing.T) {
	h := &HarnessBuilder{
		Task: RunPlanTask{
			Task: "Task A",
			File: "spec1.md",
		},
		WorktreePath: "/tmp/wt",
		BaseBranch:   "main",
		SiblingDecisions: []Decision{
			{
				TaskName: "Task A",
				Choice:   "My own previous choice",
			},
			{
				TaskName: "Task B",
				Choice:   "Someone elses choice",
			},
		},
	}

	cmd := h.Build()

	if strings.Contains(cmd, "My own previous choice") {
		t.Fatal("own decision should not be injected back")
	}
	if !strings.Contains(cmd, "Someone elses choice") {
		t.Fatal("sibling's decision should be present")
	}
}

func TestHarnessSiblingDecisionsEmptyWhenNoneExist(t *testing.T) {
	h := &HarnessBuilder{
		Task:         RunPlanTask{Task: "Solo"},
		WorktreePath: "/tmp/wt",
		BaseBranch:   "main",
	}

	cmd := h.Build()

	if strings.Contains(cmd, "Previous decisions on this spec") {
		t.Fatal("section should not appear when no sibling decisions")
	}
}

func TestHarnessLoadsSiblingDecisionsFromDisk(t *testing.T) {
	svc, webRepo := setupRunTestProject(t)
	ctx, err := svc.ResolveContext(webRepo)
	if err != nil {
		t.Fatalf("ResolveContext: %v", err)
	}

	// Persist decisions for two sibling tasks on the same spec
	_ = svc.SaveDecision(webRepo, Decision{
		SpecFile: "sibling-spec.md",
		TaskName: "First sibling",
		Choice:   "foundational choice",
	})
	_ = svc.SaveDecision(webRepo, Decision{
		SpecFile: "sibling-spec.md",
		TaskName: "Second sibling",
		Choice:   "follow-up choice",
	})

	// Build a harness for a THIRD task on the same spec
	h := &HarnessBuilder{
		Ctx: ctx,
		Task: RunPlanTask{
			Task: "Third task",
			File: "sibling-spec.md",
		},
		WorktreePath: "/tmp/wt",
		BaseBranch:   "main",
	}

	cmd := h.Build()

	if !strings.Contains(cmd, "foundational choice") {
		t.Fatal("first sibling's decision not loaded from disk")
	}
	if !strings.Contains(cmd, "follow-up choice") {
		t.Fatal("second sibling's decision not loaded from disk")
	}
}
