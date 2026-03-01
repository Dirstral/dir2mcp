// Package settings provides an interactive TUI for editing dirstral configuration.
package settings

import (
	"fmt"
	"log"
	"strings"

	"dir2mcp/internal/dirstral/config"
	"dir2mcp/internal/dirstral/host"
	"dir2mcp/internal/dirstral/ui"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// viewState tracks the current interaction mode.
type viewState int

const (
	stateBrowsing viewState = iota
	stateEditing
	stateConfirmQuit
)

// model is the bubbletea model for the settings editor.
type model struct {
	cfg                  config.Config
	fields               []config.FieldInfo
	baseline             map[string]string
	cachedPendingChanges []pendingChange
	pendingChangesValid  bool
	cursor               int
	state                viewState
	input                textinput.Model
	dirty                bool
	errMsg               string
	statusMsg            string
	width                int
	height               int
	showHelp             bool
}

func initialModel(cfg config.Config) model {
	ti := textinput.New()
	ti.CharLimit = 256
	ti.Width = 60

	fields := config.EffectiveFields(cfg)

	return model{
		cfg:      cfg,
		fields:   fields,
		baseline: snapshotValues(fields),
		cursor:   0,
		state:    stateBrowsing,
		input:    ti,
	}
}

func snapshotValues(fields []config.FieldInfo) map[string]string {
	out := make(map[string]string, len(fields))
	for _, field := range fields {
		out[field.Key] = field.Value
	}
	return out
}

// Run launches the settings TUI as its own tea.Program.
func Run(cfg config.Config) error {
	p := tea.NewProgram(initialModel(cfg), tea.WithAltScreen())
	_, err := p.Run()
	return err
}

func (m model) Init() tea.Cmd {
	return nil
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.input.Width = maxInt(settingsContentWidth(msg.Width)-4, 20)
		return m, nil
	case tea.KeyMsg:
		if msg.String() == "?" || msg.String() == "ctrl+k" {
			m.showHelp = !m.showHelp
			return m, nil
		}
		if m.showHelp {
			switch msg.String() {
			case "esc", "q", "?", "ctrl+k":
				m.showHelp = false
			}
			return m, nil
		}
		return m.handleKey(msg)
	}

	// Forward to text input when editing.
	if m.state == stateEditing {
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		return m, cmd
	}
	return m, nil
}

func (m model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch m.state {
	case stateBrowsing:
		return m.handleBrowsingKey(msg)
	case stateEditing:
		return m.handleEditingKey(msg)
	case stateConfirmQuit:
		return m.handleConfirmQuitKey(msg)
	}
	return m, nil
}

func (m model) handleBrowsingKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "q", "esc":
		if m.dirty {
			m.state = stateConfirmQuit
			m.errMsg = ""
			m.statusMsg = ""
			return m, nil
		}
		return m, tea.Quit
	case "ctrl+c":
		return m, tea.Quit
	case "up", "k":
		if m.cursor > 0 {
			m.cursor--
		}
		m.errMsg = ""
	case "down", "j":
		if m.cursor < len(m.fields)-1 {
			m.cursor++
		}
		m.errMsg = ""
	case "enter":
		m.startEditing()
		if m.state == stateEditing {
			return m, m.input.Focus()
		}
		return m, nil
	case "r":
		m.resetField()
	case "s":
		m.save()
	}
	return m, nil
}

func (m model) hasValidCursorField() bool {
	return len(m.fields) > 0 && m.cursor >= 0 && m.cursor < len(m.fields)
}

func (m *model) startEditing() {
	if !m.hasValidCursorField() {
		m.state = stateBrowsing
		m.errMsg = "No editable field selected"
		m.statusMsg = ""
		return
	}
	f := m.fields[m.cursor]
	m.state = stateEditing
	m.errMsg = ""
	m.statusMsg = ""
	m.input.SetValue(f.Value)
	m.input.CursorEnd()
	if f.Sensitive {
		m.input.EchoMode = textinput.EchoPassword
	} else {
		m.input.EchoMode = textinput.EchoNormal
	}
	m.input.Placeholder = fmt.Sprintf("Enter value for %s", f.Key)
}

