package forge

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

const (
	LocalVersion     = 1
	WorkspaceVersion = 1
	ProjectVersion   = 1
	RepoLinkVersion  = 1
)

type Service struct {
	ForgeHome   string
	specWriteMu sync.Mutex
}

type LocalConfig struct {
	Version    int                              `json:"version"`
	Workspaces map[string]LocalWorkspaceBinding `json:"workspaces"`
	Projects   map[string]LocalProjectBinding   `json:"projects"`
}

type LocalWorkspaceBinding struct {
	Root string `json:"root"`
}

type LocalProjectBinding struct {
	Workspace string            `json:"workspace"`
	RepoPaths map[string]string `json:"repoPaths,omitempty"`
}

type WorkspaceConfig struct {
	Version     int    `json:"version"`
	WorkspaceID string `json:"workspaceId"`
	Name        string `json:"name"`
	ProjectsDir string `json:"projectsDir"`
	SharedDir   string `json:"sharedDir"`
}

type ProjectConfig struct {
	Version     int                         `json:"version"`
	ProjectID   string                      `json:"projectId"`
	ProjectName string                      `json:"projectName"`
	Starter     *ProjectStarter             `json:"starter,omitempty"`
	Topology    *ProjectTopology            `json:"topology,omitempty"`
	Repos       map[string]ProjectRepo      `json:"repos"`
	Components  map[string]ProjectComponent `json:"components,omitempty"`
}

type ProjectRepo struct {
	Name string `json:"name,omitempty"`
	Role string `json:"role,omitempty"`
	Git  string `json:"git,omitempty"`
}

type RepoLinkFile struct {
	Version   int    `json:"version"`
	Workspace string `json:"workspace"`
	ProjectID string `json:"projectId"`
	RepoID    string `json:"repoId"`
}

type WorkspaceSummary struct {
	WorkspaceID string `json:"workspaceId"`
	Name        string `json:"name"`
	Root        string `json:"root"`
}

type ProjectSummary struct {
	Workspace   string `json:"workspace"`
	ProjectID   string `json:"projectId"`
	ProjectName string `json:"projectName"`
	ProjectRoot string `json:"projectRoot"`
}

type RepoSummary struct {
	Workspace string `json:"workspace"`
	ProjectID string `json:"projectId"`
	RepoID    string `json:"repoId"`
	Name      string `json:"name,omitempty"`
	Role      string `json:"role,omitempty"`
	Git       string `json:"git,omitempty"`
	LocalPath string `json:"localPath,omitempty"`
}

type ResolvedContext struct {
	ForgeHome       string        `json:"forgeHome"`
	Workspace       string        `json:"workspace"`
	WorkspaceRoot   string        `json:"workspaceRoot"`
	WorkspaceShared string        `json:"workspaceSharedDir"`
	ProjectID       string        `json:"projectId"`
	ProjectName     string        `json:"projectName"`
	ProjectRoot     string        `json:"projectRoot"`
	RepoID          string        `json:"repoId"`
	RepoRoot        string        `json:"repoRoot,omitempty"`
	RepoLocalForge  string        `json:"repoLocalForgeDir,omitempty"`
	KBDir           string        `json:"kbDir"`
	SpecsDir        string        `json:"specsDir"`
	ReportsDir      string        `json:"reportsDir"`
	InboxFile       string        `json:"inboxFile"`
	ProjectConfig   string        `json:"projectConfigFile"`
	RepoLinkFile    string        `json:"repoLinkFile,omitempty"`
	Repos           []RepoSummary `json:"repos,omitempty"`
}

type PIInstallStatus struct {
	SettingsPath string `json:"settingsPath"`
	PackagePath  string `json:"packagePath"`
	Installed    bool   `json:"installed"`
}

func NewServiceFromEnv() (*Service, error) {
	home := os.Getenv("FORGE_HOME")
	if home == "" {
		userHome, err := os.UserHomeDir()
		if err != nil {
			return nil, err
		}
		home = filepath.Join(userHome, ".forge")
	}
	abs, err := filepath.Abs(home)
	if err != nil {
		return nil, err
	}
	return &Service{ForgeHome: abs}, nil
}

func (s *Service) localConfigPath() string {
	return filepath.Join(s.ForgeHome, "local.json")
}

func defaultWorkspaceRoot(forgeHome, workspace string) string {
	return filepath.Join(forgeHome, "workspaces", workspace)
}

