package tui

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/bprendie/weazlcode/internal/coding"
	"github.com/bprendie/weazlcode/internal/lsp"
	"github.com/bprendie/weazlcode/internal/project"
)

func (m model) instructionsCommandText() string {
	instructions, ok, err := project.LoadInstructions(m.project.Root)
	if err != nil {
		return "Instructions error: " + err.Error()
	}
	commands := project.DiscoverCommands(m.project.Root)
	var b strings.Builder
	b.WriteString("Project instructions:\n")
	if ok {
		fmt.Fprintf(&b, "file: %s\n\n%s", filepath.Base(instructions.Path), strings.TrimSpace(instructions.Content))
	} else {
		b.WriteString("No WEAZLCODE.md or AGENTS.md found. Run `weazlcode init`.\n")
	}
	if len(commands) > 0 {
		b.WriteString("\n\nDiscovered commands:")
		for _, command := range commands {
			fmt.Fprintf(&b, "\n- %s", command)
		}
	}
	memories, err := m.store.ProjectMemories(m.project.Root, 10)
	if err == nil && len(memories) > 0 {
		b.WriteString("\n\nProject memory:")
		for _, memory := range memories {
			fmt.Fprintf(&b, "\n- %s: %s", memory.Key, memory.Value)
		}
	}
	return b.String()
}

func (m model) handleProjectMemoryCommand(args []string, rawArgs string) (tea.Model, tea.Cmd, bool) {
	_ = args
	rawArgs = strings.TrimSpace(rawArgs)
	if rawArgs == "" {
		m.setIDEView("memory", m.projectMemoryText())
		return m, nil, true
	}
	key, value, ok := strings.Cut(rawArgs, "=")
	if !ok || strings.TrimSpace(key) == "" || strings.TrimSpace(value) == "" {
		m.addSystemNote("Usage: /memory key=value")
		m.status = "memory usage"
		return m, nil, true
	}
	if err := m.store.RememberProject(m.project.Root, strings.TrimSpace(key), strings.TrimSpace(value), "project"); err != nil {
		m.addSystemNote("Project memory error: " + err.Error())
		m.status = "memory failed"
		return m, nil, true
	}
	m.addSystemNote("Saved project memory: " + strings.TrimSpace(key))
	m.status = "memory saved"
	return m, nil, true
}

