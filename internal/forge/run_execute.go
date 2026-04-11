package forge

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// RunStatus represents the lifecycle state of a run or task attempt.
type RunStatus string

const (
	RunStatusPending   RunStatus = "pending"
	RunStatusRunning   RunStatus = "running"
	RunStatusSucceeded RunStatus = "succeeded"
	RunStatusFailed    RunStatus = "failed"
	RunStatusSkipped   RunStatus = "skipped"
)

// TaskAttempt records one execution attempt for a single task within a run.
type TaskAttempt struct {
	Task      string    `json:"task"`
	SpecFile  string    `json:"specFile"`
	SpecTitle string    `json:"specTitle,omitempty"`
	Repo      string    `json:"repo,omitempty"`
	Status    RunStatus `json:"status"`
	Bucket    string    `json:"bucket"`
	Reasons   []string  `json:"reasons,omitempty"`
	StartedAt string    `json:"startedAt,omitempty"`
	EndedAt   string    `json:"endedAt,omitempty"`
	Error     string    `json:"error,omitempty"`
}

// RunRecord is the persisted artifact for one forge run execution.
type RunRecord struct {
	RunID       string        `json:"runId"`
	ProjectID   string        `json:"projectId"`
	ProjectName string        `json:"projectName"`
	SpecsDir    string        `json:"specsDir"`
	Mode        string        `json:"mode"`
	Status      RunStatus     `json:"status"`
	StartedAt   string        `json:"startedAt"`
	EndedAt     string        `json:"endedAt,omitempty"`
	Tasks       []TaskAttempt `json:"tasks"`
	Summary     RunSummary    `json:"summary"`
}

// RunSummary provides counts per outcome for quick inspection.
type RunSummary struct {
	Total     int `json:"total"`
	Succeeded int `json:"succeeded"`
	Failed    int `json:"failed"`
	Skipped   int `json:"skipped"`
}

// RunExecuteResult is returned by both preview and apply modes.
type RunExecuteResult struct {
	RunID     string        `json:"runId,omitempty"`
	Mode      string        `json:"mode"`
	Plan      RunPlan       `json:"plan"`
	Tasks     []TaskAttempt `json:"tasks"`
	Summary   RunSummary    `json:"summary"`
	RunFile   string        `json:"runFile,omitempty"`
	Persisted bool          `json:"persisted"`
}

// PreviewRun produces a dry-run execution result without persisting state.
func (s *Service) PreviewRun(cwd, specPath string) (RunExecuteResult, error) {
	plan, err := s.PlanRun(cwd, specPath)
	if err != nil {
		return RunExecuteResult{}, err
	}
	tasks := buildTaskAttempts(plan)
	return RunExecuteResult{
		Mode:    "preview",
		Plan:    plan,
		Tasks:   tasks,
		Summary: summarizeAttempts(tasks),
	}, nil
}

// ExecuteRun builds a run plan, executes safe/serial tasks, skips the rest,
// and persists the run record under the repo-local .forge/runs/ directory.
func (s *Service) ExecuteRun(cwd, specPath string) (RunExecuteResult, error) {
	plan, err := s.PlanRun(cwd, specPath)
	if err != nil {
		return RunExecuteResult{}, err
	}

	ctx, err := s.ResolveContext(cwd)
	if err != nil {
		return RunExecuteResult{}, err
	}

	runID := generateRunID()
	now := timeNow().UTC().Format(time.RFC3339)

	tasks := executeFromPlan(plan, now)
	summary := summarizeAttempts(tasks)

	status := RunStatusSucceeded
	if summary.Failed > 0 {
		status = RunStatusFailed
	}

	record := RunRecord{
		RunID:       runID,
		ProjectID:   plan.ProjectID,
		ProjectName: plan.ProjectName,
		SpecsDir:    plan.SpecsDir,
		Mode:        "apply",
		Status:      status,
		StartedAt:   now,
		EndedAt:     timeNow().UTC().Format(time.RFC3339),
		Tasks:       tasks,
		Summary:     summary,
	}

	runsDir := resolveRunsDir(ctx)
	runFile := filepath.Join(runsDir, runID+".json")
	if err := writeJSON(runFile, record); err != nil {
		return RunExecuteResult{}, fmt.Errorf("persisting run record: %w", err)
	}

	return RunExecuteResult{
		RunID:     runID,
		Mode:      "apply",
		Plan:      plan,
		Tasks:     tasks,
		Summary:   summary,
		RunFile:   runFile,
		Persisted: true,
	}, nil
}

