package forge

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWorkspaceProjectRepoLifecycle(t *testing.T) {
	home := t.TempDir()
	svc := &Service{ForgeHome: filepath.Join(home, ".forge")}

	workspace, err := svc.InitWorkspace("main", "Main Workspace", "")
	if err != nil {
		t.Fatalf("InitWorkspace: %v", err)
	}
	if workspace.Root != filepath.Join(svc.ForgeHome, "workspaces", "main") {
		t.Fatalf("unexpected workspace root: %s", workspace.Root)
	}

	project, err := svc.InitProject("main", "acme-platform", "Acme Platform")
	if err != nil {
		t.Fatalf("InitProject: %v", err)
	}
	if _, err := os.Stat(filepath.Join(project.ProjectRoot, "kb", "repos.md")); err != nil {
		t.Fatalf("expected repos.md: %v", err)
	}

	repoRoot := filepath.Join(home, "code", "acme-web")
	if err := os.MkdirAll(filepath.Join(repoRoot, "src"), 0o755); err != nil {
		t.Fatal(err)
	}
	ctx, err := svc.LinkRepo("main", "acme-platform", "web", "acme-web", "customer-facing web app", "", repoRoot)
	if err != nil {
		t.Fatalf("LinkRepo: %v", err)
	}
	if ctx.RepoID != "web" || ctx.ProjectID != "acme-platform" {
		t.Fatalf("unexpected context after link: %+v", ctx)
	}
	if _, err := os.Stat(filepath.Join(repoRoot, ".forge", "project.json")); err != nil {
		t.Fatalf("expected repo link file: %v", err)
	}

	resolved, err := svc.ResolveContext(filepath.Join(repoRoot, "src"))
	if err != nil {
		t.Fatalf("ResolveContext: %v", err)
	}
	if resolved.ProjectRoot != project.ProjectRoot {
		t.Fatalf("unexpected project root: %s", resolved.ProjectRoot)
	}
	if resolved.RepoRoot != repoRoot {
		t.Fatalf("unexpected repo root: %s", resolved.RepoRoot)
	}
	if len(resolved.Repos) != 1 || resolved.Repos[0].RepoID != "web" {
		t.Fatalf("unexpected repos: %+v", resolved.Repos)
	}
}

func TestPIInstallLifecycle(t *testing.T) {
	home := t.TempDir()
	t.Setenv("PI_CODING_AGENT_DIR", filepath.Join(home, ".pi", "agent"))
	svc := &Service{ForgeHome: filepath.Join(home, ".forge")}
	pkg := filepath.Join(home, "forge-skills")
	if err := os.MkdirAll(pkg, 0o755); err != nil {
		t.Fatal(err)
	}

	status, err := svc.PIInstall(pkg)
	if err != nil {
		t.Fatalf("PIInstall: %v", err)
	}
	if !status.Installed {
		t.Fatalf("expected installed status")
	}

	status, err = svc.PIStatus(pkg)
	if err != nil {
		t.Fatalf("PIStatus: %v", err)
	}
	if !status.Installed {
		t.Fatalf("expected package to be installed")
	}

	status, err = svc.PIUninstall(pkg)
	if err != nil {
		t.Fatalf("PIUninstall: %v", err)
	}
	if status.Installed {
		t.Fatalf("expected package to be removed")
	}
}
