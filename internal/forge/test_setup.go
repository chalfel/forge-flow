package forge

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type RepoTestHarness struct {
	RepoID               string            `json:"repoId"`
	RepoName             string            `json:"repoName,omitempty"`
	RepoRole             string            `json:"repoRole,omitempty"`
	Root                 string            `json:"root"`
	PackageManager       string            `json:"packageManager,omitempty"`
	Scripts              map[string]string `json:"scripts,omitempty"`
	NodeProject          bool              `json:"nodeProject,omitempty"`
	GoProject            bool              `json:"goProject,omitempty"`
	HasPlaywright        bool              `json:"hasPlaywright,omitempty"`
	HasCypress           bool              `json:"hasCypress,omitempty"`
	HasVitest            bool              `json:"hasVitest,omitempty"`
	HasJest              bool              `json:"hasJest,omitempty"`
	HasIntegrationScript bool              `json:"hasIntegrationScript,omitempty"`
	HasE2EScript         bool              `json:"hasE2EScript,omitempty"`
	HasTestScript        bool              `json:"hasTestScript,omitempty"`
	HasIntegrationFiles  bool              `json:"hasIntegrationFiles,omitempty"`
	HasSupertest         bool              `json:"hasSupertest,omitempty"`
	HasTestcontainers    bool              `json:"hasTestcontainers,omitempty"`
}

type TestSetupPlanItem struct {
	Scenario  string   `json:"scenario"`
	RepoID    string   `json:"repoId"`
	RepoName  string   `json:"repoName,omitempty"`
	Layer     string   `json:"layer"`
	Runner    string   `json:"runner"`
	Command   string   `json:"command,omitempty"`
	Strategy  string   `json:"strategy"`
	Reason    string   `json:"reason"`
	CoveredBy []string `json:"coveredBy,omitempty"`
}

type TestSetupPlan struct {
	Workspace   string              `json:"workspace"`
	ProjectID   string              `json:"projectId"`
	ProjectName string              `json:"projectName"`
	SpecsDir    string              `json:"specsDir"`
	SpecFile    string              `json:"specFile"`
	Bootstrap   bool                `json:"bootstrap"`
	Updated     bool                `json:"updated"`
	Harnesses   []RepoTestHarness   `json:"harnesses"`
	Items       []TestSetupPlanItem `json:"items"`
}

type packageJSON struct {
	Scripts         map[string]string `json:"scripts"`
	Dependencies    map[string]string `json:"dependencies"`
	DevDependencies map[string]string `json:"devDependencies"`
}

func (s *Service) PlanTestSetup(cwd, specPath string, bootstrap bool) (TestSetupPlan, error) {
	ctx, spec, err := s.inspectSingleSpec(cwd, specPath)
	if err != nil {
		return TestSetupPlan{}, err
	}
	harnesses, err := detectHarnesses(ctx.Repos)
	if err != nil {
		return TestSetupPlan{}, err
	}
	items := buildTestSetupPlanItems(spec, harnesses, bootstrap)
	return TestSetupPlan{
		Workspace:   ctx.Workspace,
		ProjectID:   ctx.ProjectID,
		ProjectName: ctx.ProjectName,
		SpecsDir:    ctx.SpecsDir,
		SpecFile:    spec.RelativePath,
		Bootstrap:   bootstrap,
		Harnesses:   harnesses,
		Items:       items,
	}, nil
}

func (s *Service) ApplyTestSetup(cwd, specPath string, bootstrap bool) (TestSetupPlan, error) {
	ctx, spec, err := s.inspectSingleSpec(cwd, specPath)
	if err != nil {
		return TestSetupPlan{}, err
	}
	harnesses, err := detectHarnesses(ctx.Repos)
	if err != nil {
		return TestSetupPlan{}, err
	}
	items := buildTestSetupPlanItems(spec, harnesses, bootstrap)
	if err := applyValidationPlanSection(spec.Path, items); err != nil {
		return TestSetupPlan{}, err
	}
	return TestSetupPlan{
		Workspace:   ctx.Workspace,
		ProjectID:   ctx.ProjectID,
		ProjectName: ctx.ProjectName,
		SpecsDir:    ctx.SpecsDir,
		SpecFile:    spec.RelativePath,
		Bootstrap:   bootstrap,
		Updated:     true,
		Harnesses:   harnesses,
		Items:       items,
	}, nil
}

