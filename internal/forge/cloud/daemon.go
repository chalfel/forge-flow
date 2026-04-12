package cloud

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"sync"
	"syscall"
	"time"
)

// ---------------------------------------------------------------------------
// Daemon — long-running process that syncs with Forge Cloud
// ---------------------------------------------------------------------------

// Daemon is the background process that maintains cloud connection.
type Daemon struct {
	client    *Client
	syncer    *Syncer
	projectID string

	// State
	mu         sync.RWMutex
	running    bool
	lastPing   time.Time

	// Notification callbacks
	onTaskAssigned func(CloudTask, CloudSpec)
	onTaskUpdated  func(CloudTask)

	// Control
	ctx    context.Context
	cancel context.CancelFunc
}

// DaemonConfig holds daemon configuration.
type DaemonConfig struct {
	ProjectID      string
	PollInterval   time.Duration
	OnTaskAssigned func(CloudTask, CloudSpec)
	OnTaskUpdated  func(CloudTask)
}

// NewDaemon creates a new daemon instance.
func NewDaemon(client *Client, cfg DaemonConfig) *Daemon {
	return &Daemon{
		client:         client,
		projectID:      cfg.ProjectID,
		onTaskAssigned: cfg.OnTaskAssigned,
		onTaskUpdated:  cfg.OnTaskUpdated,
	}
}

// Start begins the daemon process.
func (d *Daemon) Start(ctx context.Context) error {
	d.mu.Lock()
	if d.running {
		d.mu.Unlock()
		return fmt.Errorf("daemon already running")
	}
	d.running = true
	d.ctx, d.cancel = context.WithCancel(ctx)
	d.mu.Unlock()

	// Create syncer
	d.syncer = NewSyncer(d.client, d.projectID)

	// Register our listeners
	d.syncer.State().AddListener(d)

	// Start syncing
	if err := d.syncer.Start(d.ctx); err != nil {
		d.mu.Lock()
		d.running = false
		d.mu.Unlock()
		return err
	}

	// Write PID file
	if err := d.writePIDFile(); err != nil {
		// Non-fatal
	}

	return nil
}

// Stop halts the daemon.
func (d *Daemon) Stop() {
	d.mu.Lock()
	defer d.mu.Unlock()

	if !d.running {
		return
	}

	if d.cancel != nil {
		d.cancel()
	}
	if d.syncer != nil {
		d.syncer.Stop()
	}
	d.running = false
	d.removePIDFile()
}

// IsRunning returns true if the daemon is active.
func (d *Daemon) IsRunning() bool {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.running
}

// Wait blocks until the daemon stops or receives a shutdown signal.
func (d *Daemon) Wait() {
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)

	select {
	case <-d.ctx.Done():
	case <-sigCh:
		d.Stop()
	}
}

// ---------------------------------------------------------------------------
// SyncListener implementation
// ---------------------------------------------------------------------------

func (d *Daemon) OnTaskAssigned(task CloudTask, spec CloudSpec) {
	if d.onTaskAssigned != nil {
		d.onTaskAssigned(task, spec)
	}

	// Desktop notification
	if ShouldNotify("task_assigned") {
		NotifyTaskAssigned(task.Name, spec.Title)
	}

	// Write notification to local file for CLI to pick up
	d.writeNotification("task_assigned", map[string]any{
		"task": task,
		"spec": spec,
	})
}

func (d *Daemon) OnTaskUpdated(task CloudTask) {
	if d.onTaskUpdated != nil {
		d.onTaskUpdated(task)
	}
}

func (d *Daemon) OnSpecUpdated(spec CloudSpec) {
	// Could trigger local spec file update
}

func (d *Daemon) OnKnowledgeUpdated(projectID, section, content string) {
	// Could update local KB cache
}

func (d *Daemon) OnConnectionChange(connected bool) {
	d.mu.Lock()
	wasConnected := d.lastPing.After(time.Time{})
	if connected {
		d.lastPing = time.Now()
	}
	d.mu.Unlock()

	// Notify on connection state changes
	if ShouldNotify("connection") {
		if connected && !wasConnected {
			NotifyConnectionRestored()
		} else if !connected && wasConnected {
			NotifyConnectionLost()
		}
	}
}