// LoadRunRecord reads a persisted run record from disk.
func (s *Service) LoadRunRecord(cwd, runID string) (RunRecord, error) {
	ctx, err := s.ResolveContext(cwd)
	if err != nil {
		return RunRecord{}, err
	}
	runsDir := resolveRunsDir(ctx)
	runFile := filepath.Join(runsDir, runID+".json")
	var record RunRecord
	if err := readJSON(runFile, &record); err != nil {
		return RunRecord{}, fmt.Errorf("loading run %s: %w", runID, err)
	}
	return record, nil
}

// ListRunRecords returns all persisted run records, most recent first.
func (s *Service) ListRunRecords(cwd string) ([]RunRecord, error) {
	ctx, err := s.ResolveContext(cwd)
	if err != nil {
		return nil, err
	}
	runsDir := resolveRunsDir(ctx)
	entries, err := os.ReadDir(runsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var records []RunRecord
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		var record RunRecord
		if err := readJSON(filepath.Join(runsDir, entry.Name()), &record); err != nil {
			continue
		}
		records = append(records, record)
	}
	sort.Slice(records, func(i, j int) bool {
		return records[i].StartedAt > records[j].StartedAt
	})
	return records, nil
}

// resolveRunsDir returns the .forge/runs/ directory inside the repo root,
// falling back to a runs/ directory under the project root.
func resolveRunsDir(ctx ResolvedContext) string {
	if ctx.RepoLocalForge != "" {
		return filepath.Join(ctx.RepoLocalForge, "runs")
	}
	return filepath.Join(ctx.ProjectRoot, "runs")
}

// buildTaskAttempts creates TaskAttempt entries from a run plan for preview mode.
// Safe and serial tasks are marked pending; everything else is skipped with reasons.
func buildTaskAttempts(plan RunPlan) []TaskAttempt {
	var attempts []TaskAttempt

	for _, task := range plan.Safe {
		attempts = append(attempts, TaskAttempt{
			Task:      task.Task,
			SpecFile:  task.File,
			SpecTitle: task.SpecTitle,
			Repo:      task.Repo,
			Status:    RunStatusPending,
			Bucket:    "safe",
		})
	}
	for _, task := range plan.Serial {
		attempts = append(attempts, TaskAttempt{
			Task:      task.Task,
			SpecFile:  task.File,
			SpecTitle: task.SpecTitle,
			Repo:      task.Repo,
			Status:    RunStatusPending,
			Bucket:    "serial",
		})
	}
	for _, task := range plan.Conflict {
		attempts = append(attempts, TaskAttempt{
			Task:      task.Task,
			SpecFile:  task.File,
			SpecTitle: task.SpecTitle,
			Repo:      task.Repo,
			Status:    RunStatusSkipped,
			Bucket:    "conflict",
			Reasons:   task.Reasons,
		})
	}
	for _, task := range plan.Blocked {
		attempts = append(attempts, TaskAttempt{
			Task:      task.Task,
			SpecFile:  task.File,
			SpecTitle: task.SpecTitle,
			Repo:      task.Repo,
			Status:    RunStatusSkipped,
			Bucket:    "blocked",
			Reasons:   task.Reasons,
		})
	}
	for _, task := range plan.Invalid {
		attempts = append(attempts, TaskAttempt{
			Task:      task.Task,
			SpecFile:  task.File,
			SpecTitle: task.SpecTitle,
			Repo:      task.Repo,
			Status:    RunStatusSkipped,
			Bucket:    "invalid",
			Reasons:   task.Reasons,
		})
	}
	return attempts
}

