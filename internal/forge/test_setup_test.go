package forge

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPlanTestSetupReusesExistingHarnesses(t *testing.T) {
	svc, webRepo, apiRepo := setupSpecTestProject(t)
	writeNodeRepoHarness(t, webRepo, `{
  "name": "acme-web",
  "private": true,
  "scripts": {
    "test:e2e": "playwright test"
  },
  "devDependencies": {
    "@playwright/test": "1.52.0"
  }
}`)
	writeFile(t, filepath.Join(webRepo, "playwright.config.ts"), "export default {}\n")

	writeNodeRepoHarness(t, apiRepo, `{
  "name": "acme-api",
  "private": true,
  "scripts": {
    "test:integration": "vitest run --dir tests/integration"
  },
  "devDependencies": {
    "vitest": "2.0.0",
    "supertest": "7.0.0"
  }
}`)
	mustMkdirAll(t, filepath.Join(apiRepo, "tests", "integration"))

	ctx, err := svc.ResolveContext(webRepo)
	if err != nil {
		t.Fatalf("ResolveContext: %v", err)
	}
	specPath := filepath.Join(ctx.SpecsDir, "test-setup.md")
	writeFile(t, specPath, canonicalValidationSpec())

	plan, err := svc.PlanTestSetup(webRepo, specPath, false)
	if err != nil {
		t.Fatalf("PlanTestSetup: %v", err)
	}
	if len(plan.Items) != 4 {
		t.Fatalf("expected 4 scenario/repo plan items, got %+v", plan.Items)
	}
	assertPlanItem(t, plan.Items, "GitHub login succeeds", "web", "e2e", "playwright", "reuse", "pnpm test:e2e")
	assertPlanItem(t, plan.Items, "GitHub login succeeds", "api", "integration", "vitest", "reuse", "pnpm test:integration")
}

func TestPlanTestSetupCanRecommendBootstrap(t *testing.T) {
	svc, webRepo, _ := setupSpecTestProject(t)
	ctx, err := svc.ResolveContext(webRepo)
	if err != nil {
		t.Fatalf("ResolveContext: %v", err)
	}
	specPath := filepath.Join(ctx.SpecsDir, "bootstrap-test-setup.md")
	writeFile(t, specPath, canonicalValidationSpec())

	plan, err := svc.PlanTestSetup(webRepo, specPath, true)
	if err != nil {
		t.Fatalf("PlanTestSetup: %v", err)
	}
	assertPlanItem(t, plan.Items, "GitHub login succeeds", "web", "e2e", "playwright", "bootstrap", "npx playwright test")
}

func TestApplyTestSetupUpdatesValidationPlanSection(t *testing.T) {
	svc, webRepo, apiRepo := setupSpecTestProject(t)
	writeNodeRepoHarness(t, webRepo, `{
  "name": "acme-web",
  "private": true,
  "scripts": {
    "test:e2e": "playwright test"
  },
  "devDependencies": {
    "@playwright/test": "1.52.0"
  }
}`)
	writeFile(t, filepath.Join(webRepo, "playwright.config.ts"), "export default {}\n")

	writeNodeRepoHarness(t, apiRepo, `{
  "name": "acme-api",
  "private": true,
  "scripts": {
    "test:integration": "vitest run --dir tests/integration"
  },
  "devDependencies": {
    "vitest": "2.0.0",
    "supertest": "7.0.0"
  }
}`)
	mustMkdirAll(t, filepath.Join(apiRepo, "tests", "integration"))

	ctx, err := svc.ResolveContext(webRepo)
	if err != nil {
		t.Fatalf("ResolveContext: %v", err)
	}
	specPath := filepath.Join(ctx.SpecsDir, "apply-validation-plan.md")
	writeFile(t, specPath, canonicalValidationSpec())

	plan, err := svc.ApplyTestSetup(webRepo, specPath, false)
	if err != nil {
		t.Fatalf("ApplyTestSetup: %v", err)
	}
	if !plan.Updated {
		t.Fatalf("expected updated plan")
	}
	data, err := os.ReadFile(specPath)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	text := string(data)
	if !strings.Contains(text, "### Scenario: GitHub login succeeds — web") {
		t.Fatalf("expected validation plan scenario heading, got:\n%s", text)
	}
	if !strings.Contains(text, "- Command: pnpm test:e2e") {
		t.Fatalf("expected e2e command in validation plan, got:\n%s", text)
	}
	report, err := svc.LintSpecs(webRepo, specPath)
	if err != nil {
		t.Fatalf("LintSpecs: %v", err)
	}
	if !report.Valid {
		t.Fatalf("expected updated spec to remain valid, got %+v", report.Issues)
	}
}

func canonicalValidationSpec() string {
	return `# User can sign in with GitHub
<!-- status: todo -->
<!-- priority: high -->
<!-- created: 2026-04-08 -->
<!-- branch: forge/test-validation-setup -->

**What exists:** Email login already exists.

**What's missing:** GitHub OAuth across web and api.

**Demo:** User clicks Continue with GitHub and lands on the dashboard.

## Expected behavior
- Login page shows a GitHub CTA.
- Successful auth lands the user on the dashboard.
- Failed auth shows an error.

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
- Forge should replace this placeholder with repo-aware validation guidance.

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

### Task: GitHub OAuth callback
<!-- status: todo -->
<!-- parallelizable: yes -->
<!-- deps: Shared auth contract -->
<!-- repo: api -->
<!-- touches: src/routes/auth/** -->
<!-- shared-contracts: openapi/auth.yaml, src/shared/auth-types.ts -->
<!-- conflict-risk: medium -->

**Done when:**
- API exchanges code for token.

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
}

func writeNodeRepoHarness(t *testing.T, repoRoot, content string) {
	t.Helper()
	writeFile(t, filepath.Join(repoRoot, "package.json"), content)
	writeFile(t, filepath.Join(repoRoot, "pnpm-lock.yaml"), "lockfileVersion: '9.0'\n")
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	mustMkdirAll(t, filepath.Dir(path))
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile(%s): %v", path, err)
	}
}

func mustMkdirAll(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatalf("MkdirAll(%s): %v", path, err)
	}
}

func assertPlanItem(t *testing.T, items []TestSetupPlanItem, scenario, repoID, layer, runner, strategy, command string) {
	t.Helper()
	for _, item := range items {
		if item.Scenario == scenario && item.RepoID == repoID {
			if item.Layer != layer || item.Runner != runner || item.Strategy != strategy || item.Command != command {
				t.Fatalf("unexpected item for %s/%s: %+v", scenario, repoID, item)
			}
			return
		}
	}
	t.Fatalf("missing item for %s/%s in %+v", scenario, repoID, items)
}
