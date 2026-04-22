package session

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/gastownhall/gascity/internal/runtime"
)

const (
	// MCPIdentityMetadataKey stores the stable identity used to materialize
	// MCP templates for a session.
	MCPIdentityMetadataKey = "mcp_identity"
	// MCPServersSnapshotMetadataKey stores the normalized ACP session/new MCP
	// server snapshot used to resume sessions when the current catalog cannot
	// be materialized.
	MCPServersSnapshotMetadataKey = "mcp_servers_snapshot"
)

// EncodeMCPServersSnapshot returns the normalized metadata value for a
// session's persisted ACP session/new MCP server snapshot.
func EncodeMCPServersSnapshot(servers []runtime.MCPServerConfig) (string, error) {
	normalized := runtime.NormalizeMCPServerConfigs(servers)
	if len(normalized) == 0 {
		return "", nil
	}
	data, err := json.Marshal(normalized)
	if err != nil {
		return "", fmt.Errorf("marshal MCP server snapshot: %w", err)
	}
	return string(data), nil
}

// DecodeMCPServersSnapshot parses a persisted ACP session/new MCP server
// snapshot from session metadata.
func DecodeMCPServersSnapshot(raw string) ([]runtime.MCPServerConfig, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	var servers []runtime.MCPServerConfig
	if err := json.Unmarshal([]byte(raw), &servers); err != nil {
		return nil, fmt.Errorf("unmarshal MCP server snapshot: %w", err)
	}
	return runtime.NormalizeMCPServerConfigs(servers), nil
}

// WithStoredMCPMetadata returns a metadata map augmented with the stable MCP
// identity and normalized ACP session/new snapshot for the session.
func WithStoredMCPMetadata(meta map[string]string, identity string, servers []runtime.MCPServerConfig) (map[string]string, error) {
	if meta == nil {
		meta = make(map[string]string)
	}
	identity = strings.TrimSpace(identity)
	if identity != "" {
		meta[MCPIdentityMetadataKey] = identity
	}
	snapshot, err := EncodeMCPServersSnapshot(servers)
	if err != nil {
		return nil, err
	}
	if snapshot != "" {
		meta[MCPServersSnapshotMetadataKey] = snapshot
	} else if _, ok := meta[MCPServersSnapshotMetadataKey]; ok {
		meta[MCPServersSnapshotMetadataKey] = ""
	}
	return meta, nil
}
