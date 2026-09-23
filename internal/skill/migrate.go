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

	"github.com/uvwt/agentdock/internal/fs/atomicfile"
)

const (
	legacyMigrationBackupRetention = 7 * 24 * time.Hour
	legacyMigrationPendingSchema   = 1
	legacyMigrationPendingFile     = "skill-model-pending.json"
)

type LegacyMigrationResult struct {
	MigratedSkills []string
	MigratedData   []string
	PendingPath    string
}

type legacySelection struct {
	ActiveVersion string `json:"active_version"`
}

type legacyMigrationPending struct {
	SchemaVersion  int               `json:"schema_version"`
	PreparedAt     time.Time         `json:"prepared_at"`
	MigratedSkills []string          `json:"migrated_skills,omitempty"`
	MigratedData   []string          `json:"migrated_data,omitempty"`
	SkillDigests   map[string]string `json:"skill_digests,omitempty"`
	DataDigests    map[string]string `json:"data_digests,omitempty"`
}

// MigrateLegacyLayout prepares the one-time transition from the historical
// skill-store/skill-data layout to current managed Skill content.
//
// Preparation is deliberately non-destructive: the old roots stay in place until
// the outer install/update transaction has committed. That keeps pre-migration
// binaries rollback-safe even when a legacy updater does not understand the new
// migration protocol.
func MigrateLegacyLayout(ctx context.Context, agentDockHome string, manager *Manager) (LegacyMigrationResult, error) {
	return migrateLegacyLayout(ctx, agentDockHome, manager, false)
}

// MigrateLegacyLayoutForUpdate may refresh paths already created by a pending
// migration from the still-authoritative legacy roots. Only update/bootstrap
// paths may use it: normal runtime startup must never overwrite data produced by
// the new runtime after a successful legacy upgrade.
func MigrateLegacyLayoutForUpdate(ctx context.Context, agentDockHome string, manager *Manager) (LegacyMigrationResult, error) {
	return migrateLegacyLayout(ctx, agentDockHome, manager, true)
}

