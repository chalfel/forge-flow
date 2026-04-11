package forge

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type ProjectCreateInput struct {
	CWD         string `json:"cwd,omitempty"`
	Workspace   string `json:"workspace,omitempty"`
	ProjectID   string `json:"projectId,omitempty"`
	ProjectName string `json:"projectName,omitempty"`
	Pack        string `json:"pack,omitempty"`
	Topology    string `json:"topology,omitempty"`
	Path        string `json:"path,omitempty"`
	RootRepoID  string `json:"rootRepoId,omitempty"`
}

type ProjectCreatePlan struct {
	Workspace         string             `json:"workspace"`
	ProjectID         string             `json:"projectId"`
	ProjectName       string             `json:"projectName"`
	ProjectRoot       string             `json:"projectRoot"`
	ProjectConfigFile string             `json:"projectConfigFile"`
	Pack              StackPackSummary   `json:"pack"`
	Topology          ProjectTopology    `json:"topology"`
	CreationRoot      string             `json:"creationRoot"`
	Repos             []ProjectRepoPlan  `json:"repos,omitempty"`
	Components        []ProjectComponent `json:"components,omitempty"`
	ScaffoldFiles     []string           `json:"scaffoldFiles,omitempty"`
	FilesystemChanges []string           `json:"filesystemChanges,omitempty"`
	MetadataChanges   []string           `json:"metadataChanges,omitempty"`
	NextSteps         []string           `json:"nextSteps,omitempty"`
	Warnings          []string           `json:"warnings,omitempty"`
	Updated           bool               `json:"updated,omitempty"`
}

type projectCreateState struct {
	Input             ProjectCreateInput
	Workspace         string
	ProjectID         string
	ProjectName       string
	Pack              StackPack
	PackSummary       StackPackSummary
	ProjectRoot       string
	ProjectConfig     string
	CreationRoot      string
	Topology          ProjectTopology
	Repos             []ProjectRepoPlan
	Components        []ProjectComponent
	ScaffoldTargets   map[string]string
	ScaffoldSpecs     map[string]StackPackComponent
	FilesystemChanges []string
	MetadataChanges   []string
	NextSteps         []string
	Warnings          []string
}

func (s *Service) PlanProjectCreate(input ProjectCreateInput) (ProjectCreatePlan, error) {
	state, err := s.buildProjectCreateState(input)
	if err != nil {
		return ProjectCreatePlan{}, err
	}
	return state.plan(), nil
}

func (s *Service) ApplyProjectCreate(input ProjectCreateInput) (ProjectCreatePlan, error) {
	state, err := s.buildProjectCreateState(input)
	if err != nil {
		return ProjectCreatePlan{}, err
	}
	if _, err := s.InitWorkspace(state.Workspace, state.Workspace, ""); err != nil {
		return ProjectCreatePlan{}, err
	}
	if _, err := s.InitProject(state.Workspace, state.ProjectID, state.ProjectName); err != nil {
		return ProjectCreatePlan{}, err
	}
	for _, repo := range state.Repos {
		if err := os.MkdirAll(repo.Path, 0o755); err != nil {
			return ProjectCreatePlan{}, err
		}
		if _, err := s.linkRepoAtRoot(state.Workspace, state.ProjectID, repo.RepoID, repo.RepoName, repo.RepoRole, "", repo.Path); err != nil {
			return ProjectCreatePlan{}, err
		}
	}
	for _, component := range state.Components {
		if component.Location.Path == "" {
			continue
		}
		target := componentRootPath(state.Repos, component)
		if target == "" {
			continue
		}
		if err := os.MkdirAll(target, 0o755); err != nil {
			return ProjectCreatePlan{}, err
		}
	}
	projectRoot, cfg, err := s.loadProjectConfig(state.Workspace, state.ProjectID)
	if err != nil {
		return ProjectCreatePlan{}, err
	}
	cfg.Starter = &ProjectStarter{PackID: state.Pack.ID, PackName: state.Pack.Name}
	cfg.Topology = &state.Topology
	if cfg.Components == nil {
		cfg.Components = map[string]ProjectComponent{}
	}
	for _, component := range state.Components {
		cfg.Components[component.ComponentID] = component
	}
	normalizeProjectConfig(&cfg)
	if err := writeJSON(filepath.Join(projectRoot, "project.json"), cfg); err != nil {
		return ProjectCreatePlan{}, err
	}
	for _, component := range state.Components {
		if err := ensureComponentNote(filepath.Join(projectRoot, "kb", "repos.md"), component); err != nil {
			return ProjectCreatePlan{}, err
		}
	}
	if err := applyStackPackScaffolds(state); err != nil {
		return ProjectCreatePlan{}, err
	}
	if err := applyStackPackDocs(projectRoot, state.Pack, state.Topology, state.Components); err != nil {
		return ProjectCreatePlan{}, err
	}
	plan := state.plan()
	plan.Updated = true
	return plan, nil
}

