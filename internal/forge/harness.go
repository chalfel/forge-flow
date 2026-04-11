package forge

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// HarnessBuilder composes the full agent context by layering multiple
// sources of state: constraints, memory, project KB, task history, and
// runtime protocol instructions. Each section is produced by a separate
// function so they can be unit-tested and toggled independently.
type HarnessBuilder struct {
	Ctx              ResolvedContext
	Task             RunPlanTask
	WorktreePath     string
	BaseBranch       string
	RunID            string
	KBContent        string
	PastAttempts     []TaskAttemptRecord
	PastFeedback     *StructuredFeedback
	EventsFile       string // absolute path the agent should write events.jsonl to
	BoardSnapshot    string // optional snapshot of the project board for state layer
	SiblingDecisions []Decision
}

// Build returns the shell command string that spawns claude with the full
// layered context injected as --append-system-prompt and -p.
func (h *HarnessBuilder) Build() string {
	systemPrompt := h.composeSystemPrompt()
	taskPrompt := h.composeTaskPrompt()

	escapedSystem := shellEscape(systemPrompt)
	escapedTask := shellEscape(taskPrompt)

	return fmt.Sprintf("cd '%s' && claude --append-system-prompt '%s' -p '%s'",
		shellEscape(h.WorktreePath), escapedSystem, escapedTask)
}

// composeSystemPrompt builds the layered system prompt.
// Order matters: constraints first (highest priority), then memory,
// then project context, then protocol instructions.
func (h *HarnessBuilder) composeSystemPrompt() string {
	sections := []string{}

	if s := h.constraintsSection(); s != "" {
		sections = append(sections, s)
	}
	if s := h.memorySection(); s != "" {
		sections = append(sections, s)
	}
	if s := h.projectContextSection(); s != "" {
		sections = append(sections, s)
	}
	if s := h.stateSection(); s != "" {
		sections = append(sections, s)
	}
	if s := h.protocolSection(); s != "" {
		sections = append(sections, s)
	}

	// Always-present agent role description at the bottom.
	sections = append(sections, "You are a Forge agent working inside a git worktree. Follow the task instructions precisely.")

	return strings.Join(sections, "\n\n---\n\n")
}

// composeTaskPrompt builds the task-specific prompt (the -p flag).
// This is where history gets injected so the agent sees previous attempts
// as part of the task context.
func (h *HarnessBuilder) composeTaskPrompt() string {
	var b strings.Builder
	b.WriteString("You are working on a Forge task. Here is your context:\n\n")

	if h.Task.SpecTitle != "" {
		fmt.Fprintf(&b, "## Spec: %s\n\n", h.Task.SpecTitle)
	}
	fmt.Fprintf(&b, "## Task: %s\n\n", h.Task.Task)

	if len(h.Task.Deps) > 0 {
		fmt.Fprintf(&b, "Dependencies: %s\n\n", strings.Join(h.Task.Deps, ", "))
	}
	if h.Task.Repo != "" {
		fmt.Fprintf(&b, "Repo: %s\n\n", h.Task.Repo)
	}
	if len(h.Task.Touches) > 0 {
		fmt.Fprintf(&b, "Touches: %s\n\n", strings.Join(h.Task.Touches, ", "))
	}

	// History layer: past attempts + latest feedback.
	b.WriteString(h.historySection())

	// Instructions.
	b.WriteString("## Instructions\n\n")
	b.WriteString("1. Implement the task according to the done-when criteria in the spec.\n")
	b.WriteString("2. When finished, commit your changes and create a PR:\n")
	fmt.Fprintf(&b, "   git add -A && git commit -m 'feat: %s'\n", h.Task.Task)
	fmt.Fprintf(&b, "   git push -u origin HEAD\n")
	fmt.Fprintf(&b, "   gh pr create --base %s --title '%s' --fill\n", h.BaseBranch, h.Task.Task)
	b.WriteString("3. After creating the PR, exit.\n")

	return b.String()
}

