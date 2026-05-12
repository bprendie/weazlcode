package tui

import (
	"strings"

	"github.com/charmbracelet/glamour"
)

type markdownRenderer struct {
	enabled bool
	style   string
	width   int
	term    *glamour.TermRenderer
}

func (r *markdownRenderer) Render(s string, width int) string {
	if !r.enabled {
		return wrapText(s, width)
	}
	width = max(10, width)
	if r.term == nil || r.width != width {
		term, err := newGlamourRenderer(width, r.style)
		if err != nil {
			return wrapText(s, width)
		}
		r.term = term
		r.width = width
	}
	out, err := r.term.Render(s)
	if err != nil {
		return wrapText(s, width)
	}
	return strings.TrimRight(out, "\n")
}

func (r *markdownRenderer) Resize(width int) {
	if r.width != width {
		r.term = nil
		r.width = 0
	}
}

func newGlamourRenderer(width int, style string) (*glamour.TermRenderer, error) {
	options := []glamour.TermRendererOption{glamour.WithWordWrap(width)}
	if style == "" || style == "auto" {
		options = append(options, glamour.WithStandardStyle("dark"))
	} else {
		options = append(options, glamour.WithStandardStyle(style))
	}
	return glamour.NewTermRenderer(options...)
}
