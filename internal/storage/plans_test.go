package storage

import (
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/bprendie/weazlcode/internal/coding"
)

func TestPlanRoundTrip(t *testing.T) {
	store := testPlanStore(t)
	if err := store.CreateProjectSession("s1", "title", "provider", "model", "/tmp/project"); err != nil {
		t.Fatalf("CreateProjectSession: %v", err)
	}
	plan := coding.Plan{
		ID:          "plan-1",
		SessionID:   "s1",
		ProjectRoot: "/tmp/project",
		Title:       "Build planner",
		Summary:     "Add plan persistence",
		Status:      coding.PlanStatusDraft,
		Tasks: []coding.Task{
			{
				ID:     "task-1",
				PlanID: "plan-1",
				Title:  "Add storage",
				Goal:   "Persist plans and tasks",
				Status: coding.TaskStatusPending,
				InterfaceContract: coding.InterfaceContract{
					Summary: "Storage task API",
					Exports: []string{"Store"},
					Methods: []string{"SavePlan(plan coding.Plan) error"},
				},
				AllowedPaths:   []string{"internal/storage"},
				ContextFiles:   []string{"internal/storage/storage.go"},
				Skills:         []string{"go-tests"},
				DependsOn:      []string{"task-0"},
				Verification:   []string{"go test ./..."},
				ForbiddenPaths: []string{"cmd"},
				AcceptanceChecks: []coding.AcceptanceCheck{
					{Description: "Tests pass", Command: "go test ./..."},
				},
			},
		},
	}
	if err := store.SavePlan(plan); err != nil {
		t.Fatalf("SavePlan: %v", err)
	}

	got, ok, err := store.LatestPlan("s1")
	if err != nil {
		t.Fatalf("LatestPlan: %v", err)
	}
	if !ok {
		t.Fatal("LatestPlan ok = false")
	}
	if got.ID != "plan-1" || got.Title != "Build planner" || len(got.Tasks) != 1 {
		t.Fatalf("plan = %#v", got)
	}
	if got.Tasks[0].AcceptanceChecks[0].Command != "go test ./..." {
		t.Fatalf("task = %#v", got.Tasks[0])
	}
	if len(got.Tasks[0].Skills) != 1 || got.Tasks[0].Skills[0] != "go-tests" {
		t.Fatalf("skills = %#v", got.Tasks[0].Skills)
	}
	if len(got.Tasks[0].DependsOn) != 1 || got.Tasks[0].DependsOn[0] != "task-0" {
		t.Fatalf("depends_on = %#v", got.Tasks[0].DependsOn)
	}
	if got.Tasks[0].InterfaceContract.Summary != "Storage task API" || len(got.Tasks[0].InterfaceContract.Methods) != 1 {
		t.Fatalf("interface contract = %#v", got.Tasks[0].InterfaceContract)
	}
}

func TestTaskEventsRoundTrip(t *testing.T) {
	store := testPlanStore(t)
	if err := store.CreateSession("s1", "title", "provider", "model"); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	plan := coding.Plan{
		ID:        "plan-1",
		SessionID: "s1",
		Title:     "Plan",
		Status:    coding.PlanStatusDraft,
		Tasks: []coding.Task{{
			ID:     "task-1",
			PlanID: "plan-1",
			Title:  "Task",
			Goal:   "Do work",
			Status: coding.TaskStatusPending,
		}},
	}
	if err := store.SavePlan(plan); err != nil {
		t.Fatalf("SavePlan: %v", err)
	}
	payload, _ := json.Marshal(map[string]string{"tool": "git_status"})
	id, err := store.AddTaskEvent(coding.TaskEvent{TaskID: "task-1", Type: "tool", Message: "ran git status", Payload: payload})
	if err != nil {
		t.Fatalf("AddTaskEvent: %v", err)
	}
	if id == 0 {
		t.Fatal("event id = 0")
	}
	events, err := store.TaskEvents("task-1")
	if err != nil {
		t.Fatalf("TaskEvents: %v", err)
	}
	if len(events) != 1 || events[0].Type != "tool" || string(events[0].Payload) == "" {
		t.Fatalf("events = %#v", events)
	}
}

