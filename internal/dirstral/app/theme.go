package app

import (
	"fmt"
	"os"

	"dir2mcp/internal/dirstral/ui"
	"github.com/charmbracelet/lipgloss"
)

// Legacy ANSI constants kept for logo rendering which pre-dates lipgloss.
const (
	ansiReset = "\033[0m"

	colorBrandStrong = "\033[38;5;214m"
	colorBrand       = "\033[38;5;208m"
	colorMuted       = "\033[38;5;245m"
	colorSubtle      = "\033[38;5;242m"
	colorError       = "\033[38;5;203m"
	colorBold        = "\033[1m"

	colorTint1 = "\033[38;5;220m"
	colorTint2 = "\033[38;5;214m"
	colorTint3 = "\033[38;5;208m"
	colorTint4 = "\033[38;5;202m"
	colorTint5 = "\033[38;5;196m"
	colorTint6 = "\033[38;5;166m"
)

// Lipgloss color palette.
var (
	clrBrandStrong = lipgloss.Color(ui.ClrBrand)
	clrMuted       = lipgloss.Color(ui.ClrMuted)
	clrSubtle      = lipgloss.Color(ui.ClrSubtle)
	clrGreen       = lipgloss.Color(ui.ClrGreen)
)

// Reusable lipgloss styles.
var (
	styleBrandStrong  = lipgloss.NewStyle().Foreground(clrBrandStrong).Bold(true)
	styleTitle        = lipgloss.NewStyle().Foreground(clrBrandStrong).Bold(true).Underline(true)
	styleMuted        = lipgloss.NewStyle().Foreground(clrMuted)
	styleSubtle       = lipgloss.NewStyle().Foreground(clrSubtle)
	styleSelected     = lipgloss.NewStyle().Foreground(clrBrandStrong).Bold(true)
	styleSelectedRow  = lipgloss.NewStyle().Background(clrBrandStrong).Foreground(lipgloss.Color("0")).Bold(true)
	styleDescription  = lipgloss.NewStyle().Foreground(clrSubtle).Italic(true)
	styleSelectedDesc = lipgloss.NewStyle().Foreground(clrBrandStrong).Italic(true)
	styleGreen        = lipgloss.NewStyle().Foreground(clrGreen)
	styleMenuBox      = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(clrSubtle).Padding(1, 2).MarginTop(1).MarginBottom(1)
)

// paint wraps text in raw ANSI codes. Used by logo rendering.
// Respects NO_COLOR environment variable.
func paint(text string, styles ...string) string {
	if text == "" {
		return ""
	}
	if os.Getenv("NO_COLOR") != "" {
		return text
	}
	styled := ""
	for _, style := range styles {
		styled += style
	}
	return styled + text + ansiReset
}

func statusLine(label, details string) string {
	return fmt.Sprintf("%s %s", paint(label, colorBrandStrong, colorBold), paint(details, colorMuted))
}

func errorLine(err error) string {
	return paint(fmt.Sprintf("error: %v", err), colorError, colorBold)
}
