package forge

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type ProjectAddInput struct {
	CWD         string `json:"cwd,omitempty"`
	Workspace   string `json:"workspace,omitempty"`
	ProjectID   string `json:"projectId,omitempty"`
	Kind        string `json:"kind,omitempty"`
	ComponentID string `json:"componentId,omitempty"`
	Name        string `json:"name,omitempty"`
	Stack       string `json:"stack,omitempty"`
	As          string `json:"as,omitempty"`
	RepoID      string `json:"repoId,omitempty"`
	RepoName    string `json:"repoName,omitempty"`
	RepoRole    string `json:"repoRole,omitempty"`
	Path        string `json:"path,omitempty"`
}

type ProjectAddPlan struct {
	Workspace         string           `json:"workspace"`
	ProjectID         string           `json:"projectId"`
	ProjectName       string           `json:"projectName"`
	ProjectConfigFile string           `json:"projectConfigFile"`
	ReposDocFile      string           `json:"reposDocFile"`
	CurrentTopology   ProjectTopology  `json:"currentTopology"`
	ResultingTopology ProjectTopology  `json:"resultingTopology"`
	Kind              string           `json:"kind"`
	As                string           `json:"as,omitempty"`
	Component         ProjectComponent `json:"component"`
	Repo              *ProjectRepoPlan `json:"repo,omitempty"`
	RequiresChoice    bool             `json:"requiresChoice,omitempty"`
	Choices           []string         `json:"choices,omitempty"`
	FilesystemChanges []string         `json:"filesystemChanges,omitempty"`
	MetadataChanges   []string         `json:"metadataChanges,omitempty"`
	NextSteps         []string         `json:"nextSteps,omitempty"`
	Warnings          []string         `json:"warnings,omitempty"`
	Updated           bool             `json:"updated,omitempty"`
}

type ProjectRepoPlan struct {
	RepoID   string `json:"repoId"`
	RepoName string `json:"repoName,omitempty"`
	RepoRole string `json:"repoRole,omitempty"`
	Path     string `json:"path,omitempty"`
}

type projectState struct {
	Context     ResolvedContext
	ProjectRoot string
	Config      ProjectConfig
	Local       LocalConfig
	Binding     LocalProjectBinding
}

func (s *Service) PlanProjectAdd(input ProjectAddInput) (ProjectAddPlan, error) {
	state, err := s.resolveProjectState(input)
	if err != nil {
		return ProjectAddPlan{}, err
	}
	return buildProjectAddPlan(state, input)
}

func (s *Service) ApplyProjectAdd(input ProjectAddInput) (ProjectAddPlan, error) {
	state, err := s.resolveProjectState(input)
	if err != nil {
		return ProjectAddPlan{}, err
	}
	plan, err := buildProjectAddPlan(state, input)
	if err != nil {
		return ProjectAddPlan{}, err
	}
	if plan.RequiresChoice {
		return ProjectAddPlan{}, fmt.Errorf("single-repo projects require --as <repo|app|package|module> before Forge can apply project add")
	}
	componentID := plan.Component.ComponentID
	state.Config.Topology = &plan.ResultingTopology
	if state.Config.Components == nil {
		state.Config.Components = map[string]ProjectComponent{}
	}
	state.Config.Components[componentID] = plan.Component
	if plan.Repo != nil {
		if state.Config.Repos == nil {
			state.Config.Repos = map[string]ProjectRepo{}
		}
		state.Config.Repos[plan.Repo.RepoID] = ProjectRepo{Name: plan.Repo.RepoName, Role: plan.Repo.RepoRole}
		if state.Binding.RepoPaths == nil {
			state.Binding.RepoPaths = map[string]string{}
		}
		state.Binding.RepoPaths[plan.Repo.RepoID] = cleanAbs(plan.Repo.Path)
		state.Binding.Workspace = state.Context.Workspace
		state.Local.Projects[state.Context.ProjectID] = state.Binding
		if err := os.MkdirAll(plan.Repo.Path, 0o755); err != nil {
			return ProjectAddPlan{}, err
		}
	}
	if plan.Component.Location.Path != "" {
		rootRepoPath := state.Binding.RepoPaths[plan.Component.Location.RepoID]
		if rootRepoPath == "" && state.Context.RepoID == plan.Component.Location.RepoID {
			rootRepoPath = state.Context.RepoRoot
		}
		if rootRepoPath != "" {
			target := filepath.Join(rootRepoPath, filepath.FromSlash(plan.Component.Location.Path))
			if err := os.MkdirAll(target, 0o755); err != nil {
				return ProjectAddPlan{}, err
			}
		}
	}
	if err := writeJSON(filepath.Join(state.ProjectRoot, "project.json"), state.Config); err != nil {
		return ProjectAddPlan{}, err
	}
	if err := s.SaveLocalConfig(state.Local); err != nil {
		return ProjectAddPlan{}, err
	}
	if err := ensureComponentNote(filepath.Join(state.ProjectRoot, "kb", "repos.md"), plan.Component); err != nil {
		return ProjectAddPlan{}, err
	}
	plan.Updated = true
	return plan, nil
}

