package tui

import (
	"fmt"

	"github.com/charmbracelet/lipgloss"
)

// Cyberpunk color palette with enhanced glow effects
var (
	// Neon accents with glow
	neonPink   = lipgloss.Color("#FF1493") // Hot pink
	neonCyan   = lipgloss.Color("#00FFFF") // Cyan
	neonGreen  = lipgloss.Color("#39FF14") // Neon green
	neonPurple = lipgloss.Color("#BF00FF") // Electric purple
	neonOrange = lipgloss.Color("#FF6600") // Neon orange
	neonBlue   = lipgloss.Color("#00D9FF") // Electric blue

	// Glow variants (lighter versions for shadow effects)
	glowPink   = lipgloss.Color("#FF69B4")
	glowCyan   = lipgloss.Color("#7FFFD4")
	glowGreen  = lipgloss.Color("#7FFF00")
	glowPurple = lipgloss.Color("#DA70D6")

	// Dark backgrounds with depth
	deepBlack = lipgloss.Color("#0A0A0F") // Almost black
	darkPanel = lipgloss.Color("#1A1A2E") // Dark blue-black
	darkGray  = lipgloss.Color("#16213E") // Darker gray-blue
	midGray   = lipgloss.Color("#2A2A3E") // Mid-tone for depth

	// Text colors with glow
	textBright = lipgloss.Color("#FFFFFF") // Pure white
	textDim    = lipgloss.Color("#888888") // Dim gray
	textGlow   = lipgloss.Color("#E0E0FF") // Slight glow
	textNeon   = lipgloss.Color("#00FFFF") // Neon text

	// Status colors
	statusOK    = neonGreen
	statusWarn  = neonOrange
	statusError = neonPink
	statusInfo  = neonCyan
)

// CyberpunkStyles holds all lipgloss styles with enhanced cyberpunk aesthetic.
type CyberpunkStyles struct {
	// Layout
	Frame     lipgloss.Style
	Header    lipgloss.Style
	Status    lipgloss.Style
	Panel     lipgloss.Style
	PanelGlow lipgloss.Style

	// Input
	Input      lipgloss.Style
	InputFocus lipgloss.Style

	// Text roles
	User      lipgloss.Style
	Assistant lipgloss.Style
	System    lipgloss.Style

	// Status indicators
	Success lipgloss.Style
	Warning lipgloss.Style
	Error   lipgloss.Style
	Info    lipgloss.Style

	// Progress
	Spinner     lipgloss.Style
	Progress    lipgloss.Style
	ProgressBar lipgloss.Style

	// Help
	Help      lipgloss.Style
	HelpKey   lipgloss.Style
	HelpValue lipgloss.Style

	// Badges
	Badge       lipgloss.Style
	BadgeGlow   lipgloss.Style
	BadgeActive lipgloss.Style

	// Special effects
	Glow      lipgloss.Style
	Highlight lipgloss.Style
	Pulse     lipgloss.Style
}