func (s *Service) LoadLocalConfig() (LocalConfig, error) {
	path := s.localConfigPath()
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		return LocalConfig{Version: LocalVersion, Workspaces: map[string]LocalWorkspaceBinding{}, Projects: map[string]LocalProjectBinding{}}, nil
	}
	var cfg LocalConfig
	if err := readJSON(path, &cfg); err != nil {
		return LocalConfig{}, err
	}
	if cfg.Version == 0 {
		cfg.Version = LocalVersion
	}
	if cfg.Workspaces == nil {
		cfg.Workspaces = map[string]LocalWorkspaceBinding{}
	}
	if cfg.Projects == nil {
		cfg.Projects = map[string]LocalProjectBinding{}
	}
	return cfg, nil
}

func (s *Service) SaveLocalConfig(cfg LocalConfig) error {
	if cfg.Version == 0 {
		cfg.Version = LocalVersion
	}
	if cfg.Workspaces == nil {
		cfg.Workspaces = map[string]LocalWorkspaceBinding{}
	}
	if cfg.Projects == nil {
		cfg.Projects = map[string]LocalProjectBinding{}
	}
	return writeJSON(s.localConfigPath(), cfg)
}

func (s *Service) InitWorkspace(workspaceID, name, root string) (WorkspaceSummary, error) {
	if workspaceID == "" {
		workspaceID = "main"
	}
	if name == "" {
		name = strings.Title(strings.ReplaceAll(workspaceID, "-", " "))
	}
	if root == "" {
		root = defaultWorkspaceRoot(s.ForgeHome, workspaceID)
	}
	root = cleanAbs(root)

	cfg := WorkspaceConfig{
		Version:     WorkspaceVersion,
		WorkspaceID: workspaceID,
		Name:        name,
		ProjectsDir: "projects",
		SharedDir:   "shared",
	}

	if err := os.MkdirAll(filepath.Join(root, cfg.ProjectsDir), 0o755); err != nil {
		return WorkspaceSummary{}, err
	}
	if err := os.MkdirAll(filepath.Join(root, cfg.SharedDir), 0o755); err != nil {
		return WorkspaceSummary{}, err
	}
	if err := writeJSON(filepath.Join(root, "workspace.json"), cfg); err != nil {
		return WorkspaceSummary{}, err
	}

	local, err := s.LoadLocalConfig()
	if err != nil {
		return WorkspaceSummary{}, err
	}
	local.Workspaces[workspaceID] = LocalWorkspaceBinding{Root: root}
	if err := s.SaveLocalConfig(local); err != nil {
		return WorkspaceSummary{}, err
	}

	return WorkspaceSummary{WorkspaceID: workspaceID, Name: name, Root: root}, nil
}

func (s *Service) ListWorkspaces() ([]WorkspaceSummary, error) {
	local, err := s.LoadLocalConfig()
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(local.Workspaces))
	for id := range local.Workspaces {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	out := make([]WorkspaceSummary, 0, len(ids))
	for _, id := range ids {
		root := cleanAbs(local.Workspaces[id].Root)
		cfg, err := s.LoadWorkspaceConfig(id)
		if err != nil {
			out = append(out, WorkspaceSummary{WorkspaceID: id, Name: id, Root: root})
			continue
		}
		out = append(out, WorkspaceSummary{WorkspaceID: id, Name: cfg.Name, Root: root})
	}
	return out, nil
}

func (s *Service) LoadWorkspaceConfig(workspaceID string) (WorkspaceConfig, error) {
	root, err := s.resolveWorkspaceRoot(workspaceID)
	if err != nil {
		return WorkspaceConfig{}, err
	}
	var cfg WorkspaceConfig
	if err := readJSON(filepath.Join(root, "workspace.json"), &cfg); err != nil {
		return WorkspaceConfig{}, err
	}
	if cfg.Version == 0 {
		cfg.Version = WorkspaceVersion
	}
	if cfg.ProjectsDir == "" {
		cfg.ProjectsDir = "projects"
	}
	if cfg.SharedDir == "" {
		cfg.SharedDir = "shared"
	}
	if cfg.WorkspaceID == "" {
		cfg.WorkspaceID = workspaceID
	}
	if cfg.Name == "" {
		cfg.Name = workspaceID
	}
	return cfg, nil
}

