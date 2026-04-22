package session

import (
	"fmt"
	"strings"

	"github.com/gastownhall/gascity/internal/beads"
	"github.com/gastownhall/gascity/internal/runtime"
)

func (m *Manager) syncStoredMCPServers(id string, b *beads.Bead, servers []runtime.MCPServerConfig) error {
	snapshot, err := EncodeMCPServersSnapshot(servers)
	if err != nil {
		return err
	}
	current := ""
	if b != nil && b.Metadata != nil {
		current = strings.TrimSpace(b.Metadata[MCPServersSnapshotMetadataKey])
	}
	if current == snapshot {
		return nil
	}
	if err := m.store.SetMetadata(id, MCPServersSnapshotMetadataKey, snapshot); err != nil {
		return fmt.Errorf("storing MCP server snapshot: %w", err)
	}
	if b != nil {
		if b.Metadata == nil {
			b.Metadata = make(map[string]string)
		}
		b.Metadata[MCPServersSnapshotMetadataKey] = snapshot
	}
	return nil
}
