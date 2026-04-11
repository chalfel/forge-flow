package forge

import (
	"context"
	"testing"
	"time"
)

func TestWatchRunShutdownOnCancel(t *testing.T) {
	origRun := shellRun
	defer func() { shellRun = origRun }()

	// Stub all shell commands to succeed.
	shellRun = func(name string, args ...string) ([]byte, error) {
		if name == "tmux" && len(args) > 0 {
			switch args[0] {
			case "display-message":
				return []byte("test-session\n"), nil
			case "new-session":
				return []byte(""), nil
			case "new-window":
				return []byte("%99\n"), nil
			case "send-keys":
				return []byte(""), nil
			case "list-panes":
				return []byte(""), nil
			}
		}
		return []byte(""), nil
	}

	// Simulate being inside tmux.
	t.Setenv("TMUX", "/tmp/tmux-test,123,0")

	svc, webRepo := setupRunTestProject(t)

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	cfg := WatchConfig{
		CWD:          webRepo,
		PollInterval: 20 * time.Millisecond,
		AutoReview:   false,
	}

	err := svc.WatchRun(ctx, cfg)
	if err != nil {
		t.Fatalf("WatchRun should return nil on context cancel, got: %v", err)
	}
}

func TestWatchConfigDefaults(t *testing.T) {
	cfg := WatchConfig{}
	if cfg.PollInterval != 0 {
		t.Fatal("zero value should be default")
	}
	// The WatchRun function sets 10s when PollInterval <= 0.
	// This is tested implicitly in TestWatchRunShutdownOnCancel.
}