func (s *Service) inspectSingleSpec(cwd, specPath string) (ResolvedContext, ForgeSpecFile, error) {
	ctx, specs, err := s.inspectSpecs(cwd, specPath)
	if err != nil {
		return ResolvedContext{}, ForgeSpecFile{}, err
	}
	if len(specs) == 0 {
		return ResolvedContext{}, ForgeSpecFile{}, fmt.Errorf("no spec files found")
	}
	if len(specs) != 1 {
		return ResolvedContext{}, ForgeSpecFile{}, fmt.Errorf("forge test setup requires exactly one spec file, got %d", len(specs))
	}
	return ctx, specs[0], nil
}

func detectHarnesses(repos []RepoSummary) ([]RepoTestHarness, error) {
	out := make([]RepoTestHarness, 0, len(repos))
	for _, repo := range repos {
		if repo.LocalPath == "" {
			continue
		}
		harness, err := detectRepoHarness(repo)
		if err != nil {
			return nil, err
		}
		out = append(out, harness)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].RepoID < out[j].RepoID })
	return out, nil
}

func detectRepoHarness(repo RepoSummary) (RepoTestHarness, error) {
	harness := RepoTestHarness{
		RepoID:   repo.RepoID,
		RepoName: repo.Name,
		RepoRole: repo.Role,
		Root:     cleanAbs(repo.LocalPath),
		Scripts:  map[string]string{},
	}
	if harness.Root == "" {
		return harness, nil
	}
	pkgPath := filepath.Join(harness.Root, "package.json")
	if _, err := os.Stat(pkgPath); err == nil {
		harness.NodeProject = true
		harness.PackageManager = detectPackageManager(harness.Root)
		var pkg packageJSON
		if err := readJSON(pkgPath, &pkg); err == nil {
			harness.Scripts = pkg.Scripts
			deps := mergeStringMaps(pkg.Dependencies, pkg.DevDependencies)
			harness.HasPlaywright = hasStringKey(deps, "@playwright/test") || pathAnyExists(harness.Root, "playwright.config.ts", "playwright.config.js", "playwright.config.mjs", "playwright.config.cjs") || scriptContains(harness.Scripts, "playwright")
			if harness.HasPlaywright {
				harness.HasE2EScript = harness.HasE2EScript || hasAnyScript(harness.Scripts, "test:e2e", "e2e", "test:playwright")
			}
			harness.HasCypress = hasStringKey(deps, "cypress") || pathAnyExists(harness.Root, "cypress.config.ts", "cypress.config.js", "cypress.config.mjs", "cypress.config.cjs") || scriptContains(harness.Scripts, "cypress")
			if harness.HasCypress {
				harness.HasE2EScript = harness.HasE2EScript || hasAnyScript(harness.Scripts, "test:e2e", "e2e", "test:cypress")
			}
			harness.HasVitest = hasStringKey(deps, "vitest") || pathAnyExists(harness.Root, "vitest.config.ts", "vitest.config.js", "vitest.workspace.ts", "vitest.workspace.js") || scriptContains(harness.Scripts, "vitest")
			harness.HasJest = hasStringKey(deps, "jest") || pathAnyExists(harness.Root, "jest.config.ts", "jest.config.js", "jest.config.cjs", "jest.config.mjs") || scriptContains(harness.Scripts, "jest")
			harness.HasSupertest = hasStringKey(deps, "supertest")
			harness.HasTestcontainers = hasStringKey(deps, "testcontainers")
		}
		harness.HasIntegrationScript = hasAnyScript(harness.Scripts, "test:integration", "integration", "integration:test")
		harness.HasE2EScript = harness.HasE2EScript || hasAnyScript(harness.Scripts, "test:e2e", "e2e", "e2e:test")
		harness.HasTestScript = hasAnyScript(harness.Scripts, "test")
		harness.HasIntegrationFiles = pathAnyExists(harness.Root, "tests/integration", "test/integration", "integration-tests")
	}
	if _, err := os.Stat(filepath.Join(harness.Root, "go.mod")); err == nil {
		harness.GoProject = true
		if harness.PackageManager == "" {
			harness.PackageManager = "go"
		}
	}
	return harness, nil
}

