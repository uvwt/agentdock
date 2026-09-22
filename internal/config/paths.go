package config

import (
	"fmt"
	"path/filepath"
	"strings"
)

const SkillDataDirEnvKey = "SKILL_DATA_DIR"

// SkillDir returns the managed Skill root. Each child directory is the current
// installed content for one standalone managed Skill.
func SkillDir(cfg Config) string {
	return filepath.Join(cfg.AgentDockHome, "skills")
}

// SkillDataDir returns the private persistent-data directory assigned to one
// managed Skill. It validates the Skill identity itself so callers cannot turn
// a data-directory lookup into an arbitrary path join.
func SkillDataDir(cfg Config, skill string) (string, error) {
	home := filepath.Clean(strings.TrimSpace(cfg.AgentDockHome))
	if home == "." || !filepath.IsAbs(home) {
		return "", fmt.Errorf("AgentDockHome must be an absolute path")
	}
	if !validSkillDataName(skill) {
		return "", fmt.Errorf("invalid Skill name %q", skill)
	}
	root := filepath.Join(home, "data", "skills")
	target := filepath.Join(root, skill)
	relative, err := filepath.Rel(root, target)
	if err != nil || relative != skill || filepath.IsAbs(relative) {
		return "", fmt.Errorf("Skill data path escapes managed data root")
	}
	return target, nil
}

// IsReservedSkillEnvironmentKey reports whether a variable is owned by the
// AgentDock Skill runtime and cannot be supplied through user environment config.
func IsReservedSkillEnvironmentKey(key string) bool {
	return strings.EqualFold(strings.TrimSpace(key), SkillDataDirEnvKey)
}

func validSkillDataName(value string) bool {
	if len(value) < 2 || len(value) > 63 || value[0] < 'a' || value[0] > 'z' {
		return false
	}
	for index := 1; index < len(value); index++ {
		char := value[index]
		if (char >= 'a' && char <= 'z') || (char >= '0' && char <= '9') || char == '-' {
			continue
		}
		return false
	}
	return true
}