func (m model) projectMemoryText() string {
	memories, err := m.store.ProjectMemories(m.project.Root, 50)
	if err != nil {
		return "Project memory error: " + err.Error()
	}
	if len(memories) == 0 {
		return "Project memory:\nNo project memories yet. Use `/memory key=value`."
	}
	var b strings.Builder
	b.WriteString("Project memory:\n")
	for _, memory := range memories {
		fmt.Fprintf(&b, "- %s: %s", memory.Key, memory.Value)
		if memory.Tags != "" {
			fmt.Fprintf(&b, " [%s]", memory.Tags)
		}
		b.WriteString("\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

func (m model) finalReviewCommandText() string {
	plan, ok, err := m.store.LatestPlan(m.session.ID)
	if err != nil {
		return "Final review error: " + err.Error()
	}
	diff, diffErr := m.gitDiff()
	if !ok {
		if diffErr != nil {
			return "Final review error: " + diffErr.Error()
		}
		return "Final review:\nNo plan found.\n\nDiff:\n" + emptyFallback(diff, "No changes.")
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Final review:\nplan: %s\nstatus: %s\n", plan.Title, plan.Status)
	for _, task := range plan.Tasks {
		fmt.Fprintf(&b, "- [%s] %s\n", task.Status, task.Title)
		events, err := m.store.TaskEvents(task.ID)
		if err == nil {
			if verdict, ok := latestReviewVerdict(events); ok {
				fmt.Fprintf(&b, "  reviewer: %s - %s\n", verdict.Verdict, verdict.Summary)
			}
		}
	}
	if diffErr != nil {
		fmt.Fprintf(&b, "\nDiff error: %s", diffErr)
	} else {
		fmt.Fprintf(&b, "\nDiff:\n%s", emptyFallback(diff, "No changes."))
	}
	fmt.Fprintf(&b, "\n\nRollback guidance:\n- Review the diff before committing.\n- To discard uncommitted changes manually, use git restore on specific files.")
	return b.String()
}

func (m model) commitMessageCommandText() string {
	return m.generatedCommitMessage()
}

func (m model) generatedCommitMessage() string {
	plan, ok, _ := m.store.LatestPlan(m.session.ID)
	if ok && strings.TrimSpace(plan.Title) != "" {
		return "Complete " + strings.TrimSpace(plan.Title)
	}
	diff, err := m.gitDiff()
	if err != nil || strings.TrimSpace(diff) == "" {
		return "Update WeazlCode project"
	}
	return "Update project files"
}

func (m model) commitCommand(raw string) (tea.Model, tea.Cmd, bool) {
	if strings.ToLower(strings.TrimSpace(raw)) != "yes" {
		m.addSystemNote("Commit is confirmation-gated. Use `/commit yes` to run `git add .` and `git commit` with the generated message.")
		m.status = "commit needs confirmation"
		return m, nil, true
	}
	message := m.generatedCommitMessage()
	if out, err := runGitCommit(m.project.Root, message); err != nil {
		m.addSystemNote("Commit error:\n" + out + "\n" + err.Error())
		m.status = "commit failed"
		return m, nil, true
	}
	m.addSystemNote("Committed changes:\n" + message)
	m.status = "committed"
	return m, nil, true
}

func (m model) exportRunCommandText() string {
	text := m.finalReviewCommandText()
	dir := filepath.Join(m.project.StateDir, "runs")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "Export error: " + err.Error()
	}
	path := filepath.Join(dir, time.Now().Format("20060102-150405")+".md")
	body := "# WeazlCode Run Artifact\n\n" + text + "\n"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		return "Export error: " + err.Error()
	}
	return "Exported run artifact:\n" + path
}

func (m model) writeRunArtifact(kind string, payload any) {
	if strings.TrimSpace(m.project.StateDir) == "" || strings.TrimSpace(m.session.ID) == "" {
		return
	}
	name := time.Now().Format("20060102-150405.000000000") + "-" + safeArtifactName(kind) + ".json"
	dir := filepath.Join(m.project.StateDir, "runs", safeArtifactName(m.session.ID))
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return
	}
	body, err := json.MarshalIndent(struct {
		Kind      string `json:"kind"`
		SessionID string `json:"session_id"`
		CreatedAt string `json:"created_at"`
		Payload   any    `json:"payload"`
	}{
		Kind:      kind,
		SessionID: m.session.ID,
		CreatedAt: time.Now().Format(time.RFC3339Nano),
		Payload:   payload,
	}, "", "  ")
	if err != nil {
		return
	}
	_ = os.WriteFile(filepath.Join(dir, name), append(body, '\n'), 0o600)
}

func safeArtifactName(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "artifact"
	}
	var b strings.Builder
	for _, r := range raw {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' || r == '.' {
			b.WriteRune(r)
			continue
		}
		b.WriteByte('-')
	}
	out := strings.Trim(b.String(), "-")
	if out == "" {
		return "artifact"
	}
	return out
}

func latestReviewVerdict(events []coding.TaskEvent) (coding.ReviewVerdict, bool) {
	for i := len(events) - 1; i >= 0; i-- {
		if events[i].Type == "reviewer_verdict" {
			return reviewVerdictFromEvent(events[i])
		}
	}
	return coding.ReviewVerdict{}, false
}

func runGitCommit(root, message string) (string, error) {
	add := exec.Command("git", "add", ".")
	add.Dir = root
	out, err := add.CombinedOutput()
	if err != nil {
		return string(out), err
	}
	commit := exec.Command("git", "commit", "-m", message)
	commit.Dir = root
	commitOut, err := commit.CombinedOutput()
	return string(out) + string(commitOut), err
}

func (m model) projectCommandText() string {
	langs := "none detected"
	if len(m.project.Languages) > 0 {
		langs = strings.Join(m.project.Languages, ", ")
	}
	ignore := "none"
	if m.project.IgnoreFile != "" {
		ignore = filepath.Base(m.project.IgnoreFile)
	}
	return fmt.Sprintf("Project:\nroot: %s\ngit: %t\nbranch: %s\ndirty: %t\nfiles: %d\nlanguages: %s\nstate: %s\nlogs: %s\nignore: %s",
		m.project.Root,
		m.project.GitRoot,
		emptyFallback(m.project.Branch, "none"),
		m.project.Dirty,
		m.project.FileCount,
		langs,
		m.project.StateDir,
		m.project.LogDir,
		ignore,
	)
}

func (m model) modelRolesText() string {
	roleProvider := func(role, providerName string) string {
		p, ok := m.cfg.Providers[providerName]
		if !ok {
			return fmt.Sprintf("%s: %s (missing provider)", role, providerName)
		}
		return fmt.Sprintf("%s: %s -> %s/%s", role, providerName, p.Type, p.Model)
	}
	return strings.Join([]string{
		"Model roles:",
		roleProvider("orchestrator", m.cfg.ModelRoles.Orchestrator),
		roleProvider("worker", m.cfg.ModelRoles.Worker),
		roleProvider("reviewer", m.cfg.ModelRoles.Reviewer),
		roleProvider("summarizer", m.cfg.ModelRoles.Summarizer),
	}, "\n")
}

