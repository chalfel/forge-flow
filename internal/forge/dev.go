package forge

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// ---------------------------------------------------------------------------
// Config types
// ---------------------------------------------------------------------------

// DevConfig represents a project's dev session configuration.
type DevConfig struct {
	Root     string       `json:"root"`
	Session  string       `json:"session"`
	Services []DevService `json:"services"`
}

// DevService is a single runnable service within a dev session.
type DevService struct {
	Name string `json:"name"`
	Cmd  string `json:"cmd"`
	Dir  string `json:"dir,omitempty"`  // relative to Root, or absolute
	Wait int    `json:"wait,omitempty"` // seconds to wait after starting
}

// DevSessionStatus describes a project session and whether it's running.
type DevSessionStatus struct {
	Project     string `json:"project"`
	Session     string `json:"session"`
	Root        string `json:"root"`
	ServiceCount int   `json:"serviceCount"`
	Running     bool   `json:"running"`
}

// ---------------------------------------------------------------------------
// Config directory
// ---------------------------------------------------------------------------

// DevConfigDir returns the path where dev configs live.
// Respects FORGE_DEV_CONFIG env var, defaults to ~/.config/forge-dev.
func DevConfigDir() (string, error) {
	if env := os.Getenv("FORGE_DEV_CONFIG"); env != "" {
		return env, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "forge-dev"), nil
}

// DevConfigPath returns the config file path for a project name.
func DevConfigPath(project string) (string, error) {
	dir, err := DevConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, project+".yml"), nil
}

// ---------------------------------------------------------------------------
// YAML parsing (minimal, our config subset only)
// ---------------------------------------------------------------------------

var (
	reRoot    = regexp.MustCompile(`^root:\s*(.+)`)
	reSession = regexp.MustCompile(`^session:\s*(.+)`)
	reSvcName = regexp.MustCompile(`^\s*-\s*name:\s*(.+)`)
	reSvcCmd  = regexp.MustCompile(`^\s+cmd:\s*(.+)`)
	reSvcDir  = regexp.MustCompile(`^\s+dir:\s*(.+)`)
	reSvcWait = regexp.MustCompile(`^\s+wait:\s*(\d+)`)
)

// ParseDevConfig reads a dev config YAML file.
func ParseDevConfig(path string) (DevConfig, error) {
	f, err := os.Open(path)
	if err != nil {
		return DevConfig{}, err
	}
	defer f.Close()

	var cfg DevConfig
	var current *DevService
	inServices := false

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()

		// skip comments and blank lines
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}

		if m := reRoot.FindStringSubmatch(line); m != nil {
			cfg.Root = expandHome(strings.TrimSpace(m[1]))
			continue
		}
		if m := reSession.FindStringSubmatch(line); m != nil {
			cfg.Session = strings.TrimSpace(m[1])
			continue
		}

		if strings.HasPrefix(trimmed, "services:") {
			inServices = true
			continue
		}

		if !inServices {
			continue
		}

		// non-indented line that's not "services:" ends the block
		if !strings.HasPrefix(line, " ") && !strings.HasPrefix(line, "\t") {
			inServices = false
			continue
		}

		if m := reSvcName.FindStringSubmatch(line); m != nil {
			// save previous service
			if current != nil {
				cfg.Services = append(cfg.Services, *current)
			}
			current = &DevService{Name: strings.TrimSpace(m[1])}
			continue
		}

		if current == nil {
			continue
		}

		if m := reSvcCmd.FindStringSubmatch(line); m != nil {
			current.Cmd = strings.TrimSpace(m[1])
		} else if m := reSvcDir.FindStringSubmatch(line); m != nil {
			current.Dir = strings.TrimSpace(m[1])
		} else if m := reSvcWait.FindStringSubmatch(line); m != nil {
			fmt.Sscanf(m[1], "%d", &current.Wait)
		}
	}

	// save last service
	if current != nil {
		cfg.Services = append(cfg.Services, *current)
	}

	if err := scanner.Err(); err != nil {
		return DevConfig{}, err
	}

	// derive session name from filename if not set
	if cfg.Session == "" {
		base := filepath.Base(path)
		cfg.Session = strings.TrimSuffix(base, filepath.Ext(base))
	}

	return cfg, nil
}