func (s *Service) InitProject(workspaceID, projectID, projectName string) (ProjectSummary, error) {
	if workspaceID == "" {
		workspaceID = "main"
	}
	if projectID == "" {
		projectID = slugify(projectName)
	}
	if projectID == "" {
		return ProjectSummary{}, fmt.Errorf("missing project id")
	}
	if projectName == "" {
		projectName = projectID
	}

	workspaceRoot, workspaceCfg, err := s.ensureWorkspace(workspaceID)
	if err != nil {
		return ProjectSummary{}, err
	}
	projectRoot := filepath.Join(workspaceRoot, workspaceCfg.ProjectsDir, projectID)
	projectCfg := ProjectConfig{
		Version:     ProjectVersion,
		ProjectID:   projectID,
		ProjectName: projectName,
		Topology:    &ProjectTopology{Mode: TopologySingleRepo},
		Repos:       map[string]ProjectRepo{},
		Components:  map[string]ProjectComponent{},
	}

	if err := os.MkdirAll(projectRoot, 0o755); err != nil {
		return ProjectSummary{}, err
	}
	if err := ensureProjectFiles(projectRoot, projectName); err != nil {
		return ProjectSummary{}, err
	}
	cfgPath := filepath.Join(projectRoot, "project.json")
	if _, err := os.Stat(cfgPath); err == nil {
		var existing ProjectConfig
		if err := readJSON(cfgPath, &existing); err == nil {
			if existing.ProjectName != "" {
				projectCfg.ProjectName = existing.ProjectName
			}
			if existing.Starter != nil {
				projectCfg.Starter = existing.Starter
			}
			if existing.Topology != nil {
				projectCfg.Topology = existing.Topology
			}
			if len(existing.Repos) > 0 {
				projectCfg.Repos = existing.Repos
			}
			if len(existing.Components) > 0 {
				projectCfg.Components = existing.Components
			}
		}
	}
	if err := writeJSON(cfgPath, projectCfg); err != nil {
		return ProjectSummary{}, err
	}

	local, err := s.LoadLocalConfig()
	if err != nil {
		return ProjectSummary{}, err
	}
	binding := local.Projects[projectID]
	binding.Workspace = workspaceID
	if binding.RepoPaths == nil {
		binding.RepoPaths = map[string]string{}
	}
	local.Projects[projectID] = binding
	if err := s.SaveLocalConfig(local); err != nil {
		return ProjectSummary{}, err
	}

	return ProjectSummary{Workspace: workspaceID, ProjectID: projectID, ProjectName: projectCfg.ProjectName, ProjectRoot: projectRoot}, nil
}

func (s *Service) ListProjects(workspaceID string) ([]ProjectSummary, error) {
	if workspaceID == "" {
		workspaceID = "main"
	}
	workspaceRoot, workspaceCfg, err := s.ensureWorkspace(workspaceID)
	if err != nil {
		return nil, err
	}
	projectsRoot := filepath.Join(workspaceRoot, workspaceCfg.ProjectsDir)
	entries, err := os.ReadDir(projectsRoot)
	if err != nil {
		return nil, err
	}
	out := []ProjectSummary{}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		projectRoot := filepath.Join(projectsRoot, entry.Name())
		cfgPath := filepath.Join(projectRoot, "project.json")
		var cfg ProjectConfig
		if err := readJSON(cfgPath, &cfg); err != nil {
			continue
		}
		out = append(out, ProjectSummary{Workspace: workspaceID, ProjectID: cfg.ProjectID, ProjectName: cfg.ProjectName, ProjectRoot: projectRoot})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ProjectID < out[j].ProjectID })
	return out, nil
}

func (s *Service) LinkRepo(workspaceID, projectID, repoID, repoName, repoRole, gitRemote, repoPath string) (ResolvedContext, error) {
	if workspaceID == "" {
		workspaceID = "main"
	}
	if projectID == "" {
		return ResolvedContext{}, fmt.Errorf("missing project id")
	}
	if repoPath == "" {
		cwd, err := os.Getwd()
		if err != nil {
			return ResolvedContext{}, err
		}
		repoPath = cwd
	}
	repoRoot, err := findRepoRoot(repoPath)
	if err != nil {
		return ResolvedContext{}, err
	}
	return s.linkRepoAtRoot(workspaceID, projectID, repoID, repoName, repoRole, gitRemote, repoRoot)
}