func (m model) handleEditingKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.state = stateBrowsing
		m.errMsg = ""
		m.input.Blur()
		return m, nil
	case "enter":
		return m.commitEdit()
	}

	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	if !m.hasValidCursorField() {
		m.state = stateBrowsing
		m.input.Blur()
		m.errMsg = "Selected field is no longer available"
		return m, nil
	}
	// Live validation feedback.
	val := m.input.Value()
	if err := config.ValidateField(m.fields[m.cursor].Key, val); err != nil {
		m.errMsg = err.Error()
	} else {
		m.errMsg = ""
	}
	return m, cmd
}

func (m model) commitEdit() (tea.Model, tea.Cmd) {
	if !m.hasValidCursorField() {
		m.state = stateBrowsing
		m.input.Blur()
		m.errMsg = "No editable field selected"
		m.statusMsg = ""
		return m, nil
	}
	key := m.fields[m.cursor].Key
	val := m.input.Value()
	changed := m.fields[m.cursor].Value != val

	if err := config.ValidateField(key, val); err != nil {
		m.errMsg = err.Error()
		return m, nil
	}

	// Apply to the in-memory config or track secret change.
	if changed {
		if m.fields[m.cursor].Sensitive {
			m.fields[m.cursor].Value = val
		} else {
			config.ApplyField(&m.cfg, key, val)
			m.fields[m.cursor].Value = val
		}
		m.invalidatePendingChanges()
	}

	m.recomputeDirty()
	m.state = stateBrowsing
	m.errMsg = ""
	if changed {
		m.statusMsg = fmt.Sprintf("Updated %s", key)
	} else {
		m.statusMsg = fmt.Sprintf("No change for %s", key)
	}
	m.input.Blur()
	return m, nil
}

func (m *model) resetField() {
	if !m.hasValidCursorField() {
		m.errMsg = "No field selected"
		m.statusMsg = ""
		return
	}
	f := &m.fields[m.cursor]
	def := config.DefaultValueForField(f.Key)
	if def == "" && f.Sensitive {
		m.errMsg = "No default for secrets"
		return
	}
	if def == "" {
		m.errMsg = "No default value for this field"
		return
	}
	config.ApplyField(&m.cfg, f.Key, def)
	f.Value = def
	f.Source = config.SourceDefault
	m.invalidatePendingChanges()
	m.recomputeDirty()
	m.errMsg = ""
	m.statusMsg = fmt.Sprintf("Reset %s to default", f.Key)
}

