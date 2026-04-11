package forge

import (
	"fmt"
	"math"
	"strings"
)

// BoardSpec is a spec card on the board.
type BoardSpec struct {
	Title       string      `json:"title"`
	File        string      `json:"file"`
	Branch      string      `json:"branch,omitempty"`
	Priority    string      `json:"priority,omitempty"`
	TotalTasks  int         `json:"totalTasks"`
	DoneTasks   int         `json:"doneTasks"`
	ActiveTasks int         `json:"activeTasks"`
	ReadyTasks  int         `json:"readyTasks"`
	BlockedTasks int        `json:"blockedTasks"`
	ProgressPct float64     `json:"progressPct"`
	Lane        string      `json:"lane"`
	Tasks       []BoardTask `json:"tasks"`
}

// BoardTask is a task inside a spec card.
type BoardTask struct {
	Task    string   `json:"task"`
	Repo    string   `json:"repo,omitempty"`
	Status  string   `json:"status"`
	Bucket  string   `json:"bucket"`
	Reasons []string `json:"reasons,omitempty"`
	PR      string   `json:"pr,omitempty"`
}

// BoardLane represents a column on the board.
type BoardLane struct {
	Name  string      `json:"name"`
	Specs []BoardSpec `json:"specs"`
	Count int         `json:"count"`
}

// BoardSummary holds aggregate counts.
type BoardSummary struct {
	TotalSpecs  int     `json:"totalSpecs"`
	TotalTasks  int     `json:"totalTasks"`
	Done        int     `json:"done"`
	InProgress  int     `json:"inProgress"`
	InReview    int     `json:"inReview"`
	Ready       int     `json:"ready"`
	Blocked     int     `json:"blocked"`
	ProgressPct float64 `json:"progressPct"`
}

// BoardRunInfo summarizes the most recent run.
type BoardRunInfo struct {
	RunID     string     `json:"runId"`
	Status    RunStatus  `json:"status"`
	StartedAt string     `json:"startedAt"`
	Summary   RunSummary `json:"summary"`
}

// Board is the full board view returned by Service.Board().
type Board struct {
	ProjectID   string         `json:"projectId"`
	ProjectName string         `json:"projectName"`
	Lanes       []BoardLane    `json:"lanes"`
	Summary     BoardSummary   `json:"summary"`
	LastRun     *BoardRunInfo  `json:"lastRun,omitempty"`
	Feed        []ActivityItem `json:"feed,omitempty"`
	Inbox       []InboxItem    `json:"inbox,omitempty"`
	Focus       *FocusItem     `json:"focus,omitempty"`
	Agents      []AgentStatus  `json:"agents,omitempty"`
}

// Board assembles a project board from the run plan and recent run records.
func (s *Service) Board(cwd string) (Board, error) {
	plan, err := s.PlanRun(cwd, "")
	if err != nil {
		return Board{}, err
	}

	ctx, _ := s.ResolveContext(cwd)
	runs, _ := s.ListRunRecords(cwd)

	board := buildBoard(plan, runs)
	board.Feed = loadActivityFeed(ctx, runs)
	board.Inbox = loadInbox(ctx, plan, runs)
	board.Focus = deriveFocus(board.Inbox)
	board.Agents = loadAgentStatuses(ctx, plan, runs)
	return board, nil
}

