package forge

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

var metadataCommentPattern = regexp.MustCompile(`^\s*<!--\s*([a-zA-Z0-9_-]+)\s*:\s*(.*?)\s*-->\s*$`)

type ForgeSpecFile struct {
	Path             string              `json:"path"`
	RelativePath     string              `json:"relativePath,omitempty"`
	Title            string              `json:"title,omitempty"`
	Status           string              `json:"status,omitempty"`
	Priority         string              `json:"priority,omitempty"`
	Created          string              `json:"created,omitempty"`
	Branch           string              `json:"branch,omitempty"`
	Demo             string              `json:"demo,omitempty"`
	ExpectedBehavior []string            `json:"expectedBehavior,omitempty"`
	TestCases        []ForgeSpecScenario `json:"testCases,omitempty"`
	ValidationPlan   []string            `json:"validationPlan,omitempty"`
	Tasks            []ForgeSpecTask     `json:"tasks,omitempty"`

	rawMetadata map[string]string
}

type ForgeSpecTask struct {
	Name            string   `json:"name"`
	Line            int      `json:"line"`
	Status          string   `json:"status,omitempty"`
	Parallelizable  string   `json:"parallelizable,omitempty"`
	Deps            []string `json:"deps,omitempty"`
	Repo            string   `json:"repo,omitempty"`
	Touches         []string `json:"touches,omitempty"`
	SharedContracts []string `json:"sharedContracts,omitempty"`
	ConflictRisk    string   `json:"conflictRisk,omitempty"`
	ConflictsWith   []string `json:"conflictsWith,omitempty"`
	PR              string   `json:"pr,omitempty"`
	DoneWhen        []string `json:"doneWhen,omitempty"`
	Validation      []string `json:"validation,omitempty"`
	Covers          []string `json:"covers,omitempty"`

	rawMetadata map[string]string
}

type SpecLintIssue struct {
	Severity     string   `json:"severity"`
	Code         string   `json:"code"`
	File         string   `json:"file"`
	Task         string   `json:"task,omitempty"`
	Line         int      `json:"line,omitempty"`
	Message      string   `json:"message"`
	RelatedTasks []string `json:"relatedTasks,omitempty"`
}

type SpecLintReport struct {
	Workspace    string          `json:"workspace"`
	ProjectID    string          `json:"projectId"`
	ProjectName  string          `json:"projectName"`
	SpecsDir     string          `json:"specsDir"`
	Specs        []ForgeSpecFile `json:"specs"`
	Issues       []SpecLintIssue `json:"issues"`
	ErrorCount   int             `json:"errorCount"`
	WarningCount int             `json:"warningCount"`
	Valid        bool            `json:"valid"`
}

type RunPlanTask struct {
	File            string   `json:"file"`
	SpecTitle       string   `json:"specTitle,omitempty"`
	SpecBranch      string   `json:"specBranch,omitempty"`
	SpecPath        string   `json:"specPath,omitempty"`
	Task            string   `json:"task"`
	Line            int      `json:"line"`
	Repo            string   `json:"repo,omitempty"`
	Status          string   `json:"status,omitempty"`
	Parallelizable  string   `json:"parallelizable,omitempty"`
	Deps            []string `json:"deps,omitempty"`
	Touches         []string `json:"touches,omitempty"`
	SharedContracts []string `json:"sharedContracts,omitempty"`
	ConflictRisk    string   `json:"conflictRisk,omitempty"`
	Reasons         []string `json:"reasons,omitempty"`
	RelatedTasks    []string `json:"relatedTasks,omitempty"`
}

type RunPlan struct {
	Workspace    string          `json:"workspace"`
	ProjectID    string          `json:"projectId"`
	ProjectName  string          `json:"projectName"`
	SpecsDir     string          `json:"specsDir"`
	LintValid    bool            `json:"lintValid"`
	Issues       []SpecLintIssue `json:"issues,omitempty"`
	ErrorCount   int             `json:"errorCount"`
	WarningCount int             `json:"warningCount"`
	Safe         []RunPlanTask   `json:"safe,omitempty"`
	Serial       []RunPlanTask   `json:"serial,omitempty"`
	Conflict     []RunPlanTask   `json:"conflict,omitempty"`
	Blocked      []RunPlanTask   `json:"blocked,omitempty"`
	Invalid      []RunPlanTask   `json:"invalid,omitempty"`
	Active       []RunPlanTask   `json:"active,omitempty"`
	Done         []RunPlanTask   `json:"done,omitempty"`
}

func (s *Service) LintSpecs(cwd, specPath string) (SpecLintReport, error) {
	ctx, specs, err := s.inspectSpecs(cwd, specPath)
	if err != nil {
		return SpecLintReport{}, err
	}
	issues := lintSpecFiles(ctx, specs)
	return newSpecLintReport(ctx, specs, issues), nil
}

