// Package workspace manages per-issue isolated directories. Phase 2 ships a
// stub manager so the scheduler runs end-to-end; real lifecycle (path
// containment, sanitized keys, after_create / before_run / after_run /
// before_remove hooks) lands in Phase 4.
package workspace

import (
	"context"

	"github.com/chalfel/forge-flow/internal/domain"
)

type Manager interface {
	// Prepare returns a workspace ready for an attempt. Implementations are
	// responsible for after_create/before_run hooks; the scheduler treats
	// hook failures as attempt failures.
	Prepare(ctx context.Context, issue domain.Issue) (domain.Workspace, error)
	// Cleanup runs after_run / before_remove hooks. Failures are logged but
	// do not affect attempt status.
	Cleanup(ctx context.Context, ws domain.Workspace) error
}