func (s *Service) linkRepoAtRoot(workspaceID, projectID, repoID, repoName, repoRole, gitRemote, repoRoot string) (ResolvedContext, error) {
	if workspaceID == "" {
		workspaceID = "main"
	}
	if projectID == "" {
		return ResolvedContext{}, fmt.Errorf("missing project id")
	}
	repoRoot = cleanAbs(repoRoot)
	if repoName == "" {
		repoName = filepath.Base(repoRoot)
	}
	if repoID == "" {
		repoID = slugify(repoName)
	}
	if repoID == "" {
		return ResolvedContext{}, fmt.Errorf("missing repo id")
	}

	projectRoot, projectCfg, err := s.loadProjectConfig(workspaceID, projectID)
	if err != nil {
		return ResolvedContext{}, err
	}
	normalizeProjectConfig(&projectCfg)
	repoEntry := projectCfg.Repos[repoID]
	if repoName != "" {
		repoEntry.Name = repoName
	}
	if repoRole != "" {
		repoEntry.Role = repoRole
	}
	if gitRemote != "" {
		repoEntry.Git = gitRemote
	}
	projectCfg.Repos[repoID] = repoEntry
	component := projectCfg.Components[repoID]
	if component.ComponentID == "" {
		component = defaultComponentForRepo(repoID, repoEntry)
	}
	component.ComponentID = repoID
	component.Name = fallback(repoEntry.Name, component.Name)
	component.Role = fallback(repoEntry.Role, component.Role)
	if component.Kind == "" {
		component.Kind = inferComponentKind(repoID, repoEntry.Role)
	}
	if component.Location.Type == "" {
		component.Location = ProjectComponentLocation{Type: ComponentLocationRepo, RepoID: repoID}
	}
	if component.Location.Type == ComponentLocationRepo && component.Location.RepoID == "" {
		component.Location.RepoID = repoID
	}
	projectCfg.Components[repoID] = component
	if projectCfg.Topology == nil {
		projectCfg.Topology = &ProjectTopology{Mode: TopologySingleRepo}
	}
	if projectCfg.Topology.Mode != TopologyMonorepo {
		if len(projectCfg.Repos) > 1 {
			projectCfg.Topology.Mode = TopologyMultiRepo
			projectCfg.Topology.RootRepoID = ""
		} else {
			projectCfg.Topology.Mode = TopologySingleRepo
			projectCfg.Topology.RootRepoID = repoID
		}
	}
	if err := writeJSON(filepath.Join(projectRoot, "project.json"), projectCfg); err != nil {
		return ResolvedContext{}, err
	}
	if err := ensureRepoNote(filepath.Join(projectRoot, "kb", "repos.md"), repoID, repoEntry); err != nil {
		return ResolvedContext{}, err
	}

	local, err := s.LoadLocalConfig()
	if err != nil {
		return ResolvedContext{}, err
	}
	binding := local.Projects[projectID]
	binding.Workspace = workspaceID
	if binding.RepoPaths == nil {
		binding.RepoPaths = map[string]string{}
	}
	binding.RepoPaths[repoID] = cleanAbs(repoRoot)
	local.Projects[projectID] = binding
	if err := s.SaveLocalConfig(local); err != nil {
		return ResolvedContext{}, err
	}

	link := RepoLinkFile{Version: RepoLinkVersion, Workspace: workspaceID, ProjectID: projectID, RepoID: repoID}
	linkDir := filepath.Join(repoRoot, ".forge")
	if err := os.MkdirAll(filepath.Join(linkDir, "runs"), 0o755); err != nil {
		return ResolvedContext{}, err
	}
	if err := writeJSON(filepath.Join(linkDir, "project.json"), link); err != nil {
		return ResolvedContext{}, err
	}
	if err := writeFileIfMissing(filepath.Join(linkDir, "config.md"), defaultRepoConfig(projectID, repoID)); err != nil {
		return ResolvedContext{}, err
	}
	if err := writeFileIfMissing(filepath.Join(linkDir, ".gitignore"), "runs/\n"); err != nil {
		return ResolvedContext{}, err
	}

	return s.ResolveContext(repoRoot)
}

func (s *Service) ListRepos(workspaceID, projectID string) ([]RepoSummary, error) {
	if projectID == "" {
		return nil, fmt.Errorf("missing project id")
	}
	_, projectCfg, err := s.loadProjectConfig(workspaceID, projectID)
	if err != nil {
		return nil, err
	}
	local, err := s.LoadLocalConfig()
	if err != nil {
		return nil, err
	}
	binding := local.Projects[projectID]
	out := make([]RepoSummary, 0, len(projectCfg.Repos))
	for repoID, repo := range projectCfg.Repos {
		out = append(out, RepoSummary{Workspace: workspaceID, ProjectID: projectID, RepoID: repoID, Name: repo.Name, Role: repo.Role, Git: repo.Git, LocalPath: binding.RepoPaths[repoID]})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].RepoID < out[j].RepoID })
	return out, nil
}