func (s *Service) PlanRun(cwd, specPath string) (RunPlan, error) {
	ctx, specs, err := s.inspectSpecs(cwd, specPath)
	if err != nil {
		return RunPlan{}, err
	}
	issues := lintSpecFiles(ctx, specs)
	report := newSpecLintReport(ctx, specs, issues)
	plan := RunPlan{
		Workspace:    ctx.Workspace,
		ProjectID:    ctx.ProjectID,
		ProjectName:  ctx.ProjectName,
		SpecsDir:     ctx.SpecsDir,
		LintValid:    report.Valid,
		Issues:       report.Issues,
		ErrorCount:   report.ErrorCount,
		WarningCount: report.WarningCount,
	}

	taskIssueMap, fileErrorMap := buildIssueMaps(report.Issues)
	plan.Safe, plan.Serial, plan.Conflict, plan.Blocked, plan.Invalid, plan.Active, plan.Done = buildRunPlanBuckets(specs, taskIssueMap, fileErrorMap)
	return plan, nil
}

func (s *Service) inspectSpecs(cwd, specPath string) (ResolvedContext, []ForgeSpecFile, error) {
	ctx, err := s.ResolveContext(cwd)
	if err != nil {
		return ResolvedContext{}, nil, err
	}
	paths, err := resolveSpecTargets(ctx, cwd, specPath)
	if err != nil {
		return ResolvedContext{}, nil, err
	}
	specs := make([]ForgeSpecFile, 0, len(paths))
	for _, path := range paths {
		spec, err := parseForgeSpecFile(path, ctx.SpecsDir)
		if err != nil {
			return ResolvedContext{}, nil, err
		}
		specs = append(specs, spec)
	}
	return ctx, specs, nil
}

func resolveSpecTargets(ctx ResolvedContext, cwd, specPath string) ([]string, error) {
	if specPath == "" {
		return collectSpecFiles(ctx.SpecsDir)
	}
	resolved := specPath
	if !filepath.IsAbs(resolved) {
		candidate := cleanAbs(filepath.Join(fallback(cwd, "."), resolved))
		if _, err := os.Stat(candidate); err == nil {
			resolved = candidate
		} else {
			resolved = cleanAbs(filepath.Join(ctx.SpecsDir, specPath))
		}
	} else {
		resolved = cleanAbs(resolved)
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return nil, err
	}
	if info.IsDir() {
		return collectSpecFiles(resolved)
	}
	return []string{resolved}, nil
}

func collectSpecFiles(root string) ([]string, error) {
	files := []string{}
	if _, err := os.Stat(root); err != nil {
		if os.IsNotExist(err) {
			return files, nil
		}
		return nil, err
	}
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		if strings.EqualFold(filepath.Ext(entry.Name()), ".md") {
			files = append(files, cleanAbs(path))
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(files)
	return files, nil
}

func parseForgeSpecFile(path, specsDir string) (ForgeSpecFile, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return ForgeSpecFile{}, err
	}
	content := strings.ReplaceAll(string(data), "\r\n", "\n")
	lines := strings.Split(content, "\n")
	idx := 0
	if len(lines) > 0 && strings.TrimSpace(lines[0]) == "---" {
		idx++
		for idx < len(lines) && strings.TrimSpace(lines[idx]) != "---" {
			idx++
		}
		if idx < len(lines) {
			idx++
		}
	}

	spec := ForgeSpecFile{
		Path:             cleanAbs(path),
		RelativePath:     relativeToSpecsDir(specsDir, path),
		rawMetadata:      map[string]string{},
		ExpectedBehavior: []string{},
		TestCases:        []ForgeSpecScenario{},
		ValidationPlan:   []string{},
		Tasks:            []ForgeSpecTask{},
	}
	var current *ForgeSpecTask
	var currentScenario *ForgeSpecScenario
	currentSection := specSectionNone
	inDoneWhen := false
	inValidation := false
	for lineIdx := idx; lineIdx < len(lines); lineIdx++ {
		line := lines[lineIdx]
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		if strings.HasPrefix(trimmed, "# ") && spec.Title == "" {
			spec.Title = strings.TrimSpace(strings.TrimPrefix(trimmed, "# "))
			continue
		}
		if current == nil && strings.HasPrefix(trimmed, "## ") {
			currentSection = normalizeSpecSectionHeading(trimmed)
			currentScenario = nil
			continue
		}
		if current == nil && currentSection == specSectionTestCases && strings.HasPrefix(trimmed, "### Scenario:") {
			name := strings.TrimSpace(strings.TrimPrefix(trimmed, "### Scenario:"))
			spec.TestCases = append(spec.TestCases, ForgeSpecScenario{Name: name, Line: lineIdx + 1})
			currentScenario = &spec.TestCases[len(spec.TestCases)-1]
			continue
		}
		if strings.HasPrefix(trimmed, "### Task:") {
			name := strings.TrimSpace(strings.TrimPrefix(trimmed, "### Task:"))
			spec.Tasks = append(spec.Tasks, ForgeSpecTask{Name: name, Line: lineIdx + 1, rawMetadata: map[string]string{}})
			current = &spec.Tasks[len(spec.Tasks)-1]
			currentScenario = nil
			currentSection = specSectionNone
			inDoneWhen = false
			inValidation = false
			continue
		}
		if key, value, ok := parseMetadataComment(trimmed); ok {
			if current == nil {
				spec.rawMetadata[key] = value
				applySpecMetadata(&spec, key, value)
			} else {
				current.rawMetadata[key] = value
				applyTaskMetadata(current, key, value)
			}
			continue
		}
		if current == nil {
			if strings.HasPrefix(trimmed, "**Demo:**") {
				spec.Demo = strings.TrimSpace(strings.TrimPrefix(trimmed, "**Demo:**"))
				continue
			}
			switch currentSection {
			case specSectionExpectedBehavior:
				if strings.HasPrefix(trimmed, "- ") {
					spec.ExpectedBehavior = append(spec.ExpectedBehavior, strings.TrimSpace(strings.TrimPrefix(trimmed, "- ")))
				}
			case specSectionValidationPlan:
				if strings.HasPrefix(trimmed, "- ") {
					spec.ValidationPlan = append(spec.ValidationPlan, strings.TrimSpace(strings.TrimPrefix(trimmed, "- ")))
				}
			case specSectionTestCases:
				if currentScenario != nil {
					if step, ok := parseScenarioStep(trimmed); ok {
						currentScenario.Steps = append(currentScenario.Steps, step)
					}
				}
			}
			continue
		}
		if trimmed == "**Done when:**" {
			inDoneWhen = true
			inValidation = false
			continue
		}
		if trimmed == "**Validation:**" {
			inValidation = true
			inDoneWhen = false
			continue
		}
		if inDoneWhen && strings.HasPrefix(trimmed, "- ") {
			current.DoneWhen = append(current.DoneWhen, strings.TrimSpace(strings.TrimPrefix(trimmed, "- ")))
			continue
		}
		if inValidation && strings.HasPrefix(trimmed, "- ") {
			value := strings.TrimSpace(strings.TrimPrefix(trimmed, "- "))
			current.Validation = append(current.Validation, value)
			if covered := extractCoveredScenario(value); covered != "" {
				current.Covers = appendUnique(current.Covers, covered)
			}
			continue
		}
	}
	return spec, nil
}

