package forge

import (
	"os"
	"path/filepath"
	"testing"
)

func TestListAllProjectsFiltersProjectsWithoutLinkedRepo(t *testing.T) {
	home := t.TempDir()
	svc := &Service{ForgeHome: filepath.Join(home, ".forge")}

	if _, err := svc.InitWorkspace("main", "Main", ""); err != nil {
		t.Fatalf("InitWorkspace: %v", err)
	}
	if _, err := svc.InitProject("main", "with-repo", "With Repo"); err != nil {
		t.Fatalf("InitProject with-repo: %v", err)
	}
	if _, err := svc.InitProject("main", "no-repo", "No Repo"); err != nil {
		t.Fatalf("InitProject no-repo: %v", err)
	}

	// Link a repo for the first project only.
	repoPath := filepath.Join(home, "code", "with-repo")
	if err := os.MkdirAll(repoPath, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.LinkRepo("main", "with-repo", "core", "with-repo", "core", "", repoPath); err != nil {
		t.Fatalf("LinkRepo: %v", err)
	}

	projects, err := svc.ListAllProjects()
	if err != nil {
		t.Fatalf("ListAllProjects: %v", err)
	}

	if len(projects) != 1 {
		t.Fatalf("expected 1 project (only with-repo), got %d", len(projects))
	}
	if projects[0].ProjectID != "with-repo" {
		t.Errorf("expected with-repo, got %s", projects[0].ProjectID)
	}
}

func TestListAllProjectsStableSorted(t *testing.T) {
	home := t.TempDir()
	svc := &Service{ForgeHome: filepath.Join(home, ".forge")}

	if _, err := svc.InitWorkspace("main", "Main", ""); err != nil {
		t.Fatal(err)
	}
	ids := []string{"zeta", "alpha", "mike"}
	for _, id := range ids {
		if _, err := svc.InitProject("main", id, id); err != nil {
			t.Fatal(err)
		}
		repoPath := filepath.Join(home, "code", id)
		if err := os.MkdirAll(repoPath, 0o755); err != nil {
			t.Fatal(err)
		}
		if _, err := svc.LinkRepo("main", id, "core", id, "core", "", repoPath); err != nil {
			t.Fatal(err)
		}
	}

	projects, err := svc.ListAllProjects()
	if err != nil {
		t.Fatal(err)
	}
	if len(projects) != 3 {
		t.Fatalf("expected 3 projects, got %d", len(projects))
	}
	want := []string{"alpha", "mike", "zeta"}
	for i, w := range want {
		if projects[i].ProjectID != w {
			t.Errorf("position %d: expected %s, got %s", i, w, projects[i].ProjectID)
		}
	}
}

func TestCwdForProjectReturnsLinkedRepoPath(t *testing.T) {
	home := t.TempDir()
	svc := &Service{ForgeHome: filepath.Join(home, ".forge")}

	if _, err := svc.InitWorkspace("main", "Main", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.InitProject("main", "demo", "Demo"); err != nil {
		t.Fatal(err)
	}

	repoPath := filepath.Join(home, "code", "demo")
	if err := os.MkdirAll(repoPath, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.LinkRepo("main", "demo", "core", "demo", "core", "", repoPath); err != nil {
		t.Fatal(err)
	}

	got, err := svc.CwdForProject("main", "demo")
	if err != nil {
		t.Fatalf("CwdForProject: %v", err)
	}
	if got != repoPath {
		t.Errorf("expected %s, got %s", repoPath, got)
	}
}

func TestCwdForProjectDeterministicWithMultipleRepos(t *testing.T) {
	home := t.TempDir()
	svc := &Service{ForgeHome: filepath.Join(home, ".forge")}

	if _, err := svc.InitWorkspace("main", "Main", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.InitProject("main", "multi", "Multi"); err != nil {
		t.Fatal(err)
	}

	// Link three repos out of order.
	repos := []string{"web", "api", "admin"}
	for _, id := range repos {
		p := filepath.Join(home, "code", id)
		if err := os.MkdirAll(p, 0o755); err != nil {
			t.Fatal(err)
		}
		if _, err := svc.LinkRepo("main", "multi", id, id, "", "", p); err != nil {
			t.Fatal(err)
		}
	}

	// Should always return the alphabetically-first repo ID (admin).
	for i := 0; i < 3; i++ {
		got, err := svc.CwdForProject("main", "multi")
		if err != nil {
			t.Fatal(err)
		}
		want := filepath.Join(home, "code", "admin")
		if got != want {
			t.Errorf("iteration %d: expected %s, got %s", i, want, got)
		}
	}
}

func TestCwdForProjectErrorsWhenNoRepos(t *testing.T) {
	home := t.TempDir()
	svc := &Service{ForgeHome: filepath.Join(home, ".forge")}

	if _, err := svc.InitWorkspace("main", "Main", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.InitProject("main", "empty", "Empty"); err != nil {
		t.Fatal(err)
	}

	if _, err := svc.CwdForProject("main", "empty"); err == nil {
		t.Fatal("expected error for project with no linked repos")
	}
}

func TestCwdForProjectErrorsOnMissingProject(t *testing.T) {
	home := t.TempDir()
	svc := &Service{ForgeHome: filepath.Join(home, ".forge")}
	if _, err := svc.InitWorkspace("main", "Main", ""); err != nil {
		t.Fatal(err)
	}

	if _, err := svc.CwdForProject("main", "nonexistent"); err == nil {
		t.Fatal("expected error for missing project")
	}
}