func (s *Service) ResolveContext(cwd string) (ResolvedContext, error) {
	if cwd == "" {
		current, err := os.Getwd()
		if err != nil {
			return ResolvedContext{}, err
		}
		cwd = current
	}
	cwd = cleanAbs(cwd)

	local, err := s.LoadLocalConfig()
	if err != nil {
		return ResolvedContext{}, err
	}

	var link RepoLinkFile
	var repoRoot, linkPath string
	linkPath, err = findUp(cwd, filepath.Join(".forge", "project.json"))
	if err == nil && linkPath != "" {
		if err := readJSON(linkPath, &link); err != nil {
			return ResolvedContext{}, err
		}
		repoRoot = filepath.Dir(filepath.Dir(linkPath))
	}

	if link.ProjectID == "" {
		bestLen := -1
		for projectID, binding := range local.Projects {
			for repoID, repoPath := range binding.RepoPaths {
				repoPath = cleanAbs(repoPath)
				if !hasPathPrefix(cwd, repoPath) {
					continue
				}
				if len(repoPath) <= bestLen {
					continue
				}
				bestLen = len(repoPath)
				link = RepoLinkFile{Version: RepoLinkVersion, Workspace: binding.Workspace, ProjectID: projectID, RepoID: repoID}
				repoRoot = repoPath
			}
		}
	}

	if link.ProjectID == "" {
		return ResolvedContext{}, fmt.Errorf("no Forge project linked to %s", cwd)
	}
	if link.Workspace == "" {
		binding, ok := local.Projects[link.ProjectID]
		if !ok || binding.Workspace == "" {
			return ResolvedContext{}, fmt.Errorf("project %s is not bound to a workspace in local config", link.ProjectID)
		}
		link.Workspace = binding.Workspace
	}

	workspaceRoot, workspaceCfg, err := s.ensureWorkspace(link.Workspace)
	if err != nil {
		return ResolvedContext{}, err
	}
	projectRoot, projectCfg, err := s.loadProjectConfig(link.Workspace, link.ProjectID)
	if err != nil {
		return ResolvedContext{}, err
	}
	binding := local.Projects[link.ProjectID]
	if repoRoot == "" {
		repoRoot = cleanAbs(binding.RepoPaths[link.RepoID])
	}
	projectRepos := make([]RepoSummary, 0, len(projectCfg.Repos))
	for repoID, repo := range projectCfg.Repos {
		projectRepos = append(projectRepos, RepoSummary{Workspace: link.Workspace, ProjectID: link.ProjectID, RepoID: repoID, Name: repo.Name, Role: repo.Role, Git: repo.Git, LocalPath: cleanAbs(binding.RepoPaths[repoID])})
	}
	sort.Slice(projectRepos, func(i, j int) bool { return projectRepos[i].RepoID < projectRepos[j].RepoID })

	ctx := ResolvedContext{
		ForgeHome:       s.ForgeHome,
		Workspace:       link.Workspace,
		WorkspaceRoot:   workspaceRoot,
		WorkspaceShared: filepath.Join(workspaceRoot, workspaceCfg.SharedDir),
		ProjectID:       link.ProjectID,
		ProjectName:     projectCfg.ProjectName,
		ProjectRoot:     projectRoot,
		RepoID:          link.RepoID,
		KBDir:           filepath.Join(projectRoot, "kb"),
		SpecsDir:        filepath.Join(projectRoot, "specs"),
		ReportsDir:      filepath.Join(projectRoot, "reports"),
		InboxFile:       filepath.Join(projectRoot, "inbox.md"),
		ProjectConfig:   filepath.Join(projectRoot, "project.json"),
		RepoLinkFile:    linkPath,
		Repos:           projectRepos,
	}
	if repoRoot != "" {
		ctx.RepoRoot = repoRoot
		ctx.RepoLocalForge = filepath.Join(repoRoot, ".forge")
	}
	return ctx, nil
}