// executeFromPlan runs safe/serial tasks and skips the rest.
// In this first version, "executing" a task means marking it succeeded
// (the actual agent spawn is handled by the skill layer, not the CLI).
// The CLI executor is responsible for state transitions and artifact persistence.
func executeFromPlan(plan RunPlan, startTime string) []TaskAttempt {
	var attempts []TaskAttempt

	for _, task := range plan.Safe {
		attempts = append(attempts, TaskAttempt{
			Task:      task.Task,
			SpecFile:  task.File,
			SpecTitle: task.SpecTitle,
			Repo:      task.Repo,
			Status:    RunStatusSucceeded,
			Bucket:    "safe",
			StartedAt: startTime,
			EndedAt:   timeNow().UTC().Format(time.RFC3339),
		})
	}
	for _, task := range plan.Serial {
		attempts = append(attempts, TaskAttempt{
			Task:      task.Task,
			SpecFile:  task.File,
			SpecTitle: task.SpecTitle,
			Repo:      task.Repo,
			Status:    RunStatusSucceeded,
			Bucket:    "serial",
			StartedAt: startTime,
			EndedAt:   timeNow().UTC().Format(time.RFC3339),
		})
	}
	for _, task := range plan.Conflict {
		attempts = append(attempts, TaskAttempt{
			Task:      task.Task,
			SpecFile:  task.File,
			SpecTitle: task.SpecTitle,
			Repo:      task.Repo,
			Status:    RunStatusSkipped,
			Bucket:    "conflict",
			Reasons:   task.Reasons,
		})
	}
	for _, task := range plan.Blocked {
		attempts = append(attempts, TaskAttempt{
			Task:      task.Task,
			SpecFile:  task.File,
			SpecTitle: task.SpecTitle,
			Repo:      task.Repo,
			Status:    RunStatusSkipped,
			Bucket:    "blocked",
			Reasons:   task.Reasons,
		})
	}
	for _, task := range plan.Invalid {
		attempts = append(attempts, TaskAttempt{
			Task:      task.Task,
			SpecFile:  task.File,
			SpecTitle: task.SpecTitle,
			Repo:      task.Repo,
			Status:    RunStatusSkipped,
			Bucket:    "invalid",
			Reasons:   task.Reasons,
		})
	}
	return attempts
}

func summarizeAttempts(attempts []TaskAttempt) RunSummary {
	s := RunSummary{Total: len(attempts)}
	for _, a := range attempts {
		switch a.Status {
		case RunStatusSucceeded:
			s.Succeeded++
		case RunStatusFailed:
			s.Failed++
		case RunStatusSkipped:
			s.Skipped++
		}
	}
	return s
}

func generateRunID() string {
	now := timeNow().UTC()
	return fmt.Sprintf("run-%s", now.Format("20060102-150405"))
}

// timeNow is a variable so tests can override it.
var timeNow = time.Now

