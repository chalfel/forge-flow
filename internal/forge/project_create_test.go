package forge

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPlanProjectCreateBlankSingleRepo(t *testing.T) {
	home := t.TempDir()
	cwd := filepath.Join(home, "code")
	mustMkdirAll(t, cwd)
	svc := &Service{ForgeHome: filepath.Join(home, ".forge")}

	plan, err := svc.PlanProjectCreate(ProjectCreateInput{CWD: cwd, ProjectName: "Acme Platform", Pack: "blank"})
	if err != nil {
		t.Fatalf("PlanProjectCreate: %v", err)
	}
	if plan.ProjectID != "acme-platform" {
		t.Fatalf("unexpected project id: %+v", plan)
	}
	if plan.Topology.Mode != TopologySingleRepo || plan.Topology.RootRepoID != "app" {
		t.Fatalf("unexpected topology: %+v", plan.Topology)
	}
	if len(plan.Repos) != 1 || plan.Repos[0].RepoID != "app" {
		t.Fatalf("expected one root repo, got %+v", plan.Repos)
	}
	if plan.Repos[0].Path != filepath.Join(cwd, "acme-platform") {
		t.Fatalf("unexpected repo path: %+v", plan.Repos[0])
	}
	if len(plan.Components) == 0 || plan.Components[0].ComponentID != "app" {
		t.Fatalf("expected implicit app component, got %+v", plan.Components)
	}
}

func TestPlanProjectCreateNextJSMonorepo(t *testing.T) {
	home := t.TempDir()
	cwd := filepath.Join(home, "code")
	mustMkdirAll(t, cwd)
	svc := &Service{ForgeHome: filepath.Join(home, ".forge")}

	plan, err := svc.PlanProjectCreate(ProjectCreateInput{CWD: cwd, ProjectName: "Acme Platform", Pack: "nextjs-saas", Topology: "monorepo"})
	if err != nil {
		t.Fatalf("PlanProjectCreate: %v", err)
	}
	if plan.Topology.Mode != TopologyMonorepo || plan.Topology.RootRepoID != "platform" {
		t.Fatalf("unexpected topology: %+v", plan.Topology)
	}
	if len(plan.Repos) != 1 || plan.Repos[0].RepoID != "platform" {
		t.Fatalf("expected monorepo root repo, got %+v", plan.Repos)
	}
	foundWeb := false
	for _, component := range plan.Components {
		if component.ComponentID == "web" {
			foundWeb = true
			if component.Location.Type != ComponentLocationPath || component.Location.Path != "apps/web" {
				t.Fatalf("unexpected web component location: %+v", component.Location)
			}
		}
	}
	if !foundWeb {
		t.Fatalf("expected web component in plan: %+v", plan.Components)
	}
}

func TestApplyProjectCreateNextJSSingleRepo(t *testing.T) {
	home := t.TempDir()
	cwd := filepath.Join(home, "code")
	mustMkdirAll(t, cwd)
	svc := &Service{ForgeHome: filepath.Join(home, ".forge")}
	target := filepath.Join(cwd, "acme-platform")

	plan, err := svc.ApplyProjectCreate(ProjectCreateInput{CWD: cwd, ProjectName: "Acme Platform", Pack: "nextjs-saas", Path: target})
	if err != nil {
		t.Fatalf("ApplyProjectCreate: %v", err)
	}
	if !plan.Updated {
		t.Fatalf("expected updated plan")
	}
	projectRoot, cfg, err := svc.loadProjectConfig("main", "acme-platform")
	if err != nil {
		t.Fatalf("loadProjectConfig: %v", err)
	}
	if cfg.Starter == nil || cfg.Starter.PackID != "nextjs-saas" {
		t.Fatalf("expected starter metadata, got %+v", cfg.Starter)
	}
	if cfg.Topology == nil || cfg.Topology.Mode != TopologySingleRepo || cfg.Topology.RootRepoID != "web" {
		t.Fatalf("unexpected topology: %+v", cfg.Topology)
	}
	if _, ok := cfg.Repos["web"]; !ok {
		t.Fatalf("expected web repo in config: %+v", cfg.Repos)
	}
	web := cfg.Components["web"]
	if web.Stack != "nextjs" || web.Location.Type != ComponentLocationRepo || web.Location.RepoID != "web" {
		t.Fatalf("unexpected web component: %+v", web)
	}
	if _, err := os.Stat(filepath.Join(target, ".forge", "project.json")); err != nil {
		t.Fatalf("expected repo-local forge binding: %v", err)
	}
	if _, err := os.Stat(filepath.Join(target, "package.json")); err != nil {
		t.Fatalf("expected starter package.json: %v", err)
	}
	if _, err := os.Stat(filepath.Join(target, "app", "page.tsx")); err != nil {
		t.Fatalf("expected starter app/page.tsx: %v", err)
	}
	architecture, err := os.ReadFile(filepath.Join(projectRoot, "kb", "architecture.md"))
	if err != nil {
		t.Fatalf("ReadFile architecture: %v", err)
	}
	if !containsText(string(architecture), "Starter topology") {
		t.Fatalf("expected architecture starter section, got %s", string(architecture))
	}
	config, err := os.ReadFile(filepath.Join(projectRoot, "config.md"))
	if err != nil {
		t.Fatalf("ReadFile config: %v", err)
	}
	if !containsText(string(config), "Quality defaults") {
		t.Fatalf("expected quality defaults section, got %s", string(config))
	}
	resolved, err := svc.ResolveContext(target)
	if err != nil {
		t.Fatalf("ResolveContext: %v", err)
	}
	if resolved.RepoID != "web" || resolved.ProjectID != "acme-platform" {
		t.Fatalf("unexpected resolved context: %+v", resolved)
	}
}