func (s *Service) PIInstall(packagePath string) (PIInstallStatus, error) {
	if packagePath == "" {
		cwd, err := os.Getwd()
		if err != nil {
			return PIInstallStatus{}, err
		}
		packagePath = cwd
	}
	packagePath = cleanAbs(packagePath)
	settingsPath, err := piSettingsPath()
	if err != nil {
		return PIInstallStatus{}, err
	}
	state, err := readOrCreateJSONObject(settingsPath)
	if err != nil {
		return PIInstallStatus{}, err
	}
	packages := readStringArray(state, "packages")
	if !containsResolvedPath(packages, packagePath, filepath.Dir(settingsPath)) {
		packages = append(packages, packagePath)
		state["packages"] = packages
		if err := writeJSON(settingsPath, state); err != nil {
			return PIInstallStatus{}, err
		}
	}
	return PIInstallStatus{SettingsPath: settingsPath, PackagePath: packagePath, Installed: true}, nil
}

func (s *Service) PIUninstall(packagePath string) (PIInstallStatus, error) {
	if packagePath == "" {
		cwd, err := os.Getwd()
		if err != nil {
			return PIInstallStatus{}, err
		}
		packagePath = cwd
	}
	packagePath = cleanAbs(packagePath)
	settingsPath, err := piSettingsPath()
	if err != nil {
		return PIInstallStatus{}, err
	}
	state, err := readOrCreateJSONObject(settingsPath)
	if err != nil {
		return PIInstallStatus{}, err
	}
	packages := readStringArray(state, "packages")
	filtered := make([]string, 0, len(packages))
	for _, entry := range packages {
		if cleanMaybeRelative(entry, filepath.Dir(settingsPath)) == packagePath {
			continue
		}
		filtered = append(filtered, entry)
	}
	state["packages"] = filtered
	if err := writeJSON(settingsPath, state); err != nil {
		return PIInstallStatus{}, err
	}
	return PIInstallStatus{SettingsPath: settingsPath, PackagePath: packagePath, Installed: false}, nil
}

func (s *Service) PIStatus(packagePath string) (PIInstallStatus, error) {
	if packagePath == "" {
		cwd, err := os.Getwd()
		if err != nil {
			return PIInstallStatus{}, err
		}
		packagePath = cwd
	}
	packagePath = cleanAbs(packagePath)
	settingsPath, err := piSettingsPath()
	if err != nil {
		return PIInstallStatus{}, err
	}
	state, err := readOrCreateJSONObject(settingsPath)
	if err != nil {
		return PIInstallStatus{}, err
	}
	packages := readStringArray(state, "packages")
	return PIInstallStatus{SettingsPath: settingsPath, PackagePath: packagePath, Installed: containsResolvedPath(packages, packagePath, filepath.Dir(settingsPath))}, nil
}

func (s *Service) ensureWorkspace(workspaceID string) (string, WorkspaceConfig, error) {
	root, err := s.resolveWorkspaceRoot(workspaceID)
	if err == nil {
		cfg, cfgErr := s.LoadWorkspaceConfig(workspaceID)
		return root, cfg, cfgErr
	}
	summary, err := s.InitWorkspace(workspaceID, workspaceID, "")
	if err != nil {
		return "", WorkspaceConfig{}, err
	}
	cfg, err := s.LoadWorkspaceConfig(workspaceID)
	return summary.Root, cfg, err
}

func (s *Service) resolveWorkspaceRoot(workspaceID string) (string, error) {
	local, err := s.LoadLocalConfig()
	if err != nil {
		return "", err
	}
	if binding, ok := local.Workspaces[workspaceID]; ok && binding.Root != "" {
		return cleanAbs(binding.Root), nil
	}
	candidate := defaultWorkspaceRoot(s.ForgeHome, workspaceID)
	if _, err := os.Stat(filepath.Join(candidate, "workspace.json")); err == nil {
		return candidate, nil
	}
	return "", fmt.Errorf("workspace %s not found", workspaceID)
}

func (s *Service) loadProjectConfig(workspaceID, projectID string) (string, ProjectConfig, error) {
	workspaceRoot, workspaceCfg, err := s.ensureWorkspace(workspaceID)
	if err != nil {
		return "", ProjectConfig{}, err
	}
	projectRoot := filepath.Join(workspaceRoot, workspaceCfg.ProjectsDir, projectID)
	var cfg ProjectConfig
	if err := readJSON(filepath.Join(projectRoot, "project.json"), &cfg); err != nil {
		return "", ProjectConfig{}, err
	}
	normalizeProjectConfig(&cfg)
	return projectRoot, cfg, nil
}

