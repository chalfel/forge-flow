package forge

import (
	"fmt"
	"sort"
)

// ListAllProjects returns every project across every workspace that has at
// least one linked repo on this machine, sorted by workspace then project ID.
// Used by the board web UI to populate the project picker dropdown.
//
// Projects without a linked repo are filtered out because the board resolves
// context by walking up from a repo path. A project with no local binding
// can't be opened in the board.
func (s *Service) ListAllProjects() ([]ProjectSummary, error) {
	workspaces, err := s.ListWorkspaces()
	if err != nil {
		return nil, err
	}

	local, err := s.LoadLocalConfig()
	if err != nil {
		return nil, err
	}

	var all []ProjectSummary
	for _, ws := range workspaces {
		projects, err := s.ListProjects(ws.WorkspaceID)
		if err != nil {
			// Skip workspaces that fail to list — don't block the whole call.
			continue
		}
		for _, p := range projects {
			binding, ok := local.Projects[p.ProjectID]
			if !ok || binding.Workspace != ws.WorkspaceID {
				continue
			}
			if !hasAnyLinkedRepo(binding) {
				continue
			}
			all = append(all, p)
		}
	}

	sort.Slice(all, func(i, j int) bool {
		if all[i].Workspace != all[j].Workspace {
			return all[i].Workspace < all[j].Workspace
		}
		return all[i].ProjectID < all[j].ProjectID
	})
	return all, nil
}

// hasAnyLinkedRepo returns true if the binding has at least one non-empty
// repo path registered on this machine.
func hasAnyLinkedRepo(binding LocalProjectBinding) bool {
	for _, path := range binding.RepoPaths {
		if path != "" {
			return true
		}
	}
	return false
}

// CwdForProject returns the first linked repo path for the given project,
// which can be used as the `cwd` argument for other Service methods (Board,
// PlanRun, etc.) to operate on that project.
//
// This lets the board web UI switch projects at runtime without changing
// how any service method is called. Returns an error if the project has no
// linked repos on this machine.
func (s *Service) CwdForProject(workspaceID, projectID string) (string, error) {
	if workspaceID == "" {
		workspaceID = "main"
	}
	if projectID == "" {
		return "", fmt.Errorf("missing project id")
	}

	local, err := s.LoadLocalConfig()
	if err != nil {
		return "", err
	}

	binding, ok := local.Projects[projectID]
	if !ok || binding.Workspace != workspaceID {
		return "", fmt.Errorf("project %s/%s has no local binding", workspaceID, projectID)
	}

	// Pick a stable repo (sorted by repoID) so the choice is deterministic.
	repoIDs := make([]string, 0, len(binding.RepoPaths))
	for repoID := range binding.RepoPaths {
		repoIDs = append(repoIDs, repoID)
	}
	sort.Strings(repoIDs)
	for _, repoID := range repoIDs {
		if path := binding.RepoPaths[repoID]; path != "" {
			return path, nil
		}
	}

	return "", fmt.Errorf("project %s/%s has no linked repos", workspaceID, projectID)
}
