package forge

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// SpecGenerateResult is the output of a spec generation run.
type SpecGenerateResult struct {
	Output   string `json:"output"`
	SpecPath string `json:"specPath,omitempty"`
	PaneID   string `json:"paneId,omitempty"`
}

// GenerateSpec runs a Claude agent to generate a Forge spec from a
// capability description. By default runs in the foreground so the user
// sees the agent working. When background=true, spawns in a tmux window.
func (s *Service) GenerateSpec(ctx context.Context, cwd, capability string, background bool) (SpecGenerateResult, error) {
	if err := EnsureClaudeAvailable(); err != nil {
		return SpecGenerateResult{}, err
	}

	resolvedCtx, err := s.ResolveContext(cwd)
	if err != nil {
		return SpecGenerateResult{}, err
	}

	// Build system prompt from KB + skill + scaffolding rules.
	systemPrompt := buildSpecGenSystemPrompt(resolvedCtx)

	// Build the task prompt.
	taskPrompt := buildSpecGenTaskPrompt(resolvedCtx, capability)

	// Resolve the working directory (repo root or cwd).
	workDir := resolvedCtx.RepoRoot
	if workDir == "" {
		workDir, _ = os.Getwd()
	}

	if background {
		if err := EnsureTmuxAvailable(); err != nil {
			return SpecGenerateResult{}, fmt.Errorf("--background requires tmux: %w", err)
		}
		return s.generateSpecInTmux(ctx, workDir, systemPrompt, taskPrompt, resolvedCtx)
	}

	// Default: foreground — user sees the agent working.
	return s.generateSpecForeground(workDir, systemPrompt, taskPrompt, resolvedCtx)
}

func (s *Service) generateSpecInTmux(ctx context.Context, workDir, systemPrompt, taskPrompt string, resolvedCtx ResolvedContext) (SpecGenerateResult, error) {
	sessionName, insideTmux := DetectTmux()
	if !insideTmux {
		sessionName = "forge-spec-gen"
		if err := CreateTmuxSession(sessionName); err != nil {
			return SpecGenerateResult{}, err
		}
	}

	cmd := fmt.Sprintf("cd %s && claude --append-system-prompt '%s' -p '%s'",
		workDir, shellEscape(systemPrompt), shellEscape(taskPrompt))

	paneID, err := NewTmuxWindow(sessionName, "spec-generate")
	if err != nil {
		return SpecGenerateResult{}, fmt.Errorf("creating tmux window: %w", err)
	}
	if err := SendKeys(paneID, cmd); err != nil {
		return SpecGenerateResult{}, fmt.Errorf("sending command: %w", err)
	}

	if !insideTmux {
		fmt.Printf("Tmux session ready: tmux attach -t %s\n", sessionName)
	}

	return SpecGenerateResult{
		Output: fmt.Sprintf("Spec generation agent spawned in tmux (pane: %s, session: %s).\n"+
			"The agent will create the spec in %s/specs/ and auto-lint it.\n",
			paneID, sessionName, resolvedCtx.SpecsDir),
		PaneID: paneID,
	}, nil
}

// generateSpecForeground runs claude interactively in the current terminal.
// The user sees the agent thinking and writing in real-time.
func (s *Service) generateSpecForeground(workDir, systemPrompt, taskPrompt string, resolvedCtx ResolvedContext) (SpecGenerateResult, error) {
	claudePath, err := exec.LookPath("claude")
	if err != nil {
		return SpecGenerateResult{}, fmt.Errorf("claude not found: %w", err)
	}

	// Run claude interactively with the prompt as the initial message.
	// NOT using -p (print mode) because that suppresses the interactive UI.
	cmd := exec.Command(claudePath,
		"--append-system-prompt", systemPrompt,
		taskPrompt,
	)
	cmd.Dir = workDir
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return SpecGenerateResult{}, fmt.Errorf("claude exited with error: %w", err)
	}

	return SpecGenerateResult{
		Output: fmt.Sprintf("Spec generation complete. Check %s for the new spec.\n", resolvedCtx.SpecsDir),
	}, nil
}

func buildSpecGenSystemPrompt(ctx ResolvedContext) string {
	var b strings.Builder

	b.WriteString("You are a Forge spec generator. You create AI-native task definitions (specs) in Forge markdown format.\n\n")

	// Inject KB.
	kbContent := collectKBContent(ctx.KBDir)
	if kbContent != "" {
		b.WriteString("# Project Knowledge Base\n\n")
		b.WriteString(kbContent)
		b.WriteString("\n\n")
	}

	// Inject the scaffolding pattern rule.
	b.WriteString(`# Critical Rule: Scaffolding Pattern

When a spec creates a NEW module, feature area, or directory that doesn't exist yet:
- The FIRST task MUST be a serial scaffolding task (parallelizable: no, deps: none)
- This task sets up the directory structure, shared types, interfaces, and test helpers
- ALL other tasks in that module MUST depend on this scaffolding task
- This prevents parallel agents from creating the same files in separate worktrees

Example:
### Task: Scaffold payment module structure
<!-- parallelizable: no -->
<!-- deps: none -->

### Task: Add payment form
<!-- parallelizable: yes -->
<!-- deps: Scaffold payment module structure -->

### Task: Add payment API
<!-- parallelizable: yes -->
<!-- deps: Scaffold payment module structure -->

`)

	// Inject the spec format rules.
	b.WriteString(`# Spec Format Rules

- One # capability title
- One **Demo:** line (user-facing, not technical)
- ## Expected behavior with bullets
- ## Test cases with ### Scenario: headings and Given/When/Then steps
- ## Validation plan
- 2-5 ### Task: sections
- Every task: status, parallelizable, deps, repo, conflict-risk, touches
- Every task: **Done when:** with bullets
- Every task: **Validation:** with Covers: Scenario references
- Branch format: <!-- branch: forge/{random-adj}-{random-noun}-{random-3digits} -->

`)

	// Inject existing specs list for context.
	specFiles, _ := collectSpecFiles(ctx.SpecsDir)
	if len(specFiles) > 0 {
		b.WriteString("# Existing Specs\n\n")
		for _, f := range specFiles {
			b.WriteString("- ")
			b.WriteString(filepath.Base(f))
			b.WriteString("\n")
		}
		b.WriteString("\n")
	}

	// Lint instruction.
	b.WriteString("After creating the spec file, run: forge spec lint --cwd . --spec {spec-path}\n")
	b.WriteString("Fix any errors before finishing.\n")

	return b.String()
}

func buildSpecGenTaskPrompt(ctx ResolvedContext, capability string) string {
	var b strings.Builder

	fmt.Fprintf(&b, "Create a Forge spec for this capability: %s\n\n", capability)
	fmt.Fprintf(&b, "Save the spec to: %s/{date}-{slug}.md\n", ctx.SpecsDir)
	b.WriteString("Use today's date in the filename.\n\n")
	b.WriteString("Follow the spec format rules in your system prompt exactly.\n")
	b.WriteString("Apply the scaffolding pattern when the spec touches a new module.\n")
	b.WriteString("After saving, run 'forge spec lint --cwd . --spec {path}' and fix any issues.\n")

	return b.String()
}