func expandHome(p string) string {
	if strings.HasPrefix(p, "~/") || p == "~" {
		home, err := os.UserHomeDir()
		if err != nil {
			return p
		}
		return filepath.Join(home, p[2:])
	}
	return p
}

// ---------------------------------------------------------------------------
// Config generation (writes YAML)
// ---------------------------------------------------------------------------

// WriteDevConfig writes a DevConfig to the standard config path.
func WriteDevConfig(project string, cfg DevConfig) (string, error) {
	dir, err := DevConfigDir()
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}

	path := filepath.Join(dir, project+".yml")

	var b strings.Builder
	fmt.Fprintf(&b, "# %s\n", project)
	fmt.Fprintf(&b, "# Generated by forge dev — edit freely\n")
	fmt.Fprintf(&b, "# Detected: %s\n\n", time.Now().Format("2006-01-02"))
	fmt.Fprintf(&b, "root: %s\n", cfg.Root)
	if cfg.Session != "" && cfg.Session != project {
		fmt.Fprintf(&b, "session: %s\n", cfg.Session)
	}
	b.WriteString("\nservices:\n")
	for _, svc := range cfg.Services {
		fmt.Fprintf(&b, "  - name: %s\n", svc.Name)
		fmt.Fprintf(&b, "    cmd: %s\n", svc.Cmd)
		if svc.Dir != "" {
			fmt.Fprintf(&b, "    dir: %s\n", svc.Dir)
		}
		if svc.Wait > 0 {
			fmt.Fprintf(&b, "    wait: %d\n", svc.Wait)
		}
		b.WriteString("\n")
	}

	if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
		return "", err
	}

	return path, nil
}

// ---------------------------------------------------------------------------
// Project detection
// ---------------------------------------------------------------------------

// DetectedProject holds auto-detected services for a directory.
type DetectedProject struct {
	Root        string       `json:"root"`
	ProjectName string       `json:"projectName"`
	PackageManager string    `json:"packageManager"`
	Services    []DevService `json:"services"`
}

// DetectDevServices analyzes a project directory and returns detected services.
func DetectDevServices(root string) (DetectedProject, error) {
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return DetectedProject{}, err
	}

	dp := DetectedProject{
		Root:        absRoot,
		ProjectName: deriveProjectName(absRoot),
	}

	dp.PackageManager = detectDevPackageManager(absRoot)

	// 1. Infrastructure
	if svc := detectInfra(absRoot); svc != nil {
		dp.Services = append(dp.Services, *svc)
	}

	// 2. Monorepo subdirectories
	monoServices := detectMonorepoServices(absRoot, dp.PackageManager)

	// 3. Root-level app (only if not a monorepo with runnable sub-services)
	if rootSvc := detectRootApp(absRoot, dp.PackageManager); rootSvc != nil {
		// If we have mono services, only add root if it has a different cmd
		if len(monoServices) == 0 {
			dp.Services = append(dp.Services, *rootSvc)
		}
	}

	dp.Services = append(dp.Services, monoServices...)

	return dp, nil
}

func deriveProjectName(absRoot string) string {
	base := filepath.Base(absRoot)
	// if the dir name is generic, include parent
	generic := map[string]bool{"app": true, "web": true, "api": true, "src": true, "frontend": true, "backend": true}
	if generic[strings.ToLower(base)] {
		parent := filepath.Base(filepath.Dir(absRoot))
		if parent != "." && parent != "/" {
			return parent + "-" + base
		}
	}
	return base
}