// ExecuteRunLive builds a run plan, spawns agents in tmux panes inside git
// worktrees, waits for completion, creates stacked PRs, and updates spec
// metadata. This is the "real" execution mode triggered by --live.
func (s *Service) ExecuteRunLive(ctx context.Context, cwd, specPath string) (RunExecuteResult, error) {
	if err := EnsureTmuxAvailable(); err != nil {
		return RunExecuteResult{}, err
	}
	if err := EnsureClaudeAvailable(); err != nil {
		return RunExecuteResult{}, err
	}

	plan, err := s.PlanRun(cwd, specPath)
	if err != nil {
		return RunExecuteResult{}, err
	}
	if !plan.LintValid {
		return RunExecuteResult{
			Mode: "live",
			Plan: plan,
		}, fmt.Errorf("spec has lint errors; fix them before running")
	}

	resolvedCtx, err := s.ResolveContext(cwd)
	if err != nil {
		return RunExecuteResult{}, err
	}

	runID := generateRunID()
	now := timeNow().UTC().Format(time.RFC3339)

	// Collect KB files for context injection.
	kbContent := collectKBContent(resolvedCtx.KBDir)

	// Build task branch map for all tasks across all specs.
	taskBranchMap := map[string]string{}
	allTasks := concatPlanTasks(plan)
	for _, task := range allTasks {
		specBranch := task.SpecBranch
		if specBranch == "" {
			specBranch = DefaultSpecBranch(task.SpecTitle)
		}
		taskBranchMap[strings.ToLower(strings.TrimSpace(task.Task))] = TaskBranchName(specBranch, task.Task)
	}

	// Determine tmux session.
	sessionName, insideTmux := DetectTmux()
	if !insideTmux {
		sessionName = "forge-" + runID
		if err := CreateTmuxSession(sessionName); err != nil {
			return RunExecuteResult{}, err
		}
	}

	var attempts []TaskAttempt

	// Phase 1: Safe parallel tasks — spawn all at once.
	if len(plan.Safe) > 0 {
		safeAttempts, err := s.spawnTaskGroup(ctx, plan.Safe, sessionName, resolvedCtx, taskBranchMap, kbContent, now)
		if err != nil {
			return RunExecuteResult{}, fmt.Errorf("spawning safe tasks: %w", err)
		}
		attempts = append(attempts, safeAttempts...)
	}

	// Phase 2: Serial tasks — one at a time.
	for _, task := range plan.Serial {
		serialAttempts, err := s.spawnTaskGroup(ctx, []RunPlanTask{task}, sessionName, resolvedCtx, taskBranchMap, kbContent, now)
		if err != nil {
			return RunExecuteResult{}, fmt.Errorf("spawning serial task %q: %w", task.Task, err)
		}
		attempts = append(attempts, serialAttempts...)
	}

	// Skip conflict/blocked/invalid.
	for _, task := range plan.Conflict {
		attempts = append(attempts, TaskAttempt{
			Task: task.Task, SpecFile: task.File, SpecTitle: task.SpecTitle,
			Repo: task.Repo, Status: RunStatusSkipped, Bucket: "conflict", Reasons: task.Reasons,
		})
	}
	for _, task := range plan.Blocked {
		attempts = append(attempts, TaskAttempt{
			Task: task.Task, SpecFile: task.File, SpecTitle: task.SpecTitle,
			Repo: task.Repo, Status: RunStatusSkipped, Bucket: "blocked", Reasons: task.Reasons,
		})
	}
	for _, task := range plan.Invalid {
		attempts = append(attempts, TaskAttempt{
			Task: task.Task, SpecFile: task.File, SpecTitle: task.SpecTitle,
			Repo: task.Repo, Status: RunStatusSkipped, Bucket: "invalid", Reasons: task.Reasons,
		})
	}

	summary := summarizeAttempts(attempts)
	status := RunStatusSucceeded
	if summary.Failed > 0 {
		status = RunStatusFailed
	}

	record := RunRecord{
		RunID:       runID,
		ProjectID:   plan.ProjectID,
		ProjectName: plan.ProjectName,
		SpecsDir:    plan.SpecsDir,
		Mode:        "live",
		Status:      status,
		StartedAt:   now,
		EndedAt:     timeNow().UTC().Format(time.RFC3339),
		Tasks:       attempts,
		Summary:     summary,
	}

	runsDir := resolveRunsDir(resolvedCtx)
	runFile := filepath.Join(runsDir, runID+".json")
	if err := writeJSON(runFile, record); err != nil {
		return RunExecuteResult{}, fmt.Errorf("persisting run record: %w", err)
	}

	// If we created the tmux session, print how to attach.
	// We don't call AttachToSession here because it blocks on cmd.Run()
	// and would leak as a goroutine if the main process exits first.
	if !insideTmux {
		fmt.Printf("Tmux session ready: tmux attach -t %s\n", sessionName)
	}

	return RunExecuteResult{
		RunID:     runID,
		Mode:      "live",
		Plan:      plan,
		Tasks:     attempts,
		Summary:   summary,
		RunFile:   runFile,
		Persisted: true,
	}, nil
}

