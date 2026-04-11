package forge

import "testing"

func TestNormalizeProjectConfigInfersSingleRepoTopology(t *testing.T) {
	cfg := ProjectConfig{
		ProjectID:   "forge",
		ProjectName: "Forge",
		Repos: map[string]ProjectRepo{
			"core": {Name: "forge-skills", Role: "core runtime"},
		},
	}

	normalizeProjectConfig(&cfg)

	if cfg.Topology == nil || cfg.Topology.Mode != TopologySingleRepo {
		t.Fatalf("expected single-repo topology, got %+v", cfg.Topology)
	}
	if cfg.Topology.RootRepoID != "core" {
		t.Fatalf("expected root repo core, got %+v", cfg.Topology)
	}
	component, ok := cfg.Components["core"]
	if !ok {
		t.Fatalf("expected default component for core repo")
	}
	if component.Location.Type != ComponentLocationRepo || component.Location.RepoID != "core" {
		t.Fatalf("unexpected component location: %+v", component.Location)
	}
}

func TestNormalizeProjectConfigInfersMultiRepoTopology(t *testing.T) {
	cfg := ProjectConfig{
		ProjectID:   "acme",
		ProjectName: "Acme",
		Repos: map[string]ProjectRepo{
			"web": {Name: "acme-web", Role: "web app"},
			"api": {Name: "acme-api", Role: "api"},
		},
	}

	normalizeProjectConfig(&cfg)

	if cfg.Topology == nil || cfg.Topology.Mode != TopologyMultiRepo {
		t.Fatalf("expected multi-repo topology, got %+v", cfg.Topology)
	}
	if cfg.Topology.RootRepoID != "" {
		t.Fatalf("expected empty root repo for multi-repo, got %+v", cfg.Topology)
	}
	if len(cfg.Components) != 2 {
		t.Fatalf("expected two inferred components, got %+v", cfg.Components)
	}
}

func TestNormalizeProjectConfigPreservesExplicitMonorepoTopology(t *testing.T) {
	cfg := ProjectConfig{
		ProjectID:   "acme",
		ProjectName: "Acme",
		Topology:    &ProjectTopology{Mode: TopologyMonorepo, RootRepoID: "platform"},
		Repos: map[string]ProjectRepo{
			"platform": {Name: "acme-platform", Role: "monorepo root"},
		},
		Components: map[string]ProjectComponent{
			"admin": {
				ComponentID: "admin",
				Kind:        "admin",
				Location:    ProjectComponentLocation{Type: ComponentLocationPath, RepoID: "platform", Path: "apps/admin"},
			},
		},
	}

	normalizeProjectConfig(&cfg)

	if cfg.Topology == nil || cfg.Topology.Mode != TopologyMonorepo || cfg.Topology.RootRepoID != "platform" {
		t.Fatalf("expected explicit monorepo topology to survive, got %+v", cfg.Topology)
	}
	admin := cfg.Components["admin"]
	if admin.Location.Type != ComponentLocationPath || admin.Location.Path != "apps/admin" {
		t.Fatalf("unexpected admin component location: %+v", admin.Location)
	}
	if _, ok := cfg.Components["platform"]; !ok {
		t.Fatalf("expected root repo component to be normalized into components map")
	}
}
