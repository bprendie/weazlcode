package tools

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

type ListFilesTool struct{ limits Limits }
type ReadFileTool struct{ limits Limits }
type SearchFilesTool struct{ limits Limits }
type CreateFileTool struct{ limits Limits }

func NewListFilesTool(limits Limits) *ListFilesTool     { return &ListFilesTool{limits: limits} }
func NewReadFileTool(limits Limits) *ReadFileTool       { return &ReadFileTool{limits: limits} }
func NewSearchFilesTool(limits Limits) *SearchFilesTool { return &SearchFilesTool{limits: limits} }
func NewCreateFileTool(limits Limits) *CreateFileTool   { return &CreateFileTool{limits: limits} }

func (t *ListFilesTool) Name() string               { return "list_files" }
func (t *ReadFileTool) Name() string                { return "read_file" }
func (t *SearchFilesTool) Name() string             { return "search_files" }
func (t *CreateFileTool) Name() string              { return "create_file" }
func (t *ListFilesTool) SafetyLevel() SafetyLevel   { return SafetyLevelSafe }
func (t *ReadFileTool) SafetyLevel() SafetyLevel    { return SafetyLevelSafe }
func (t *SearchFilesTool) SafetyLevel() SafetyLevel { return SafetyLevelSafe }
func (t *CreateFileTool) SafetyLevel() SafetyLevel  { return SafetyLevelSafe }

func (t *ListFilesTool) Description() string {
	return "List files under a configured workspace root"
}

func (t *ReadFileTool) Description() string {
	return "Read a text file under a configured workspace root"
}

func (t *SearchFilesTool) Description() string {
	return "Search text files under a configured workspace root for a literal query"
}

func (t *CreateFileTool) Description() string {
	return "Create a new text file under a configured workspace root. Refuses to overwrite existing files"
}

func (t *ListFilesTool) Parameters() []Parameter {
	return []Parameter{
		{Name: "root", Type: "string", Description: "Directory under a configured workspace root", Required: true},
		{Name: "glob", Type: "string", Description: "Optional filepath glob matched against relative paths, such as **/*.go or *.md", Required: false},
		{Name: "max_files", Type: "number", Description: "Maximum files to return, defaults to 200", Required: false},
	}
}

func (t *ReadFileTool) Parameters() []Parameter {
	return []Parameter{
		{Name: "path", Type: "string", Description: "File path under a configured workspace root", Required: true},
		{Name: "max_chars", Type: "number", Description: "Maximum characters to return", Required: false},
	}
}

func (t *SearchFilesTool) Parameters() []Parameter {
	return []Parameter{
		{Name: "root", Type: "string", Description: "Directory under a configured workspace root", Required: true},
		{Name: "query", Type: "string", Description: "Literal text to search for", Required: true},
		{Name: "glob", Type: "string", Description: "Optional filepath glob matched against relative paths", Required: false},
		{Name: "max_matches", Type: "number", Description: "Maximum matching lines to return, defaults to 100", Required: false},
	}
}

func (t *CreateFileTool) Parameters() []Parameter {
	return []Parameter{
		{Name: "path", Type: "string", Description: "New file path under a configured workspace root", Required: true},
		{Name: "content", Type: "string", Description: "Text content to write", Required: true},
		{Name: "create_parent_dirs", Type: "boolean", Description: "Create missing parent directories. Defaults to false", Required: false},
	}
}

func (t *ListFilesTool) Execute(ctx context.Context, params map[string]any) (string, error) {
	if err := t.limits.RequireRoots(); err != nil {
		return "", err
	}
	rootParam, _ := params["root"].(string)
	root, err := t.limits.ResolveAllowed(rootParam)
	if err != nil {
		return "", err
	}
	maxFiles := intParam(params, "max_files", 200, 1, 1000)
	glob, _ := params["glob"].(string)

	var out strings.Builder
	count := 0
	err = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || count >= maxFiles {
			return err
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if path == root || skipHidden(root, path) {
			if d != nil && d.IsDir() && skipHidden(root, path) {
				return filepath.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			return nil
		}
		rel, _ := filepath.Rel(root, path)
		if glob != "" && !matchPathGlob(glob, rel) {
			return nil
		}
		info, _ := d.Info()
		fmt.Fprintf(&out, "%s\t%d bytes\n", rel, info.Size())
		count++
		return nil
	})
	if err != nil {
		return "", err
	}
	if count == 0 {
		return "No files found.", nil
	}
	return t.limits.Truncate(out.String()), nil
}

