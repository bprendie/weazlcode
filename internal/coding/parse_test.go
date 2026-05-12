package coding

import "testing"

func TestParsePlanJSONRejectsUnknownFields(t *testing.T) {
	raw := []byte(`{"id":"p","session_id":"s","title":"t","status":"draft","unknown":true}`)
	if _, err := ParsePlanJSON(raw); err == nil {
		t.Fatal("ParsePlanJSON returned nil error for unknown field")
	}
}

func TestPrepareImportedPlanDefaultsRuntimeFields(t *testing.T) {
	plan := Plan{
		Title:   "Plan",
		Summary: "Summary",
		Tasks: []Task{{
			Title: "Task",
			Goal:  "Do work",
		}},
	}
	next := 0
	got := PrepareImportedPlan(plan, "session-1", "/tmp/project", func() string {
		next++
		if next == 1 {
			return "plan-1"
		}
		return "task-1"
	})
	if got.ID != "plan-1" || got.SessionID != "session-1" || got.ProjectRoot != "/tmp/project" || got.Status != PlanStatusDraft {
		t.Fatalf("plan = %#v", got)
	}
	if got.Tasks[0].ID != "task-1" || got.Tasks[0].PlanID != "plan-1" || got.Tasks[0].Status != TaskStatusPending {
		t.Fatalf("task = %#v", got.Tasks[0])
	}
	if err := ValidatePlan(got); err != nil {
		t.Fatalf("ValidatePlan: %v", err)
	}
}

func TestParseWorkerPatchJSON(t *testing.T) {
	patch, err := ParseWorkerPatchJSON([]byte(`{"task_id":"task-1","summary":"changed","patch":"diff --git a/a b/a"}`))
	if err != nil {
		t.Fatalf("ParseWorkerPatchJSON: %v", err)
	}
	if patch.TaskID != "task-1" {
		t.Fatalf("patch = %#v", patch)
	}
}

func TestParseReviewVerdictJSON(t *testing.T) {
	if _, err := ParseReviewVerdictJSON([]byte(`{"verdict":"approve","summary":"ok"}`)); err != nil {
		t.Fatalf("ParseReviewVerdictJSON: %v", err)
	}
	if _, err := ParseReviewVerdictJSON([]byte(`{"verdict":"maybe","summary":"ok"}`)); err == nil {
		t.Fatal("ParseReviewVerdictJSON returned nil error for bad verdict")
	}
}
