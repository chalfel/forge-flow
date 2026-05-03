package captain

import (
	"context"
	"fmt"
	"strings"

	"github.com/chalfel/forge-flow/internal/agent"
	"github.com/chalfel/forge-flow/internal/domain"
)

// Agent adapts a Captain to the agent.Agent interface so the scheduler can
// dispatch decomposition work just like any other agent. The "demand" is
// reconstructed from the issue's title + description; on success the agent
// returns StatusDecomposed which the scheduler maps to skip-set + release.
type Agent struct {
	captain *Captain
}

func NewAgent(c *Captain) *Agent { return &Agent{captain: c} }

func (a *Agent) Run(ctx context.Context, req agent.RunRequest) agent.RunResult {
	demand := buildDemand(req.Issue)
	drafts, output, err := a.captain.Plan(ctx, demand, req.Workspace.Path)
	if err != nil {
		return agent.RunResult{Status: domain.StatusFailed, Err: err, Output: output}
	}
	res, err := a.captain.Create(ctx, drafts)
	if err != nil {
		return agent.RunResult{
			Status: domain.StatusFailed,
			Err:    fmt.Errorf("create %d/%d: %w", len(res.Created), len(drafts), err),
			Output: summary(res, output),
		}
	}
	return agent.RunResult{Status: domain.StatusDecomposed, Output: summary(res, output)}
}

func buildDemand(issue domain.Issue) string {
	var b strings.Builder
	b.WriteString(issue.Title)
	b.WriteString("\n\n")
	if strings.TrimSpace(issue.Description) != "" {
		b.WriteString(issue.Description)
		b.WriteString("\n\n")
	}
	if len(issue.Labels) > 0 {
		fmt.Fprintf(&b, "(parent labels: %s)\n", strings.Join(issue.Labels, ", "))
	}
	return b.String()
}

func summary(res Result, agentOutput string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "captain decomposed parent into %d ticket(s):\n", len(res.Created))
	for _, is := range res.Created {
		fmt.Fprintf(&b, "  - %s — %s\n", is.Identifier, is.Title)
	}
	if agentOutput != "" {
		b.WriteString("\n--- agent output ---\n")
		b.WriteString(agentOutput)
	}
	return b.String()
}
