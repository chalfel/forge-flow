package forge

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPlanProjectCreateUsesCustomLocalPack(t *testing.T) {
	home := t.TempDir()
	cwd := filepath.Join(home, "code")
	mustMkdirAll(t, filepath.Join(home, "local-packs", "custom-web"))
	mustMkdirAll(t, cwd)
	writeTestStackPack(t, filepath.Join(home, "local-packs", "custom-web", "stack.json"), StackPack{
		ID:                  "custom-web",
		Name:                "Custom Web",
		Description:         "Local custom web starter",
		DefaultTopology:     TopologySingleRepo,
		SupportedTopologies: []string{TopologySingleRepo},
		Components: []StackPackComponent{
			{
				ComponentID: "web",
				Name:        "Web",
				Kind:        "web",
				Role:        "customer-facing web app",
				Hosts: map[string]StackPackComponentHost{
					TopologySingleRepo: {
						Type:             ComponentLocationRepo,
						RepoID:           "web",
						RepoNameTemplate: "{{projectId}}",
						RepoRole:         "customer-facing web app",
					},
				},
				ScaffoldFiles: map[string]string{
					"README.md": "# {{projectName}}\n",
				},
			},
		},
	})
	t.Setenv("FORGE_STACKS_DIR", filepath.Join(home, "local-packs"))

	svc := &Service{ForgeHome: filepath.Join(home, ".forge")}
	plan, err := svc.PlanProjectCreate(ProjectCreateInput{CWD: cwd, ProjectName: "Acme Platform", Pack: "custom-web"})
	if err != nil {
		t.Fatalf("PlanProjectCreate: %v", err)
	}
	if plan.Pack.ID != "custom-web" || plan.Pack.SourceKind != StackPackSourceLocal {
		t.Fatalf("expected local custom pack in plan, got %+v", plan.Pack)
	}
	if plan.Pack.SourcePath != filepath.Join(home, "local-packs") {
		t.Fatalf("unexpected source path: %+v", plan.Pack)
	}
	if len(plan.Repos) != 1 || plan.Repos[0].RepoID != "web" {
		t.Fatalf("expected web repo from custom pack, got %+v", plan.Repos)
	}
	if len(plan.ScaffoldFiles) != 1 || filepath.Base(plan.ScaffoldFiles[0]) != "README.md" {
		t.Fatalf("expected custom scaffold file, got %+v", plan.ScaffoldFiles)
	}
}

func TestApplyProjectCreateRejectsInvalidCustomPackBeforeWritingFiles(t *testing.T) {
	home := t.TempDir()
	cwd := filepath.Join(home, "code")
	mustMkdirAll(t, filepath.Join(home, "local-packs", "broken-pack"))
	mustMkdirAll(t, cwd)
	if err := os.WriteFile(filepath.Join(home, "local-packs", "broken-pack", "stack.json"), []byte("{\n  \"id\": \"broken-pack\"\n}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("FORGE_STACKS_DIR", filepath.Join(home, "local-packs"))

	svc := &Service{ForgeHome: filepath.Join(home, ".forge")}
	_, err := svc.ApplyProjectCreate(ProjectCreateInput{CWD: cwd, ProjectName: "Broken App", Pack: "broken-pack"})
	if err == nil {
		t.Fatalf("expected invalid custom pack error")
	}
	projectRoot := filepath.Join(svc.ForgeHome, "workspaces", "main", "projects", "broken-app")
	if _, statErr := os.Stat(projectRoot); !os.IsNotExist(statErr) {
		t.Fatalf("expected project root not to be created, stat err=%v", statErr)
	}
	repoRoot := filepath.Join(cwd, "broken-app")
	if _, statErr := os.Stat(repoRoot); !os.IsNotExist(statErr) {
		t.Fatalf("expected repo root not to be created, stat err=%v", statErr)
	}
}
