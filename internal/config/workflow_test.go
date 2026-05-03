package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeTemp(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "WORKFLOW.md")
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatalf("write temp: %v", err)
	}
	return p
}

func TestLoad_AppliesDefaults(t *testing.T) {
	t.Setenv("LINEAR_API_KEY", "lin_test")
	p := writeTemp(t, `---
tracker:
  kind: linear
  project_slug: ABC
  api_key: $LINEAR_API_KEY
  active_states: [Todo]
codex:
  command: codex app-server
---
# Body
Hello {{ issue.title }}
`)
	wf, err := Load(p)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if wf.Polling.IntervalMs != defaultPollingIntervalMs {
		t.Errorf("default interval not applied: got %d", wf.Polling.IntervalMs)
	}
	if wf.Agent.MaxConcurrentAgents != defaultMaxConcurrent {
		t.Errorf("default max_concurrent not applied: got %d", wf.Agent.MaxConcurrentAgents)
	}
	if wf.Tracker.APIKey != "lin_test" {
		t.Errorf("env var not resolved: got %q", wf.Tracker.APIKey)
	}
	if !strings.Contains(wf.PromptBody, "Hello {{ issue.title }}") {
		t.Errorf("prompt body lost: %q", wf.PromptBody)
	}
	if !filepath.IsAbs(wf.Workspace.Root) {
		t.Errorf("workspace root not absolutized: %q", wf.Workspace.Root)
	}
}

func TestLoad_NoFrontMatter(t *testing.T) {
	p := writeTemp(t, "no front matter, just body\n")
	wf, err := Load(p)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if !strings.Contains(wf.PromptBody, "no front matter") {
		t.Errorf("body wrong: %q", wf.PromptBody)
	}
}

func TestLoad_UnclosedFrontMatter(t *testing.T) {
	p := writeTemp(t, "---\ntracker:\n  kind: linear\n")
	if _, err := Load(p); err == nil {
		t.Fatalf("expected error for unclosed front matter")
	}
}

func TestValidate_LinearMissingSlug(t *testing.T) {
	t.Setenv("LINEAR_API_KEY", "lin_test")
	p := writeTemp(t, `---
tracker:
  kind: linear
  api_key: $LINEAR_API_KEY
  active_states: [Todo]
codex:
  command: codex app-server
---
body
`)
	wf, err := Load(p)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	err = wf.Validate()
	if err == nil {
		t.Fatalf("expected validation error")
	}
	if !IsValidationError(err) {
		t.Fatalf("expected ValidationError, got %T", err)
	}
	if !strings.Contains(err.Error(), "project_slug") {
		t.Errorf("expected project_slug message, got %v", err)
	}
}

func TestValidate_GitHubRequiresRepo(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "ghp_test")
	p := writeTemp(t, `---
tracker:
  kind: github
  api_key: $GITHUB_TOKEN
  active_states: [open]
codex:
  command: codex app-server
---
body
`)
	wf, _ := Load(p)
	err := wf.Validate()
	if err == nil || !strings.Contains(err.Error(), "tracker.repo required") {
		t.Fatalf("expected repo required error, got %v", err)
	}
}

func TestValidate_OK(t *testing.T) {
	t.Setenv("LINEAR_API_KEY", "lin_test")
	p := writeTemp(t, `---
tracker:
  kind: linear
  project_slug: ABC
  api_key: $LINEAR_API_KEY
  active_states: [Todo, In Progress]
codex:
  command: codex app-server
---
body
`)
	wf, err := Load(p)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if err := wf.Validate(); err != nil {
		t.Fatalf("expected OK, got %v", err)
	}
}

func TestValidate_ClaudeCodeAgent(t *testing.T) {
	t.Setenv("LINEAR_API_KEY", "lin_test")
	p := writeTemp(t, `---
tracker:
  kind: linear
  project_slug: ABC
  api_key: $LINEAR_API_KEY
  active_states: [Todo]
agent:
  kind: claude_code
claude_code:
  command: claude --print
---
body
`)
	wf, err := Load(p)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if err := wf.Validate(); err != nil {
		t.Fatalf("expected OK, got %v", err)
	}
	if wf.AgentCommandFor().Command != "claude --print" {
		t.Errorf("agent command resolution wrong: %q", wf.AgentCommandFor().Command)
	}
}