func cleanAbs(input string) string {
	if input == "" {
		return ""
	}
	if strings.HasPrefix(input, "~/") {
		home, err := os.UserHomeDir()
		if err == nil {
			input = filepath.Join(home, strings.TrimPrefix(input, "~/"))
		}
	}
	abs, err := filepath.Abs(input)
	if err != nil {
		return filepath.Clean(input)
	}
	return filepath.Clean(abs)
}

func hasPathPrefix(target, prefix string) bool {
	target = filepath.Clean(target)
	prefix = filepath.Clean(prefix)
	if prefix == "" || prefix == "." {
		return false
	}
	if target == prefix {
		return true
	}
	return strings.HasPrefix(target, prefix+string(os.PathSeparator))
}

func findRepoRoot(start string) (string, error) {
	start = cleanAbs(start)
	if info, err := os.Stat(start); err == nil && !info.IsDir() {
		start = filepath.Dir(start)
	}
	current := start
	for {
		if _, err := os.Stat(filepath.Join(current, ".git")); err == nil {
			return current, nil
		}
		parent := filepath.Dir(current)
		if parent == current {
			return start, nil
		}
		current = parent
	}
}

func findUp(start, relative string) (string, error) {
	start = cleanAbs(start)
	if info, err := os.Stat(start); err == nil && !info.IsDir() {
		start = filepath.Dir(start)
	}
	current := start
	for {
		candidate := filepath.Join(current, relative)
		if _, err := os.Stat(candidate); err == nil {
			return candidate, nil
		}
		parent := filepath.Dir(current)
		if parent == current {
			return "", os.ErrNotExist
		}
		current = parent
	}
}

func slugify(input string) string {
	input = strings.ToLower(strings.TrimSpace(input))
	if input == "" {
		return ""
	}
	var b strings.Builder
	prevDash := false
	for _, r := range input {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			prevDash = false
		default:
			if !prevDash {
				b.WriteByte('-')
				prevDash = true
			}
		}
	}
	out := strings.Trim(b.String(), "-")
	return strings.ReplaceAll(out, "--", "-")
}

func ensureProjectFiles(projectRoot, projectName string) error {
	if err := os.MkdirAll(filepath.Join(projectRoot, "kb"), 0o755); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Join(projectRoot, "specs"), 0o755); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Join(projectRoot, "reports"), 0o755); err != nil {
		return err
	}
	files := map[string]string{
		filepath.Join(projectRoot, "kb", "architecture.md"): defaultArchitecture(projectName),
		filepath.Join(projectRoot, "kb", "business.md"):     defaultBusiness(projectName),
		filepath.Join(projectRoot, "kb", "roadmap.md"):      defaultRoadmap(projectName),
		filepath.Join(projectRoot, "kb", "repos.md"):        defaultRepos(projectName),
		filepath.Join(projectRoot, "inbox.md"):              defaultInbox(),
		filepath.Join(projectRoot, "config.md"):             defaultProjectConfig(),
		filepath.Join(projectRoot, "constraints.md"):        defaultConstraints(),
	}
	for path, content := range files {
		if err := writeFileIfMissing(path, content); err != nil {
			return err
		}
	}
	return nil
}

func ensureRepoNote(path, repoID string, repo ProjectRepo) error {
	content, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	text := string(content)
	heading := fmt.Sprintf("## %s", repoID)
	if strings.Contains(text, heading+"\n") || strings.HasSuffix(strings.TrimSpace(text), heading) {
		return nil
	}
	block := fmt.Sprintf("\n## %s\n- **Name:** %s\n- **Role:** %s\n- **Owns:** Fill in the main surfaces this repo owns.\n- **Depends on:** Fill in important upstream repos/services.\n", repoID, fallback(repo.Name, repoID), fallback(repo.Role, "Not specified yet"))
	return os.WriteFile(path, []byte(strings.TrimRight(text, "\n")+block+"\n"), 0o644)
}

func defaultArchitecture(projectName string) string {
	return fmt.Sprintf("# Architecture — %s\n\n## Stack\n- Fill in the main frameworks, runtime, database, infra, and deployment targets.\n\n## Constraints\n- List technical rules that every repo should follow.\n", projectName)
}

func defaultBusiness(projectName string) string {
	return fmt.Sprintf("# Business — %s\n\n## Product Vision\n- What are we building and for whom?\n\n## Core Rules\n- List the business rules agents must respect.\n", projectName)
}

func defaultRoadmap(projectName string) string {
	return fmt.Sprintf("# Roadmap — %s\n\n## Now\n- \n\n## Next\n- \n\n## Later\n- \n\n## Vision\n- \n", projectName)
}