func (s *Service) resolveProjectState(input ProjectAddInput) (projectState, error) {
	cwd := input.CWD
	if cwd == "" {
		current, err := os.Getwd()
		if err != nil {
			return projectState{}, err
		}
		cwd = current
	}
	cwd = cleanAbs(cwd)
	local, err := s.LoadLocalConfig()
	if err != nil {
		return projectState{}, err
	}

	ctx, resolveErr := s.ResolveContext(cwd)
	workspace := input.Workspace
	projectID := input.ProjectID
	if resolveErr == nil {
		if workspace == "" {
			workspace = ctx.Workspace
		}
		if projectID == "" {
			projectID = ctx.ProjectID
		}
		if projectID != ctx.ProjectID {
			return projectState{}, fmt.Errorf("current repo is linked to project %s, not %s", ctx.ProjectID, projectID)
		}
	} else {
		if workspace == "" {
			workspace = "main"
		}
		if projectID == "" {
			return projectState{}, fmt.Errorf("unable to resolve Forge context from %s; provide --workspace and --project-id", cwd)
		}
	}

	projectRoot, cfg, err := s.loadProjectConfig(workspace, projectID)
	if err != nil {
		return projectState{}, err
	}
	normalizeProjectConfig(&cfg)
	binding := local.Projects[projectID]
	if binding.RepoPaths == nil {
		binding.RepoPaths = map[string]string{}
	}
	if binding.Workspace == "" {
		binding.Workspace = workspace
	}
	if ctx.ProjectID == "" {
		ctx = ResolvedContext{
			Workspace:     workspace,
			ProjectID:     cfg.ProjectID,
			ProjectName:   cfg.ProjectName,
			ProjectRoot:   projectRoot,
			ProjectConfig: filepath.Join(projectRoot, "project.json"),
			Repos:         reposFromBinding(workspace, projectID, cfg, binding),
		}
	}
	return projectState{Context: ctx, ProjectRoot: projectRoot, Config: cfg, Local: local, Binding: binding}, nil
}

