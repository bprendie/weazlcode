package tui

import (
	"fmt"
	"math"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

type rgb struct {
	r float64
	g float64
	b float64
}

var logoStops = []rgb{
	hexRGB(0xFF5FA2),
	hexRGB(0xD75DFF),
	hexRGB(0x7D56F4),
}

func renderLogo(s string, width int) string {
	wordmark := gradientLogo(s)
	logoWidth := maxLineWidth(s)
	if width <= 0 || width < logoWidth+10 {
		return wordmark
	}

	fieldStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#F3E600")).Bold(true)
	leftWidth := 6
	gapWidth := 1
	rightWidth := max(4, width-logoWidth-leftWidth-(gapWidth*2))
	lines := strings.Split(wordmark, "\n")
	var out strings.Builder
	for i, line := range lines {
		right := max(1, rightWidth-i)
		out.WriteString(fieldStyle.Render(strings.Repeat("╱", leftWidth)))
		out.WriteString(strings.Repeat(" ", gapWidth))
		out.WriteString(line)
		if pad := logoWidth - lipgloss.Width(line); pad > 0 {
			out.WriteString(strings.Repeat(" ", pad))
		}
		out.WriteString(strings.Repeat(" ", gapWidth))
		out.WriteString(fieldStyle.Render(strings.Repeat("╱", right)))
		if i < len(lines)-1 {
			out.WriteByte('\n')
		}
	}
	return out.String()
}

func gradientLogo(s string) string {
	lines := strings.Split(s, "\n")
	width := maxLineWidth(s)

	var out strings.Builder
	for y, line := range lines {
		for x, r := range line {
			if r == ' ' {
				out.WriteRune(r)
				continue
			}
			t := float64(x) / float64(width-1)
			color := sampleGradient(t, logoStops)
			out.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color(color)).Bold(true).Render(string(r)))
		}
		if y < len(lines)-1 {
			out.WriteByte('\n')
		}
	}
	return out.String()
}

func maxLineWidth(s string) int {
	width := 1
	for _, line := range strings.Split(s, "\n") {
		if lipgloss.Width(line) > width {
			width = lipgloss.Width(line)
		}
	}
	return width
}

func sampleGradient(t float64, stops []rgb) string {
	if len(stops) == 0 {
		return "#FFFFFF"
	}
	if len(stops) == 1 {
		return stops[0].hex()
	}
	scaled := t * float64(len(stops)-1)
	i := int(math.Floor(scaled))
	if i >= len(stops)-1 {
		return stops[len(stops)-1].hex()
	}
	local := scaled - float64(i)
	a := stops[i]
	b := stops[i+1]
	return rgb{
		r: a.r + ((b.r - a.r) * local),
		g: a.g + ((b.g - a.g) * local),
		b: a.b + ((b.b - a.b) * local),
	}.hex()
}

func hexRGB(v int) rgb {
	return rgb{
		r: float64((v >> 16) & 0xff),
		g: float64((v >> 8) & 0xff),
		b: float64(v & 0xff),
	}
}

func (c rgb) hex() string {
	return fmt.Sprintf("#%02X%02X%02X", clamp(c.r), clamp(c.g), clamp(c.b))
}

func clamp(v float64) int {
	if v < 0 {
		return 0
	}
	if v > 255 {
		return 255
	}
	return int(math.Round(v))
}