func parseMetadataComment(line string) (string, string, bool) {
	matches := metadataCommentPattern.FindStringSubmatch(line)
	if len(matches) != 3 {
		return "", "", false
	}
	return strings.ToLower(strings.TrimSpace(matches[1])), strings.TrimSpace(matches[2]), true
}

func applySpecMetadata(spec *ForgeSpecFile, key, value string) {
	switch key {
	case "status":
		spec.Status = value
	case "priority":
		spec.Priority = value
	case "created":
		spec.Created = value
	case "branch":
		spec.Branch = value
	}
}

func applyTaskMetadata(task *ForgeSpecTask, key, value string) {
	switch key {
	case "status":
		task.Status = value
	case "parallelizable":
		task.Parallelizable = strings.ToLower(value)
	case "deps":
		task.Deps = splitList(value)
	case "repo":
		task.Repo = strings.TrimSpace(value)
	case "touches":
		task.Touches = splitList(value)
	case "shared-contracts":
		task.SharedContracts = splitList(value)
	case "conflict-risk":
		task.ConflictRisk = strings.ToLower(strings.TrimSpace(value))
	case "conflicts-with":
		task.ConflictsWith = splitList(value)
	case "pr":
		task.PR = strings.TrimSpace(value)
	}
}

func splitList(value string) []string {
	value = strings.TrimSpace(value)
	if value == "" || strings.EqualFold(value, "none") {
		return nil
	}
	parts := strings.Split(value, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		cleaned := strings.TrimSpace(part)
		if cleaned == "" || strings.EqualFold(cleaned, "none") {
			continue
		}
		out = append(out, cleaned)
	}
	return out
}

func relativeToSpecsDir(specsDir, path string) string {
	rel, err := filepath.Rel(specsDir, path)
	if err != nil {
		return filepath.Base(path)
	}
	return filepath.ToSlash(rel)
}

