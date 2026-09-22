package command

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/uvwt/agentdock/internal/config"
	"github.com/uvwt/agentdock/internal/fs/securepath"
)

// ensureManagedSkillDataDir creates the persistent data path only for a managed
// Skill invocation. os.Root keeps every operation beneath AgentDockHome, while
// Lstat rejects symlinked components so the stable data identity cannot be
// redirected to another location.
func (svc *Service) ensureManagedSkillDataDir(skillName string) (string, error) {
	if skillName == "" {
		return "", nil
	}
	cfg := svc.config()
	dataDir, err := config.SkillDataDir(cfg, skillName)
	if err != nil {
		return "", toolErrorDetails("SKILL_DATA_DIR_INVALID", "resolve managed Skill data directory", "runtime", map[string]any{
			"skill": skillName, "reason": err.Error(),
		})
	}

	root, err := os.OpenRoot(cfg.AgentDockHome)
	if err != nil {
		return "", toolErrorDetails("SKILL_DATA_DIR_INVALID", "open AgentDock home for managed Skill data", "runtime", map[string]any{
			"skill": skillName, "reason": err.Error(),
		})
	}
	defer root.Close()

	for _, relative := range []string{
		"data",
		filepath.Join("data", "skills"),
		filepath.Join("data", "skills", skillName),
	} {
		info, statErr := root.Lstat(relative)
		if errors.Is(statErr, os.ErrNotExist) {
			if mkdirErr := root.Mkdir(relative, 0o700); mkdirErr != nil && !errors.Is(mkdirErr, os.ErrExist) {
				return "", toolErrorDetails("SKILL_DATA_DIR_INVALID", "create managed Skill data directory", "runtime", map[string]any{
					"skill": skillName, "path": relative, "reason": mkdirErr.Error(),
				})
			}
			info, statErr = root.Lstat(relative)
		}
		if statErr != nil {
			return "", toolErrorDetails("SKILL_DATA_DIR_INVALID", "inspect managed Skill data directory", "runtime", map[string]any{
				"skill": skillName, "path": relative, "reason": statErr.Error(),
			})
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return "", toolErrorDetails("SKILL_DATA_DIR_INVALID", "managed Skill data path contains a symlink or non-directory component", "validation", map[string]any{
				"skill": skillName, "path": relative,
			})
		}
		absolute := filepath.Join(cfg.AgentDockHome, relative)
		if err := securepath.EnsurePrivate(absolute); err != nil {
			return "", toolErrorDetails("SKILL_DATA_DIR_INVALID", "secure managed Skill data directory", "runtime", map[string]any{
				"skill": skillName, "path": absolute, "reason": err.Error(),
			})
		}
	}
	return dataDir, nil
}

func reservedSkillEnvironmentError(key string) error {
	return fmt.Errorf("environment variable %q is reserved by the Skill runtime", key)
}