// constraintsSection reads {projectRoot}/constraints.md if present.
// These are non-negotiable rules every agent must respect.
func (h *HarnessBuilder) constraintsSection() string {
	if h.Ctx.ProjectRoot == "" {
		return ""
	}
	path := filepath.Join(h.Ctx.ProjectRoot, "constraints.md")
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	content := strings.TrimSpace(string(data))
	if content == "" {
		return ""
	}
	return "# Non-negotiable constraints (must follow)\n\n" + content
}

// memorySection reads {projectRoot}/kb/memory.md if present.
// Curated cross-session learnings.
func (h *HarnessBuilder) memorySection() string {
	if h.Ctx.KBDir == "" {
		return ""
	}
	path := filepath.Join(h.Ctx.KBDir, "memory.md")
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	content := strings.TrimSpace(string(data))
	if content == "" {
		return ""
	}
	return "# Project memory (learnings persisted across sessions)\n\n" + content
}

// projectContextSection returns the full KB content (architecture, business,
// conventions, etc.) minus memory.md which is already in its own section.
func (h *HarnessBuilder) projectContextSection() string {
	if h.KBContent == "" {
		return ""
	}
	return "# Project knowledge base\n\n" + h.KBContent
}

// stateSection returns a snapshot of the current project state (board summary)
// so the agent knows what else is happening in the project.
func (h *HarnessBuilder) stateSection() string {
	if h.BoardSnapshot == "" {
		return ""
	}
	return "# Current project state\n\n" + h.BoardSnapshot
}

// historySection returns previous attempts, latest feedback, and sibling
// decisions for this task. Injected into the task prompt (not system prompt)
// so the agent treats it as task-specific context.
func (h *HarnessBuilder) historySection() string {
	hasAttempts := len(h.PastAttempts) > 0
	hasFeedback := h.PastFeedback != nil && len(h.PastFeedback.Items) > 0
	siblings := h.siblingDecisions()
	hasSiblings := len(siblings) > 0

	if !hasAttempts && !hasFeedback && !hasSiblings {
		return ""
	}

	var b strings.Builder

	if len(h.PastAttempts) > 0 {
		b.WriteString("## Previous attempts on this task\n\n")
		b.WriteString("This task has been attempted before. Learn from these outcomes:\n\n")
		for _, attempt := range h.PastAttempts {
			fmt.Fprintf(&b, "- **Attempt %d** (%s): %s", attempt.Attempt, attempt.StartedAt, strings.ToUpper(attempt.Status))
			if attempt.Error != "" {
				fmt.Fprintf(&b, " — %s", attempt.Error)
			}
			b.WriteString("\n")
			if len(attempt.ReviewFeedback) > 0 {
				b.WriteString("  Review feedback on that attempt:\n")
				for _, item := range attempt.ReviewFeedback {
					fmt.Fprintf(&b, "  - [%s] %s", strings.ToUpper(item.Kind), item.Text)
					if item.File != "" {
						fmt.Fprintf(&b, " (%s", item.File)
						if item.Line > 0 {
							fmt.Fprintf(&b, ":%d", item.Line)
						}
						b.WriteString(")")
					}
					b.WriteString("\n")
				}
			}
		}
		b.WriteString("\n**Do not repeat the same mistakes.** Address any feedback above explicitly.\n\n")
	}

	if h.PastFeedback != nil && len(h.PastFeedback.Items) > 0 {
		b.WriteString("## Latest review feedback (must be addressed)\n\n")
		for _, item := range h.PastFeedback.Items {
			fmt.Fprintf(&b, "- **[%s]** %s", strings.ToUpper(item.Kind), item.Text)
			if item.File != "" {
				fmt.Fprintf(&b, " (%s", item.File)
				if item.Line > 0 {
					fmt.Fprintf(&b, ":%d", item.Line)
				}
				b.WriteString(")")
			}
			b.WriteString("\n")
		}
		b.WriteString("\n")
	}

	// Sibling decisions: choices made by other tasks on the same spec.
	// The current task's own decisions are filtered out (agent is the author).
	if len(siblings) > 0 {
		b.WriteString("## Previous decisions on this spec\n\n")
		b.WriteString("Other tasks on this same spec have already made the following architectural choices. Stay consistent with them unless you have a strong reason to diverge:\n\n")
		for _, d := range siblings {
			fmt.Fprintf(&b, "- **%s** (chose by %q): %s", d.Choice, d.TaskName, d.Rationale)
			if len(d.Alternatives) > 0 {
				fmt.Fprintf(&b, " [alternatives considered: %s]", strings.Join(d.Alternatives, ", "))
			}
			b.WriteString("\n")
		}
		b.WriteString("\n")
	}

	return b.String()
}

