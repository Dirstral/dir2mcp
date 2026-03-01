package app

// LighthouseMenuConfig returns the menu configuration for the lighthouse submenu.
func LighthouseMenuConfig() MenuConfig {
	return MenuConfig{
		Title: "Lighthouse",
		Intro: []string{"Manage local dir2mcp host process or probe remote MCP."},
		Items: []MenuItem{
			{Label: lighthouseActionStart, Description: "Launch dir2mcp in background", Value: lighthouseActionStart},
			{Label: lighthouseActionStatus, Description: "Check process and endpoint health", Value: lighthouseActionStatus},
			{Label: lighthouseActionRemote, Description: "Probe configured remote MCP endpoint", Value: lighthouseActionRemote},
			{Label: lighthouseActionLogs, Description: "Tail dir2mcp output", Value: lighthouseActionLogs},
			{Label: lighthouseActionBack, Description: "Return to main menu", Value: lighthouseActionBack},
		},
		ShowLogo: false,
		Controls: "up/down or j/k move · enter select · esc/q back",
	}
}
