package forge

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	builtinpacks "github.com/chalfel/forge-flow/stacks"
)

type ProjectStarter struct {
	PackID   string `json:"packId,omitempty"`
	PackName string `json:"packName,omitempty"`
}

type StackPack struct {
	ID                  string               `json:"id"`
	Name                string               `json:"name"`
	Description         string               `json:"description,omitempty"`
	DefaultTopology     string               `json:"defaultTopology,omitempty"`
	SupportedTopologies []string             `json:"supportedTopologies,omitempty"`
	RootRepo            *StackPackRootRepo   `json:"rootRepo,omitempty"`
	QualityExpectations []string             `json:"qualityExpectations,omitempty"`
	StarterSteps        []string             `json:"starterSteps,omitempty"`
	Components          []StackPackComponent `json:"components,omitempty"`
}

type StackPackSummary struct {
	ID                  string   `json:"id"`
	Name                string   `json:"name"`
	Description         string   `json:"description,omitempty"`
	DefaultTopology     string   `json:"defaultTopology,omitempty"`
	SupportedTopologies []string `json:"supportedTopologies,omitempty"`
	SourceKind          string   `json:"sourceKind,omitempty"`
	SourceName          string   `json:"sourceName,omitempty"`
	SourcePath          string   `json:"sourcePath,omitempty"`
}

type StackPackRootRepo struct {
	RepoID           string `json:"repoId,omitempty"`
	RepoNameTemplate string `json:"repoNameTemplate,omitempty"`
	Role             string `json:"role,omitempty"`
}

type StackPackComponent struct {
	ComponentID   string                            `json:"componentId"`
	Name          string                            `json:"name,omitempty"`
	Kind          string                            `json:"kind,omitempty"`
	Role          string                            `json:"role,omitempty"`
	Stack         string                            `json:"stack,omitempty"`
	Hosts         map[string]StackPackComponentHost `json:"hosts,omitempty"`
	ScaffoldFiles map[string]string                 `json:"scaffoldFiles,omitempty"`
}

type StackPackComponentHost struct {
	Type             string `json:"type,omitempty"`
	RepoID           string `json:"repoId,omitempty"`
	Path             string `json:"path,omitempty"`
	RepoNameTemplate string `json:"repoNameTemplate,omitempty"`
	RepoRole         string `json:"repoRole,omitempty"`
}

