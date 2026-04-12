package forge

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/chalfel/forge-flow/internal/forge/cloud"
)

// ---------------------------------------------------------------------------
// Cloud-integrated task execution
// ---------------------------------------------------------------------------

// CloudTaskRunner handles starting tasks from Forge Cloud.
type CloudTaskRunner struct {
	svc     *Service
	client  *cloud.Client
	syncer  *cloud.Syncer
	ctx     ResolvedContext
}

// NewCloudTaskRunner creates a runner with cloud integration.
func NewCloudTaskRunner(svc *Service, client *cloud.Client, projectID string) *CloudTaskRunner {
	return &CloudTaskRunner{
		svc:    svc,
		client: client,
		syncer: cloud.NewSyncer(client, projectID),
	}
}

// StartCloudTask fetches a task from the cloud and spawns a Claude session for it.
// This is the cloud-equivalent of ExecuteRunLive for a single task.
func (r *CloudTaskRunner) StartCloudTask(ctx context.Context, taskID string, cwd string) (*CloudTaskSession, error) {
	if err := EnsureTmuxAvailable(); err != nil {
		return nil, err
	}
	if err := EnsureClaudeAvailable(); err != nil {
		return nil, err
	}

	// Resolve local context
	resolvedCtx, err := r.svc.ResolveContext(cwd)
	if err != nil {
		return nil, fmt.Errorf("resolving context: %w", err)
	}
	r.ctx = resolvedCtx

	// Fetch task and spec from cloud
	cloudTask, err := r.client.GetTask(taskID)
	if err != nil {
		return nil, fmt.Errorf("fetching task: %w", err)
	}

	cloudSpec, err := r.client.GetSpec(cloudTask.SpecID)
	if err != nil {
		return nil, fmt.Errorf("fetching spec: %w", err)
	}

	// Update status to in_progress
	if err := r.client.UpdateTaskStatus(taskID, cloud.TaskStatusUpdate{
		TaskID:    taskID,
		Status:    "in_progress",
		Timestamp: time.Now(),
	}); err != nil {
		// Non-fatal, continue
		fmt.Printf("Warning: could not update task status: %v\n", err)
	}

	// Convert cloud types to local types
	planTask := cloudTaskToRunPlanTask(*cloudTask, *cloudSpec)

	// Determine spec branch
	specBranch := cloudSpec.Branch
	if specBranch == "" {
		specBranch = DefaultSpecBranch(cloudSpec.Title)
	}

	// Resolve repo root
	repoRoot, err := ResolveRepoRoot(resolvedCtx, cloudTask.Repo)
	if err != nil {
		return nil, fmt.Errorf("resolving repo: %w", err)
	}

	// Create worktree
	depBranch := "" // TODO: resolve from cloud task deps
	wtPath, branch, err := CreateWorktree(repoRoot, specBranch, cloudTask.Name, depBranch)
	if err != nil {
		return nil, fmt.Errorf("creating worktree: %w", err)
	}

	// Collect KB content (local + cloud)
	kbContent := r.collectKBWithCloud(resolvedCtx, cloudSpec.ProjectID)

	// Determine tmux session
	sessionName, insideTmux := DetectTmux()
	if !insideTmux {
		sessionName = fmt.Sprintf("forge-%s", slugify(cloudTask.Name))
		if err := CreateTmuxSession(sessionName); err != nil {
			return nil, fmt.Errorf("creating tmux session: %w", err)
		}
	}

	// Create window for this task
	windowName := slugify(cloudTask.Name)
	if len(windowName) > 20 {
		windowName = windowName[:20]
	}
	paneID, err := NewTmuxWindow(sessionName, windowName)
	if err != nil {
		return nil, fmt.Errorf("creating tmux window: %w", err)
	}

	// Setup events file for agent communication
	runtimeDir := filepath.Join(resolvedCtx.RepoLocalForge, "runtime", "cloud-"+taskID)
	eventsFile := filepath.Join(runtimeDir, "events.jsonl")
	if err := writeJSON(filepath.Join(runtimeDir, "task.json"), cloudTask); err != nil {
		// Non-fatal
	}

	// Build harness with cloud spec content
	baseBranch := "main"
	if depBranch != "" {
		baseBranch = depBranch
	}

	harness := &HarnessBuilder{
		Ctx:          resolvedCtx,
		Task:         planTask,
		WorktreePath: wtPath,
		BaseBranch:   baseBranch,
		RunID:        "cloud-" + taskID,
		KBContent:    kbContent,
		EventsFile:   eventsFile,
	}

	// Inject cloud spec content into task prompt
	harness.Task.SpecPath = "" // No local spec file
	// The spec content will be in the prompt via cloudSpecContent

	claudeCmd := harness.Build()

	// Send keys to tmux pane
	if err := SendKeys(paneID, claudeCmd); err != nil {
		return nil, fmt.Errorf("sending claude command: %w", err)
	}

	session := &CloudTaskSession{
		TaskID:      taskID,
		TaskName:    cloudTask.Name,
		SpecID:      cloudSpec.ID,
		SpecTitle:   cloudSpec.Title,
		Session:     sessionName,
		Window:      windowName,
		PaneID:      paneID,
		WorktreePath: wtPath,
		Branch:      branch,
		EventsFile:  eventsFile,
		StartedAt:   time.Now(),
		client:      r.client,
	}

	// Start event watcher in background
	go session.watchEvents(ctx)

	if !insideTmux {
		fmt.Printf("Tmux session ready: tmux attach -t %s\n", sessionName)
	}

	return session, nil
}

