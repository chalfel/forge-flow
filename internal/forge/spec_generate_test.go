package forge

import (
	"strings"
	"testing"
)

func TestBuildSpecGenSystemPrompt(t *testing.T) {
	svc, webRepo := setupRunTestProject(t)
	ctx, err := svc.ResolveContext(webRepo)
	if err != nil {
		t.Fatalf("ResolveContext: %v", err)
	}

	prompt := buildSpecGenSystemPrompt(ctx)

	if !strings.Contains(prompt, "Forge spec generator") {
		t.Fatal("expected role statement in system prompt")
	}
	if !strings.Contains(prompt, "Scaffolding Pattern") {
		t.Fatal("expected scaffolding pattern rule")
	}
	if !strings.Contains(prompt, "parallelizable: no") {
		t.Fatal("expected scaffolding example")
	}
	if !strings.Contains(prompt, "Spec Format Rules") {
		t.Fatal("expected format rules")
	}
	if !strings.Contains(prompt, "forge spec lint") {
		t.Fatal("expected lint instruction")
	}
}

func TestBuildSpecGenTaskPrompt(t *testing.T) {
	ctx := ResolvedContext{
		SpecsDir: "/project/specs",
	}

	prompt := buildSpecGenTaskPrompt(ctx, "user can export data to CSV")

	if !strings.Contains(prompt, "user can export data to CSV") {
		t.Fatal("expected capability in prompt")
	}
	if !strings.Contains(prompt, "/project/specs/") {
		t.Fatal("expected specs dir in prompt")
	}
	if !strings.Contains(prompt, "scaffolding pattern") {
		t.Fatal("expected scaffolding reminder")
	}
}

func TestBuildSpecGenSystemPromptIncludesExistingSpecs(t *testing.T) {
	svc, webRepo := setupRunTestProject(t)
	ctx, err := svc.ResolveContext(webRepo)
	if err != nil {
		t.Fatalf("ResolveContext: %v", err)
	}

	prompt := buildSpecGenSystemPrompt(ctx)

	// The test project has a spec file created by setupRunTestProject
	if !strings.Contains(prompt, "Existing Specs") {
		t.Fatal("expected existing specs section")
	}
	if !strings.Contains(prompt, "test-run-spec.md") {
		t.Fatal("expected test spec filename in existing specs list")
	}
}