func (t *ReadFileTool) Execute(ctx context.Context, params map[string]any) (string, error) {
	if err := t.limits.RequireRoots(); err != nil {
		return "", err
	}
	pathParam, _ := params["path"].(string)
	path, err := t.limits.ResolveAllowed(pathParam)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(path)
	if err != nil {
		return "", err
	}
	if info.IsDir() {
		return "", fmt.Errorf("%s is a directory", pathParam)
	}
	if info.Size() > t.limits.fileLimit() {
		return "", fmt.Errorf("%s is too large: %d bytes", pathParam, info.Size())
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	if !looksText(data) {
		return "", fmt.Errorf("%s does not look like a text file", pathParam)
	}
	maxChars := intParam(params, "max_chars", t.limits.outputLimit(), 1, t.limits.outputLimit())
	text := string(data)
	if len(text) > maxChars {
		text = text[:maxChars] + fmt.Sprintf("\n\n[truncated: %d chars omitted]", len(string(data))-maxChars)
	}
	return text, nil
}

func (t *SearchFilesTool) Execute(ctx context.Context, params map[string]any) (string, error) {
	if err := t.limits.RequireRoots(); err != nil {
		return "", err
	}
	rootParam, _ := params["root"].(string)
	root, err := t.limits.ResolveAllowed(rootParam)
	if err != nil {
		return "", err
	}
	query, _ := params["query"].(string)
	if query == "" {
		return "", fmt.Errorf("query parameter is required")
	}
	glob, _ := params["glob"].(string)
	maxMatches := intParam(params, "max_matches", 100, 1, 500)

	var out strings.Builder
	matches := 0
	err = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || matches >= maxMatches {
			return err
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if path == root || skipHidden(root, path) {
			if d != nil && d.IsDir() && skipHidden(root, path) {
				return filepath.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			return nil
		}
		rel, _ := filepath.Rel(root, path)
		if glob != "" && !matchPathGlob(glob, rel) {
			return nil
		}
		info, err := d.Info()
		if err != nil || info.Size() > t.limits.fileLimit() {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil || !looksText(data) {
			return nil
		}
		for i, line := range strings.Split(string(data), "\n") {
			if matches >= maxMatches {
				break
			}
			if strings.Contains(line, query) {
				fmt.Fprintf(&out, "%s:%d: %s\n", rel, i+1, strings.TrimSpace(line))
				matches++
			}
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	if matches == 0 {
		return "No matches found.", nil
	}
	return t.limits.Truncate(out.String()), nil
}

func (t *CreateFileTool) Execute(ctx context.Context, params map[string]any) (string, error) {
	if err := t.limits.RequireRoots(); err != nil {
		return "", err
	}
	pathParam, _ := params["path"].(string)
	path, err := t.limits.ResolveCreateAllowed(pathParam)
	if err != nil {
		return "", err
	}
	content, ok := params["content"].(string)
	if !ok {
		return "", fmt.Errorf("content parameter is required and must be a string")
	}
	if int64(len(content)) > t.limits.fileLimit() {
		return "", fmt.Errorf("content is too large: %d bytes", len(content))
	}
	if !looksText([]byte(content)) {
		return "", fmt.Errorf("content does not look like text")
	}
	createParentDirs, _ := params["create_parent_dirs"].(bool)
	parent := filepath.Dir(path)
	if createParentDirs {
		if err := os.MkdirAll(parent, 0o755); err != nil {
			return "", err
		}
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		if os.IsExist(err) {
			return "", fmt.Errorf("%s already exists; create_file refuses to overwrite", pathParam)
		}
		return "", err
	}
	defer f.Close()
	if _, err := f.WriteString(content); err != nil {
		return "", err
	}
	return fmt.Sprintf("Created %s (%d bytes)", path, len(content)), nil
}

func intParam(params map[string]any, key string, def, min, max int) int {
	if _, ok := params[key]; !ok {
		return def
	}
	n, err := getNumber(params, key)
	if err != nil {
		return def
	}
	v := int(n)
	if v < min {
		return min
	}
	if v > max {
		return max
	}
	return v
}

func skipHidden(root, path string) bool {
	rel, err := filepath.Rel(root, path)
	if err != nil || rel == "." {
		return false
	}
	for _, part := range strings.Split(rel, string(os.PathSeparator)) {
		if strings.HasPrefix(part, ".") {
			return true
		}
	}
	return false
}

func matchPathGlob(pattern, rel string) bool {
	pattern = filepath.ToSlash(pattern)
	rel = filepath.ToSlash(rel)
	if strings.HasPrefix(pattern, "**/") {
		ok, _ := filepath.Match(strings.TrimPrefix(pattern, "**/"), filepath.Base(rel))
		return ok
	}
	ok, _ := filepath.Match(pattern, rel)
	return ok
}

func looksText(data []byte) bool {
	for i, b := range data {
		if i > 4096 {
			break
		}
		if b == 0 {
			return false
		}
	}
	return true
}
