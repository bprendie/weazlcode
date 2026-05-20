package project

import (
	"crypto/sha1"
	"encoding/hex"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

type Summary struct {
	Root       string
	GitRoot    bool
	Branch     string
	Dirty      bool
	StateDir   string
	LogDir     string
	Languages  []string
	FileCount  int
	IgnoreFile string
}

func Detect(cwd string) (Summary, error) {
	if strings.TrimSpace(cwd) == "" {
		var err error
		cwd, err = os.Getwd()
		if err != nil {
			return Summary{}, err
		}
	}
	abs, err := filepath.Abs(cwd)
	if err != nil {
		return Summary{}, err
	}
	root, gitRoot := gitRoot(abs)
	if root == "" {
		root = abs
	}
	stateDir := stateDirForRoot(root)
	logDir := filepath.Join(stateDir, "logs")
	if err := os.MkdirAll(logDir, 0o700); err != nil {
		return Summary{}, err
	}
	s := Summary{
		Root:     root,
		GitRoot:  gitRoot,
		StateDir: stateDir,
		LogDir:   logDir,
	}
	if gitRoot {
		s.Branch = gitBranch(root)
		s.Dirty = gitDirty(root)
	}
	s.IgnoreFile = ignoreFile(root)
	s.Languages, s.FileCount = scanProject(root, loadIgnoreRules(s.IgnoreFile))
	return s, nil
}

func stateDirForRoot(root string) string {
	if runtime.GOOS != "windows" {
		return filepath.Join(root, ".weazlcode")
	}
	base := strings.TrimSpace(filepath.Base(root))
	if base == "" || base == "." || base == string(filepath.Separator) {
		base = "project"
	}
	base = safeStateDirName(base)
	sum := sha1.Sum([]byte(root))
	return filepath.Join(windowsAppDataRoot(), "projects", base+"-"+hex.EncodeToString(sum[:])[:10])
}

func safeStateDirName(name string) string {
	var b strings.Builder
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
			b.WriteRune(r)
		default:
			b.WriteByte('-')
		}
	}
	out := strings.Trim(b.String(), "-_")
	if out == "" {
		return "project"
	}
	return out
}

func windowsAppDataRoot() string {
	if p := os.Getenv("WEAZLCODE_HOME"); p != "" {
		return p
	}
	if p := os.Getenv("APPDATA"); p != "" {
		return filepath.Join(p, "weazlcode")
	}
	if dir, err := os.UserConfigDir(); err == nil && dir != "" {
		return filepath.Join(dir, "weazlcode")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, "AppData", "Roaming", "weazlcode")
}

func gitRoot(cwd string) (string, bool) {
	cmd := exec.Command("git", "rev-parse", "--show-toplevel")
	cmd.Dir = cwd
	out, err := cmd.Output()
	if err != nil {
		return "", false
	}
	root := strings.TrimSpace(string(out))
	if root == "" {
		return "", false
	}
	return root, true
}

func gitBranch(root string) string {
	cmd := exec.Command("git", "branch", "--show-current")
	cmd.Dir = root
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	branch := strings.TrimSpace(string(out))
	if branch != "" {
		return branch
	}
	cmd = exec.Command("git", "rev-parse", "--short", "HEAD")
	cmd.Dir = root
	out, err = cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func gitDirty(root string) bool {
	cmd := exec.Command("git", "status", "--porcelain")
	cmd.Dir = root
	out, err := cmd.Output()
	return err == nil && strings.TrimSpace(string(out)) != ""
}

func ignoreFile(root string) string {
	path := filepath.Join(root, ".weazlcodeignore")
	if _, err := os.Stat(path); err == nil {
		return path
	}
	return ""
}

type ignoreRule struct {
	pattern string
	dirOnly bool
	anchor  bool
}

func loadIgnoreRules(path string) []ignoreRule {
	if path == "" {
		return nil
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	lines := strings.Split(string(raw), "\n")
	rules := make([]ignoreRule, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "!") {
			continue
		}
		rule := ignoreRule{pattern: line}
		if strings.HasPrefix(rule.pattern, "/") {
			rule.anchor = true
			rule.pattern = strings.TrimPrefix(rule.pattern, "/")
		}
		if strings.HasSuffix(rule.pattern, "/") {
			rule.dirOnly = true
			rule.pattern = strings.TrimSuffix(rule.pattern, "/")
		}
		if rule.pattern != "" {
			rules = append(rules, rule)
		}
	}
	return rules
}

func scanProject(root string, ignores []ignoreRule) ([]string, int) {
	seen := map[string]bool{}
	count := 0
	_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		name := d.Name()
		if ignored(root, path, d.IsDir(), ignores) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			switch name {
			case ".git", ".weazlcode", "node_modules", "vendor", ".gocache", ".gomodcache":
				if path != root {
					return filepath.SkipDir
				}
			}
			return nil
		}
		count++
		switch strings.ToLower(filepath.Ext(name)) {
		case ".go":
			seen["go"] = true
		case ".js", ".jsx", ".ts", ".tsx":
			seen["javascript/typescript"] = true
		case ".py":
			seen["python"] = true
		case ".rs":
			seen["rust"] = true
		case ".sh", ".bash", ".zsh":
			seen["shell"] = true
		case ".md":
			seen["markdown"] = true
		}
		return nil
	})
	ordered := []string{"go", "javascript/typescript", "python", "rust", "shell", "markdown"}
	langs := make([]string, 0, len(seen))
	for _, lang := range ordered {
		if seen[lang] {
			langs = append(langs, lang)
		}
	}
	return langs, count
}

func ignored(root, path string, isDir bool, rules []ignoreRule) bool {
	if len(rules) == 0 {
		return false
	}
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return false
	}
	rel = filepath.ToSlash(rel)
	base := filepath.Base(rel)
	for _, rule := range rules {
		if rule.dirOnly && !isDir {
			continue
		}
		if rule.anchor {
			if matchPattern(rule.pattern, rel) {
				return true
			}
			continue
		}
		if strings.Contains(rule.pattern, "/") {
			if matchPattern(rule.pattern, rel) {
				return true
			}
			continue
		}
		if matchPattern(rule.pattern, base) {
			return true
		}
	}
	return false
}

func matchPattern(pattern, value string) bool {
	ok, err := filepath.Match(pattern, value)
	if err == nil && ok {
		return true
	}
	return pattern == value || strings.HasPrefix(value, strings.TrimSuffix(pattern, "/")+"/")
}

func (s Summary) StatusLabel() string {
	root := filepath.Base(s.Root)
	if root == "." || root == string(filepath.Separator) || root == "" {
		root = s.Root
	}
	if s.GitRoot {
		branch := s.Branch
		if branch == "" {
			branch = "detached"
		}
		dirty := "clean"
		if s.Dirty {
			dirty = "dirty"
		}
		return root + " [" + branch + " " + dirty + "]"
	}
	return root + " [no git]"
}
