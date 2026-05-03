// Package router dispatches an attempt to one of several agents based on
// per-issue labels. The first matching rule wins; if no rule matches the
// fallback agent runs. Used to wire the captain alongside the worker agent
// when `captain.watch_label` is set: tickets with the watch label go to the
// captain, everything else goes to the worker.
package router

import (
	"context"
	"strings"

	"github.com/chalfel/forge-flow/internal/agent"
)

type Rule struct {
	Label string
	Agent agent.Agent
}

type Router struct {
	rules    []Rule
	fallback agent.Agent
}

func New(fallback agent.Agent, rules ...Rule) *Router {
	return &Router{fallback: fallback, rules: rules}
}

func (r *Router) Run(ctx context.Context, req agent.RunRequest) agent.RunResult {
	for _, rule := range r.rules {
		for _, l := range req.Issue.Labels {
			if strings.EqualFold(l, rule.Label) {
				return rule.Agent.Run(ctx, req)
			}
		}
	}
	return r.fallback.Run(ctx, req)
}
