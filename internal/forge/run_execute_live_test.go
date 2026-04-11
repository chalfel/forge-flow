package forge

import (
	"strings"
	"testing"
)

func TestBuildClaudeCommand(t *testing.T) {
	task := RunPlanTask{
		Task:      "Add login button",
		SpecTitle: "User can sign in with GitHub",
		Repo:      "web",
		Touches:   []string{"app/login/**"},
		Deps:      []string{"Shared auth contract"},
	}
	kbContent := "# Architecture\nGo CLI + React frontend."

	cmd := BuildClaudeCommand("/tmp/worktree", "/project/root", task, kbContent, "forge/test/shared-auth-contract")

	if !strings.Contains(cmd, "/tmp/worktree") || !strings.Contains(cmd, "claude") {
		t.Fatalf("expected command with worktree path and claude, got: %s", cmd[:80])
	}
	if !strings.Contains(cmd, "--append-system-prompt") {
		t.Fatal("expected --append-system-prompt flag")
	}
	if !strings.Contains(cmd, "-p") {
		t.Fatal("expected -p flag for prompt")
	}
	if !strings.Contains(cmd, "Add login button") {
		t.Fatal("expected task name in prompt")
	}
	if !strings.Contains(cmd, "forge/test/shared-auth-contract") {
		t.Fatal("expected base branch in PR instructions")
	}
}

func TestBuildClaudeCommandNoKB(t *testing.T) {
	task := RunPlanTask{
		Task:      "Simple task",
		SpecTitle: "Simple spec",
	}

	cmd := BuildClaudeCommand("/tmp/wt", "/project", task, "", "main")
	if !strings.Contains(cmd, "/tmp/wt") || !strings.Contains(cmd, "claude") {
		t.Fatalf("unexpected command: %s", cmd[:60])
	}
}

func TestShellEscape(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"hello", "hello"},
		{"it's a test", "it'\\''s a test"},
		{"no quotes", "no quotes"},
	}
	for _, tt := range tests {
		got := shellEscape(tt.input)
		if got != tt.want {
			t.Errorf("shellEscape(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestBucketForTask(t *testing.T) {
	if bucketForTask(RunPlanTask{Parallelizable: "yes"}) != "safe" {
		t.Fatal("expected safe for parallelizable=yes")
	}
	if bucketForTask(RunPlanTask{Parallelizable: "no"}) != "serial" {
		t.Fatal("expected serial for parallelizable=no")
	}
}

func TestConcatPlanTasks(t *testing.T) {
	plan := RunPlan{
		Safe:   []RunPlanTask{{Task: "a"}},
		Serial: []RunPlanTask{{Task: "b"}},
		Blocked: []RunPlanTask{{Task: "c"}},
	}
	all := concatPlanTasks(plan)
	if len(all) != 3 {
		t.Fatalf("expected 3 tasks, got %d", len(all))
	}
}

func TestCollectKBContentEmpty(t *testing.T) {
	content := collectKBContent("")
	if content != "" {
		t.Fatalf("expected empty for no KB dir, got %q", content)
	}
}

func TestCollectKBContentNonexistent(t *testing.T) {
	content := collectKBContent("/nonexistent/path")
	if content != "" {
		t.Fatalf("expected empty for nonexistent path, got %q", content)
	}
}
