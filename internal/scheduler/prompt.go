package scheduler

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/chalfel/forge-flow/internal/domain"
)

// placeholderRE matches `{{ name }}` and `{{ namespace.field }}` with any
// surrounding whitespace. We deliberately keep the grammar narrow so the
// prompt body can be plain markdown without escaping concerns.
var placeholderRE = regexp.MustCompile(`{{\s*([a-zA-Z_][a-zA-Z0-9_]*(?:\.[a-zA-Z_][a-zA-Z0-9_]*)?)\s*}}`)

// renderPrompt expands `{{ issue.* }}` and `{{ attempt }}` placeholders. Any
// unknown placeholder is left intact so a typo surfaces in the rendered
// output rather than silently disappearing.
func renderPrompt(body string, issue domain.Issue, attempt int) string {
	vars := promptVars(issue, attempt)
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
