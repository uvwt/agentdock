package plugin

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/uvwt/agentdock/internal/fs/atomicfile"
	"github.com/uvwt/agentdock/internal/fs/filelock"
)

const (
	registryVersion      = 1
	maxRegistryFileBytes = 1 << 20
	maxDescriptionBytes  = 1024
)

var identifierPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._+-]{0,63}$`)

type registryFile struct {
	Version int          `json:"version"`
	Plugins []Definition `json:"plugins"`
}

type Store struct {
	path     string
	lockPath string
}

func New(agentDockHome string) (*Store, error) {
	if strings.TrimSpace(agentDockHome) == "" {
		return nil, newError("PLUGIN_STORE_INVALID", "AgentDock home is required for the plugin registry", nil, nil)
	}
	root := filepath.Join(agentDockHome, "plugins")
	store := &Store{path: filepath.Join(root, "plugins.json"), lockPath: filepath.Join(root, ".store.lock")}
	if _, err := store.load(); err != nil {
		return nil, err
	}
	return store, nil
}

func (s *Store) Path() string { return s.path }

func (s *Store) List() ([]Definition, error) {
	plugins, err := s.load()
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(plugins))
	for name := range plugins {
		names = append(names, name)
	}
	sort.Strings(names)
	items := make([]Definition, 0, len(names))
	for _, name := range names {
		items = append(items, cloneDefinition(plugins[name]))
	}
	return items, nil
}

func (s *Store) Get(name string) (Definition, error) {
	name = strings.TrimSpace(name)
	plugins, err := s.load()
	if err != nil {
		return Definition{}, err
	}
	definition, ok := plugins[name]
	if !ok {
		return Definition{}, newError("PLUGIN_NOT_FOUND", "plugin is not registered", map[string]any{"name": name}, nil)
	}
	return cloneDefinition(definition), nil
}

func (s *Store) Upsert(raw Definition) (Definition, error) {
	definition, err := normalizeDefinition(raw)
	if err != nil {
		return Definition{}, err
	}
	plugins, err := s.update(func(plugins map[string]Definition) error {
		if err := ensureUniqueMembership(plugins, definition); err != nil {
			return err
		}
		plugins[definition.Name] = definition
		return nil
	})
	if err != nil {
		return Definition{}, err
	}
	return cloneDefinition(plugins[definition.Name]), nil
}

func (s *Store) Remove(name string) error {
	name = strings.TrimSpace(name)
	if !identifierPattern.MatchString(name) {
		return invalidIdentifier("plugin", name)
	}
	_, err := s.update(func(plugins map[string]Definition) error {
		if _, ok := plugins[name]; !ok {
			return newError("PLUGIN_NOT_FOUND", "plugin is not registered", map[string]any{"name": name}, nil)
		}
		delete(plugins, name)
		return nil
	})
	return err
}

func (s *Store) SetEnabled(name string, enabled bool) (Definition, error) {
	name = strings.TrimSpace(name)
	if !identifierPattern.MatchString(name) {
		return Definition{}, invalidIdentifier("plugin", name)
	}
	plugins, err := s.update(func(plugins map[string]Definition) error {
		definition, ok := plugins[name]
		if !ok {
			return newError("PLUGIN_NOT_FOUND", "plugin is not registered", map[string]any{"name": name}, nil)
		}
		definition.Enabled = enabled
		plugins[name] = definition
		return nil
	})
	if err != nil {
		return Definition{}, err
	}
	return cloneDefinition(plugins[name]), nil
}

func (s *Store) SkillMembership(name string) (Membership, bool, error) {
	return s.membership(name, true)
}

func (s *Store) MCPMembership(name string) (Membership, bool, error) {
	return s.membership(name, false)
}

func (s *Store) membership(name string, skill bool) (Membership, bool, error) {
	name = strings.TrimSpace(name)
	plugins, err := s.load()
	if err != nil {
		return Membership{}, false, err
	}
	for _, definition := range plugins {
		members := definition.MCPServers
		if skill {
			members = definition.Skills
		}
		if contains(members, name) {
			return Membership{Plugin: definition.Name, Enabled: definition.Enabled}, true, nil
		}
	}
	return Membership{}, false, nil
}

func (s *Store) load() (map[string]Definition, error) {
	release, err := s.acquire()
	if err != nil {
		return nil, err
	}
	defer release()
	return s.loadUnlocked()
}

func (s *Store) loadUnlocked() (map[string]Definition, error) {
	registryHandle, err := os.Open(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return map[string]Definition{}, nil
	}
	if err != nil {
		return nil, newError("PLUGIN_STORE_READ_FAILED", "read plugin registry", nil, err)
	}
	defer registryHandle.Close()
	data, err := io.ReadAll(io.LimitReader(registryHandle, maxRegistryFileBytes+1))
	if err != nil {
		return nil, newError("PLUGIN_STORE_READ_FAILED", "read plugin registry", nil, err)
	}
	if len(data) > maxRegistryFileBytes {
		return nil, newError("PLUGIN_STORE_INVALID", fmt.Sprintf("plugin registry exceeds %d bytes", maxRegistryFileBytes), nil, nil)
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var file registryFile
	if err := decoder.Decode(&file); err != nil {
		return nil, newError("PLUGIN_STORE_INVALID", "decode plugin registry", nil, err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return nil, newError("PLUGIN_STORE_INVALID", "plugin registry must contain exactly one JSON value", nil, err)
	}
	if file.Version != registryVersion {
		return nil, newError("PLUGIN_STORE_VERSION_UNSUPPORTED", "unsupported plugin registry version", map[string]any{"version": file.Version}, nil)
	}
	plugins := make(map[string]Definition, len(file.Plugins))
	for _, raw := range file.Plugins {
		definition, err := normalizeDefinition(raw)
		if err != nil {
			return nil, err
		}
		if _, exists := plugins[definition.Name]; exists {
			return nil, newError("PLUGIN_DUPLICATE", "duplicate plugin name", map[string]any{"name": definition.Name}, nil)
		}
		if err := ensureUniqueMembership(plugins, definition); err != nil {
			return nil, err
		}
		plugins[definition.Name] = definition
	}
	return plugins, nil
}

func (s *Store) saveUnlocked(plugins map[string]Definition) error {
	names := make([]string, 0, len(plugins))
	for name := range plugins {
		names = append(names, name)
	}
	sort.Strings(names)
	file := registryFile{Version: registryVersion, Plugins: make([]Definition, 0, len(names))}
	for _, name := range names {
		file.Plugins = append(file.Plugins, cloneDefinition(plugins[name]))
	}
	data, err := json.MarshalIndent(file, "", "  ")
	if err != nil {
		return newError("PLUGIN_STORE_WRITE_FAILED", "encode plugin registry", nil, err)
	}
	data = append(data, '\n')
	if len(data) > maxRegistryFileBytes {
		return newError("PLUGIN_STORE_INVALID", fmt.Sprintf("plugin registry exceeds %d bytes", maxRegistryFileBytes), nil, nil)
	}
	if err := atomicfile.Write(s.path, data, 0o600); err != nil {
		return newError("PLUGIN_STORE_WRITE_FAILED", "write plugin registry", nil, err)
	}
	return nil
}

func (s *Store) update(mutator func(map[string]Definition) error) (map[string]Definition, error) {
	release, err := s.acquire()
	if err != nil {
		return nil, err
	}
	defer release()
	plugins, err := s.loadUnlocked()
	if err != nil {
		return nil, err
	}
	if err := mutator(plugins); err != nil {
		return nil, err
	}
	if err := s.saveUnlocked(plugins); err != nil {
		return nil, err
	}
	return plugins, nil
}

func (s *Store) acquire() (func(), error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	release, err := filelock.Acquire(ctx, s.lockPath)
	if err != nil {
		return nil, newError("PLUGIN_STORE_LOCK_FAILED", "lock plugin registry", nil, err)
	}
	return release, nil
}

func normalizeDefinition(raw Definition) (Definition, error) {
	definition := Definition{
		Name:        strings.TrimSpace(raw.Name),
		Description: strings.TrimSpace(raw.Description),
		Enabled:     raw.Enabled,
		Skills:      normalizeMembers(raw.Skills),
		MCPServers:  normalizeMembers(raw.MCPServers),
	}
	if !identifierPattern.MatchString(definition.Name) {
		return Definition{}, invalidIdentifier("plugin", definition.Name)
	}
	if definition.Description == "" {
		return Definition{}, newError("PLUGIN_DESCRIPTION_REQUIRED", "plugin description is required", map[string]any{"name": definition.Name}, nil)
	}
	if len([]byte(definition.Description)) > maxDescriptionBytes {
		return Definition{}, newError("PLUGIN_DESCRIPTION_TOO_LONG", "plugin description is too long", map[string]any{"name": definition.Name, "maximum_bytes": maxDescriptionBytes}, nil)
	}
	if len(definition.Skills) == 0 && len(definition.MCPServers) == 0 {
		return Definition{}, newError("PLUGIN_MEMBERS_REQUIRED", "plugin must contain at least one Skill or MCP server", map[string]any{"name": definition.Name}, nil)
	}
	for _, name := range append(append([]string{}, definition.Skills...), definition.MCPServers...) {
		if !identifierPattern.MatchString(name) {
			return Definition{}, invalidIdentifier("plugin member", name)
		}
	}
	return definition, nil
}

func ensureUniqueMembership(plugins map[string]Definition, candidate Definition) error {
	for _, existing := range plugins {
		if existing.Name == candidate.Name {
			continue
		}
		if member, ok := firstIntersection(existing.Skills, candidate.Skills); ok {
			return newError("PLUGIN_MEMBER_CONFLICT", "Skill already belongs to another plugin", map[string]any{"member_type": "skill", "member": member, "plugin": existing.Name}, nil)
		}
		if member, ok := firstIntersection(existing.MCPServers, candidate.MCPServers); ok {
			return newError("PLUGIN_MEMBER_CONFLICT", "MCP server already belongs to another plugin", map[string]any{"member_type": "mcp", "member": member, "plugin": existing.Name}, nil)
		}
	}
	return nil
}

func normalizeMembers(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	items := make([]string, 0, len(values))
	for _, raw := range values {
		value := strings.TrimSpace(raw)
		if value == "" {
			continue
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		items = append(items, value)
	}
	sort.Strings(items)
	return items
}

func cloneDefinition(value Definition) Definition {
	value.Skills = append([]string(nil), value.Skills...)
	value.MCPServers = append([]string(nil), value.MCPServers...)
	return value
}

func invalidIdentifier(kind, value string) error {
	return newError("PLUGIN_IDENTIFIER_INVALID", fmt.Sprintf("invalid %s identifier", kind), map[string]any{"kind": kind, "value": value}, nil)
}

func contains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func firstIntersection(left, right []string) (string, bool) {
	seen := make(map[string]struct{}, len(left))
	for _, value := range left {
		seen[value] = struct{}{}
	}
	for _, value := range right {
		if _, ok := seen[value]; ok {
			return value, true
		}
	}
	return "", false
}
