package config

import (
	"path/filepath"
	"strings"
)

// ResolveNexusStateDir returns the shared local state directory used by both
// the Nexus background agent and the MCP-facing Skill Runtime tool.
func ResolveNexusStateDir(cfg Config) (string, error) {
	value := strings.TrimSpace(cfg.NexusStateDir)
	if value == "" {
		value = filepath.Join(cfg.AgentDockDir, "nexus")
	}
	if !filepath.IsAbs(value) {
		value = filepath.Join(cfg.Workspace, value)
	}
	return filepath.Abs(value)
}