func defaultRepos(projectName string) string {
	return fmt.Sprintf("# Repositories — %s\n\nUse this document to describe what each repository owns inside the project.\n", projectName)
}

func defaultInbox() string {
	return "# Inbox\n\nCapture rough ideas and future explorations here.\n\n## Proposed memory\n\nAgents propose additions to `kb/memory.md` here. Review periodically with `forge memory review` and promote approved items.\n"
}

func defaultConstraints() string {
	return `# Forge Constraints (non-negotiable)

These rules apply to every Forge agent spawned in this project. Agents must follow them regardless of task instructions.

## Git
- NEVER force push
- NEVER push directly to main or master
- Always commit to the task's branch inside the worktree
- Always create a PR via ` + "`gh pr create`" + ` before exiting

## Security
- NEVER commit secrets (API keys, passwords, tokens, private keys)
- NEVER run destructive commands outside the worktree (rm -rf, drop table, truncate)
- NEVER access files outside the worktree or its declared ` + "`touches:`" + ` metadata

## Quality
- Always run the project's lint command before declaring the task done
- Always ensure tests pass
- Never leave commented-out dead code in the diff

## Escalation
- If blocked, write an event to $FORGE_EVENTS_FILE with type "blocked" and exit
- If you need operator input, write an event with type "need_info" and exit
- Do not loop indefinitely on a task you cannot complete
`
}

func defaultProjectConfig() string {
	return "# Forge Config\n<!-- auto-advance: confirm -->\n\n## Agents\n\n### default\n<!-- role: spec,execute -->\nYou are a senior software engineer. Analyze the codebase, understand existing patterns, and implement clean, tested code.\n"
}

func defaultRepoConfig(projectID, repoID string) string {
	return fmt.Sprintf("# Forge Repo Config — %s\n<!-- project: %s -->\n\nOptional repo-specific overrides live here.\n", repoID, projectID)
}

// safeJoin joins base and child paths and verifies the result stays inside
// the base directory. Prevents path traversal attacks (e.g. child = "../../etc/passwd").
func safeJoin(base, child string) (string, error) {
	joined := filepath.Join(base, child)
	absBase, err := filepath.Abs(base)
	if err != nil {
		return "", fmt.Errorf("invalid base path: %w", err)
	}
	absJoined, err := filepath.Abs(joined)
	if err != nil {
		return "", fmt.Errorf("invalid path: %w", err)
	}
	if !strings.HasPrefix(absJoined, absBase+string(filepath.Separator)) && absJoined != absBase {
		return "", fmt.Errorf("path escapes base directory")
	}
	return absJoined, nil
}

func writeFileIfMissing(path, content string) error {
	if _, err := os.Stat(path); err == nil {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(content), 0o644)
}

func readJSON(path string, into any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, into)
}

func writeJSON(path string, value any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return os.WriteFile(path, data, 0o644)
}

func fallback(value, defaultValue string) string {
	if strings.TrimSpace(value) == "" {
		return defaultValue
	}
	return value
}

func piSettingsPath() (string, error) {
	if dir := os.Getenv("PI_CODING_AGENT_DIR"); dir != "" {
		return filepath.Join(cleanAbs(dir), "settings.json"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".pi", "agent", "settings.json"), nil
}

func readOrCreateJSONObject(path string) (map[string]any, error) {
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		return map[string]any{}, nil
	}
	var value map[string]any
	if err := readJSON(path, &value); err != nil {
		return nil, err
	}
	if value == nil {
		value = map[string]any{}
	}
	return value, nil
}

func readStringArray(state map[string]any, key string) []string {
	raw, ok := state[key]
	if !ok {
		return []string{}
	}
	items, ok := raw.([]any)
	if !ok {
		return []string{}
	}
	out := make([]string, 0, len(items))
	for _, item := range items {
		if str, ok := item.(string); ok {
			out = append(out, str)
		}
	}
	return out
}

func containsResolvedPath(entries []string, target, baseDir string) bool {
	for _, entry := range entries {
		if cleanMaybeRelative(entry, baseDir) == target {
			return true
		}
	}
	return false
}

func cleanMaybeRelative(value, baseDir string) string {
	if value == "" {
		return ""
	}
	if filepath.IsAbs(value) {
		return cleanAbs(value)
	}
	return cleanAbs(filepath.Join(baseDir, value))
}
