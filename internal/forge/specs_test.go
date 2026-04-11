package forge

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLintSpecsRejectsMalformedSpec(t *testing.T) {
	svc, webRepo, _ := setupSpecTestProject(t)
	ctx, err := svc.ResolveContext(webRepo)
	if err != nil {
		t.Fatalf("ResolveContext: %v", err)
	}

	specPath := filepath.Join(ctx.SpecsDir, "bad-spec.md")
	content := `---
title: GitHub Login
---

This is not a valid Forge spec.

- just a list
- not task sections
`
	if err := os.WriteFile(specPath, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	report, err := svc.LintSpecs(webRepo, specPath)
	if err != nil {
		t.Fatalf("LintSpecs: %v", err)
	}
	if report.Valid {
		t.Fatalf("expected malformed spec to be invalid")
	}
	assertIssueCode(t, report.Issues, "missing_title")
	assertIssueCode(t, report.Issues, "missing_demo")
	assertIssueCode(t, report.Issues, "missing_tasks")
}

func TestLintSpecsAcceptsCanonicalBehaviorDrivenSpec(t *testing.T) {
	svc, webRepo, _ := setupSpecTestProject(t)
	ctx, err := svc.ResolveContext(webRepo)
	if err != nil {
		t.Fatalf("ResolveContext: %v", err)
	}

	specPath := filepath.Join(ctx.SpecsDir, "canonical-v2.md")
	content := `# User can sign in with GitHub
<!-- status: todo -->
<!-- priority: high -->
<!-- created: 2026-04-08 -->
<!-- branch: forge/test-canonical -->

**What exists:** Email login already exists.

**What's missing:** GitHub OAuth across web and api.

**Demo:** User clicks Continue with GitHub and lands on the dashboard.

## Expected behavior
- Login page shows a GitHub CTA.
- Successful auth lands the user on the dashboard.
- Failed auth shows an error without breaking email login.

## Test cases

### Scenario: GitHub login succeeds
Given a user is on the login screen
When the user clicks Continue with GitHub and completes OAuth successfully
Then a session is created
And the user lands on the dashboard

### Scenario: GitHub login fails
Given a user is on the login screen
When the OAuth callback fails
Then the user sees an error message
And email login still works

## Validation plan
- Web flow should be covered by the browser runner.
- API callback behavior should be covered by integration tests.

### Task: Shared auth contract
<!-- status: todo -->
<!-- parallelizable: no -->
<!-- deps: none -->
<!-- repo: api -->
<!-- shared-contracts: prisma/schema.prisma, openapi/auth.yaml, src/shared/auth-types.ts -->
<!-- conflict-risk: high -->

**Done when:**
- Schema and shared auth contract are updated.

**Validation:**
- Covers: Scenario "GitHub login succeeds"
- Covers: Scenario "GitHub login fails"

### Task: Login button UI
<!-- status: todo -->
<!-- parallelizable: yes -->
<!-- deps: Shared auth contract -->
<!-- repo: web -->
<!-- touches: app/login/** -->
<!-- conflict-risk: low -->

**Done when:**
- Login page shows a GitHub CTA.

**Validation:**
- Covers: Scenario "GitHub login succeeds"
- Covers: Scenario "GitHub login fails"
`
	if err := os.WriteFile(specPath, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	report, err := svc.LintSpecs(webRepo, specPath)
	if err != nil {
		t.Fatalf("LintSpecs: %v", err)
	}
	if !report.Valid {
		t.Fatalf("expected canonical behavior-driven spec to be valid, got %+v", report.Issues)
	}
	if report.WarningCount != 0 {
		t.Fatalf("expected no warnings, got %+v", report.Issues)
	}
}

func TestLintSpecsRejectsMalformedScenarioStructure(t *testing.T) {
	svc, webRepo, _ := setupSpecTestProject(t)
	ctx, err := svc.ResolveContext(webRepo)
	if err != nil {
		t.Fatalf("ResolveContext: %v", err)
	}

	specPath := filepath.Join(ctx.SpecsDir, "malformed-scenarios.md")
	content := `# User can sign in with GitHub
<!-- status: todo -->
<!-- priority: high -->
<!-- created: 2026-04-08 -->
<!-- branch: forge/test-malformed-scenarios -->

**What exists:** Email login already exists.

**What's missing:** GitHub OAuth across web and api.

**Demo:** User clicks Continue with GitHub and lands on the dashboard.

## Expected behavior
- Login page shows a GitHub CTA.

## Test cases

### Scenario: GitHub login succeeds
Given a user is on the login screen
And the provider is available

## Validation plan
- Browser runner should cover the happy path.

### Task: Login button UI
<!-- status: todo -->
<!-- parallelizable: yes -->
<!-- deps: none -->
<!-- repo: web -->
<!-- touches: app/login/** -->
<!-- conflict-risk: low -->

**Done when:**
- Login page shows a GitHub CTA.

**Validation:**
- Covers: Scenario "GitHub login succeeds"
`
	if err := os.WriteFile(specPath, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	report, err := svc.LintSpecs(webRepo, specPath)
	if err != nil {
		t.Fatalf("LintSpecs: %v", err)
	}
	if report.Valid {
		t.Fatalf("expected malformed scenario structure to be invalid")
	}
	assertIssueCode(t, report.Issues, "scenario_missing_when")
	assertIssueCode(t, report.Issues, "scenario_missing_then")
}

func TestLintSpecsDetectsOverlappingParallelTouches(t *testing.T) {
	svc, webRepo, _ := setupSpecTestProject(t)
	ctx, err := svc.ResolveContext(webRepo)
	if err != nil {
		t.Fatalf("ResolveContext: %v", err)
	}

	specPath := filepath.Join(ctx.SpecsDir, "overlap-spec.md")
	content := `# User can sign in with GitHub
<!-- status: todo -->
<!-- priority: high -->
<!-- created: 2026-04-08 -->
<!-- branch: forge/test-overlap -->

**What exists:** Email login already exists.

**What's missing:** GitHub OAuth across web and api.

**Demo:** User clicks Continue with GitHub and lands on the dashboard.

## Expected behavior
- Login page shows a GitHub CTA.
- Successful auth lands the user on the dashboard.

## Test cases

### Scenario: GitHub login succeeds
Given a user is on the login screen
When the user clicks Continue with GitHub
Then the user lands on the dashboard

## Validation plan
- Browser runner should cover the happy path.

### Task: Web login CTA
<!-- status: todo -->
<!-- parallelizable: yes -->
<!-- deps: none -->
<!-- repo: web -->
<!-- touches: app/login/**, src/features/auth/** -->
<!-- conflict-risk: low -->

**Done when:**
- Login page shows a GitHub CTA.

**Validation:**
- Covers: Scenario "GitHub login succeeds"

### Task: Web auth shell
<!-- status: todo -->
<!-- parallelizable: yes -->
<!-- deps: none -->
<!-- repo: web -->
<!-- touches: app/layout.tsx, src/features/auth/** -->
<!-- conflict-risk: medium -->

**Done when:**
- App shell supports the authenticated GitHub user state.

**Validation:**
- Covers: Scenario "GitHub login succeeds"
`
	if err := os.WriteFile(specPath, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	report, err := svc.LintSpecs(webRepo, specPath)
	if err != nil {
		t.Fatalf("LintSpecs: %v", err)
	}
	if report.Valid {
		t.Fatalf("expected overlapping spec to be invalid")
	}
	assertIssueCode(t, report.Issues, "overlapping_touches")
}

func TestPlanRunRespectsSerialBlockedAndConflictTasks(t *testing.T) {
	svc, webRepo, _ := setupSpecTestProject(t)
	ctx, err := svc.ResolveContext(webRepo)
	if err != nil {
		t.Fatalf("ResolveContext: %v", err)
	}

	specPath := filepath.Join(ctx.SpecsDir, "run-plan.md")
	content := `# User can sign in with GitHub
<!-- status: todo -->
<!-- priority: high -->
<!-- created: 2026-04-08 -->
<!-- branch: forge/test-plan -->

**What exists:** Email login already exists.

**What's missing:** GitHub OAuth across web and api.

**Demo:** User clicks Continue with GitHub and lands on the dashboard.

### Task: Shared auth contract
<!-- status: done -->
<!-- parallelizable: no -->
<!-- deps: none -->
<!-- repo: api -->
<!-- touches: prisma/schema.prisma, openapi/auth.yaml, src/shared/auth-types.ts -->
<!-- shared-contracts: prisma/schema.prisma, openapi/auth.yaml, src/shared/auth-types.ts -->
<!-- conflict-risk: high -->

**Done when:**
- Schema and shared auth contract are updated.

### Task: API GitHub callback
<!-- status: todo -->
<!-- parallelizable: yes -->
<!-- deps: Shared auth contract -->
<!-- repo: api -->
<!-- touches: src/routes/auth/** -->
<!-- shared-contracts: openapi/auth.yaml, src/shared/auth-types.ts -->
<!-- conflict-risk: medium -->

**Done when:**
- API exchanges the GitHub code for a session.

### Task: API session cleanup
<!-- status: todo -->
<!-- parallelizable: no -->
<!-- deps: Shared auth contract -->
<!-- repo: api -->
<!-- touches: src/session/** -->
<!-- conflict-risk: medium -->

**Done when:**
- Session lifecycle code is cleaned up for the new login flow.

### Task: Web login CTA
<!-- status: todo -->
<!-- parallelizable: yes -->
<!-- deps: Shared auth contract -->
<!-- repo: web -->
<!-- touches: app/login/** -->
<!-- conflict-risk: low -->
<!-- conflicts-with: Web auth shell -->

**Done when:**
- Login page shows a GitHub CTA.

### Task: Web auth shell
<!-- status: todo -->
<!-- parallelizable: yes -->
<!-- deps: Shared auth contract -->
<!-- repo: web -->
<!-- touches: app/layout.tsx -->
<!-- conflict-risk: medium -->
<!-- conflicts-with: Web login CTA -->

**Done when:**
- App shell handles authenticated GitHub state.

### Task: Web dashboard session state
<!-- status: todo -->
<!-- parallelizable: yes -->
<!-- deps: Web auth shell -->
<!-- repo: web -->
<!-- touches: app/dashboard/** -->
<!-- conflict-risk: low -->

**Done when:**
- Dashboard reads the GitHub-backed session correctly.
`
	if err := os.WriteFile(specPath, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	plan, err := svc.PlanRun(webRepo, specPath)
	if err != nil {
		t.Fatalf("PlanRun: %v", err)
	}
	if !plan.LintValid {
		t.Fatalf("expected valid lint report, got errors: %+v", plan.Issues)
	}
	if len(plan.Safe) != 1 || plan.Safe[0].Task != "API GitHub callback" {
		t.Fatalf("unexpected safe tasks: %+v", plan.Safe)
	}
	if len(plan.Serial) != 1 || plan.Serial[0].Task != "API session cleanup" {
		t.Fatalf("unexpected serial tasks: %+v", plan.Serial)
	}
	if len(plan.Conflict) != 2 {
		t.Fatalf("expected 2 conflict tasks, got %+v", plan.Conflict)
	}
	if len(plan.Blocked) != 1 || plan.Blocked[0].Task != "Web dashboard session state" {
		t.Fatalf("unexpected blocked tasks: %+v", plan.Blocked)
	}
	if len(plan.Done) != 1 || plan.Done[0].Task != "Shared auth contract" {
		t.Fatalf("unexpected done tasks: %+v", plan.Done)
	}
	if len(plan.Invalid) != 0 {
		t.Fatalf("unexpected invalid tasks: %+v", plan.Invalid)
	}
}

func setupSpecTestProject(t *testing.T) (*Service, string, string) {
	t.Helper()
	home := t.TempDir()
	svc := &Service{ForgeHome: filepath.Join(home, ".forge")}
	if _, err := svc.InitWorkspace("main", "Main Workspace", ""); err != nil {
		t.Fatalf("InitWorkspace: %v", err)
	}
	if _, err := svc.InitProject("main", "acme-platform", "Acme Platform"); err != nil {
		t.Fatalf("InitProject: %v", err)
	}

	webRepo := filepath.Join(home, "code", "acme-web")
	apiRepo := filepath.Join(home, "code", "acme-api")
	if err := os.MkdirAll(filepath.Join(webRepo, "src"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(apiRepo, "src"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.LinkRepo("main", "acme-platform", "web", "acme-web", "customer-facing web app", "", webRepo); err != nil {
		t.Fatalf("LinkRepo web: %v", err)
	}
	if _, err := svc.LinkRepo("main", "acme-platform", "api", "acme-api", "core api", "", apiRepo); err != nil {
		t.Fatalf("LinkRepo api: %v", err)
	}
	return svc, webRepo, apiRepo
}

func assertIssueCode(t *testing.T, issues []SpecLintIssue, code string) {
	t.Helper()
	for _, issue := range issues {
		if issue.Code == code {
			return
		}
	}
	t.Fatalf("expected lint issue code %q, got %+v", code, issues)
}

func assertNoIssueCode(t *testing.T, issues []SpecLintIssue, code string) {
	t.Helper()
	for _, issue := range issues {
		if issue.Code == code {
			t.Fatalf("did not expect lint issue code %q, but found: %s", code, issue.Message)
		}
	}
}

func TestLintDetectsMissingScaffoldingTask(t *testing.T) {
	svc, webRepo, _ := setupSpecTestProject(t)
	ctx, err := svc.ResolveContext(webRepo)
	if err != nil {
		t.Fatalf("ResolveContext: %v", err)
	}

	// Two parallel tasks in same repo touching the same new directory — no scaffolding.
	specPath := filepath.Join(ctx.SpecsDir, "scaffolding-missing.md")
	content := `# New payment module
<!-- status: todo -->
<!-- priority: high -->
<!-- created: 2026-04-09 -->
<!-- branch: forge/test-scaffolding -->

**Demo:** User can process payments.

## Expected behavior
- Payments work.

## Test cases

### Scenario: Payment succeeds
Given a user with a valid card
When they submit payment
Then the payment is processed

## Validation plan
- Integration tests.

### Task: Add payment form
<!-- status: todo -->
<!-- parallelizable: yes -->
<!-- deps: none -->
<!-- repo: web -->
<!-- touches: src/payments/form.tsx, src/payments/types.ts -->
<!-- conflict-risk: medium -->

**Done when:**
- Payment form renders.

**Validation:**
- Covers: Scenario "Payment succeeds"

### Task: Add payment API client
<!-- status: todo -->
<!-- parallelizable: yes -->
<!-- deps: none -->
<!-- repo: web -->
<!-- touches: src/payments/api.ts, src/payments/types.ts -->
<!-- conflict-risk: medium -->

**Done when:**
- API client works.

**Validation:**
- Covers: Scenario "Payment succeeds"
`
	if err := os.WriteFile(specPath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	report, err := svc.LintSpecs(webRepo, specPath)
	if err != nil {
		t.Fatalf("LintSpecs: %v", err)
	}
	assertIssueCode(t, report.Issues, "missing_scaffolding_task")
}

func TestLintAcceptsProperScaffoldingPattern(t *testing.T) {
	svc, webRepo, _ := setupSpecTestProject(t)
	ctx, err := svc.ResolveContext(webRepo)
	if err != nil {
		t.Fatalf("ResolveContext: %v", err)
	}

	// Correct pattern: serial scaffolding task + parallel tasks that depend on it.
	specPath := filepath.Join(ctx.SpecsDir, "scaffolding-correct.md")
	content := `# New payment module
<!-- status: todo -->
<!-- priority: high -->
<!-- created: 2026-04-09 -->
<!-- branch: forge/test-scaffolding-ok -->

**Demo:** User can process payments.

## Expected behavior
- Payments work.

## Test cases

### Scenario: Payment succeeds
Given a user with a valid card
When they submit payment
Then the payment is processed

## Validation plan
- Integration tests.

### Task: Scaffold payment module structure
<!-- status: todo -->
<!-- parallelizable: no -->
<!-- deps: none -->
<!-- repo: web -->
<!-- touches: src/payments/** -->
<!-- conflict-risk: low -->

**Done when:**
- Directory structure and shared types exist.

**Validation:**
- Covers: Scenario "Payment succeeds"

### Task: Add payment form
<!-- status: todo -->
<!-- parallelizable: yes -->
<!-- deps: Scaffold payment module structure -->
<!-- repo: web -->
<!-- touches: src/payments/form.tsx -->
<!-- conflict-risk: low -->

**Done when:**
- Payment form renders.

**Validation:**
- Covers: Scenario "Payment succeeds"

### Task: Add payment API client
<!-- status: todo -->
<!-- parallelizable: yes -->
<!-- deps: Scaffold payment module structure -->
<!-- repo: web -->
<!-- touches: src/payments/api.ts -->
<!-- conflict-risk: low -->

**Done when:**
- API client works.

**Validation:**
- Covers: Scenario "Payment succeeds"
`
	if err := os.WriteFile(specPath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	report, err := svc.LintSpecs(webRepo, specPath)
	if err != nil {
		t.Fatalf("LintSpecs: %v", err)
	}
	assertNoIssueCode(t, report.Issues, "missing_scaffolding_task")
}

func TestTouchDirectory(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"internal/forge/run_execute.go", "internal/forge"},
		{"src/payments/form.tsx", "src/payments"},
		{"src/features/auth/**", "src/features"},
		{"README.md", ""},
		{"", ""},
	}
	for _, tt := range tests {
		got := touchDirectory(tt.input)
		if got != tt.want {
			t.Errorf("touchDirectory(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}
