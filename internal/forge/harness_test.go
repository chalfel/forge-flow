package forge

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestHarnessBuilderConstraintsSection(t *testing.T) {
	dir := t.TempDir()
	constraintsPath := filepath.Join(dir, "constraints.md")
	content := `# Forge Constraints

## Git
- NEVER force push
- NEVER push to main
`
	if err := os.WriteFile(constraintsPath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	h := &HarnessBuilder{Ctx: ResolvedContext{ProjectRoot: dir}}
	out := h.constraintsSection()

	if !strings.Contains(out, "Non-negotiable constraints") {
		t.Fatal("expected constraints header")
	}
	if !strings.Contains(out, "NEVER force push") {
		t.Fatal("expected constraints content")
	}
}

func TestHarnessBuilderConstraintsSectionMissing(t *testing.T) {
	h := &HarnessBuilder{Ctx: ResolvedContext{ProjectRoot: t.TempDir()}}
	if out := h.constraintsSection(); out != "" {
		t.Fatalf("expected empty when constraints.md missing, got %q", out)
	}
}

func TestHarnessBuilderMemorySection(t *testing.T) {
	dir := t.TempDir()
	kbDir := filepath.Join(dir, "kb")
	if err := os.MkdirAll(kbDir, 0o755); err != nil {
		t.Fatal(err)
	}
	memPath := filepath.Join(kbDir, "memory.md")
	content := `# Project Memory

## Conventions
- Use Zod for all API validation
- Never use moment.js, prefer date-fns
`
	if err := os.WriteFile(memPath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	h := &HarnessBuilder{Ctx: ResolvedContext{KBDir: kbDir}}
	out := h.memorySection()

	if !strings.Contains(out, "Project memory") {
		t.Fatal("expected memory header")
	}
	if !strings.Contains(out, "Use Zod") {
		t.Fatal("expected memory content")
	}
}

func TestHarnessBuilderMemorySectionMissing(t *testing.T) {
	dir := t.TempDir()
	kbDir := filepath.Join(dir, "kb")
	_ = os.MkdirAll(kbDir, 0o755)

	h := &HarnessBuilder{Ctx: ResolvedContext{KBDir: kbDir}}
	if out := h.memorySection(); out != "" {
		t.Fatalf("expected empty when memory.md missing, got %q", out)
	}
}

func TestHarnessBuilderProjectContextSection(t *testing.T) {
	h := &HarnessBuilder{KBContent: "# Architecture\n\nGo CLI + React frontend."}
	out := h.projectContextSection()

	if !strings.Contains(out, "Project knowledge base") {
		t.Fatal("expected KB header")
	}
	if !strings.Contains(out, "Go CLI + React frontend") {
		t.Fatal("expected KB content")
	}
}

func TestHarnessBuilderStateSection(t *testing.T) {
	h := &HarnessBuilder{BoardSnapshot: "9 specs, 26 tasks, 69% done"}
	out := h.stateSection()

	if !strings.Contains(out, "Current project state") {
		t.Fatal("expected state header")
	}
	if !strings.Contains(out, "9 specs, 26 tasks") {
		t.Fatal("expected board snapshot content")
	}
}

func TestHarnessBuilderHistorySectionWithAttempts(t *testing.T) {
	h := &HarnessBuilder{
		PastAttempts: []TaskAttemptRecord{
			{
				Attempt:   1,
				StartedAt: "2026-04-10T10:00:00Z",
				Status:    "failed",
				Error:     "Build error",
			},
		},
	}
	out := h.historySection()

	if !strings.Contains(out, "Previous attempts") {
		t.Fatal("expected attempts header")
	}
	if !strings.Contains(out, "Build error") {
		t.Fatal("expected error message")
	}
	if !strings.Contains(out, "Do not repeat the same mistakes") {
		t.Fatal("expected warning")
	}
}

func TestHarnessBuilderHistorySectionEmpty(t *testing.T) {
	h := &HarnessBuilder{}
	if out := h.historySection(); out != "" {
		t.Fatalf("expected empty, got %q", out)
	}
}

func TestHarnessBuilderProtocolSectionEmpty(t *testing.T) {
	h := &HarnessBuilder{}
	if out := h.protocolSection(); out != "" {
		t.Fatal("expected empty protocol section when EventsFile unset")
	}
}

func TestHarnessBuilderProtocolSectionPresent(t *testing.T) {
	h := &HarnessBuilder{EventsFile: "/tmp/forge-run/events.jsonl"}
	out := h.protocolSection()

	if !strings.Contains(out, "Runtime protocol") {
		t.Fatal("expected protocol header")
	}
	if !strings.Contains(out, "$FORGE_EVENTS_FILE") {
		t.Fatal("expected events file env var reference")
	}
	if !strings.Contains(out, "proposed_memory") {
		t.Fatal("expected proposed_memory event type documented")
	}
}

func TestHarnessBuilderBuildComposesLayers(t *testing.T) {
	dir := t.TempDir()
	kbDir := filepath.Join(dir, "kb")
	_ = os.MkdirAll(kbDir, 0o755)
	_ = os.WriteFile(filepath.Join(kbDir, "memory.md"), []byte("# Memory\n- rule 1"), 0o644)
	_ = os.WriteFile(filepath.Join(dir, "constraints.md"), []byte("# Constraints\n- no force push"), 0o644)

	h := &HarnessBuilder{
		Ctx: ResolvedContext{
			ProjectRoot: dir,
			KBDir:       kbDir,
		},
		Task: RunPlanTask{
			Task:      "Build feature",
			SpecTitle: "User login",
			Repo:      "web",
		},
		WorktreePath: "/tmp/wt",
		BaseBranch:   "main",
		KBContent:    "# Architecture\n- Go CLI",
		EventsFile:   "/tmp/events.jsonl",
	}

	cmd := h.Build()

	// Every layer should be in the output
	if !strings.Contains(cmd, "Non-negotiable constraints") {
		t.Fatal("missing constraints layer")
	}
	if !strings.Contains(cmd, "Project memory") {
		t.Fatal("missing memory layer")
	}
	if !strings.Contains(cmd, "Project knowledge base") {
		t.Fatal("missing KB layer")
	}
	if !strings.Contains(cmd, "Runtime protocol") {
		t.Fatal("missing protocol layer")
	}
	if !strings.Contains(cmd, "Build feature") {
		t.Fatal("missing task")
	}
	if !strings.Contains(cmd, "/tmp/wt") {
		t.Fatal("missing worktree path")
	}
}
