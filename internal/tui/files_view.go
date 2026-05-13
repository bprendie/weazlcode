package tui

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type filePick struct {
	Path  string
	Size  int64
	Score int
}

func (m model) filesCommandText(query string) string {
	files, err := projectFiles(m.project.Root, 5000)
	if err != nil {
		return "Files error: " + err.Error()
	}
	picks := rankFiles(files, query)
	if len(picks) > 40 {
		picks = picks[:40]
	}
	if len(picks) == 0 {
		return "Files:\nNo files found."
	}
	var b strings.Builder
	if strings.TrimSpace(query) == "" {
		fmt.Fprintf(&b, "Files: %d shown\n\n", len(picks))
	} else {
		fmt.Fprintf(&b, "Files: %d match(es) for %q\n\n", len(picks), query)
	}
	for i, pick := range picks {
		fmt.Fprintf(&b, "%2d. %s  %d bytes\n", i+1, pick.Path, pick.Size)
	}
	b.WriteString("\nPreview:\n")
	b.WriteString(m.previewFileText(picks[0].Path, 80))
	return strings.TrimRight(b.String(), "\n")
}

func (m model) previewCommandText(path string) string {
	if strings.TrimSpace(path) == "" {
		return "Usage: /preview <path>"
	}
	return m.previewFileText(path, 160)
}

func (m model) previewFileText(rel string, maxLines int) string {
	clean, err := cleanPreviewPath(rel)
	if err != nil {
		return "Preview error: " + err.Error()
	}
	full := filepath.Join(m.project.Root, clean)
	info, err := os.Stat(full)
	if err != nil {
		return "Preview error: " + err.Error()
	}
	if info.IsDir() {
		return "Preview error: path is a directory"
	}
	data, err := os.ReadFile(full)
	if err != nil {
		return "Preview error: " + err.Error()
	}
	if !looksPreviewText(data) {
		return fmt.Sprintf("Preview: %s\n%d bytes\nbinary or non-text file", clean, info.Size())
	}
	lines := strings.Split(string(data), "\n")
	if maxLines <= 0 {
		maxLines = 120
	}
	end := min(len(lines), maxLines)
	var b strings.Builder
	fmt.Fprintf(&b, "Preview: %s\n%d bytes\n\n", clean, info.Size())
	for i := 0; i < end; i++ {
		fmt.Fprintf(&b, "%4d  %s\n", i+1, lines[i])
	}
	if len(lines) > end {
		fmt.Fprintf(&b, "\n[truncated: %d lines omitted]", len(lines)-end)
	}
	return strings.TrimRight(b.String(), "\n")
}

func projectFiles(root string, limit int) ([]filePick, error) {
	if strings.TrimSpace(root) == "" {
		return nil, fmt.Errorf("project root is required")
	}
	var files []filePick
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if path == root {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if d.IsDir() {
			if skipPickerDir(rel) {
				return filepath.SkipDir
			}
			return nil
		}
		if skipPickerFile(rel) {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		files = append(files, filePick{Path: rel, Size: info.Size()})
		if limit > 0 && len(files) >= limit {
			return filepath.SkipAll
		}
		return nil
	})
	return files, err
}

func rankFiles(files []filePick, query string) []filePick {
	query = strings.ToLower(strings.TrimSpace(query))
	picks := make([]filePick, 0, len(files))
	for _, file := range files {
		score := fuzzyFileScore(strings.ToLower(file.Path), query)
		if query != "" && score == 0 {
			continue
		}
		file.Score = score
		picks = append(picks, file)
	}
	sort.Slice(picks, func(i, j int) bool {
		if picks[i].Score != picks[j].Score {
			return picks[i].Score > picks[j].Score
		}
		return picks[i].Path < picks[j].Path
	})
	return picks
}

func fuzzyFileScore(path, query string) int {
	if query == "" {
		return 1
	}
	if path == query {
		return 1000
	}
	if strings.Contains(path, query) {
		return 500 + len(query)
	}
	next := 0
	score := 0
	streak := 0
	for _, r := range path {
		if next >= len(query) {
			break
		}
		if byte(r) == query[next] {
			next++
			streak++
			score += 10 + streak
			continue
		}
		streak = 0
	}
	if next != len(query) {
		return 0
	}
	return score
}

func skipPickerDir(rel string) bool {
	base := filepath.Base(rel)
	switch base {
	case ".git", ".weazlcode", ".gocache", ".gomodcache", "node_modules", "vendor":
		return true
	default:
		return false
	}
}

func skipPickerFile(rel string) bool {
	base := filepath.Base(rel)
	return strings.HasPrefix(base, ".") && base != ".gitignore" && base != ".weazlcodeignore"
}

func cleanPreviewPath(path string) (string, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return "", fmt.Errorf("path is required")
	}
	clean := filepath.ToSlash(filepath.Clean(path))
	if filepath.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, "../") {
		return "", fmt.Errorf("path must stay inside project root")
	}
	return clean, nil
}

func looksPreviewText(data []byte) bool {
	for _, b := range data {
		if b == 0 {
			return false
		}
	}
	return true
}
