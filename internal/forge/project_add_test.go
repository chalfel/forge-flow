package forge

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPlanProjectAddMultiRepoDefaultsToRepo(t *testing.T) {
	home := t.TempDir()
	svc := &Service{ForgeHome: filepath.Join(home, ".forge")}
	if _, err := svc.InitWorkspace("main", "Main Workspace", ""); err != nil {
		t.Fatalf("InitWorkspace: %v", err)
	}
	if _, err := svc.InitProject("main", "acme-platform", "Acme Platform"); err != nil {
		t.Fatalf("InitProject: %v", err)
	}
	webRepo := filepath.Join(home, "code", "acme-web")
	mustMkdirAll(t, filepath.Join(webRepo, "src"))
	if _, err := svc.LinkRepo("main", "acme-platform", "web", "acme-web", "customer-facing web app", "", webRepo); err != nil {
		t.Fatalf("LinkRepo: %v", err)
	}
	ctx, err := svc.ResolveContext(webRepo)
	if err != nil {
		t.Fatalf("ResolveContext: %v", err)
	}

	projectRoot, cfg, err := svc.loadProjectConfig(ctx.Workspace, ctx.ProjectID)
	if err != nil {
		t.Fatalf("loadProjectConfig: %v", err)
	}
	cfg.Topology = &ProjectTopology{Mode: TopologyMultiRepo}
	if err := writeJSON(filepath.Join(projectRoot, "project.json"), cfg); err != nil {
		t.Fatalf("writeJSON: %v", err)
	}

	plan, err := svc.PlanProjectAdd(ProjectAddInput{CWD: webRepo, Kind: "api"})
	if err != nil {
		t.Fatalf("PlanProjectAdd: %v", err)
	}
	if plan.As != ComponentLocationRepo {
		t.Fatalf("expected repo host, got %+v", plan)
	}
	if plan.Repo == nil || plan.Repo.RepoID != "api" {
		t.Fatalf("expected repo plan for api, got %+v", plan.Repo)
	}
	if plan.ResultingTopology.Mode != TopologyMultiRepo {
		t.Fatalf("expected multi-repo result, got %+v", plan.ResultingTopology)
	}
	if plan.Component.Location.Type != ComponentLocationRepo {
		t.Fatalf("expected repo-backed component, got %+v", plan.Component.Location)
	}
}

func TestPlanProjectAddMonorepoCreatesAppPath(t *testing.T) {
	home := t.TempDir()
	svc := &Service{ForgeHome: filepath.Join(home, ".forge")}
	if _, err := svc.InitWorkspace("main", "Main Workspace", ""); err != nil {
		t.Fatalf("InitWorkspace: %v", err)
	}
	if _, err := svc.InitProject("main", "acme-platform", "Acme Platform"); err != nil {
		t.Fatalf("InitProject: %v", err)
	}
	repoRoot := filepath.Join(home, "code", "acme-platform")
	mustMkdirAll(t, filepath.Join(repoRoot, "apps", "web"))
	if _, err := svc.LinkRepo("main", "acme-platform", "platform", "acme-platform", "monorepo root", "", repoRoot); err != nil {
		t.Fatalf("LinkRepo: %v", err)
	}
	projectRoot, cfg, err := svc.loadProjectConfig("main", "acme-platform")
	if err != nil {
		t.Fatalf("loadProjectConfig: %v", err)
	}
	cfg.Topology = &ProjectTopology{Mode: TopologyMonorepo, RootRepoID: "platform"}
	cfg.Components["web"] = ProjectComponent{ComponentID: "web", Name: "web", Kind: "web", Role: "customer-facing web app", Location: ProjectComponentLocation{Type: ComponentLocationPath, RepoID: "platform", Path: "apps/web"}}
	if err := writeJSON(filepath.Join(projectRoot, "project.json"), cfg); err != nil {
		t.Fatalf("writeJSON: %v", err)
	}

	plan, err := svc.PlanProjectAdd(ProjectAddInput{CWD: repoRoot, Kind: "admin"})
	if err != nil {
		t.Fatalf("PlanProjectAdd: %v", err)
	}
	if plan.As != ComponentLocationPath {
		t.Fatalf("expected path host for monorepo app, got %+v", plan)
	}
	if plan.Component.Location.RepoID != "platform" || plan.Component.Location.Path != "apps/admin" {
		t.Fatalf("unexpected component location: %+v", plan.Component.Location)
	}
	if plan.ResultingTopology.Mode != TopologyMonorepo {
		t.Fatalf("expected monorepo result, got %+v", plan.ResultingTopology)
	}
}

