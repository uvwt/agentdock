package config

import "path/filepath"

// SkillRuntimeStateDir returns AgentDock's local Skill Runtime state directory.
func SkillRuntimeStateDir(cfg Config) (string, error) {
	return filepath.Join(cfg.AgentDockHome, "skill-runtime"), nil
}