// spawnTaskGroup spawns agents for a group of tasks in tmux, waits for them
// to complete, creates PRs, and updates spec metadata.
func (s *Service) spawnTaskGroup(
	ctx context.Context,
	tasks []RunPlanTask,
	session string,
	resolvedCtx ResolvedContext,
	taskBranchMap map[string]string,
	kbContent string,
	startTime string,
) ([]TaskAttempt, error) {
	type liveTask struct {
		task       RunPlanTask
		paneID     string
		wtPath     string
		branch     string
		baseBranch string
		eventsFile string
	}

	var live []liveTask
	var attempts []TaskAttempt

	for _, task := range tasks {
		specBranch := task.SpecBranch
		if specBranch == "" {
			specBranch = DefaultSpecBranch(task.SpecTitle)
		}

		repoRoot, err := ResolveRepoRoot(resolvedCtx, task.Repo)
		if err != nil {
			attempts = append(attempts, TaskAttempt{
				Task: task.Task, SpecFile: task.File, SpecTitle: task.SpecTitle,
				Repo: task.Repo, Status: RunStatusFailed, Bucket: bucketForTask(task),
				Error: fmt.Sprintf("repo resolution: %v", err), StartedAt: startTime,
			})
			continue
		}

		depBranch := ResolveDepBranch(specBranch, task.Deps, taskBranchMap)
		baseBranch := "main"
		if depBranch != "" {
			baseBranch = depBranch
		}

		wtPath, branch, err := CreateWorktree(repoRoot, specBranch, task.Task, depBranch)
		if err != nil {
			attempts = append(attempts, TaskAttempt{
				Task: task.Task, SpecFile: task.File, SpecTitle: task.SpecTitle,
				Repo: task.Repo, Status: RunStatusFailed, Bucket: bucketForTask(task),
				Error: fmt.Sprintf("worktree creation: %v", err), StartedAt: startTime,
			})
			continue
		}

		// Update spec status to in_progress.
		if task.SpecPath != "" {
			_ = UpdateTaskMetadata(task.SpecPath, task.Task, map[string]string{"status": "in_progress"})
		}

		// Load past attempts and feedback BEFORE appending the new one
		// so the agent sees only history from previous runs.
		pastAttempts, _ := s.loadAttemptsWithCtx(resolvedCtx, task.File, task.Task)
		pastFeedback, _ := s.loadFeedbackWithCtx(resolvedCtx, task.File, task.Task)

		runID := runIDFromStartTime(startTime)

		// Record the attempt start in history.
		_ = s.appendAttemptWithCtx(resolvedCtx, TaskAttemptRecord{
			RunID:    runID,
			SpecFile: task.File,
			TaskName: task.Task,
			Status:   "running",
		})

		// Provision the per-task events file so the agent can stream status back.
		eventsFile := EventsFilePath(resolvedCtx, runID, task.Task)
		_ = EnsureEventsFile(eventsFile)

		// Build and send the claude command with full layered harness.
		harness := &HarnessBuilder{
			Ctx:          resolvedCtx,
			Task:         task,
			WorktreePath: wtPath,
			BaseBranch:   baseBranch,
			KBContent:    kbContent,
			PastAttempts: pastAttempts,
			PastFeedback: pastFeedback,
			EventsFile:   eventsFile,
			RunID:        runID,
		}
		cmd := harness.Build()
		// Prefix the command with the env var export so the agent can use $FORGE_EVENTS_FILE.
		cmd = fmt.Sprintf("export FORGE_EVENTS_FILE='%s' && %s", shellEscape(eventsFile), cmd)
		slug := slugify(task.Task)
		paneID, err := NewTmuxWindow(session, slug)
		if err != nil {
			attempts = append(attempts, TaskAttempt{
				Task: task.Task, SpecFile: task.File, SpecTitle: task.SpecTitle,
				Repo: task.Repo, Status: RunStatusFailed, Bucket: bucketForTask(task),
				Error: fmt.Sprintf("tmux window: %v", err), StartedAt: startTime,
			})
			continue
		}

		if err := SendKeys(paneID, cmd); err != nil {
			attempts = append(attempts, TaskAttempt{
				Task: task.Task, SpecFile: task.File, SpecTitle: task.SpecTitle,
				Repo: task.Repo, Status: RunStatusFailed, Bucket: bucketForTask(task),
				Error: fmt.Sprintf("send keys: %v", err), StartedAt: startTime,
			})
			continue
		}

		live = append(live, liveTask{
			task: task, paneID: paneID, wtPath: wtPath,
			branch: branch, baseBranch: baseBranch,
			eventsFile: eventsFile,
		})
	}

	if len(live) == 0 {
		return attempts, nil
	}

	// Start tailing the events files from all live tasks in parallel
	// so we can react to blocked/proposed_memory events in real-time.
	tailCtx, stopTail := context.WithCancel(ctx)
	defer stopTail()

	blockedReasons := map[string]string{} // taskName -> reason
	var tailMu sync.Mutex

	tail := newEventTail(func(path string, ev Event) {
		// Find which task this path belongs to.
		var matched *liveTask
		for i := range live {
			if live[i].eventsFile == path {
				matched = &live[i]
				break
			}
		}
		if matched == nil {
			return
		}
		s.handleAgentEvent(resolvedCtx, matched.task, ev, blockedReasons, &tailMu)
	}, 2*time.Second)
	for _, lt := range live {
		tail.Watch(lt.eventsFile)
	}
	go tail.Run(tailCtx)

	// Wait for all panes to close.
	paneIDs := make([]string, len(live))
	for i, lt := range live {
		paneIDs[i] = lt.paneID
	}
	_, waitErr := WaitForPanes(ctx, paneIDs, 2*time.Second)
	stopTail()

	// Collect results.
	endTime := timeNow().UTC().Format(time.RFC3339)
	for _, lt := range live {
		attempt := TaskAttempt{
			Task:      lt.task.Task,
			SpecFile:  lt.task.File,
			SpecTitle: lt.task.SpecTitle,
			Repo:      lt.task.Repo,
			Bucket:    bucketForTask(lt.task),
			StartedAt: startTime,
			EndedAt:   endTime,
		}

		tailMu.Lock()
		blockedReason, wasBlocked := blockedReasons[lt.task.Task]
		tailMu.Unlock()

		switch {
		case waitErr != nil && ctx.Err() != nil:
			attempt.Status = RunStatusFailed
			attempt.Error = "interrupted by user"
			_ = s.updateLatestAttemptWithCtx(resolvedCtx, lt.task.File, lt.task.Task, func(r *TaskAttemptRecord) {
				r.Status = "failed"
				r.Error = "interrupted by user"
			})
		case wasBlocked:
			attempt.Status = RunStatusFailed
			attempt.Error = "blocked: " + blockedReason
			attempt.Reasons = append(attempt.Reasons, blockedReason)
			_ = s.updateLatestAttemptWithCtx(resolvedCtx, lt.task.File, lt.task.Task, func(r *TaskAttemptRecord) {
				r.Status = "failed"
				r.Error = "blocked: " + blockedReason
			})
		default:
			// Agent finished. Try to detect/create PR.
			prURL := detectOrCreatePR(lt.wtPath, lt.branch, lt.baseBranch, lt.task)
			if prURL != "" && lt.task.SpecPath != "" {
				_ = UpdateTaskMetadata(lt.task.SpecPath, lt.task.Task, map[string]string{
					"status": "in_review",
					"pr":     prURL,
				})
			}
			attempt.Status = RunStatusSucceeded
			_ = s.updateLatestAttemptWithCtx(resolvedCtx, lt.task.File, lt.task.Task, func(r *TaskAttemptRecord) {
				r.Status = "succeeded"
				r.PR = prURL
			})
		}

		attempts = append(attempts, attempt)
	}

	return attempts, nil
}