func detectPackageManager(root string) string {
	switch {
	case pathAnyExists(root, "pnpm-lock.yaml"):
		return "pnpm"
	case pathAnyExists(root, "yarn.lock"):
		return "yarn"
	case pathAnyExists(root, "bun.lockb", "bun.lock"):
		return "bun"
	case pathAnyExists(root, "package-lock.json", "npm-shrinkwrap.json"):
		return "npm"
	default:
		return "npm"
	}
}

func buildTestSetupPlanItems(spec ForgeSpecFile, harnesses []RepoTestHarness, bootstrap bool) []TestSetupPlanItem {
	harnessByRepo := map[string]RepoTestHarness{}
	for _, harness := range harnesses {
		harnessByRepo[harness.RepoID] = harness
	}
	coverage := map[string]map[string][]ForgeSpecTask{}
	allTasksByRepo := map[string][]ForgeSpecTask{}
	for _, task := range spec.Tasks {
		if task.Repo != "" {
			allTasksByRepo[task.Repo] = append(allTasksByRepo[task.Repo], task)
		}
		for _, covered := range task.Covers {
			key := strings.ToLower(strings.TrimSpace(covered))
			if coverage[key] == nil {
				coverage[key] = map[string][]ForgeSpecTask{}
			}
			coverage[key][task.Repo] = append(coverage[key][task.Repo], task)
		}
	}

	items := []TestSetupPlanItem{}
	for _, scenario := range spec.TestCases {
		scenarioKey := strings.ToLower(strings.TrimSpace(scenario.Name))
		repoTasks := coverage[scenarioKey]
		if len(repoTasks) == 0 {
			repoTasks = map[string][]ForgeSpecTask{}
			for repoID, tasks := range allTasksByRepo {
				repoTasks[repoID] = append([]ForgeSpecTask(nil), tasks...)
			}
		}
		repoIDs := make([]string, 0, len(repoTasks))
		for repoID := range repoTasks {
			if strings.TrimSpace(repoID) == "" {
				continue
			}
			repoIDs = append(repoIDs, repoID)
		}
		sort.Strings(repoIDs)
		for _, repoID := range repoIDs {
			tasks := repoTasks[repoID]
			item := recommendValidationForScenario(scenario, repoID, tasks, harnessByRepo[repoID], bootstrap)
			items = append(items, item)
		}
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].Scenario != items[j].Scenario {
			return items[i].Scenario < items[j].Scenario
		}
		return items[i].RepoID < items[j].RepoID
	})
	return items
}