func (m *model) save() {
	// Save non-sensitive fields to config.toml.
	if err := config.Save(m.cfg); err != nil {
		m.errMsg = fmt.Sprintf("Save failed: %v", err)
		return
	}

	// Save secrets to .env.local.
	secretFailures := make([]string, 0)
	failedSecretKeys := map[string]bool{}
	for _, f := range m.fields {
		if !f.Sensitive {
			continue
		}
		envKey := config.EnvVarForField(f.Key)
		if envKey == "" {
			log.Printf("settings: no environment mapping for %s; skipping secret save", f.Key)
			failedSecretKeys[f.Key] = true
			secretFailures = append(secretFailures, fmt.Sprintf("%s: no mapped environment variable", f.Key))
			continue
		}
		storedSecret, loadErr := config.LoadSecret(envKey)
		if loadErr != nil {
			failedSecretKeys[f.Key] = true
			secretFailures = append(secretFailures, fmt.Sprintf("%s: %v", f.Key, loadErr))
			continue
		}
		if strings.TrimSpace(storedSecret) == strings.TrimSpace(f.Value) {
			continue
		}
		var err error
		if strings.TrimSpace(f.Value) == "" {
			err = config.DeleteSecret(envKey)
		} else {
			err = config.SaveSecret(envKey, f.Value)
		}
		if err != nil {
			failedSecretKeys[f.Key] = true
			secretFailures = append(secretFailures, fmt.Sprintf("%s: %v", f.Key, err))
		}
	}
	if len(secretFailures) > 0 {
		m.applyPersistedSources(failedSecretKeys)

		newBaseline := snapshotValues(m.fields)
		for key := range failedSecretKeys {
			if oldValue, ok := m.baseline[key]; ok {
				newBaseline[key] = oldValue
			} else {
				delete(newBaseline, key)
			}
		}
		m.baseline = newBaseline
		m.invalidatePendingChanges()
		m.recomputeDirty()

		m.errMsg = fmt.Sprintf("Save secrets failed for %s. Some changes may have been saved.", strings.Join(secretFailures, "; "))
		m.statusMsg = ""
		return
	}

	m.applyPersistedSources(nil)

	m.baseline = snapshotValues(m.fields)
	m.invalidatePendingChanges()
	m.recomputeDirty()
	m.errMsg = ""
	m.statusMsg = "Settings saved"
}

func (m model) handleConfirmQuitKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "y", "Y":
		m.save()
		if m.errMsg != "" {
			// Save failed, stay in confirm state.
			return m, nil
		}
		return m, tea.Quit
	case "n", "N":
		return m, tea.Quit
	case "esc", "c":
		m.state = stateBrowsing
		m.statusMsg = ""
		return m, nil
	case "ctrl+c":
		return m, tea.Quit
	}
	return m, nil
}

type pendingChange struct {
	key       string
	oldValue  string
	newValue  string
	sensitive bool
}

func (m *model) invalidatePendingChanges() {
	m.cachedPendingChanges = nil
	m.pendingChangesValid = false
}

func (m *model) recomputeDirty() {
	m.dirty = len(m.pendingChanges()) > 0
}

func (m *model) applyPersistedSources(failedSecretKeys map[string]bool) {
	for i := range m.fields {
		before := m.baseline[m.fields[i].Key]
		if m.fields[i].Value == before {
			continue
		}
		if m.fields[i].Sensitive {
			if failedSecretKeys != nil && failedSecretKeys[m.fields[i].Key] {
				continue
			}
			if strings.TrimSpace(m.fields[i].Value) == "" {
				m.fields[i].Source = config.SourceDefault
			} else {
				m.fields[i].Source = config.SourceDotEnvLocal
			}
			continue
		}
		m.fields[i].Source = config.SourceConfigFile
	}
}

func (m *model) pendingChanges() []pendingChange {
	if m.pendingChangesValid {
		return m.cachedPendingChanges
	}
	changes := make([]pendingChange, 0)
	for _, field := range m.fields {
		before := m.baseline[field.Key]
		if field.Value == before {
			continue
		}
		changes = append(changes, pendingChange{
			key:       field.Key,
			oldValue:  before,
			newValue:  field.Value,
			sensitive: field.Sensitive,
		})
	}
	m.cachedPendingChanges = changes
	m.pendingChangesValid = true
	return m.cachedPendingChanges
}

func (m *model) pendingChangeLines(maxVisible int) []string {
	changes := m.pendingChanges()
	if len(changes) == 0 {
		return nil
	}

	if maxVisible <= 0 {
		maxVisible = len(changes)
	}

	lines := make([]string, 0, maxVisible+2)
	lines = append(lines, ui.Yellow.Render(fmt.Sprintf("Unsaved changes (%d):", len(changes))))
	for i, ch := range changes {
		if i >= maxVisible {
			lines = append(lines, settingsSubtleStyle.Render(fmt.Sprintf("  ... +%d more", len(changes)-maxVisible)))
			break
		}
		lines = append(lines, ui.Green.Render(fmt.Sprintf("  - %s: %s -> %s", ch.key, changeValueDisplay(ch.oldValue, ch.sensitive), changeValueDisplay(ch.newValue, ch.sensitive))))
	}
	return lines
}

