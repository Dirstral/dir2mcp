package protocol

const (
	ToolNameSearch           = "dir2mcp.search"
	ToolNameAsk              = "dir2mcp.ask"
	ToolNameAskAudio         = "dir2mcp.ask_audio"
	ToolNameOpenFile         = "dir2mcp.open_file"
	ToolNameListFiles        = "dir2mcp.list_files"
	ToolNameStats            = "dir2mcp.stats"
	ToolNameTranscribe       = "dir2mcp.transcribe"
	ToolNameAnnotate         = "dir2mcp.annotate"
	ToolNameTranscribeAndAsk = "dir2mcp.transcribe_and_ask"
)

const (
	ErrorCodeUnauthorized     = "UNAUTHORIZED"
	ErrorCodeSessionNotFound  = "SESSION_NOT_FOUND"
	ErrorCodeIndexNotReady    = "INDEX_NOT_READY"
	ErrorCodeFileNotFound     = "FILE_NOT_FOUND"
	ErrorCodePermissionDenied = "PERMISSION_DENIED"
	ErrorCodeRateLimited      = "RATE_LIMITED"
)

const (
	DefaultListenAddr = "127.0.0.1:8087"
	DefaultMCPPath    = "/mcp"

	MCPSessionHeader = "MCP-Session-Id"
)
