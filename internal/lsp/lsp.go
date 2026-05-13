package lsp

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"go/ast"
	"go/parser"
	"go/scanner"
	"go/token"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"unicode"
)

type Manager struct {
	ProjectRoot string
	Languages   []string
}

type Server struct {
	Language  string `json:"language"`
	Command   string `json:"command"`
	Available bool   `json:"available"`
}

type Process struct {
	Server Server
	cmd    *exec.Cmd
}

type Diagnostic struct {
	File     string `json:"file"`
	Line     int    `json:"line"`
	Column   int    `json:"column"`
	Severity string `json:"severity"`
	Message  string `json:"message"`
	Source   string `json:"source"`
}

type Symbol struct {
	Name string `json:"name"`
	Kind string `json:"kind"`
	File string `json:"file"`
	Line int    `json:"line"`
}

type Reference struct {
	File string `json:"file"`
	Line int    `json:"line"`
	Text string `json:"text"`
}

func NewManager(projectRoot string, languages []string) Manager {
	return Manager{ProjectRoot: projectRoot, Languages: languages}
}

func (m Manager) DetectServers() []Server {
	var servers []Server
	if m.hasGo() {
		_, err := exec.LookPath("gopls")
		servers = append(servers, Server{Language: "go", Command: "gopls", Available: err == nil})
	}
	return servers
}

func (m Manager) Start(ctx context.Context, server Server) (*Process, error) {
	if strings.TrimSpace(server.Command) == "" {
		return nil, fmt.Errorf("language server command is required")
	}
	if _, err := exec.LookPath(server.Command); err != nil {
		return nil, err
	}
	args := []string{"serve"}
	cmd := exec.CommandContext(ctx, server.Command, args...)
	cmd.Dir = m.ProjectRoot
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	server.Available = true
	return &Process{Server: server, cmd: cmd}, nil
}

func (p *Process) Stop() error {
	if p == nil || p.cmd == nil || p.cmd.Process == nil {
		return nil
	}
	_ = p.cmd.Process.Kill()
	_, err := p.cmd.Process.Wait()
	return err
}

func (m Manager) Diagnostics(ctx context.Context) ([]Diagnostic, error) {
	files, err := goFiles(m.ProjectRoot)
	if err != nil {
		return nil, err
	}
	var diagnostics []Diagnostic
	for _, rel := range files {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		full := filepath.Join(m.ProjectRoot, rel)
		src, err := os.ReadFile(full)
		if err != nil {
			continue
		}
		fset := token.NewFileSet()
		if _, err := parser.ParseFile(fset, full, src, parser.AllErrors); err != nil {
			if list, ok := err.(scanner.ErrorList); ok {
				for _, item := range list {
					diagnostics = append(diagnostics, parseDiagnostic(m.ProjectRoot, item))
				}
				continue
			}
			diagnostics = append(diagnostics, Diagnostic{File: rel, Severity: "error", Message: err.Error(), Source: "go/parser"})
		}
	}
	return diagnostics, nil
}

func (m Manager) Symbols(ctx context.Context, query string) ([]Symbol, error) {
	files, err := goFiles(m.ProjectRoot)
	if err != nil {
		return nil, err
	}
	query = strings.ToLower(strings.TrimSpace(query))
	var symbols []Symbol
	for _, rel := range files {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		full := filepath.Join(m.ProjectRoot, rel)
		fset := token.NewFileSet()
		file, err := parser.ParseFile(fset, full, nil, 0)
		if err != nil {
			continue
		}
		for _, decl := range file.Decls {
			switch d := decl.(type) {
			case *ast.FuncDecl:
				addSymbol(&symbols, fset, rel, d.Name.Name, "func", d.Pos(), query)
			case *ast.GenDecl:
				for _, spec := range d.Specs {
					switch s := spec.(type) {
					case *ast.TypeSpec:
						addSymbol(&symbols, fset, rel, s.Name.Name, "type", s.Pos(), query)
					case *ast.ValueSpec:
						kind := strings.ToLower(d.Tok.String())
						for _, name := range s.Names {
							addSymbol(&symbols, fset, rel, name.Name, kind, name.Pos(), query)
						}
					}
				}
			}
		}
	}
	sort.Slice(symbols, func(i, j int) bool {
		if symbols[i].Name != symbols[j].Name {
			return symbols[i].Name < symbols[j].Name
		}
		return symbols[i].File < symbols[j].File
	})
	return symbols, nil
}