func TestApplyProjectCreateBlankMultiRepo(t *testing.T) {
	home := t.TempDir()
	cwd := filepath.Join(home, "code")
	mustMkdirAll(t, cwd)
	svc := &Service{ForgeHome: filepath.Join(home, ".forge")}

	plan, err := svc.ApplyProjectCreate(ProjectCreateInput{CWD: cwd, ProjectName: "Acme Platform", Pack: "blank", Topology: "multi-repo"})
	if err != nil {
		t.Fatalf("ApplyProjectCreate: %v", err)
	}
	if !plan.Updated {
		t.Fatalf("expected updated plan")
	}
	_, cfg, err := svc.loadProjectConfig("main", "acme-platform")
	if err != nil {
		t.Fatalf("loadProjectConfig: %v", err)
	}
	if cfg.Topology == nil || cfg.Topology.Mode != TopologyMultiRepo {
		t.Fatalf("expected multi-repo topology, got %+v", cfg.Topology)
	}
	if len(cfg.Repos) != 0 {
		t.Fatalf("expected no starter repos for blank multi-repo, got %+v", cfg.Repos)
	}
}

func containsText(text, needle string) bool {
	return strings.Contains(text, needle)
}

// ---------------------------------------------------------------------------
// Existing mode tests
// ---------------------------------------------------------------------------

func TestPlanProjectCreateExisting(t *testing.T) {
	home := t.TempDir()
	parent := filepath.Join(home, "projects", "Floki")
	cardAPI := filepath.Join(parent, "card-api")
	cardApp := filepath.Join(parent, "card-app")
	mustMkdirAll(t, cardAPI)
	mustMkdirAll(t, cardApp)
	svc := &Service{ForgeHome: filepath.Join(home, ".forge")}

	plan, err := svc.PlanProjectCreateExisting(ProjectCreateExistingInput{
		CWD:         parent,
		ProjectName: "Floki Card",
		Repos: []ExistingRepoRef{
			{Path: cardAPI},
			{Path: cardApp},
		},
	})
	if err != nil {
		t.Fatalf("PlanProjectCreateExisting: %v", err)
	}
	if plan.ProjectID != "floki-card" {
		t.Errorf("projectId = %q, want floki-card", plan.ProjectID)
	}
	if plan.Topology.Mode != TopologyMultiRepo {
		t.Errorf("topology = %q, want multi-repo", plan.Topology.Mode)
	}
	if len(plan.Repos) != 2 {
		t.Fatalf("repos = %d, want 2", len(plan.Repos))
	}
	if plan.Repos[0].RepoID != "card-api" || plan.Repos[1].RepoID != "card-app" {
		t.Errorf("unexpected repo IDs: %v, %v", plan.Repos[0].RepoID, plan.Repos[1].RepoID)
	}
	if plan.Updated {
		t.Error("plan should not be marked as updated")
	}
}

func TestPlanProjectCreateExistingSingleRepo(t *testing.T) {
	home := t.TempDir()
	parent := filepath.Join(home, "projects")
	web := filepath.Join(parent, "web")
	mustMkdirAll(t, web)
	svc := &Service{ForgeHome: filepath.Join(home, ".forge")}

	plan, err := svc.PlanProjectCreateExisting(ProjectCreateExistingInput{
		CWD:         parent,
		ProjectName: "Floki Web",
		Repos:       []ExistingRepoRef{{Path: web}},
	})
	if err != nil {
		t.Fatalf("PlanProjectCreateExisting: %v", err)
	}
	if plan.Topology.Mode != TopologySingleRepo {
		t.Errorf("topology = %q, want single-repo", plan.Topology.Mode)
	}
	if plan.Topology.RootRepoID != "web" {
		t.Errorf("rootRepoID = %q, want web", plan.Topology.RootRepoID)
	}
}