func detectDevPackageManager(root string) string {
	checks := []struct {
		file string
		pm   string
	}{
		{"bun.lockb", "bun"},
		{"bun.lock", "bun"},
		{"pnpm-lock.yaml", "pnpm"},
		{"yarn.lock", "yarn"},
		{"package-lock.json", "npm"},
	}
	for _, c := range checks {
		if fileExists(filepath.Join(root, c.file)) {
			return c.pm
		}
	}
	if fileExists(filepath.Join(root, "package.json")) {
		return "npm"
	}
	return ""
}

func detectInfra(root string) *DevService {
	composeFiles := []string{
		"docker-compose.yml",
		"docker-compose.yaml",
		"compose.yml",
		"compose.yaml",
	}

	for _, f := range composeFiles {
		if fileExists(filepath.Join(root, f)) {
			// check for dev override
			devOverride := ""
			devFiles := []string{"docker-compose.dev.yml", "docker-compose.dev.yaml", "compose.dev.yml", "compose.dev.yaml"}
			for _, df := range devFiles {
				if fileExists(filepath.Join(root, df)) {
					devOverride = df
					break
				}
			}

			cmd := "docker compose up"
			if devOverride != "" {
				cmd = fmt.Sprintf("docker compose -f %s -f %s up", f, devOverride)
			}

			return &DevService{
				Name: "infra",
				Cmd:  cmd,
				Wait: 5,
			}
		}
	}

	// tilt
	if fileExists(filepath.Join(root, "Tiltfile")) {
		return &DevService{Name: "infra", Cmd: "tilt up"}
	}

	return nil
}

func detectRootApp(root, pm string) *DevService {
	// package.json
	if pm != "" {
		scripts := readPkgScripts(filepath.Join(root, "package.json"))
		if _, ok := scripts["dev"]; ok {
			return &DevService{Name: "app", Cmd: pm + " run dev"}
		}
		if _, ok := scripts["start:dev"]; ok {
			return &DevService{Name: "app", Cmd: pm + " run start:dev"}
		}
		if _, ok := scripts["start"]; ok {
			return &DevService{Name: "app", Cmd: pm + " run start"}
		}
	}

	// Makefile
	if targets := readMakeTargets(filepath.Join(root, "Makefile")); len(targets) > 0 {
		for _, t := range []string{"dev", "run", "serve", "start"} {
			if targets[t] {
				return &DevService{Name: "app", Cmd: "make " + t}
			}
		}
	}

	// Go
	if fileExists(filepath.Join(root, "go.mod")) {
		if fileExists(filepath.Join(root, ".air.toml")) || fileExists(filepath.Join(root, "air.toml")) {
			return &DevService{Name: "app", Cmd: "air"}
		}
		// check if there's a main.go or cmd/
		if fileExists(filepath.Join(root, "main.go")) || dirExists(filepath.Join(root, "cmd")) {
			return &DevService{Name: "app", Cmd: "go run ."}
		}
	}

	// Python
	if fileExists(filepath.Join(root, "pyproject.toml")) || fileExists(filepath.Join(root, "requirements.txt")) {
		if fileExists(filepath.Join(root, "manage.py")) {
			return &DevService{Name: "app", Cmd: "python manage.py runserver"}
		}
	}

	// Elixir
	if fileExists(filepath.Join(root, "mix.exs")) {
		if dirExists(filepath.Join(root, "lib")) {
			return &DevService{Name: "app", Cmd: "mix phx.server"}
		}
	}

	// Rust
	if fileExists(filepath.Join(root, "Cargo.toml")) {
		return &DevService{Name: "app", Cmd: "cargo run"}
	}

	return nil
}