func TestSavePlanQualifiesTaskIDsThatCollideAcrossPlans(t *testing.T) {
	store := testPlanStore(t)
	if err := store.CreateSession("s1", "title", "provider", "model"); err != nil {
		t.Fatalf("CreateSession s1: %v", err)
	}
	if err := store.CreateSession("s2", "title", "provider", "model"); err != nil {
		t.Fatalf("CreateSession s2: %v", err)
	}
	first := coding.Plan{
		ID:        "plan-one",
		SessionID: "s1",
		Title:     "Plan one",
		Status:    coding.PlanStatusDraft,
		Tasks: []coding.Task{{
			ID:     "task-1",
			PlanID: "plan-one",
			Title:  "Task",
			Goal:   "Do work",
			Status: coding.TaskStatusPending,
		}},
	}
	if err := store.SavePlan(first); err != nil {
		t.Fatalf("SavePlan first: %v", err)
	}
	second := coding.Plan{
		ID:        "plan-two",
		SessionID: "s2",
		Title:     "Plan two",
		Status:    coding.PlanStatusDraft,
		Tasks: []coding.Task{
			{
				ID:     "task-1",
				PlanID: "plan-two",
				Title:  "Task one",
				Goal:   "Do one",
				Status: coding.TaskStatusPending,
			},
			{
				ID:        "task-2",
				PlanID:    "plan-two",
				Title:     "Task two",
				Goal:      "Do two",
				Status:    coding.TaskStatusPending,
				DependsOn: []string{"task-1"},
			},
		},
	}
	if err := store.SavePlan(second); err != nil {
		t.Fatalf("SavePlan second: %v", err)
	}
	got, ok, err := store.LatestPlan("s2")
	if err != nil || !ok {
		t.Fatalf("LatestPlan s2: %v ok=%v", err, ok)
	}
	if got.Tasks[0].ID == "task-1" {
		t.Fatalf("colliding task id was not qualified: %#v", got.Tasks)
	}
	if got.Tasks[1].DependsOn[0] != got.Tasks[0].ID {
		t.Fatalf("dependency was not rewritten: %#v", got.Tasks)
	}
}

func TestPlanAndTaskStatusUpdates(t *testing.T) {
	store := testPlanStore(t)
	if err := store.CreateSession("s1", "title", "provider", "model"); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	plan := coding.Plan{
		ID:        "plan-1",
		SessionID: "s1",
		Title:     "Plan",
		Status:    coding.PlanStatusDraft,
		Tasks: []coding.Task{{
			ID:     "task-1",
			PlanID: "plan-1",
			Title:  "Task",
			Goal:   "Do work",
			Status: coding.TaskStatusPending,
		}},
	}
	if err := store.SavePlan(plan); err != nil {
		t.Fatalf("SavePlan: %v", err)
	}
	if err := store.UpdatePlanStatus("plan-1", coding.PlanStatusApproved); err != nil {
		t.Fatalf("UpdatePlanStatus: %v", err)
	}
	if err := store.UpdateTaskStatus("task-1", coding.TaskStatusBlocked); err != nil {
		t.Fatalf("UpdateTaskStatus: %v", err)
	}
	got, ok, err := store.LatestPlan("s1")
	if err != nil {
		t.Fatalf("LatestPlan: %v", err)
	}
	if !ok || got.Status != coding.PlanStatusApproved || got.Tasks[0].Status != coding.TaskStatusBlocked {
		t.Fatalf("plan = %#v ok=%v", got, ok)
	}
	if err := store.UpdatePlanStatus("plan-1", "wat"); err == nil {
		t.Fatal("UpdatePlanStatus accepted bad status")
	}
}

func testPlanStore(t *testing.T) *Store {
	t.Helper()
	store, err := Open(filepath.Join(t.TempDir(), "test.sqlite3"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if err := store.Migrate(); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	return store
}
