// Package config parses, validates, and resolves WORKFLOW.md files.
//
// A WORKFLOW.md file is composed of an optional YAML front matter block
// (delimited by `---` on its own line) followed by a markdown prompt body.
// The front matter configures the scheduler; the body is the prompt template
// rendered each turn with `{{ issue.* }}` and `{{ attempt }}` placeholders.
package config

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

type Workflow struct {
	Tracker      Tracker      `yaml:"tracker"`
	Polling      Polling      `yaml:"polling"`
	Workspace    Workspace    `yaml:"workspace"`
	Hooks        Hooks        `yaml:"hooks"`
	Agent        Agent        `yaml:"agent"`
	Codex        AgentCommand `yaml:"codex"`
	ClaudeCode   AgentCommand `yaml:"claude_code"`
	Captain      Captain      `yaml:"captain"`
	PromptBody   string       `yaml:"-"`
	SourcePath   string       `yaml:"-"`
}

// Captain configures the planning agent that turns high-level demands into
// a list of tickets. Defaults: agent kind = workflow.agent.kind; command =
// AgentCommandFor's command; prompt = baked-in default.
type Captain struct {
	Command       string `yaml:"command"`
	PromptPath    string `yaml:"prompt_path"`
	TurnTimeoutMs int    `yaml:"turn_timeout_ms"`
}

type TrackerKind string

const (
	TrackerLinear TrackerKind = "linear"
	TrackerGitHub TrackerKind = "github"
)

type Tracker struct {
	Kind           TrackerKind `yaml:"kind"`
	ProjectSlug    string      `yaml:"project_slug"`
	Repo           string      `yaml:"repo"`
	APIKey         string      `yaml:"api_key"`
	ActiveStates   []string    `yaml:"active_states"`
	TerminalStates []string    `yaml:"terminal_states"`
}

type Polling struct {
	IntervalMs int `yaml:"interval_ms"`
}

type Workspace struct {
	Root string `yaml:"root"`
}

type Hooks struct {
	AfterCreate  string `yaml:"after_create"`
	BeforeRun    string `yaml:"before_run"`
	AfterRun     string `yaml:"after_run"`
	BeforeRemove string `yaml:"before_remove"`
	TimeoutMs    int    `yaml:"timeout_ms"`
}

type Agent struct {
	Kind                string `yaml:"kind"`
	MaxConcurrentAgents int    `yaml:"max_concurrent_agents"`
	MaxTurns            int    `yaml:"max_turns"`
	MaxRetryBackoffMs   int    `yaml:"max_retry_backoff_ms"`
}

type AgentCommand struct {
	Command       string `yaml:"command"`
	TurnTimeoutMs int    `yaml:"turn_timeout_ms"`
	ReadTimeoutMs int    `yaml:"read_timeout_ms"`
	StallTimeoutMs int   `yaml:"stall_timeout_ms"`
}

const (
	defaultPollingIntervalMs = 30_000
	defaultHookTimeoutMs     = 60_000
	defaultMaxConcurrent     = 3
	defaultMaxTurns          = 20
	defaultMaxRetryBackoffMs = 300_000
	defaultTurnTimeoutMs     = 3_600_000
	defaultReadTimeoutMs     = 5_000
	defaultStallTimeoutMs    = 300_000
	defaultWorkspaceRoot     = "~/symphony_workspaces"
	defaultAgentKind         = "codex"
)

// Load reads and parses a WORKFLOW.md file, applies defaults, and resolves
// `$VAR` indirections from the environment. It does NOT validate the result;
// call Validate separately so callers can present validation errors distinctly
// from parse errors.
func Load(path string) (*Workflow, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("resolve workflow path: %w", err)
	}
	f, err := os.Open(abs)
	if err != nil {
		return nil, fmt.Errorf("open workflow: %w", err)
	}
	defer f.Close()

	frontMatter, body, err := splitFrontMatter(f)
	if err != nil {
		return nil, err
	}

	wf := &Workflow{
		PromptBody: body,
		SourcePath: abs,
	}
	if frontMatter != "" {
		if err := yaml.Unmarshal([]byte(frontMatter), wf); err != nil {
			return nil, fmt.Errorf("parse front matter: %w", err)
		}
	}
	wf.applyDefaults()
	wf.resolveEnv()
	if err := wf.normalizePaths(); err != nil {
		return nil, err
	}
	return wf, nil
}

