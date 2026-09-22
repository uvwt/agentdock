package skill

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const legacyMigrationBackupRetention = 7 * 24 * time.Hour

type LegacyMigrationResult struct {
	MigratedSkills []string
	MigratedData   []string
	BackupPath     string
}

type legacySelection struct {
	ActiveVersion string `json:"active_version"`
}

// MigrateLegacyLayout performs the one-time transition from the historical
// skill-store/skill-data layout to current managed Skill content. The legacy
// roots are never read by the runtime after this function returns successfully.
func MigrateLegacyLayout(ctx context.Context, agentDockHome string, manager *Manager) (LegacyMigrationResult, error) {
	if manager == nil || manager.State == nil {
		return LegacyMigrationResult{}, errors.New("managed Skill manager is required")
	}
	home, err := filepath.Abs(strings.TrimSpace(agentDockHome))
	if err != nil || strings.TrimSpace(agentDockHome) == "" {
		return LegacyMigrationResult{}, errors.New("agentdock home is required for Skill migration")
	}
	legacyStore := filepath.Join(home, "skill-store")
	legacyData := filepath.Join(home, "skill-data")
	storeExists, err := regularDirectoryExists(legacyStore)
	if err != nil {
		return LegacyMigrationResult{}, fmt.Errorf("inspect legacy Skill store: %w", err)
	}
	dataExists, err := regularDirectoryExists(legacyData)
	if err != nil {
		return LegacyMigrationResult{}, fmt.Errorf("inspect legacy Skill data: %w", err)
	}
	if !storeExists && !dataExists {
		cleanupLegacyMigrationBackups(filepath.Join(home, "migrations"))
		return LegacyMigrationResult{}, nil
	}

	result := LegacyMigrationResult{}
	if storeExists {
		names, err := legacyActiveSkills(legacyStore)
		if err != nil {
			return LegacyMigrationResult{}, err
		}
		for _, name := range names {
			if err := ctx.Err(); err != nil {
				return LegacyMigrationResult{}, err
			}
			installed, err := manager.State.IsInstalled(name)
			if err != nil {
				return LegacyMigrationResult{}, err
			}
			if installed {
				continue
			}
			selection, err := readLegacySelection(legacyStore, name)
			if err != nil {
				return LegacyMigrationResult{}, err
			}
			if selection.ActiveVersion == "" {
				continue
			}
			if !validLegacySegment(selection.ActiveVersion) {
				return LegacyMigrationResult{}, fmt.Errorf("legacy Skill %s has invalid active version", name)
			}
			source := filepath.Join(legacyStore, "installed", name, selection.ActiveVersion)
			info, err := os.Lstat(source)
			if err != nil {
				return LegacyMigrationResult{}, fmt.Errorf("legacy Skill %s active content is unavailable: %w", name, err)
			}
			if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
				return LegacyMigrationResult{}, fmt.Errorf("legacy Skill %s active content is not a regular directory", name)
			}
			candidate, err := manager.State.TempPath("legacy-" + name)
			if err != nil {
				return LegacyMigrationResult{}, fmt.Errorf("prepare legacy Skill %s migration: %w", name, err)
			}
			if err := copyLegacyActiveContent(source, candidate); err != nil {
				_ = os.RemoveAll(candidate)
				return LegacyMigrationResult{}, fmt.Errorf("prepare legacy Skill %s content: %w", name, err)
			}
			installedResult, installErr := manager.Install(ctx, InstallRequest{Source: candidate})
			_ = os.RemoveAll(candidate)
			err = installErr
			if err != nil {
				return LegacyMigrationResult{}, fmt.Errorf("migrate legacy Skill %s: %w", name, err)
			}
			if installedResult.Skill != name {
				return LegacyMigrationResult{}, fmt.Errorf("legacy Skill %s document identity changed to %s", name, installedResult.Skill)
			}
			result.MigratedSkills = append(result.MigratedSkills, name)
		}
	}

	if dataExists {
		migrated, err := migrateLegacyData(legacyData, filepath.Join(home, "data", "skills"))
		if err != nil {
			return LegacyMigrationResult{}, err
		}
		result.MigratedData = migrated
	}

	backupRoot, err := archiveLegacySkillRoots(home, legacyStore, legacyData)
	if err != nil {
		return LegacyMigrationResult{}, err
	}
	result.BackupPath = backupRoot
	cleanupLegacyMigrationBackups(filepath.Join(home, "migrations"))
	return result, nil
}