// collectKBWithCloud merges local KB with cloud KB.
func (r *CloudTaskRunner) collectKBWithCloud(ctx ResolvedContext, projectID string) string {
	var sections []string

	// Local KB first
	if ctx.KBDir != "" {
		local := collectKBContent(ctx.KBDir)
		if local != "" {
			sections = append(sections, local)
		}
	}

	// Cloud KB
	if r.client != nil && projectID != "" {
		cloudKB, err := r.client.GetKnowledge(projectID)
		if err == nil && cloudKB != nil {
			if cloudKB.Constraints != "" {
				sections = append(sections, "# Constraints (from cloud)\n\n"+cloudKB.Constraints)
			}
			if cloudKB.Architecture != "" {
				sections = append(sections, "# Architecture (from cloud)\n\n"+cloudKB.Architecture)
			}
			if cloudKB.Business != "" {
				sections = append(sections, "# Business Context (from cloud)\n\n"+cloudKB.Business)
			}
			if cloudKB.Conventions != "" {
				sections = append(sections, "# Conventions (from cloud)\n\n"+cloudKB.Conventions)
			}
		}
	}

	return strings.Join(sections, "\n\n---\n\n")
}

// cloudTaskToRunPlanTask converts a cloud task to local RunPlanTask.
func cloudTaskToRunPlanTask(task cloud.CloudTask, spec cloud.CloudSpec) RunPlanTask {
	return RunPlanTask{
		File:       fmt.Sprintf("cloud:%s", spec.ID),
		SpecTitle:  spec.Title,
		SpecBranch: spec.Branch,
		Task:       task.Name,
		Repo:       task.Repo,
		Status:     task.Status,
		Deps:       task.Deps,
		Touches:    task.Touches,
	}
}

// ---------------------------------------------------------------------------
// CloudTaskSession tracks a running cloud task
// ---------------------------------------------------------------------------

// CloudTaskSession represents an active task session from the cloud.
type CloudTaskSession struct {
	TaskID       string
	TaskName     string
	SpecID       string
	SpecTitle    string
	Session      string
	Window       string
	PaneID       string
	WorktreePath string
	Branch       string
	EventsFile   string
	StartedAt    time.Time
	client       *cloud.Client
}

// watchEvents monitors the events file and syncs progress to cloud.
func (s *CloudTaskSession) watchEvents(ctx context.Context) {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	var lastOffset int64

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			events, newOffset, err := readNewEvents(s.EventsFile, lastOffset)
			if err != nil || len(events) == 0 {
				continue
			}
			lastOffset = newOffset

			for _, event := range events {
				// Sync to cloud
				s.client.SendTaskProgress(cloud.TaskProgressEvent{
					TaskID:    s.TaskID,
					Type:      event.Type,
					Message:   event.Message,
					Details:   event.Details,
					Timestamp: time.Now(),
				})
			}
		}
	}
}

// Attach attaches to the tmux session/window for this task.
func (s *CloudTaskSession) Attach() error {
	return AttachToSession(s.Session)
}

// IsAlive checks if the Claude pane is still running.
func (s *CloudTaskSession) IsAlive() bool {
	alive, err := IsPaneAlive(s.PaneID)
	return err == nil && alive
}

// MarkComplete updates the cloud task status to done/in_review.
func (s *CloudTaskSession) MarkComplete(status string) error {
	return s.client.UpdateTaskStatus(s.TaskID, cloud.TaskStatusUpdate{
		TaskID:    s.TaskID,
		Status:    status,
		Branch:    s.Branch,
		Timestamp: time.Now(),
	})
}

// ---------------------------------------------------------------------------
// Event reading helper
// ---------------------------------------------------------------------------

// AgentEvent is an event emitted by the agent.
type AgentEvent struct {
	Type      string `json:"type"`
	Message   string `json:"message"`
	Timestamp string `json:"timestamp"`
	Details   any    `json:"details,omitempty"`
}

// readNewEvents reads events from the file starting at offset.
func readNewEvents(path string, offset int64) ([]AgentEvent, int64, error) {
	// This is a simplified version - in production we'd use proper file watching
	data, err := readFileFromOffset(path, offset)
	if err != nil {
		return nil, offset, err
	}
	if len(data) == 0 {
		return nil, offset, nil
	}

	var events []AgentEvent
	lines := strings.Split(string(data), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var event AgentEvent
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			continue
		}
		events = append(events, event)
	}

	return events, offset + int64(len(data)), nil
}

// readFileFromOffset reads a file starting from a byte offset.
func readFileFromOffset(path string, offset int64) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if info.Size() <= offset {
		return nil, nil
	}

	f.Seek(offset, 0)
	data := make([]byte, info.Size()-offset)
	n, err := f.Read(data)
	if err != nil {
		return nil, err
	}
	return data[:n], nil
}