func recommendValidationForScenario(scenario ForgeSpecScenario, repoID string, tasks []ForgeSpecTask, harness RepoTestHarness, bootstrap bool) TestSetupPlanItem {
	focus := inferRepoFocus(repoID, harness.RepoRole, tasks)
	item := TestSetupPlanItem{
		Scenario:  scenario.Name,
		RepoID:    repoID,
		RepoName:  fallback(harness.RepoName, repoID),
		CoveredBy: coveredTaskNames(tasks),
	}

	switch {
	case focus == "ui" && (harness.HasPlaywright || harness.HasCypress || harness.HasE2EScript):
		item.Layer = "e2e"
		item.Runner = preferredUIRunner(harness)
		item.Command = preferredE2ECommand(harness)
		item.Strategy = "reuse"
		item.Reason = "existing browser runner detected in repo"
	case focus == "api" && (harness.HasIntegrationScript || harness.HasIntegrationFiles || harness.HasSupertest || harness.HasTestcontainers):
		item.Layer = "integration"
		item.Runner = preferredIntegrationRunner(harness)
		item.Command = preferredIntegrationCommand(harness)
		item.Strategy = "reuse"
		item.Reason = "existing integration-oriented test harness detected in repo"
	case harness.HasVitest || harness.HasJest || harness.GoProject || harness.HasTestScript:
		item.Layer = fallback(validationLayerForFocus(focus), "unit")
		item.Runner = preferredGeneralRunner(harness)
		item.Command = preferredGeneralCommand(harness)
		item.Strategy = "reuse"
		item.Reason = "reusing the repo's existing primary test runner"
	case bootstrap:
		item.Layer = bootstrapLayerForFocus(focus)
		item.Runner = bootstrapRunnerForFocus(harness, focus)
		item.Command = bootstrapCommandForFocus(harness, focus)
		item.Strategy = "bootstrap"
		item.Reason = "no suitable harness was found; Forge should bootstrap the minimum viable validation setup"
	default:
		item.Layer = "manual"
		item.Runner = "manual"
		item.Strategy = "manual"
		item.Reason = "no suitable harness was found; enable --bootstrap to get a setup recommendation"
	}
	return item
}

func inferRepoFocus(repoID, repoRole string, tasks []ForgeSpecTask) string {
	combined := strings.ToLower(repoID + " " + repoRole)
	if strings.Contains(combined, "web") || strings.Contains(combined, "frontend") || strings.Contains(combined, "mobile") || strings.Contains(combined, "admin") || strings.Contains(combined, "ui") {
		return "ui"
	}
	if strings.Contains(combined, "api") || strings.Contains(combined, "backend") || strings.Contains(combined, "worker") || strings.Contains(combined, "service") {
		return "api"
	}
	for _, task := range tasks {
		for _, touch := range task.Touches {
			touch = normalizeScope(touch)
			switch {
			case hasSlashPrefix(touch, "app"), hasSlashPrefix(touch, "pages"), hasSlashPrefix(touch, "src/app"), hasSlashPrefix(touch, "src/components"), strings.Contains(touch, "/screen"), strings.Contains(touch, "/ui"):
				return "ui"
			case hasSlashPrefix(touch, "src/routes"), hasSlashPrefix(touch, "src/controllers"), hasSlashPrefix(touch, "src/services"), hasSlashPrefix(touch, "openapi"), hasSlashPrefix(touch, "prisma"), strings.Contains(touch, "schema"):
				return "api"
			}
		}
	}
	return "general"
}

func coveredTaskNames(tasks []ForgeSpecTask) []string {
	names := make([]string, 0, len(tasks))
	for _, task := range tasks {
		names = append(names, task.Name)
	}
	sort.Strings(names)
	return names
}

func preferredUIRunner(harness RepoTestHarness) string {
	switch {
	case harness.HasPlaywright:
		return "playwright"
	case harness.HasCypress:
		return "cypress"
	case harness.HasE2EScript:
		return "browser-runner"
	default:
		return "playwright"
	}
}

func preferredIntegrationRunner(harness RepoTestHarness) string {
	switch {
	case harness.HasIntegrationScript && harness.HasVitest:
		return "vitest"
	case harness.HasIntegrationScript && harness.HasJest:
		return "jest"
	case harness.GoProject:
		return "go test"
	case harness.HasVitest:
		return "vitest"
	case harness.HasJest:
		return "jest"
	default:
		return "integration-runner"
	}
}

func preferredGeneralRunner(harness RepoTestHarness) string {
	switch {
	case harness.GoProject:
		return "go test"
	case harness.HasVitest:
		return "vitest"
	case harness.HasJest:
		return "jest"
	case harness.HasTestScript:
		return "repo-test-script"
	default:
		return "manual"
	}
}

