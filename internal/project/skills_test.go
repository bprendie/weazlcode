package project

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDiscoverSkillsFindsSkillMarkdown(t *testing.T) {
	root := t.TempDir()
	skillDir := filepath.Join(root, ".weazlcode", "skills", "go-tests")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	body := `---
name: go-tests
description: Write focused Go tests for changed behavior.
---

# Go Tests
`
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(body), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	skills, err := DiscoverSkills(root, []string{".weazlcode/skills"})
	if err != nil {
		t.Fatalf("DiscoverSkills: %v", err)
	}
	if len(skills) != 1 {
		t.Fatalf("len(skills) = %d, want 1", len(skills))
	}
	if skills[0].Name != "go-tests" || skills[0].Description != "Write focused Go tests for changed behavior." {
		t.Fatalf("skill = %#v", skills[0])
	}
	if skills[0].Source != ".weazlcode/skills" {
		t.Fatalf("Source = %q", skills[0].Source)
	}
}

func TestLoadSkillContentsLoadsSelectedSkills(t *testing.T) {
	root := t.TempDir()
	skillDir := filepath.Join(root, ".weazlcode", "skills", "go-tests")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	body := "name: go-tests\ndescription: Write focused Go tests.\n\nUse table tests."
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(body), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	skills, err := LoadSkillContents(root, []string{".weazlcode/skills"}, []string{"go-tests"}, 12)
	if err != nil {
		t.Fatalf("LoadSkillContents: %v", err)
	}
	if len(skills) != 1 || skills[0].Name != "go-tests" || skills[0].Content != body[:12] || !skills[0].Truncated {
		t.Fatalf("skills = %#v", skills)
	}
}

func TestDiscoverSkillsUsesFirstPathAsPrecedence(t *testing.T) {
	root := t.TempDir()
	for _, base := range []string{".weazlcode/skills", ".agents/skills"} {
		skillDir := filepath.Join(root, base, "style")
		if err := os.MkdirAll(skillDir, 0o755); err != nil {
			t.Fatalf("MkdirAll: %v", err)
		}
		if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("name: style\ndescription: "+base+"\n"), 0o644); err != nil {
			t.Fatalf("WriteFile: %v", err)
		}
	}
	skills, err := DiscoverSkills(root, []string{".weazlcode/skills", ".agents/skills"})
	if err != nil {
		t.Fatalf("DiscoverSkills: %v", err)
	}
	if len(skills) != 1 || skills[0].Source != ".weazlcode/skills" {
		t.Fatalf("skills = %#v", skills)
	}
}

func TestDiscoverSkillsIgnoresMissingDirs(t *testing.T) {
	skills, err := DiscoverSkills(t.TempDir(), []string{".weazlcode/skills"})
	if err != nil {
		t.Fatalf("DiscoverSkills: %v", err)
	}
	if len(skills) != 0 {
		t.Fatalf("skills = %#v, want none", skills)
	}
}