func migrateLegacyLayout(ctx context.Context, agentDockHome string, manager *Manager, refreshPending bool) (LegacyMigrationResult, error) {
	if manager == nil || manager.State == nil {
		return LegacyMigrationResult{}, errors.New("managed Skill manager is required")
	}
	home, err := normalizeLegacyMigrationHome(agentDockHome)
	if err != nil {
		return LegacyMigrationResult{}, err
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
	pendingPath := filepath.Join(home, "migrations", legacyMigrationPendingFile)
	if !storeExists && !dataExists {
		if err := os.Remove(pendingPath); err != nil && !errors.Is(err, os.ErrNotExist) {
			return LegacyMigrationResult{}, fmt.Errorf("clear completed Skill migration marker: %w", err)
		}
		cleanupLegacyMigrationBackups(filepath.Join(home, "migrations"))
		return LegacyMigrationResult{}, nil
	}

	pending, err := loadLegacyMigrationPending(pendingPath)
	if err != nil {
		return LegacyMigrationResult{}, err
	}
	if pending.SchemaVersion == 0 {
		pending = legacyMigrationPending{
			SchemaVersion: legacyMigrationPendingSchema,
			PreparedAt:    time.Now().UTC(),
		}
		if err := writeLegacyMigrationPending(pendingPath, pending); err != nil {
			return LegacyMigrationResult{}, err
		}
	}
	ensureLegacyMigrationDigestMaps(&pending)
	result := LegacyMigrationResult{
		MigratedSkills: append([]string(nil), pending.MigratedSkills...),
		MigratedData:   append([]string(nil), pending.MigratedData...),
		PendingPath:    pendingPath,
	}

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
			migrationOwned := containsString(pending.MigratedSkills, name)
			if installed && (!migrationOwned || !refreshPending) {
				// Current-layout content that existed before migration always wins.
				// Normal runtime startup also leaves pending migration copies alone;
				// only an explicit update/bootstrap retry may reconcile them.
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
			legacyDigest, err := digestPackageContent(candidate)
			if err != nil {
				_ = os.RemoveAll(candidate)
				return LegacyMigrationResult{}, fmt.Errorf("digest legacy Skill %s content: %w", name, err)
			}

			if migrationOwned && installed {
				destination, pathErr := manager.State.SkillPath(name)
				if pathErr != nil {
					_ = os.RemoveAll(candidate)
					return LegacyMigrationResult{}, pathErr
				}
				currentDigest, currentExists, digestErr := installedContentDigest(destination)
				if digestErr != nil {
					_ = os.RemoveAll(candidate)
					return LegacyMigrationResult{}, fmt.Errorf("digest current Skill %s content: %w", name, digestErr)
				}
				refresh, conflict := migrationRefreshDecision(
					pending.SkillDigests[name],
					currentDigest,
					legacyDigest,
					currentExists,
				)
				if conflict {
					_ = os.RemoveAll(candidate)
					return LegacyMigrationResult{}, fmt.Errorf(
						"legacy Skill %s changed on both old and current layouts while migration was pending; resolve the conflict before retrying",
						name,
					)
				}
				if !refresh {
					// If both sides independently converged to the same content,
					// advance the snapshot so later retries compare from that point.
					if currentDigest == legacyDigest && currentDigest != pending.SkillDigests[name] {
						pending.SkillDigests[name] = currentDigest
						if err := writeLegacyMigrationPending(pendingPath, pending); err != nil {
							_ = os.RemoveAll(candidate)
							return LegacyMigrationResult{}, err
						}
					}
					_ = os.RemoveAll(candidate)
					continue
				}
			}

			if !migrationOwned {
				// Record ownership before mutating the current layout. If the
				// process crashes after commit but before a later marker update,
				// retry still knows that this path came from migration.
				pending.MigratedSkills = appendUniqueSorted(pending.MigratedSkills, name)
				pending.SkillDigests[name] = legacyDigest
				if err := writeLegacyMigrationPending(pendingPath, pending); err != nil {
					_ = os.RemoveAll(candidate)
					return LegacyMigrationResult{}, err
				}
			}

			installedResult, installErr := manager.Install(ctx, InstallRequest{Source: candidate})
			_ = os.RemoveAll(candidate)
			if installErr != nil {
				return LegacyMigrationResult{}, fmt.Errorf("migrate legacy Skill %s: %w", name, installErr)
			}
			if installedResult.Skill != name {
				return LegacyMigrationResult{}, fmt.Errorf("legacy Skill %s document identity changed to %s", name, installedResult.Skill)
			}
			pending.SkillDigests[name] = installedResult.ContentDigest
			if err := writeLegacyMigrationPending(pendingPath, pending); err != nil {
				return LegacyMigrationResult{}, err
			}
			result.MigratedSkills = appendUniqueSorted(result.MigratedSkills, name)
		}

	}

	if dataExists {
		migrated, err := migrateLegacyData(
			legacyData,
			filepath.Join(home, "data", "skills"),
			pendingPath,
			&pending,
			refreshPending,
		)
		if err != nil {
			return LegacyMigrationResult{}, err
		}
		result.MigratedData = appendUniqueSorted(result.MigratedData, migrated...)
	}

	cleanupLegacyMigrationBackups(filepath.Join(home, "migrations"))
	return result, nil
}

// FinalizeLegacyMigration archives the preserved legacy roots after the outer
// install/update transaction has committed. It is safe to call repeatedly.
func FinalizeLegacyMigration(agentDockHome string) (string, error) {
	home, err := normalizeLegacyMigrationHome(agentDockHome)
	if err != nil {
		return "", err
	}
	pendingPath := filepath.Join(home, "migrations", legacyMigrationPendingFile)
	pending, err := loadLegacyMigrationPending(pendingPath)
	if err != nil {
		return "", err
	}
	if pending.SchemaVersion == 0 {
		cleanupLegacyMigrationBackups(filepath.Join(home, "migrations"))
		return "", nil
	}
	if pending.SchemaVersion != legacyMigrationPendingSchema {
		return "", fmt.Errorf("unsupported Skill migration marker schema: %d", pending.SchemaVersion)
	}

	backupRoot, err := archiveLegacySkillRoots(home, filepath.Join(home, "skill-store"), filepath.Join(home, "skill-data"))
	if err != nil {
		return "", err
	}
	if err := os.Remove(pendingPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return backupRoot, fmt.Errorf("remove Skill migration marker: %w", err)
	}
	cleanupLegacyMigrationBackups(filepath.Join(home, "migrations"))
	return backupRoot, nil
}

