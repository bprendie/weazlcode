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

const (
	openMeteoGeocodingEndpoint = "https://geocoding-api.open-meteo.com/v1/search"
	openMeteoForecastEndpoint  = "https://api.open-meteo.com/v1/forecast"
)

// WeatherTool fetches current weather and a short forecast from Open-Meteo.
type WeatherTool struct {
	client *http.Client
}

func NewWeatherTool() *WeatherTool {
	return &WeatherTool{
		client: &http.Client{Timeout: 10 * time.Second},
	}
}

func (t *WeatherTool) Name() string {
	return "get_weather"
}

func (t *WeatherTool) Description() string {
	return "Get current weather and a short forecast for a city or location name"
}

func (t *WeatherTool) Parameters() []Parameter {
	return []Parameter{
		{
			Name:        "location",
			Type:        "string",
			Description: "City or location name, such as Boston, MA or Berlin",
			Required:    true,
		},
		{
			Name:        "days",
			Type:        "number",
			Description: "Number of forecast days from 1 to 7. Defaults to 3",
			Required:    false,
		},
		{
			Name:        "temperature_unit",
			Type:        "string",
			Description: "Temperature unit: fahrenheit or celsius. Defaults to fahrenheit",
			Required:    false,
			Enum:        []any{"fahrenheit", "celsius"},
		},
	}
}

func (t *WeatherTool) SafetyLevel() SafetyLevel {
	return SafetyLevelSafe
}

func (t *WeatherTool) Execute(ctx context.Context, params map[string]any) (string, error) {
	location, ok := params["location"].(string)
	location = strings.TrimSpace(location)
	if !ok || location == "" {
		return "", fmt.Errorf("location parameter is required and must be a string")
	}

	days := 3
	if _, ok := params["days"]; ok {
		n, err := getNumber(params, "days")
		if err != nil {
			return "", err
		}
		days = int(n)
	}
	if days < 1 {
		days = 1
	}
	if days > 7 {
		days = 7
	}

	tempUnit, _ := params["temperature_unit"].(string)
	tempUnit = strings.ToLower(strings.TrimSpace(tempUnit))
	if tempUnit == "" {
		tempUnit = "fahrenheit"
	}
	if tempUnit != "fahrenheit" && tempUnit != "celsius" {
		return "", fmt.Errorf("temperature_unit must be fahrenheit or celsius")
	}

	place, err := t.geocode(ctx, location)
	if err != nil {
		return "", err
	}
	forecast, err := t.forecast(ctx, place, days, tempUnit)
	if err != nil {
		return "", err
	}
	return formatWeather(place, forecast, tempUnit), nil
}

type weatherPlace struct {
	Name      string
	Admin1    string
	Country   string
	Latitude  float64
	Longitude float64
	Timezone  string
}

type weatherForecast struct {
	Current struct {
		Time                string  `json:"time"`
		Temperature2m       float64 `json:"temperature_2m"`
		ApparentTemperature float64 `json:"apparent_temperature"`
		Precipitation       float64 `json:"precipitation"`
		WeatherCode         int     `json:"weather_code"`
		WindSpeed10m        float64 `json:"wind_speed_10m"`
		WindDirection10m    int     `json:"wind_direction_10m"`
		RelativeHumidity2m  int     `json:"relative_humidity_2m"`
	} `json:"current"`
	CurrentUnits struct {
		Temperature2m       string `json:"temperature_2m"`
		ApparentTemperature string `json:"apparent_temperature"`
		Precipitation       string `json:"precipitation"`
		WindSpeed10m        string `json:"wind_speed_10m"`
	} `json:"current_units"`
	Daily struct {
		Time             []string  `json:"time"`
		WeatherCode      []int     `json:"weather_code"`
		TemperatureMax   []float64 `json:"temperature_2m_max"`
		TemperatureMin   []float64 `json:"temperature_2m_min"`
		PrecipitationSum []float64 `json:"precipitation_sum"`
	} `json:"daily"`
	DailyUnits struct {
		TemperatureMax   string `json:"temperature_2m_max"`
		TemperatureMin   string `json:"temperature_2m_min"`
		PrecipitationSum string `json:"precipitation_sum"`
	} `json:"daily_units"`
}

func (t *WeatherTool) geocode(ctx context.Context, location string) (weatherPlace, error) {
	u, err := url.Parse(openMeteoGeocodingEndpoint)
	if err != nil {
		return weatherPlace{}, err
	}
	q := u.Query()
	q.Set("name", location)
	q.Set("count", "1")
	q.Set("language", "en")
	q.Set("format", "json")
	u.RawQuery = q.Encode()

	var result struct {
		Results []struct {
			Name      string  `json:"name"`
			Admin1    string  `json:"admin1"`
			Country   string  `json:"country"`
			Latitude  float64 `json:"latitude"`
			Longitude float64 `json:"longitude"`
			Timezone  string  `json:"timezone"`
		} `json:"results"`
	}
	if err := t.getJSON(ctx, u.String(), &result); err != nil {
		return weatherPlace{}, err
	}
	if len(result.Results) == 0 {
		return weatherPlace{}, fmt.Errorf("no location found for %q", location)
	}
	first := result.Results[0]
	return weatherPlace{
		Name:      first.Name,
		Admin1:    first.Admin1,
		Country:   first.Country,
		Latitude:  first.Latitude,
		Longitude: first.Longitude,
		Timezone:  first.Timezone,
	}, nil
}