func (s *Service) buildProjectCreateState(input ProjectCreateInput) (projectCreateState, error) {
	cwd := input.CWD
	if cwd == "" {
		current, err := os.Getwd()
		if err != nil {
			return projectCreateState{}, err
		}
		cwd = current
	}
	cwd = cleanAbs(cwd)
	workspace := firstNonEmpty(strings.TrimSpace(input.Workspace), "main")
	projectID := slugify(firstNonEmpty(input.ProjectID, input.ProjectName))
	if projectID == "" {
		return projectCreateState{}, fmt.Errorf("forge project create requires --project-id or --name")
	}
	projectName := firstNonEmpty(strings.TrimSpace(input.ProjectName), projectID)
	packID := slugify(firstNonEmpty(input.Pack, "blank"))
	resolvedPack, err := s.ResolveStackPack(packID)
	if err != nil {
		return projectCreateState{}, err
	}
	pack := resolvedPack.Pack
	packSummary := stackPackSummaryFromResolved(resolvedPack)
	topologyMode := normalizeTopology(firstNonEmpty(input.Topology, pack.DefaultTopology))
	if !containsString(pack.SupportedTopologies, topologyMode) {
		return projectCreateState{}, fmt.Errorf("stack pack %s does not support topology %s", pack.ID, topologyMode)
	}
	workspaceRoot, projectsDir, err := s.previewWorkspaceLayout(workspace)
	if err != nil {
		return projectCreateState{}, err
	}
	projectRoot := filepath.Join(workspaceRoot, projectsDir, projectID)
	if _, err := os.Stat(filepath.Join(projectRoot, "project.json")); err == nil {
		return projectCreateState{}, fmt.Errorf("project %s already exists in workspace %s", projectID, workspace)
	}
	creationRoot := filepath.Join(cwd, projectID)
	if strings.TrimSpace(input.Path) != "" {
		creationRoot = resolveInputPath(cwd, input.Path)
	}
	state := projectCreateState{
		Input:             input,
		Workspace:         workspace,
		ProjectID:         projectID,
		ProjectName:       projectName,
		Pack:              pack,
		PackSummary:       packSummary,
		ProjectRoot:       projectRoot,
		ProjectConfig:     filepath.Join(projectRoot, "project.json"),
		CreationRoot:      creationRoot,
		Topology:          ProjectTopology{Mode: topologyMode},
		ScaffoldTargets:   map[string]string{},
		ScaffoldSpecs:     map[string]StackPackComponent{},
		FilesystemChanges: []string{fmt.Sprintf("create shared project root %s", projectRoot)},
		MetadataChanges: []string{
			fmt.Sprintf("register project '%s' in workspace '%s'", projectID, workspace),
			fmt.Sprintf("record starter pack '%s' in project.json", pack.ID),
			fmt.Sprintf("set project topology to %s", topologyMode),
		},
		NextSteps: append([]string{}, pack.StarterSteps...),
	}
	if topologyMode == TopologyMultiRepo {
		state.FilesystemChanges = append(state.FilesystemChanges, fmt.Sprintf("use %s as the parent directory for new repos", creationRoot))
	} else {
		state.FilesystemChanges = append(state.FilesystemChanges, fmt.Sprintf("create repo root %s", creationRoot))
	}
	if err := s.populateProjectCreatePlan(&state); err != nil {
		return projectCreateState{}, err
	}
	state.MetadataChanges = append(state.MetadataChanges,
		"seed shared kb/, specs/, reports/, inbox.md, and config.md",
		"attach starter topology and quality defaults to the shared project docs",
	)
	if len(state.NextSteps) == 0 {
		state.NextSteps = []string{
			"create the first capability spec before implementing product logic",
			"run forge test setup once the first behavior-driven spec exists",
		}
	}
	return state, nil
}