func copyLegacyActiveContent(source, destination string) error {
	return filepath.WalkDir(source, func(current string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if current == source {
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("legacy Skill contains symlink: %s", current)
		}
		relative, err := filepath.Rel(source, current)
		if err != nil {
			return err
		}
		if filepath.ToSlash(relative) == ".agentdock-install.json" {
			return nil
		}
		target := filepath.Join(destination, relative)
		if entry.IsDir() {
			return os.MkdirAll(target, 0o700)
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		mode := info.Mode().Perm() & 0o755
		if mode == 0 {
			mode = 0o600
		}
		return copyRegularFile(current, target, mode)
	})
}

func legacyActiveSkills(legacyStore string) ([]string, error) {
	stateRoot := filepath.Join(legacyStore, "state")
	entries, err := os.ReadDir(stateRoot)
	if errors.Is(err, os.ErrNotExist) {
		return []string{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read legacy Skill state: %w", err)
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		name := strings.TrimSuffix(entry.Name(), ".json")
		if !validLegacySegment(name) {
			return nil, fmt.Errorf("legacy Skill state has invalid name %q", name)
		}
		names = append(names, name)
	}
	sort.Strings(names)
	return names, nil
}

func readLegacySelection(legacyStore, name string) (legacySelection, error) {
	path := filepath.Join(legacyStore, "state", name+".json")
	data, err := os.ReadFile(path)
	if err != nil {
		return legacySelection{}, fmt.Errorf("read legacy Skill %s state: %w", name, err)
	}
	var selection legacySelection
	if err := json.Unmarshal(data, &selection); err != nil {
		return legacySelection{}, fmt.Errorf("decode legacy Skill %s state: %w", name, err)
	}
	return selection, nil
}

func migrateLegacyData(sourceRoot, destinationRoot string) ([]string, error) {
	entries, err := os.ReadDir(sourceRoot)
	if errors.Is(err, os.ErrNotExist) {
		return []string{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read legacy Skill data: %w", err)
	}
	if err := os.MkdirAll(destinationRoot, 0o700); err != nil {
		return nil, fmt.Errorf("create managed Skill data root: %w", err)
	}
	migrated := make([]string, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() || entry.Type()&os.ModeSymlink != 0 || !validLegacySegment(entry.Name()) {
			continue
		}
		source := filepath.Join(sourceRoot, entry.Name())
		destination := filepath.Join(destinationRoot, entry.Name())
		if _, err := os.Lstat(destination); err == nil {
			continue
		} else if !errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("inspect managed Skill data %s: %w", entry.Name(), err)
		}
		if err := os.Rename(source, destination); err != nil {
			return nil, fmt.Errorf("migrate Skill data %s: %w", entry.Name(), err)
		}
		migrated = append(migrated, entry.Name())
	}
	sort.Strings(migrated)
	return migrated, nil
}

func archiveLegacySkillRoots(home, legacyStore, legacyData string) (string, error) {
	storeExists, err := regularDirectoryExists(legacyStore)
	if err != nil {
		return "", err
	}
	dataExists, err := regularDirectoryExists(legacyData)
	if err != nil {
		return "", err
	}
	if !storeExists && !dataExists {
		return "", nil
	}
	migrationRoot := filepath.Join(home, "migrations")
	if err := os.MkdirAll(migrationRoot, 0o700); err != nil {
		return "", fmt.Errorf("create Skill migration backup root: %w", err)
	}
	backup := filepath.Join(migrationRoot, "skill-model-legacy-"+time.Now().UTC().Format("20060102T150405.000000000Z"))
	if err := os.Mkdir(backup, 0o700); err != nil {
		return "", fmt.Errorf("create Skill migration backup: %w", err)
	}
	if storeExists {
		if err := os.Rename(legacyStore, filepath.Join(backup, "skill-store")); err != nil {
			_ = os.Remove(backup)
			return "", fmt.Errorf("archive legacy Skill store: %w", err)
		}
	}
	if dataExists {
		if err := os.Rename(legacyData, filepath.Join(backup, "skill-data")); err != nil {
			return backup, fmt.Errorf("archive legacy Skill data: %w", err)
		}
	}
	return backup, nil
}

func cleanupLegacyMigrationBackups(root string) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return
	}
	cutoff := time.Now().Add(-legacyMigrationBackupRetention)
	for _, entry := range entries {
		if !entry.IsDir() || !strings.HasPrefix(entry.Name(), "skill-model-legacy-") {
			continue
		}
		path := filepath.Join(root, entry.Name())
		info, err := entry.Info()
		if err == nil && info.ModTime().Before(cutoff) {
			_ = os.RemoveAll(path)
		}
	}
}

func regularDirectoryExists(path string) (bool, error) {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return false, fmt.Errorf("%s is not a regular directory", path)
	}
	return true, nil
}

func validLegacySegment(value string) bool {
	value = strings.TrimSpace(value)
	return value != "" && value != "." && value != ".." && filepath.Base(value) == value &&
		!filepath.IsAbs(value) && !strings.ContainsAny(value, `/\\`)
}
