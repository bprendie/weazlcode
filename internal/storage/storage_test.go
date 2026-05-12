package storage

import (
	"path/filepath"
	"testing"
)

func TestMemoryRoundTrip(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "test.sqlite3"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer store.Close()
	if err := store.Migrate(); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	if err := store.CreateVault("pw"); err != nil {
		t.Fatalf("CreateVault: %v", err)
	}
	if err := store.Remember("project", "WeazlCode uses local tools.", "chat,tools"); err != nil {
		t.Fatalf("Remember: %v", err)
	}

	memories, err := store.RecallMemories("local tools", 10)
	if err != nil {
		t.Fatalf("RecallMemories: %v", err)
	}
	if len(memories) != 1 {
		t.Fatalf("len(memories) = %d, want 1", len(memories))
	}
	if memories[0].Key != "project" || memories[0].Value != "WeazlCode uses local tools." {
		t.Fatalf("memory = %#v", memories[0])
	}

	if err := store.ForgetMemory("project"); err != nil {
		t.Fatalf("ForgetMemory: %v", err)
	}
	memories, err = store.Memories(10)
	if err != nil {
		t.Fatalf("Memories: %v", err)
	}
	if len(memories) != 0 {
		t.Fatalf("len(memories) = %d, want 0", len(memories))
	}
}

func TestContextCheckpointRoundTrip(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "test.sqlite3"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer store.Close()
	if err := store.Migrate(); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	if err := store.CreateVault("pw"); err != nil {
		t.Fatalf("CreateVault: %v", err)
	}
	if err := store.CreateSession("s1", "title", "provider", "model"); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	if err := store.AddMessage("s1", "user", "hello"); err != nil {
		t.Fatalf("AddMessage: %v", err)
	}
	messages, err := store.Messages("s1")
	if err != nil {
		t.Fatalf("Messages: %v", err)
	}
	if err := store.SaveContextCheckpoint("s1", messages[0].ID, "summary"); err != nil {
		t.Fatalf("SaveContextCheckpoint: %v", err)
	}
	cp, ok, err := store.LatestContextCheckpoint("s1")
	if err != nil {
		t.Fatalf("LatestContextCheckpoint: %v", err)
	}
	if !ok {
		t.Fatal("LatestContextCheckpoint ok = false")
	}
	if cp.Summary != "summary" || cp.ThroughMessageID != messages[0].ID {
		t.Fatalf("checkpoint = %#v", cp)
	}
	after, err := store.MessagesAfter("s1", cp.ThroughMessageID)
	if err != nil {
		t.Fatalf("MessagesAfter: %v", err)
	}
	if len(after) != 0 {
		t.Fatalf("len(after) = %d, want 0", len(after))
	}
}

func TestClearSessionContext(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "test.sqlite3"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer store.Close()
	if err := store.Migrate(); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	if err := store.CreateVault("pw"); err != nil {
		t.Fatalf("CreateVault: %v", err)
	}
	if err := store.CreateSession("s1", "title", "provider", "model"); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	if err := store.AddMessage("s1", "user", "hello"); err != nil {
		t.Fatalf("AddMessage: %v", err)
	}
	messages, err := store.Messages("s1")
	if err != nil {
		t.Fatalf("Messages: %v", err)
	}
	if err := store.SaveContextCheckpoint("s1", messages[0].ID, "summary"); err != nil {
		t.Fatalf("SaveContextCheckpoint: %v", err)
	}
	if err := store.AddSessionTokens("s1", 10, 20); err != nil {
		t.Fatalf("AddSessionTokens: %v", err)
	}

	if err := store.ClearSessionContext("s1"); err != nil {
		t.Fatalf("ClearSessionContext: %v", err)
	}
	messages, err = store.Messages("s1")
	if err != nil {
		t.Fatalf("Messages after clear: %v", err)
	}
	if len(messages) != 0 {
		t.Fatalf("len(messages) = %d, want 0", len(messages))
	}
	if _, ok, err := store.LatestContextCheckpoint("s1"); err != nil {
		t.Fatalf("LatestContextCheckpoint after clear: %v", err)
	} else if ok {
		t.Fatal("LatestContextCheckpoint ok = true, want false")
	}
	sess, ok, err := store.Session("s1")
	if err != nil {
		t.Fatalf("Session: %v", err)
	}
	if !ok {
		t.Fatal("Session ok = false, want true")
	}
	if sess.Title != "New session" || sess.InputTokens != 0 || sess.OutputTokens != 0 {
		t.Fatalf("session after clear = %#v, want reset title/tokens", sess)
	}
}

func TestRenameWorkspace(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "test.sqlite3"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer store.Close()
	if err := store.Migrate(); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	if err := store.CreateVault("pw"); err != nil {
		t.Fatalf("CreateVault: %v", err)
	}
	if err := store.CreateSession("s1", "title", "provider", "model"); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	id, err := store.SaveWorkspace("old name", "s1", "snapshot", 0)
	if err != nil {
		t.Fatalf("SaveWorkspace: %v", err)
	}
	if err := store.RenameWorkspace(id, "  new   name  "); err != nil {
		t.Fatalf("RenameWorkspace: %v", err)
	}
	saves, err := store.WorkspaceSaves(10)
	if err != nil {
		t.Fatalf("WorkspaceSaves: %v", err)
	}
	if len(saves) != 1 || saves[0].Name != "new name" {
		t.Fatalf("saves = %#v, want renamed workspace", saves)
	}
	if err := store.UpdateWorkspace(id, "s1", "updated snapshot", 0); err != nil {
		t.Fatalf("UpdateWorkspace: %v", err)
	}
	saves, err = store.WorkspaceSaves(10)
	if err != nil {
		t.Fatalf("WorkspaceSaves after update: %v", err)
	}
	if len(saves) != 1 || saves[0].Name != "new name" {
		t.Fatalf("saves after update = %#v, want name preserved", saves)
	}
	if err := store.DeleteWorkspace(id); err != nil {
		t.Fatalf("DeleteWorkspace: %v", err)
	}
	saves, err = store.WorkspaceSaves(10)
	if err != nil {
		t.Fatalf("WorkspaceSaves after delete: %v", err)
	}
	if len(saves) != 0 {
		t.Fatalf("len(saves) = %d, want 0 after delete", len(saves))
	}
}