// splitFrontMatter consumes the optional `---` delimited YAML block at the top
// of the file and returns (frontMatter, body). When the file does not begin
// with `---`, the entire content is treated as body.
func splitFrontMatter(r *os.File) (string, string, error) {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 1024*1024), 8*1024*1024)

	var (
		lines      []string
		fmStart    = false
		fmEnd      = false
		fmLines    []string
		bodyLines  []string
		firstLine  = true
	)
	for scanner.Scan() {
		line := scanner.Text()
		lines = append(lines, line)
		if firstLine {
			firstLine = false
			if strings.TrimSpace(line) == "---" {
				fmStart = true
				continue
			}
			bodyLines = append(bodyLines, line)
			continue
		}
		if fmStart && !fmEnd {
			if strings.TrimSpace(line) == "---" {
				fmEnd = true
				continue
			}
			fmLines = append(fmLines, line)
			continue
		}
		bodyLines = append(bodyLines, line)
	}
	if err := scanner.Err(); err != nil {
		return "", "", fmt.Errorf("read workflow: %w", err)
	}
	if fmStart && !fmEnd {
		return "", "", errors.New("front matter not closed: missing trailing `---`")
	}
	_ = lines
	return strings.Join(fmLines, "\n"), strings.Join(bodyLines, "\n"), nil
}

func (w *Workflow) applyDefaults() {
	if w.Polling.IntervalMs == 0 {
		w.Polling.IntervalMs = defaultPollingIntervalMs
	}
	if w.Hooks.TimeoutMs == 0 {
		w.Hooks.TimeoutMs = defaultHookTimeoutMs
	}
	if w.Agent.MaxConcurrentAgents == 0 {
		w.Agent.MaxConcurrentAgents = defaultMaxConcurrent
	}
	if w.Agent.MaxTurns == 0 {
		w.Agent.MaxTurns = defaultMaxTurns
	}
	if w.Agent.MaxRetryBackoffMs == 0 {
		w.Agent.MaxRetryBackoffMs = defaultMaxRetryBackoffMs
	}
	if w.Agent.Kind == "" {
		w.Agent.Kind = defaultAgentKind
	}
	if w.Workspace.Root == "" {
		w.Workspace.Root = defaultWorkspaceRoot
	}
	for _, cmd := range []*AgentCommand{&w.Codex, &w.ClaudeCode} {
		if cmd.TurnTimeoutMs == 0 {
			cmd.TurnTimeoutMs = defaultTurnTimeoutMs
		}
		if cmd.ReadTimeoutMs == 0 {
			cmd.ReadTimeoutMs = defaultReadTimeoutMs
		}
		if cmd.StallTimeoutMs == 0 {
			cmd.StallTimeoutMs = defaultStallTimeoutMs
		}
	}
}

// resolveEnv expands `$VAR` style indirections for sensitive fields. Only the
// API key is resolved here; commands and hooks pass through to the shell which
// expands its own env.
func (w *Workflow) resolveEnv() {
	w.Tracker.APIKey = expandEnvVar(w.Tracker.APIKey)
}

func expandEnvVar(s string) string {
	s = strings.TrimSpace(s)
	if !strings.HasPrefix(s, "$") {
		return s
	}
	name := strings.TrimPrefix(s, "$")
	return os.Getenv(name)
}

func (w *Workflow) normalizePaths() error {
	root := w.Workspace.Root
	if strings.HasPrefix(root, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return fmt.Errorf("resolve workspace root: %w", err)
		}
		root = filepath.Join(home, strings.TrimPrefix(root, "~/"))
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return fmt.Errorf("absolutize workspace root: %w", err)
	}
	w.Workspace.Root = abs
	return nil
}

// AgentCommandFor returns the AgentCommand for the configured agent kind.
func (w *Workflow) AgentCommandFor() AgentCommand {
	switch w.Agent.Kind {
	case "claude_code", "claude-code", "claudecode":
		return w.ClaudeCode
	default:
		return w.Codex
	}
}
