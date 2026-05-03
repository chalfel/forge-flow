// Symphony entry point. Phase 2 adds `run` (daemon loop) on top of the
// Phase 1 `validate` / `print` subcommands. Real tracker and agent adapters
// land in subsequent phases — `run --stub` exercises the loop end-to-end
// with in-memory implementations.
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/chalfel/forge-flow/internal/agent"
	"github.com/chalfel/forge-flow/internal/agent/shell"
	agentstub "github.com/chalfel/forge-flow/internal/agent/stub"
	"github.com/chalfel/forge-flow/internal/config"
	"github.com/chalfel/forge-flow/internal/domain"
	"github.com/chalfel/forge-flow/internal/scheduler"
	"github.com/chalfel/forge-flow/internal/tracker"
	githubtracker "github.com/chalfel/forge-flow/internal/tracker/github"
	"github.com/chalfel/forge-flow/internal/tracker/linear"
	trackerstub "github.com/chalfel/forge-flow/internal/tracker/stub"
	"github.com/chalfel/forge-flow/internal/workspace"
)

const usage = `symphony — orchestrate coding agents from a tracker

Usage:
  symphony validate <path/to/WORKFLOW.md>
  symphony print    <path/to/WORKFLOW.md>
  symphony run      <path/to/WORKFLOW.md> [--stub] [--once]
  symphony version

Flags for run:
  --stub   use in-memory tracker/agent/workspace (real adapters TBD)
  --once   run a single tick and exit (useful for smoke tests)
`

func main() {
	flag.Usage = func() { fmt.Fprint(os.Stderr, usage) }
	flag.Parse()
	args := flag.Args()
	if len(args) == 0 {
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}
	switch args[0] {
	case "validate":
		os.Exit(runValidate(args[1:]))
	case "print":
		os.Exit(runPrint(args[1:]))
	case "run":
		os.Exit(runRun(args[1:]))
	case "version":
		fmt.Println("symphony 0.1.0")
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n%s", args[0], usage)
		os.Exit(2)
	}
}

func runValidate(args []string) int {
	if len(args) != 1 {
		fmt.Fprintln(os.Stderr, "validate requires <path/to/WORKFLOW.md>")
		return 2
	}
	wf, err := config.Load(args[0])
	if err != nil {
		fmt.Fprintf(os.Stderr, "load: %v\n", err)
		return 1
	}
	if err := wf.Validate(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	fmt.Printf("ok: %s (tracker=%s agent=%s interval=%dms concurrency=%d)\n",
		wf.SourcePath, wf.Tracker.Kind, wf.Agent.Kind,
		wf.Polling.IntervalMs, wf.Agent.MaxConcurrentAgents)
	return 0
}

func runPrint(args []string) int {
	if len(args) != 1 {
		fmt.Fprintln(os.Stderr, "print requires <path/to/WORKFLOW.md>")
		return 2
	}
	wf, err := config.Load(args[0])
	if err != nil {
		fmt.Fprintf(os.Stderr, "load: %v\n", err)
		return 1
	}
	cmd := wf.AgentCommandFor()
	fmt.Printf("source:           %s\n", wf.SourcePath)
	fmt.Printf("tracker.kind:     %s\n", wf.Tracker.Kind)
	if wf.Tracker.Kind == config.TrackerLinear {
		fmt.Printf("tracker.slug:     %s\n", wf.Tracker.ProjectSlug)
	} else if wf.Tracker.Kind == config.TrackerGitHub {
		fmt.Printf("tracker.repo:     %s\n", wf.Tracker.Repo)
	}
	fmt.Printf("active_states:    %v\n", wf.Tracker.ActiveStates)
	fmt.Printf("terminal_states:  %v\n", wf.Tracker.TerminalStates)
	fmt.Printf("polling:          %dms\n", wf.Polling.IntervalMs)
	fmt.Printf("workspace.root:   %s\n", wf.Workspace.Root)
	fmt.Printf("agent.kind:       %s\n", wf.Agent.Kind)
	fmt.Printf("agent.command:    %s\n", cmd.Command)
	fmt.Printf("max_concurrent:   %d\n", wf.Agent.MaxConcurrentAgents)
	fmt.Printf("max_turns:        %d\n", wf.Agent.MaxTurns)
	fmt.Printf("hook.timeout_ms:  %d\n", wf.Hooks.TimeoutMs)
	fmt.Println("---")
	fmt.Print(wf.PromptBody)
	return 0
}

func runRun(args []string) int {
	fs := flag.NewFlagSet("run", flag.ContinueOnError)
	stub := fs.Bool("stub", false, "use in-memory tracker/agent/workspace stubs")
	once := fs.Bool("once", false, "run a single tick and exit")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() != 1 {
		fmt.Fprintln(os.Stderr, "run requires <path/to/WORKFLOW.md>")
		return 2
	}
	wf, err := config.Load(fs.Arg(0))
	if err != nil {
		fmt.Fprintf(os.Stderr, "load: %v\n", err)
		return 1
	}
	if err := wf.Validate(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))

	tr, err := buildTracker(wf, *stub)
	if err != nil {
		fmt.Fprintf(os.Stderr, "tracker: %v\n", err)
		return 1
	}
	if *stub {
		// Seed the stub tracker so the loop has something to dispatch.
		stubTracker, _ := tr.(*trackerstub.Tracker)
		stubTracker.Set(domain.Issue{
			ID:         "demo-1",
			Identifier: wf.Tracker.ProjectSlug + "-1",
			Title:      "stub demo issue",
			State:      firstOr(wf.Tracker.ActiveStates, "Todo"),
			CreatedAt:  time.Now(),
		})
	}
	ag, err := buildAgent(wf, *stub, logger)
	if err != nil {
		fmt.Fprintf(os.Stderr, "agent: %v\n", err)
		return 1
	}
	ws, err := buildWorkspace(wf, *stub, logger)
	if err != nil {
		fmt.Fprintf(os.Stderr, "workspace: %v\n", err)
		return 1
	}

	s := scheduler.New(scheduler.Options{
		Config:    wf,
		Tracker:   tr,
		Agent:     ag,
		Workspace: ws,
		Logger:    logger,
	})

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	if *once {
		if err := s.Tick(ctx); err != nil {
			fmt.Fprintf(os.Stderr, "tick: %v\n", err)
			return 1
		}
		// Give the dispatched goroutine a beat to write its completion.
		time.Sleep(100 * time.Millisecond)
		_ = s.Tick(ctx)
		printSnapshot(s)
		return 0
	}

	logger.Info("symphony starting",
		"source", wf.SourcePath,
		"tracker", wf.Tracker.Kind,
		"agent", wf.Agent.Kind,
		"interval_ms", wf.Polling.IntervalMs,
	)
	if err := s.Run(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "run: %v\n", err)
		return 1
	}
	printSnapshot(s)
	return 0
}