func detectMonorepoServices(root, pm string) []DevService {
	var services []DevService

	// known subdirectory names that are typically services
	knownDirs := []string{
		"api", "server", "backend",
		"web", "app", "frontend", "client",
		"admin", "backoffice", "dashboard",
		"mobile", "expo",
		"worker", "jobs", "queue",
		"landing", "marketing", "docs",
	}

	for _, name := range knownDirs {
		dir := filepath.Join(root, name)
		if !dirExists(dir) {
			continue
		}
		if svc := detectSubdirService(root, name, pm); svc != nil {
			services = append(services, *svc)
		}
	}

	// apps/* (turborepo/nx)
	services = append(services, scanSubdirPattern(root, "apps", pm)...)

	// packages/* (but only if they have dev/start scripts — libraries don't need running)
	services = append(services, scanSubdirPattern(root, "packages", pm)...)

	return services
}

func scanSubdirPattern(root, parent, pm string) []DevService {
	var services []DevService
	parentDir := filepath.Join(root, parent)
	if !dirExists(parentDir) {
		return nil
	}
	entries, err := os.ReadDir(parentDir)
	if err != nil {
		return nil
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		relPath := filepath.Join(parent, entry.Name())
		if svc := detectSubdirService(root, relPath, pm); svc != nil {
			services = append(services, *svc)
		}
	}
	return services
}

func detectSubdirService(root, relDir, pm string) *DevService {
	absDir := filepath.Join(root, relDir)
	pkgPath := filepath.Join(absDir, "package.json")
	scripts := readPkgScripts(pkgPath)

	runCmd := ""
	// check for local package manager first
	localPM := detectDevPackageManager(absDir)
	if localPM == "" {
		localPM = pm
	}
	if localPM == "" {
		localPM = "npm"
	}

	if _, ok := scripts["dev"]; ok {
		runCmd = localPM + " run dev"
	} else if _, ok := scripts["start:dev"]; ok {
		runCmd = localPM + " run start:dev"
	} else if _, ok := scripts["start"]; ok {
		runCmd = localPM + " run start"
	}

	// Makefile fallback
	if runCmd == "" {
		if targets := readMakeTargets(filepath.Join(absDir, "Makefile")); len(targets) > 0 {
			for _, t := range []string{"dev", "run", "serve", "start"} {
				if targets[t] {
					runCmd = "make " + t
					break
				}
			}
		}
	}

	// Go fallback
	if runCmd == "" && fileExists(filepath.Join(absDir, "go.mod")) {
		if fileExists(filepath.Join(absDir, ".air.toml")) || fileExists(filepath.Join(absDir, "air.toml")) {
			runCmd = "air"
		} else if fileExists(filepath.Join(absDir, "main.go")) || dirExists(filepath.Join(absDir, "cmd")) {
			runCmd = "go run ."
		}
	}

	if runCmd == "" {
		return nil
	}

	// use the last path segment as the service name for readability
	name := filepath.Base(relDir)
	return &DevService{
		Name: name,
		Cmd:  runCmd,
		Dir:  relDir,
	}
}

// ---------------------------------------------------------------------------
// Session management (uses tmux.go helpers)
// ---------------------------------------------------------------------------

// DevStartSession creates a tmux session with all services from the config.
func DevStartSession(cfg DevConfig) error {
	if err := EnsureTmuxAvailable(); err != nil {
		return err
	}

	if !dirExists(cfg.Root) {
		return fmt.Errorf("project root not found: %s", cfg.Root)
	}

	// check if already running
	if DevSessionRunning(cfg.Session) {
		return fmt.Errorf("session %q already running (use: forge dev attach %s)", cfg.Session, cfg.Session)
	}

	if len(cfg.Services) == 0 {
		return fmt.Errorf("no services defined")
	}

	// create session with first service
	if err := CreateTmuxSession(cfg.Session); err != nil {
		return err
	}

	// rename the default window and run the first service
	firstDir := resolveServiceDir(cfg.Root, cfg.Services[0].Dir)
	shellRun("tmux", "rename-window", "-t", cfg.Session+":0", cfg.Services[0].Name)
	shellRun("tmux", "send-keys", "-t", cfg.Session+":"+cfg.Services[0].Name, "cd "+firstDir, "Enter")
	SendKeys(cfg.Session+":"+cfg.Services[0].Name, cfg.Services[0].Cmd)

	if cfg.Services[0].Wait > 0 {
		time.Sleep(time.Duration(cfg.Services[0].Wait) * time.Second)
	}

	// remaining services
	for _, svc := range cfg.Services[1:] {
		svcDir := resolveServiceDir(cfg.Root, svc.Dir)
		paneID, err := NewTmuxWindow(cfg.Session, svc.Name)
		if err != nil {
			return fmt.Errorf("creating window for %s: %w", svc.Name, err)
		}
		shellRun("tmux", "send-keys", "-t", paneID, "cd "+svcDir, "Enter")
		SendKeys(paneID, svc.Cmd)

		if svc.Wait > 0 {
			time.Sleep(time.Duration(svc.Wait) * time.Second)
		}
	}

	// add a shell window
	shellRun("tmux", "new-window", "-t", cfg.Session, "-n", "shell")
	shellRun("tmux", "send-keys", "-t", cfg.Session+":shell", "cd "+cfg.Root, "Enter")
	// select shell
	shellRun("tmux", "select-window", "-t", cfg.Session+":shell")

	return nil
}

