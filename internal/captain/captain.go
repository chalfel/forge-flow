// Package captain plans a high-level demand into a small set of atomic
// tickets and writes them to the tracker. The captain runs an agent
// (typically Claude Code or Codex) with a planning prompt; the agent's
// final output must contain a fenced JSON code block matching the schema
// below. The captain parses it and calls TrackerWriter.CreateIssue for each
// entry.
//
//	{
//	  "tickets": [
//	    {
//	      "title": "...",
//	      "description": "...",
//	      "priority": 1,
//	      "labels": ["Todo"]
//	    }
//	  ]
//	}
package captain

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"regexp"
	"strings"

	"github.com/chalfel/forge-flow/internal/agent"
	"github.com/chalfel/forge-flow/internal/config"
	"github.com/chalfel/forge-flow/internal/domain"
	"github.com/chalfel/forge-flow/internal/tracker"
)

type Captain struct {
	cfg    *config.Workflow
	agent  agent.Agent
	writer tracker.Writer
	log    *slog.Logger
}

type Options struct {
	Config *config.Workflow
	Agent  agent.Agent
	Writer tracker.Writer
	Logger *slog.Logger
}

func New(opts Options) (*Captain, error) {
	if opts.Config == nil {
		return nil, errors.New("captain: config required")
	}
	if opts.Agent == nil {
		return nil, errors.New("captain: agent required")
	}
	if opts.Writer == nil {
		return nil, errors.New("captain: tracker does not support writes (no Writer impl)")
	}
	logger := opts.Logger
	if logger == nil {
		logger = slog.Default()
	}
	return &Captain{cfg: opts.Config, agent: opts.Agent, writer: opts.Writer, log: logger}, nil
}

type Result struct {
	Drafts  []domain.IssueDraft
	Created []*domain.Issue
}

// Plan runs the planning agent with the supplied demand and an optional
// workspace path (used as cwd so the agent can read repo files for context).
// Returns the parsed drafts. Plan does NOT write to the tracker; pair with
// Create to materialise the tickets.
func (c *Captain) Plan(ctx context.Context, demand, workspacePath string) ([]domain.IssueDraft, string, error) {
	prompt := c.buildPrompt(demand)
	c.log.Info("captain planning", "demand_chars", len(demand))
	res := c.agent.Run(ctx, agent.RunRequest{
		Issue:     domain.Issue{Identifier: "captain", Title: "Planning"},
		Workspace: domain.Workspace{Path: workspacePath, WorkspaceKey: "captain"},
		Prompt:    prompt,
	})
	if res.Err != nil {
		return nil, res.Output, fmt.Errorf("captain agent: %w", res.Err)
	}
	drafts, err := parseTickets(res.Output)
	if err != nil {
		return nil, res.Output, fmt.Errorf("captain parse: %w", err)
	}
	return drafts, res.Output, nil
}

// Create writes drafts to the tracker. Partial success is supported: the
// returned Result contains every successfully created issue and the first
// error halts the loop so the operator can inspect what was written before
// retrying.
func (c *Captain) Create(ctx context.Context, drafts []domain.IssueDraft) (Result, error) {
	out := Result{Drafts: drafts}
	for _, d := range drafts {
		issue, err := c.writer.CreateIssue(ctx, d)
		if err != nil {
			return out, fmt.Errorf("create %q: %w", d.Title, err)
		}
		out.Created = append(out.Created, issue)
		c.log.Info("ticket created", "title", d.Title, "identifier", issue.Identifier, "url", issue.URL)
	}
	return out, nil
}

// buildPrompt loads the captain prompt template (custom file or baked-in
// default), substitutes the demand, and prepends a hint about the
// workflow's active states so the agent labels tickets correctly.
func (c *Captain) buildPrompt(demand string) string {
	body := defaultPrompt
	if path := strings.TrimSpace(c.cfg.Captain.PromptPath); path != "" {
		if data, err := os.ReadFile(path); err == nil {
			body = string(data)
		} else {
			c.log.Warn("captain prompt_path unreadable; falling back to default", "err", err)
		}
	}
	active := strings.Join(c.cfg.Tracker.ActiveStates, ", ")
	body = strings.ReplaceAll(body, "{{ demand }}", demand)
	body = strings.ReplaceAll(body, "{{ active_states }}", active)
	return body
}

const defaultPrompt = `You are a planning captain. Decompose the demand below into a small set of atomic tickets that a coding agent can each complete end-to-end (3-5 typical, 8 maximum). Each ticket must have a clear acceptance criterion in its description.

Demand:
{{ demand }}

OUTPUT
After your reasoning, emit a single fenced JSON code block matching this schema. Do NOT emit any other JSON code blocks.

` + "```json" + `
{
  "tickets": [
    {
      "title": "Short imperative title",
      "description": "Markdown body. Include why, what changes, and acceptance criteria.",
      "priority": 2,
      "labels": ["{{ active_states }}"]
    }
  ]
}
` + "```" + `

The first label MUST be one of: {{ active_states }} — Symphony will only pick the ticket up if it carries an active-state label.`

// fencedJSONRE matches a fenced JSON code block. The (?s) flag lets `.` span
// newlines so multi-line ticket descriptions are captured.
var fencedJSONRE = regexp.MustCompile("(?s)```json\\s*(\\{.*?\\})\\s*```")

func parseTickets(output string) ([]domain.IssueDraft, error) {
	matches := fencedJSONRE.FindAllStringSubmatch(output, -1)
	if len(matches) == 0 {
		return nil, errors.New("no fenced json code block found in agent output")
	}
	// Use the LAST block — the agent may discuss schema in earlier blocks.
	jsonBlock := matches[len(matches)-1][1]
	var parsed struct {
		Tickets []struct {
			Title       string   `json:"title"`
			Description string   `json:"description"`
			Priority    int      `json:"priority"`
			Labels      []string `json:"labels"`
		} `json:"tickets"`
	}
	if err := json.Unmarshal([]byte(jsonBlock), &parsed); err != nil {
		return nil, fmt.Errorf("decode tickets: %w", err)
	}
	if len(parsed.Tickets) == 0 {
		return nil, errors.New("tickets array empty")
	}
	out := make([]domain.IssueDraft, 0, len(parsed.Tickets))
	for _, t := range parsed.Tickets {
		if strings.TrimSpace(t.Title) == "" {
			return nil, errors.New("ticket missing title")
		}
		out = append(out, domain.IssueDraft{
			Title:       t.Title,
			Description: t.Description,
			Priority:    t.Priority,
			Labels:      t.Labels,
		})
	}
	return out, nil
}
