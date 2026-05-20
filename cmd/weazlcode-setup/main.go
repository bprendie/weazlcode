package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/bprendie/weazlcode/internal/config"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "setup: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	reader := bufio.NewReader(os.Stdin)
	cfg, cfgPath, err := config.Load()
	if err != nil {
		return err
	}

	fmt.Println("WeazlCode provider setup")
	fmt.Println("Configure the local worker first. Then choose an optional LLM provider for planning and review.")
	providerType := askChoice(reader, "Local worker provider", []string{"ollama", "vllm"}, "ollama")
	defaultURL := "http://localhost:8000"
	if providerType == "ollama" {
		defaultURL = "http://localhost:11434"
	}

	fmt.Println(urlHelp(providerType))
	serverURL := normalizeServerURL(providerType, askString(reader, "Base URL", defaultURL))
	fmt.Printf("Using base URL: %s\n", serverURL)
	models, err := fetchModels(providerType, serverURL)
	if err != nil {
		fmt.Printf("Could not query models: %v\n", err)
		model := askString(reader, "Model name", defaultModel(providerType))
		contextWindow := askContextWindow(reader)
		return writeConfig(cfgPath, configureSetupOptions(reader, cfg), providerType, serverURL, model, contextWindow)
	}
	if len(models) == 0 {
		fmt.Println("Provider returned no models.")
		model := askString(reader, "Model name", defaultModel(providerType))
		contextWindow := askContextWindow(reader)
		return writeConfig(cfgPath, configureSetupOptions(reader, cfg), providerType, serverURL, model, contextWindow)
	}

	model := askModel(reader, models)
	contextWindow := askContextWindow(reader)
	return writeConfig(cfgPath, configureSetupOptions(reader, cfg), providerType, serverURL, model, contextWindow)
}

func configureSetupOptions(reader *bufio.Reader, cfg config.Config) config.Config {
	cfg = configureLLMProvider(reader, cfg)
	return configureTools(reader, cfg)
}

func fetchModels(providerType, serverURL string) ([]string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	serverURL = normalizeServerURL(providerType, serverURL)

	switch providerType {
	case "vllm":
		return fetchVLLMModels(ctx, serverURL)
	case "ollama":
		return fetchOllamaModels(ctx, serverURL)
	default:
		return nil, fmt.Errorf("unsupported provider %q", providerType)
	}
}

func fetchVLLMModels(ctx context.Context, serverURL string) ([]string, error) {
	var body struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := getJSON(ctx, strings.TrimRight(serverURL, "/")+"/v1/models", &body); err != nil {
		return nil, err
	}
	models := make([]string, 0, len(body.Data))
	for _, model := range body.Data {
		if model.ID != "" {
			models = append(models, model.ID)
		}
	}
	return models, nil
}

func fetchOllamaModels(ctx context.Context, serverURL string) ([]string, error) {
	var body struct {
		Models []struct {
			Name string `json:"name"`
		} `json:"models"`
	}
	if err := getJSON(ctx, strings.TrimRight(serverURL, "/")+"/api/tags", &body); err != nil {
		return nil, err
	}
	models := make([]string, 0, len(body.Models))
	for _, model := range body.Models {
		if model.Name != "" {
			models = append(models, model.Name)
		}
	}
	return models, nil
}