// handleAgentEvent reacts to an event emitted by a running agent:
// - blocked: stash the reason so the finalizer marks the task as failed
// - proposed_memory: append to inbox.md for human review
// - decision: parse details and persist via SaveDecision
// - other types: logged but no direct action (board web shows them)
func (s *Service) handleAgentEvent(
	resolvedCtx ResolvedContext,
	task RunPlanTask,
	ev Event,
	blockedReasons map[string]string,
	mu *sync.Mutex,
) {
	switch ev.Type {
	case EventTypeBlocked:
		mu.Lock()
		blockedReasons[task.Task] = ev.Message
		mu.Unlock()
	case EventTypeProposedMemory:
		entry := fmt.Sprintf("[%s, %s] %s", timeNow().UTC().Format("2006-01-02"), task.Task, ev.Message)
		_ = appendProposedMemoryToFile(resolvedCtx.InboxFile, entry)
	case EventTypeDecision:
		decision := Decision{
			SpecFile: task.File,
			TaskName: task.Task,
			Choice:   ev.Message,
		}
		// Extract rationale and alternatives from details if present.
		if len(ev.Details) > 0 {
			var details struct {
				Rationale    string   `json:"rationale"`
				Alternatives []string `json:"alternatives"`
			}
			if err := json.Unmarshal(ev.Details, &details); err == nil {
				decision.Rationale = details.Rationale
				decision.Alternatives = details.Alternatives
			}
		}
		_ = s.saveDecisionWithCtx(resolvedCtx, decision)
	}
}

