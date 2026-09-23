package plugin

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/uvwt/agentdock/internal/fs/atomicfile"
)

const removalRecordSchemaVersion = 1

// PurgeOwnership 是 remove keep 后仍需保留的最小清理元数据。
// Plugin data 与 Plugin Skill data/env 都可由 Name 确定；只有 MCP storage key
// 可能因为长名称哈希而无法从 Plugin 名反推，因此需要持久化。
type PurgeOwnership struct {
	Name           string   `json:"name"`
	MCPStorageKeys []string `json:"mcp_storage_keys,omitempty"`
}

type removalRecord struct {
	SchemaVersion int    `json:"schema_version"`
	Phase         string `json:"phase,omitempty"`
	PurgeOwnership
	RemovedState *State `json:"removed_state,omitempty"`
}

const removalPhasePurging = "purging"

func purgeOwnershipFromState(state State) PurgeOwnership {
	return PurgeOwnership{Name: state.Name, MCPStorageKeys: append([]string(nil), state.MCPStorageKeys...)}
}

func mergePurgeOwnership(name string, groups ...PurgeOwnership) PurgeOwnership {
	keys := make([]string, 0)
	for _, group := range groups {
		keys = append(keys, group.MCPStorageKeys...)
	}
	sort.Strings(keys)
	return PurgeOwnership{Name: name, MCPStorageKeys: uniqueStrings(keys)}
}

func (s *Store) removalRecordRoot() (string, error) {
	root, err := os.OpenRoot(s.stateRoot)
	if err != nil {
		return "", err
	}
	defer root.Close()
	info, statErr := root.Lstat("removals")
	if errors.Is(statErr, os.ErrNotExist) {
		if err := root.Mkdir("removals", 0o700); err != nil && !errors.Is(err, os.ErrExist) {
			return "", fmt.Errorf("create Plugin removal record directory: %w", err)
		}
		info, statErr = root.Lstat("removals")
	}
	if statErr != nil {
		return "", statErr
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return "", errors.New("Plugin removal record root is not a regular directory")
	}
	return filepath.Join(s.stateRoot, "removals"), nil
}

func (s *Store) removalRecordPath(name string) (string, error) {
	name, err := pluginNamePathSegment(name)
	if err != nil {
		return "", err
	}
	root, err := s.removalRecordRoot()
	if err != nil {
		return "", err
	}
	return filepath.Join(root, name+".json"), nil
}

func (s *Store) SaveRemovalOwnership(ownership PurgeOwnership) error {
	return s.SaveRemovalRecord(removalRecord{SchemaVersion: removalRecordSchemaVersion, PurgeOwnership: ownership})
}

func (s *Store) SaveRemovalRecord(record removalRecord) error {
	if err := validateRemovalRecord(record); err != nil {
		return err
	}
	path, err := s.removalRecordPath(record.Name)
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	if len(data) > maxStateBytes {
		return fmt.Errorf("Plugin removal record exceeds %d bytes", maxStateBytes)
	}
	return atomicfile.Write(path, data, 0o600)
}

func (s *Store) LoadRemovalOwnership(name string) (PurgeOwnership, error) {
	record, err := s.LoadRemovalRecord(name)
	if err != nil {
		return PurgeOwnership{}, err
	}
	return record.PurgeOwnership, nil
}

func (s *Store) LoadRemovalRecord(name string) (removalRecord, error) {
	name, err := pluginNamePathSegment(name)
	if err != nil {
		return removalRecord{}, err
	}
	rootPath, err := s.removalRecordRoot()
	if err != nil {
		return removalRecord{}, err
	}
	root, err := os.OpenRoot(rootPath)
	if err != nil {
		return removalRecord{}, err
	}
	defer root.Close()
	file, err := root.Open(name + ".json")
	if err != nil {
		return removalRecord{}, err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, int64(maxStateBytes)+1))
	if err != nil {
		return removalRecord{}, err
	}
	if len(data) > maxStateBytes {
		return removalRecord{}, fmt.Errorf("Plugin removal record exceeds %d bytes", maxStateBytes)
	}
	var record removalRecord
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&record); err != nil {
		return removalRecord{}, fmt.Errorf("decode Plugin removal record: %w", err)
	}
	if err := validateRemovalRecord(record); err != nil {
		return removalRecord{}, err
	}
	return record, nil
}

func (s *Store) DeleteRemovalOwnership(name string) error {
	name, err := pluginNamePathSegment(name)
	if err != nil {
		return err
	}
	rootPath, err := s.removalRecordRoot()
	if err != nil {
		return err
	}
	root, err := os.OpenRoot(rootPath)
	if err != nil {
		return err
	}
	defer root.Close()
	if err := root.Remove(name + ".json"); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

func validateRemovalRecord(record removalRecord) error {
	if record.SchemaVersion != removalRecordSchemaVersion {
		return fmt.Errorf("unsupported Plugin removal record schema %d", record.SchemaVersion)
	}
	if err := ValidateName(record.Name); err != nil {
		return err
	}
	for _, key := range record.MCPStorageKeys {
		if strings.TrimSpace(key) == "" || strings.ContainsAny(key, `/\\`) {
			return errors.New("Plugin removal record contains invalid MCP storage key")
		}
	}
	switch record.Phase {
	case "":
		if record.RemovedState != nil {
			return errors.New("Plugin ownership-only removal record cannot contain removed state")
		}
	case removalPhasePurging:
		if record.RemovedState != nil {
			if err := validateState(*record.RemovedState); err != nil {
				return fmt.Errorf("invalid removed Plugin state: %w", err)
			}
			if record.RemovedState.Name != record.Name {
				return errors.New("Plugin removal record state identity mismatch")
			}
		}
	default:
		return fmt.Errorf("invalid Plugin removal phase %q", record.Phase)
	}
	return nil
}