func validationLayerForFocus(focus string) string {
	switch focus {
	case "ui":
		return "e2e"
	case "api":
		return "integration"
	default:
		return "unit"
	}
}

func bootstrapLayerForFocus(focus string) string {
	switch focus {
	case "ui":
		return "e2e"
	case "api":
		return "integration"
	default:
		return "integration"
	}
}

func bootstrapRunnerForFocus(harness RepoTestHarness, focus string) string {
	if harness.GoProject {
		return "go test"
	}
	switch focus {
	case "ui":
		return "playwright"
	case "api":
		if harness.HasVitest || harness.NodeProject {
			return "vitest"
		}
		if harness.HasJest {
			return "jest"
		}
		return "integration-runner"
	default:
		if harness.HasVitest || harness.NodeProject {
			return "vitest"
		}
		if harness.HasJest {
			return "jest"
		}
		return "playwright"
	}
}

func bootstrapCommandForFocus(harness RepoTestHarness, focus string) string {
	runner := bootstrapRunnerForFocus(harness, focus)
	switch runner {
	case "go test":
		return "go test ./..."
	case "playwright":
		return packageManagerExec(harness.PackageManager, "playwright test")
	case "vitest":
		return packageManagerExec(harness.PackageManager, "vitest run")
	case "jest":
		return packageManagerExec(harness.PackageManager, "jest")
	default:
		return "define a repo-specific validation command"
	}
}

func preferredE2ECommand(harness RepoTestHarness) string {
	switch {
	case hasScript(harness.Scripts, "test:e2e"):
		return packageManagerRun(harness.PackageManager, "test:e2e")
	case hasScript(harness.Scripts, "e2e"):
		return packageManagerRun(harness.PackageManager, "e2e")
	case harness.HasPlaywright:
		return packageManagerExec(harness.PackageManager, "playwright test")
	case harness.HasCypress:
		return packageManagerExec(harness.PackageManager, "cypress run")
	default:
		return ""
	}
}

func preferredIntegrationCommand(harness RepoTestHarness) string {
	switch {
	case hasScript(harness.Scripts, "test:integration"):
		return packageManagerRun(harness.PackageManager, "test:integration")
	case hasScript(harness.Scripts, "integration"):
		return packageManagerRun(harness.PackageManager, "integration")
	case harness.GoProject:
		return "go test ./..."
	case harness.HasVitest:
		return packageManagerExec(harness.PackageManager, "vitest run")
	case harness.HasJest:
		return packageManagerExec(harness.PackageManager, "jest")
	default:
		return ""
	}
}

func preferredGeneralCommand(harness RepoTestHarness) string {
	switch {
	case hasScript(harness.Scripts, "test"):
		return packageManagerRun(harness.PackageManager, "test")
	case harness.GoProject:
		return "go test ./..."
	case harness.HasVitest:
		return packageManagerExec(harness.PackageManager, "vitest run")
	case harness.HasJest:
		return packageManagerExec(harness.PackageManager, "jest")
	default:
		return ""
	}
}

func applyValidationPlanSection(specPath string, items []TestSetupPlanItem) error {
	content, err := os.ReadFile(specPath)
	if err != nil {
		return err
	}
	text := strings.ReplaceAll(string(content), "\r\n", "\n")
	lines := strings.Split(text, "\n")
	newSection := strings.Split(strings.TrimSuffix(renderValidationPlan(items), "\n"), "\n")

	start := -1
	end := -1
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "## Validation plan" {
			start = i
			end = len(lines)
			for j := i + 1; j < len(lines); j++ {
				candidate := strings.TrimSpace(lines[j])
				if strings.HasPrefix(candidate, "### Task:") || (strings.HasPrefix(candidate, "## ") && candidate != "## Validation plan") {
					end = j
					break
				}
			}
			break
		}
	}

	var out []string
	if start >= 0 {
		out = append(out, lines[:start]...)
		out = append(out, newSection...)
		out = append(out, lines[end:]...)
	} else {
		insertAt := len(lines)
		for i, line := range lines {
			if strings.HasPrefix(strings.TrimSpace(line), "### Task:") {
				insertAt = i
				break
			}
		}
		out = append(out, lines[:insertAt]...)
		if insertAt > 0 && strings.TrimSpace(out[len(out)-1]) != "" {
			out = append(out, "")
		}
		out = append(out, newSection...)
		out = append(out, lines[insertAt:]...)
	}
	return os.WriteFile(specPath, []byte(strings.TrimRight(strings.Join(out, "\n"), "\n")+"\n"), 0o644)
}

