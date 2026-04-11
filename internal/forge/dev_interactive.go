package forge

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/charmbracelet/huh"
)

// ---------------------------------------------------------------------------
// Exported types for folder scanning
// ---------------------------------------------------------------------------

// CandidateDir is a subdirectory that might be a project.
type CandidateDir struct {
	Name string `json:"name"`
	Path string `json:"path"`
	Hint string `json:"hint,omitempty"` // e.g. "git, node", "go, docker"
}

// ---------------------------------------------------------------------------
// Pure logic: scan + detect (no UI)
// ---------------------------------------------------------------------------

// ScanCandidateDirs finds subdirectories in parent that look like projects.
func ScanCandidateDirs(parent string) []CandidateDir {
	entries, err := os.ReadDir(parent)
	if err != nil {
		return nil
	}

	skip := map[string]bool{
		"node_modules": true, "vendor": true, "dist": true,
		"build": true, "target": true, ".git": true,
		"__pycache__": true, ".venv": true, "venv": true,
	}

	var candidates []CandidateDir
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		name := entry.Name()
		if strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_") {
			continue
		}
		if skip[name] {
			continue
		}

		dirPath := filepath.Join(parent, name)
		hint := detectDirHint(dirPath)

		candidates = append(candidates, CandidateDir{
			Name: name,
			Path: dirPath,
			Hint: hint,
		})
	}

	return candidates
}

// DetectServicesForPaths detects dev services across multiple directories and
// returns a merged, ordered list. Service names are prefixed with the folder
// name when multiple paths are given to avoid collisions. Dirs are made
// relative to parentRoot.
//
// This function is pure computation — no UI, no file writes.
func DetectServicesForPaths(parentRoot string, selectedPaths []string) ([]DevService, error) {
	absParent, err := filepath.Abs(parentRoot)
	if err != nil {
		return nil, err
	}

	var allServices []DevService

	for _, p := range selectedPaths {
		detected, err := DetectDevServices(p)
		if err != nil {
			continue
		}

		folderName := filepath.Base(p)
		for i := range detected.Services {
			svc := &detected.Services[i]
			// prefix name to avoid collisions (e.g., two "infra")
			if len(selectedPaths) > 1 {
				svc.Name = folderName + "-" + svc.Name
			}
			// make dir relative to parent
			if svc.Dir != "" {
				svc.Dir = filepath.Join(folderName, svc.Dir)
			} else {
				svc.Dir = folderName
			}
		}
		allServices = append(allServices, detected.Services...)
	}

	// reorder: infra services first
	sort.SliceStable(allServices, func(i, j int) bool {
		iInfra := strings.Contains(allServices[i].Name, "infra")
		jInfra := strings.Contains(allServices[j].Name, "infra")
		if iInfra && !jInfra {
			return true
		}
		return false
	})

	_ = absParent // used for future port-conflict detection
	return allServices, nil
}

// ---------------------------------------------------------------------------
// Interactive flow (uses huh)
// ---------------------------------------------------------------------------

// DevInteractiveResult holds the output of the interactive detect flow.
type DevInteractiveResult struct {
	ProjectName string            `json:"projectName"`
	ConfigPath  string            `json:"configPath"`
	Config      DevConfig         `json:"config"`
	Detected    []DetectedProject `json:"detected"`
}

// DevDetectInteractive runs the interactive folder selection + detection flow.
// dir is the parent directory to scan for subdirectories.
func DevDetectInteractive(dir string) (*DevInteractiveResult, error) {
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}

	candidates := ScanCandidateDirs(absDir)
	if len(candidates) == 0 {
		return nil, fmt.Errorf("no project directories found in %s", absDir)
	}

	// build huh options
	options := make([]huh.Option[string], len(candidates))
	for i, c := range candidates {
		label := c.Name
		if c.Hint != "" {
			label = fmt.Sprintf("%s  (%s)", c.Name, c.Hint)
		}
		options[i] = huh.NewOption(label, c.Path)
	}

	var selectedPaths []string
	var projectName string
	defaultName := filepath.Base(absDir)

	form := huh.NewForm(
		huh.NewGroup(
			huh.NewMultiSelect[string]().
				Title("Select folders to include").
				Description(absDir).
				Options(options...).
				Value(&selectedPaths),
		),
		huh.NewGroup(
			huh.NewInput().
				Title("Project name").
				Description("Used as the tmux session name and config filename").
				Placeholder(defaultName).
				Value(&projectName),
		),
	)

	if err := form.Run(); err != nil {
		return nil, err
	}

	if len(selectedPaths) == 0 {
		return nil, fmt.Errorf("no folders selected")
	}

	if strings.TrimSpace(projectName) == "" {
		projectName = defaultName
	}

	// detect using the extracted pure function
	services, err := DetectServicesForPaths(absDir, selectedPaths)
	if err != nil {
		return nil, err
	}

	if len(services) == 0 {
		return nil, fmt.Errorf("no services detected in selected folders")
	}

	cfg := DevConfig{
		Root:     absDir,
		Session:  projectName,
		Services: services,
	}

	configPath, err := WriteDevConfig(projectName, cfg)
	if err != nil {
		return nil, err
	}

	return &DevInteractiveResult{
		ProjectName: projectName,
		ConfigPath:  configPath,
		Config:      cfg,
	}, nil
}

// ---------------------------------------------------------------------------
// Hint detection helpers
// ---------------------------------------------------------------------------

func detectDirHint(dir string) string {
	hints := []struct {
		file  string
		label string
	}{
		{"docker-compose.yml", "docker"},
		{"compose.yml", "docker"},
		{"package.json", ""},
		{"go.mod", "go"},
		{"Cargo.toml", "rust"},
		{"pyproject.toml", "python"},
		{"mix.exs", "elixir"},
		{"Gemfile", "ruby"},
		{"pom.xml", "java"},
		{"build.gradle", "java"},
		{"Makefile", "make"},
	}

	var labels []string
	for _, h := range hints {
		if fileExists(filepath.Join(dir, h.file)) {
			if h.label != "" {
				labels = append(labels, h.label)
			} else if h.file == "package.json" {
				labels = append(labels, detectNodeHint(dir))
			}
		}
	}

	if dirExists(filepath.Join(dir, ".git")) {
		labels = append([]string{"git"}, labels...)
	}

	if len(labels) == 0 {
		return ""
	}
	return strings.Join(labels, ", ")
}

func detectNodeHint(dir string) string {
	scripts := readPkgScripts(filepath.Join(dir, "package.json"))
	if scripts == nil {
		return "node"
	}

	frameworks := []struct {
		file  string
		label string
	}{
		{"next.config.js", "next"},
		{"next.config.ts", "next"},
		{"next.config.mjs", "next"},
		{"nuxt.config.ts", "nuxt"},
		{"vite.config.ts", "vite"},
		{"vite.config.js", "vite"},
		{"angular.json", "angular"},
		{"svelte.config.js", "svelte"},
		{"astro.config.mjs", "astro"},
		{"remix.config.js", "remix"},
		{"app.json", "expo"},
	}

	for _, f := range frameworks {
		if fileExists(filepath.Join(dir, f.file)) {
			return f.label
		}
	}

	return "node"
}