func (s *Service) previewWorkspaceLayout(workspace string) (string, string, error) {
	if workspace == "" {
		workspace = "main"
	}
	if root, err := s.resolveWorkspaceRoot(workspace); err == nil {
		cfg, cfgErr := s.LoadWorkspaceConfig(workspace)
		if cfgErr != nil {
			return "", "", cfgErr
		}
		return root, cfg.ProjectsDir, nil
	}
	return defaultWorkspaceRoot(s.ForgeHome, workspace), "projects", nil
}

func (s *Service) populateProjectCreatePlan(state *projectCreateState) error {
	repoPlans := map[string]ProjectRepoPlan{}
	componentMap := map[string]ProjectComponent{}
	rootRepoID := ""
	ensureRepoPlan := func(repoID, repoNameTemplate, repoRole, repoPath string) ProjectRepoPlan {
		repoID = slugify(repoID)
		if existing, ok := repoPlans[repoID]; ok {
			return existing
		}
		vars := stackTemplateVars(state.ProjectID, state.ProjectName, repoID, "", "", topologyString(state.Topology))
		repoName := renderStackTemplate(firstNonEmpty(repoNameTemplate, defaultRepoName(state.ProjectID, repoID)), vars)
		plan := ProjectRepoPlan{RepoID: repoID, RepoName: repoName, RepoRole: strings.TrimSpace(repoRole), Path: cleanAbs(repoPath)}
		repoPlans[repoID] = plan
		return plan
	}
	ensureRootRepoPlan := func(defaultID, defaultNameTemplate, defaultRole string) ProjectRepoPlan {
		repoID := slugify(firstNonEmpty(inputRootRepoID(state.Input, state.Topology.Mode), defaultID, defaultRootRepoID(state.Topology.Mode)))
		plan := ensureRepoPlan(repoID, firstNonEmpty(defaultNameTemplate, "{{projectId}}"), firstNonEmpty(defaultRole, defaultRootRepoRole(state.Topology.Mode)), state.CreationRoot)
		if rootRepoID == "" {
			rootRepoID = plan.RepoID
		}
		return plan
	}

	for _, spec := range state.Pack.Components {
		host, ok := spec.Hosts[state.Topology.Mode]
		if !ok {
			return fmt.Errorf("stack pack %s does not define component %s for topology %s", state.Pack.ID, spec.ComponentID, state.Topology.Mode)
		}
		component := ProjectComponent{ComponentID: spec.ComponentID, Name: firstNonEmpty(spec.Name, spec.ComponentID), Kind: spec.Kind, Role: firstNonEmpty(spec.Role, defaultComponentRole(spec.Kind)), Stack: spec.Stack}
		switch host.Type {
		case ComponentLocationRepo:
			repoID := slugify(firstNonEmpty(host.RepoID, spec.ComponentID))
			repoPath := state.CreationRoot
			if state.Topology.Mode == TopologyMultiRepo {
				vars := stackTemplateVars(state.ProjectID, state.ProjectName, repoID, spec.ComponentID, component.Name, topologyString(state.Topology))
				repoName := renderStackTemplate(firstNonEmpty(host.RepoNameTemplate, defaultRepoName(state.ProjectID, repoID)), vars)
				repoPath = filepath.Join(state.CreationRoot, repoName)
			}
			plan := ensureRepoPlan(repoID, host.RepoNameTemplate, firstNonEmpty(host.RepoRole, component.Role), repoPath)
			component.Location = ProjectComponentLocation{Type: ComponentLocationRepo, RepoID: plan.RepoID}
			state.ScaffoldTargets[component.ComponentID] = plan.Path
			state.ScaffoldSpecs[component.ComponentID] = spec
			if state.Topology.Mode != TopologyMultiRepo && rootRepoID == "" {
				rootRepoID = plan.RepoID
			}
		case ComponentLocationPath, ComponentLocationPackage, ComponentLocationModule:
			rootSpecID := firstNonEmpty(host.RepoID, packRootRepoID(state.Pack), inputRootRepoID(state.Input, state.Topology.Mode))
			rootPlan := ensureRootRepoPlan(rootSpecID, packRootRepoNameTemplate(state.Pack), packRootRepoRole(state.Pack, state.Topology.Mode))
			relPath := firstNonEmpty(host.Path, defaultComponentPath(host.Type, spec.ComponentID))
			component.Location = ProjectComponentLocation{Type: host.Type, RepoID: rootPlan.RepoID, Path: relPath}
			state.ScaffoldTargets[component.ComponentID] = filepath.Join(rootPlan.Path, filepath.FromSlash(relPath))
			state.ScaffoldSpecs[component.ComponentID] = spec
		default:
			return fmt.Errorf("unsupported stack host type %q", host.Type)
		}
		componentMap[component.ComponentID] = component
	}
	if state.Topology.Mode != TopologyMultiRepo && len(repoPlans) == 0 {
		rootPlan := ensureRootRepoPlan(packRootRepoID(state.Pack), packRootRepoNameTemplate(state.Pack), packRootRepoRole(state.Pack, state.Topology.Mode))
		if _, exists := componentMap[rootPlan.RepoID]; !exists {
			componentMap[rootPlan.RepoID] = defaultComponentForRepo(rootPlan.RepoID, ProjectRepo{Name: rootPlan.RepoName, Role: rootPlan.RepoRole})
		}
	}
	if state.Topology.Mode == TopologySingleRepo && len(repoPlans) > 1 {
		return fmt.Errorf("stack pack %s produces %d repos in single-repo mode", state.Pack.ID, len(repoPlans))
	}
	if state.Topology.Mode == TopologyMonorepo && len(repoPlans) > 1 {
		return fmt.Errorf("stack pack %s produces %d repos in monorepo mode", state.Pack.ID, len(repoPlans))
	}
	if state.Topology.Mode != TopologyMultiRepo && rootRepoID == "" {
		rootRepoID = firstRepoPlanID(repoPlans)
	}
	if state.Topology.Mode != TopologyMultiRepo {
		state.Topology.RootRepoID = rootRepoID
	}
	for repoID, repo := range repoPlans {
		if _, exists := componentMap[repoID]; !exists {
			componentMap[repoID] = defaultComponentForRepo(repoID, ProjectRepo{Name: repo.RepoName, Role: repo.RepoRole})
		}
	}
	state.Repos = sortRepoPlans(repoPlans)
	state.Components = sortComponents(componentMap)
	for _, repo := range state.Repos {
		state.FilesystemChanges = append(state.FilesystemChanges, fmt.Sprintf("create repo directory %s", repo.Path))
		state.MetadataChanges = append(state.MetadataChanges, fmt.Sprintf("link repo '%s' (%s)", repo.RepoID, repo.RepoName))
	}
	for _, component := range state.Components {
		if component.Location.Path != "" {
			state.FilesystemChanges = append(state.FilesystemChanges, fmt.Sprintf("create component directory %s", componentRootPath(state.Repos, component)))
		}
		state.MetadataChanges = append(state.MetadataChanges, fmt.Sprintf("register component '%s' [%s]", component.ComponentID, component.Kind))
	}
	for componentID, spec := range state.ScaffoldSpecs {
		targetRoot := state.ScaffoldTargets[componentID]
		for relPath := range spec.ScaffoldFiles {
			state.FilesystemChanges = append(state.FilesystemChanges, fmt.Sprintf("write starter file %s", filepath.Join(targetRoot, filepath.FromSlash(relPath))))
		}
	}
	return nil
}