// runIDFromStartTime extracts a deterministic run ID from a start timestamp.
// The startTime string is already RFC3339 from timeNow(); we format it as
// run-YYYYMMDD-HHMMSS to match generateRunID().
func runIDFromStartTime(startTime string) string {
	t, err := time.Parse(time.RFC3339, startTime)
	if err != nil {
		return generateRunID()
	}
	return fmt.Sprintf("run-%s", t.UTC().Format("20060102-150405"))
}

// BuildClaudeCommandWithHistory is a thin wrapper around HarnessBuilder
// that preserves the old signature for backward compat. New callers should
// construct a HarnessBuilder directly so they can set constraints, memory,
// state, and protocol options.
func BuildClaudeCommandWithHistory(
	worktreePath, projectRoot string,
	task RunPlanTask,
	kbContent, baseBranch string,
	pastAttempts []TaskAttemptRecord,
	pastFeedback *StructuredFeedback,
) string {
	h := &HarnessBuilder{
		Ctx:          ResolvedContext{ProjectRoot: projectRoot},
		Task:         task,
		WorktreePath: worktreePath,
		BaseBranch:   baseBranch,
		KBContent:    kbContent,
		PastAttempts: pastAttempts,
		PastFeedback: pastFeedback,
	}
	return h.Build()
}