func (m model) toolsViewText() string {
	registered := m.toolRegistry.List()
	if len(registered) == 0 {
		return "Tools:\nnone registered"
	}
	var b strings.Builder
	b.WriteString("Tools:\n")
	for _, tool := range registered {
		fmt.Fprintf(&b, "- %s [%s]\n  %s\n", tool.Name(), safetyLabel(tool.SafetyLevel()), tool.Description())
	}
	return strings.TrimRight(b.String(), "\n")
}

func (m model) skillsViewText() string {
	var b strings.Builder
	b.WriteString("Skills:\n")
	if !m.cfg.Skills.SkillsEnabled() {
		b.WriteString("disabled in config")
		return b.String()
	}
	if len(m.cfg.Skills.Paths) > 0 {
		fmt.Fprintf(&b, "precedence: %s\n\n", strings.Join(m.cfg.Skills.Paths, " > "))
	}
	skills, err := project.DiscoverSkills(m.project.Root, m.cfg.Skills.Paths)
	if err != nil {
		return "Skills error: " + err.Error()
	}
	if len(skills) == 0 {
		b.WriteString("none discovered")
		return b.String()
	}
	for _, skill := range skills {
		fmt.Fprintf(&b, "- %s [%s]\n", skill.Name, skill.Source)
		if strings.TrimSpace(skill.Description) != "" {
			fmt.Fprintf(&b, "  %s\n", skill.Description)
		}
		fmt.Fprintf(&b, "  path: %s\n", skill.Path)
	}
	return strings.TrimRight(b.String(), "\n")
}

func (m model) configViewText() string {
	active := m.cfg.Active()
	var b strings.Builder
	fmt.Fprintf(&b, "Config:\npath: %s\nactive_provider: %s\nactive_model: %s/%s\nserver_url: %s\ncontext_window: %d\nmarkdown: %t (%s)\nresume_last_session: %t\n\nModel roles:\n",
		emptyFallback(m.cfgPath, "default"),
		m.cfg.ActiveProvider,
		active.Type,
		active.Model,
		emptyFallback(active.ServerURL, "not set"),
		active.ContextWindow,
		m.cfg.UI.MarkdownEnabled(),
		m.cfg.UI.MarkdownStyle,
		m.cfg.UI.ResumeLastSession,
	)
	fmt.Fprintf(&b, "- orchestrator: %s\n", m.cfg.ModelRoles.Orchestrator)
	fmt.Fprintf(&b, "- worker: %s\n", m.cfg.ModelRoles.Worker)
	fmt.Fprintf(&b, "- reviewer: %s\n", m.cfg.ModelRoles.Reviewer)
	fmt.Fprintf(&b, "- summarizer: %s\n", m.cfg.ModelRoles.Summarizer)
	fmt.Fprintf(&b, "\nTools:\nenabled: %t\nauto_execute_safe: %t\nmax_output_chars: %d\nmax_file_bytes: %d\n", m.cfg.Tools.Enabled, m.cfg.Tools.AutoExecute, m.cfg.Tools.MaxOutputChars, m.cfg.Tools.MaxFileBytes)
	fmt.Fprintf(&b, "\nSkills:\nenabled: %t\npaths: %s\n", m.cfg.Skills.SkillsEnabled(), strings.Join(m.cfg.Skills.Paths, ", "))
	workerProfile := m.workerCapacityProfile()
	fmt.Fprintf(&b, "\nWorkers:\nconcurrency: %d\nrequest_timeout_seconds: %d\ncapacity: %s\n", m.workerConcurrency(), int(m.workerRequestTimeout().Seconds()), workerProfile.Label)
	fmt.Fprintf(&b, "\nHooks:\nenabled: %t\ntimeout_seconds: %d\nevents: %d\n", m.cfg.Hooks.Enabled, m.cfg.Hooks.TimeoutSeconds, len(m.cfg.Hooks.Events))
	bell := m.cfg.Notifications.Bell != nil && *m.cfg.Notifications.Bell
	fmt.Fprintf(&b, "\nNotifications:\nenabled: %t\nbell: %t\nevents: %s\n", m.cfg.Notifications.Enabled, bell, strings.Join(m.cfg.Notifications.Events, ", "))
	fmt.Fprintf(&b, "\nEditor:\ncommand: %s\nargs: %s\nwait: %t\n", emptyFallback(m.cfg.Editor.Command, "VISUAL/EDITOR"), strings.Join(m.cfg.Editor.Args, " "), m.cfg.Editor.Wait)
	fmt.Fprintf(&b, "\nDebug:\nadapters: %d\nconfigurations: %d\ntimeout_seconds: %d\n", len(m.cfg.Debug.Adapters), len(m.cfg.Debug.Configurations), m.debugTimeoutSeconds())
	return strings.TrimRight(b.String(), "\n")
}

