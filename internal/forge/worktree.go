package forge

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// TaskBranchName computes the git branch name for a task.
// Format: {specBranch}/{slugified-task-name}
func TaskBranchName(specBranch, taskName string) string {
	slug := slugify(taskName)
	if specBranch == "" {
		return slug
	}
	return specBranch + "/" + slug
}

// DefaultSpecBranch generates a branch name from the spec title
// when <!-- branch: --> is not declared.
func DefaultSpecBranch(specTitle string) string {
	slug := slugify(specTitle)
	if slug == "" {
		return "forge/run"
	}
	return "forge/" + slug
}

// WorktreePath computes the worktree directory path.
// Format: {repoRoot}/.worktrees/{slugified-task-name}
func WorktreePath(repoRoot, taskName string) string {
	return filepath.Join(repoRoot, ".worktrees", slugify(taskName))
}

// CreateWorktree creates a git worktree for a task.
// For independent tasks, depBranch should be empty.
// For dependent tasks (stacked PRs), depBranch is the dependency's branch.
// Returns the worktree path and branch name.
func CreateWorktree(repoRoot, specBranch, taskName, depBranch string) (string, string, error) {
	if specBranch == "" {
		return "", "", fmt.Errorf("spec branch is required to create a worktree")
	}

	branchName := TaskBranchName(specBranch, taskName)
	wtPath := WorktreePath(repoRoot, taskName)

	if _, err := os.Stat(wtPath); err == nil {
		return wtPath, branchName, nil // already exists, reuse
	}

	if err := os.MkdirAll(filepath.Dir(wtPath), 0o755); err != nil {
		return "", "", fmt.Errorf("creating worktrees directory: %w", err)
	}

	var args []string
	if depBranch != "" {
		// Stacked: branch from the dependency branch
		args = []string{"-C", repoRoot, "worktree", "add", wtPath, depBranch, "-b", branchName}
	} else {
		// Independent: branch from current HEAD
		args = []string{"-C", repoRoot, "worktree", "add", wtPath, "-b", branchName}
	}

	out, err := shellRun("git", args...)
	if err != nil {
		return "", "", fmt.Errorf("creating worktree for %q: %s (%w)", taskName, strings.TrimSpace(string(out)), err)
	}

	return wtPath, branchName, nil
}

// RemoveWorktree removes a git worktree.
func RemoveWorktree(repoRoot, wtPath string) error {
	out, err := shellRun("git", "-C", repoRoot, "worktree", "remove", wtPath, "--force")
	if err != nil {
		return fmt.Errorf("removing worktree %s: %s (%w)", wtPath, strings.TrimSpace(string(out)), err)
	}
	return nil
}

// ResolveDepBranch determines the base branch for a dependent task.
// If the task has dependencies, returns the branch name of the first dep.
// Returns empty string for independent tasks (base = main).
func ResolveDepBranch(specBranch string, deps []string, taskBranchMap map[string]string) string {
	if len(deps) == 0 {
		return ""
	}
	// Use the first dependency's branch as the base.
	// For multiple deps, they should all be done already, so pick the first.
	for _, dep := range deps {
		if branch, ok := taskBranchMap[strings.ToLower(strings.TrimSpace(dep))]; ok {
			return branch
		}
	}
	return ""
}

// ResolveRepoRoot finds the local path for a repo ID from the resolved context.
func ResolveRepoRoot(ctx ResolvedContext, repoID string) (string, error) {
	if repoID == "" {
		if ctx.RepoRoot != "" {
			return ctx.RepoRoot, nil
		}
		return "", fmt.Errorf("no repo ID specified and no default repo root available")
	}
	for _, repo := range ctx.Repos {
		if strings.EqualFold(repo.RepoID, repoID) {
			if repo.LocalPath != "" {
				return repo.LocalPath, nil
			}
		}
	}
	// Fall back to current repo if it matches
	if strings.EqualFold(ctx.RepoID, repoID) && ctx.RepoRoot != "" {
		return ctx.RepoRoot, nil
	}
	return "", fmt.Errorf("repo %q not found in registered repos", repoID)
}
