package forge

import (
	"sort"
	"strings"
)

const (
	TopologySingleRepo = "single-repo"
	TopologyMonorepo   = "monorepo"
	TopologyMultiRepo  = "multi-repo"

	ComponentLocationRepo    = "repo"
	ComponentLocationPath    = "path"
	ComponentLocationPackage = "package"
	ComponentLocationModule  = "module"
)

type ProjectTopology struct {
	Mode       string `json:"mode"`
	RootRepoID string `json:"rootRepoId,omitempty"`
}

type ProjectComponent struct {
	ComponentID string                   `json:"componentId"`
	Name        string                   `json:"name,omitempty"`
	Kind        string                   `json:"kind,omitempty"`
	Role        string                   `json:"role,omitempty"`
	Stack       string                   `json:"stack,omitempty"`
	Location    ProjectComponentLocation `json:"location"`
}

type ProjectComponentLocation struct {
	Type   string `json:"type"`
	RepoID string `json:"repoId,omitempty"`
	Path   string `json:"path,omitempty"`
}

func normalizeProjectConfig(cfg *ProjectConfig) {
	if cfg.Version == 0 {
		cfg.Version = ProjectVersion
	}
	if cfg.Repos == nil {
		cfg.Repos = map[string]ProjectRepo{}
	}
	if cfg.Components == nil {
		cfg.Components = map[string]ProjectComponent{}
	}
	if cfg.Topology == nil || cfg.Topology.Mode == "" {
		inferred := inferProjectTopology(*cfg)
		cfg.Topology = &inferred
	}
	if cfg.Topology != nil {
		switch cfg.Topology.Mode {
		case TopologySingleRepo, TopologyMonorepo:
			if cfg.Topology.RootRepoID == "" {
				cfg.Topology.RootRepoID = firstRepoID(cfg.Repos)
			}
		case TopologyMultiRepo:
			cfg.Topology.RootRepoID = ""
		}
	}
	for repoID, repo := range cfg.Repos {
		component, ok := cfg.Components[repoID]
		if ok {
			component.ComponentID = repoID
			if component.Location.Type == "" {
				component.Location = ProjectComponentLocation{Type: ComponentLocationRepo, RepoID: repoID}
			}
			if component.Location.Type == ComponentLocationRepo && component.Location.RepoID == "" {
				component.Location.RepoID = repoID
			}
			if component.Name == "" {
				component.Name = fallback(repo.Name, repoID)
			}
			if component.Role == "" {
				component.Role = repo.Role
			}
			if component.Kind == "" {
				component.Kind = inferComponentKind(repoID, repo.Role)
			}
			cfg.Components[repoID] = component
			continue
		}
		cfg.Components[repoID] = defaultComponentForRepo(repoID, repo)
	}
}

func inferProjectTopology(cfg ProjectConfig) ProjectTopology {
	if len(cfg.Repos) > 1 {
		return ProjectTopology{Mode: TopologyMultiRepo}
	}
	return ProjectTopology{Mode: TopologySingleRepo, RootRepoID: firstRepoID(cfg.Repos)}
}

func defaultComponentForRepo(repoID string, repo ProjectRepo) ProjectComponent {
	return ProjectComponent{
		ComponentID: repoID,
		Name:        fallback(repo.Name, repoID),
		Kind:        inferComponentKind(repoID, repo.Role),
		Role:        repo.Role,
		Location: ProjectComponentLocation{
			Type:   ComponentLocationRepo,
			RepoID: repoID,
		},
	}
}

func inferComponentKind(componentID, role string) string {
	text := slugify(componentID + " " + role)
	switch {
	case containsAny(text, "api", "backend", "service"):
		return "api"
	case containsAny(text, "mobile", "ios", "android", "expo"):
		return "mobile"
	case containsAny(text, "worker", "queue", "job"):
		return "worker"
	case containsAny(text, "admin", "dashboard"):
		return "admin"
	case containsAny(text, "package", "sdk", "library", "lib"):
		return "package"
	case containsAny(text, "docs", "documentation"):
		return "docs"
	case containsAny(text, "web", "frontend", "ui", "site"):
		return "web"
	default:
		return "component"
	}
}

func firstRepoID(repos map[string]ProjectRepo) string {
	if len(repos) == 0 {
		return ""
	}
	ids := make([]string, 0, len(repos))
	for id := range repos {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids[0]
}

func containsAny(text string, needles ...string) bool {
	for _, needle := range needles {
		if needle != "" && strings.Contains(text, needle) {
			return true
		}
	}
	return false
}