func printSnapshot(s *scheduler.Scheduler) {
	snap := s.Snapshot()
	fmt.Println("--- snapshot ---")
	for _, e := range snap {
		fmt.Printf("issue=%s state=%s attempt=%d err=%q\n",
			e.IssueID, e.State, e.Attempt, e.LastErr)
	}
}

func firstOr(xs []string, fallback string) string {
	if len(xs) > 0 {
		return xs[0]
	}
	return fallback
}

// buildAgent wires the configured agent runner. Both `codex` and
// `claude_code` use the shell runner (Phase 6/7) — they only differ in
// which AgentCommand block is read.
func buildAgent(wf *config.Workflow, useStub bool, logger *slog.Logger) (agent.Agent, error) {
	if useStub {
		return agentstub.New(), nil
	}
	return shell.FromAgentCommand(wf.AgentCommandFor(), logger)
}

// buildWorkspace returns the FS manager for real runs and the stub for
// dry-run mode. The FS manager honours the workflow's hook config and
// enforces path containment.
func buildWorkspace(wf *config.Workflow, useStub bool, logger *slog.Logger) (workspace.Manager, error) {
	if useStub {
		return workspace.NewStub(), nil
	}
	return workspace.NewFS(workspace.FSOptions{
		Root:   wf.Workspace.Root,
		Hooks:  wf.Hooks,
		Logger: logger,
	})
}

// buildTracker wires the right adapter based on the workflow's tracker kind.
// `--stub` always overrides to the in-memory tracker so dry-runs work even
// when the workflow points at a real Linear project.
func buildTracker(wf *config.Workflow, useStub bool) (tracker.Tracker, error) {
	if useStub {
		return trackerstub.New(), nil
	}
	switch wf.Tracker.Kind {
	case config.TrackerLinear:
		return linear.New(linear.Options{
			APIKey:      wf.Tracker.APIKey,
			ProjectSlug: wf.Tracker.ProjectSlug,
		}), nil
	case config.TrackerGitHub:
		return githubtracker.New(githubtracker.Options{
			APIKey:       wf.Tracker.APIKey,
			Repo:         wf.Tracker.Repo,
			ActiveStates: wf.Tracker.ActiveStates,
		})
	default:
		return nil, fmt.Errorf("unsupported tracker.kind %q", wf.Tracker.Kind)
	}
}
