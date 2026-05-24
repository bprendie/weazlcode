package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

const headerStripeWidth = 7

// asciiLogo returns the WeazlCode ASCII banner.
func asciiLogo() string {
	return ` __      __          _______________.__    .____  _______       .___________  
/  \    /  \ ____   /  |  \____    /|  |   |   _| \   _  \    __| _/\_____  \ 
\   \/\/   // __ \ /   |  |_/     / |  |   |  |   /  /_\  \  / __ |   _(__  < 
 \        /\  ___//    ^   /     /_ |  |__ |  |   \  \_/   \/ /_/ |  /       \
  \__/\  /  \___  >____   /_______ \|____/ |  |_   \_____  /\____ | /______  /
       \/       \/     |__|       \/       |____|        \/      \/        \/ `
}

func renderLogo(s string, width int) string {
	if width < maxLineWidth(s) {
		return "WEAZLCODE"
	}
	return gradientLogo(s)
}

func renderLogoBanner(s string, width int) string {
	if width < maxLineWidth(s)+headerStripeWidth+4 {
		return compactLogoBanner(width)
	}

	logoLines := strings.Split(gradientLogo(s), "\n")
	logoWidth := maxLineWidth(s)
	leftWidth := min(headerStripeWidth, max(0, width-logoWidth))
	rightWidth := max(0, width-leftWidth-logoWidth)

	var b strings.Builder
	for row, line := range logoLines {
		b.WriteString(diagonalStripeLine(leftWidth, row))
		b.WriteString(line)
		if pad := logoWidth - lipgloss.Width(line); pad > 0 {
			b.WriteString(strings.Repeat(" ", pad))
		}
		b.WriteString(diagonalStripeLine(rightWidth, row))
		if row < len(logoLines)-1 {
			b.WriteByte('\n')
		}
	}
	return b.String()
}

func compactLogoBanner(width int) string {
	label := " WEAZLCODE "
	labelWidth := lipgloss.Width(label)
	if width <= labelWidth {
		return lipgloss.NewStyle().Foreground(neonPink).Bold(true).Render(truncateMiddle("WEAZLCODE", width))
	}
	return lipgloss.JoinHorizontal(lipgloss.Left,
		diagonalStripeLine(min(headerStripeWidth, width-labelWidth), 0),
		lipgloss.NewStyle().Foreground(neonPink).Background(darkPanel).Bold(true).Render(label),
		diagonalStripeLine(max(0, width-labelWidth-headerStripeWidth), 0),
	)
}

func diagonalStripeLine(width, row int) string {
	if width <= 0 {
		return ""
	}
	var b strings.Builder
	for col := 0; col < width; col++ {
		if (col+row)%2 == 0 {
			b.WriteByte('/')
		} else {
			b.WriteByte(' ')
		}
	}
	return lipgloss.NewStyle().
		Foreground(lipgloss.Color("#C7D600")).
		Background(darkPanel).
		Bold(true).
		Render(b.String())
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
			t := float64(x) / float64(max(1, width-1))
			out.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color(sampleLogoColor(t))).Bold(true).Render(string(r)))
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
		width = max(width, lipgloss.Width(line))
	}
	return width
}

func sampleLogoColor(t float64) string {
	switch {
	case t < 0.34:
		return "#F25D94"
	case t < 0.67:
		return "#D75DFF"
	default:
		return "#7D56F4"
	}
}
