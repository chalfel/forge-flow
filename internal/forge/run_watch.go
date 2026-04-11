package forge

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"
)

// WatchConfig holds configuration for the watch daemon.
type WatchConfig struct {
	CWD          string
	SpecPath     string
	PollInterval time.Duration
	AutoReview   bool
}

// watchedTask tracks a running task in the watch loop.
type watchedTask struct {
	task       RunPlanTask
	paneID     string
	wtPath     string
	branch     string
	baseBranch string
	eventsFile string
}

// WatchRun starts a polling loop that monitors spec status changes and
// auto-spawns agents when tasks become ready. Blocks until ctx is cancelled.
func (s *Service) WatchRun(ctx context.Context, cfg WatchConfig) error {
	if err := EnsureTmuxAvailable(); err != nil {
		return err
	}
	if err := EnsureClaudeAvailable(); err != nil {
		return err
	}

	if cfg.PollInterval <= 0 {
		cfg.PollInterval = 10 * time.Second
	}

	// Determine tmux session.
	sessionName, insideTmux := DetectTmux()
	if !insideTmux {
		sessionName = "forge-watch"
		if err := CreateTmuxSession(sessionName); err != nil {
			return err
		}
	}

	var mu sync.Mutex
	running := map[string]*watchedTask{} // keyed by lowercase task name

	fmt.Printf("Forge watch started (poll every %s, session: %s)\n", cfg.PollInterval, sessionName)
	if cfg.AutoReview {
		fmt.Println("Auto-review: enabled")
	}

	ticker := time.NewTicker(cfg.PollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			fmt.Println("\nForge watch shutting down. Running panes will continue.")
			return nil
		case <-ticker.C:
			s.watchTick(ctx, cfg, sessionName, &mu, running)
		}
	}
}

