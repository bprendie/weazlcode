package coding

import "testing"

func TestValidatePlan(t *testing.T) {
	plan := Plan{
		ID:        "plan-1",
		SessionID: "session-1",
		Title:     "Implement feature",
		Status:    PlanStatusDraft,
		Tasks: []Task{
			{
				ID:     "task-1",
				PlanID: "plan-1",
				Title:  "Add types",
				Goal:   "Create the task types",
				Status: TaskStatusPending,
				AcceptanceChecks: []AcceptanceCheck{
					{Description: "Tests pass", Command: "go test ./..."},
				},
			},
		},
	}
	if err := ValidatePlan(plan); err != nil {
		t.Fatalf("ValidatePlan: %v", err)
	}
}

func TestValidatePlanRejectsBadStatus(t *testing.T) {
	plan := Plan{ID: "plan-1", SessionID: "session-1", Title: "x", Status: "wat"}
	if err := ValidatePlan(plan); err == nil {
		t.Fatal("ValidatePlan returned nil error for bad status")
	}
}

func TestValidateReviewVerdict(t *testing.T) {
	if err := ValidateReviewVerdict(ReviewVerdict{Verdict: ReviewNeedsFix}); err != nil {
		t.Fatalf("ValidateReviewVerdict: %v", err)
	}
	if err := ValidateReviewVerdict(ReviewVerdict{Verdict: "maybe"}); err == nil {
		t.Fatal("ValidateReviewVerdict returned nil error for bad verdict")
	}
}