func (m Manager) Definition(ctx context.Context, name string) (Symbol, bool, error) {
	symbols, err := m.Symbols(ctx, name)
	if err != nil {
		return Symbol{}, false, err
	}
	for _, symbol := range symbols {
		if symbol.Name == name {
			return symbol, true, nil
		}
	}
	return Symbol{}, false, nil
}

func (m Manager) References(ctx context.Context, name string) ([]Reference, error) {
	files, err := goFiles(m.ProjectRoot)
	if err != nil {
		return nil, err
	}
	var refs []Reference
	for _, rel := range files {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		data, err := os.ReadFile(filepath.Join(m.ProjectRoot, rel))
		if err != nil {
			continue
		}
		scanner := bufio.NewScanner(bytes.NewReader(data))
		lineNo := 0
		for scanner.Scan() {
			lineNo++
			text := scanner.Text()
			if containsIdent(text, name) {
				refs = append(refs, Reference{File: rel, Line: lineNo, Text: strings.TrimSpace(text)})
			}
		}
	}
	return refs, nil
}

func (m Manager) hasGo() bool {
	for _, lang := range m.Languages {
		if lang == "go" {
			return true
		}
	}
	if _, err := os.Stat(filepath.Join(m.ProjectRoot, "go.mod")); err == nil {
		return true
	}
	files, _ := goFiles(m.ProjectRoot)
	return len(files) > 0
}

func addSymbol(symbols *[]Symbol, fset *token.FileSet, rel, name, kind string, pos token.Pos, query string) {
	if query != "" && !strings.Contains(strings.ToLower(name), query) {
		return
	}
	position := fset.Position(pos)
	*symbols = append(*symbols, Symbol{Name: name, Kind: kind, File: rel, Line: position.Line})
}

func goFiles(root string) ([]string, error) {
	var files []string
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
			switch filepath.Base(rel) {
			case ".git", ".weazlcode", ".gocache", ".gomodcache", "vendor", "node_modules":
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasSuffix(rel, ".go") {
			files = append(files, rel)
		}
		return nil
	})
	sort.Strings(files)
	return files, err
}

func containsIdent(line, name string) bool {
	if name == "" {
		return false
	}
	start := 0
	for {
		idx := strings.Index(line[start:], name)
		if idx < 0 {
			return false
		}
		idx += start
		beforeOK := idx == 0 || !isIdentRune(rune(line[idx-1]))
		after := idx + len(name)
		afterOK := after >= len(line) || !isIdentRune(rune(line[after]))
		if beforeOK && afterOK {
			return true
		}
		start = after
	}
}

func isIdentRune(r rune) bool {
	return r == '_' || unicode.IsLetter(r) || unicode.IsDigit(r)
}

func parseDiagnostic(root string, err error) Diagnostic {
	msg := err.Error()
	parts := strings.SplitN(msg, ":", 4)
	if len(parts) < 4 {
		return Diagnostic{Severity: "error", Message: msg, Source: "go/parser"}
	}
	file := strings.TrimSpace(parts[0])
	if rel, relErr := filepath.Rel(root, file); relErr == nil {
		file = filepath.ToSlash(rel)
	}
	line := parseSmallInt(parts[1])
	column := parseSmallInt(parts[2])
	return Diagnostic{File: file, Line: line, Column: column, Severity: "error", Message: strings.TrimSpace(parts[3]), Source: "go/parser"}
}

func parseSmallInt(s string) int {
	n := 0
	for _, r := range strings.TrimSpace(s) {
		if r < '0' || r > '9' {
			break
		}
		n = n*10 + int(r-'0')
	}
	return n
}