func normalizeLegacyMigrationHome(agentDockHome string) (string, error) {
	value := strings.TrimSpace(agentDockHome)
	if value == "" {
		return "", errors.New("agentdock home is required for Skill migration")
	}
	home, err := filepath.Abs(value)
	if err != nil {
		return "", fmt.Errorf("resolve AgentDock home for Skill migration: %w", err)
	}
	return home, nil
}

func loadLegacyMigrationPending(path string) (legacyMigrationPending, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return legacyMigrationPending{}, nil
	}
	if err != nil {
		return legacyMigrationPending{}, fmt.Errorf("read Skill migration marker: %w", err)
	}
	var pending legacyMigrationPending
	if err := json.Unmarshal(data, &pending); err != nil {
		return legacyMigrationPending{}, fmt.Errorf("decode Skill migration marker: %w", err)
	}
	if pending.SchemaVersion != legacyMigrationPendingSchema {
		return legacyMigrationPending{}, fmt.Errorf("unsupported Skill migration marker schema: %d", pending.SchemaVersion)
	}
	return pending, nil
}

func writeLegacyMigrationPending(path string, pending legacyMigrationPending) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create Skill migration state directory: %w", err)
	}
	data, err := json.MarshalIndent(pending, "", "  ")
	if err != nil {
		return fmt.Errorf("encode Skill migration marker: %w", err)
	}
	data = append(data, '\n')
	if err := atomicfile.Write(path, data, 0o600); err != nil {
		return fmt.Errorf("persist Skill migration marker: %w", err)
	}
	return nil
}

func ensureLegacyMigrationDigestMaps(pending *legacyMigrationPending) {
	if pending.SkillDigests == nil {
		pending.SkillDigests = make(map[string]string)
	}
	if pending.DataDigests == nil {
		pending.DataDigests = make(map[string]string)
	}
}

