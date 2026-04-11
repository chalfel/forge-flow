package forge

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

// commandRunner abstracts shell command execution for testing.
type commandRunner func(name string, args ...string) ([]byte, error)

var shellRun commandRunner = defaultShellRun

func defaultShellRun(name string, args ...string) ([]byte, error) {
	return exec.Command(name, args...).CombinedOutput()
}

// runIn executes a command with a specific working directory.
// Used for tools like `gh` that don't accept a -C/--cwd flag and must
// run inside the target git repo.
func runIn(dir, name string, args ...string) ([]byte, error) {
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	return cmd.CombinedOutput()
}

// DetectTmux checks if we're inside a tmux session.
// Returns session name and whether we're inside tmux.
func DetectTmux() (string, bool) {
	tmuxEnv := os.Getenv("TMUX")
	if tmuxEnv == "" {
		return "", false
	}
	// $TMUX format: /tmp/tmux-501/default,12345,0
	// The session name is not directly in $TMUX; query it.
	out, err := shellRun("tmux", "display-message", "-p", "#{session_name}")
	if err != nil {
		return "", true // inside tmux but can't get session name
	}
	return strings.TrimSpace(string(out)), true
}

// EnsureTmuxAvailable checks that the tmux binary exists on PATH.
func EnsureTmuxAvailable() error {
	_, err := exec.LookPath("tmux")
	if err != nil {
		return fmt.Errorf("tmux is required for live execution but was not found on PATH")
	}
	return nil
}

// EnsureClaudeAvailable checks that the claude CLI exists on PATH.
func EnsureClaudeAvailable() error {
	_, err := exec.LookPath("claude")
	if err != nil {
		return fmt.Errorf("claude CLI is required for live execution but was not found on PATH")
	}
	return nil
}

// CreateTmuxSession creates a new detached tmux session.
func CreateTmuxSession(name string) error {
	out, err := shellRun("tmux", "new-session", "-d", "-s", name, "-x", "220", "-y", "50")
	if err != nil {
		return fmt.Errorf("creating tmux session %q: %s (%w)", name, string(out), err)
	}
	return nil
}

// NewTmuxWindow creates a new window in the given session.
// Returns the pane ID (e.g. "%42") for tracking.
func NewTmuxWindow(session, windowName string) (string, error) {
	out, err := shellRun("tmux", "new-window", "-t", session, "-n", windowName, "-P", "-F", "#{pane_id}")
	if err != nil {
		return "", fmt.Errorf("creating tmux window %q in session %q: %s (%w)", windowName, session, string(out), err)
	}
	return strings.TrimSpace(string(out)), nil
}

// SendKeys sends a command string to a specific tmux pane.
func SendKeys(paneID, command string) error {
	out, err := shellRun("tmux", "send-keys", "-t", paneID, command, "Enter")
	if err != nil {
		return fmt.Errorf("sending keys to pane %s: %s (%w)", paneID, string(out), err)
	}
	return nil
}

// IsPaneAlive checks if a tmux pane is still running.
func IsPaneAlive(paneID string) (bool, error) {
	out, err := shellRun("tmux", "list-panes", "-a", "-F", "#{pane_id} #{pane_dead}")
	if err != nil {
		return false, fmt.Errorf("listing panes: %s (%w)", string(out), err)
	}
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		parts := strings.Fields(line)
		if len(parts) >= 2 && parts[0] == paneID {
			return parts[1] == "0", nil
		}
	}
	// Pane not found means it's gone
	return false, nil
}

// WaitForPanes polls all tracked panes until they all exit or the context is cancelled.
// Returns a map of paneID -> alive (true = still alive when context cancelled).
func WaitForPanes(ctx context.Context, paneIDs []string, pollInterval time.Duration) (map[string]bool, error) {
	result := make(map[string]bool, len(paneIDs))
	for _, id := range paneIDs {
		result[id] = true
	}

	for {
		select {
		case <-ctx.Done():
			return result, ctx.Err()
		default:
		}

		allDone := true
		for _, id := range paneIDs {
			if !result[id] {
				continue
			}
			alive, err := IsPaneAlive(id)
			if err != nil {
				return result, err
			}
			if !alive {
				result[id] = false
			} else {
				allDone = false
			}
		}

		if allDone {
			return result, nil
		}

		select {
		case <-ctx.Done():
			return result, ctx.Err()
		case <-time.After(pollInterval):
		}
	}
}

// AttachToSession attaches the current terminal to a tmux session.
// This replaces the current process with tmux attach.
func AttachToSession(name string) error {
	tmuxPath, err := exec.LookPath("tmux")
	if err != nil {
		return err
	}
	// Use exec.Command instead of syscall.Exec for portability.
	cmd := exec.Command(tmuxPath, "attach-session", "-t", name)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}
