package config

import "path/filepath"

// SkillStateDir returns AgentDock's local document Skill store.
func SkillStateDir(cfg Config) (string, error) {
	return filepath.Join(cfg.AgentDockHome, "skill-store"), nil
}
