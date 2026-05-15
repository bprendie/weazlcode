package config

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
)

const appName = "weazlcode"

type Config struct {
	ActiveProvider string              `json:"active_provider"`
	Providers      map[string]Provider `json:"providers"`
	ModelRoles     ModelRoles          `json:"model_roles"`
	Database       Database            `json:"database"`
	UI             UI                  `json:"ui"`
	Tools          Tools               `json:"tools"`
	Skills         Skills              `json:"skills"`
}

type ModelRoles struct {
	Orchestrator string `json:"orchestrator,omitempty"`
	Worker       string `json:"worker,omitempty"`
	Reviewer     string `json:"reviewer,omitempty"`
	Summarizer   string `json:"summarizer,omitempty"`
}

type Provider struct {
	Type          string `json:"type"`
	ServerURL     string `json:"server_url"`
	Model         string `json:"model"`
	APIKey        string `json:"api_key,omitempty"`
	ContextWindow int    `json:"context_window,omitempty"`
}

type Database struct {
	Path string `json:"path"`
}

type UI struct {
	ResumeLastSession bool   `json:"resume_last_session"`
	RenderMarkdown    *bool  `json:"render_markdown,omitempty"`
	MarkdownStyle     string `json:"markdown_style,omitempty"`
}

type Tools struct {
	Enabled         bool     `json:"enabled"`
	AutoExecute     bool     `json:"auto_execute_safe"`
	AlphaVantageKey string   `json:"alpha_vantage_api_key,omitempty"`
	BraveAPIKey     string   `json:"brave_api_key,omitempty"`
	WorkspaceRoots  []string `json:"workspace_roots,omitempty"`
	MaxOutputChars  int      `json:"max_output_chars,omitempty"`
	MaxFileBytes    int64    `json:"max_file_bytes,omitempty"`
}

type Skills struct {
	Enabled *bool    `json:"enabled,omitempty"`
	Paths   []string `json:"paths,omitempty"`
}

func Load() (Config, string, error) {
	path := configPath()
	cfg, err := LoadPath(path)
	return cfg, path, err
}

func LoadPath(path string) (Config, error) {
	cfg := Default()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return cfg, err
	}
	if err := os.MkdirAll(filepath.Dir(cfg.Database.Path), 0o700); err != nil {
		return cfg, err
	}

	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return cfg, Save(path, cfg)
	}
	if err != nil {
		return cfg, err
	}
	if err := json.Unmarshal(b, &cfg); err != nil {
		return cfg, err
	}
	cfg.withDefaults()
	return cfg, nil
}

func Save(path string, cfg Config) error {
	cfg.withDefaults()
	b, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return os.WriteFile(path, append(b, '\n'), 0o600)
}

func Default() Config {
	dataDir := dataDir()
	return Config{
		ActiveProvider: "local-vllm",
		Providers: map[string]Provider{
			"local-vllm": {
				Type:          "vllm",
				ServerURL:     "http://localhost:8000",
				Model:         "local-model",
				ContextWindow: 32768,
			},
			"local-ollama": {
				Type:          "ollama",
				ServerURL:     "http://localhost:11434",
				Model:         "llama3.1",
				ContextWindow: 32768,
			},
		},
		Database: Database{Path: filepath.Join(dataDir, "weazlcode.sqlite3")},
		ModelRoles: ModelRoles{
			Orchestrator: "local-vllm",
			Worker:       "local-ollama",
			Reviewer:     "local-vllm",
			Summarizer:   "local-ollama",
		},
		UI: UI{
			ResumeLastSession: true,
			RenderMarkdown:    boolPtr(true),
			MarkdownStyle:     "dark",
		},
		Tools: Tools{
			Enabled:        false,
			AutoExecute:    true,
			MaxOutputChars: 12000,
			MaxFileBytes:   1024 * 1024,
		},
		Skills: Skills{
			Enabled: boolPtr(true),
			Paths:   defaultSkillPaths(),
		},
	}
}

func (c *Config) Active() Provider {
	if c.Providers == nil {
		c.Providers = map[string]Provider{}
	}
	p, ok := c.Providers[c.ActiveProvider]
	if !ok {
		return Provider{}
	}
	return p
}

func (c *Config) withDefaults() {
	def := Default()
	if c.ActiveProvider == "" {
		c.ActiveProvider = def.ActiveProvider
	}
	if c.Providers == nil || len(c.Providers) == 0 {
		c.Providers = def.Providers
	}
	if c.ModelRoles.Orchestrator == "" {
		c.ModelRoles.Orchestrator = c.ActiveProvider
	}
	if c.ModelRoles.Worker == "" {
		c.ModelRoles.Worker = c.ActiveProvider
	}
	if c.ModelRoles.Reviewer == "" {
		c.ModelRoles.Reviewer = c.ModelRoles.Orchestrator
	}
	if c.ModelRoles.Summarizer == "" {
		c.ModelRoles.Summarizer = c.ModelRoles.Worker
	}
	for name, provider := range c.Providers {
		if provider.ContextWindow <= 0 {
			provider.ContextWindow = 32768
			c.Providers[name] = provider
		}
	}
	if c.Database.Path == "" {
		c.Database.Path = def.Database.Path
	}
	if c.UI.RenderMarkdown == nil {
		c.UI.RenderMarkdown = def.UI.RenderMarkdown
	}
	if c.UI.MarkdownStyle == "" {
		c.UI.MarkdownStyle = def.UI.MarkdownStyle
	}
	if c.Tools.MaxOutputChars <= 0 {
		c.Tools.MaxOutputChars = def.Tools.MaxOutputChars
	}
	if c.Tools.MaxFileBytes <= 0 {
		c.Tools.MaxFileBytes = def.Tools.MaxFileBytes
	}
	if len(c.Skills.Paths) == 0 {
		c.Skills.Paths = def.Skills.Paths
	}
	if c.Skills.Enabled == nil {
		c.Skills.Enabled = def.Skills.Enabled
	}
}

func (c *Config) ProviderForRole(role string) Provider {
	name := c.ActiveProvider
	switch role {
	case "orchestrator":
		name = c.ModelRoles.Orchestrator
	case "worker":
		name = c.ModelRoles.Worker
	case "reviewer":
		name = c.ModelRoles.Reviewer
	case "summarizer":
		name = c.ModelRoles.Summarizer
	}
	if p, ok := c.Providers[name]; ok {
		return p
	}
	return c.Active()
}

func (ui UI) MarkdownEnabled() bool {
	return ui.RenderMarkdown == nil || *ui.RenderMarkdown
}

func (s Skills) SkillsEnabled() bool {
	return s.Enabled == nil || *s.Enabled
}

func boolPtr(v bool) *bool {
	return &v
}

func defaultSkillPaths() []string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return []string{".weazlcode/skills", ".agents/skills"}
	}
	return []string{
		".weazlcode/skills",
		".agents/skills",
		filepath.Join(home, ".codex", "skills"),
		filepath.Join(home, ".weazlcode", "skills"),
	}
}

func configPath() string {
	if p := os.Getenv("WEAZLCODE_CONFIG"); p != "" {
		return p
	}
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		return filepath.Join(xdg, appName, "config.json")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", appName, "config.json")
}

func dataDir() string {
	if p := os.Getenv("WEAZLCODE_DATA"); p != "" {
		return p
	}
	if xdg := os.Getenv("XDG_DATA_HOME"); xdg != "" {
		return filepath.Join(xdg, appName)
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".local", "share", appName)
}
