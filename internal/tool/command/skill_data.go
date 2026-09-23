package command

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/uvwt/agentdock/internal/config"
	"github.com/uvwt/agentdock/internal/fs/securepath"
)

// ensureManagedSkillDataDir preserves the standalone helper used by existing
// callers while delegating creation to the path-based Skill data primitive.
func (svc *Service) ensureManagedSkillDataDir(skillName string) (string, error) {
	if skillName == "" {
		return "", nil
	}
	dataDir, err := config.SkillDataDir(svc.config(), skillName)
	if err != nil {
		return "", toolErrorDetails("SKILL_DATA_DIR_INVALID", "resolve managed Skill data directory", "runtime", map[string]any{
			"skill": skillName, "reason": err.Error(),
		})
	}
	return svc.ensureSkillDataDirPath(dataDir, skillName)
}

// ensureSkillDataDirPath creates either a standalone or Plugin-owned Skill data
// directory. The caller supplies an already validated stable path; this method
// verifies that it stays beneath AgentDockHome/data/skills and rejects symlink
// or non-directory components before making it private.
func (svc *Service) ensureSkillDataDirPath(dataDir, identity string) (string, error) {
	cfg := svc.config()
	home := filepath.Clean(cfg.AgentDockHome)
	rootPath := filepath.Join(home, "data", "skills")
	target := filepath.Clean(dataDir)
	relative, err := filepath.Rel(rootPath, target)
	if err != nil || relative == "." || !filepath.IsLocal(relative) {
		return "", toolErrorDetails("SKILL_DATA_DIR_INVALID", "Skill data directory escapes managed data root", "validation", map[string]any{
			"skill": identity, "path": target,
		})
	}
	root, err := os.OpenRoot(home)
	if err != nil {
		return "", toolErrorDetails("SKILL_DATA_DIR_INVALID", "open AgentDock home for Skill data", "runtime", map[string]any{
			"skill": identity, "reason": err.Error(),
		})
	}
	defer root.Close()

	paths := []string{"data", filepath.Join("data", "skills")}
	current := filepath.Join("data", "skills")
	for _, part := range strings.Split(relative, string(filepath.Separator)) {
		if part == "" || part == "." || part == ".." {
			return "", toolErrorDetails("SKILL_DATA_DIR_INVALID", "Skill data path contains invalid segment", "validation", map[string]any{
				"skill": identity, "path": target,
			})
		}
		current = filepath.Join(current, part)
		paths = append(paths, current)
	}
	for _, item := range paths {
		info, statErr := root.Lstat(item)
		if errors.Is(statErr, os.ErrNotExist) {
			if mkdirErr := root.Mkdir(item, 0o700); mkdirErr != nil && !errors.Is(mkdirErr, os.ErrExist) {
				return "", toolErrorDetails("SKILL_DATA_DIR_INVALID", "create Skill data directory", "runtime", map[string]any{
					"skill": identity, "path": item, "reason": mkdirErr.Error(),
				})
			}
			info, statErr = root.Lstat(item)
		}
		if statErr != nil {
			return "", toolErrorDetails("SKILL_DATA_DIR_INVALID", "inspect Skill data directory", "runtime", map[string]any{
				"skill": identity, "path": item, "reason": statErr.Error(),
			})
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return "", toolErrorDetails("SKILL_DATA_DIR_INVALID", "Skill data path contains a symlink or non-directory component", "validation", map[string]any{
				"skill": identity, "path": item,
			})
		}
		absolute := filepath.Join(home, item)
		if err := securepath.EnsurePrivate(absolute); err != nil {
			return "", toolErrorDetails("SKILL_DATA_DIR_INVALID", "secure Skill data directory", "runtime", map[string]any{
				"skill": identity, "path": absolute, "reason": err.Error(),
			})
		}
	}
	return target, nil
}

func reservedSkillEnvironmentError(key string) error {
	return fmt.Errorf("environment variable %q is reserved by the Skill runtime", key)
}