func (s *Service) ListStackPacks() ([]StackPackSummary, error) {
	catalog, err := s.ResolveStackPackCatalog()
	if err != nil {
		return nil, err
	}
	out := make([]StackPackSummary, 0, len(catalog.Packs))
	for _, resolved := range catalog.Packs {
		out = append(out, StackPackSummary{
			ID:                  resolved.Pack.ID,
			Name:                resolved.Pack.Name,
			Description:         resolved.Pack.Description,
			DefaultTopology:     resolved.Pack.DefaultTopology,
			SupportedTopologies: append([]string(nil), resolved.Pack.SupportedTopologies...),
			SourceKind:          resolved.Source.Kind,
			SourceName:          resolved.Source.Name,
			SourcePath:          resolved.Source.Root,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

func (s *Service) LoadStackPack(packID string) (StackPack, error) {
	resolved, err := s.ResolveStackPack(packID)
	if err != nil {
		return StackPack{}, err
	}
	return resolved.Pack, nil
}

func loadBuiltinStackPacks() ([]StackPack, error) {
	return loadStackPacksFromFS(builtinpacks.BuiltinFS)
}

func loadStackPacksFromDir(root string) ([]StackPack, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, err
	}
	packs := make([]StackPack, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		path := filepath.Join(root, entry.Name(), "stack.json")
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		pack, err := parseStackPack(data)
		if err != nil {
			return nil, fmt.Errorf("parse stack pack %s: %w", path, err)
		}
		packs = append(packs, pack)
	}
	sort.Slice(packs, func(i, j int) bool { return packs[i].ID < packs[j].ID })
	return packs, nil
}

func loadStackPacksFromFS(fsys fs.FS) ([]StackPack, error) {
	matches, err := fs.Glob(fsys, "*/stack.json")
	if err != nil {
		return nil, err
	}
	packs := make([]StackPack, 0, len(matches))
	for _, match := range matches {
		data, err := fs.ReadFile(fsys, match)
		if err != nil {
			return nil, err
		}
		pack, err := parseStackPack(data)
		if err != nil {
			return nil, fmt.Errorf("parse stack pack %s: %w", match, err)
		}
		packs = append(packs, pack)
	}
	sort.Slice(packs, func(i, j int) bool { return packs[i].ID < packs[j].ID })
	return packs, nil
}

func parseStackPack(data []byte) (StackPack, error) {
	var pack StackPack
	if err := json.Unmarshal(data, &pack); err != nil {
		return StackPack{}, err
	}
	normalizeStackPack(&pack)
	if err := validateStackPack(pack); err != nil {
		return StackPack{}, err
	}
	return pack, nil
}

func normalizeStackPack(pack *StackPack) {
	pack.ID = slugify(pack.ID)
	pack.Name = strings.TrimSpace(pack.Name)
	pack.Description = strings.TrimSpace(pack.Description)
	pack.DefaultTopology = normalizeTopology(pack.DefaultTopology)
	pack.SupportedTopologies = normalizeTopologyList(pack.SupportedTopologies)
	if len(pack.SupportedTopologies) == 0 && pack.DefaultTopology != "" {
		pack.SupportedTopologies = []string{pack.DefaultTopology}
	}
	if len(pack.SupportedTopologies) == 0 {
		pack.SupportedTopologies = []string{TopologySingleRepo}
	}
	if pack.DefaultTopology == "" {
		pack.DefaultTopology = pack.SupportedTopologies[0]
	}
	if pack.RootRepo != nil {
		pack.RootRepo.RepoID = slugify(pack.RootRepo.RepoID)
		pack.RootRepo.Role = strings.TrimSpace(pack.RootRepo.Role)
		pack.RootRepo.RepoNameTemplate = strings.TrimSpace(pack.RootRepo.RepoNameTemplate)
	}
	for i := range pack.QualityExpectations {
		pack.QualityExpectations[i] = strings.TrimSpace(pack.QualityExpectations[i])
	}
	for i := range pack.StarterSteps {
		pack.StarterSteps[i] = strings.TrimSpace(pack.StarterSteps[i])
	}
	for i := range pack.Components {
		component := &pack.Components[i]
		component.ComponentID = slugify(component.ComponentID)
		component.Name = strings.TrimSpace(component.Name)
		component.Kind = slugify(component.Kind)
		component.Role = strings.TrimSpace(component.Role)
		component.Stack = strings.TrimSpace(component.Stack)
		if component.Hosts == nil {
			component.Hosts = map[string]StackPackComponentHost{}
		}
		normalizedHosts := make(map[string]StackPackComponentHost, len(component.Hosts))
		for topology, host := range component.Hosts {
			topology = normalizeTopology(topology)
			host.Type = normalizeComponentLocation(host.Type)
			host.RepoID = slugify(host.RepoID)
			host.Path = normalizeRelativePath(host.Path)
			host.RepoNameTemplate = strings.TrimSpace(host.RepoNameTemplate)
			host.RepoRole = strings.TrimSpace(host.RepoRole)
			normalizedHosts[topology] = host
		}
		component.Hosts = normalizedHosts
		if component.ScaffoldFiles == nil {
			component.ScaffoldFiles = map[string]string{}
		}
	}
}

func validateStackPack(pack StackPack) error {
	if pack.ID == "" {
		return fmt.Errorf("missing id")
	}
	if pack.Name == "" {
		return fmt.Errorf("missing name")
	}
	if !containsString(pack.SupportedTopologies, pack.DefaultTopology) {
		return fmt.Errorf("default topology %q must be listed in supportedTopologies", pack.DefaultTopology)
	}
	for _, topology := range pack.SupportedTopologies {
		if !isValidTopology(topology) {
			return fmt.Errorf("unsupported topology %q", topology)
		}
	}
	for _, component := range pack.Components {
		if component.ComponentID == "" {
			return fmt.Errorf("component is missing componentId")
		}
		if component.Kind == "" {
			return fmt.Errorf("component %s is missing kind", component.ComponentID)
		}
		if len(component.Hosts) == 0 {
			return fmt.Errorf("component %s is missing hosts", component.ComponentID)
		}
		for _, topology := range pack.SupportedTopologies {
			host, ok := component.Hosts[topology]
			if !ok {
				return fmt.Errorf("component %s is missing a host for topology %s", component.ComponentID, topology)
			}
			if !isValidComponentLocation(host.Type) {
				return fmt.Errorf("component %s has unsupported host type %q for topology %s", component.ComponentID, host.Type, topology)
			}
			if host.Type == ComponentLocationRepo && host.RepoID == "" {
				return fmt.Errorf("component %s needs repoId for repo topology host %s", component.ComponentID, topology)
			}
			if host.Type != ComponentLocationRepo && host.RepoID == "" {
				return fmt.Errorf("component %s needs repoId for non-repo topology host %s", component.ComponentID, topology)
			}
			if host.Type != ComponentLocationRepo && host.Path == "" {
				return fmt.Errorf("component %s needs path for non-repo topology host %s", component.ComponentID, topology)
			}
		}
	}
	return nil
}

func normalizeTopology(value string) string {
	value = slugify(value)
	switch value {
	case "single", "single-repo", "repo":
		return TopologySingleRepo
	case "monorepo", "mono-repo", "mono":
		return TopologyMonorepo
	case "multi-repo", "multirepo", "multi":
		return TopologyMultiRepo
	default:
		return value
	}
}

func normalizeTopologyList(values []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		normalized := normalizeTopology(value)
		if normalized == "" || seen[normalized] {
			continue
		}
		seen[normalized] = true
		out = append(out, normalized)
	}
	return out
}

func normalizeComponentLocation(value string) string {
	value = slugify(value)
	switch value {
	case "repo":
		return ComponentLocationRepo
	case "path", "app":
		return ComponentLocationPath
	case "package", "pkg":
		return ComponentLocationPackage
	case "module":
		return ComponentLocationModule
	default:
		return value
	}
}

func isValidTopology(value string) bool {
	switch value {
	case TopologySingleRepo, TopologyMonorepo, TopologyMultiRepo:
		return true
	default:
		return false
	}
}

func isValidComponentLocation(value string) bool {
	switch value {
	case ComponentLocationRepo, ComponentLocationPath, ComponentLocationPackage, ComponentLocationModule:
		return true
	default:
		return false
	}
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
