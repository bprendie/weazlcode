package coding

import (
	"strings"
	"testing"
)

func TestValidatePlanQualityAcceptsConcreteTask(t *testing.T) {
	plan := Plan{Tasks: []Task{{
		ID:           "task-1",
		Title:        "Update README",
		Goal:         "Update README.md to document the Phase 4 setup smoke result.",
		AllowedPaths: []string{"README.md"},
		AcceptanceChecks: []AcceptanceCheck{
			{Description: "README mentions the Phase 4 setup smoke result"},
		},
	}}}

	if issues := ValidatePlanQuality(plan); len(issues) != 0 {
		t.Fatalf("issues = %#v, want none", issues)
	}
}

func TestValidatePlanQualityFlagsVagueAndUnboundedTask(t *testing.T) {
	plan := Plan{Tasks: []Task{{
		ID:     "task-1",
		Title:  "Loose task",
		Goal:   "do work",
		Status: TaskStatusPending,
	}}}

	issues := ValidatePlanQuality(plan)
	for _, want := range []string{"goal is too vague", "allowed_paths is empty", "acceptance_checks is empty"} {
		if !qualityIssuesContain(issues, want) {
			t.Fatalf("issues missing %q: %#v", want, issues)
		}
	}
}

func TestValidatePlanQualityFlagsBroadPathScope(t *testing.T) {
	plan := Plan{Tasks: []Task{{
		ID:           "task-1",
		Title:        "Broad task",
		Goal:         "Update the repository documentation to describe the setup flow.",
		AllowedPaths: []string{"."},
		AcceptanceChecks: []AcceptanceCheck{
			{Description: "Documentation describes the setup flow"},
		},
	}}}

	issues := ValidatePlanQuality(plan)
	if !qualityIssuesContain(issues, "allowed_paths is too broad") {
		t.Fatalf("issues = %#v, want broad path issue", issues)
	}
}

func qualityIssuesContain(issues []PlanQualityIssue, want string) bool {
	for _, issue := range issues {
		if strings.Contains(issue.Message, want) {
			return true
		}
	}
	return false
}