// NewCyberpunkStyles creates a new cyberpunk-themed style set with visual effects.
func NewCyberpunkStyles() CyberpunkStyles {
	return CyberpunkStyles{
		// Layout styles with depth
		Frame: lipgloss.NewStyle().
			Background(deepBlack).
			Foreground(textGlow),

		Header: lipgloss.NewStyle().
			Foreground(neonPink).
			Bold(true),

		Status: lipgloss.NewStyle().
			Foreground(neonCyan).
			Bold(true),

		Panel: lipgloss.NewStyle().
			Background(darkPanel).
			Foreground(textGlow).
			Border(lipgloss.NormalBorder()).
			BorderForeground(neonPurple).
			Padding(0, 1),

		PanelGlow: lipgloss.NewStyle().
			Background(midGray).
			Foreground(textNeon).
			Border(lipgloss.ThickBorder()).
			BorderForeground(neonCyan).
			Padding(1, 2),

		// Input styles with glow effect
		Input: lipgloss.NewStyle().
			Border(lipgloss.ThickBorder()).
			BorderForeground(neonPurple).
			Foreground(textBright).
			Padding(0, 1),

		InputFocus: lipgloss.NewStyle().
			Border(lipgloss.ThickBorder()).
			BorderForeground(neonPink).
			Foreground(textNeon).
			Padding(0, 1),

		// Role styles with symbols
		User: lipgloss.NewStyle().
			Foreground(neonGreen).
			Background(darkGray).
			Bold(true).
			Padding(0, 1),

		Assistant: lipgloss.NewStyle().
			Foreground(neonPink).
			Background(darkGray).
			Bold(true).
			Padding(0, 1),

		System: lipgloss.NewStyle().
			Foreground(neonCyan).
			Background(darkGray).
			Bold(true).
			Padding(0, 1),

		// Status styles with backgrounds
		Success: lipgloss.NewStyle().
			Foreground(deepBlack).
			Background(statusOK).
			Bold(true).
			Padding(0, 1),

		Warning: lipgloss.NewStyle().
			Foreground(deepBlack).
			Background(statusWarn).
			Bold(true).
			Padding(0, 1),

		Error: lipgloss.NewStyle().
			Foreground(textBright).
			Background(statusError).
			Bold(true).
			Padding(0, 1),

		Info: lipgloss.NewStyle().
			Foreground(deepBlack).
			Background(statusInfo).
			Bold(true).
			Padding(0, 1),

		// Progress styles with animation feel
		Spinner: lipgloss.NewStyle().
			Foreground(neonPurple).
			Bold(true),

		Progress: lipgloss.NewStyle().
			Foreground(neonCyan).
			Background(darkGray).
			Border(lipgloss.RoundedBorder()).
			BorderForeground(neonBlue).
			Padding(0, 1),

		ProgressBar: lipgloss.NewStyle().
			Background(neonGreen).
			Foreground(deepBlack),

		// Help styles
		Help: lipgloss.NewStyle().
			Foreground(textDim).
			Background(deepBlack),

		HelpKey: lipgloss.NewStyle().
			Foreground(neonCyan).
			Background(darkGray).
			Bold(true).
			Padding(0, 1),

		HelpValue: lipgloss.NewStyle().
			Foreground(textGlow),

		// Badge styles with glow
		Badge: lipgloss.NewStyle().
			Foreground(deepBlack).
			Background(neonCyan).
			Bold(true).
			Padding(0, 1).
			Border(lipgloss.NormalBorder()).
			BorderForeground(glowCyan),

		BadgeGlow: lipgloss.NewStyle().
			Foreground(deepBlack).
			Background(neonPink).
			Bold(true).
			Padding(0, 1).
			Border(lipgloss.ThickBorder()).
			BorderForeground(glowPink),

		BadgeActive: lipgloss.NewStyle().
			Foreground(deepBlack).
			Background(neonGreen).
			Bold(true).
			Padding(0, 1).
			Border(lipgloss.DoubleBorder()).
			BorderForeground(glowGreen),

		// Special effect styles
		Glow: lipgloss.NewStyle().
			Foreground(neonCyan).
			Bold(true).
			Underline(true),

		Highlight: lipgloss.NewStyle().
			Foreground(neonPink).
			Background(darkPanel).
			Bold(true).
			Padding(0, 1),

		Pulse: lipgloss.NewStyle().
			Foreground(neonPurple).
			Bold(true).
			Italic(true),
	}
}