func (t *WeatherTool) forecast(ctx context.Context, place weatherPlace, days int, tempUnit string) (weatherForecast, error) {
	u, err := url.Parse(openMeteoForecastEndpoint)
	if err != nil {
		return weatherForecast{}, err
	}
	q := u.Query()
	q.Set("latitude", fmt.Sprintf("%.6f", place.Latitude))
	q.Set("longitude", fmt.Sprintf("%.6f", place.Longitude))
	q.Set("current", "temperature_2m,relative_humidity_2m,apparent_temperature,precipitation,weather_code,wind_speed_10m,wind_direction_10m")
	q.Set("daily", "weather_code,temperature_2m_max,temperature_2m_min,precipitation_sum")
	q.Set("forecast_days", fmt.Sprintf("%d", days))
	q.Set("timezone", place.Timezone)
	q.Set("wind_speed_unit", "mph")
	q.Set("precipitation_unit", "inch")
	if tempUnit == "fahrenheit" {
		q.Set("temperature_unit", "fahrenheit")
	}
	u.RawQuery = q.Encode()

	var forecast weatherForecast
	if err := t.getJSON(ctx, u.String(), &forecast); err != nil {
		return weatherForecast{}, err
	}
	return forecast, nil
}

func (t *WeatherTool) getJSON(ctx context.Context, requestURL string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "weazlcode (https://github.com/bprendie/weazlcode)")

	resp, err := t.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return fmt.Errorf("%s returned %s: %s", requestURL, resp.Status, strings.TrimSpace(string(body)))
	}
	return json.Unmarshal(body, out)
}

func formatWeather(place weatherPlace, forecast weatherForecast, tempUnit string) string {
	unit := forecast.CurrentUnits.Temperature2m
	if unit == "" {
		if tempUnit == "fahrenheit" {
			unit = "°F"
		} else {
			unit = "°C"
		}
	}

	location := place.Name
	if place.Admin1 != "" {
		location += ", " + place.Admin1
	}
	if place.Country != "" {
		location += ", " + place.Country
	}

	requestDate := time.Now().Format("2006-01-02")
	if place.Timezone != "" {
		if loc, err := time.LoadLocation(place.Timezone); err == nil {
			requestDate = time.Now().In(loc).Format("2006-01-02")
		}
	}

	var b strings.Builder
	fmt.Fprintf(&b, "Weather for %s\n", location)
	fmt.Fprintf(&b, "Request date: %s", requestDate)
	if place.Timezone != "" {
		fmt.Fprintf(&b, " (%s)", place.Timezone)
	}
	b.WriteByte('\n')
	fmt.Fprintf(&b, "Current: %.0f%s, feels like %.0f%s, %s\n",
		forecast.Current.Temperature2m,
		unit,
		forecast.Current.ApparentTemperature,
		unit,
		weatherCodeText(forecast.Current.WeatherCode),
	)
	fmt.Fprintf(&b, "Humidity: %d%%\n", forecast.Current.RelativeHumidity2m)
	fmt.Fprintf(&b, "Wind: %.0f %s from %d°\n", forecast.Current.WindSpeed10m, forecast.CurrentUnits.WindSpeed10m, forecast.Current.WindDirection10m)
	fmt.Fprintf(&b, "Precipitation: %.2f %s\n", forecast.Current.Precipitation, forecast.CurrentUnits.Precipitation)
	if forecast.Current.Time != "" {
		fmt.Fprintf(&b, "Observed: %s\n", forecast.Current.Time)
	}

	if len(forecast.Daily.Time) > 0 {
		b.WriteString("\nForecast:\n")
		for i, day := range forecast.Daily.Time {
			if i >= len(forecast.Daily.TemperatureMax) || i >= len(forecast.Daily.TemperatureMin) || i >= len(forecast.Daily.WeatherCode) {
				break
			}
			precip := 0.0
			if i < len(forecast.Daily.PrecipitationSum) {
				precip = forecast.Daily.PrecipitationSum[i]
			}
			label := day
			if day == requestDate {
				label += " (today)"
			}
			fmt.Fprintf(&b, "%s: %.0f%s/%.0f%s, %s, precip %.2f %s\n",
				label,
				forecast.Daily.TemperatureMax[i],
				unit,
				forecast.Daily.TemperatureMin[i],
				unit,
				weatherCodeText(forecast.Daily.WeatherCode[i]),
				precip,
				forecast.DailyUnits.PrecipitationSum,
			)
		}
	}
	return strings.TrimSpace(b.String())
}

func weatherCodeText(code int) string {
	switch code {
	case 0:
		return "clear sky"
	case 1, 2, 3:
		return "partly cloudy"
	case 45, 48:
		return "fog"
	case 51, 53, 55:
		return "drizzle"
	case 56, 57:
		return "freezing drizzle"
	case 61, 63, 65:
		return "rain"
	case 66, 67:
		return "freezing rain"
	case 71, 73, 75:
		return "snow"
	case 77:
		return "snow grains"
	case 80, 81, 82:
		return "rain showers"
	case 85, 86:
		return "snow showers"
	case 95:
		return "thunderstorm"
	case 96, 99:
		return "thunderstorm with hail"
	default:
		return fmt.Sprintf("weather code %d", code)
	}
}
