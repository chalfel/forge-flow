package forge

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// ProjectAnalysis is Claude's narrative health assessment for a project,
// persisted on disk so the dashboard can render it without re-running.
type ProjectAnalysis struct {
	Markdown    string `json:"markdown"`
	GeneratedAt string `json:"generatedAt"`
	Model       string `json:"model,omitempty"`
}

// analysisCachePath returns the canonical location for a project's cached
// Claude analysis.
func analysisCachePath(ctx ResolvedContext) string {
	base := ctx.RepoLocalForge
	if base == "" {
		base = filepath.Join(ctx.ProjectRoot, ".forge")
	}
	return filepath.Join(base, "runtime", "analysis.json")
}

// LoadCachedAnalysis reads the cached analysis for the current project.
// Returns (nil, nil) if no cache exists yet — that's an expected state, not
// an error.
func (s *Service) LoadCachedAnalysis(cwd string) (*ProjectAnalysis, error) {
	ctx, err := s.ResolveContext(cwd)
	if err != nil {
		return nil, err
	}
	path := analysisCachePath(ctx)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var analysis ProjectAnalysis
	if err := json.Unmarshal(data, &analysis); err != nil {
		return nil, err
	}
	return &analysis, nil
}

// RunProjectAnalysis composes a milestone-focused prompt from the dashboard
// state and calls `claude -p` to produce a narrative health analysis. The
// result is persisted to disk so subsequent loads can show it immediately.
func (s *Service) RunProjectAnalysis(cwd string) (*ProjectAnalysis, error) {
	if err := EnsureClaudeAvailable(); err != nil {
		return nil, err
	}

	ctx, err := s.ResolveContext(cwd)
	if err != nil {
		return nil, err
	}

	view, err := s.Dashboard(cwd)
	if err != nil {
		return nil, err
	}

	// Also grab board-level inbox for context.
	board, _ := s.Board(cwd)

	roadmapContent := ""
	if data, err := os.ReadFile(filepath.Join(ctx.KBDir, "roadmap.md")); err == nil {
		roadmapContent = string(data)
	}

	prompt := composeAnalysisPrompt(view, roadmapContent, board)

	out, err := shellRun("claude", "-p", prompt)
	if err != nil {
		return nil, fmt.Errorf("running claude: %s (%w)", strings.TrimSpace(string(out)), err)
	}

	analysis := &ProjectAnalysis{
		Markdown:    strings.TrimSpace(string(out)),
		GeneratedAt: timeNow().UTC().Format(time.RFC3339),
		Model:       "claude",
	}

	cachePath := analysisCachePath(ctx)
	if err := os.MkdirAll(filepath.Dir(cachePath), 0o755); err != nil {
		return nil, err
	}
	data, _ := json.MarshalIndent(analysis, "", "  ")
	if err := os.WriteFile(cachePath, data, 0o644); err != nil {
		return nil, err
	}

	return analysis, nil
}

// composeAnalysisPrompt builds a focused prompt asking Claude to report on
// milestone progress, blockers, and the next ship. Uses raw roadmap text +
// compact spec state so Claude can correlate intent with execution.
func composeAnalysisPrompt(view DashboardView, roadmapContent string, board Board) string {
	var b strings.Builder

	b.WriteString("You are analyzing a spec-driven AI agent project called ")
	b.WriteString(view.ProjectName)
	b.WriteString(".\n\n")
	b.WriteString("Your job is to assess ROADMAP MILESTONE PROGRESS — not generic project health.\n")
	b.WriteString("Focus on: which milestone are we in (Now/Next/Later), how far along, what's blocking it, what ships next.\n\n")

	b.WriteString("# Roadmap (from kb/roadmap.md)\n\n")
	if strings.TrimSpace(roadmapContent) != "" {
		b.WriteString(roadmapContent)
	} else {
		b.WriteString("(no roadmap found)\n")
	}
	b.WriteString("\n\n")

	b.WriteString("# Spec states\n\n")
	if len(view.SpecSummaries) == 0 {
		b.WriteString("(no specs)\n")
	} else {
		for _, sp := range view.SpecSummaries {
			fmt.Fprintf(&b, "- **%s** [%s] — %d/%d tasks done\n", sp.Title, sp.Status, sp.DoneTasks, sp.TotalTasks)
		}
	}
	b.WriteString("\n")

	b.WriteString("# Parsed milestones (bullet -> matched spec)\n\n")
	if len(view.Milestones) == 0 {
		b.WriteString("(roadmap has no Now/Next/Later sections)\n")
	} else {
		for _, m := range view.Milestones {
			fmt.Fprintf(&b, "## %s (%d/%d done)\n", m.Name, m.DoneCount, m.Total)
			for _, item := range m.Bullets {
				marker := "[ ]"
				if item.Done {
					marker = "[x]"
				}
				fmt.Fprintf(&b, "- %s %s", marker, item.Text)
				if item.MatchedSpec != "" {
					fmt.Fprintf(&b, " (spec: %s, %d%%)", item.MatchedSpec, item.SpecProgress)
				}
				b.WriteString("\n")
			}
			b.WriteString("\n")
		}
	}

	b.WriteString("# Inbox — what needs operator attention\n\n")
	if len(board.Inbox) == 0 {
		b.WriteString("(inbox empty)\n")
	} else {
		maxInbox := 5
		for i, item := range board.Inbox {
			if i >= maxInbox {
				break
			}
			fmt.Fprintf(&b, "- [%s] %s: %s\n", item.Urgency, item.Title, item.Detail)
		}
	}
	b.WriteString("\n")

	b.WriteString("# What to write\n\n")
	b.WriteString("Write a concise markdown report (under 400 words) with these sections:\n\n")
	b.WriteString("**Current milestone:** which section we're in and why.\n\n")
	b.WriteString("**Progress:** concrete numbers — X of Y items done, key ones shipped, key ones pending. Cite the data, do not invent numbers.\n\n")
	b.WriteString("**Blockers:** top 1-3 things preventing the current milestone from closing. Be specific — name the spec/task.\n\n")
	b.WriteString("**Ships next:** the single most important thing to ship to close the current milestone or start the next one. Name the spec.\n\n")
	b.WriteString("**Momentum:** one line — are we accelerating, steady, or slowing?\n\n")
	b.WriteString("Be direct and confident. No hedging. Use the data, not generic advice.\n")

	return b.String()
}
