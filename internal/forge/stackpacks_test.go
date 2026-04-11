package forge

import "testing"

func TestListStackPacksIncludesBuiltins(t *testing.T) {
	svc := &Service{}
	packs, err := svc.ListStackPacks()
	if err != nil {
		t.Fatalf("ListStackPacks: %v", err)
	}
	if len(packs) < 2 {
		t.Fatalf("expected at least two built-in packs, got %+v", packs)
	}
	if packs[0].ID != "blank" {
		t.Fatalf("expected blank pack first, got %+v", packs)
	}
	foundNext := false
	for _, pack := range packs {
		if pack.ID == "nextjs-saas" {
			foundNext = true
			if pack.DefaultTopology != TopologySingleRepo {
				t.Fatalf("unexpected default topology: %+v", pack)
			}
		}
	}
	if !foundNext {
		t.Fatalf("expected nextjs-saas pack, got %+v", packs)
	}
}

func TestLoadStackPackBlank(t *testing.T) {
	svc := &Service{}
	pack, err := svc.LoadStackPack("blank")
	if err != nil {
		t.Fatalf("LoadStackPack: %v", err)
	}
	if pack.ID != "blank" || pack.Name == "" {
		t.Fatalf("unexpected blank pack: %+v", pack)
	}
	if !containsString(pack.SupportedTopologies, TopologyMultiRepo) {
		t.Fatalf("expected blank pack to support multi-repo: %+v", pack)
	}
	if pack.RootRepo == nil || pack.RootRepo.RepoID != "app" {
		t.Fatalf("expected blank pack root repo defaults: %+v", pack.RootRepo)
	}
}

func TestLoadStackPackNextJSSaaS(t *testing.T) {
	svc := &Service{}
	pack, err := svc.LoadStackPack("nextjs-saas")
	if err != nil {
		t.Fatalf("LoadStackPack: %v", err)
	}
	if len(pack.Components) != 1 {
		t.Fatalf("expected one starter component, got %+v", pack.Components)
	}
	web := pack.Components[0]
	if web.ComponentID != "web" || web.Kind != "web" {
		t.Fatalf("unexpected web component: %+v", web)
	}
	if web.Hosts[TopologyMonorepo].Type != ComponentLocationPath {
		t.Fatalf("expected monorepo host to be path, got %+v", web.Hosts[TopologyMonorepo])
	}
	if len(web.ScaffoldFiles) == 0 {
		t.Fatalf("expected starter scaffold files, got %+v", web)
	}
}
