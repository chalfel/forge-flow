package forge

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// ActivityItem is a single entry in the board's real-time activity feed.
// It represents an agent event — progress updates, decisions, blockers —
// giving the operator a heartbeat view of what agents are doing right now.
type ActivityItem struct {
	Timestamp string `json:"timestamp"`
	Type      string `json:"type"`      // progress|decision|blocked|need_info|handoff|proposed_memory
	Message   string `json:"message"`
	TaskName  string `json:"taskName"`
	SpecTitle string `json:"specTitle,omitempty"`
	RunID     string `json:"runId"`
	Ago       string `json:"ago"` // human-readable relative time
}

// InboxItem is something that needs the operator's attention.
// The inbox answers "what needs me?" so the operator can act in <30s.
type InboxItem struct {
	Type      string `json:"type"`      // blocked|need_info|review|memory|failed
	Urgency   string `json:"urgency"`   // high|medium|low
	Title     string `json:"title"`
	Detail    string `json:"detail,omitempty"`
	SpecFile  string `json:"specFile,omitempty"`
	SpecTitle string `json:"specTitle,omitempty"`
	TaskName  string `json:"taskName,omitempty"`
	RunID     string `json:"runId,omitempty"`
	Timestamp string `json:"timestamp,omitempty"`
}

// FocusItem is the single most important thing the operator should do right now.
// Derived from the inbox — if there's nothing to focus on, the operator is free.
type FocusItem struct {
	Action    string `json:"action"`              // verb: review|respond|triage|unblock|approve
	Title     string `json:"title"`
	Detail    string `json:"detail,omitempty"`
	SpecFile  string `json:"specFile,omitempty"`
	TaskName  string `json:"taskName,omitempty"`
	Command   string `json:"command,omitempty"`   // suggested CLI command or tmux attach
	InboxType string `json:"inboxType"`           // source inbox item type
}

// AgentStatus tracks a live agent working on a task.
type AgentStatus struct {
	TaskName   string `json:"taskName"`
	SpecTitle  string `json:"specTitle,omitempty"`
	SpecFile   string `json:"specFile"`
	Status     string `json:"status"`             // in_progress|in_review
	PaneID     string `json:"paneId,omitempty"`   // tmux pane for joining
	Session    string `json:"session,omitempty"`   // tmux session name
	LastEvent  string `json:"lastEvent,omitempty"` // latest progress message
	LastEventAt string `json:"lastEventAt,omitempty"`
	Ago        string `json:"ago,omitempty"`
}

const maxFeedItems = 30

// loadActivityFeed collects recent events from the most recent runs and
// returns them as a unified, reverse-chronological activity stream.
func loadActivityFeed(ctx ResolvedContext, runs []RunRecord) []ActivityItem {
	// Collect events from recent runs (up to 3).
	limit := 3
	if len(runs) < limit {
		limit = len(runs)
	}

	var all []ActivityItem
	for _, run := range runs[:limit] {
		events := loadRunEventsFromDir(ctx, run.RunID)
		specTitles := specTitlesFromRun(run)
		for _, ev := range events {
			all = append(all, ActivityItem{
				Timestamp: ev.Timestamp,
				Type:      ev.Type,
				Message:   ev.Message,
				TaskName:  ev.TaskName,
				SpecTitle: specTitles[ev.TaskName],
				RunID:     run.RunID,
				Ago:       relativeTime(ev.Timestamp),
			})
		}
	}

	// Sort newest first.
	sort.Slice(all, func(i, j int) bool {
		return all[i].Timestamp > all[j].Timestamp
	})

	if len(all) > maxFeedItems {
		all = all[:maxFeedItems]
	}
	return all
}