func getJSON(ctx context.Context, url string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return fmt.Errorf("%s returned %s", url, resp.Status)
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

func writeConfig(cfgPath string, cfg config.Config, providerType, serverURL, model string, contextWindow int) error {
	providerID := "primary-" + providerType
	previousActive := cfg.ActiveProvider
	serverURL = normalizeServerURL(providerType, serverURL)
	if cfg.Providers == nil {
		cfg.Providers = map[string]config.Provider{}
	}
	if contextWindow <= 0 {
		contextWindow = 32768
	}
	cfg.ActiveProvider = providerID
	cfg.Providers[providerID] = config.Provider{
		Type:          providerType,
		ServerURL:     serverURL,
		Model:         model,
		ContextWindow: contextWindow,
	}
	cfg.ModelRoles.Worker = providerID
	cfg.ModelRoles.Summarizer = providerID
	if cfg.Workers.Concurrency <= config.RecommendedWorkerConcurrency("ollama") {
		cfg.Workers.Concurrency = config.RecommendedWorkerConcurrency(providerType)
	}
	if cfg.ModelRoles.Orchestrator == "" || cfg.ModelRoles.Orchestrator == previousActive {
		cfg.ModelRoles.Orchestrator = providerID
	}
	if cfg.ModelRoles.Reviewer == "" || cfg.ModelRoles.Reviewer == previousActive {
		cfg.ModelRoles.Reviewer = cfg.ModelRoles.Orchestrator
	}
	if err := config.Save(cfgPath, cfg); err != nil {
		return err
	}
	fmt.Printf("Wrote config: %s\n", cfgPath)
	fmt.Printf("Active provider: %s (%s / %s)\n", providerID, providerType, model)
	fmt.Printf("Roles: orchestrator=%s worker=%s reviewer=%s summarizer=%s\n", cfg.ModelRoles.Orchestrator, cfg.ModelRoles.Worker, cfg.ModelRoles.Reviewer, cfg.ModelRoles.Summarizer)
	fmt.Printf("Worker concurrency: %d\n", cfg.Workers.Concurrency)
	if cfg.Tools.Enabled {
		fmt.Println("Tools enabled")
	}
	return nil
}

func configureLLMProvider(reader *bufio.Reader, cfg config.Config) config.Config {
	fmt.Println("")
	fmt.Println("Optional LLM provider for planning and review")
	fmt.Println("OpenAI or Claude are good starting points. Choose none to use the configured local model provider.")
	provider := askChoice(reader, "Planning/review LLM provider", []string{"openai", "claude", "custom", "none"}, "openai")
	if provider == "none" {
		fmt.Println("Warning: planning and review will use the local model provider. Planning mode may not be as robust as OpenAI or Claude.")
		cfg.ModelRoles.Orchestrator = ""
		cfg.ModelRoles.Reviewer = ""
		return cfg
	}
	profile := llmProviderProfile(provider)
	apiKey := askSecret(reader, "LLM API key", "")
	baseURL := normalizeServerURL(profile.Type, askString(reader, "LLM provider base URL", profile.BaseURL))
	model := askString(reader, "Planning model", profile.Model)
	reviewerModel := askString(reader, "Review model", model)
	if cfg.Providers == nil {
		cfg.Providers = map[string]config.Provider{}
	}
	cfg.Providers["planning-llm"] = config.Provider{
		Type:          profile.Type,
		ServerURL:     baseURL,
		Model:         model,
		APIKey:        apiKey,
		ContextWindow: 128000,
	}
	cfg.ModelRoles.Orchestrator = "planning-llm"
	cfg.Providers["review-llm"] = config.Provider{
		Type:          profile.Type,
		ServerURL:     baseURL,
		Model:         reviewerModel,
		APIKey:        apiKey,
		ContextWindow: 128000,
	}
	cfg.ModelRoles.Reviewer = "review-llm"
	return cfg
}

type llmProviderDefaults struct {
	Type    string
	BaseURL string
	Model   string
}

func llmProviderProfile(provider string) llmProviderDefaults {
	switch provider {
	case "openai":
		return llmProviderDefaults{Type: "vllm", BaseURL: "https://api.openai.com", Model: "gpt-4.1"}
	case "claude":
		return llmProviderDefaults{Type: "anthropic", BaseURL: "https://api.anthropic.com", Model: "claude-sonnet-4-5"}
	default:
		return llmProviderDefaults{Type: "vllm", BaseURL: "https://host", Model: "model-name"}
	}
}

func configureTools(reader *bufio.Reader, cfg config.Config) config.Config {
	fmt.Println("")
	fmt.Println("Optional tool API keys")
	fmt.Println("Leave a key blank to keep the current value.")

	alphaKey := askSecret(reader, "Alpha Vantage API key for stock lookups", cfg.Tools.AlphaVantageKey)
	braveKey := askSecret(reader, "Brave Search API key for web search", cfg.Tools.BraveAPIKey)
	workspaceRoots := askRoots(reader, cfg.Tools.WorkspaceRoots)
	enableTools := askChoice(reader, "Enable tools", []string{"yes", "no"}, "yes") == "yes"

	cfg.Tools.AlphaVantageKey = alphaKey
	cfg.Tools.BraveAPIKey = braveKey
	cfg.Tools.WorkspaceRoots = workspaceRoots
	cfg.Tools.AutoExecute = true
	cfg.Tools.Enabled = enableTools
	return cfg
}

func askChoice(reader *bufio.Reader, label string, choices []string, def string) string {
	for {
		fmt.Printf("%s:\n", label)
		for i, choice := range choices {
			fmt.Printf("  %d) %s\n", i+1, choice)
		}
		answer := askString(reader, "Select", def)
		if answer == "" {
			return def
		}
		if n, err := strconv.Atoi(answer); err == nil && n >= 1 && n <= len(choices) {
			return choices[n-1]
		}
		for _, choice := range choices {
			if strings.EqualFold(answer, choice) {
				return choice
			}
		}
		fmt.Println("Enter a menu number or provider name.")
	}
}

func askModel(reader *bufio.Reader, models []string) string {
	for {
		fmt.Println("Models:")
		for i, model := range models {
			fmt.Printf("  %d) %s\n", i+1, model)
		}
		answer := askString(reader, "Select model", "1")
		n, err := strconv.Atoi(answer)
		if err == nil && n >= 1 && n <= len(models) {
			return models[n-1]
		}
		if answer != "" && contains(models, answer) {
			return answer
		}
		fmt.Println("Enter a menu number or exact model name.")
	}
}

func askContextWindow(reader *bufio.Reader) int {
	choices := []struct {
		Name   string
		Tokens int
		Note   string
	}{
		{Name: "small", Tokens: 8192},
		{Name: "medium", Tokens: 16384},
		{Name: "large", Tokens: 32768},
		{Name: "xl", Tokens: 128000, Note: "may cause out-of-memory errors on smaller local servers"},
	}
	for {
		fmt.Println("Context window:")
		for i, choice := range choices {
			label := fmt.Sprintf("  %d) %s (%d tokens)", i+1, choice.Name, choice.Tokens)
			if choice.Note != "" {
				label += " - " + choice.Note
			}
			fmt.Println(label)
		}
		answer := askString(reader, "Select", "large")
		if answer == "" {
			return 32768
		}
		if n, err := strconv.Atoi(answer); err == nil && n >= 1 && n <= len(choices) {
			return choices[n-1].Tokens
		}
		for _, choice := range choices {
			if strings.EqualFold(answer, choice.Name) {
				return choice.Tokens
			}
		}
		fmt.Println("Enter small, medium, large, xl, or a menu number.")
	}
}

func askString(reader *bufio.Reader, label, def string) string {
	if def == "" {
		fmt.Printf("%s: ", label)
	} else {
		fmt.Printf("%s [%s]: ", label, def)
	}
	answer, err := reader.ReadString('\n')
	if err != nil && answer == "" {
		return def
	}
	answer = strings.TrimSpace(answer)
	if answer == "" {
		return def
	}
	return answer
}

func askSecret(reader *bufio.Reader, label, current string) string {
	if current != "" {
		fmt.Printf("%s [saved; blank keeps, - clears]: ", label)
	} else {
		fmt.Printf("%s [optional]: ", label)
	}
	answer, err := reader.ReadString('\n')
	if err != nil && answer == "" {
		return current
	}
	answer = strings.TrimSpace(answer)
	if answer == "" {
		return current
	}
	if answer == "-" {
		return ""
	}
	return answer
}

func askRoots(reader *bufio.Reader, current []string) []string {
	if len(current) > 0 {
		fmt.Printf("Workspace roots [saved: %s; blank keeps, - clears]: ", strings.Join(current, ", "))
	} else {
		fmt.Print("Workspace roots for file/shell/sql tools [optional, comma-separated]: ")
	}
	answer, err := reader.ReadString('\n')
	if err != nil && answer == "" {
		return current
	}
	answer = strings.TrimSpace(answer)
	if answer == "" {
		return current
	}
	if answer == "-" {
		return nil
	}
	parts := strings.Split(answer, ",")
	roots := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			roots = append(roots, part)
		}
	}
	return roots
}

func urlHelp(providerType string) string {
	switch providerType {
	case "vllm":
		return "Enter the base vLLM server URL only, without /v1. Example: http://localhost:8000"
	case "ollama":
		return "Enter the base Ollama server URL only, without /api. Example: http://localhost:11434"
	default:
		return "Enter the provider base URL."
	}
}

func normalizeServerURL(providerType, raw string) string {
	u := strings.TrimRight(strings.TrimSpace(raw), "/")
	switch providerType {
	case "vllm":
		u = strings.TrimSuffix(u, "/v1")
	case "ollama":
		u = strings.TrimSuffix(u, "/api")
	case "anthropic":
		u = strings.TrimSuffix(u, "/v1")
	}
	return u
}

func defaultModel(providerType string) string {
	if providerType == "ollama" {
		return "llama3.1"
	}
	return "local-model"
}

func contains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