// DevSessionRunning checks if a tmux session exists.
func DevSessionRunning(session string) bool {
	_, err := shellRun("tmux", "has-session", "-t", "="+session)
	return err == nil
}

// DevKillSession kills a dev tmux session.
func DevKillSession(session string) error {
	if !DevSessionRunning(session) {
		return fmt.Errorf("no active session %q", session)
	}
	_, err := shellRun("tmux", "kill-session", "-t", session)
	return err
}

// DevListSessions lists all configured projects and their status.
func DevListSessions() ([]DevSessionStatus, error) {
	dir, err := DevConfigDir()
	if err != nil {
		return nil, err
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var results []DevSessionStatus
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".yml") {
			continue
		}

		path := filepath.Join(dir, entry.Name())
		cfg, err := ParseDevConfig(path)
		if err != nil {
			continue
		}

		project := strings.TrimSuffix(entry.Name(), ".yml")
		results = append(results, DevSessionStatus{
			Project:      project,
			Session:      cfg.Session,
			Root:         cfg.Root,
			ServiceCount: len(cfg.Services),
			Running:      DevSessionRunning(cfg.Session),
		})
	}

	return results, nil
}

func resolveServiceDir(root, dir string) string {
	if dir == "" {
		return root
	}
	if filepath.IsAbs(dir) {
		return dir
	}
	return filepath.Join(root, dir)
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func dirExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

// readPkgScripts reads the "scripts" map from a package.json.
func readPkgScripts(path string) map[string]bool {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	// minimal JSON extraction — find "scripts" object keys
	result := map[string]bool{}
	s := string(data)
	idx := strings.Index(s, `"scripts"`)
	if idx < 0 {
		return nil
	}
	// find the opening {
	rest := s[idx:]
	braceStart := strings.Index(rest, "{")
	if braceStart < 0 {
		return nil
	}
	rest = rest[braceStart:]
	// find matching }
	depth := 0
	end := 0
	for i, ch := range rest {
		if ch == '{' {
			depth++
		} else if ch == '}' {
			depth--
			if depth == 0 {
				end = i
				break
			}
		}
	}
	if end == 0 {
		return nil
	}
	block := rest[:end+1]
	// extract keys (simple: find "key":)
	re := regexp.MustCompile(`"([^"]+)"\s*:`)
	for _, m := range re.FindAllStringSubmatch(block, -1) {
		result[m[1]] = true
	}
	return result
}

// readMakeTargets reads target names from a Makefile.
func readMakeTargets(path string) map[string]bool {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()

	re := regexp.MustCompile(`^([a-zA-Z_][a-zA-Z0-9_-]*)\s*:`)
	result := map[string]bool{}
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		if m := re.FindStringSubmatch(scanner.Text()); m != nil {
			result[m[1]] = true
		}
	}
	return result
}