func lintSpecFiles(ctx ResolvedContext, specs []ForgeSpecFile) []SpecLintIssue {
	issues := []SpecLintIssue{}
	knownRepos := map[string]bool{}
	for _, repo := range ctx.Repos {
		knownRepos[repo.RepoID] = true
	}
	multiRepo := len(ctx.Repos) > 1
	validStatuses := map[string]bool{"todo": true, "in_progress": true, "in_review": true, "changes_requested": true, "done": true}
	validRisks := map[string]bool{"low": true, "medium": true, "high": true}

	for _, spec := range specs {
		if spec.Title == "" {
			issues = append(issues, lintError(spec, nil, "missing_title", "Spec must start with a '# ' capability title."))
		}
		if spec.Demo == "" {
			issues = append(issues, lintError(spec, nil, "missing_demo", "Spec must include a '**Demo:**' line with a user-facing outcome."))
		}
		if spec.Branch == "" {
			issues = append(issues, lintWarning(spec, nil, "missing_branch", "Spec should declare a branch with '<!-- branch: ... -->' so tasks can create predictable worktrees."))
		}
		if len(spec.ExpectedBehavior) == 0 {
			issues = append(issues, lintWarning(spec, nil, "missing_expected_behavior", "Canonical Forge specs should include a '## Expected behavior' section with bullet points."))
		}
		if len(spec.TestCases) == 0 {
			issues = append(issues, lintWarning(spec, nil, "missing_test_cases", "Canonical Forge specs should include a '## Test cases' section with scenario flows."))
		}
		if len(spec.ValidationPlan) == 0 {
			issues = append(issues, lintWarning(spec, nil, "missing_validation_plan", "Canonical Forge specs should include a '## Validation plan' section that explains how the behavior will be proven."))
		}
		if len(spec.Tasks) == 0 {
			issues = append(issues, lintError(spec, nil, "missing_tasks", "Spec must contain at least one '### Task:' section."))
			continue
		}
		if len(spec.Tasks) > 5 {
			issues = append(issues, lintWarning(spec, nil, "too_many_tasks", "Specs with more than 5 tasks usually need to be split to reduce coordination overhead."))
		}

		scenarioNames := map[string]*ForgeSpecScenario{}
		scenarioCoverage := map[string][]string{}
		for idx := range spec.TestCases {
			scenario := &spec.TestCases[idx]
			key := strings.ToLower(strings.TrimSpace(scenario.Name))
			if key == "" {
				issues = append(issues, SpecLintIssue{Severity: "error", Code: "missing_scenario_name", File: spec.RelativePath, Line: scenario.Line, Message: "Scenario heading must be '### Scenario: <name>'."})
				continue
			}
			scenarioNames[key] = scenario
			if !scenarioHasKeyword(*scenario, "given") {
				issues = append(issues, SpecLintIssue{Severity: "error", Code: "scenario_missing_given", File: spec.RelativePath, Line: scenario.Line, Message: fmt.Sprintf("Scenario '%s' must include at least one Given step.", scenario.Name)})
			}
			if !scenarioHasKeyword(*scenario, "when") {
				issues = append(issues, SpecLintIssue{Severity: "error", Code: "scenario_missing_when", File: spec.RelativePath, Line: scenario.Line, Message: fmt.Sprintf("Scenario '%s' must include at least one When step.", scenario.Name)})
			}
			if !scenarioHasKeyword(*scenario, "then") {
				issues = append(issues, SpecLintIssue{Severity: "error", Code: "scenario_missing_then", File: spec.RelativePath, Line: scenario.Line, Message: fmt.Sprintf("Scenario '%s' must include at least one Then step.", scenario.Name)})
			}
		}

		nameToTask := map[string]*ForgeSpecTask{}
		parallelByRepo := map[string][]*ForgeSpecTask{}
		for idx := range spec.Tasks {
			task := &spec.Tasks[idx]
			key := strings.ToLower(strings.TrimSpace(task.Name))
			if key == "" {
				issues = append(issues, lintError(spec, task, "missing_task_name", "Task heading must be '### Task: <name>'."))
			} else if previous, ok := nameToTask[key]; ok {
				issues = append(issues, SpecLintIssue{
					Severity:     "error",
					Code:         "duplicate_task_name",
					File:         spec.RelativePath,
					Task:         task.Name,
					Line:         task.Line,
					Message:      fmt.Sprintf("Task name '%s' is duplicated in the same spec.", task.Name),
					RelatedTasks: []string{previous.Name},
				})
			} else {
				nameToTask[key] = task
			}

			if len(spec.TestCases) > 0 && len(task.Validation) == 0 {
				issues = append(issues, lintWarning(spec, task, "missing_task_validation", "Tasks in behavior-driven specs should include a '**Validation:**' block that references covered scenarios."))
			}
			if len(spec.TestCases) > 0 && len(task.Covers) == 0 {
				issues = append(issues, lintWarning(spec, task, "missing_task_coverage", "Tasks should declare which scenarios they cover with '- Covers: Scenario ...' lines."))
			}
			for _, covered := range task.Covers {
				scenario := scenarioNames[strings.ToLower(strings.TrimSpace(covered))]
				if scenario == nil {
					issues = append(issues, lintWarning(spec, task, "unknown_covered_scenario", fmt.Sprintf("Task validation references unknown scenario '%s'.", covered)))
					continue
				}
				scenarioCoverage[strings.ToLower(strings.TrimSpace(scenario.Name))] = appendUnique(scenarioCoverage[strings.ToLower(strings.TrimSpace(scenario.Name))], task.Name)
			}

			if task.Status == "" {
				issues = append(issues, lintError(spec, task, "missing_task_status", "Task must include '<!-- status: ... -->'."))
			} else if !validStatuses[task.Status] {
				issues = append(issues, lintError(spec, task, "invalid_task_status", fmt.Sprintf("Unsupported task status '%s'.", task.Status)))
			}
			if _, ok := task.rawMetadata["parallelizable"]; !ok {
				issues = append(issues, lintError(spec, task, "missing_parallelizable", "Task must include '<!-- parallelizable: yes|no -->'."))
			} else if task.Parallelizable != "yes" && task.Parallelizable != "no" {
				issues = append(issues, lintError(spec, task, "invalid_parallelizable", fmt.Sprintf("Unsupported parallelizable value '%s'.", task.Parallelizable)))
			}
			if _, ok := task.rawMetadata["deps"]; !ok {
				issues = append(issues, lintError(spec, task, "missing_deps", "Task must include '<!-- deps: ... -->' even when the value is 'none'."))
			}
			if multiRepo && task.Repo == "" {
				issues = append(issues, lintError(spec, task, "missing_repo", "Task must include '<!-- repo: ... -->' in multi-repo projects."))
			}
			if task.Repo != "" && len(knownRepos) > 0 && !knownRepos[task.Repo] {
				issues = append(issues, lintError(spec, task, "unknown_repo", fmt.Sprintf("Task repo '%s' is not linked in this Forge project.", task.Repo)))
			}
			if task.ConflictRisk == "" {
				issues = append(issues, lintWarning(spec, task, "missing_conflict_risk", "Task should include '<!-- conflict-risk: low|medium|high -->' for scheduling decisions."))
			} else if !validRisks[task.ConflictRisk] {
				issues = append(issues, lintError(spec, task, "invalid_conflict_risk", fmt.Sprintf("Unsupported conflict-risk value '%s'.", task.ConflictRisk)))
			}
			if len(task.DoneWhen) == 0 {
				issues = append(issues, lintError(spec, task, "missing_done_when", "Task must include a '**Done when:**' section with at least one bullet."))
			}
			if task.Parallelizable == "yes" && task.ConflictRisk == "high" {
				issues = append(issues, lintError(spec, task, "parallel_high_conflict", "Tasks marked as parallelizable cannot also have conflict-risk: high."))
			}
			if task.Parallelizable == "yes" && len(task.Touches) == 0 && len(task.SharedContracts) == 0 {
				issues = append(issues, lintWarning(spec, task, "missing_conflict_scope", "Parallelizable tasks should declare 'touches' and/or 'shared-contracts' so Forge can detect merge hotspots."))
			}
			if task.Parallelizable == "yes" && len(task.ConflictsWith) > 0 {
				issues = append(issues, lintWarning(spec, task, "explicit_conflict_on_parallel_task", "Task is marked parallelizable but also declares conflicts-with; Forge will serialize it when needed."))
			}
			if task.Parallelizable == "yes" && task.Repo != "" {
				parallelByRepo[task.Repo] = append(parallelByRepo[task.Repo], task)
			}
		}

		for _, task := range spec.Tasks {
			for _, dep := range task.Deps {
				if nameToTask[strings.ToLower(dep)] == nil {
					issues = append(issues, lintError(spec, &task, "unknown_dep", fmt.Sprintf("Dependency '%s' does not match any task in this spec.", dep)))
				}
			}
			for _, conflict := range task.ConflictsWith {
				if nameToTask[strings.ToLower(conflict)] == nil {
					issues = append(issues, lintError(spec, &task, "unknown_conflict_task", fmt.Sprintf("conflicts-with references unknown task '%s'.", conflict)))
				}
			}
		}
		for key, scenario := range scenarioNames {
			if len(scenarioCoverage[key]) == 0 {
				issues = append(issues, SpecLintIssue{Severity: "warning", Code: "uncovered_scenario", File: spec.RelativePath, Line: scenario.Line, Message: fmt.Sprintf("Scenario '%s' is not referenced by any task Validation block.", scenario.Name)})
			}
		}

		for repoID, tasks := range parallelByRepo {
			if len(tasks) <= 1 {
				continue
			}
			for _, task := range tasks {
				if len(task.Touches) == 0 {
					issues = append(issues, lintError(spec, task, "parallel_same_repo_missing_touches", fmt.Sprintf("Parallel tasks in repo '%s' must all declare 'touches' so conflict detection can prove they are independent.", repoID)))
				}
			}
			for i := 0; i < len(tasks); i++ {
				for j := i + 1; j < len(tasks); j++ {
					a := tasks[i]
					b := tasks[j]
					if overlapsAny(a.Touches, b.Touches) {
						issues = append(issues, SpecLintIssue{
							Severity:     "error",
							Code:         "overlapping_touches",
							File:         spec.RelativePath,
							Task:         a.Name,
							Line:         a.Line,
							Message:      fmt.Sprintf("Parallel tasks '%s' and '%s' overlap in the same repo on declared touches.", a.Name, b.Name),
							RelatedTasks: []string{b.Name},
						})
					}
					if overlapsAny(a.SharedContracts, b.SharedContracts) {
						issues = append(issues, SpecLintIssue{
							Severity:     "error",
							Code:         "overlapping_shared_contracts",
							File:         spec.RelativePath,
							Task:         a.Name,
							Line:         a.Line,
							Message:      fmt.Sprintf("Parallel tasks '%s' and '%s' overlap on shared-contracts and should be serialized or merged.", a.Name, b.Name),
							RelatedTasks: []string{b.Name},
						})
					}
				}
			}
		}

		parallelTasks := make([]*ForgeSpecTask, 0, len(spec.Tasks))
		for idx := range spec.Tasks {
			if spec.Tasks[idx].Parallelizable == "yes" {
				parallelTasks = append(parallelTasks, &spec.Tasks[idx])
			}
		}
		for i := 0; i < len(parallelTasks); i++ {
			for j := i + 1; j < len(parallelTasks); j++ {
				a := parallelTasks[i]
				b := parallelTasks[j]
				if overlapsAny(a.SharedContracts, b.SharedContracts) && a.Repo != b.Repo {
					issues = append(issues, SpecLintIssue{
						Severity:     "error",
						Code:         "cross_repo_shared_contract_overlap",
						File:         spec.RelativePath,
						Task:         a.Name,
						Line:         a.Line,
						Message:      fmt.Sprintf("Parallel tasks '%s' and '%s' overlap on shared-contracts across repos; add deps or serialize them.", a.Name, b.Name),
						RelatedTasks: []string{b.Name},
					})
				}
			}
		}

		// Scaffolding check: when parallel tasks in the same repo share a
		// common directory prefix in their 'touches', they likely need a
		// serial scaffolding task to lay down the structure first.
		issues = append(issues, checkScaffoldingNeeded(spec, parallelByRepo, nameToTask)...)
	}

	sort.SliceStable(issues, func(i, j int) bool {
		if issues[i].File != issues[j].File {
			return issues[i].File < issues[j].File
		}
		if issues[i].Line != issues[j].Line {
			return issues[i].Line < issues[j].Line
		}
		return issues[i].Code < issues[j].Code
	})
	return issues
}