func buildProjectAddPlan(state projectState, input ProjectAddInput) (ProjectAddPlan, error) {
	kind := slugify(firstNonEmpty(input.Kind, input.ComponentID, input.Name))
	if kind == "" {
		return ProjectAddPlan{}, fmt.Errorf("missing component kind; use --kind <web|api|mobile|worker|package|docs|...>")
	}
	componentID := slugify(firstNonEmpty(input.ComponentID, kind, input.Name))
	if componentID == "" {
		return ProjectAddPlan{}, fmt.Errorf("missing component id")
	}
	if _, exists := state.Config.Components[componentID]; exists {
		return ProjectAddPlan{}, fmt.Errorf("component %s already exists in project %s", componentID, state.Config.ProjectID)
	}
	if _, exists := state.Config.Repos[componentID]; exists && input.As != ComponentLocationRepo {
		return ProjectAddPlan{}, fmt.Errorf("repo/component id %s already exists", componentID)
	}

	currentTopology := *state.Config.Topology
	host := normalizeAddHost(input.As)
	plan := ProjectAddPlan{
		Workspace:         state.Context.Workspace,
		ProjectID:         state.Config.ProjectID,
		ProjectName:       state.Config.ProjectName,
		ProjectConfigFile: filepath.Join(state.ProjectRoot, "project.json"),
		ReposDocFile:      filepath.Join(state.ProjectRoot, "kb", "repos.md"),
		CurrentTopology:   currentTopology,
		ResultingTopology: currentTopology,
		Kind:              kind,
		As:                host,
		Component: ProjectComponent{
			ComponentID: componentID,
			Name:        defaultComponentName(componentID, kind, input.Name),
			Kind:        kind,
			Role:        defaultComponentRole(kind),
			Stack:       strings.TrimSpace(input.Stack),
		},
	}
	if strings.TrimSpace(input.RepoRole) != "" {
		plan.Component.Role = strings.TrimSpace(input.RepoRole)
	}

	if host == "" && currentTopology.Mode == TopologySingleRepo {
		plan.RequiresChoice = true
		plan.Choices = []string{"repo", "app", "package", "module"}
		plan.Warnings = append(plan.Warnings, "single-repo projects require a hosting choice before Forge can add a new surface")
		plan.NextSteps = append(plan.NextSteps,
			"re-run with --as repo to split the new surface into its own repository",
			"re-run with --as app or --as package to transition toward a monorepo layout",
			"re-run with --as module to keep the new surface inside the existing repo",
		)
		return plan, nil
	}
	if host == "" {
		host = defaultAddHost(currentTopology.Mode, kind)
		plan.As = host
	}

	switch host {
	case ComponentLocationRepo:
		repoID := slugify(firstNonEmpty(input.RepoID, componentID))
		repoName := firstNonEmpty(strings.TrimSpace(input.RepoName), defaultRepoName(state.Config.ProjectID, componentID))
		repoRole := firstNonEmpty(strings.TrimSpace(input.RepoRole), plan.Component.Role)
		repoPath := defaultNewRepoPath(state.Context.RepoRoot, state.Config.ProjectID, componentID)
		if strings.TrimSpace(input.Path) != "" {
			repoPath = resolveInputPath(state.Context.RepoRoot, input.Path)
		}
		plan.Component.Location = ProjectComponentLocation{Type: ComponentLocationRepo, RepoID: repoID}
		plan.ResultingTopology = ProjectTopology{Mode: TopologyMultiRepo}
		plan.Repo = &ProjectRepoPlan{RepoID: repoID, RepoName: repoName, RepoRole: repoRole, Path: repoPath}
		plan.MetadataChanges = append(plan.MetadataChanges,
			fmt.Sprintf("register component '%s' as a repo-backed surface", componentID),
			fmt.Sprintf("add repo '%s' to project.json and local bindings", repoID),
			"append the new surface to kb/repos.md",
		)
		plan.FilesystemChanges = append(plan.FilesystemChanges,
			fmt.Sprintf("create repo directory %s", repoPath),
			fmt.Sprintf("future scaffold/bootstrap work can initialize Git and boilerplate inside %s", repoPath),
		)
		plan.NextSteps = append(plan.NextSteps,
			fmt.Sprintf("scaffold the new repo at %s", repoPath),
			fmt.Sprintf("link or initialize Forge inside the new repo using repo id '%s'", repoID),
			"consider running forge test setup after the first capability spec exists for the new surface",
		)
	case ComponentLocationPath, ComponentLocationPackage, ComponentLocationModule:
		rootRepoID, err := rootRepoForTopology(state)
		if err != nil {
			return ProjectAddPlan{}, err
		}
		relPath := defaultComponentPath(host, componentID)
		if strings.TrimSpace(input.Path) != "" {
			relPath = normalizeRelativePath(input.Path)
		}
		plan.Component.Location = ProjectComponentLocation{Type: host, RepoID: rootRepoID, Path: relPath}
		if currentTopology.Mode == TopologySingleRepo && host != ComponentLocationModule {
			plan.ResultingTopology = ProjectTopology{Mode: TopologyMonorepo, RootRepoID: rootRepoID}
		}
		if currentTopology.Mode == TopologySingleRepo && host == ComponentLocationModule {
			plan.ResultingTopology = ProjectTopology{Mode: TopologySingleRepo, RootRepoID: rootRepoID}
		}
		if currentTopology.Mode == TopologyMonorepo {
			plan.ResultingTopology = ProjectTopology{Mode: TopologyMonorepo, RootRepoID: rootRepoID}
		}
		rootRepoPath := state.Binding.RepoPaths[rootRepoID]
		if rootRepoPath == "" && state.Context.RepoID == rootRepoID {
			rootRepoPath = state.Context.RepoRoot
		}
		fullPath := filepath.Join(rootRepoPath, filepath.FromSlash(relPath))
		plan.MetadataChanges = append(plan.MetadataChanges,
			fmt.Sprintf("register component '%s' in project.json with %s location", componentID, host),
			fmt.Sprintf("track the new surface under repo '%s' at %s", rootRepoID, relPath),
			"append the new surface to kb/repos.md",
		)
		plan.FilesystemChanges = append(plan.FilesystemChanges,
			fmt.Sprintf("create directory %s", fullPath),
			fmt.Sprintf("future scaffold/bootstrap work can add starter files under %s", relPath),
		)
		plan.NextSteps = append(plan.NextSteps,
			fmt.Sprintf("scaffold the new %s inside repo '%s' at %s", kind, rootRepoID, relPath),
			"update any repo-level workspace files if the chosen stack needs them",
			"consider creating the first capability spec for the new surface immediately",
		)
	default:
		return ProjectAddPlan{}, fmt.Errorf("unsupported add host '%s'", host)
	}

	return plan, nil
}

func reposFromBinding(workspace, projectID string, cfg ProjectConfig, binding LocalProjectBinding) []RepoSummary {
	out := make([]RepoSummary, 0, len(cfg.Repos))
	for repoID, repo := range cfg.Repos {
		out = append(out, RepoSummary{Workspace: workspace, ProjectID: projectID, RepoID: repoID, Name: repo.Name, Role: repo.Role, Git: repo.Git, LocalPath: cleanAbs(binding.RepoPaths[repoID])})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].RepoID < out[j].RepoID })
	return out
}

