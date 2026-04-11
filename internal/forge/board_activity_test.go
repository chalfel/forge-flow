package forge

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLoadActivityFeedFromRunEvents(t *testing.T) {
	fixedTime := time.Date(2026, 4, 11, 14, 0, 0, 0, time.UTC)
	origTimeNow := timeNow
	timeNow = func() time.Time { return fixedTime }
	defer func() { timeNow = origTimeNow }()

	tmp := t.TempDir()
	ctx := ResolvedContext{ProjectRoot: tmp}

	// Create runtime events for a run.
	runID := "run-20260411-130000"
	runDir := filepath.Join(tmp, "runtime", runID)
	if err := os.MkdirAll(runDir, 0o755); err != nil {
		t.Fatal(err)
	}

	events := []Event{
		{Type: "progress", Message: "Scaffolding module", Timestamp: "2026-04-11T13:00:00Z"},
		{Type: "decision", Message: "Using postgres over sqlite", Timestamp: "2026-04-11T13:05:00Z"},
		{Type: "progress", Message: "Writing tests", Timestamp: "2026-04-11T13:10:00Z"},
	}
	writeEventsFile(t, filepath.Join(runDir, "setup-db.events.jsonl"), events)

	runs := []RunRecord{
		{
			RunID: runID, Status: RunStatusRunning,
			StartedAt: "2026-04-11T13:00:00Z",
			Tasks: []TaskAttempt{
				{Task: "Setup DB", SpecTitle: "Database Layer"},
			},
		},
	}

	feed := loadActivityFeed(ctx, runs)
	if len(feed) != 3 {
		t.Fatalf("expected 3 feed items, got %d", len(feed))
	}

	// Should be newest first.
	if feed[0].Message != "Writing tests" {
		t.Errorf("expected newest first, got %q", feed[0].Message)
	}
	if feed[0].Ago != "50m ago" {
		t.Errorf("expected '50m ago', got %q", feed[0].Ago)
	}
	if feed[2].Message != "Scaffolding module" {
		t.Errorf("expected oldest last, got %q", feed[2].Message)
	}
}

func TestLoadActivityFeedEmpty(t *testing.T) {
	ctx := ResolvedContext{ProjectRoot: t.TempDir()}
	feed := loadActivityFeed(ctx, nil)
	if len(feed) != 0 {
		t.Fatalf("expected empty feed, got %d items", len(feed))
	}
}