func newSpecLintReport(ctx ResolvedContext, specs []ForgeSpecFile, issues []SpecLintIssue) SpecLintReport {
	report := SpecLintReport{
		Workspace:   ctx.Workspace,
		ProjectID:   ctx.ProjectID,
		ProjectName: ctx.ProjectName,
		SpecsDir:    ctx.SpecsDir,
		Specs:       specs,
		Issues:      issues,
		Valid:       true,
	}
	for _, issue := range issues {
		switch issue.Severity {
		case "error":
			report.ErrorCount++
			report.Valid = false
		case "warning":
			report.WarningCount++
		}
	}
	return report
}

func lintError(spec ForgeSpecFile, task *ForgeSpecTask, code, message string) SpecLintIssue {
	issue := SpecLintIssue{Severity: "error", Code: code, File: spec.RelativePath, Message: message}
	if task != nil {
		issue.Task = task.Name
		issue.Line = task.Line
	}
	return issue
}

func lintWarning(spec ForgeSpecFile, task *ForgeSpecTask, code, message string) SpecLintIssue {
	issue := SpecLintIssue{Severity: "warning", Code: code, File: spec.RelativePath, Message: message}
	if task != nil {
		issue.Task = task.Name
		issue.Line = task.Line
	}
	return issue
}

func buildIssueMaps(issues []SpecLintIssue) (map[string][]SpecLintIssue, map[string]bool) {
	taskIssues := map[string][]SpecLintIssue{}
	fileErrorMap := map[string]bool{}
	for _, issue := range issues {
		if issue.Severity != "error" {
			continue
		}
		if issue.Task == "" {
			fileErrorMap[issue.File] = true
			continue
		}
		key := issue.File + "::" + strings.ToLower(issue.Task)
		taskIssues[key] = append(taskIssues[key], issue)
	}
	return taskIssues, fileErrorMap
}