// loadInbox builds the operator's attention queue from plan state, run events,
// and proposed memories. Items are sorted by urgency (high first).
func loadInbox(ctx ResolvedContext, plan RunPlan, runs []RunRecord) []InboxItem {
	var items []InboxItem

	// 1. Need-info events from active runs — highest urgency, agent is waiting.
	if len(runs) > 0 {
		latest := runs[0]
		if latest.Status == RunStatusRunning {
			events := loadRunEventsFromDir(ctx, latest.RunID)
			specTitles := specTitlesFromRun(latest)
			for _, ev := range events {
				if ev.Type == EventTypeNeedInfo {
					items = append(items, InboxItem{
						Type:      "need_info",
						Urgency:   "high",
						Title:     "Agent needs info",
						Detail:    ev.Message,
						TaskName:  ev.TaskName,
						SpecTitle: specTitles[ev.TaskName],
						RunID:     latest.RunID,
						Timestamp: ev.Timestamp,
					})
				}
			}
		}
	}

	// 2. Tasks in review — operator judgment needed.
	for _, t := range plan.Active {
		if t.Status == "in_review" {
			items = append(items, InboxItem{
				Type:      "review",
				Urgency:   "high",
				Title:     "Review needed",
				Detail:    t.Task,
				SpecFile:  t.File,
				SpecTitle: t.SpecTitle,
				TaskName:  t.Task,
			})
		}
	}

	// 3. Failed tasks from the latest run — needs triage.
	if len(runs) > 0 {
		latest := runs[0]
		for _, ta := range latest.Tasks {
			if ta.Status == RunStatusFailed {
				items = append(items, InboxItem{
					Type:      "failed",
					Urgency:   "high",
					Title:     "Task failed",
					Detail:    ta.Error,
					SpecFile:  ta.SpecFile,
					SpecTitle: ta.SpecTitle,
					TaskName:  ta.Task,
					RunID:     latest.RunID,
					Timestamp: ta.EndedAt,
				})
			}
		}
	}

	// 4. Blocked tasks — may need operator unblocking.
	for _, t := range plan.Blocked {
		reason := ""
		if len(t.Reasons) > 0 {
			reason = strings.Join(t.Reasons, "; ")
		}
		items = append(items, InboxItem{
			Type:      "blocked",
			Urgency:   "medium",
			Title:     "Task blocked",
			Detail:    reason,
			SpecFile:  t.File,
			SpecTitle: t.SpecTitle,
			TaskName:  t.Task,
		})
	}

	// 5. Proposed memories — low urgency, review when convenient.
	memories := loadProposedMemoriesQuiet(ctx.InboxFile)
	for _, m := range memories {
		items = append(items, InboxItem{
			Type:    "memory",
			Urgency: "low",
			Title:   "Memory to review",
			Detail:  m.Text,
		})
	}

	// Sort by urgency: high > medium > low.
	urgencyOrder := map[string]int{"high": 0, "medium": 1, "low": 2}
	sort.SliceStable(items, func(i, j int) bool {
		return urgencyOrder[items[i].Urgency] < urgencyOrder[items[j].Urgency]
	})

	return items
}

// loadRunEventsFromDir loads all events for a run from the runtime directory.
// Returns nil on any error (best-effort for the feed).
func loadRunEventsFromDir(ctx ResolvedContext, runID string) []Event {
	base := ctx.RepoLocalForge
	if base == "" {
		base = ctx.ProjectRoot
	}
	runDir := filepath.Join(base, "runtime", runID)

	entries, err := os.ReadDir(runDir)
	if err != nil {
		return nil
	}

	var all []Event
	for _, entry := range entries {
		name := entry.Name()
		if !strings.HasSuffix(name, ".events.jsonl") {
			continue
		}
		taskSlug := strings.TrimSuffix(name, ".events.jsonl")
		events, err := LoadEvents(filepath.Join(runDir, name))
		if err != nil {
			continue
		}
		for i := range events {
			events[i].TaskName = taskSlug
			events[i].RunID = runID
		}
		all = append(all, events...)
	}
	return all
}

// specTitlesFromRun builds a taskSlug -> specTitle map from a run record.
func specTitlesFromRun(run RunRecord) map[string]string {
	m := make(map[string]string, len(run.Tasks))
	for _, ta := range run.Tasks {
		m[slugify(ta.Task)] = ta.SpecTitle
	}
	return m
}

