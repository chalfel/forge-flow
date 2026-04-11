package forge

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTaskBranchName(t *testing.T) {
	tests := []struct {
		specBranch string
		taskName   string
		want       string
	}{
		{"forge/steady-orbit-114", "Add run execution models", "forge/steady-orbit-114/add-run-execution-models"},
		{"forge/steady-orbit-114", "Add CLI flow for `forge run`", "forge/steady-orbit-114/add-cli-flow-for-forge-run"},
		{"forge/test", "Simple", "forge/test/simple"},
		{"", "Orphan task", "orphan-task"},
	}

	for _, tt := range tests {
		got := TaskBranchName(tt.specBranch, tt.taskName)
		if got != tt.want {
			t.Errorf("TaskBranchName(%q, %q) = %q, want %q", tt.specBranch, tt.taskName, got, tt.want)
		}
	}
}

func TestDefaultSpecBranch(t *testing.T) {
	tests := []struct {
		specTitle string
		want      string
	}{
		{"Forge can execute a run plan end-to-end", "forge/forge-can-execute-a-run-plan-end-to-end"},
		{"", "forge/run"},
	}
	for _, tt := range tests {
		got := DefaultSpecBranch(tt.specTitle)
		if got != tt.want {
			t.Errorf("DefaultSpecBranch(%q) = %q, want %q", tt.specTitle, got, tt.want)
		}
	}
}

func TestWorktreePath(t *testing.T) {
	got := WorktreePath("/home/user/repo", "Add run execution models")
	want := filepath.Join("/home/user/repo", ".worktrees", "add-run-execution-models")
	if got != want {
		t.Errorf("WorktreePath = %q, want %q", got, want)
	}
}

func TestCreateWorktreeIndependent(t *testing.T) {
	repoRoot := setupGitRepo(t)

	wtPath, branch, err := CreateWorktree(repoRoot, "forge/test", "My task", "")
	if err != nil {
		t.Fatalf("CreateWorktree: %v", err)
	}

	if !strings.Contains(wtPath, ".worktrees") {
		t.Fatalf("expected worktree path under .worktrees, got %s", wtPath)
	}
	if branch != "forge/test/my-task" {
		t.Fatalf("expected branch forge/test/my-task, got %s", branch)
	}
	if _, err := os.Stat(wtPath); err != nil {
		t.Fatalf("worktree directory should exist: %v", err)
	}
}

func TestCreateWorktreeDependent(t *testing.T) {
	repoRoot := setupGitRepo(t)

	// First create the base branch
	_, baseBranch, err := CreateWorktree(repoRoot, "forge/test", "Base task", "")
	if err != nil {
		t.Fatalf("CreateWorktree base: %v", err)
	}

	// Create dependent worktree stacked on the base
	wtPath, branch, err := CreateWorktree(repoRoot, "forge/test", "Dependent task", baseBranch)
	if err != nil {
		t.Fatalf("CreateWorktree dependent: %v", err)
	}

	if branch != "forge/test/dependent-task" {
		t.Fatalf("expected branch forge/test/dependent-task, got %s", branch)
	}
	if _, err := os.Stat(wtPath); err != nil {
		t.Fatalf("dependent worktree directory should exist: %v", err)
	}
}

func TestCreateWorktreeReusesExisting(t *testing.T) {
	repoRoot := setupGitRepo(t)

	wtPath1, _, err := CreateWorktree(repoRoot, "forge/test", "Reuse task", "")
	if err != nil {
		t.Fatalf("CreateWorktree first: %v", err)
	}

	wtPath2, _, err := CreateWorktree(repoRoot, "forge/test", "Reuse task", "")
	if err != nil {
		t.Fatalf("CreateWorktree second: %v", err)
	}

	if wtPath1 != wtPath2 {
		t.Fatalf("expected same worktree path, got %s and %s", wtPath1, wtPath2)
	}
}

func TestCreateWorktreeRequiresSpecBranch(t *testing.T) {
	_, _, err := CreateWorktree("/tmp", "", "Some task", "")
	if err == nil {
		t.Fatalf("expected error when spec branch is empty")
	}
}

func TestResolveDepBranch(t *testing.T) {
	taskBranchMap := map[string]string{
		"task a": "forge/test/task-a",
		"task b": "forge/test/task-b",
	}

	// With deps
	got := ResolveDepBranch("forge/test", []string{"Task A"}, taskBranchMap)
	if got != "forge/test/task-a" {
		t.Fatalf("expected forge/test/task-a, got %s", got)
	}

	// No deps
	got = ResolveDepBranch("forge/test", nil, taskBranchMap)
	if got != "" {
		t.Fatalf("expected empty for no deps, got %s", got)
	}

	// Unknown dep
	got = ResolveDepBranch("forge/test", []string{"Unknown"}, taskBranchMap)
	if got != "" {
		t.Fatalf("expected empty for unknown dep, got %s", got)
	}
}

func TestResolveRepoRoot(t *testing.T) {
	ctx := ResolvedContext{
		RepoID:   "core",
		RepoRoot: "/home/user/core-repo",
		Repos: []RepoSummary{
			{RepoID: "web", LocalPath: "/home/user/web-repo"},
			{RepoID: "api", LocalPath: "/home/user/api-repo"},
		},
	}

	// Registered repo
	root, err := ResolveRepoRoot(ctx, "web")
	if err != nil {
		t.Fatalf("ResolveRepoRoot web: %v", err)
	}
	if root != "/home/user/web-repo" {
		t.Fatalf("expected /home/user/web-repo, got %s", root)
	}

	// Current repo fallback
	root, err = ResolveRepoRoot(ctx, "core")
	if err != nil {
		t.Fatalf("ResolveRepoRoot core: %v", err)
	}
	if root != "/home/user/core-repo" {
		t.Fatalf("expected /home/user/core-repo, got %s", root)
	}

	// Empty repo ID defaults to current repo
	root, err = ResolveRepoRoot(ctx, "")
	if err != nil {
		t.Fatalf("ResolveRepoRoot empty: %v", err)
	}
	if root != "/home/user/core-repo" {
		t.Fatalf("expected /home/user/core-repo, got %s", root)
	}

	// Unknown repo
	_, err = ResolveRepoRoot(ctx, "unknown")
	if err == nil {
		t.Fatalf("expected error for unknown repo")
	}
}

// setupGitRepo creates a temporary git repo with an initial commit.
func setupGitRepo(t *testing.T) string {
	t.Helper()
	origRun := shellRun
	shellRun = defaultShellRun
	t.Cleanup(func() { shellRun = origRun })

	dir := t.TempDir()
	for _, args := range [][]string{
		{"git", "-C", dir, "init"},
		{"git", "-C", dir, "config", "user.email", "test@test.com"},
		{"git", "-C", dir, "config", "user.name", "Test"},
	} {
		if out, err := defaultShellRun(args[0], args[1:]...); err != nil {
			t.Fatalf("git setup %v: %s (%v)", args, string(out), err)
		}
	}
	// Create initial commit
	readmePath := filepath.Join(dir, "README.md")
	if err := os.WriteFile(readmePath, []byte("# test\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"git", "-C", dir, "add", "."},
		{"git", "-C", dir, "commit", "-m", "initial"},
	} {
		if out, err := defaultShellRun(args[0], args[1:]...); err != nil {
			t.Fatalf("git commit %v: %s (%v)", args, string(out), err)
		}
	}
	return dir
}