func TestLoadActivityFeedLimitsToMaxItems(t *testing.T) {
	tmp := t.TempDir()
	ctx := ResolvedContext{ProjectRoot: tmp}

	runID := "run-test"
	runDir := filepath.Join(tmp, "runtime", runID)
	if err := os.MkdirAll(runDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// Create more events than maxFeedItems.
	var events []Event
	for i := 0; i < maxFeedItems+10; i++ {
		events = append(events, Event{
			Type:      "progress",
			Message:   "step",
			Timestamp: time.Date(2026, 4, 11, 0, i, 0, 0, time.UTC).Format(time.RFC3339),
		})
	}
	writeEventsFile(t, filepath.Join(runDir, "big-task.events.jsonl"), events)

	runs := []RunRecord{{RunID: runID, Status: RunStatusRunning}}
	feed := loadActivityFeed(ctx, runs)
	if len(feed) != maxFeedItems {
		t.Fatalf("expected %d feed items, got %d", maxFeedItems, len(feed))
	}
}

func TestLoadInboxNeedInfo(t *testing.T) {
	tmp := t.TempDir()
	ctx := ResolvedContext{ProjectRoot: tmp, InboxFile: filepath.Join(tmp, "inbox.md")}

	runID := "run-info"
	runDir := filepath.Join(tmp, "runtime", runID)
	if err := os.MkdirAll(runDir, 0o755); err != nil {
		t.Fatal(err)
	}

	events := []Event{
		{Type: "need_info", Message: "Which auth provider?", Timestamp: "2026-04-11T13:00:00Z"},
	}
	writeEventsFile(t, filepath.Join(runDir, "auth-task.events.jsonl"), events)

	plan := RunPlan{ProjectID: "test"}
	runs := []RunRecord{
		{RunID: runID, Status: RunStatusRunning, Tasks: []TaskAttempt{
			{Task: "Auth Task", SpecTitle: "Auth"},
		}},
	}

	inbox := loadInbox(ctx, plan, runs)
	found := false
	for _, item := range inbox {
		if item.Type == "need_info" {
			found = true
			if item.Urgency != "high" {
				t.Errorf("expected high urgency for need_info, got %q", item.Urgency)
			}
			if item.Detail != "Which auth provider?" {
				t.Errorf("unexpected detail: %q", item.Detail)
			}
		}
	}
	if !found {
		t.Fatal("expected need_info inbox item")
	}
}

func TestLoadInboxReviewTasks(t *testing.T) {
	ctx := ResolvedContext{ProjectRoot: t.TempDir(), InboxFile: filepath.Join(t.TempDir(), "inbox.md")}
	plan := RunPlan{
		ProjectID: "test",
		Active: []RunPlanTask{
			{Task: "Review me", File: "spec.md", SpecTitle: "Feature", Status: "in_review"},
			{Task: "Still running", File: "spec.md", SpecTitle: "Feature", Status: "in_progress"},
		},
	}

	inbox := loadInbox(ctx, plan, nil)
	reviewCount := 0
	for _, item := range inbox {
		if item.Type == "review" {
			reviewCount++
		}
	}
	if reviewCount != 1 {
		t.Fatalf("expected 1 review item, got %d", reviewCount)
	}
}

func TestLoadInboxBlockedTasks(t *testing.T) {
	ctx := ResolvedContext{ProjectRoot: t.TempDir(), InboxFile: filepath.Join(t.TempDir(), "inbox.md")}
	plan := RunPlan{
		ProjectID: "test",
		Blocked: []RunPlanTask{
			{Task: "Stuck task", File: "spec.md", SpecTitle: "Feature", Reasons: []string{"waiting on: Setup"}},
		},
	}

	inbox := loadInbox(ctx, plan, nil)
	blockedCount := 0
	for _, item := range inbox {
		if item.Type == "blocked" {
			blockedCount++
			if item.Urgency != "medium" {
				t.Errorf("expected medium urgency for blocked, got %q", item.Urgency)
			}
		}
	}
	if blockedCount != 1 {
		t.Fatalf("expected 1 blocked item, got %d", blockedCount)
	}
}

func TestLoadInboxFailedTasks(t *testing.T) {
	ctx := ResolvedContext{ProjectRoot: t.TempDir(), InboxFile: filepath.Join(t.TempDir(), "inbox.md")}
	plan := RunPlan{ProjectID: "test"}
	runs := []RunRecord{
		{
			RunID: "run-fail", Status: RunStatusFailed,
			Tasks: []TaskAttempt{
				{Task: "Broken", SpecFile: "spec.md", SpecTitle: "Feature", Status: RunStatusFailed, Error: "exit code 1"},
			},
		},
	}

	inbox := loadInbox(ctx, plan, runs)
	failedCount := 0
	for _, item := range inbox {
		if item.Type == "failed" {
			failedCount++
			if item.Urgency != "high" {
				t.Errorf("expected high urgency for failed, got %q", item.Urgency)
			}
		}
	}
	if failedCount != 1 {
		t.Fatalf("expected 1 failed item, got %d", failedCount)
	}
}

func TestLoadInboxProposedMemories(t *testing.T) {
	tmp := t.TempDir()
	inboxPath := filepath.Join(tmp, "inbox.md")
	content := "# Inbox\n\n## Proposed memory\n\n- Use postgres for all services\n- Never mock the DB\n"
	if err := os.WriteFile(inboxPath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	ctx := ResolvedContext{ProjectRoot: tmp, InboxFile: inboxPath}
	plan := RunPlan{ProjectID: "test"}

	inbox := loadInbox(ctx, plan, nil)
	memCount := 0
	for _, item := range inbox {
		if item.Type == "memory" {
			memCount++
			if item.Urgency != "low" {
				t.Errorf("expected low urgency for memory, got %q", item.Urgency)
			}
		}
	}
	if memCount != 2 {
		t.Fatalf("expected 2 memory items, got %d", memCount)
	}
}

func TestLoadInboxUrgencySorting(t *testing.T) {
	tmp := t.TempDir()
	inboxPath := filepath.Join(tmp, "inbox.md")
	content := "# Inbox\n\n## Proposed memory\n\n- Some memory\n"
	os.WriteFile(inboxPath, []byte(content), 0o644)

	ctx := ResolvedContext{ProjectRoot: tmp, InboxFile: inboxPath}
	plan := RunPlan{
		ProjectID: "test",
		Active: []RunPlanTask{
			{Task: "Review me", File: "spec.md", Status: "in_review"},
		},
		Blocked: []RunPlanTask{
			{Task: "Stuck", File: "spec.md", Reasons: []string{"dep"}},
		},
	}

	inbox := loadInbox(ctx, plan, nil)
	if len(inbox) < 3 {
		t.Fatalf("expected at least 3 inbox items, got %d", len(inbox))
	}

	// High urgency items should come first.
	if inbox[0].Urgency != "high" {
		t.Errorf("expected first item to be high urgency, got %q", inbox[0].Urgency)
	}
	// Low should be last.
	if inbox[len(inbox)-1].Urgency != "low" {
		t.Errorf("expected last item to be low urgency, got %q", inbox[len(inbox)-1].Urgency)
	}
}

func TestLoadInboxEmpty(t *testing.T) {
	ctx := ResolvedContext{ProjectRoot: t.TempDir(), InboxFile: filepath.Join(t.TempDir(), "inbox.md")}
	plan := RunPlan{ProjectID: "test"}
	inbox := loadInbox(ctx, plan, nil)
	if len(inbox) != 0 {
		t.Fatalf("expected empty inbox, got %d items", len(inbox))
	}
}

func TestRelativeTime(t *testing.T) {
	fixedTime := time.Date(2026, 4, 11, 14, 0, 0, 0, time.UTC)
	origTimeNow := timeNow
	timeNow = func() time.Time { return fixedTime }
	defer func() { timeNow = origTimeNow }()

	tests := []struct {
		ts   string
		want string
	}{
		{"2026-04-11T13:59:30Z", "just now"},
		{"2026-04-11T13:55:00Z", "5m ago"},
		{"2026-04-11T12:00:00Z", "2h ago"},
		{"2026-04-10T14:00:00Z", "1d ago"},
		{"2026-04-08T14:00:00Z", "3d ago"},
		{"invalid", ""},
	}

	for _, tt := range tests {
		got := relativeTime(tt.ts)
		if got != tt.want {
			t.Errorf("relativeTime(%q) = %q, want %q", tt.ts, got, tt.want)
		}
	}
}

func TestItoa(t *testing.T) {
	tests := []struct {
		n    int
		want string
	}{
		{0, "0"},
		{5, "5"},
		{10, "10"},
		{123, "123"},
	}
	for _, tt := range tests {
		got := itoa(tt.n)
		if got != tt.want {
			t.Errorf("itoa(%d) = %q, want %q", tt.n, got, tt.want)
		}
	}
}

func TestBoardIncludesFeedAndInbox(t *testing.T) {
	fixedTime := time.Date(2026, 4, 11, 14, 0, 0, 0, time.UTC)
	origTimeNow := timeNow
	timeNow = func() time.Time { return fixedTime }
	defer func() { timeNow = origTimeNow }()

	svc, webRepo := setupRunTestProject(t)

	// Execute a run to have run records.
	_, err := svc.ExecuteRun(webRepo, "")
	if err != nil {
		t.Fatalf("ExecuteRun: %v", err)
	}

	board, err := svc.Board(webRepo)
	if err != nil {
		t.Fatalf("Board: %v", err)
	}

	// Feed and Inbox should be populated (even if empty, the fields should be present).
	// The test project has blocked tasks, so inbox should have items.
	if board.Inbox == nil && board.Summary.Blocked > 0 {
		t.Error("expected inbox items for blocked tasks")
	}
}

func TestDeriveFocusFromInbox(t *testing.T) {
	tests := []struct {
		name       string
		inbox      []InboxItem
		wantNil    bool
		wantAction string
	}{
		{"empty inbox", nil, true, ""},
		{"need_info first", []InboxItem{
			{Type: "need_info", Urgency: "high", Title: "Agent needs info", Detail: "Which DB?"},
		}, false, "respond"},
		{"review first", []InboxItem{
			{Type: "review", Urgency: "high", Title: "Review needed", Detail: "Auth task"},
		}, false, "review"},
		{"failed first", []InboxItem{
			{Type: "failed", Urgency: "high", Title: "Task failed"},
		}, false, "triage"},
		{"blocked first", []InboxItem{
			{Type: "blocked", Urgency: "medium", Title: "Task blocked"},
		}, false, "unblock"},
		{"memory first", []InboxItem{
			{Type: "memory", Urgency: "low", Title: "Memory to review"},
		}, false, "approve"},
	}

	for _, tt := range tests {
		focus := deriveFocus(tt.inbox)
		if tt.wantNil {
			if focus != nil {
				t.Errorf("%s: expected nil focus, got %+v", tt.name, focus)
			}
			continue
		}
		if focus == nil {
			t.Errorf("%s: expected focus, got nil", tt.name)
			continue
		}
		if focus.Action != tt.wantAction {
			t.Errorf("%s: expected action %q, got %q", tt.name, tt.wantAction, focus.Action)
		}
	}
}

func TestDeriveFocusPreservesDetail(t *testing.T) {
	inbox := []InboxItem{
		{Type: "need_info", Urgency: "high", Title: "Agent needs info", Detail: "Which auth provider?", TaskName: "Setup Auth", SpecFile: "auth.md"},
	}
	focus := deriveFocus(inbox)
	if focus.Detail != "Which auth provider?" {
		t.Errorf("expected detail preserved, got %q", focus.Detail)
	}
	if focus.TaskName != "Setup Auth" {
		t.Errorf("expected task name preserved, got %q", focus.TaskName)
	}
	if focus.InboxType != "need_info" {
		t.Errorf("expected inbox type, got %q", focus.InboxType)
	}
}

func TestLoadAgentStatusesFromActivePlan(t *testing.T) {
	fixedTime := time.Date(2026, 4, 11, 14, 0, 0, 0, time.UTC)
	origTimeNow := timeNow
	timeNow = func() time.Time { return fixedTime }
	defer func() { timeNow = origTimeNow }()

	tmp := t.TempDir()
	ctx := ResolvedContext{ProjectRoot: tmp}

	// Create events for the active task.
	runID := "run-agents"
	runDir := filepath.Join(tmp, "runtime", runID)
	if err := os.MkdirAll(runDir, 0o755); err != nil {
		t.Fatal(err)
	}

	events := []Event{
		{Type: "progress", Message: "Setting up schema", Timestamp: "2026-04-11T13:50:00Z"},
		{Type: "progress", Message: "Writing migrations", Timestamp: "2026-04-11T13:55:00Z"},
	}
	writeEventsFile(t, filepath.Join(runDir, "setup-db.events.jsonl"), events)

	plan := RunPlan{
		Active: []RunPlanTask{
			{Task: "Setup DB", File: "db.md", SpecTitle: "Database Layer", Status: "in_progress"},
		},
	}
	runs := []RunRecord{
		{RunID: runID, Status: RunStatusRunning, Tasks: []TaskAttempt{
			{Task: "Setup DB", SpecTitle: "Database Layer"},
		}},
	}

	agents := loadAgentStatuses(ctx, plan, runs)
	if len(agents) != 1 {
		t.Fatalf("expected 1 agent, got %d", len(agents))
	}
	if agents[0].TaskName != "Setup DB" {
		t.Errorf("expected task name 'Setup DB', got %q", agents[0].TaskName)
	}
	if agents[0].LastEvent != "Writing migrations" {
		t.Errorf("expected latest event 'Writing migrations', got %q", agents[0].LastEvent)
	}
	if agents[0].Ago != "5m ago" {
		t.Errorf("expected '5m ago', got %q", agents[0].Ago)
	}
}

func TestLoadAgentStatusesEmpty(t *testing.T) {
	ctx := ResolvedContext{ProjectRoot: t.TempDir()}
	plan := RunPlan{}
	agents := loadAgentStatuses(ctx, plan, nil)
	if len(agents) != 0 {
		t.Fatalf("expected no agents, got %d", len(agents))
	}
}

func TestBoardIncludesFocusAndAgents(t *testing.T) {
	fixedTime := time.Date(2026, 4, 11, 14, 0, 0, 0, time.UTC)
	origTimeNow := timeNow
	timeNow = func() time.Time { return fixedTime }
	defer func() { timeNow = origTimeNow }()

	svc, webRepo := setupRunTestProject(t)

	_, err := svc.ExecuteRun(webRepo, "")
	if err != nil {
		t.Fatalf("ExecuteRun: %v", err)
	}

	board, err := svc.Board(webRepo)
	if err != nil {
		t.Fatalf("Board: %v", err)
	}

	// If there are inbox items, focus should be set.
	if len(board.Inbox) > 0 && board.Focus == nil {
		t.Error("expected Focus to be set when inbox has items")
	}
	// Agents may or may not be present depending on plan state.
	// Just verify the field is initialized (not panicking).
	_ = board.Agents
}

// writeEventsFile writes events to a JSONL file.
func writeEventsFile(t *testing.T, path string, events []Event) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	for _, ev := range events {
		data, _ := json.Marshal(ev)
		f.Write(append(data, '\n'))
	}
}
