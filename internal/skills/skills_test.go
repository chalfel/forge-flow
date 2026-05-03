package skills

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeSkill(t *testing.T, root, name, md string, scripts ...string) {
	t.Helper()
	dir := filepath.Join(root, relSkillsRoot, name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if md != "" {
		if err := os.WriteFile(filepath.Join(dir, skillFile), []byte(md), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for _, s := range scripts {
		if err := os.WriteFile(filepath.Join(dir, s), []byte("#!/bin/sh\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
}

func TestDiscover_MissingDirReturnsNilNil(t *testing.T) {
	got, err := Discover(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if got != nil {
		t.Fatalf("expected nil skills, got %+v", got)
	}
}

func TestDiscover_ParsesDescriptionAndScripts(t *testing.T) {
	root := t.TempDir()
	writeSkill(t, root, "grafana", `# Grafana logs

Fetch production logs from Grafana for any service or trace ID.

## Usage
Run fetch-logs.sh "<query>".`, "fetch-logs.sh")
	writeSkill(t, root, "playwright-video", `# Playwright video evidence
Record an annotated browser session and upload it to the ticket.`, "record-start.sh", "record-stop.sh")

	got, err := Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 skills, got %d", len(got))
	}
	// Sorted alphabetically.
	if got[0].Name != "grafana" || got[1].Name != "playwright-video" {
		t.Errorf("ordering wrong: %v, %v", got[0].Name, got[1].Name)
	}
	if !strings.Contains(got[0].Description, "Fetch production logs") {
		t.Errorf("description wrong: %q", got[0].Description)
	}
	if len(got[1].Scripts) != 2 || got[1].Scripts[0] != "record-start.sh" {
		t.Errorf("scripts wrong: %v", got[1].Scripts)
	}
}

func TestRender_EmptyAndPopulated(t *testing.T) {
	got := Render(nil)
	if !strings.Contains(got, "No skills configured") {
		t.Errorf("empty render: %q", got)
	}

	got = Render([]Skill{{
		Name:        "grafana",
		Path:        "/repo/.symphony/skills/grafana",
		Description: "fetch logs",
		Scripts:     []string{"fetch.sh"},
	}})
	for _, want := range []string{"grafana", "fetch logs", "fetch.sh", "/repo/.symphony/skills/grafana"} {
		if !strings.Contains(got, want) {
			t.Errorf("rendered missing %q in:\n%s", want, got)
		}
	}
}

func TestFirstParagraph_CapsLength(t *testing.T) {
	s := strings.Repeat("a ", 200)
	got := firstParagraph(s)
	if len(got) > 240 {
		t.Fatalf("expected <=240 chars, got %d", len(got))
	}
	if !strings.HasSuffix(got, "...") {
		t.Errorf("expected trailing ellipsis, got %q", got)
	}
}

func TestDiscover_IgnoresFiles(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, relSkillsRoot), 0o755); err != nil {
		t.Fatal(err)
	}
	// A regular file at the skills root should be ignored.
	_ = os.WriteFile(filepath.Join(root, relSkillsRoot, "stray.txt"), []byte("x"), 0o644)
	got, err := Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("expected 0 skills, got %v", got)
	}
}
