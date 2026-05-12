package tui

import (
	"fmt"
	"strings"
)

type diffFileView struct {
	OldPath string
	NewPath string
	Added   int
	Removed int
	Hunks   []diffHunkView
	Meta    []string
}

type diffHunkView struct {
	Header string
	Lines  []string
}

func renderDiffView(raw string) string {
	files := parseDiffView(raw)
	if len(files) == 0 {
		return "Diff:\nNo changes."
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Diff: %d file(s)\n\nFiles:\n", len(files))
	for i, file := range files {
		fmt.Fprintf(&b, "%d. %s  +%d -%d\n", i+1, diffDisplayPath(file), file.Added, file.Removed)
	}
	for _, file := range files {
		fmt.Fprintf(&b, "\n--- %s  +%d -%d\n", diffDisplayPath(file), file.Added, file.Removed)
		for _, meta := range file.Meta {
			fmt.Fprintf(&b, "%s\n", meta)
		}
		if len(file.Hunks) == 0 {
			continue
		}
		for _, hunk := range file.Hunks {
			fmt.Fprintf(&b, "%s\n", hunk.Header)
			for _, line := range hunk.Lines {
				fmt.Fprintf(&b, "%s\n", line)
			}
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

func parseDiffView(raw string) []diffFileView {
	var files []diffFileView
	var current *diffFileView
	var hunk *diffHunkView
	for _, line := range strings.Split(raw, "\n") {
		if strings.HasPrefix(line, "diff --git ") {
			files = append(files, diffFileView{})
			current = &files[len(files)-1]
			hunk = nil
			parts := strings.Fields(line)
			if len(parts) >= 4 {
				current.OldPath = trimDiffPrefix(parts[2])
				current.NewPath = trimDiffPrefix(parts[3])
			}
			continue
		}
		if current == nil {
			continue
		}
		switch {
		case strings.HasPrefix(line, "--- "):
			current.OldPath = trimDiffHeaderPath(strings.TrimSpace(strings.TrimPrefix(line, "--- ")))
		case strings.HasPrefix(line, "+++ "):
			current.NewPath = trimDiffHeaderPath(strings.TrimSpace(strings.TrimPrefix(line, "+++ ")))
		case strings.HasPrefix(line, "@@"):
			current.Hunks = append(current.Hunks, diffHunkView{Header: line})
			hunk = &current.Hunks[len(current.Hunks)-1]
		case hunk != nil:
			hunk.Lines = append(hunk.Lines, line)
			if strings.HasPrefix(line, "+") && !strings.HasPrefix(line, "+++") {
				current.Added++
			}
			if strings.HasPrefix(line, "-") && !strings.HasPrefix(line, "---") {
				current.Removed++
			}
		case strings.TrimSpace(line) != "":
			current.Meta = append(current.Meta, line)
		}
	}
	return files
}

func trimDiffHeaderPath(path string) string {
	if path == "/dev/null" {
		return path
	}
	if i := strings.IndexAny(path, "\t "); i >= 0 {
		path = path[:i]
	}
	return trimDiffPrefix(path)
}

func trimDiffPrefix(path string) string {
	path = strings.TrimPrefix(path, "a/")
	path = strings.TrimPrefix(path, "b/")
	path = strings.TrimPrefix(path, "i/")
	path = strings.TrimPrefix(path, "w/")
	return path
}

func diffDisplayPath(file diffFileView) string {
	if file.NewPath != "" && file.NewPath != "/dev/null" {
		return file.NewPath
	}
	if file.OldPath != "" {
		return file.OldPath
	}
	return "(unknown)"
}