func (state projectCreateState) plan() ProjectCreatePlan {
	scaffoldFiles := make([]string, 0)
	for componentID, spec := range state.ScaffoldSpecs {
		targetRoot := state.ScaffoldTargets[componentID]
		paths := make([]string, 0, len(spec.ScaffoldFiles))
		for relPath := range spec.ScaffoldFiles {
			paths = append(paths, filepath.Join(targetRoot, filepath.FromSlash(relPath)))
		}
		sort.Strings(paths)
		scaffoldFiles = append(scaffoldFiles, paths...)
	}
	sort.Strings(scaffoldFiles)
	return ProjectCreatePlan{
		Workspace:         state.Workspace,
		ProjectID:         state.ProjectID,
		ProjectName:       state.ProjectName,
		ProjectRoot:       state.ProjectRoot,
		ProjectConfigFile: state.ProjectConfig,
		Pack:              state.PackSummary,
		Topology:          state.Topology,
		CreationRoot:      state.CreationRoot,
		Repos:             append([]ProjectRepoPlan(nil), state.Repos...),
		Components:        append([]ProjectComponent(nil), state.Components...),
		ScaffoldFiles:     scaffoldFiles,
		FilesystemChanges: uniqueSortedStrings(state.FilesystemChanges),
		MetadataChanges:   uniqueSortedStrings(state.MetadataChanges),
		NextSteps:         uniqueSortedStrings(state.NextSteps),
		Warnings:          uniqueSortedStrings(state.Warnings),
	}
}

