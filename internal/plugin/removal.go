package plugin

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
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
	SchemaVersion int `json:"schema_version"`
	PurgeOwnership
}

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
	record := removalRecord{SchemaVersion: removalRecordSchemaVersion, PurgeOwnership: ownership}
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
	path, err := s.removalRecordPath(name)
	if err != nil {
		return PurgeOwnership{}, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return PurgeOwnership{}, err
	}
	var record removalRecord
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&record); err != nil {
		return PurgeOwnership{}, fmt.Errorf("decode Plugin removal record: %w", err)
	}
	if err := validateRemovalRecord(record); err != nil {
		return PurgeOwnership{}, err
	}
	return record.PurgeOwnership, nil
}

func (s *Store) DeleteRemovalOwnership(name string) error {
	path, err := s.removalRecordPath(name)
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
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
	return nil
}
