package app

const (
	lighthouseActionStart  = "Start Server"
	lighthouseActionStatus = "Server Status"
	lighthouseActionRemote = "Remote MCP Status"
	lighthouseActionLogs   = "View Logs"
	lighthouseActionStop   = "Stop Server"
	lighthouseActionBack   = "Back"
)

func LighthouseMenuItems() []string {
	return []string{lighthouseActionStart, lighthouseActionStatus, lighthouseActionRemote, lighthouseActionLogs, lighthouseActionStop, lighthouseActionBack}
}
