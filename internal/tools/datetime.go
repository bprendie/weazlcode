package tools

import (
	"context"
	"fmt"
	"time"
)

// DateTimeTool reports the current local time or the time in a named location.
type DateTimeTool struct{}

func NewDateTimeTool() *DateTimeTool {
	return &DateTimeTool{}
}

func (t *DateTimeTool) Name() string {
	return "get_current_time"
}

func (t *DateTimeTool) Description() string {
	return "Get the current date and time. Optionally accepts an IANA timezone such as America/New_York or UTC"
}

func (t *DateTimeTool) Parameters() []Parameter {
	return []Parameter{
		{
			Name:        "timezone",
			Type:        "string",
			Description: "Optional IANA timezone name. Use UTC for Coordinated Universal Time",
			Required:    false,
		},
	}
}

func (t *DateTimeTool) SafetyLevel() SafetyLevel {
	return SafetyLevelSafe
}

func (t *DateTimeTool) Execute(ctx context.Context, params map[string]any) (string, error) {
	timezone, _ := params["timezone"].(string)
	if timezone == "" {
		timezone = time.Local.String()
	}

	loc, err := time.LoadLocation(timezone)
	if err != nil {
		return "", fmt.Errorf("invalid timezone %q: %w", timezone, err)
	}

	now := time.Now().In(loc)
	return fmt.Sprintf("Current time in %s: %s", loc.String(), now.Format(time.RFC1123Z)), nil
}