func buildBoard(plan RunPlan, runs []RunRecord) Board {
	// Group all plan tasks by spec file to build per-spec aggregates.
	type specAgg struct {
		title    string
		file     string
		branch   string
		priority string
		tasks    []BoardTask
	}

	specMap := map[string]*specAgg{}
	specOrder := []string{}

	addTask := func(pt RunPlanTask, bucket string) {
		key := pt.File
		agg, ok := specMap[key]
		if !ok {
			agg = &specAgg{title: pt.SpecTitle, file: pt.File, branch: pt.SpecBranch}
			specMap[key] = agg
			specOrder = append(specOrder, key)
		}
		status := pt.Status
		if status == "" {
			status = bucket
		}
		agg.tasks = append(agg.tasks, BoardTask{
			Task:    pt.Task,
			Repo:    pt.Repo,
			Status:  status,
			Bucket:  bucket,
			Reasons: pt.Reasons,
		})
	}

	for _, t := range plan.Safe {
		addTask(t, "ready")
	}
	for _, t := range plan.Serial {
		addTask(t, "ready")
	}
	for _, t := range plan.Active {
		addTask(t, "active")
	}
	for _, t := range plan.Conflict {
		addTask(t, "blocked")
	}
	for _, t := range plan.Blocked {
		addTask(t, "blocked")
	}
	for _, t := range plan.Invalid {
		addTask(t, "blocked")
	}
	for _, t := range plan.Done {
		addTask(t, "done")
	}

	// Build BoardSpec for each spec and determine its lane.
	var allSpecs []BoardSpec
	for _, key := range specOrder {
		agg := specMap[key]
		bs := BoardSpec{
			Title:  agg.title,
			File:   agg.file,
			Branch: agg.branch,
			Tasks:  agg.tasks,
		}

		for _, t := range agg.tasks {
			bs.TotalTasks++
			switch t.Bucket {
			case "done":
				bs.DoneTasks++
			case "active":
				if t.Status == "in_review" {
					bs.ActiveTasks++ // counted as active for the spec
				} else {
					bs.ActiveTasks++
				}
			case "ready":
				bs.ReadyTasks++
			case "blocked":
				bs.BlockedTasks++
			}
		}

		if bs.TotalTasks > 0 {
			bs.ProgressPct = math.Round(float64(bs.DoneTasks) / float64(bs.TotalTasks) * 100)
		}

		// Determine spec lane based on its task composition.
		bs.Lane = specLane(bs)
		allSpecs = append(allSpecs, bs)
	}

	// Group specs into lanes.
	laneMap := map[string][]BoardSpec{}
	for _, bs := range allSpecs {
		laneMap[bs.Lane] = append(laneMap[bs.Lane], bs)
	}

	laneOrder := []string{"Ready", "In Progress", "In Review", "Blocked", "Done"}
	var lanes []BoardLane
	for _, name := range laneOrder {
		specs := laneMap[name]
		lanes = append(lanes, BoardLane{Name: name, Specs: specs, Count: len(specs)})
	}

	// Summary
	totalTasks := 0
	doneTasks := 0
	activeTasks := 0
	readyTasks := 0
	blockedTasks := 0
	for _, bs := range allSpecs {
		totalTasks += bs.TotalTasks
		doneTasks += bs.DoneTasks
		activeTasks += bs.ActiveTasks
		readyTasks += bs.ReadyTasks
		blockedTasks += bs.BlockedTasks
	}

	pct := 0.0
	if totalTasks > 0 {
		pct = math.Round(float64(doneTasks) / float64(totalTasks) * 100)
	}

	summary := BoardSummary{
		TotalSpecs:  len(allSpecs),
		TotalTasks:  totalTasks,
		Done:        doneTasks,
		InProgress:  activeTasks,
		Ready:       readyTasks,
		Blocked:     blockedTasks,
		ProgressPct: pct,
	}

	board := Board{
		ProjectID:   plan.ProjectID,
		ProjectName: plan.ProjectName,
		Lanes:       lanes,
		Summary:     summary,
	}

	if len(runs) > 0 {
		r := runs[0]
		board.LastRun = &BoardRunInfo{
			RunID:     r.RunID,
			Status:    r.Status,
			StartedAt: r.StartedAt,
			Summary:   r.Summary,
		}
	}

	return board
}

// specLane determines which lane a spec belongs to based on its tasks.
func specLane(bs BoardSpec) string {
	if bs.DoneTasks == bs.TotalTasks {
		return "Done"
	}
	if bs.ActiveTasks > 0 {
		// Check if all active are in_review
		allReview := true
		for _, t := range bs.Tasks {
			if t.Bucket == "active" && t.Status != "in_review" {
				allReview = false
				break
			}
		}
		if allReview {
			return "In Review"
		}
		return "In Progress"
	}
	if bs.ReadyTasks > 0 {
		return "Ready"
	}
	if bs.BlockedTasks > 0 {
		return "Blocked"
	}
	return "Ready"
}

// FormatBoard renders a terminal-friendly kanban board.
func FormatBoard(board Board) string {
	var b strings.Builder

	if board.ProjectName != "" {
		fmt.Fprintf(&b, "Forge Board — %s\n", board.ProjectName)
	} else {
		b.WriteString("Forge Board\n")
	}

	s := board.Summary
	barWidth := 30
	filled := 0
	if s.TotalTasks > 0 {
		filled = int(float64(barWidth) * float64(s.Done) / float64(s.TotalTasks))
	}
	b.WriteString("[")
	for i := 0; i < barWidth; i++ {
		if i < filled {
			b.WriteByte('=')
		} else {
			b.WriteByte(' ')
		}
	}
	fmt.Fprintf(&b, "] %.0f%% done\n", s.ProgressPct)
	fmt.Fprintf(&b, "%d specs | %d tasks | %d done | %d active | %d ready | %d blocked\n\n",
		s.TotalSpecs, s.TotalTasks, s.Done, s.InProgress, s.Ready, s.Blocked)

	for _, lane := range board.Lanes {
		fmt.Fprintf(&b, "%s (%d)\n", lane.Name, lane.Count)
		if len(lane.Specs) == 0 {
			b.WriteString("  (none)\n")
		}
		for _, spec := range lane.Specs {
			// Progress bar per spec
			specBar := 10
			specFilled := 0
			if spec.TotalTasks > 0 {
				specFilled = int(float64(specBar) * float64(spec.DoneTasks) / float64(spec.TotalTasks))
			}
			b.WriteString("  ")
			for i := 0; i < specBar; i++ {
				if i < specFilled {
					b.WriteString("=")
				} else {
					b.WriteString("-")
				}
			}
			fmt.Fprintf(&b, "  %s  %d/%d tasks\n", spec.Title, spec.DoneTasks, spec.TotalTasks)
		}
		b.WriteString("\n")
	}

	if board.LastRun != nil {
		r := board.LastRun
		fmt.Fprintf(&b, "Last run: %s  %s  total:%d ok:%d skip:%d fail:%d\n",
			r.RunID, r.Status, r.Summary.Total, r.Summary.Succeeded, r.Summary.Skipped, r.Summary.Failed)
	}

	return b.String()
}
