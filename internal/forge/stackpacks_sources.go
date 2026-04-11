package forge

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const (
	StackPackSourceBuiltin   = "builtin"
	StackPackSourceLocal     = "local"
	StackPackSourceWorkspace = "workspace"
)

type StackPackSource struct {
	Kind      string `json:"kind"`
	Name      string `json:"name,omitempty"`
	Root      string `json:"root,omitempty"`
	Workspace string `json:"workspace,omitempty"`
	Order     int    `json:"order"`
}

type ResolvedStackPack struct {
	Pack     StackPack         `json:"pack"`
	Source   StackPackSource   `json:"source"`
	Shadowed []StackPackSource `json:"shadowed,omitempty"`
}

type StackPackCatalog struct {
	Sources []StackPackSource   `json:"sources,omitempty"`
	Packs   []ResolvedStackPack `json:"packs,omitempty"`
}

func (s *Service) ResolveStackPackCatalog() (StackPackCatalog, error) {
	sources, err := s.discoverStackPackSources()
	if err != nil {
		return StackPackCatalog{}, err
	}
	resolvedByID := map[string]ResolvedStackPack{}
	ids := []string{}
	for _, source := range sources {
		packs, err := loadStackPacksFromSource(source)
		if err != nil {
			return StackPackCatalog{}, fmt.Errorf("load stack packs from %s: %w", source.Label(), err)
		}
		for _, pack := range packs {
			current, exists := resolvedByID[pack.ID]
			if exists {
				current.Shadowed = append(current.Shadowed, source)
				resolvedByID[pack.ID] = current
				continue
			}
			resolvedByID[pack.ID] = ResolvedStackPack{Pack: pack, Source: source}
			ids = append(ids, pack.ID)
		}
	}
	sort.Strings(ids)
	catalog := StackPackCatalog{Sources: sources, Packs: make([]ResolvedStackPack, 0, len(ids))}
	for _, id := range ids {
		catalog.Packs = append(catalog.Packs, resolvedByID[id])
	}
	return catalog, nil
}

func (s *Service) ResolveStackPack(packID string) (ResolvedStackPack, error) {
	catalog, err := s.ResolveStackPackCatalog()
	if err != nil {
		return ResolvedStackPack{}, err
	}
	resolved, ok := catalog.Lookup(packID)
	if !ok {
		return ResolvedStackPack{}, fmt.Errorf("unknown stack pack %q", slugify(packID))
	}
	return resolved, nil
}

func (c StackPackCatalog) Lookup(packID string) (ResolvedStackPack, bool) {
	packID = slugify(packID)
	for _, pack := range c.Packs {
		if pack.Pack.ID == packID {
			return pack, true
		}
	}
	return ResolvedStackPack{}, false
}

func (s StackPackSource) Label() string {
	switch s.Kind {
	case StackPackSourceBuiltin:
		return "built-in packs"
	case StackPackSourceWorkspace:
		if s.Workspace != "" && s.Root != "" {
			return fmt.Sprintf("workspace %s (%s)", s.Workspace, s.Root)
		}
		if s.Workspace != "" {
			return fmt.Sprintf("workspace %s", s.Workspace)
		}
	case StackPackSourceLocal:
		if s.Root != "" {
			return s.Root
		}
	}
	if s.Name != "" {
		return s.Name
	}
	if s.Root != "" {
		return s.Root
	}
	return s.Kind
}

func loadStackPacksFromSource(source StackPackSource) ([]StackPack, error) {
	switch source.Kind {
	case StackPackSourceBuiltin:
		return loadBuiltinStackPacks()
	case StackPackSourceLocal, StackPackSourceWorkspace:
		return loadStackPacksFromDir(source.Root)
	default:
		return nil, fmt.Errorf("unsupported stack pack source kind %q", source.Kind)
	}
}

func (s *Service) discoverStackPackSources() ([]StackPackSource, error) {
	sources := make([]StackPackSource, 0)
	for _, root := range parseStackPackRootList(os.Getenv("FORGE_STACKS_DIR")) {
		sources = append(sources, StackPackSource{Kind: StackPackSourceLocal, Name: filepath.Base(root), Root: root})
	}
	workspaceSources, err := s.workspaceStackPackSources()
	if err != nil {
		return nil, err
	}
	sources = append(sources, workspaceSources...)
	sources = append(sources, StackPackSource{Kind: StackPackSourceBuiltin, Name: "builtin"})
	for i := range sources {
		sources[i].Order = i
	}
	return sources, nil
}

func parseStackPackRootList(value string) []string {
	parts := filepath.SplitList(strings.TrimSpace(value))
	seen := map[string]bool{}
	roots := make([]string, 0, len(parts))
	for _, part := range parts {
		root := cleanAbs(strings.TrimSpace(part))
		if root == "" || seen[root] {
			continue
		}
		seen[root] = true
		roots = append(roots, root)
	}
	return roots
}

func (s *Service) workspaceStackPackSources() ([]StackPackSource, error) {
	roots, err := s.knownWorkspaceRoots()
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(roots))
	for workspaceID := range roots {
		ids = append(ids, workspaceID)
	}
	sort.Strings(ids)
	sources := make([]StackPackSource, 0, len(ids))
	for _, workspaceID := range ids {
		root := roots[workspaceID]
		sharedDir := "shared"
		if cfg, err := readWorkspaceConfigFromRoot(root); err == nil && cfg.SharedDir != "" {
			sharedDir = cfg.SharedDir
		}
		stackRoot := filepath.Join(root, sharedDir, "stacks")
		info, err := os.Stat(stackRoot)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return nil, err
		}
		if !info.IsDir() {
			continue
		}
		sources = append(sources, StackPackSource{Kind: StackPackSourceWorkspace, Name: workspaceID, Workspace: workspaceID, Root: stackRoot})
	}
	return sources, nil
}

func (s *Service) knownWorkspaceRoots() (map[string]string, error) {
	roots := map[string]string{}
	local, err := s.LoadLocalConfig()
	if err != nil {
		return nil, err
	}
	for workspaceID, binding := range local.Workspaces {
		if strings.TrimSpace(binding.Root) == "" {
			continue
		}
		roots[workspaceID] = cleanAbs(binding.Root)
	}
	workspaceParent := filepath.Join(s.ForgeHome, "workspaces")
	entries, err := os.ReadDir(workspaceParent)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return roots, nil
		}
		return nil, err
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		workspaceID := entry.Name()
		if _, exists := roots[workspaceID]; exists {
			continue
		}
		root := filepath.Join(workspaceParent, workspaceID)
		if _, err := os.Stat(filepath.Join(root, "workspace.json")); err == nil {
			roots[workspaceID] = cleanAbs(root)
		}
	}
	return roots, nil
}

func readWorkspaceConfigFromRoot(root string) (WorkspaceConfig, error) {
	var cfg WorkspaceConfig
	if err := readJSON(filepath.Join(root, "workspace.json"), &cfg); err != nil {
		return WorkspaceConfig{}, err
	}
	if cfg.SharedDir == "" {
		cfg.SharedDir = "shared"
	}
	return cfg, nil
}
