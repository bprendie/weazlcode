package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const braveWebSearchEndpoint = "https://api.search.brave.com/res/v1/web/search"

// WebSearchTool searches the web with Brave Search API.
type WebSearchTool struct {
	apiKey string
	client *http.Client
}

func NewWebSearchTool(apiKey string) *WebSearchTool {
	return &WebSearchTool{
		apiKey: apiKey,
		client: &http.Client{Timeout: 10 * time.Second},
	}
}

func (t *WebSearchTool) Name() string {
	return "web_search"
}

func (t *WebSearchTool) Description() string {
	return "Search the web for recent or factual information and return top result titles, URLs, and snippets"
}

func (t *WebSearchTool) Parameters() []Parameter {
	return []Parameter{
		{
			Name:        "query",
			Type:        "string",
			Description: "Search query",
			Required:    true,
		},
		{
			Name:        "count",
			Type:        "number",
			Description: "Number of search results to return, from 1 to 10. Defaults to 5",
			Required:    false,
		},
		{
			Name:        "country",
			Type:        "string",
			Description: "Optional two-letter country code for localized results, such as US",
			Required:    false,
		},
		{
			Name:        "search_lang",
			Type:        "string",
			Description: "Optional two-letter search language, such as en",
			Required:    false,
		},
	}
}

func (t *WebSearchTool) SafetyLevel() SafetyLevel {
	return SafetyLevelSafe
}

func (t *WebSearchTool) Execute(ctx context.Context, params map[string]any) (string, error) {
	query, ok := params["query"].(string)
	query = strings.TrimSpace(query)
	if !ok || query == "" {
		return "", fmt.Errorf("query parameter is required and must be a string")
	}
	if t.apiKey == "" {
		return "", fmt.Errorf("Brave API key not configured")
	}

	count := 5
	if _, ok := params["count"]; ok {
		n, err := getNumber(params, "count")
		if err != nil {
			return "", err
		}
		count = int(n)
	}
	if count < 1 {
		count = 1
	}
	if count > 10 {
		count = 10
	}

	u, err := url.Parse(braveWebSearchEndpoint)
	if err != nil {
		return "", err
	}
	q := u.Query()
	q.Set("q", query)
	q.Set("count", fmt.Sprintf("%d", count))
	if country, _ := params["country"].(string); strings.TrimSpace(country) != "" {
		q.Set("country", strings.ToUpper(strings.TrimSpace(country)))
	}
	if searchLang, _ := params["search_lang"].(string); strings.TrimSpace(searchLang) != "" {
		q.Set("search_lang", strings.ToLower(strings.TrimSpace(searchLang)))
	}
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-Subscription-Token", t.apiKey)

	resp, err := t.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to search web: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return "", fmt.Errorf("Brave Search API returned %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}

	var result struct {
		Web struct {
			Results []struct {
				Title       string `json:"title"`
				URL         string `json:"url"`
				Description string `json:"description"`
				Age         string `json:"age"`
			} `json:"results"`
		} `json:"web"`
		Query struct {
			Original string `json:"original"`
		} `json:"query"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return "", fmt.Errorf("failed to parse response: %w", err)
	}
	if len(result.Web.Results) == 0 {
		return fmt.Sprintf("No web results found for %q", query), nil
	}

	var b strings.Builder
	if result.Query.Original != "" {
		fmt.Fprintf(&b, "Web results for %q:\n", result.Query.Original)
	} else {
		fmt.Fprintf(&b, "Web results for %q:\n", query)
	}
	for i, item := range result.Web.Results {
		fmt.Fprintf(&b, "\n%d. %s\n%s", i+1, strings.TrimSpace(item.Title), strings.TrimSpace(item.URL))
		if item.Age != "" {
			fmt.Fprintf(&b, "\nDate: %s", strings.TrimSpace(item.Age))
		}
		if item.Description != "" {
			fmt.Fprintf(&b, "\n%s", strings.TrimSpace(item.Description))
		}
		b.WriteByte('\n')
	}
	return strings.TrimSpace(b.String()), nil
}
