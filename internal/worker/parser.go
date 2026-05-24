package worker

import (
	"fmt"
	"strings"

	"github.com/bprendie/weazlcode/internal/coding"
)

// FileBlock represents a parsed file block from the response.
type FileBlock struct {
	Path    string
	Content string
}

func parseTaskResponse(response string, task *coding.Task) (*coding.TaskResult, error) {
	result := &coding.TaskResult{
		Success: true,
		Files:   make(map[string]string),
		Patches: make(map[string]string),
	}

	blocks := extractFileBlocks(response)
	if len(blocks) == 0 {
		blocks = fallbackSingleFileBlock(response, task)
	}
	if len(blocks) == 0 {
		return nil, fmt.Errorf("no file outputs found in response")
	}

	for _, block := range blocks {
		if block.Path == "" {
			continue
		}

		content := normalizeFileContent(block.Content)
		if task.PreferPatch && looksLikePatch(content) {
			result.Patches[block.Path] = content
		} else {
			if !looksLikeFileContent(content, block.Path) {
				return nil, fmt.Errorf("invalid file content for %s", block.Path)
			}
			result.Files[block.Path] = content
		}
	}

	for _, expectedFile := range task.OutputFiles {
		found := false
		if _, ok := result.Files[expectedFile]; ok {
			found = true
		}
		if _, ok := result.Patches[expectedFile]; ok {
			found = true
		}
		if !found {
			result.Success = false
			result.ValidationErrors = append(result.ValidationErrors,
				fmt.Sprintf("missing expected output file: %s", expectedFile))
		}
	}

	return result, nil
}

func extractFileBlocks(response string) []FileBlock {
	blocks := make([]FileBlock, 0)
	lines := strings.Split(response, "\n")

	var currentBlock *FileBlock
	inContent := false

	for _, line := range lines {
		if strings.HasPrefix(line, "FILE:") {
			if currentBlock != nil && currentBlock.Path != "" {
				blocks = append(blocks, *currentBlock)
			}
			path := strings.TrimSpace(strings.TrimPrefix(line, "FILE:"))
			currentBlock = &FileBlock{Path: path}
			inContent = false
			continue
		}

		if strings.TrimSpace(line) == "---" {
			if currentBlock != nil {
				if !inContent {
					inContent = true
				} else {
					if currentBlock.Path != "" {
						blocks = append(blocks, *currentBlock)
					}
					currentBlock = nil
					inContent = false
				}
			}
			continue
		}

		if inContent && currentBlock != nil {
			if currentBlock.Content != "" {
				currentBlock.Content += "\n"
			}
			currentBlock.Content += line
		}
	}

	if currentBlock != nil && currentBlock.Path != "" {
		blocks = append(blocks, *currentBlock)
	}

	return blocks
}

func fallbackSingleFileBlock(response string, task *coding.Task) []FileBlock {
	if len(task.OutputFiles) != 1 || task.PreferPatch {
		return nil
	}
	content := normalizeFileContent(strings.TrimSpace(response))
	if content == "" || !looksLikeFileContent(content, task.OutputFiles[0]) {
		return nil
	}
	return []FileBlock{{Path: task.OutputFiles[0], Content: content}}
}

func normalizeFileContent(content string) string {
	return stripCodeFence(strings.TrimSpace(content))
}

func stripCodeFence(content string) string {
	if !strings.HasPrefix(content, "```") {
		return content
	}
	lines := strings.Split(content, "\n")
	if len(lines) < 3 {
		return content
	}
	if strings.TrimSpace(lines[len(lines)-1]) == "```" {
		return strings.TrimSpace(strings.Join(lines[1:len(lines)-1], "\n"))
	}
	return content
}

func looksLikeFileContent(content, path string) bool {
	first := firstNonEmptyLine(content)
	if first == "" {
		return false
	}
	if strings.HasSuffix(strings.ToLower(path), ".py") {
		if strings.Contains(content, "```") || hasRepairNarration(content) {
			return false
		}
		return strings.HasPrefix(first, "import ") ||
			strings.HasPrefix(first, "from ") ||
			strings.HasPrefix(first, "#!") ||
			strings.HasPrefix(first, `"""`) ||
			strings.HasPrefix(first, "'''")
	}
	return !strings.Contains(strings.ToLower(first), "here is") &&
		!strings.HasPrefix(strings.ToLower(first), "i ")
}

func hasRepairNarration(content string) bool {
	for _, line := range strings.Split(content, "\n") {
		text := strings.ToLower(strings.TrimSpace(strings.TrimPrefix(line, "#")))
		if text == "" {
			continue
		}
		return strings.HasPrefix(text, "assuming ") ||
			strings.HasPrefix(text, "original ") ||
			strings.HasPrefix(text, "corrected ") ||
			strings.HasPrefix(text, "note:") ||
			strings.HasPrefix(text, "here is ")
	}
	return false
}

func firstNonEmptyLine(content string) string {
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			return line
		}
	}
	return ""
}

func looksLikePatch(content string) bool {
	lines := strings.Split(content, "\n")
	if len(lines) < 3 {
		return false
	}

	hasDiffMarker := false
	hasHunkMarker := false
	limit := min(10, len(lines))
	for _, line := range lines[:limit] {
		if strings.HasPrefix(line, "---") || strings.HasPrefix(line, "+++") {
			hasDiffMarker = true
		}
		if strings.HasPrefix(line, "@@") {
			hasHunkMarker = true
		}
	}

	return hasDiffMarker && hasHunkMarker
}