func buildRunPlanBuckets(specs []ForgeSpecFile, taskIssueMap map[string][]SpecLintIssue, fileErrorMap map[string]bool) ([]RunPlanTask, []RunPlanTask, []RunPlanTask, []RunPlanTask, []RunPlanTask, []RunPlanTask, []RunPlanTask) {
	type taskRef struct {
		spec *ForgeSpecFile
		task *ForgeSpecTask
	}

	activeTasks := []taskRef{}
	readyTasks := []taskRef{}
	for specIdx := range specs {
		spec := &specs[specIdx]
		for taskIdx := range spec.Tasks {
			task := &spec.Tasks[taskIdx]
			switch task.Status {
			case "in_progress", "in_review":
				activeTasks = append(activeTasks, taskRef{spec: spec, task: task})
			case "todo", "changes_requested":
				readyTasks = append(readyTasks, taskRef{spec: spec, task: task})
			}
		}
	}

	conflictReasons := map[string][]string{}
	relatedTasks := map[string][]string{}
	registerConflict := func(a, b taskRef, reason string) {
		for _, pair := range []struct{ left, right taskRef }{{a, b}, {b, a}} {
			key := runPlanKey(pair.left.spec.RelativePath, pair.left.task.Name)
			conflictReasons[key] = appendUnique(conflictReasons[key], reason)
			relatedTasks[key] = appendUnique(relatedTasks[key], pair.right.task.Name)
		}
	}

	for i := 0; i < len(readyTasks); i++ {
		for j := i + 1; j < len(readyTasks); j++ {
			if reason, ok := runtimeConflictReason(readyTasks[i], readyTasks[j]); ok {
				registerConflict(readyTasks[i], readyTasks[j], reason)
			}
		}
	}
	for _, ready := range readyTasks {
		for _, active := range activeTasks {
			if reason, ok := runtimeConflictReason(ready, active); ok {
				registerConflict(ready, active, reason)
			}
			if ready.task.Parallelizable == "no" && ready.task.Repo != "" && ready.task.Repo == active.task.Repo {
				registerConflict(ready, active, fmt.Sprintf("serial task in repo '%s' cannot start while '%s' is active", ready.task.Repo, active.task.Name))
			}
		}
	}

	safe := []RunPlanTask{}
	serial := []RunPlanTask{}
	conflict := []RunPlanTask{}
	blocked := []RunPlanTask{}
	invalid := []RunPlanTask{}
	active := []RunPlanTask{}
	done := []RunPlanTask{}

	for specIdx := range specs {
		spec := &specs[specIdx]
		fileHasError := fileErrorMap[spec.RelativePath]
		taskMap := map[string]*ForgeSpecTask{}
		for taskIdx := range spec.Tasks {
			task := &spec.Tasks[taskIdx]
			taskMap[strings.ToLower(task.Name)] = task
		}
		for taskIdx := range spec.Tasks {
			task := &spec.Tasks[taskIdx]
			item := newRunPlanTask(*spec, *task)
			taskKey := runPlanKey(spec.RelativePath, task.Name)
			if fileHasError || len(taskIssueMap[taskKey]) > 0 {
				for _, issue := range taskIssueMap[taskKey] {
					item.Reasons = appendUnique(item.Reasons, issue.Message)
				}
				if fileHasError {
					item.Reasons = appendUnique(item.Reasons, "spec has file-level lint errors")
				}
			}

			switch task.Status {
			case "done":
				done = append(done, item)
				continue
			case "in_progress", "in_review":
				active = append(active, item)
				continue
			case "todo", "changes_requested":
				// continue below
			default:
				item.Reasons = appendUnique(item.Reasons, fmt.Sprintf("unsupported status '%s'", task.Status))
				invalid = append(invalid, item)
				continue
			}

			unmet := unmetDeps(*task, taskMap)
			if len(unmet) > 0 {
				item.Reasons = append(item.Reasons, fmt.Sprintf("waiting on: %s", strings.Join(unmet, ", ")))
				blocked = append(blocked, item)
				continue
			}
			if fileHasError || len(taskIssueMap[taskKey]) > 0 {
				invalid = append(invalid, item)
				continue
			}
			if reasons := conflictReasons[taskKey]; len(reasons) > 0 {
				item.Reasons = appendUnique(item.Reasons, reasons...)
				item.RelatedTasks = appendUnique(item.RelatedTasks, relatedTasks[taskKey]...)
				conflict = append(conflict, item)
				continue
			}
			if task.Parallelizable == "no" {
				item.Reasons = appendUnique(item.Reasons, "serial-only task")
				serial = append(serial, item)
				continue
			}
			safe = append(safe, item)
		}
	}

	sortRunPlanTasks(safe)
	sortRunPlanTasks(serial)
	sortRunPlanTasks(conflict)
	sortRunPlanTasks(blocked)
	sortRunPlanTasks(invalid)
	sortRunPlanTasks(active)
	sortRunPlanTasks(done)
	return safe, serial, conflict, blocked, invalid, active, done
}