func (m model) diffCommandText() string {
	diff, err := m.gitDiff()
	if err != nil {
		return "Diff error: " + err.Error()
	}
	return renderDiffView(diff)
}

func (m model) lspManager() lsp.Manager {
	return lsp.NewManager(m.project.Root, m.project.Languages)
}

func (m model) lspCommandText() string {
	servers := m.lspManager().DetectServers()
	if len(servers) == 0 {
		return "LSP:\nNo language servers detected for this project."
	}
	var b strings.Builder
	b.WriteString("LSP:\n")
	for _, server := range servers {
		state := "missing"
		if server.Available {
			state = "available"
		}
		fmt.Fprintf(&b, "- %s: %s (%s)\n", server.Language, server.Command, state)
	}
	return strings.TrimRight(b.String(), "\n")
}

func (m model) diagnosticsCommandText() string {
	diagnostics, err := m.lspManager().Diagnostics(context.Background())
	if err != nil {
		return "Diagnostics error: " + err.Error()
	}
	if len(diagnostics) == 0 {
		return "Diagnostics:\nNo diagnostics."
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Diagnostics: %d\n", len(diagnostics))
	for i, diagnostic := range diagnostics {
		if i >= 100 {
			fmt.Fprintf(&b, "\n[truncated: %d diagnostics omitted]", len(diagnostics)-i)
			break
		}
		fmt.Fprintf(&b, "\n%d. %s:%d:%d [%s] %s", i+1, diagnostic.File, diagnostic.Line, diagnostic.Column, diagnostic.Severity, diagnostic.Message)
		if diagnostic.Source != "" {
			fmt.Fprintf(&b, " (%s)", diagnostic.Source)
		}
	}
	return b.String()
}

func (m model) symbolsCommandText(query string) string {
	symbols, err := m.lspManager().Symbols(context.Background(), query)
	if err != nil {
		return "Symbols error: " + err.Error()
	}
	if len(symbols) == 0 {
		return "Symbols:\nNo symbols found."
	}
	var b strings.Builder
	if query == "" {
		fmt.Fprintf(&b, "Symbols: %d\n", len(symbols))
	} else {
		fmt.Fprintf(&b, "Symbols: %d match(es) for %q\n", len(symbols), query)
	}
	for i, symbol := range symbols {
		if i >= 100 {
			fmt.Fprintf(&b, "\n[truncated: %d symbols omitted]", len(symbols)-i)
			break
		}
		fmt.Fprintf(&b, "\n%d. %s %s  %s:%d", i+1, symbol.Kind, symbol.Name, symbol.File, symbol.Line)
	}
	return b.String()
}

func (m model) definitionCommandText(name string) string {
	if strings.TrimSpace(name) == "" {
		return "Usage: /definition <symbol>"
	}
	symbol, ok, err := m.lspManager().Definition(context.Background(), name)
	if err != nil {
		return "Definition error: " + err.Error()
	}
	if !ok {
		return "Definition:\nNo definition found for " + name
	}
	return fmt.Sprintf("Definition:\n%s %s\n%s:%d", symbol.Kind, symbol.Name, symbol.File, symbol.Line)
}

func (m model) referencesCommandText(name string) string {
	if strings.TrimSpace(name) == "" {
		return "Usage: /references <symbol>"
	}
	refs, err := m.lspManager().References(context.Background(), name)
	if err != nil {
		return "References error: " + err.Error()
	}
	if len(refs) == 0 {
		return "References:\nNo references found for " + name
	}
	var b strings.Builder
	fmt.Fprintf(&b, "References: %d for %s\n", len(refs), name)
	for i, ref := range refs {
		if i >= 100 {
			fmt.Fprintf(&b, "\n[truncated: %d references omitted]", len(refs)-i)
			break
		}
		fmt.Fprintf(&b, "\n%d. %s:%d  %s", i+1, ref.File, ref.Line, ref.Text)
	}
	return b.String()
}

func emptyFallback(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}
