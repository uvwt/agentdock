package config

import "path/filepath"

// NexusStateDir returns AgentDock's internal Nexus state directory.
func NexusStateDir(cfg Config) (string, error) {
	return filepath.Join(cfg.AgentDockHome, "nexus"), nil
}
