package forge

import "strings"

const (
	specSectionNone             = ""
	specSectionExpectedBehavior = "expected_behavior"
	specSectionTestCases        = "test_cases"
	specSectionValidationPlan   = "validation_plan"
)

type ForgeSpecScenario struct {
	Name  string              `json:"name"`
	Line  int                 `json:"line"`
	Steps []ForgeScenarioStep `json:"steps,omitempty"`
}

type ForgeScenarioStep struct {
	Keyword string `json:"keyword"`
	Text    string `json:"text"`
}

func normalizeSpecSectionHeading(line string) string {
	heading := strings.ToLower(strings.TrimSpace(strings.TrimPrefix(line, "## ")))
	switch heading {
	case "expected behavior":
		return specSectionExpectedBehavior
	case "test cases":
		return specSectionTestCases
	case "validation plan":
		return specSectionValidationPlan
	default:
		return specSectionNone
	}
}

func parseScenarioStep(line string) (ForgeScenarioStep, bool) {
	for _, keyword := range []string{"Given", "When", "Then", "And", "But"} {
		prefix := keyword + " "
		if !strings.HasPrefix(line, prefix) {
			continue
		}
		text := strings.TrimSpace(strings.TrimPrefix(line, prefix))
		if text == "" {
			return ForgeScenarioStep{}, false
		}
		return ForgeScenarioStep{Keyword: strings.ToLower(keyword), Text: text}, true
	}
	return ForgeScenarioStep{}, false
}

func scenarioHasKeyword(scenario ForgeSpecScenario, keyword string) bool {
	for _, step := range scenario.Steps {
		if step.Keyword == keyword {
			return true
		}
	}
	return false
}

func extractCoveredScenario(validationLine string) string {
	line := strings.TrimSpace(validationLine)
	if !strings.HasPrefix(strings.ToLower(line), "covers:") {
		return ""
	}
	line = strings.TrimSpace(strings.TrimPrefix(line, "Covers:"))
	line = strings.TrimSpace(strings.TrimPrefix(line, "covers:"))
	line = strings.TrimSpace(strings.TrimPrefix(line, "Scenario"))
	line = strings.TrimSpace(strings.TrimPrefix(line, ":"))
	line = strings.Trim(line, "\"'")
	return strings.TrimSpace(line)
}