var (
	settingsTitleStyle = lipgloss.NewStyle().
				Foreground(ui.ClrBrand).
				Bold(true).
				Underline(true)

	settingsMutedStyle = lipgloss.NewStyle().
				Foreground(ui.ClrMuted)

	settingsSubtleStyle = lipgloss.NewStyle().
				Foreground(ui.ClrSubtle)

	settingsPanelStyle = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(ui.ClrSubtle).
				Padding(1, 2).
				MarginTop(1).
				MarginBottom(1)

	settingsSelectedMarkerStyle = lipgloss.NewStyle().
					Foreground(ui.ClrBrand).
					Bold(true)

	settingsSelectedKeyStyle = lipgloss.NewStyle().
					Background(ui.ClrBrand).
					Foreground(lipgloss.Color("0")).
					Bold(true)

	settingsKeyStyle = lipgloss.NewStyle().
				Foreground(ui.ClrMuted)

	settingsValueStyle = lipgloss.NewStyle().
				Foreground(ui.ClrSubtle)

	settingsSelectedValueStyle = lipgloss.NewStyle().
					Foreground(ui.ClrBrand).
					Italic(true)

	settingsSourceStyle = lipgloss.NewStyle().
				Foreground(ui.ClrMuted).
				Italic(true)

	settingsSelectedSourceStyle = lipgloss.NewStyle().
					Foreground(ui.ClrYellow).
					Italic(true)
)

func (m model) View() string {
	viewWidth := m.width
	if viewWidth <= 0 {
		viewWidth = 100
	}

	tinyHeight := m.height > 0 && m.height < 14
	contentWidth := settingsContentWidth(viewWidth)
	panelWidth := settingsPanelWidth(viewWidth, contentWidth)

	title := settingsTitleStyle.MaxWidth(contentWidth).Render("Settings")
	intro := settingsMutedStyle.MaxWidth(contentWidth).Render("Edit config and API defaults for Dirstral.")
	header := joinVerticalNonEmpty(
		lipgloss.Center,
		lipgloss.PlaceHorizontal(panelWidth, lipgloss.Center, title),
		lipgloss.PlaceHorizontal(panelWidth, lipgloss.Center, intro),
	)

	panelLines := m.fieldRows(contentWidth)
	stateLines := m.stateLines()
	if len(stateLines) > 0 {
		panelLines = append(panelLines, "")
		panelLines = append(panelLines, stateLines...)
	}
	panel := settingsPanelStyle.MaxWidth(panelWidth).Render(strings.Join(panelLines, "\n"))

	body := joinVerticalNonEmpty(lipgloss.Center, header, panel)
	if m.showHelp {
		help := settingsPanelStyle.MaxWidth(panelWidth).Render(settingsHelpText(contentWidth, tinyHeight))
		body = joinVerticalNonEmpty(lipgloss.Center, body, help)
	}

	footer := settingsSubtleStyle.MaxWidth(contentWidth).Render(truncateText(m.controlsHint(), contentWidth))
	content := composeWithPinnedFooter(body, footer, m.height)

	if m.height <= 0 {
		return content
	}
	vAlign := lipgloss.Center
	if tinyHeight {
		vAlign = lipgloss.Top
	}
	return lipgloss.Place(viewWidth, m.height, lipgloss.Center, vAlign, content)
}

