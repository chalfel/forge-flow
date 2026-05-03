package scheduler

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/chalfel/forge-flow/internal/domain"
	"github.com/chalfel/forge-flow/internal/skills"
)

// placeholderRE matches `{{ name }}` and `{{ namespace.field }}` with any
// surrounding whitespace. We deliberately keep the grammar narrow so the
// prompt body can be plain markdown without escaping concerns.
var placeholderRE = regexp.MustCompile(`{{\s*([a-zA-Z_][a-zA-Z0-9_]*(?:\.[a-zA-Z_][a-zA-Z0-9_]*)?)\s*}}`)

// renderPrompt expands `{{ issue.* }}`, `{{ attempt }}`, and `{{ skills }}`
// placeholders. Skills are discovered from the workspace; pass an empty path
// (or a workspace without a .symphony/skills directory) to render the empty
// inventory note. Unknown placeholders are left intact so typos surface.
func renderPrompt(body string, issue domain.Issue, attempt int, workspaceRoot string) string {
	vars := promptVars(issue, attempt)
	if found, _ := skills.Discover(workspaceRoot); found != nil || workspaceRoot != "" {
		vars["skills"] = skills.Render(found)
	}
	return placeholderRE.ReplaceAllStringFunc(body, func(match string) string {
		key := strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(match, "{{"), "}}"))
		if v, ok := vars[key]; ok {
			return v
		}
		return match
	})
}

func promptVars(issue domain.Issue, attempt int) map[string]string {
	return map[string]string{
		"attempt":           strconv.Itoa(attempt),
		"issue.id":          issue.ID,
		"issue.identifier":  issue.Identifier,
		"issue.title":       issue.Title,
		"issue.description": issue.Description,
		"issue.priority":    fmt.Sprintf("%d", issue.Priority),
		"issue.state":       issue.State,
		"issue.branch_name": issue.BranchName,
		"issue.url":         issue.URL,
		"issue.labels":      strings.Join(issue.Labels, ","),
	}
}