func applyStackPackScaffolds(state projectCreateState) error {
	for componentID, spec := range state.ScaffoldSpecs {
		targetRoot := state.ScaffoldTargets[componentID]
		component := projectComponentByID(state.Components, componentID)
		if component.ComponentID == "" {
			continue
		}
		vars := stackTemplateVars(state.ProjectID, state.ProjectName, component.Location.RepoID, component.ComponentID, component.Name, topologyString(state.Topology))
		vars["repoName"] = repoNameByID(state.Repos, component.Location.RepoID)
		vars["componentPackageName"] = firstNonEmpty(vars["repoName"], defaultRepoName(state.ProjectID, component.ComponentID))
		for relPath, content := range spec.ScaffoldFiles {
			targetFile := filepath.Join(targetRoot, filepath.FromSlash(relPath))
			rendered := renderStackTemplate(content, vars)
			if err := writeFileIfMissing(targetFile, rendered); err != nil {
				return err
			}
		}
	}
	return nil
}

func applyStackPackDocs(projectRoot string, pack StackPack, topology ProjectTopology, components []ProjectComponent) error {
	marker := fmt.Sprintf("<!-- forge:starter:%s -->", pack.ID)
	componentLines := make([]string, 0, len(components))
	for _, component := range components {
		location := component.Location.Type
		if component.Location.Path != "" {
			location = fmt.Sprintf("%s in repo %s at %s", component.Location.Type, component.Location.RepoID, component.Location.Path)
		} else if component.Location.RepoID != "" {
			location = fmt.Sprintf("%s in repo %s", component.Location.Type, component.Location.RepoID)
		}
		line := fmt.Sprintf("- `%s` — %s — %s", component.ComponentID, fallback(component.Kind, "component"), location)
		if component.Stack != "" {
			line += fmt.Sprintf(" — stack: %s", component.Stack)
		}
		componentLines = append(componentLines, line)
	}
	sort.Strings(componentLines)
	architectureSection := strings.TrimSpace(fmt.Sprintf(`
%s
## Starter topology
- **Pack:** %s (`+"`%s`"+`)
- **Mode:** %s
%s
`, marker, pack.Name, pack.ID, topology.Mode, strings.Join(componentLines, "\n"))) + "\n"
	if err := appendIfMissing(filepath.Join(projectRoot, "kb", "architecture.md"), marker, architectureSection); err != nil {
		return err
	}
	qualityLines := make([]string, 0, len(pack.QualityExpectations))
	for _, item := range pack.QualityExpectations {
		if strings.TrimSpace(item) == "" {
			continue
		}
		qualityLines = append(qualityLines, "- "+item)
	}
	sort.Strings(qualityLines)
	if len(qualityLines) > 0 {
		configSection := strings.TrimSpace(fmt.Sprintf(`
%s
## Quality defaults
%s
`, marker, strings.Join(qualityLines, "\n"))) + "\n"
		if err := appendIfMissing(filepath.Join(projectRoot, "config.md"), marker, configSection); err != nil {
			return err
		}
	}
	return nil
}

func appendIfMissing(path, marker, section string) error {
	content, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	text := string(content)
	if strings.Contains(text, marker) {
		return nil
	}
	return os.WriteFile(path, []byte(strings.TrimRight(text, "\n")+"\n\n"+strings.TrimSpace(section)+"\n"), 0o644)
}

func sortRepoPlans(plans map[string]ProjectRepoPlan) []ProjectRepoPlan {
	out := make([]ProjectRepoPlan, 0, len(plans))
	for _, plan := range plans {
		out = append(out, plan)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].RepoID < out[j].RepoID })
	return out
}

func sortComponents(components map[string]ProjectComponent) []ProjectComponent {
	out := make([]ProjectComponent, 0, len(components))
	for _, component := range components {
		out = append(out, component)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ComponentID < out[j].ComponentID })
	return out
}