func (m model) fieldRows(contentWidth int) []string {
	maxRows := m.visibleRows()
	start, end := m.visibleRange(maxRows)

	keyWidth := clampInt(contentWidth/3, 18, 30)
	sourceWidth := clampInt(contentWidth/6, 10, 18)
	valueWidth := maxInt(contentWidth-keyWidth-sourceWidth-10, 12)

	lines := make([]string, 0, end-start+2)
	for i := start; i < end; i++ {
		f := m.fields[i]
		marker := settingsSubtleStyle.Render(" ")

		keyText := fitText(f.Key, keyWidth)
		valueText := fitText(fieldDisplayValue(f), valueWidth)
		sourceText := fitText("("+string(f.Source)+")", sourceWidth)

		keyCell := settingsKeyStyle.Width(keyWidth).Render(keyText)
		valueCell := settingsValueStyle.Width(valueWidth).Render(valueText)
		if i == m.cursor {
			marker = settingsSelectedMarkerStyle.Render(">")
			keyCell = settingsSelectedKeyStyle.Width(keyWidth).Render(keyText)
			valueCell = settingsSelectedValueStyle.Width(valueWidth).Render(valueText)
		}
		sourceCell := settingsSourceStyle.Width(sourceWidth).Render(sourceText)
		if i == m.cursor {
			sourceCell = settingsSelectedSourceStyle.Width(sourceWidth).Render(sourceText)
		}
		lines = append(lines, fmt.Sprintf("  %s %s  %s  %s", marker, keyCell, valueCell, sourceCell))
	}

	if len(lines) == 0 {
		lines = append(lines, settingsSubtleStyle.Render("  (no settings fields)"))
	}

	if len(m.fields) > maxRows {
		lines = append(lines, "")
		lines = append(lines, settingsSubtleStyle.Render(fmt.Sprintf("  [%d/%d]", m.cursor+1, len(m.fields))))
	}

	return lines
}

func (m model) stateLines() []string {
	lines := make([]string, 0, 4)

	switch m.state {
	case stateEditing:
		if !m.hasValidCursorField() {
			lines = append(lines, ui.Red.Render("Selected field is out of range."))
			break
		}
		lines = append(lines, settingsMutedStyle.Render("Editing "+m.fields[m.cursor].Key))
		lines = append(lines, "  "+m.input.View())
		if m.errMsg != "" {
			lines = append(lines, ui.Red.Render("  "+m.errMsg))
		}
	case stateConfirmQuit:
		lines = append(lines, ui.Yellow.Render("Unsaved changes. Save before quitting?"))
		if m.errMsg != "" {
			lines = append(lines, ui.Red.Render(m.errMsg))
		}
		lines = append(lines, m.pendingChangeLines(3)...)
	default:
		if m.errMsg != "" {
			lines = append(lines, ui.Red.Render(m.errMsg))
		}
		if m.statusMsg != "" {
			lines = append(lines, ui.Green.Render(m.statusMsg))
		}
		pending := m.pendingChangeLines(4)
		lines = append(lines, pending...)
		if len(pending) > 0 {
			lines = append(lines, settingsSubtleStyle.Render("Source labels update after save."))
		}

		// Issue 78: Show auth contract details
		health := host.CheckHealth()
		if health.AuthSourceType != "" && health.AuthSourceType != "unknown" {
			lines = append(lines, settingsSubtleStyle.Render(fmt.Sprintf("Active auth contract: %s", health.AuthSourceType)))
		}
	}

	return lines
}

func settingsHelpText(contentWidth int, tinyHeight bool) string {
	if tinyHeight {
		return settingsSubtleStyle.Render(truncateText("Keys: up/down or j/k move, enter edit, r reset, s save, esc/q back, ? toggle help", contentWidth))
	}

	lines := []string{
		settingsTitleStyle.Render("Settings Keymap"),
		settingsMutedStyle.Render("up/down or j/k  move selection"),
		settingsMutedStyle.Render("enter           edit selected value"),
		settingsMutedStyle.Render("r               reset selected value"),
		settingsMutedStyle.Render("s               save changes"),
		settingsMutedStyle.Render("esc/q           back/quit"),
		settingsMutedStyle.Render("? or ctrl+k     toggle this help"),
	}
	return lipgloss.NewStyle().MaxWidth(maxInt(contentWidth-2, 12)).Render(strings.Join(lines, "\n"))
}

