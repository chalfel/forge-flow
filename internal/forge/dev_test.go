package forge

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseDevConfig(t *testing.T) {
	content := `# Test project
root: /tmp/test-project
session: my-session

services:
  - name: infra
    cmd: docker compose up
    wait: 5

  - name: api
    cmd: npm run dev
    dir: api

  - name: web
    cmd: pnpm run dev
    dir: web
`
	dir := t.TempDir()
	path := filepath.Join(dir, "test.yml")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := ParseDevConfig(path)
	if err != nil {
		t.Fatal(err)
	}

	if cfg.Root != "/tmp/test-project" {
		t.Errorf("root = %q, want /tmp/test-project", cfg.Root)
	}
	if cfg.Session != "my-session" {
		t.Errorf("session = %q, want my-session", cfg.Session)
	}
	if len(cfg.Services) != 3 {
		t.Fatalf("services count = %d, want 3", len(cfg.Services))
	}

	// infra
	if cfg.Services[0].Name != "infra" {
		t.Errorf("svc[0].Name = %q, want infra", cfg.Services[0].Name)
	}
	if cfg.Services[0].Cmd != "docker compose up" {
		t.Errorf("svc[0].Cmd = %q, want 'docker compose up'", cfg.Services[0].Cmd)
	}
	if cfg.Services[0].Wait != 5 {
		t.Errorf("svc[0].Wait = %d, want 5", cfg.Services[0].Wait)
	}

	// api
	if cfg.Services[1].Name != "api" {
		t.Errorf("svc[1].Name = %q, want api", cfg.Services[1].Name)
	}
	if cfg.Services[1].Dir != "api" {
		t.Errorf("svc[1].Dir = %q, want api", cfg.Services[1].Dir)
	}

	// web
	if cfg.Services[2].Name != "web" {
		t.Errorf("svc[2].Name = %q, want web", cfg.Services[2].Name)
	}
	if cfg.Services[2].Cmd != "pnpm run dev" {
		t.Errorf("svc[2].Cmd = %q, want 'pnpm run dev'", cfg.Services[2].Cmd)
	}
}

func TestParseDevConfig_SessionFromFilename(t *testing.T) {
	content := `root: /tmp/proj
services:
  - name: app
    cmd: npm run dev
`
	dir := t.TempDir()
	path := filepath.Join(dir, "amplify.yml")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := ParseDevConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Session != "amplify" {
		t.Errorf("session = %q, want amplify (derived from filename)", cfg.Session)
	}
}

func TestParseDevConfig_HomeExpansion(t *testing.T) {
	content := `root: ~/projects/my-app
services:
  - name: app
    cmd: npm start
`
	dir := t.TempDir()
	path := filepath.Join(dir, "test.yml")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := ParseDevConfig(path)
	if err != nil {
		t.Fatal(err)
	}

	home, _ := os.UserHomeDir()
	want := filepath.Join(home, "projects/my-app")
	if cfg.Root != want {
		t.Errorf("root = %q, want %q", cfg.Root, want)
	}
}

func TestDetectDevServices_NodeMonorepo(t *testing.T) {
	dir := t.TempDir()

	// root package.json (no dev script — monorepo root)
	devWriteFile(t, filepath.Join(dir, "package.json"), `{"name": "monorepo", "workspaces": ["api", "web"]}`)
	devWriteFile(t, filepath.Join(dir, "pnpm-lock.yaml"), "")

	// docker compose
	devWriteFile(t, filepath.Join(dir, "docker-compose.yml"), "version: '3'\nservices:\n  db:\n    image: postgres")

	// api sub-project
	os.MkdirAll(filepath.Join(dir, "api"), 0o755)
	devWriteFile(t, filepath.Join(dir, "api", "package.json"), `{"name": "api", "scripts": {"dev": "nest start --watch"}}`)

	// web sub-project
	os.MkdirAll(filepath.Join(dir, "web"), 0o755)
	devWriteFile(t, filepath.Join(dir, "web", "package.json"), `{"name": "web", "scripts": {"dev": "next dev"}}`)

	detected, err := DetectDevServices(dir)
	if err != nil {
		t.Fatal(err)
	}

	if detected.PackageManager != "pnpm" {
		t.Errorf("pm = %q, want pnpm", detected.PackageManager)
	}

	// should have: infra, api, web (no root app since it's monorepo without dev script)
	names := map[string]bool{}
	for _, svc := range detected.Services {
		names[svc.Name] = true
	}

	if !names["infra"] {
		t.Error("missing infra service")
	}
	if !names["api"] {
		t.Error("missing api service")
	}
	if !names["web"] {
		t.Error("missing web service")
	}
}

func TestDetectDevServices_SingleApp(t *testing.T) {
	dir := t.TempDir()

	devWriteFile(t, filepath.Join(dir, "package.json"), `{"name": "my-app", "scripts": {"dev": "vite"}}`)
	devWriteFile(t, filepath.Join(dir, "yarn.lock"), "")

	detected, err := DetectDevServices(dir)
	if err != nil {
		t.Fatal(err)
	}

	if detected.PackageManager != "yarn" {
		t.Errorf("pm = %q, want yarn", detected.PackageManager)
	}

	if len(detected.Services) != 1 {
		t.Fatalf("services = %d, want 1", len(detected.Services))
	}

	if detected.Services[0].Cmd != "yarn run dev" {
		t.Errorf("cmd = %q, want 'yarn run dev'", detected.Services[0].Cmd)
	}
}

