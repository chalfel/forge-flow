package forge

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"
)

func TestDetectTmuxInsideSession(t *testing.T) {
	origRun := shellRun
	defer func() { shellRun = origRun }()

	// Simulate being inside tmux
	t.Setenv("TMUX", "/tmp/tmux-501/default,12345,0")
	shellRun = func(name string, args ...string) ([]byte, error) {
		if name == "tmux" && len(args) > 0 && args[0] == "display-message" {
			return []byte("my-session\n"), nil
		}
		return nil, nil
	}

	session, inside := DetectTmux()
	if !inside {
		t.Fatalf("expected to be inside tmux")
	}
	if session != "my-session" {
		t.Fatalf("expected session name 'my-session', got %q", session)
	}
}

func TestDetectTmuxOutsideSession(t *testing.T) {
	t.Setenv("TMUX", "")

	_, inside := DetectTmux()
	if inside {
		t.Fatalf("expected to NOT be inside tmux")
	}
}

func TestEnsureTmuxAvailable(t *testing.T) {
	// This test verifies the function runs without crashing.
	// It may pass or fail depending on the environment.
	err := EnsureTmuxAvailable()
	if err != nil {
		t.Skipf("tmux not available in test environment: %v", err)
	}
}

func TestEnsureClaudeAvailable(t *testing.T) {
	err := EnsureClaudeAvailable()
	if err != nil {
		t.Skipf("claude not available in test environment: %v", err)
	}
}

func TestCreateTmuxSessionCommand(t *testing.T) {
	origRun := shellRun
	defer func() { shellRun = origRun }()

	var capturedArgs []string
	shellRun = func(name string, args ...string) ([]byte, error) {
		capturedArgs = append([]string{name}, args...)
		return []byte(""), nil
	}

	err := CreateTmuxSession("forge-test")
	if err != nil {
		t.Fatalf("CreateTmuxSession: %v", err)
	}
	if len(capturedArgs) == 0 {
		t.Fatalf("expected command to be captured")
	}
	joined := strings.Join(capturedArgs, " ")
	if !strings.Contains(joined, "new-session") || !strings.Contains(joined, "forge-test") {
		t.Fatalf("unexpected command: %s", joined)
	}
}

func TestNewTmuxWindowCommand(t *testing.T) {
	origRun := shellRun
	defer func() { shellRun = origRun }()

	shellRun = func(name string, args ...string) ([]byte, error) {
		return []byte("%42\n"), nil
	}

	paneID, err := NewTmuxWindow("forge-test", "task-a")
	if err != nil {
		t.Fatalf("NewTmuxWindow: %v", err)
	}
	if paneID != "%42" {
		t.Fatalf("expected pane ID '%%42', got %q", paneID)
	}
}

func TestSendKeysCommand(t *testing.T) {
	origRun := shellRun
	defer func() { shellRun = origRun }()

	var capturedArgs []string
	shellRun = func(name string, args ...string) ([]byte, error) {
		capturedArgs = append([]string{name}, args...)
		return []byte(""), nil
	}

	err := SendKeys("%42", "echo hello")
	if err != nil {
		t.Fatalf("SendKeys: %v", err)
	}
	joined := strings.Join(capturedArgs, " ")
	if !strings.Contains(joined, "send-keys") || !strings.Contains(joined, "%42") {
		t.Fatalf("unexpected command: %s", joined)
	}
}

func TestIsPaneAliveFound(t *testing.T) {
	origRun := shellRun
	defer func() { shellRun = origRun }()

	shellRun = func(name string, args ...string) ([]byte, error) {
		return []byte("%40 0\n%41 1\n%42 0\n"), nil
	}

	alive, err := IsPaneAlive("%42")
	if err != nil {
		t.Fatalf("IsPaneAlive: %v", err)
	}
	if !alive {
		t.Fatal("expected pane %%42 to be alive")
	}

	alive, err = IsPaneAlive("%41")
	if err != nil {
		t.Fatalf("IsPaneAlive: %v", err)
	}
	if alive {
		t.Fatal("expected pane %%41 to be dead")
	}
}

func TestIsPaneAliveNotFound(t *testing.T) {
	origRun := shellRun
	defer func() { shellRun = origRun }()

	shellRun = func(name string, args ...string) ([]byte, error) {
		return []byte("%40 0\n"), nil
	}

	alive, err := IsPaneAlive("%99")
	if err != nil {
		t.Fatalf("IsPaneAlive: %v", err)
	}
	if alive {
		t.Fatalf("expected missing pane to be not alive")
	}
}

func TestWaitForPanesAllDone(t *testing.T) {
	origRun := shellRun
	defer func() { shellRun = origRun }()

	shellRun = func(name string, args ...string) ([]byte, error) {
		// All panes dead
		return []byte("%1 1\n%2 1\n"), nil
	}

	// Bypass env check since we're not actually in tmux
	os.Setenv("TMUX", "")

	ctx := context.Background()
	result, err := WaitForPanes(ctx, []string{"%1", "%2"}, 10*time.Millisecond)
	if err != nil {
		t.Fatalf("WaitForPanes: %v", err)
	}
	for id, alive := range result {
		if alive {
			t.Fatalf("expected pane %s to be done", id)
		}
	}
}

func TestWaitForPanesCancelled(t *testing.T) {
	origRun := shellRun
	defer func() { shellRun = origRun }()

	shellRun = func(name string, args ...string) ([]byte, error) {
		// Pane stays alive
		return []byte("%1 0\n"), nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	result, err := WaitForPanes(ctx, []string{"%1"}, 10*time.Millisecond)
	if err == nil {
		t.Fatalf("expected context error")
	}
	if !result["%1"] {
		t.Fatalf("pane should still be marked alive when cancelled")
	}
}
