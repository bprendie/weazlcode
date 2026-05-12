package tools

import (
	"context"
	"strings"

	"github.com/charmbracelet/glamour"
)

type MarkdownCheckerTool struct{}

func NewMarkdownCheckerTool() *MarkdownCheckerTool {
	return &MarkdownCheckerTool{}
}

func (t *MarkdownCheckerTool) Name() string {
	return "markdown_checker"
}

func (t *MarkdownCheckerTool) Description() string {
	return "Render Markdown with Glamour and report whether it can be rendered successfully"
}

func (t *MarkdownCheckerTool) Parameters() []Parameter {
	return []Parameter{
		{
			Name:        "markdown",
			Type:        "string",
			Description: "Markdown text to render and validate",
			Required:    true,
		},
	}
}

func (t *MarkdownCheckerTool) SafetyLevel() SafetyLevel {
	return SafetyLevelSafe
}

// Execute runs the markdown validation against the Glamour renderer.
func (t *MarkdownCheckerTool) Execute(ctx context.Context, params map[string]any) (string, error) {
	select {
	case <-ctx.Done():
		return "", ctx.Err()
	default:
	}
	markdown, ok := params["markdown"].(string)
	if !ok || strings.TrimSpace(markdown) == "" {
		return "", errRequiredString("markdown")
	}
	rendered, err := glamourRender(markdown)
	if err != nil {
		return "FAIL: Markdown could not be rendered: " + err.Error(), nil
	}
	preview := rendered
	if len(preview) > 200 {
		preview = preview[:200] + "..."
	}
	return "PASS: Markdown rendered successfully. Preview: " + strings.TrimSpace(preview), nil
}

// Helper that renders markdown via Glamour without ANSI styling.
func glamourRender(markdown string) (string, error) {
	return glamour.Render(markdown, "notty")
}

func errRequiredString(name string) error {
	return &missingStringError{name: name}
}

type missingStringError struct {
	name string
}

func (e *missingStringError) Error() string {
	return e.name + " parameter is required"
}