func renderValidationPlan(items []TestSetupPlanItem) string {
	var b strings.Builder
	b.WriteString("## Validation plan\n\n")
	if len(items) == 0 {
		b.WriteString("- No validation scenarios were inferred yet.\n")
		return b.String()
	}
	for _, item := range items {
		heading := item.Scenario
		if item.RepoID != "" {
			heading = fmt.Sprintf("%s — %s", item.Scenario, item.RepoID)
		}
		fmt.Fprintf(&b, "### Scenario: %s\n", heading)
		fmt.Fprintf(&b, "- Repo: %s\n", item.RepoID)
		fmt.Fprintf(&b, "- Layer: %s\n", item.Layer)
		fmt.Fprintf(&b, "- Preferred runner: %s\n", item.Runner)
		fmt.Fprintf(&b, "- Strategy: %s\n", item.Strategy)
		if item.Command != "" {
			fmt.Fprintf(&b, "- Command: %s\n", item.Command)
		}
		if len(item.CoveredBy) > 0 {
			fmt.Fprintf(&b, "- Covered by tasks: %s\n", strings.Join(item.CoveredBy, ", "))
		}
		fmt.Fprintf(&b, "- Notes: %s\n\n", item.Reason)
	}
	return b.String()
}

func packageManagerRun(pm, script string) string {
	switch pm {
	case "pnpm":
		return "pnpm " + script
	case "yarn":
		return "yarn " + script
	case "bun":
		return "bun run " + script
	case "npm", "":
		return "npm run " + script
	case "go":
		return "go test ./..."
	default:
		return pm + " run " + script
	}
}

func packageManagerExec(pm, command string) string {
	switch pm {
	case "pnpm":
		return "pnpm exec " + command
	case "yarn":
		return "yarn " + command
	case "bun":
		return "bunx " + command
	case "npm", "":
		return "npx " + command
	case "go":
		return command
	default:
		return command
	}
}

func hasAnyScript(scripts map[string]string, names ...string) bool {
	for _, name := range names {
		if hasScript(scripts, name) {
			return true
		}
	}
	return false
}

func hasScript(scripts map[string]string, name string) bool {
	_, ok := scripts[name]
	return ok
}

func scriptContains(scripts map[string]string, needle string) bool {
	for _, script := range scripts {
		if strings.Contains(strings.ToLower(script), strings.ToLower(needle)) {
			return true
		}
	}
	return false
}

func mergeStringMaps(left, right map[string]string) map[string]string {
	out := map[string]string{}
	for key, value := range left {
		out[key] = value
	}
	for key, value := range right {
		out[key] = value
	}
	return out
}

func hasStringKey(values map[string]string, key string) bool {
	_, ok := values[key]
	return ok
}

func pathAnyExists(root string, paths ...string) bool {
	for _, value := range paths {
		if _, err := os.Stat(filepath.Join(root, value)); err == nil {
			return true
		}
	}
	return false
}

func (h RepoTestHarness) MarshalJSON() ([]byte, error) {
	type alias RepoTestHarness
	if h.Scripts == nil {
		h.Scripts = map[string]string{}
	}
	return json.Marshal(alias(h))
}
