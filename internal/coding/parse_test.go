package coding

import (
	"strings"
	"testing"
)

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

func TestPrepareImportedPlanNormalizesProjectPaths(t *testing.T) {
	plan := Plan{
		Title:   "Plan",
		Summary: "Summary",
		Tasks: []Task{{
			Title:            "Task",
			Goal:             "Do work",
			AllowedPaths:     []string{"/tmp/project/README.md"},
			ForbiddenPaths:   []string{"/tmp/project/secrets"},
			ContextFiles:     []string{"/tmp/project/internal/app.go"},
			Skills:           []string{"go-tests", "go-tests", ""},
			DependsOn:        []string{"task-0", "task-0", ""},
			Verification:     []string{"go test ./..."},
			AcceptanceChecks: []AcceptanceCheck{{Description: "done"}},
		}},
	}
	got := PrepareImportedPlan(plan, "session-1", "/tmp/project", func() string { return "id" })
	task := got.Tasks[0]
	if task.AllowedPaths[0] != "README.md" || task.ForbiddenPaths[0] != "secrets" || task.ContextFiles[0] != "internal/app.go" {
		t.Fatalf("task paths = %#v", task)
	}
	if len(task.Skills) != 1 || task.Skills[0] != "go-tests" {
		t.Fatalf("skills = %#v", task.Skills)
	}
	if len(task.DependsOn) != 1 || task.DependsOn[0] != "task-0" {
		t.Fatalf("depends_on = %#v", task.DependsOn)
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

func TestParseWorkerPatchJSONAllowsFileEdits(t *testing.T) {
	patch, err := ParseWorkerPatchJSON([]byte(`{"task_id":"task-1","summary":"changed","patch":"","files":[{"path":" README.md ","content":"new\n"}]}`))
	if err != nil {
		t.Fatalf("ParseWorkerPatchJSON: %v", err)
	}
	if len(patch.Files) != 1 || patch.Files[0].Path != "README.md" || patch.Files[0].Content != "new\n" {
		t.Fatalf("patch = %#v", patch)
	}
}

func TestParseWorkerPatchJSONStripsWholeFileMarkdownFence(t *testing.T) {
	raw := "{\"task_id\":\"task-1\",\"summary\":\"changed\",\"patch\":\"\",\"files\":[{\"path\":\"app.py\",\"content\":\"```python\\nprint('ok')\\n```\\n\"}]}"
	patch, err := ParseWorkerPatchJSON([]byte(raw))
	if err != nil {
		t.Fatalf("ParseWorkerPatchJSON: %v", err)
	}
	if len(patch.Files) != 1 || patch.Files[0].Content != "print('ok')\n" {
		t.Fatalf("patch = %#v", patch)
	}
}

func TestParseWorkerPatchJSONRecoversInvalidStringEscapes(t *testing.T) {
	raw := `{"task_id":"task-1","summary":"changed","patch":"-\.card\n+\.card\n","files":[{"path":"styles.css","content":".card {\n  color: red;\n}\n"}]}`
	patch, err := ParseWorkerPatchJSON([]byte(raw))
	if err != nil {
		t.Fatalf("ParseWorkerPatchJSON: %v", err)
	}
	if len(patch.Files) != 1 || !strings.Contains(patch.Files[0].Content, ".card") {
		t.Fatalf("patch = %#v", patch)
	}
}

func TestParseWorkerPatchJSONRecoversLooseFileObjects(t *testing.T) {
	raw := `{"task_id":"task-1","summary":"changed","patch":"","files":[{"path":"index.html","content":"<main>ok</main>\n"},"path":"styles.css","content":"body { color: red; }\n"}`
	patch, err := ParseWorkerPatchJSON([]byte(raw))
	if err != nil {
		t.Fatalf("ParseWorkerPatchJSON: %v", err)
	}
	if len(patch.Files) != 2 {
		t.Fatalf("files = %#v", patch.Files)
	}
	if patch.Files[0].Path != "index.html" || patch.Files[1].Path != "styles.css" {
		t.Fatalf("files = %#v", patch.Files)
	}
}

func TestParseWorkerPatchJSONNormalizesNoBlockerSentinels(t *testing.T) {
	patch, err := ParseWorkerPatchJSON([]byte(`{"task_id":" task-1 ","summary":" changed ","patch":"diff --git a/a b/a","blocker":"none"}`))
	if err != nil {
		t.Fatalf("ParseWorkerPatchJSON: %v", err)
	}
	if patch.TaskID != "task-1" || patch.Summary != "changed" || patch.Blocker != "" {
		t.Fatalf("patch = %#v", patch)
	}
	if _, err := ParseWorkerPatchJSON([]byte(`{"task_id":"task-1","summary":"changed","patch":"","blocker":"none"}`)); err == nil {
		t.Fatal("ParseWorkerPatchJSON returned nil error for empty patch with no real blocker")
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
