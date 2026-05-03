package config

import (
	"errors"
	"fmt"
	"strings"
)

// ValidationError aggregates every issue found during Validate so the operator
// sees them all at once rather than one per restart.
type ValidationError struct {
	Issues []string
}

func (e *ValidationError) Error() string {
	return "workflow invalid:\n  - " + strings.Join(e.Issues, "\n  - ")
}

// Validate checks the conditions described in SPEC.md "Configuration Validation
// and Dynamic Reload": the workflow must be loadable, tracker.kind must be
// supported, the API key must resolve, the project slug (or repo) must be
// present for the chosen tracker, and the agent command must be set.
func (w *Workflow) Validate() error {
	var issues []string

	switch w.Tracker.Kind {
	case TrackerLinear:
		if strings.TrimSpace(w.Tracker.ProjectSlug) == "" {
			issues = append(issues, "tracker.project_slug required for linear")
		}
	case TrackerGitHub:
		if strings.TrimSpace(w.Tracker.Repo) == "" {
			issues = append(issues, "tracker.repo required for github (format: owner/name)")
		} else if !strings.Contains(w.Tracker.Repo, "/") {
			issues = append(issues, "tracker.repo must be in `owner/name` form")
		}
	case "":
		issues = append(issues, "tracker.kind required")
	default:
		issues = append(issues, fmt.Sprintf("tracker.kind %q not supported (linear, github)", w.Tracker.Kind))
	}

	if strings.TrimSpace(w.Tracker.APIKey) == "" {
		issues = append(issues, "tracker.api_key empty after $VAR resolution; export the env var or set inline")
	}

	if len(w.Tracker.ActiveStates) == 0 {
		issues = append(issues, "tracker.active_states must list at least one state")
	}

	cmd := w.AgentCommandFor()
	if strings.TrimSpace(cmd.Command) == "" {
		issues = append(issues, fmt.Sprintf("agent kind %q has no command configured", w.Agent.Kind))
	}

	if w.Polling.IntervalMs < 1000 {
		issues = append(issues, "polling.interval_ms must be >= 1000")
	}

	if w.Agent.MaxConcurrentAgents < 1 {
		issues = append(issues, "agent.max_concurrent_agents must be >= 1")
	}

	if len(issues) == 0 {
		return nil
	}
	return &ValidationError{Issues: issues}
}

// IsValidationError is a small helper so callers can branch on validation vs
// other error kinds without importing reflect.
func IsValidationError(err error) bool {
	var ve *ValidationError
	return errors.As(err, &ve)
}