func TestDetectDevServices_GoProject(t *testing.T) {
	dir := t.TempDir()

	devWriteFile(t, filepath.Join(dir, "go.mod"), "module example.com/test\n\ngo 1.21")
	devWriteFile(t, filepath.Join(dir, "main.go"), "package main\nfunc main() {}")
	devWriteFile(t, filepath.Join(dir, ".air.toml"), "[build]\ncmd = \"go build\"")

	detected, err := DetectDevServices(dir)
	if err != nil {
		t.Fatal(err)
	}

	if len(detected.Services) != 1 {
		t.Fatalf("services = %d, want 1", len(detected.Services))
	}

	if detected.Services[0].Cmd != "air" {
		t.Errorf("cmd = %q, want air", detected.Services[0].Cmd)
	}
}

func TestDetectDevServices_DockerComposeDevOverride(t *testing.T) {
	dir := t.TempDir()

	devWriteFile(t, filepath.Join(dir, "docker-compose.yml"), "version: '3'")
	devWriteFile(t, filepath.Join(dir, "docker-compose.dev.yml"), "version: '3'")

	detected, err := DetectDevServices(dir)
	if err != nil {
		t.Fatal(err)
	}

	found := false
	for _, svc := range detected.Services {
		if svc.Name == "infra" {
			found = true
			want := "docker compose -f docker-compose.yml -f docker-compose.dev.yml up"
			if svc.Cmd != want {
				t.Errorf("infra cmd = %q, want %q", svc.Cmd, want)
			}
		}
	}
	if !found {
		t.Error("missing infra service")
	}
}

func TestDetectDevServices_Makefile(t *testing.T) {
	dir := t.TempDir()

	devWriteFile(t, filepath.Join(dir, "Makefile"), "dev:\n\t@echo running\n\nbuild:\n\t@echo building")

	detected, err := DetectDevServices(dir)
	if err != nil {
		t.Fatal(err)
	}

	if len(detected.Services) != 1 {
		t.Fatalf("services = %d, want 1", len(detected.Services))
	}

	if detected.Services[0].Cmd != "make dev" {
		t.Errorf("cmd = %q, want 'make dev'", detected.Services[0].Cmd)
	}
}

func TestDeriveProjectName(t *testing.T) {
	tests := []struct {
		path string
		want string
	}{
		{"/home/user/projects/amplify", "amplify"},
		{"/home/user/floki/web", "floki-web"},
		{"/home/user/floki/api", "floki-api"},
		{"/home/user/my-cool-app", "my-cool-app"},
	}
	for _, tt := range tests {
		got := deriveProjectName(tt.path)
		if got != tt.want {
			t.Errorf("deriveProjectName(%q) = %q, want %q", tt.path, got, tt.want)
		}
	}
}

func TestWriteAndParseDevConfig_Roundtrip(t *testing.T) {
	orig := os.Getenv("FORGE_DEV_CONFIG")
	dir := t.TempDir()
	os.Setenv("FORGE_DEV_CONFIG", dir)
	defer os.Setenv("FORGE_DEV_CONFIG", orig)

	cfg := DevConfig{
		Root:    "/tmp/myproject",
		Session: "myproj",
		Services: []DevService{
			{Name: "infra", Cmd: "docker compose up", Wait: 3},
			{Name: "api", Cmd: "npm run dev", Dir: "api"},
		},
	}

	path, err := WriteDevConfig("myproject", cfg)
	if err != nil {
		t.Fatal(err)
	}

	parsed, err := ParseDevConfig(path)
	if err != nil {
		t.Fatal(err)
	}

	if parsed.Root != cfg.Root {
		t.Errorf("root = %q, want %q", parsed.Root, cfg.Root)
	}
	if parsed.Session != cfg.Session {
		t.Errorf("session = %q, want %q", parsed.Session, cfg.Session)
	}
	if len(parsed.Services) != len(cfg.Services) {
		t.Fatalf("services = %d, want %d", len(parsed.Services), len(cfg.Services))
	}
	if parsed.Services[0].Wait != 3 {
		t.Errorf("svc[0].wait = %d, want 3", parsed.Services[0].Wait)
	}
	if parsed.Services[1].Dir != "api" {
		t.Errorf("svc[1].dir = %q, want api", parsed.Services[1].Dir)
	}
}

func TestReadPkgScripts(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "package.json")
	devWriteFile(t, path, `{
  "name": "test",
  "scripts": {
    "dev": "vite",
    "build": "tsc && vite build",
    "start": "node dist/index.js"
  }
}`)

	scripts := readPkgScripts(path)
	if !scripts["dev"] {
		t.Error("missing dev script")
	}
	if !scripts["build"] {
		t.Error("missing build script")
	}
	if !scripts["start"] {
		t.Error("missing start script")
	}
	if scripts["test"] {
		t.Error("unexpected test script")
	}
}

func TestReadMakeTargets(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "Makefile")
	devWriteFile(t, path, `dev:
	@echo dev

build:
	@echo build

.PHONY: dev build
`)

	targets := readMakeTargets(path)
	if !targets["dev"] {
		t.Error("missing dev target")
	}
	if !targets["build"] {
		t.Error("missing build target")
	}
}

func devWriteFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
