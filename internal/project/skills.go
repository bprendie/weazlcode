package project

import (
	"bufio"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type Skill struct {
	Name        string
	Description string
	Path        string
	Source      string
	Content     string
	Truncated   bool
}

func DiscoverSkills(root string, paths []string) ([]Skill, error) {
	seenDirs := map[string]bool{}
	skillsByName := map[string]Skill{}
	for _, base := range paths {
		base = strings.TrimSpace(base)
		if base == "" {
			continue
		}
		if !filepath.IsAbs(base) {
			base = filepath.Join(root, base)
		}
		base = filepath.Clean(base)
		if seenDirs[base] {
			continue
		}
		seenDirs[base] = true
		found, err := discoverSkillsInDir(base)
		if err != nil {
			return nil, err
		}
		for _, skill := range found {
			skill.Source = skillSource(root, base)
			if _, exists := skillsByName[skill.Name]; exists {
				continue
			}
			skillsByName[skill.Name] = skill
		}
	}
	skills := make([]Skill, 0, len(skillsByName))
	for _, skill := range skillsByName {
		skills = append(skills, skill)
	}
	sort.Slice(skills, func(i, j int) bool {
		if skills[i].Source == skills[j].Source {
			return skills[i].Name < skills[j].Name
		}
		return skills[i].Source < skills[j].Source
	})
	return skills, nil
}

func LoadSkillContents(root string, paths, names []string, maxChars int) ([]Skill, error) {
	if maxChars <= 0 {
		maxChars = 12000
	}
	discovered, err := DiscoverSkills(root, paths)
	if err != nil {
		return nil, err
	}
	byName := map[string]Skill{}
	for _, skill := range discovered {
		byName[skill.Name] = skill
	}
	out := make([]Skill, 0, len(names))
	for _, name := range names {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		skill, ok := byName[name]
		if !ok {
			return nil, &UnknownSkillError{Name: name}
		}
		data, err := os.ReadFile(skill.Path)
		if err != nil {
			return nil, err
		}
		content := string(data)
		if len(content) > maxChars {
			content = content[:maxChars]
			skill.Truncated = true
		}
		skill.Content = content
		out = append(out, skill)
	}
	return out, nil
}

type UnknownSkillError struct {
	Name string
}

func (e *UnknownSkillError) Error() string {
	return "unknown skill " + e.Name
}

func discoverSkillsInDir(base string) ([]Skill, error) {
	entries, err := os.ReadDir(base)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var skills []Skill
	for _, entry := range entries {
		if !entry.IsDir() || strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		path := filepath.Join(base, entry.Name(), "SKILL.md")
		data, err := os.ReadFile(path)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, err
		}
		name, description := parseSkillHeader(entry.Name(), string(data))
		skills = append(skills, Skill{
			Name:        name,
			Description: description,
			Path:        path,
		})
	}
	return skills, nil
}

func parseSkillHeader(fallbackName, body string) (string, string) {
	name := strings.TrimSpace(fallbackName)
	description := ""
	scanner := bufio.NewScanner(strings.NewReader(body))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || line == "---" {
			continue
		}
		lower := strings.ToLower(line)
		switch {
		case strings.HasPrefix(lower, "name:"):
			name = cleanSkillHeaderValue(line[len("name:"):])
		case strings.HasPrefix(lower, "description:"):
			description = cleanSkillHeaderValue(line[len("description:"):])
		case strings.HasPrefix(line, "#") && name == fallbackName:
			name = strings.TrimSpace(strings.TrimLeft(line, "#"))
		case description == "" && !strings.HasPrefix(line, "#"):
			description = strings.TrimSpace(line)
		}
		if name != "" && description != "" {
			break
		}
	}
	return name, description
}

func cleanSkillHeaderValue(value string) string {
	value = strings.TrimSpace(value)
	value = strings.Trim(value, `"'`)
	return value
}

func skillSource(root, base string) string {
	if rel, err := filepath.Rel(root, base); err == nil && !strings.HasPrefix(rel, "..") && rel != "." {
		return filepath.ToSlash(rel)
	}
	return base
}
