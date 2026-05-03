package scheduler

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/chalfel/forge-flow/internal/domain"
)

func TestRenderPrompt_BasicSubstitution(t *testing.T) {
	body := "Issue {{ issue.identifier }}: {{ issue.title }} (attempt {{ attempt }})"
	out := renderPrompt(body, domain.Issue{
		ID:         "id1",
		Identifier: "ABC-7",
		Title:      "Add dark mode",
	}, 2, "")
	want := "Issue ABC-7: Add dark mode (attempt 2)"
	if out != want {
		t.Fatalf("want %q, got %q", want, out)
	}
}

func TestRenderPrompt_UnknownPlaceholderLeftIntact(t *testing.T) {
	body := "Hello {{ issue.title }} and {{ unknown.field }}"
	out := renderPrompt(body, domain.Issue{Title: "x"}, 0, "")
	if !strings.Contains(out, "{{ unknown.field }}") {
		t.Fatalf("expected unknown placeholder to remain, got %q", out)
	}
}

func TestRenderPrompt_LabelsJoined(t *testing.T) {
	body := "{{ issue.labels }}"
	out := renderPrompt(body, domain.Issue{Labels: []string{"bug", "urgent"}}, 0, "")
	if out != "bug,urgent" {
		t.Fatalf("got %q", out)
	}
}

func TestRenderPrompt_SkillsInventoryInjected(t *testing.T) {
	root := t.TempDir()
	skillDir := filepath.Join(root, ".symphony", "skills", "grafana")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"),
		[]byte("# Grafana\nFetch logs from production."), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "fetch.sh"), []byte("#!/bin/sh"), 0o755); err != nil {
		t.Fatal(err)
	}

	body := "## Skills\n{{ skills }}"
	out := renderPrompt(body, domain.Issue{}, 0, root)
	for _, want := range []string{"grafana", "Fetch logs", "fetch.sh"} {
		if !strings.Contains(out, want) {
			t.Errorf("rendered missing %q in:\n%s", want, out)
		}
	}
}

func TestRenderPrompt_SkillsEmptyWhenNoneConfigured(t *testing.T) {
	body := "{{ skills }}"
	out := renderPrompt(body, domain.Issue{}, 0, t.TempDir())
	if !strings.Contains(out, "No skills configured") {
		t.Fatalf("expected empty-skills note, got %q", out)
	}
}