// RenderProviderBadge renders a provider badge with enhanced cyberpunk styling.
func (s CyberpunkStyles) RenderProviderBadge(role, provider, model string) string {
	roleStyle := lipgloss.NewStyle().
		Foreground(deepBlack).
		Background(neonPurple).
		Bold(true).
		Padding(0, 1).
		Border(lipgloss.NormalBorder()).
		BorderForeground(glowPurple)

	providerStyle := lipgloss.NewStyle().
		Foreground(neonCyan).
		Bold(true)

	modelStyle := lipgloss.NewStyle().
		Foreground(textGlow)

	separator := lipgloss.NewStyle().
		Foreground(neonPurple).
		Render("▸")

	return lipgloss.JoinHorizontal(
		lipgloss.Left,
		roleStyle.Render(role),
		" ",
		providerStyle.Render(provider),
		separator,
		modelStyle.Render(model),
	)
}

// RenderTaskProgress renders an enhanced task progress indicator.
func (s CyberpunkStyles) RenderTaskProgress(completed, total int, active int) string {
	// Progress bar
	barWidth := 10
	filledWidth := 0
	if total > 0 {
		filledWidth = (completed * barWidth) / total
	}

	filled := lipgloss.NewStyle().
		Background(neonGreen).
		Foreground(deepBlack).
		Render(lipgloss.NewStyle().Width(filledWidth).Render(""))

	empty := lipgloss.NewStyle().
		Background(darkGray).
		Render(lipgloss.NewStyle().Width(barWidth - filledWidth).Render(""))

	bar := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(neonCyan).
		Render(filled + empty)

	// Stats
	completedStyle := lipgloss.NewStyle().
		Foreground(neonGreen).
		Bold(true)

	totalStyle := lipgloss.NewStyle().
		Foreground(textGlow)

	stats := lipgloss.JoinHorizontal(
		lipgloss.Left,
		completedStyle.Render(fmt.Sprintf("%d", completed)),
		"/",
		totalStyle.Render(fmt.Sprintf("%d", total)),
	)

	result := bar + " " + stats

	// Active workers indicator
	if active > 0 {
		activeStyle := lipgloss.NewStyle().
			Foreground(deepBlack).
			Background(neonPurple).
			Bold(true).
			Padding(0, 1).
			Border(lipgloss.ThickBorder()).
			BorderForeground(glowPurple)

		result += " " + activeStyle.Render(fmt.Sprintf("⚡%d", active))
	}

	return result
}

// RenderStatusLine renders a cyberpunk-styled status line with separators.
func (s CyberpunkStyles) RenderStatusLine(parts ...string) string {
	separator := lipgloss.NewStyle().
		Foreground(neonPurple).
		Bold(true).
		Render(" ▸ ")

	styled := make([]string, 0, len(parts)*2-1)
	for i, part := range parts {
		if i > 0 {
			styled = append(styled, separator)
		}
		styled = append(styled, part)
	}

	return s.Status.Render(lipgloss.JoinHorizontal(lipgloss.Left, styled...))
}

// RenderSpinner renders an animated spinner with enhanced glow effect.
func (s CyberpunkStyles) RenderSpinner(frame string, text string) string {
	spinnerStyle := lipgloss.NewStyle().
		Foreground(neonPurple).
		Background(darkGray).
		Bold(true).
		Padding(0, 1).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(glowPurple)

	textStyle := lipgloss.NewStyle().
		Foreground(neonCyan).
		Bold(true)

	return lipgloss.JoinHorizontal(
		lipgloss.Left,
		spinnerStyle.Render(frame),
		" ",
		textStyle.Render(text),
	)
}

// RenderBox renders a content box with glow border effect.
func (s CyberpunkStyles) RenderBox(title, content string, glowColor lipgloss.Color) string {
	titleStyle := lipgloss.NewStyle().
		Foreground(deepBlack).
		Background(glowColor).
		Bold(true).
		Padding(0, 1)

	boxStyle := lipgloss.NewStyle().
		Border(lipgloss.ThickBorder()).
		BorderForeground(glowColor).
		Padding(1, 2).
		Background(darkPanel).
		Foreground(textGlow)

	return lipgloss.JoinVertical(
		lipgloss.Left,
		titleStyle.Render(title),
		boxStyle.Render(content),
	)
}
