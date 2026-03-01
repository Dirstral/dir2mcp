package test

import (
	"reflect"
	"strings"
	"testing"

	"dir2mcp/internal/dirstral/app"
	tea "github.com/charmbracelet/bubbletea"
)

func TestLighthouseMenuItemsOrder(t *testing.T) {
	want := []string{"Start Server", "Server Status", "Remote MCP Status", "View Logs", "Stop Server", "Back"}
	if got := app.LighthouseMenuItems(); !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected lighthouse options: got %v want %v", got, want)
	}
}

func TestLighthouseMenuConfigIncludesLogsByDefault(t *testing.T) {
	cfg := app.LighthouseMenuConfig()
	got := make([]string, 0, len(cfg.Items))
	for _, item := range cfg.Items {
		got = append(got, item.Value)
	}
	want := []string{"Start Server", "Server Status", "Remote MCP Status", "View Logs", "Back"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected lighthouse menu config items: got %v want %v", got, want)
	}
}

func TestLighthouseMenuControlsAreKeyboardFirst(t *testing.T) {
	cfg := app.LighthouseMenuConfig()
	if !strings.Contains(cfg.Controls, "j/k") {
		t.Fatalf("expected j/k controls, got %q", cfg.Controls)
	}
	if !strings.Contains(cfg.Controls, "esc/q") {
		t.Fatalf("expected esc/q controls, got %q", cfg.Controls)
	}
}

// TestLighthouseMenuHelpOverlayToggleVisibility verifies help visibility toggles in Lighthouse menu.
func TestLighthouseMenuHelpOverlayToggleVisibility(t *testing.T) {
	m := app.NewMenuModel(app.LighthouseMenuConfig())
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 90, Height: 28})
	m = updated.(app.MenuModel)

	if strings.Contains(m.View(), "Lighthouse Keymap") {
		t.Fatalf("expected help overlay hidden by default")
	}

	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'?'}})
	withHelp := updated.(app.MenuModel)
	if !strings.Contains(withHelp.View(), "Lighthouse Keymap") {
		t.Fatalf("expected help overlay visible after ?")
	}

	updated, _ = withHelp.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'?'}})
	withoutHelp := updated.(app.MenuModel)
	if strings.Contains(withoutHelp.View(), "Lighthouse Keymap") {
		t.Fatalf("expected help overlay hidden after second ?")
	}
}
