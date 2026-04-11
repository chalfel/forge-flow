package forge

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveStackPackCatalogIncludesWorkspaceSharedPacks(t *testing.T) {
	home := t.TempDir()
	svc := &Service{ForgeHome: filepath.Join(home, ".forge")}
	workspace, err := svc.InitWorkspace("main", "Main Workspace", "")
	if err != nil {
		t.Fatalf("InitWorkspace: %v", err)
	}
	stackRoot := filepath.Join(workspace.Root, "shared", "stacks", "workspace-pack")
	if err := os.MkdirAll(stackRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	writeTestStackPack(t, filepath.Join(stackRoot, "stack.json"), StackPack{
		ID:                  "workspace-pack",
		Name:                "Workspace Pack",
		DefaultTopology:     TopologySingleRepo,
		SupportedTopologies: []string{TopologySingleRepo},
	})

	catalog, err := svc.ResolveStackPackCatalog()
	if err != nil {
		t.Fatalf("ResolveStackPackCatalog: %v", err)
	}
	resolved, ok := catalog.Lookup("workspace-pack")
	if !ok {
		t.Fatalf("expected workspace-pack in catalog: %+v", catalog)
	}
	if resolved.Source.Kind != StackPackSourceWorkspace || resolved.Source.Workspace != "main" {
		t.Fatalf("expected workspace source, got %+v", resolved.Source)
	}
	pack, err := svc.LoadStackPack("workspace-pack")
	if err != nil {
		t.Fatalf("LoadStackPack: %v", err)
	}
	if pack.Name != "Workspace Pack" {
		t.Fatalf("unexpected pack: %+v", pack)
	}
}

func TestResolveStackPackCatalogPrefersEarlierLocalSource(t *testing.T) {
	home := t.TempDir()
	firstRoot := filepath.Join(home, "first")
	secondRoot := filepath.Join(home, "second")
	mustMkdirAll(t, filepath.Join(firstRoot, "acme-pack"))
	mustMkdirAll(t, filepath.Join(secondRoot, "acme-pack"))
	writeTestStackPack(t, filepath.Join(firstRoot, "acme-pack", "stack.json"), StackPack{
		ID:                  "acme-pack",
		Name:                "First Pack",
		DefaultTopology:     TopologySingleRepo,
		SupportedTopologies: []string{TopologySingleRepo},
	})
	writeTestStackPack(t, filepath.Join(secondRoot, "acme-pack", "stack.json"), StackPack{
		ID:                  "acme-pack",
		Name:                "Second Pack",
		DefaultTopology:     TopologySingleRepo,
		SupportedTopologies: []string{TopologySingleRepo},
	})
	t.Setenv("FORGE_STACKS_DIR", firstRoot+string(os.PathListSeparator)+secondRoot)

	svc := &Service{ForgeHome: filepath.Join(home, ".forge")}
	catalog, err := svc.ResolveStackPackCatalog()
	if err != nil {
		t.Fatalf("ResolveStackPackCatalog: %v", err)
	}
	resolved, ok := catalog.Lookup("acme-pack")
	if !ok {
		t.Fatalf("expected acme-pack in catalog: %+v", catalog)
	}
	if resolved.Pack.Name != "First Pack" {
		t.Fatalf("expected first local source to win, got %+v", resolved)
	}
	if resolved.Source.Kind != StackPackSourceLocal || resolved.Source.Root != cleanAbs(firstRoot) {
		t.Fatalf("unexpected winning source: %+v", resolved.Source)
	}
	if len(resolved.Shadowed) == 0 || resolved.Shadowed[0].Root != cleanAbs(secondRoot) {
		t.Fatalf("expected second source to be shadowed, got %+v", resolved.Shadowed)
	}
}

func TestResolveStackPackCatalogLocalOverrideBeatsBuiltin(t *testing.T) {
	home := t.TempDir()
	localRoot := filepath.Join(home, "local-packs")
	mustMkdirAll(t, filepath.Join(localRoot, "blank"))
	writeTestStackPack(t, filepath.Join(localRoot, "blank", "stack.json"), StackPack{
		ID:                  "blank",
		Name:                "Blank Override",
		DefaultTopology:     TopologySingleRepo,
		SupportedTopologies: []string{TopologySingleRepo, TopologyMonorepo, TopologyMultiRepo},
	})
	t.Setenv("FORGE_STACKS_DIR", localRoot)

	svc := &Service{ForgeHome: filepath.Join(home, ".forge")}
	pack, err := svc.LoadStackPack("blank")
	if err != nil {
		t.Fatalf("LoadStackPack: %v", err)
	}
	if pack.Name != "Blank Override" {
		t.Fatalf("expected local blank override, got %+v", pack)
	}
	catalog, err := svc.ResolveStackPackCatalog()
	if err != nil {
		t.Fatalf("ResolveStackPackCatalog: %v", err)
	}
	resolved, ok := catalog.Lookup("blank")
	if !ok {
		t.Fatalf("expected blank in catalog")
	}
	if resolved.Source.Kind != StackPackSourceLocal {
		t.Fatalf("expected local source to win, got %+v", resolved.Source)
	}
	foundBuiltinShadow := false
	for _, shadowed := range resolved.Shadowed {
		if shadowed.Kind == StackPackSourceBuiltin {
			foundBuiltinShadow = true
		}
	}
	if !foundBuiltinShadow {
		t.Fatalf("expected builtin blank pack to be shadowed, got %+v", resolved.Shadowed)
	}
	items, err := svc.ListStackPacks()
	if err != nil {
		t.Fatalf("ListStackPacks: %v", err)
	}
	for _, item := range items {
		if item.ID == "blank" {
			if item.SourceKind != StackPackSourceLocal || item.SourcePath != cleanAbs(localRoot) {
				t.Fatalf("expected summary to expose local source, got %+v", item)
			}
			return
		}
	}
	t.Fatalf("expected blank summary entry, got %+v", items)
}

func TestResolveStackPackCatalogRejectsInvalidCustomPack(t *testing.T) {
	home := t.TempDir()
	localRoot := filepath.Join(home, "local-packs")
	mustMkdirAll(t, filepath.Join(localRoot, "broken-pack"))
	if err := os.WriteFile(filepath.Join(localRoot, "broken-pack", "stack.json"), []byte("{\n  \"id\": \"broken-pack\"\n}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("FORGE_STACKS_DIR", localRoot)

	svc := &Service{ForgeHome: filepath.Join(home, ".forge")}
	_, err := svc.ResolveStackPackCatalog()
	if err == nil {
		t.Fatalf("expected invalid custom pack error")
	}
	if !containsAny(err.Error(), "broken-pack", "missing name") {
		t.Fatalf("expected actionable error, got %v", err)
	}
}

func writeTestStackPack(t *testing.T, path string, pack StackPack) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := writeJSON(path, pack); err != nil {
		t.Fatal(err)
	}
}