func TestPlanProjectAddSingleRepoRequiresChoice(t *testing.T) {
	svc, webRepo, _ := setupSpecTestProject(t)
	ctx, err := svc.ResolveContext(webRepo)
	if err != nil {
		t.Fatalf("ResolveContext: %v", err)
	}
	projectRoot, cfg, err := svc.loadProjectConfig(ctx.Workspace, ctx.ProjectID)
	if err != nil {
		t.Fatalf("loadProjectConfig: %v", err)
	}
	cfg.Topology = &ProjectTopology{Mode: TopologySingleRepo, RootRepoID: "web"}
	cfg.Repos = map[string]ProjectRepo{"web": cfg.Repos["web"]}
	cfg.Components = map[string]ProjectComponent{"web": cfg.Components["web"]}
	if err := writeJSON(filepath.Join(projectRoot, "project.json"), cfg); err != nil {
		t.Fatalf("writeJSON: %v", err)
	}

	plan, err := svc.PlanProjectAdd(ProjectAddInput{CWD: webRepo, Kind: "api"})
	if err != nil {
		t.Fatalf("PlanProjectAdd: %v", err)
	}
	if !plan.RequiresChoice {
		t.Fatalf("expected single-repo add to require a choice, got %+v", plan)
	}
	if len(plan.Choices) == 0 {
		t.Fatalf("expected choices, got %+v", plan)
	}
}

func TestApplyProjectAddRegistersRepoAndComponent(t *testing.T) {
	svc, webRepo, _ := setupSpecTestProject(t)
	ctx, err := svc.ResolveContext(webRepo)
	if err != nil {
		t.Fatalf("ResolveContext: %v", err)
	}
	projectRoot, cfg, err := svc.loadProjectConfig(ctx.Workspace, ctx.ProjectID)
	if err != nil {
		t.Fatalf("loadProjectConfig: %v", err)
	}
	cfg.Topology = &ProjectTopology{Mode: TopologyMultiRepo}
	if err := writeJSON(filepath.Join(projectRoot, "project.json"), cfg); err != nil {
		t.Fatalf("writeJSON: %v", err)
	}
	targetPath := filepath.Join(filepath.Dir(webRepo), "acme-worker")

	plan, err := svc.ApplyProjectAdd(ProjectAddInput{CWD: webRepo, Kind: "worker", As: "repo", RepoID: "worker", RepoName: "acme-worker", Path: targetPath})
	if err != nil {
		t.Fatalf("ApplyProjectAdd: %v", err)
	}
	if !plan.Updated {
		t.Fatalf("expected updated plan")
	}
	if _, err := os.Stat(targetPath); err != nil {
		t.Fatalf("expected repo path to exist: %v", err)
	}
	_, updated, err := svc.loadProjectConfig(ctx.Workspace, ctx.ProjectID)
	if err != nil {
		t.Fatalf("loadProjectConfig: %v", err)
	}
	if _, ok := updated.Repos["worker"]; !ok {
		t.Fatalf("expected worker repo in project config: %+v", updated.Repos)
	}
	component, ok := updated.Components["worker"]
	if !ok {
		t.Fatalf("expected worker component in project config: %+v", updated.Components)
	}
	if component.Location.Type != ComponentLocationRepo || component.Location.RepoID != "worker" {
		t.Fatalf("unexpected worker component location: %+v", component.Location)
	}
	local, err := svc.LoadLocalConfig()
	if err != nil {
		t.Fatalf("LoadLocalConfig: %v", err)
	}
	if local.Projects[ctx.ProjectID].RepoPaths["worker"] != targetPath {
		t.Fatalf("expected local binding for worker repo, got %+v", local.Projects[ctx.ProjectID].RepoPaths)
	}
}