func runtimeConflictReason(a, b struct {
	spec *ForgeSpecFile
	task *ForgeSpecTask
}) (string, bool) {
	if a.task.Name == b.task.Name && a.spec.RelativePath == b.spec.RelativePath {
		return "", false
	}
	if referencesTask(a.task.ConflictsWith, b.task.Name) || referencesTask(b.task.ConflictsWith, a.task.Name) {
		return fmt.Sprintf("explicit conflicts-with between '%s' and '%s'", a.task.Name, b.task.Name), true
	}
	if a.task.Parallelizable != "yes" || b.task.Parallelizable != "yes" {
		return "", false
	}
	if a.task.Repo != "" && a.task.Repo == b.task.Repo {
		if len(a.task.Touches) == 0 || len(b.task.Touches) == 0 {
			return fmt.Sprintf("same repo '%s' but Forge cannot prove isolation because 'touches' metadata is incomplete", a.task.Repo), true
		}
		if overlapsAny(a.task.Touches, b.task.Touches) {
			return fmt.Sprintf("overlapping touches in repo '%s'", a.task.Repo), true
		}
	}
	if overlapsAny(a.task.SharedContracts, b.task.SharedContracts) {
		return "overlapping shared-contracts", true
	}
	return "", false
}

func newRunPlanTask(spec ForgeSpecFile, task ForgeSpecTask) RunPlanTask {
	return RunPlanTask{
		File:            spec.RelativePath,
		SpecTitle:       spec.Title,
		SpecBranch:      spec.Branch,
		SpecPath:        spec.Path,
		Task:            task.Name,
		Line:            task.Line,
		Repo:            task.Repo,
		Status:          task.Status,
		Parallelizable:  task.Parallelizable,
		Deps:            append([]string(nil), task.Deps...),
		Touches:         append([]string(nil), task.Touches...),
		SharedContracts: append([]string(nil), task.SharedContracts...),
		ConflictRisk:    task.ConflictRisk,
	}
}

func unmetDeps(task ForgeSpecTask, taskMap map[string]*ForgeSpecTask) []string {
	unmet := []string{}
	for _, dep := range task.Deps {
		depTask := taskMap[strings.ToLower(dep)]
		if depTask == nil {
			unmet = append(unmet, dep)
			continue
		}
		if depTask.Status != "done" {
			unmet = append(unmet, depTask.Name)
		}
	}
	return unmet
}

func referencesTask(values []string, taskName string) bool {
	for _, value := range values {
		if strings.EqualFold(strings.TrimSpace(value), taskName) {
			return true
		}
	}
	return false
}

