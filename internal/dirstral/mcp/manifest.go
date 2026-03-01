package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"time"
)

type ToolManifest struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	HasSchema   bool   `json:"has_schema"`
}

type EndpointManifest struct {
	Endpoint        string `json:"endpoint"`
	Transport       string `json:"transport"`
	ProtocolVersion string `json:"protocol_version"`
	SessionID       string `json:"session_id,omitempty"`
}

type CapabilityManifest struct {
	GeneratedAt time.Time        `json:"generated_at"`
	Endpoint    EndpointManifest `json:"endpoint"`
	Tools       []ToolManifest   `json:"tools"`
}

func BuildCapabilityManifest(ctx context.Context, client *Client) (CapabilityManifest, error) {
	if client == nil {
		return CapabilityManifest{}, fmt.Errorf("mcp client is nil")
	}
	tools, err := client.ListTools(ctx)
	if err != nil {
		return CapabilityManifest{}, fmt.Errorf("tools/list failed: %w", err)
	}
	manifestTools := make([]ToolManifest, 0, len(tools))
	for _, tool := range tools {
		manifestTools = append(manifestTools, ToolManifest{
			Name:        tool.Name,
			Description: strings.TrimSpace(tool.Description),
			HasSchema:   tool.InputSchema != nil,
		})
	}
	sort.Slice(manifestTools, func(i, j int) bool {
		return manifestTools[i].Name < manifestTools[j].Name
	})

	return CapabilityManifest{
		GeneratedAt: time.Now().UTC(),
		Endpoint: EndpointManifest{
			Endpoint:        sanitizeEndpoint(client.endpoint),
			Transport:       strings.TrimSpace(client.transport),
			ProtocolVersion: protocolVersion,
			SessionID:       strings.TrimSpace(client.sessionID),
		},
		Tools: manifestTools,
	}, nil
}

func RenderCapabilityManifestHuman(manifest CapabilityManifest) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Generated: %s\n", manifest.GeneratedAt.Format(time.RFC3339))
	fmt.Fprintf(&b, "Endpoint: %s\n", orUnknown(manifest.Endpoint.Endpoint))
	fmt.Fprintf(&b, "Transport: %s\n", orUnknown(manifest.Endpoint.Transport))
	fmt.Fprintf(&b, "Protocol: %s\n", orUnknown(manifest.Endpoint.ProtocolVersion))
	fmt.Fprintf(&b, "Session: %s\n", orUnknown(manifest.Endpoint.SessionID))
	fmt.Fprintf(&b, "Tools (%d):\n", len(manifest.Tools))
	for _, tool := range manifest.Tools {
		desc := strings.TrimSpace(tool.Description)
		if desc == "" {
			desc = "(no description)"
		}
		schema := "no"
		if tool.HasSchema {
			schema = "yes"
		}
		fmt.Fprintf(&b, "- %s\n  schema: %s\n  description: %s\n", tool.Name, schema, desc)
	}
	return strings.TrimSpace(b.String())
}

func RenderCapabilityManifestJSON(manifest CapabilityManifest) ([]byte, error) {
	return json.MarshalIndent(manifest, "", "  ")
}

func orUnknown(v string) string {
	v = strings.TrimSpace(v)
	if v == "" {
		return "unknown"
	}
	return v
}

func sanitizeEndpoint(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ""
	}
	if !strings.Contains(trimmed, "://") {
		parts := strings.Fields(trimmed)
		if len(parts) <= 1 {
			return trimmed
		}
		return parts[0] + " <redacted>"
	}
	u, err := url.Parse(trimmed)
	if err != nil {
		return trimmed
	}
	u.User = nil
	u.RawQuery = ""
	u.Fragment = ""
	return u.String()
}