func firstRepoPlanID(plans map[string]ProjectRepoPlan) string {
	ids := make([]string, 0, len(plans))
	for id := range plans {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	if len(ids) == 0 {
		return ""
	}
	return ids[0]
}

func repoNameByID(repos []ProjectRepoPlan, repoID string) string {
	for _, repo := range repos {
		if repo.RepoID == repoID {
			return repo.RepoName
		}
	}
	return ""
}

func componentRootPath(repos []ProjectRepoPlan, component ProjectComponent) string {
	for _, repo := range repos {
		if repo.RepoID != component.Location.RepoID {
			continue
		}
		if component.Location.Path == "" {
			return repo.Path
		}
		return filepath.Join(repo.Path, filepath.FromSlash(component.Location.Path))
	}
	return ""
}

func projectComponentByID(components []ProjectComponent, componentID string) ProjectComponent {
	for _, component := range components {
		if component.ComponentID == componentID {
			return component
		}
	}
	return ProjectComponent{}
}

func stackPackSummary(pack StackPack) StackPackSummary {
	return StackPackSummary{ID: pack.ID, Name: pack.Name, Description: pack.Description, DefaultTopology: pack.DefaultTopology, SupportedTopologies: append([]string(nil), pack.SupportedTopologies...)}
}

// ---------------------------------------------------------------------------
// Existing mode — link existing repos to a new Forge project
// ---------------------------------------------------------------------------

// ExistingRepoRef points to an existing directory to link as a repo.
type ExistingRepoRef struct {
	Path   string `json:"path"`
	RepoID string `json:"repoId,omitempty"`
	Role   string `json:"role,omitempty"`
}

// ProjectCreateExistingInput is the input for creating a project from existing repos.
type ProjectCreateExistingInput struct {
	CWD         string            `json:"cwd,omitempty"`
	Workspace   string            `json:"workspace,omitempty"`
	ProjectID   string            `json:"projectId,omitempty"`
	ProjectName string            `json:"projectName,omitempty"`
	Repos       []ExistingRepoRef `json:"repos"`
	SetupDev    bool              `json:"setupDev,omitempty"`
}

// ProjectCreateExistingPlan describes what will happen when linking existing repos.
type ProjectCreateExistingPlan struct {
	Workspace         string             `json:"workspace"`
	ProjectID         string             `json:"projectId"`
	ProjectName       string             `json:"projectName"`
	ProjectRoot       string             `json:"projectRoot"`
	ProjectConfigFile string             `json:"projectConfigFile"`
	Topology          ProjectTopology    `json:"topology"`
	Repos             []ProjectRepoPlan  `json:"repos,omitempty"`
	Components        []ProjectComponent `json:"components,omitempty"`
	MetadataChanges   []string           `json:"metadataChanges,omitempty"`
	FilesystemChanges []string           `json:"filesystemChanges,omitempty"`
	NextSteps         []string           `json:"nextSteps,omitempty"`
	Warnings          []string           `json:"warnings,omitempty"`
	DevConfigPath     string             `json:"devConfigPath,omitempty"`
	Updated           bool               `json:"updated,omitempty"`
}

func (s *Service) PlanProjectCreateExisting(input ProjectCreateExistingInput) (ProjectCreateExistingPlan, error) {
	return s.buildProjectCreateExistingPlan(input, false)
}

func (s *Service) ApplyProjectCreateExisting(input ProjectCreateExistingInput) (ProjectCreateExistingPlan, error) {
	return s.buildProjectCreateExistingPlan(input, true)
}

func (s *Service) buildProjectCreateExistingPlan(input ProjectCreateExistingInput, apply bool) (ProjectCreateExistingPlan, error) {
	cwd := input.CWD
	if cwd == "" {
		current, err := os.Getwd()
		if err != nil {
			return ProjectCreateExistingPlan{}, err
		}
		cwd = current
	}
	cwd = cleanAbs(cwd)

	workspace := firstNonEmpty(strings.TrimSpace(input.Workspace), "main")
	projectID := slugify(firstNonEmpty(input.ProjectID, input.ProjectName))
	if projectID == "" {
		return ProjectCreateExistingPlan{}, fmt.Errorf("forge project create --existing requires --project-id or --name")
	}
	projectName := firstNonEmpty(strings.TrimSpace(input.ProjectName), projectID)

	if len(input.Repos) == 0 {
		return ProjectCreateExistingPlan{}, fmt.Errorf("at least one repo path is required")
	}

	// resolve and validate repo paths
	var repos []ProjectRepoPlan
	var components []ProjectComponent
	for _, ref := range input.Repos {
		absPath := ref.Path
		if !filepath.IsAbs(absPath) {
			absPath = filepath.Join(cwd, absPath)
		}
		absPath = cleanAbs(absPath)

		if !dirExists(absPath) {
			return ProjectCreateExistingPlan{}, fmt.Errorf("repo path does not exist: %s", absPath)
		}

		repoID := slugify(firstNonEmpty(ref.RepoID, filepath.Base(absPath)))
		role := firstNonEmpty(ref.Role, "")

		repos = append(repos, ProjectRepoPlan{
			RepoID:   repoID,
			RepoName: filepath.Base(absPath),
			RepoRole: role,
			Path:     absPath,
		})

		kind := inferComponentKind(repoID, role)
		components = append(components, ProjectComponent{
			ComponentID: repoID,
			Name:        filepath.Base(absPath),
			Kind:        kind,
			Role:        role,
			Location: ProjectComponentLocation{
				Type:   ComponentLocationRepo,
				RepoID: repoID,
			},
		})
	}

	// infer topology
	topologyMode := TopologyMultiRepo
	if len(repos) == 1 {
		topologyMode = TopologySingleRepo
	}
	topology := ProjectTopology{Mode: topologyMode}
	if topologyMode == TopologySingleRepo {
		topology.RootRepoID = repos[0].RepoID
	}

	// resolve workspace layout
	_, projectsDir, err := s.previewWorkspaceLayout(workspace)
	if err != nil {
		return ProjectCreateExistingPlan{}, err
	}
	workspaceRoot := defaultWorkspaceRoot(s.ForgeHome, workspace)
	if resolved, resolveErr := s.resolveWorkspaceRoot(workspace); resolveErr == nil {
		workspaceRoot = resolved
	}
	projectRoot := filepath.Join(workspaceRoot, projectsDir, projectID)

	// check project doesn't already exist
	if _, err := os.Stat(filepath.Join(projectRoot, "project.json")); err == nil {
		return ProjectCreateExistingPlan{}, fmt.Errorf("project %s already exists in workspace %s", projectID, workspace)
	}

	// build plan metadata
	metadataChanges := []string{
		fmt.Sprintf("register project '%s' in workspace '%s'", projectID, workspace),
		fmt.Sprintf("set project topology to %s", topologyMode),
	}
	for _, repo := range repos {
		metadataChanges = append(metadataChanges, fmt.Sprintf("link existing repo '%s' at %s", repo.RepoID, repo.Path))
	}
	for _, comp := range components {
		metadataChanges = append(metadataChanges, fmt.Sprintf("register component '%s' [%s]", comp.ComponentID, comp.Kind))
	}
	metadataChanges = append(metadataChanges, "seed shared kb/, specs/, reports/, inbox.md, and config.md")

	filesystemChanges := []string{
		fmt.Sprintf("create shared project root %s", projectRoot),
	}
	for _, repo := range repos {
		filesystemChanges = append(filesystemChanges, fmt.Sprintf("create .forge/ binding in %s", repo.Path))
	}

	nextSteps := []string{
		"create the first capability spec with /forge-spec",
		"use forge project add to register more surfaces later",
	}

	var warnings []string
	var devConfigPath string

	if input.SetupDev {
		metadataChanges = append(metadataChanges, fmt.Sprintf("generate dev session config to ~/.config/forge-dev/%s.yml", projectID))
	}

	plan := ProjectCreateExistingPlan{
		Workspace:         workspace,
		ProjectID:         projectID,
		ProjectName:       projectName,
		ProjectRoot:       projectRoot,
		ProjectConfigFile: filepath.Join(projectRoot, "project.json"),
		Topology:          topology,
		Repos:             repos,
		Components:        components,
		MetadataChanges:   metadataChanges,
		FilesystemChanges: filesystemChanges,
		NextSteps:         nextSteps,
		Warnings:          warnings,
	}

	if !apply {
		return plan, nil
	}

	// --- Apply ---

	if _, err := s.InitWorkspace(workspace, workspace, ""); err != nil {
		return ProjectCreateExistingPlan{}, err
	}
	if _, err := s.InitProject(workspace, projectID, projectName); err != nil {
		return ProjectCreateExistingPlan{}, err
	}

	for _, repo := range repos {
		if _, err := s.linkRepoAtRoot(workspace, projectID, repo.RepoID, repo.RepoName, repo.RepoRole, "", repo.Path); err != nil {
			return ProjectCreateExistingPlan{}, err
		}
	}

	// write topology and components to project.json
	projRoot, cfg, err := s.loadProjectConfig(workspace, projectID)
	if err != nil {
		return ProjectCreateExistingPlan{}, err
	}
	cfg.Topology = &topology
	if cfg.Components == nil {
		cfg.Components = map[string]ProjectComponent{}
	}
	for _, comp := range components {
		cfg.Components[comp.ComponentID] = comp
	}
	normalizeProjectConfig(&cfg)
	if err := writeJSON(filepath.Join(projRoot, "project.json"), cfg); err != nil {
		return ProjectCreateExistingPlan{}, err
	}

	// ensure component notes in KB
	for _, comp := range components {
		if err := ensureComponentNote(filepath.Join(projRoot, "kb", "repos.md"), comp); err != nil {
			return ProjectCreateExistingPlan{}, err
		}
	}

	// dev session config
	if input.SetupDev && len(repos) > 0 {
		paths := make([]string, len(repos))
		for i, r := range repos {
			paths[i] = r.Path
		}
		services, svcErr := DetectServicesForPaths(cwd, paths)
		if svcErr == nil && len(services) > 0 {
			devCfg := DevConfig{
				Root:     cwd,
				Session:  projectID,
				Services: services,
			}
			if path, writeErr := WriteDevConfig(projectID, devCfg); writeErr == nil {
				devConfigPath = path
			}
		}
		if devConfigPath == "" {
			plan.Warnings = append(plan.Warnings, "no dev services detected — skipped dev session config")
		}
	}

	plan.DevConfigPath = devConfigPath
	plan.Updated = true
	return plan, nil
}

func stackPackSummaryFromResolved(resolved ResolvedStackPack) StackPackSummary {
	summary := stackPackSummary(resolved.Pack)
	summary.SourceKind = resolved.Source.Kind
	summary.SourceName = resolved.Source.Name
	summary.SourcePath = resolved.Source.Root
	return summary
}

func stackTemplateVars(projectID, projectName, repoID, componentID, componentName, topology string) map[string]string {
	return map[string]string{
		"projectId":            projectID,
		"projectName":          projectName,
		"repoId":               repoID,
		"componentId":          componentID,
		"componentName":        componentName,
		"topology":             topology,
		"componentPackageName": defaultRepoName(projectID, firstNonEmpty(componentID, repoID)),
	}
}

func renderStackTemplate(content string, vars map[string]string) string {
	out := content
	keys := make([]string, 0, len(vars))
	for key := range vars {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		out = strings.ReplaceAll(out, "{{"+key+"}}", vars[key])
	}
	return out
}

func topologyString(topology ProjectTopology) string {
	return topology.Mode
}

func inputRootRepoID(input ProjectCreateInput, topologyMode string) string {
	if topologyMode == TopologyMultiRepo {
		return ""
	}
	return slugify(input.RootRepoID)
}

func packRootRepoID(pack StackPack) string {
	if pack.RootRepo == nil {
		return ""
	}
	return pack.RootRepo.RepoID
}

func packRootRepoNameTemplate(pack StackPack) string {
	if pack.RootRepo == nil {
		return ""
	}
	return pack.RootRepo.RepoNameTemplate
}

func packRootRepoRole(pack StackPack, topologyMode string) string {
	if pack.RootRepo != nil && strings.TrimSpace(pack.RootRepo.Role) != "" {
		return pack.RootRepo.Role
	}
	return defaultRootRepoRole(topologyMode)
}

func defaultRootRepoID(topologyMode string) string {
	if topologyMode == TopologyMonorepo {
		return "platform"
	}
	return "app"
}

func defaultRootRepoRole(topologyMode string) string {
	if topologyMode == TopologyMonorepo {
		return "monorepo root"
	}
	return "primary application repo"
}

func uniqueSortedStrings(values []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}
