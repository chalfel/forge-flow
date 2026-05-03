package shell

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/chalfel/forge-flow/internal/agent"
	"github.com/chalfel/forge-flow/internal/domain"
)

func newRunner(t *testing.T, command string, timeoutMs int) *Runner {
	t.Helper()
	r, err := New(Options{
		Command:       command,
		TurnTimeoutMs: timeoutMs,
		Logger:        slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	if err != nil {
		t.Fatalf("new runner: %v", err)
	}
	return r
}

func tmpReq(t *testing.T, prompt string) agent.RunRequest {
	t.Helper()
	return agent.RunRequest{
		Issue:     domain.Issue{ID: "1", Identifier: "ABC-1"},
		Attempt:   0,
		Workspace: domain.Workspace{Path: t.TempDir(), WorkspaceKey: "ABC-1"},
		Prompt:    prompt,
	}
}

func TestRun_SuccessOnExitZero(t *testing.T) {
	r := newRunner(t, "cat > /dev/null && exit 0", 5000)
	res := r.Run(context.Background(), tmpReq(t, "hello"))
	if res.Status != domain.StatusSucceeded {
		t.Fatalf("expected Succeeded, got %s (err=%v)", res.Status, res.Err)
	}
}

func TestRun_FailureOnNonZero(t *testing.T) {
	r := newRunner(t, "exit 7", 5000)
	res := r.Run(context.Background(), tmpReq(t, ""))
	if res.Status != domain.StatusFailed {
		t.Fatalf("expected Failed, got %s", res.Status)
	}
	if res.Err == nil {
		t.Fatal("expected non-nil err on failure")
	}
}

func TestRun_TimeoutMapsToTimedOut(t *testing.T) {
	r := newRunner(t, "sleep 5", 100)
	res := r.Run(context.Background(), tmpReq(t, ""))
	if res.Status != domain.StatusTimedOut {
		t.Fatalf("expected TimedOut, got %s (err=%v)", res.Status, res.Err)
	}
}

func TestRun_PromptPipedToStdin(t *testing.T) {
	out := filepath.Join(t.TempDir(), "stdin")
	r := newRunner(t, "cat > "+out, 5000)
	req := tmpReq(t, "the prompt body")
	res := r.Run(context.Background(), req)
	if res.Status != domain.StatusSucceeded {
		t.Fatalf("status: %s err=%v", res.Status, res.Err)
	}
	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "the prompt body") {
		t.Fatalf("stdin not piped, got %q", data)
	}
}

func TestRun_CwdIsWorkspace(t *testing.T) {
	out := filepath.Join(t.TempDir(), "cwd")
	r := newRunner(t, "pwd > "+out, 5000)
	req := tmpReq(t, "")
	res := r.Run(context.Background(), req)
	if res.Status != domain.StatusSucceeded {
		t.Fatalf("status: %s", res.Status)
	}
	data, _ := os.ReadFile(out)
	got := strings.TrimSpace(string(data))
	if got != req.Workspace.Path {
		t.Fatalf("cwd wrong: got %q, want %q", got, req.Workspace.Path)
	}
}

func TestNew_RequiresCommand(t *testing.T) {
	if _, err := New(Options{}); err == nil {
		t.Fatal("expected error for empty command")
	}
}

func TestRun_StallTimeoutMapsToStalled(t *testing.T) {
	// `sleep 5` produces no stdout. With a 200ms stall budget the watchdog
	// should kill the process well before the turn timeout would fire and
	// return StatusStalled.
	r, err := New(Options{
		Command:        "sleep 5",
		TurnTimeoutMs:  10_000,
		StallTimeoutMs: 200,
		Logger:         slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	if err != nil {
		t.Fatal(err)
	}
	res := r.Run(context.Background(), tmpReq(t, ""))
	if res.Status != domain.StatusStalled {
		t.Fatalf("expected Stalled, got %s (err=%v)", res.Status, res.Err)
	}
}

func TestRun_OutputResetsStallWatchdog(t *testing.T) {
	// Print a line every 50ms for ~400ms; a 200ms stall budget must NOT
	// fire because the watchdog sees fresh activity.
	r, err := New(Options{
		Command:        "for i in 1 2 3 4 5 6 7 8; do echo $i; sleep 0.05; done",
		TurnTimeoutMs:  10_000,
		StallTimeoutMs: 200,
		Logger:         slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	if err != nil {
		t.Fatal(err)
	}
	res := r.Run(context.Background(), tmpReq(t, ""))
	if res.Status != domain.StatusSucceeded {
		t.Fatalf("expected Succeeded, got %s (err=%v output=%q)", res.Status, res.Err, res.Output)
	}
}

