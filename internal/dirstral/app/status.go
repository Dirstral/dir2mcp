package app

import (
	"time"

	"dir2mcp/internal/dirstral/host"
	tea "github.com/charmbracelet/bubbletea"
)

const statusPollInterval = 3 * time.Second

// lighthouseStatusMsg carries the latest health check result.
type lighthouseStatusMsg struct {
	Health host.HealthInfo
}

// pollLighthouseStatus returns a tea.Cmd that checks lighthouse health and
// sends the result as a lighthouseStatusMsg.
func pollLighthouseStatus() tea.Msg {
	return lighthouseStatusMsg{Health: host.CheckHealth()}
}

// tickLighthouseStatus returns a tea.Cmd that waits for the poll interval
// then triggers another health check.
func tickLighthouseStatus() tea.Cmd {
	return tea.Tick(statusPollInterval, func(t time.Time) tea.Msg {
		return lighthouseStatusMsg{Health: host.CheckHealth()}
	})
}