func normalizeAddHost(value string) string {
	value = slugify(value)
	switch value {
	case "repo":
		return ComponentLocationRepo
	case "app", "path":
		return ComponentLocationPath
	case "package", "pkg":
		return ComponentLocationPackage
	case "module":
		return ComponentLocationModule
	default:
		return ""
	}
}

func defaultAddHost(mode, kind string) string {
	switch mode {
	case TopologyMultiRepo:
		return ComponentLocationRepo
	case TopologyMonorepo:
		if kind == "package" || kind == "sdk" || kind == "library" || kind == "design-system" {
			return ComponentLocationPackage
		}
		if kind == "module" {
			return ComponentLocationModule
		}
		return ComponentLocationPath
	default:
		return ""
	}
}

func defaultComponentName(componentID, kind, explicitName string) string {
	if strings.TrimSpace(explicitName) != "" {
		return explicitName
	}
	if kind == componentID {
		return strings.Title(strings.ReplaceAll(componentID, "-", " "))
	}
	return strings.Title(strings.ReplaceAll(componentID, "-", " "))
}

func defaultComponentRole(kind string) string {
	switch kind {
	case "web":
		return "customer-facing web app"
	case "api":
		return "core api"
	case "mobile":
		return "mobile app"
	case "admin":
		return "admin surface"
	case "worker":
		return "background worker"
	case "package", "sdk", "library", "design-system":
		return "shared package"
	case "docs":
		return "documentation surface"
	default:
		return kind + " component"
	}
}

func defaultRepoName(projectID, componentID string) string {
	return slugify(projectID + "-" + componentID)
}

func defaultNewRepoPath(currentRepoRoot, projectID, componentID string) string {
	name := defaultRepoName(projectID, componentID)
	if currentRepoRoot != "" {
		return filepath.Join(filepath.Dir(cleanAbs(currentRepoRoot)), name)
	}
	home, err := os.UserHomeDir()
	if err == nil {
		return filepath.Join(home, "code", name)
	}
	return cleanAbs(name)
}

func defaultComponentPath(host, componentID string) string {
	switch host {
	case ComponentLocationPackage:
		return filepath.ToSlash(filepath.Join("packages", componentID))
	case ComponentLocationModule:
		return filepath.ToSlash(filepath.Join("modules", componentID))
	default:
		return filepath.ToSlash(filepath.Join("apps", componentID))
	}
}

func resolveInputPath(base, input string) string {
	input = strings.TrimSpace(input)
	if input == "" {
		return cleanAbs(base)
	}
	if filepath.IsAbs(input) {
		return cleanAbs(input)
	}
	if base == "" {
		return cleanAbs(input)
	}
	return cleanAbs(filepath.Join(base, input))
}

func normalizeRelativePath(value string) string {
	value = filepath.ToSlash(strings.TrimSpace(value))
	value = strings.TrimPrefix(value, "./")
	value = strings.TrimPrefix(value, "/")
	return value
}

func rootRepoForTopology(state projectState) (string, error) {
	if state.Config.Topology != nil && state.Config.Topology.RootRepoID != "" {
		return state.Config.Topology.RootRepoID, nil
	}
	if state.Context.RepoID != "" {
		return state.Context.RepoID, nil
	}
	if repoID := firstRepoID(state.Config.Repos); repoID != "" {
		return repoID, nil
	}
	return "", fmt.Errorf("could not determine the root repo for this project")
}

func ensureComponentNote(path string, component ProjectComponent) error {
	if component.Location.Type == ComponentLocationRepo {
		return ensureRepoNote(path, component.ComponentID, ProjectRepo{Name: component.Name, Role: component.Role})
	}
	content, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	text := string(content)
	heading := fmt.Sprintf("## %s", component.ComponentID)
	if strings.Contains(text, heading+"\n") || strings.HasSuffix(strings.TrimSpace(text), heading) {
		return nil
	}
	location := component.Location.Type
	if component.Location.Path != "" {
		location = fmt.Sprintf("%s in repo %s at %s", component.Location.Type, component.Location.RepoID, component.Location.Path)
	}
	block := fmt.Sprintf("\n## %s\n- **Name:** %s\n- **Kind:** %s\n- **Role:** %s\n- **Location:** %s\n- **Owns:** Fill in the main surfaces this component owns.\n- **Depends on:** Fill in important upstream repos/services.\n", component.ComponentID, fallback(component.Name, component.ComponentID), fallback(component.Kind, "component"), fallback(component.Role, "Not specified yet"), location)
	return os.WriteFile(path, []byte(strings.TrimRight(text, "\n")+block+"\n"), 0o644)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
