package tools

import (
	"context"
	"strings"
	"testing"
)

func TestRegistryListIsSorted(t *testing.T) {
	registry := NewRegistry()
	registry.Register(NewStockPriceTool("key"))
	registry.Register(NewCalculatorTool())
	registry.Register(NewDateTimeTool())
	registry.Register(NewWeatherTool())

	got := registry.List()
	names := make([]string, len(got))
	for i, tool := range got {
		names[i] = tool.Name()
	}

	want := []string{"calculate", "get_current_time", "get_stock_price", "get_weather"}
	for i := range want {
		if names[i] != want[i] {
			t.Fatalf("names[%d] = %q, want %q; all names: %v", i, names[i], want[i], names)
		}
	}
}

func TestRegistryExecuteParsesRawArguments(t *testing.T) {
	registry := NewRegistry()
	registry.Register(NewCalculatorTool())

	result := registry.Execute(context.Background(), ToolCall{
		ID:      "call_1",
		Name:    "calculate",
		RawArgs: `{"operation":"multiply","a":6,"b":7}`,
	})

	if result.Error != "" {
		t.Fatalf("unexpected error: %s", result.Error)
	}
	if result.ToolCallID != "call_1" {
		t.Fatalf("ToolCallID = %q, want call_1", result.ToolCallID)
	}
	if !strings.Contains(result.Content, "42") {
		t.Fatalf("Content = %q, want result containing 42", result.Content)
	}
}

func TestDateTimeToolUTC(t *testing.T) {
	result, err := NewDateTimeTool().Execute(context.Background(), map[string]any{
		"timezone": "UTC",
	})
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if !strings.Contains(result, "Current time in UTC:") {
		t.Fatalf("result = %q, want UTC time output", result)
	}
}

func TestDateTimeToolInvalidTimezone(t *testing.T) {
	_, err := NewDateTimeTool().Execute(context.Background(), map[string]any{
		"timezone": "No/SuchZone",
	})
	if err == nil {
		t.Fatal("Execute returned nil error for invalid timezone")
	}
}

func TestWebSearchToolRequiresQuery(t *testing.T) {
	_, err := NewWebSearchTool("key").Execute(context.Background(), map[string]any{})
	if err == nil {
		t.Fatal("Execute returned nil error without query")
	}
}

func TestWebSearchToolRequiresAPIKey(t *testing.T) {
	_, err := NewWebSearchTool("").Execute(context.Background(), map[string]any{
		"query": "weazlcode",
	})
	if err == nil {
		t.Fatal("Execute returned nil error without API key")
	}
}

func TestWeatherToolRequiresLocation(t *testing.T) {
	_, err := NewWeatherTool().Execute(context.Background(), map[string]any{})
	if err == nil {
		t.Fatal("Execute returned nil error without location")
	}
}

func TestWeatherToolRejectsInvalidTemperatureUnit(t *testing.T) {
	_, err := NewWeatherTool().Execute(context.Background(), map[string]any{
		"location":         "Boston",
		"temperature_unit": "kelvin",
	})
	if err == nil {
		t.Fatal("Execute returned nil error for invalid temperature_unit")
	}
}

func TestFormatWeather(t *testing.T) {
	place := weatherPlace{Name: "Philadelphia", Admin1: "Pennsylvania", Country: "United States"}
	var forecast weatherForecast
	forecast.Current.Temperature2m = 72
	forecast.Current.ApparentTemperature = 70
	forecast.Current.WeatherCode = 61
	forecast.Current.RelativeHumidity2m = 54
	forecast.Current.WindSpeed10m = 8
	forecast.Current.WindDirection10m = 270
	forecast.CurrentUnits.Temperature2m = "°F"
	forecast.CurrentUnits.WindSpeed10m = "mph"
	forecast.CurrentUnits.Precipitation = "inch"
	forecast.Daily.Time = []string{"2026-05-06"}
	forecast.Daily.TemperatureMax = []float64{75}
	forecast.Daily.TemperatureMin = []float64{58}
	forecast.Daily.WeatherCode = []int{1}
	forecast.Daily.PrecipitationSum = []float64{0.1}
	forecast.DailyUnits.PrecipitationSum = "inch"

	got := formatWeather(place, forecast, "fahrenheit")
	if !strings.Contains(got, "Weather for Philadelphia, Pennsylvania, United States") {
		t.Fatalf("forecast output missing location: %q", got)
	}
	if !strings.Contains(got, "Current: 72°F, feels like 70°F, rain") {
		t.Fatalf("forecast output missing current weather: %q", got)
	}
	if !strings.Contains(got, "2026-05-06: 75°F/58°F") {
		t.Fatalf("forecast output missing daily forecast: %q", got)
	}
}

func TestWeatherCodeText(t *testing.T) {
	if got := weatherCodeText(61); got != "rain" {
		t.Fatalf("weatherCodeText(61) = %q, want rain", got)
	}
	if got := weatherCodeText(999); !strings.Contains(got, "999") {
		t.Fatalf("weatherCodeText(999) = %q, want code in fallback", got)
	}
}
