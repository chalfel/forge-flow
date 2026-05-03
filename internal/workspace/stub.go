package workspace

import (
	"context"

	"github.com/chalfel/forge-flow/internal/domain"
)

// Stub returns a no-op workspace per issue. Used in tests and the
// `symphony run --stub` dry-run mode while real filesystem lifecycle is
// implemented in Phase 4.
type Stub struct{}

func NewStub() *Stub { return &Stub{} }

func (Stub) Prepare(_ context.Context, issue domain.Issue) (domain.Workspace, error) {
	return domain.Workspace{
		Path:         "/tmp/symphony-stub/" + issue.ID,
		WorkspaceKey: issue.Identifier,
		CreatedNow:   true,
	}, nil
}

func (Stub) Cleanup(_ context.Context, _ domain.Workspace) error { return nil }