func migrationRefreshDecision(expected, current, legacy string, currentExists bool) (refresh, conflict bool) {
	if !currentExists || expected == "" {
		return true, false
	}
	currentChanged := current != expected
	legacyChanged := legacy != expected
	if currentChanged && legacyChanged && current != legacy {
		return false, true
	}
	if !currentChanged && legacyChanged {
		return true, false
	}
	return false, false
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func appendUniqueSorted(values []string, additions ...string) []string {
	seen := make(map[string]struct{}, len(values)+len(additions))
	for _, value := range values {
		seen[value] = struct{}{}
	}
	for _, value := range additions {
		if value != "" {
			seen[value] = struct{}{}
		}
	}
	result := make([]string, 0, len(seen))
	for value := range seen {
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}

func copyLegacyActiveContent(source, destination string) error {
	return copyLegacyTree(source, destination, true)
}

func copyLegacyTree(source, destination string, skipInstallReceipt bool) error {
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
		if skipInstallReceipt && filepath.ToSlash(relative) == ".agentdock-install.json" {
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
		if !info.Mode().IsRegular() {
			return fmt.Errorf("legacy Skill contains non-regular file: %s", current)
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

func migrateLegacyData(
	sourceRoot, destinationRoot, pendingPath string,
	pending *legacyMigrationPending,
	refreshPending bool,
) ([]string, error) {
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
		name := entry.Name()
		destination := filepath.Join(destinationRoot, name)
		destinationExists := false
		if info, err := os.Lstat(destination); err == nil {
			if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
				return nil, fmt.Errorf("managed Skill data %s is not a regular directory", name)
			}
			destinationExists = true
		} else if !errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("inspect managed Skill data %s: %w", name, err)
		}

		migrationOwned := containsString(pending.MigratedData, name)
		if destinationExists && (!migrationOwned || !refreshPending) {
			// Data that predates migration belongs to the current layout and is
			// never overwritten by a legacy source. Normal runtime startup also
			// leaves pending copies untouched; update/bootstrap owns reconciliation.
			continue
		}
		source := filepath.Join(sourceRoot, name)
		sourceDigest, err := digestDirectory(source, true)
		if err != nil {
			return nil, fmt.Errorf("digest legacy Skill data %s: %w", name, err)
		}
		currentDigest := ""
		if destinationExists {
			currentDigest, err = digestDirectory(destination, true)
			if err != nil {
				return nil, fmt.Errorf("digest current Skill data %s: %w", name, err)
			}
		}
		if migrationOwned {
			refresh, conflict := migrationRefreshDecision(
				pending.DataDigests[name],
				currentDigest,
				sourceDigest,
				destinationExists,
			)
			if conflict {
				return nil, fmt.Errorf(
					"Skill data %s changed on both old and current layouts while migration was pending; resolve the conflict before retrying",
					name,
				)
			}
			if !refresh {
				if currentDigest == sourceDigest && currentDigest != pending.DataDigests[name] {
					pending.DataDigests[name] = currentDigest
					if err := writeLegacyMigrationPending(pendingPath, *pending); err != nil {
						return nil, err
					}
				}
				continue
			}
		}

		staged, err := os.MkdirTemp(destinationRoot, ".legacy-"+name+"-")
		if err != nil {
			return nil, fmt.Errorf("stage Skill data %s: %w", name, err)
		}
		if err := os.Chmod(staged, 0o700); err != nil {
			_ = os.RemoveAll(staged)
			return nil, fmt.Errorf("secure staged Skill data %s: %w", name, err)
		}
		if err := copyLegacyTree(source, staged, false); err != nil {
			_ = os.RemoveAll(staged)
			return nil, fmt.Errorf("copy Skill data %s: %w", name, err)
		}

		if !migrationOwned {
			// Persist ownership before the first destination commit. A crash
			// after rename still leaves enough information for a safe retry.
			pending.MigratedData = appendUniqueSorted(pending.MigratedData, name)
			pending.DataDigests[name] = sourceDigest
			if err := writeLegacyMigrationPending(pendingPath, *pending); err != nil {
				_ = os.RemoveAll(staged)
				return nil, err
			}
		}

		if destinationExists {
			backup := destination + ".legacy-refresh"
			if err := os.RemoveAll(backup); err != nil {
				_ = os.RemoveAll(staged)
				return nil, fmt.Errorf("clear Skill data refresh backup %s: %w", name, err)
			}
			if err := os.Rename(destination, backup); err != nil {
				_ = os.RemoveAll(staged)
				return nil, fmt.Errorf("backup Skill data %s for refresh: %w", name, err)
			}
			if err := os.Rename(staged, destination); err != nil {
				restoreErr := os.Rename(backup, destination)
				_ = os.RemoveAll(staged)
				return nil, errors.Join(
					fmt.Errorf("refresh Skill data %s: %w", name, err),
					wrapLegacyRestoreError("restore previous managed Skill data", restoreErr),
				)
			}
			if err := os.RemoveAll(backup); err != nil {
				return nil, fmt.Errorf("cleanup Skill data refresh backup %s: %w", name, err)
			}
		} else if err := os.Rename(staged, destination); err != nil {
			_ = os.RemoveAll(staged)
			return nil, fmt.Errorf("commit Skill data %s: %w", name, err)
		}

		pending.DataDigests[name] = sourceDigest
		if err := writeLegacyMigrationPending(pendingPath, *pending); err != nil {
			return nil, err
		}
		migrated = append(migrated, name)
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

	storeBackup := filepath.Join(backup, "skill-store")
	dataBackup := filepath.Join(backup, "skill-data")
	storeMoved := false
	if storeExists {
		if err := os.Rename(legacyStore, storeBackup); err != nil {
			_ = os.Remove(backup)
			return "", fmt.Errorf("archive legacy Skill store: %w", err)
		}
		storeMoved = true
	}
	if dataExists {
		if err := os.Rename(legacyData, dataBackup); err != nil {
			restoreErr := error(nil)
			if storeMoved {
				restoreErr = os.Rename(storeBackup, legacyStore)
			}
			if restoreErr == nil {
				_ = os.RemoveAll(backup)
			}
			return "", errors.Join(
				fmt.Errorf("archive legacy Skill data: %w", err),
				wrapLegacyRestoreError("restore legacy Skill store after archive failure", restoreErr),
			)
		}
	}
	return backup, nil
}

func wrapLegacyRestoreError(message string, err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%s: %w", message, err)
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
		!filepath.IsAbs(value) && !strings.ContainsAny(value, `/\`)
}