// ---------------------------------------------------------------------------
// Local state management
// ---------------------------------------------------------------------------

func (d *Daemon) daemonDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".forge", "daemon")
}

func (d *Daemon) writePIDFile() error {
	dir := d.daemonDir()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	pid := os.Getpid()
	return os.WriteFile(filepath.Join(dir, "pid"), []byte(fmt.Sprintf("%d", pid)), 0o600)
}

func (d *Daemon) removePIDFile() {
	os.Remove(filepath.Join(d.daemonDir(), "pid"))
}

func (d *Daemon) writeNotification(notifType string, data any) error {
	dir := d.daemonDir()
	notifDir := filepath.Join(dir, "notifications")
	if err := os.MkdirAll(notifDir, 0o700); err != nil {
		return err
	}

	notif := map[string]any{
		"type":      notifType,
		"data":      data,
		"timestamp": time.Now().Format(time.RFC3339),
	}

	jsonData, err := json.Marshal(notif)
	if err != nil {
		return err
	}

	// Write to timestamped file
	filename := fmt.Sprintf("%d-%s.json", time.Now().UnixNano(), notifType)
	return os.WriteFile(filepath.Join(notifDir, filename), jsonData, 0o600)
}

// ---------------------------------------------------------------------------
// Daemon status (for CLI)
// ---------------------------------------------------------------------------

// DaemonStatus represents the current daemon state.
type DaemonStatus struct {
	Running     bool      `json:"running"`
	PID         int       `json:"pid,omitempty"`
	Connected   bool      `json:"connected"`
	LastPing    time.Time `json:"lastPing,omitempty"`
	ProjectID   string    `json:"projectId,omitempty"`
	TaskCount   int       `json:"taskCount"`
}

// GetDaemonStatus reads the current daemon status from local files.
func GetDaemonStatus() DaemonStatus {
	home, _ := os.UserHomeDir()
	daemonDir := filepath.Join(home, ".forge", "daemon")

	status := DaemonStatus{}

	// Check PID file
	pidData, err := os.ReadFile(filepath.Join(daemonDir, "pid"))
	if err == nil {
		var pid int
		fmt.Sscanf(string(pidData), "%d", &pid)
		if pid > 0 {
			// Check if process is still running
			process, err := os.FindProcess(pid)
			if err == nil {
				err = process.Signal(syscall.Signal(0))
				if err == nil {
					status.Running = true
					status.PID = pid
				}
			}
		}
	}

	// Read state file if exists
	stateData, err := os.ReadFile(filepath.Join(daemonDir, "state.json"))
	if err == nil {
		var state struct {
			Connected bool      `json:"connected"`
			LastPing  time.Time `json:"lastPing"`
			ProjectID string    `json:"projectId"`
			TaskCount int       `json:"taskCount"`
		}
		if json.Unmarshal(stateData, &state) == nil {
			status.Connected = state.Connected
			status.LastPing = state.LastPing
			status.ProjectID = state.ProjectID
			status.TaskCount = state.TaskCount
		}
	}

	return status
}

// ---------------------------------------------------------------------------
// Run daemon as foreground process (for `forge daemon` command)
// ---------------------------------------------------------------------------

// RunDaemon starts the daemon and blocks until stopped.
func RunDaemon(projectID string) error {
	client, err := NewClientFromStored()
	if err != nil {
		return err
	}

	daemon := NewDaemon(client, DaemonConfig{
		ProjectID: projectID,
		OnTaskAssigned: func(task CloudTask, spec CloudSpec) {
			fmt.Printf("[%s] New task assigned: %s\n", time.Now().Format("15:04:05"), task.Name)
		},
		OnTaskUpdated: func(task CloudTask) {
			fmt.Printf("[%s] Task updated: %s -> %s\n", time.Now().Format("15:04:05"), task.Name, task.Status)
		},
	})

	ctx := context.Background()
	if err := daemon.Start(ctx); err != nil {
		return err
	}

	fmt.Println("Forge daemon started. Press Ctrl+C to stop.")
	fmt.Printf("Project: %s\n", projectID)
	fmt.Printf("Connected to: %s\n", client.baseURL)

	daemon.Wait()
	return nil
}
