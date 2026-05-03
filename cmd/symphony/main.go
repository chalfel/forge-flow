// Symphony entry point. Phase 1 supports `validate` and `print`; the daemon
// loop ships in subsequent phases per the SPEC.md staging.
package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/chalfel/forge-flow/internal/config"
)

const usage = `symphony — orchestrate coding agents from a tracker

Usage:
  symphony validate <path/to/WORKFLOW.md>
  symphony print    <path/to/WORKFLOW.md>
  symphony version
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