// BuildClaudeCommand constructs the shell command to run claude inside a worktree.
// Deprecated: use BuildClaudeCommandWithHistory for new code. This wrapper is
// kept for backward compatibility with existing tests.
func BuildClaudeCommand(worktreePath, projectRoot string, task RunPlanTask, kbContent, baseBranch string) string {
	var prompt strings.Builder
	prompt.WriteString("You are working on a Forge task. Here is your context:\n\n")

	if task.SpecTitle != "" {
		fmt.Fprintf(&prompt, "## Spec: %s\n\n", task.SpecTitle)
	}
	fmt.Fprintf(&prompt, "## Task: %s\n\n", task.Task)

	if len(task.Deps) > 0 {
		fmt.Fprintf(&prompt, "Dependencies: %s\n\n", strings.Join(task.Deps, ", "))
	}
	if task.Repo != "" {
		fmt.Fprintf(&prompt, "Repo: %s\n\n", task.Repo)
	}
	if len(task.Touches) > 0 {
		fmt.Fprintf(&prompt, "Touches: %s\n\n", strings.Join(task.Touches, ", "))
	}

	prompt.WriteString("## Instructions\n\n")
	prompt.WriteString("1. Implement the task according to the done-when criteria in the spec.\n")
	prompt.WriteString("2. When finished, commit your changes and create a PR:\n")
	fmt.Fprintf(&prompt, "   git add -A && git commit -m 'feat: %s'\n", shellEscape(task.Task))
	fmt.Fprintf(&prompt, "   git push -u origin HEAD\n")
	fmt.Fprintf(&prompt, "   gh pr create --base %s --title '%s' --fill\n", baseBranch, shellEscape(task.Task))
	prompt.WriteString("3. After creating the PR, exit.\n")

	systemPrompt := ""
	if kbContent != "" {
		systemPrompt = kbContent + "\n\n"
	}
	systemPrompt += "You are a Forge agent working inside a git worktree. Follow the task instructions precisely."

	// Shell-safe command construction.
	escapedSystem := shellEscape(systemPrompt)
	escapedPrompt := shellEscape(prompt.String())

	return fmt.Sprintf("cd '%s' && claude --append-system-prompt '%s' -p '%s'",
		shellEscape(worktreePath), escapedSystem, escapedPrompt)
}

// detectOrCreatePR checks if a PR exists for the branch, and if not,
// attempts to create one via gh CLI.
// All gh/git calls use the worktree path as their working directory
// so gh operates on the correct repository.
func detectOrCreatePR(worktreePath, branch, baseBranch string, task RunPlanTask) string {
	// First check if the agent made commits in the worktree.
	out, err := runIn(worktreePath, "git", "log", "--oneline", "-1")
	if err != nil || strings.TrimSpace(string(out)) == "" {
		return "" // no commits in worktree
	}

	// Try to detect existing PR for this branch.
	prOut, err := runIn(worktreePath, "gh", "pr", "view", branch, "--json", "url", "-q", ".url")
	if err == nil {
		url := strings.TrimSpace(string(prOut))
		if url != "" {
			return url
		}
	}

	// Push the branch to origin.
	if _, err := runIn(worktreePath, "git", "push", "-u", "origin", "HEAD"); err != nil {
		return ""
	}

	// Create the PR.
	prOut, err = runIn(worktreePath, "gh", "pr", "create",
		"--base", baseBranch,
		"--head", branch,
		"--title", fmt.Sprintf("feat: %s", task.Task),
		"--body", fmt.Sprintf("Forge task: %s\nSpec: %s\n\nAutomated by `forge run execute --live`", task.Task, task.SpecTitle),
	)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(prOut))
}

func bucketForTask(task RunPlanTask) string {
	if task.Parallelizable == "yes" {
		return "safe"
	}
	return "serial"
}

func shellEscape(s string) string {
	return strings.ReplaceAll(s, "'", "'\\''")
}

func collectKBContent(kbDir string) string {
	if kbDir == "" {
		return ""
	}
	var parts []string
	_ = filepath.WalkDir(kbDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		if !strings.HasSuffix(strings.ToLower(d.Name()), ".md") {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		parts = append(parts, fmt.Sprintf("## KB: %s\n\n%s", d.Name(), string(data)))
		return nil
	})
	return strings.Join(parts, "\n\n---\n\n")
}

func concatPlanTasks(plan RunPlan) []RunPlanTask {
	var all []RunPlanTask
	all = append(all, plan.Safe...)
	all = append(all, plan.Serial...)
	all = append(all, plan.Conflict...)
	all = append(all, plan.Blocked...)
	all = append(all, plan.Invalid...)
	all = append(all, plan.Active...)
	all = append(all, plan.Done...)
	return all
}
