package app

import (
	"time"

	"dir2mcp/internal/dirstral/ui"
	tea "github.com/charmbracelet/bubbletea"
)

const (
	revealDelay = 30 * time.Millisecond
	tipDelay    = 4 * time.Second
)

// revealTickMsg triggers showing the next menu item.
type revealTickMsg struct{}

// tipTickMsg triggers rotating the startup tip line.
type tipTickMsg struct{}

// animationsEnabled returns false when NO_COLOR is set or TERM=dumb.
func animationsEnabled() bool {
	return ui.Enabled()
}

// tickReveal returns a tea.Cmd that fires a revealTickMsg after the delay.
func tickReveal() tea.Cmd {
	return tea.Tick(revealDelay, func(t time.Time) tea.Msg {
		return revealTickMsg{}
	})
}

// tickStartupTip returns a tea.Cmd that rotates startup tips.
func tickStartupTip() tea.Cmd {
	return tea.Tick(tipDelay, func(t time.Time) tea.Msg {
		return tipTickMsg{}
	})
}
