package coding

import (
	"os"
	"path/filepath"
	"testing"
)

func TestBuildTaskPacketPacksContext(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "internal"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "internal", "file.go"), []byte("one\ntwo\nthree\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	task := Task{
		ID:           "task-1",
		PlanID:       "plan-1",
		Title:        "Task",
		Goal:         "Edit file",
		Status:       TaskStatusPending,
		AllowedPaths: []string{"internal"},
		ContextFiles: []string{"internal/file.go"},
	}
	packet, err := BuildTaskPacket(task, ContextPackOptions{
		ProjectRoot: root,
		Skills: []SkillContext{{
			Name:    "go-tests",
			Path:    "/skills/go-tests/SKILL.md",
			Content: "Use focused tests.",
		}},
		LineRangeByFile: map[string]LineRange{
			"internal/file.go": {StartLine: 2, EndLine: 3},
		},
	})
	if err != nil {
		t.Fatalf("BuildTaskPacket: %v", err)
	}
	if packet.Role != "worker" || packet.TaskID != "task-1" || len(packet.ContextFiles) != 1 {
		t.Fatalf("packet = %#v", packet)
	}
	if packet.ContextFiles[0].Content != "two\nthree" {
		t.Fatalf("content = %q", packet.ContextFiles[0].Content)
	}
	if packet.ContextPolicy.Mode != "tool_requested" || len(packet.ContextPolicy.RequestTools) == 0 {
		t.Fatalf("context policy = %#v", packet.ContextPolicy)
	}
	if len(packet.Skills) != 1 || packet.Skills[0].Name != "go-tests" {
		t.Fatalf("skills = %#v", packet.Skills)
	}
}

func TestBuildTaskPacketPacksContextRangeSpecs(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "internal"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "internal", "file.go"), []byte("one\ntwo\nthree\nfour\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	task := Task{
		ID:           "task-1",
		PlanID:       "plan-1",
		Title:        "Task",
		Goal:         "Edit file",
		Status:       TaskStatusPending,
		AllowedPaths: []string{"internal/file.go"},
		ContextFiles: []string{"internal/file.go#L2-L3"},
	}
	packet, err := BuildTaskPacket(task, ContextPackOptions{ProjectRoot: root})
	if err != nil {
		t.Fatalf("BuildTaskPacket: %v", err)
	}
	if len(packet.ContextFiles) != 1 || packet.ContextFiles[0].Path != "internal/file.go" {
		t.Fatalf("context files = %#v", packet.ContextFiles)
	}
	if packet.ContextFiles[0].StartLine != 2 || packet.ContextFiles[0].EndLine != 3 || packet.ContextFiles[0].Content != "two\nthree" {
		t.Fatalf("context file = %#v", packet.ContextFiles[0])
	}
}

func TestBuildTaskPacketRejectsOutsideContext(t *testing.T) {
	task := Task{
		ID:           "task-1",
		PlanID:       "plan-1",
		Title:        "Task",
		Goal:         "Edit file",
		Status:       TaskStatusPending,
		AllowedPaths: []string{"."},
		ContextFiles: []string{"../secret"},
	}
	_, err := BuildTaskPacket(task, ContextPackOptions{ProjectRoot: t.TempDir()})
	if err == nil {
		t.Fatal("BuildTaskPacket returned nil error for outside context")
	}
}

func TestValidateWorkerPatch(t *testing.T) {
	if err := ValidateWorkerPatch(WorkerPatch{TaskID: "task-1", Patch: "diff --git ..."}); err != nil {
		t.Fatalf("ValidateWorkerPatch: %v", err)
	}
	if err := ValidateWorkerPatch(WorkerPatch{TaskID: "task-1", Blocker: "missing context"}); err != nil {
		t.Fatalf("ValidateWorkerPatch blocker: %v", err)
	}
	if err := ValidateWorkerPatch(WorkerPatch{TaskID: "task-1"}); err == nil {
		t.Fatal("ValidateWorkerPatch returned nil error without patch or blocker")
	}
}
