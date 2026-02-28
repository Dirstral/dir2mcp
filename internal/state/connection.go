package state

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

// ConnectionJSON is the schema written to connection.json (SPEC §4.3).
type ConnectionJSON struct {
	Transport string            `json:"transport"`
	URL       string            `json:"url"`
	Headers   map[string]string `json:"headers"`
	Session   ConnectionSession `json:"session"`
	TokenSource string          `json:"token_source,omitempty"`
	TokenFile  string          `json:"token_file,omitempty"`
}

type ConnectionSession struct {
	UsesMCPSessionID      bool   `json:"uses_mcp_session_id"`
	HeaderName            string `json:"header_name"`
	AssignedOnInitialize  bool   `json:"assigned_on_initialize"`
}

// WriteConnectionJSON writes connection.json. If authMode is "file:<path>", sets token_source and token_file.
func WriteConnectionJSON(stateDir, mcpURL, token, tokenSource, authMode string) error {
	headers := map[string]string{
		"MCP-Protocol-Version": "2025-11-25",
	}
	if token != "" && tokenSource != "none" {
		headers["Authorization"] = "Bearer " + token
	}

	conn := ConnectionJSON{
		Transport: "mcp_streamable_http",
		URL:       mcpURL,
		Headers:   headers,
		Session: ConnectionSession{
			UsesMCPSessionID:     true,
			HeaderName:          "MCP-Session-Id",
			AssignedOnInitialize: true,
		},
		TokenSource: tokenSource,
	}
	if strings.HasPrefix(authMode, "file:") {
		conn.TokenSource = "file"
		conn.TokenFile = strings.TrimPrefix(authMode, "file:")
	}
	data, err := json.MarshalIndent(conn, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(stateDir, "connection.json"), data, 0600)
}