// siblingDecisions returns the decisions made by OTHER tasks on the same spec,
// filtering out any decisions where TaskName matches the current task.
// If the builder has SiblingDecisions pre-populated, those are used directly.
// Otherwise it tries to load from disk via the service path.
func (h *HarnessBuilder) siblingDecisions() []Decision {
	source := h.SiblingDecisions
	if source == nil && h.Ctx.ProjectRoot != "" && h.Task.File != "" {
		source = loadSpecDecisionsFromCtx(h.Ctx, h.Task.File)
	}
	if len(source) == 0 {
		return nil
	}

	var out []Decision
	for _, d := range source {
		if strings.EqualFold(strings.TrimSpace(d.TaskName), strings.TrimSpace(h.Task.Task)) {
			continue // the agent is the author of this decision, skip
		}
		out = append(out, d)
	}
	return out
}

// loadSpecDecisionsFromCtx is a package-private helper to read decisions
// for a spec directly from the ResolvedContext, without requiring a Service.
// It mirrors the Service.LoadDecisionsForSpec logic but operates on the
// already-resolved context, avoiding a reverse lookup.
func loadSpecDecisionsFromCtx(ctx ResolvedContext, specFile string) []Decision {
	specSlug := slugifySpecFile(specFile)
	dir := filepath.Join(historyRoot(ctx), "decisions", specSlug)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var all []Decision
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".jsonl") {
			continue
		}
		decisions, err := loadDecisionsFromFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			continue
		}
		all = append(all, decisions...)
	}
	return all
}

// protocolSection instructs the agent how to communicate back to Forge
// via the events.jsonl file. Only included when EventsFile is set.
func (h *HarnessBuilder) protocolSection() string {
	if h.EventsFile == "" {
		return ""
	}
	return `# Runtime protocol

You can report structured events back to Forge by writing JSON lines to a file.
The path is set in environment variable $FORGE_EVENTS_FILE (already set for you).

Write one JSON object per line, each with fields:
- type: one of "progress", "decision", "blocked", "need_info", "handoff", "proposed_memory"
- message: human-readable message
- timestamp: RFC3339 timestamp
- details: optional type-specific payload

Examples:
  echo '{"type":"progress","message":"Scaffolded types","timestamp":"2026-04-10T12:00:00Z"}' >> "$FORGE_EVENTS_FILE"
  echo '{"type":"decision","message":"Using Zod for validation","timestamp":"2026-04-10T12:05:00Z","details":{"alternatives":["yup","joi"],"rationale":"Better TypeScript inference"}}' >> "$FORGE_EVENTS_FILE"
  echo '{"type":"blocked","message":"Cannot resolve import — scaffold task not done","timestamp":"2026-04-10T12:10:00Z"}' >> "$FORGE_EVENTS_FILE"
  echo '{"type":"proposed_memory","message":"Supabase migrations must regenerate types after","timestamp":"2026-04-10T12:20:00Z"}' >> "$FORGE_EVENTS_FILE"

Use these events to:
- Report progress (Forge shows in real-time on the board)
- Record significant decisions (auto-saved to decision journal)
- Signal blockers so the operator can intervene
- Propose new entries for project memory (goes to inbox.md for human review)`
}
