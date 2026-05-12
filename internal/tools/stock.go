package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

// StockPriceTool fetches current stock prices
type StockPriceTool struct {
	apiKey string
	client *http.Client
}

// NewStockPriceTool creates a new stock price tool
// Uses Alpha Vantage API (free tier: 25 requests/day)
func NewStockPriceTool(apiKey string) *StockPriceTool {
	return &StockPriceTool{
		apiKey: apiKey,
		client: &http.Client{Timeout: 10 * time.Second},
	}
}

func (t *StockPriceTool) Name() string {
	return "get_stock_price"
}

func (t *StockPriceTool) Description() string {
	return "Get the current stock price and basic information for a given stock symbol (e.g., IBM, AAPL, GOOGL)"
}

func (t *StockPriceTool) Parameters() []Parameter {
	return []Parameter{
		{
			Name:        "symbol",
			Type:        "string",
			Description: "The stock ticker symbol (e.g., IBM, AAPL, MSFT)",
			Required:    true,
		},
	}
}

func (t *StockPriceTool) SafetyLevel() SafetyLevel {
	return SafetyLevelSafe // Read-only operation
}

func (t *StockPriceTool) Execute(ctx context.Context, params map[string]any) (string, error) {
	symbol, ok := params["symbol"].(string)
	if !ok || symbol == "" {
		return "", fmt.Errorf("symbol parameter is required and must be a string")
	}

	if t.apiKey == "" {
		return "", fmt.Errorf("Alpha Vantage API key not configured. Get a free key at https://www.alphavantage.co/support/#api-key")
	}

	// Build API URL
	baseURL := "https://www.alphavantage.co/query"
	params2 := url.Values{}
	params2.Set("function", "GLOBAL_QUOTE")
	params2.Set("symbol", symbol)
	params2.Set("apikey", t.apiKey)

	reqURL := baseURL + "?" + params2.Encode()

	// Make request
	req, err := http.NewRequestWithContext(ctx, "GET", reqURL, nil)
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := t.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to fetch stock data: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("API returned status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read response: %w", err)
	}

	// Parse response
	var result struct {
		GlobalQuote struct {
			Symbol           string `json:"01. symbol"`
			Price            string `json:"05. price"`
			Volume           string `json:"06. volume"`
			LatestTradingDay string `json:"07. latest trading day"`
			PreviousClose    string `json:"08. previous close"`
			Change           string `json:"09. change"`
			ChangePercent    string `json:"10. change percent"`
		} `json:"Global Quote"`
		Note  string `json:"Note"`
		Error string `json:"Error Message"`
	}

	if err := json.Unmarshal(body, &result); err != nil {
		return "", fmt.Errorf("failed to parse response: %w", err)
	}

	// Check for API errors
	if result.Error != "" {
		return "", fmt.Errorf("API error: %s", result.Error)
	}

	if result.Note != "" {
		return "", fmt.Errorf("API limit reached: %s", result.Note)
	}

	// Check if we got data
	if result.GlobalQuote.Symbol == "" {
		return "", fmt.Errorf("no data found for symbol %s", symbol)
	}

	// Format response
	return fmt.Sprintf(
		"Stock: %s\nPrice: $%s\nChange: %s (%s)\nVolume: %s\nPrevious Close: $%s\nLatest Trading Day: %s",
		result.GlobalQuote.Symbol,
		result.GlobalQuote.Price,
		result.GlobalQuote.Change,
		result.GlobalQuote.ChangePercent,
		result.GlobalQuote.Volume,
		result.GlobalQuote.PreviousClose,
		result.GlobalQuote.LatestTradingDay,
	), nil
}