func runPlanKey(file, task string) string {
	return file + "::" + strings.ToLower(strings.TrimSpace(task))
}

func sortRunPlanTasks(tasks []RunPlanTask) {
	sort.Slice(tasks, func(i, j int) bool {
		if tasks[i].File != tasks[j].File {
			return tasks[i].File < tasks[j].File
		}
		if tasks[i].Repo != tasks[j].Repo {
			return tasks[i].Repo < tasks[j].Repo
		}
		return tasks[i].Task < tasks[j].Task
	})
}

func overlapsAny(left, right []string) bool {
	for _, a := range left {
		for _, b := range right {
			if pathScopesOverlap(a, b) {
				return true
			}
		}
	}
	return false
}

func pathScopesOverlap(a, b string) bool {
	a = normalizeScope(a)
	b = normalizeScope(b)
	if a == "" || b == "" {
		return false
	}
	if a == b {
		return true
	}
	if hasSlashPrefix(a, b) || hasSlashPrefix(b, a) {
		return true
	}
	return false
}

func normalizeScope(value string) string {
	value = strings.TrimSpace(value)
	value = strings.ReplaceAll(value, "\\", "/")
	value = strings.TrimPrefix(value, "./")
	value = strings.Trim(value, "`")
	value = globRoot(value)
	value = strings.TrimSuffix(value, "/")
	return value
}

func globRoot(value string) string {
	idx := strings.IndexAny(value, "*?[")
	if idx == -1 {
		return strings.TrimSpace(value)
	}
	return strings.TrimSpace(value[:idx])
}

func hasSlashPrefix(pathValue, prefix string) bool {
	if prefix == "" {
		return false
	}
	if pathValue == prefix {
		return true
	}
	return strings.HasPrefix(pathValue, prefix+"/")
}

// checkScaffoldingNeeded detects when parallel tasks in the same repo share
// a common directory in their 'touches' but no serial scaffolding task exists
// to lay down the structure first.
func checkScaffoldingNeeded(spec ForgeSpecFile, parallelByRepo map[string][]*ForgeSpecTask, nameToTask map[string]*ForgeSpecTask) []SpecLintIssue {
	var issues []SpecLintIssue

	for repoID, tasks := range parallelByRepo {
		if len(tasks) < 2 {
			continue
		}

		// Collect all directory prefixes from touches across parallel tasks.
		dirToTasks := map[string][]*ForgeSpecTask{}
		for _, task := range tasks {
			for _, touch := range task.Touches {
				dir := touchDirectory(touch)
				if dir == "" {
					continue
				}
				dirToTasks[dir] = append(dirToTasks[dir], task)
			}
		}

		// For each directory touched by 2+ parallel tasks, check if there's
		// a serial scaffolding task that they all depend on.
		for dir, touching := range dirToTasks {
			if len(touching) < 2 {
				continue
			}

			// Check if there's a serial task (parallelizable: no, deps: none)
			// in the same repo that touches this directory and that all parallel
			// tasks depend on.
			hasScaffold := false
			for _, task := range spec.Tasks {
				if task.Parallelizable != "no" {
					continue
				}
				if task.Repo != repoID && task.Repo != "" {
					continue
				}
				if len(task.Deps) > 0 {
					continue
				}
				// Check if all parallel tasks touching this dir depend on this serial task.
				allDepend := true
				for _, pt := range touching {
					if pt.Name == task.Name {
						continue
					}
					found := false
					for _, dep := range pt.Deps {
						if strings.EqualFold(strings.TrimSpace(dep), task.Name) {
							found = true
							break
						}
					}
					if !found {
						allDepend = false
						break
					}
				}
				if allDepend {
					hasScaffold = true
					break
				}
			}

			if !hasScaffold {
				taskNames := make([]string, len(touching))
				for i, t := range touching {
					taskNames[i] = t.Name
				}
				issues = append(issues, SpecLintIssue{
					Severity:     "warning",
					Code:         "missing_scaffolding_task",
					File:         spec.RelativePath,
					Task:         touching[0].Name,
					Line:         touching[0].Line,
					Message:      fmt.Sprintf("Parallel tasks in repo '%s' share directory '%s' but have no serial scaffolding task to set up the structure first. Add a non-parallelizable task with no deps that the others depend on, or they will create conflicting files in separate worktrees.", repoID, dir),
					RelatedTasks: taskNames[1:],
				})
			}
		}
	}

	return issues
}

// touchDirectory extracts the parent directory from a touches entry.
// e.g. "internal/forge/run_execute.go" -> "internal/forge"
//
//	"src/features/auth/**" -> "src/features/auth"
func touchDirectory(touch string) string {
	normalized := normalizeScope(touch)
	if normalized == "" {
		return ""
	}
	// Extract the parent directory. Root-level files have no meaningful directory.
	idx := strings.LastIndex(normalized, "/")
	if idx <= 0 {
		return ""
	}
	return normalized[:idx]
}

func appendUnique(values []string, incoming ...string) []string {
	seen := map[string]bool{}
	for _, value := range values {
		seen[value] = true
	}
	for _, value := range incoming {
		if strings.TrimSpace(value) == "" || seen[value] {
			continue
		}
		values = append(values, value)
		seen[value] = true
	}
	return values
}
