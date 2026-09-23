package config

import (
	"fmt"
	"path/filepath"
	"strings"
)

const (
	SkillDataDirEnvKey  = "SKILL_DATA_DIR"
	PluginDataDirEnvKey = "PLUGIN_DATA_DIR"
)

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

// PluginDataDir returns the private persistent-data directory assigned to one
// installed Plugin. The path is version-independent so updates preserve data.
func PluginDataDir(cfg Config, pluginName string) (string, error) {
	home := filepath.Clean(strings.TrimSpace(cfg.AgentDockHome))
	if home == "." || !filepath.IsAbs(home) {
		return "", fmt.Errorf("AgentDockHome must be an absolute path")
	}
	if !validPluginDataName(pluginName) {
		return "", fmt.Errorf("invalid Plugin name %q", pluginName)
	}
	root := filepath.Join(home, "data", "plugins")
	target := filepath.Join(root, pluginName)
	relative, err := filepath.Rel(root, target)
	if err != nil || relative != pluginName || filepath.IsAbs(relative) {
		return "", fmt.Errorf("Plugin data path escapes managed data root")
	}
	return target, nil
}

// IsReservedSkillEnvironmentKey reports whether a variable is owned by the
// AgentDock Skill runtime and cannot be supplied through user environment config.
func IsReservedSkillEnvironmentKey(key string) bool {
	return strings.EqualFold(strings.TrimSpace(key), SkillDataDirEnvKey)
}

// IsReservedPluginEnvironmentKey reports whether a variable is owned by the
// Plugin runtime and cannot be supplied through user environment config.
func IsReservedPluginEnvironmentKey(key string) bool {
	return strings.EqualFold(strings.TrimSpace(key), PluginDataDirEnvKey)
}

// IsReservedCommandEnvironmentKey covers every runtime-owned command variable.
func IsReservedCommandEnvironmentKey(key string) bool {
	return IsReservedSkillEnvironmentKey(key) || IsReservedPluginEnvironmentKey(key)
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

func validPluginDataName(value string) bool {
	value = strings.TrimSpace(value)
	if len(value) < 1 || len(value) > 64 || strings.Contains(value, "--") || strings.Contains(value, "..") {
		return false
	}
	isAlphaNumeric := func(char byte) bool {
		return (char >= 'a' && char <= 'z') || (char >= '0' && char <= '9')
	}
	if !isAlphaNumeric(value[0]) || !isAlphaNumeric(value[len(value)-1]) {
		return false
	}
	for index := 0; index < len(value); index++ {
		char := value[index]
		if isAlphaNumeric(char) || char == '-' || char == '.' {
			continue
		}
		return false
	}
	return true
}