func TestApplyProjectCreateExisting(t *testing.T) {
	home := t.TempDir()
	parent := filepath.Join(home, "projects", "Floki")
	cardAPI := filepath.Join(parent, "card-api")
	cardApp := filepath.Join(parent, "card-app")
	mustMkdirAll(t, cardAPI)
	mustMkdirAll(t, cardApp)
	svc := &Service{ForgeHome: filepath.Join(home, ".forge")}

	plan, err := svc.ApplyProjectCreateExisting(ProjectCreateExistingInput{
		CWD:         parent,
		ProjectName: "Floki Card",
		Repos: []ExistingRepoRef{
			{Path: cardAPI},
			{Path: cardApp},
		},
		SetupDev: false,
	})
	if err != nil {
		t.Fatalf("ApplyProjectCreateExisting: %v", err)
	}
	if !plan.Updated {
		t.Error("expected updated = true after apply")
	}

	// verify project.json exists
	_, cfg, err := svc.loadProjectConfig("main", "floki-card")
	if err != nil {
		t.Fatalf("loadProjectConfig: %v", err)
	}
	if cfg.Topology == nil || cfg.Topology.Mode != TopologyMultiRepo {
		t.Fatalf("unexpected topology: %+v", cfg.Topology)
	}
	if _, ok := cfg.Repos["card-api"]; !ok {
		t.Error("expected card-api repo in config")
	}
	if _, ok := cfg.Repos["card-app"]; !ok {
		t.Error("expected card-app repo in config")
	}

	// verify .forge/project.json in each repo
	for _, dir := range []string{cardAPI, cardApp} {
		link := filepath.Join(dir, ".forge", "project.json")
		if _, err := os.Stat(link); err != nil {
			t.Errorf("expected .forge/project.json in %s: %v", dir, err)
		}
	}

	// verify components
	if comp, ok := cfg.Components["card-api"]; !ok {
		t.Error("expected card-api component")
	} else if comp.Location.Type != ComponentLocationRepo {
		t.Errorf("expected repo location, got %q", comp.Location.Type)
	}
}

func TestApplyProjectCreateExistingWithDevSetup(t *testing.T) {
	home := t.TempDir()
	parent := filepath.Join(home, "projects", "MyApp")
	api := filepath.Join(parent, "api")
	web := filepath.Join(parent, "web")
	mustMkdirAll(t, api)
	mustMkdirAll(t, web)

	// create package.json with dev scripts so detection finds services
	os.WriteFile(filepath.Join(api, "package.json"), []byte(`{"scripts":{"dev":"node server.js"}}`), 0o644)
	os.WriteFile(filepath.Join(web, "package.json"), []byte(`{"scripts":{"dev":"next dev"}}`), 0o644)

	// override dev config dir to temp
	origConfig := os.Getenv("FORGE_DEV_CONFIG")
	os.Setenv("FORGE_DEV_CONFIG", filepath.Join(home, ".config", "forge-dev"))
	defer os.Setenv("FORGE_DEV_CONFIG", origConfig)

	svc := &Service{ForgeHome: filepath.Join(home, ".forge")}

	plan, err := svc.ApplyProjectCreateExisting(ProjectCreateExistingInput{
		CWD:         parent,
		ProjectName: "MyApp",
		Repos: []ExistingRepoRef{
			{Path: api},
			{Path: web},
		},
		SetupDev: true,
	})
	if err != nil {
		t.Fatalf("ApplyProjectCreateExisting: %v", err)
	}
	if plan.DevConfigPath == "" {
		t.Error("expected DevConfigPath to be set")
	}
	if _, err := os.Stat(plan.DevConfigPath); err != nil {
		t.Errorf("dev config file not found: %v", err)
	}
}

func TestApplyProjectCreateExistingPathNotFound(t *testing.T) {
	home := t.TempDir()
	svc := &Service{ForgeHome: filepath.Join(home, ".forge")}

	_, err := svc.PlanProjectCreateExisting(ProjectCreateExistingInput{
		CWD:         home,
		ProjectName: "Bad",
		Repos:       []ExistingRepoRef{{Path: filepath.Join(home, "nonexistent")}},
	})
	if err == nil {
		t.Fatal("expected error for nonexistent path")
	}
	if !strings.Contains(err.Error(), "does not exist") {
		t.Errorf("unexpected error: %v", err)
	}
}
