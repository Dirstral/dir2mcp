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
	conn := ConnectionJSON{
		Transport: "mcp_streamable_http",
		URL:       mcpURL,
		Headers: map[string]string{
			"MCP-Protocol-Version": "2025-11-25",
			"Authorization":       "Bearer " + token,
		},
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
	target := filepath.Join(stateDir, "connection.json")
	tmp, err := os.CreateTemp(stateDir, "connection.json.tmp-*")
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(tmp.Name()) }()
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), target)
}