func (s *Service) watchTick(
	ctx context.Context,
	cfg WatchConfig,
	session string,
	mu *sync.Mutex,
	running map[string]*watchedTask,
) {
	plan, err := s.PlanRun(cfg.CWD, cfg.SpecPath)
	if err != nil {
		fmt.Printf("watch: plan error: %v\n", err)
		return
	}

	resolvedCtx, err := s.ResolveContext(cfg.CWD)
	if err != nil {
		fmt.Printf("watch: context error: %v\n", err)
		return
	}

	kbContent := collectKBContent(resolvedCtx.KBDir)

	// Build task branch map.
	taskBranchMap := map[string]string{}
	for _, task := range concatPlanTasks(plan) {
		specBranch := task.SpecBranch
		if specBranch == "" {
			specBranch = DefaultSpecBranch(task.SpecTitle)
		}
		taskBranchMap[strings.ToLower(strings.TrimSpace(task.Task))] = TaskBranchName(specBranch, task.Task)
	}

	mu.Lock()
	defer mu.Unlock()

	// Check running panes for completion.
	for key, wt := range running {
		alive, err := IsPaneAlive(wt.paneID)
		if err != nil {
			fmt.Printf("watch: pane check error for %s: %v\n", wt.task.Task, err)
			continue
		}
		if alive {
			continue
		}

		fmt.Printf("watch: task %q completed\n", wt.task.Task)

		// Process any events the agent wrote before we create the PR.
		if wt.eventsFile != "" {
			events, _ := LoadEvents(wt.eventsFile)
			dummyBlocked := map[string]string{}
			var dummyMu sync.Mutex
			for _, ev := range events {
				s.handleAgentEvent(resolvedCtx, wt.task, ev, dummyBlocked, &dummyMu)
			}
		}

		// Task finished. Create PR and update spec.
		prURL := detectOrCreatePR(wt.wtPath, wt.branch, wt.baseBranch, wt.task)
		if prURL != "" && wt.task.SpecPath != "" {
			_ = UpdateTaskMetadata(wt.task.SpecPath, wt.task.Task, map[string]string{
				"status": "in_review",
				"pr":     prURL,
			})
			fmt.Printf("watch: PR created for %q: %s\n", wt.task.Task, prURL)
		} else if wt.task.SpecPath != "" {
			_ = UpdateTaskMetadata(wt.task.SpecPath, wt.task.Task, map[string]string{
				"status": "in_review",
			})
		}

		// Update the history attempt record with completion.
		_ = s.updateLatestAttemptWithCtx(resolvedCtx, wt.task.File, wt.task.Task, func(r *TaskAttemptRecord) {
			r.Status = "succeeded"
			r.PR = prURL
		})

		// Auto-review: spawn a review agent.
		if cfg.AutoReview && wt.task.SpecPath != "" {
			s.spawnReviewAgent(session, wt)
		}

		delete(running, key)
	}

	// Spawn new ready tasks.
	ready := append([]RunPlanTask(nil), plan.Safe...)
	ready = append(ready, plan.Serial...)

	for _, task := range ready {
		key := strings.ToLower(strings.TrimSpace(task.Task))
		if _, isRunning := running[key]; isRunning {
			continue
		}

		specBranch := task.SpecBranch
		if specBranch == "" {
			specBranch = DefaultSpecBranch(task.SpecTitle)
		}

		repoRoot, err := ResolveRepoRoot(resolvedCtx, task.Repo)
		if err != nil {
			fmt.Printf("watch: repo resolution for %q: %v\n", task.Task, err)
			continue
		}

		depBranch := ResolveDepBranch(specBranch, task.Deps, taskBranchMap)
		baseBranch := "main"
		if depBranch != "" {
			baseBranch = depBranch
		}

		wtPath, branch, err := CreateWorktree(repoRoot, specBranch, task.Task, depBranch)
		if err != nil {
			fmt.Printf("watch: worktree for %q: %v\n", task.Task, err)
			continue
		}

		if task.SpecPath != "" {
			_ = UpdateTaskMetadata(task.SpecPath, task.Task, map[string]string{"status": "in_progress"})
		}

		// Load history before recording the new attempt.
		pastAttempts, _ := s.loadAttemptsWithCtx(resolvedCtx, task.File, task.Task)
		pastFeedback, _ := s.loadFeedbackWithCtx(resolvedCtx, task.File, task.Task)
		_ = s.appendAttemptWithCtx(resolvedCtx, TaskAttemptRecord{
			RunID:    "watch-" + timeNow().UTC().Format("20060102-150405"),
			SpecFile: task.File,
			TaskName: task.Task,
			Status:   "running",
		})

		watchRunID := "watch-" + timeNow().UTC().Format("20060102-150405")
		eventsFile := EventsFilePath(resolvedCtx, watchRunID, task.Task)
		_ = EnsureEventsFile(eventsFile)

		harness := &HarnessBuilder{
			Ctx:          resolvedCtx,
			Task:         task,
			WorktreePath: wtPath,
			BaseBranch:   baseBranch,
			KBContent:    kbContent,
			PastAttempts: pastAttempts,
			PastFeedback: pastFeedback,
			EventsFile:   eventsFile,
			RunID:        watchRunID,
		}
		cmd := fmt.Sprintf("export FORGE_EVENTS_FILE='%s' && %s", shellEscape(eventsFile), harness.Build())
		slug := slugify(task.Task)
		paneID, err := NewTmuxWindow(session, slug)
		if err != nil {
			fmt.Printf("watch: tmux window for %q: %v\n", task.Task, err)
			continue
		}
		if err := SendKeys(paneID, cmd); err != nil {
			fmt.Printf("watch: send keys for %q: %v\n", task.Task, err)
			continue
		}

		running[key] = &watchedTask{
			task: task, paneID: paneID, wtPath: wtPath,
			branch: branch, baseBranch: baseBranch,
			eventsFile: eventsFile,
		}
		fmt.Printf("watch: spawned agent for %q (pane: %s)\n", task.Task, paneID)
	}
}

func (s *Service) spawnReviewAgent(session string, wt *watchedTask) {
	reviewPrompt := fmt.Sprintf(
		"Review the changes in this worktree for task %q (spec: %s). "+
			"Check if the done-when criteria are met. "+
			"If changes are needed, list them clearly. "+
			"If everything looks good, say APPROVED.",
		wt.task.Task, wt.task.SpecTitle,
	)
	slug := "review-" + slugify(wt.task.Task)
	reviewCmd := fmt.Sprintf("cd %s && claude -p '%s'", wt.wtPath, shellEscape(reviewPrompt))

	paneID, err := NewTmuxWindow(session, slug)
	if err != nil {
		fmt.Printf("watch: review pane for %q: %v\n", wt.task.Task, err)
		return
	}
	if err := SendKeys(paneID, reviewCmd); err != nil {
		fmt.Printf("watch: review keys for %q: %v\n", wt.task.Task, err)
		return
	}
	fmt.Printf("watch: spawned review for %q (pane: %s)\n", wt.task.Task, paneID)
}