func (m model) controlsHint() string {
	switch m.state {
	case stateEditing:
		return "type value · enter confirm · esc cancel · ? help"
	case stateConfirmQuit:
		return "y save & quit · n discard & quit · c/esc cancel · ? help"
	default:
		return "up/down or j/k move · enter edit · r reset · s save · esc/q back · ? help"
	}
}

func fieldDisplayValue(f config.FieldInfo) string {
	return changeValueDisplay(f.Value, f.Sensitive)
}

func changeValueDisplay(value string, sensitive bool) string {
	if sensitive {
		if strings.TrimSpace(value) == "" {
			return "(not set)"
		}
		return "****"
	}
	if strings.TrimSpace(value) == "" {
		return "(not set)"
	}
	return value
}

func (m model) visibleRows() int {
	maxRows := m.height - 12
	if m.showHelp {
		maxRows -= 4
	}
	if maxRows < 5 {
		maxRows = 5
	}
	return maxRows
}

func (m model) visibleRange(maxRows int) (int, int) {
	start := 0
	if m.cursor >= maxRows {
		start = m.cursor - maxRows + 1
	}
	end := start + maxRows
	if end > len(m.fields) {
		end = len(m.fields)
	}
	return start, end
}

func settingsContentWidth(viewWidth int) int {
	menuHorizChrome := 6
	if viewWidth >= 56 {
		menuHorizChrome = 10
	}
	return clampInt(viewWidth-menuHorizChrome, 28, 108)
}

func settingsPanelWidth(viewWidth, contentWidth int) int {
	menuHorizChrome := 6
	if viewWidth >= 56 {
		menuHorizChrome = 10
	}
	return contentWidth + menuHorizChrome - 2
}

func composeWithPinnedFooter(body, footer string, height int) string {
	if height <= 0 {
		return joinVerticalNonEmpty(lipgloss.Left, body, footer)
	}
	bodyLines := splitLines(body)
	footerLines := splitLines(footer)
	if len(footerLines) >= height {
		return strings.Join(footerLines[:height], "\n")
	}
	bodyBudget := height - len(footerLines)
	if bodyBudget < len(bodyLines) {
		if bodyBudget <= 0 {
			bodyLines = nil
		} else {
			bodyLines = bodyLines[:bodyBudget]
		}
	}
	lines := make([]string, 0, len(bodyLines)+len(footerLines))
	lines = append(lines, bodyLines...)
	lines = append(lines, footerLines...)
	return strings.Join(lines, "\n")
}

func joinVerticalNonEmpty(pos lipgloss.Position, items ...string) string {
	nonEmpty := make([]string, 0, len(items))
	for _, item := range items {
		if strings.TrimSpace(item) == "" {
			continue
		}
		nonEmpty = append(nonEmpty, item)
	}
	if len(nonEmpty) == 0 {
		return ""
	}
	return lipgloss.JoinVertical(pos, nonEmpty...)
}

func splitLines(s string) []string {
	if s == "" {
		return nil
	}
	return strings.Split(s, "\n")
}

func truncateText(s string, width int) string {
	if width <= 0 {
		return ""
	}
	r := []rune(strings.TrimSpace(s))
	if len(r) <= width {
		return string(r)
	}
	if width <= 3 {
		return string(r[:width])
	}
	return string(r[:width-3]) + "..."
}

func fitText(s string, width int) string {
	if width <= 0 {
		return ""
	}
	r := []rune(strings.TrimSpace(s))
	if len(r) <= width {
		return string(r) + strings.Repeat(" ", width-len(r))
	}
	if width <= 3 {
		return string(r[:width])
	}
	return string(r[:width-3]) + "..."
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func clampInt(v, minV, maxV int) int {
	if v < minV {
		return minV
	}
	if v > maxV {
		return maxV
	}
	return v
}