// loadProposedMemoriesQuiet loads proposed memories without returning errors.
func loadProposedMemoriesQuiet(inboxPath string) []ProposedMemory {
	memories, _ := loadProposedMemoriesFromFile(inboxPath)
	return memories
}

// relativeTime converts an RFC3339 timestamp into a human-readable "ago" string.
func relativeTime(ts string) string {
	t, err := time.Parse(time.RFC3339, ts)
	if err != nil {
		return ""
	}
	d := timeNow().Sub(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		m := int(d.Minutes())
		if m == 1 {
			return "1m ago"
		}
		return itoa(m) + "m ago"
	case d < 24*time.Hour:
		h := int(d.Hours())
		if h == 1 {
			return "1h ago"
		}
		return itoa(h) + "h ago"
	default:
		days := int(d.Hours() / 24)
		if days == 1 {
			return "1d ago"
		}
		return itoa(days) + "d ago"
	}
}

// deriveFocus picks the single highest-priority action from the inbox.
// Returns nil if the inbox is empty (operator is free).
func deriveFocus(inbox []InboxItem) *FocusItem {
	if len(inbox) == 0 {
		return nil
	}

	// The inbox is already sorted by urgency, so take the first item.
	top := inbox[0]

	action := "check"
	switch top.Type {
	case "need_info":
		action = "respond"
	case "review":
		action = "review"
	case "failed":
		action = "triage"
	case "blocked":
		action = "unblock"
	case "memory":
		action = "approve"
	}

	cmd := ""
	switch top.Type {
	case "review":
		if top.SpecFile != "" {
			cmd = "forge board --web  # then click spec → review PR"
		}
	case "need_info":
		if top.TaskName != "" {
			cmd = "tmux attach  # join the agent session to respond"
		}
	case "failed":
		if top.SpecFile != "" && top.TaskName != "" {
			cmd = "forge history show --spec " + top.SpecFile + " --task \"" + top.TaskName + "\""
		}
	}

	return &FocusItem{
		Action:    action,
		Title:     top.Title,
		Detail:    top.Detail,
		SpecFile:  top.SpecFile,
		TaskName:  top.TaskName,
		Command:   cmd,
		InboxType: top.Type,
	}
}

// loadAgentStatuses builds a list of active agents from the run plan and
// their latest events. Each entry shows the task, its latest progress, and
// the tmux session info for joining.
func loadAgentStatuses(ctx ResolvedContext, plan RunPlan, runs []RunRecord) []AgentStatus {
	if len(plan.Active) == 0 {
		return nil
	}

	// Load events once from the most recent run to avoid N+1 disk reads.
	var allEvents []Event
	if len(runs) > 0 {
		allEvents = loadRunEventsFromDir(ctx, runs[0].RunID)
	}

	// Index latest event per task slug.
	latestByTask := map[string]*Event{}
	for i := range allEvents {
		ev := &allEvents[i]
		prev, ok := latestByTask[ev.TaskName]
		if !ok || ev.Timestamp > prev.Timestamp {
			latestByTask[ev.TaskName] = ev
		}
	}

	var agents []AgentStatus
	for _, t := range plan.Active {
		agent := AgentStatus{
			TaskName:  t.Task,
			SpecTitle: t.SpecTitle,
			SpecFile:  t.File,
			Status:    t.Status,
		}

		if ev, ok := latestByTask[slugify(t.Task)]; ok {
			agent.LastEvent = ev.Message
			agent.LastEventAt = ev.Timestamp
			agent.Ago = relativeTime(ev.Timestamp)
		}

		agents = append(agents, agent)
	}

	return agents
}

// itoa is a minimal int-to-string for small numbers, avoiding strconv import.
func itoa(n int) string {
	if n < 0 {
		return "-" + itoa(-n)
	}
	if n < 10 {
		return string(rune('0' + n))
	}
	return itoa(n/10) + string(rune('0'+n%10))
}
