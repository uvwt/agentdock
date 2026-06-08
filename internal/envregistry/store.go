package envregistry

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

const (
	KindPlain  = "plain"
	KindSecret = "secret"
)

var envNamePattern = regexp.MustCompile(`^[A-Z_][A-Z0-9_]*$`)

type Definition struct {
	Skill  string `json:"skill"`
	Name   string `json:"name"`
	Kind   string `json:"kind"`
	Source string `json:"source,omitempty"`
}

type Entry struct {
	Skill          string     `json:"skill"`
	Name           string     `json:"name"`
	Kind           string     `json:"kind"`
	Source         string     `json:"source"`
	Configured     bool       `json:"configured"`
	Length         int        `json:"length,omitempty"`
	SHA256Prefix   string     `json:"sha256_prefix,omitempty"`
	UpdatedAt      *time.Time `json:"updated_at,omitempty"`
	LastVerifiedAt *time.Time `json:"last_verified_at,omitempty"`
	VerifyOK       *bool      `json:"verify_ok,omitempty"`
	VerifyMessage  string     `json:"verify_message,omitempty"`
}

type SkillSummary struct {
	Skill string  `json:"skill"`
	Vars  []Entry `json:"vars"`
}

type Store struct {
	root        string
	registry    string
	valuesDir   string
	definitions func() []Definition
}

type registryFile struct {
	Skills map[string]skillRegistry `json:"skills"`
}

type skillRegistry struct {
	Vars map[string]varRegistry `json:"vars"`
}

type varRegistry struct {
	Kind           string     `json:"kind"`
	Source         string     `json:"source"`
	UpdatedAt      *time.Time `json:"updated_at,omitempty"`
	LastVerifiedAt *time.Time `json:"last_verified_at,omitempty"`
	VerifyOK       *bool      `json:"verify_ok,omitempty"`
	VerifyMessage  string     `json:"verify_message,omitempty"`
}

type valuesFile struct {
	Env     map[string]string `json:"env,omitempty"`
	Secrets map[string]string `json:"secrets,omitempty"`
}

func New(root string, definitions func() []Definition) (*Store, error) {
	if strings.TrimSpace(root) == "" {
		return nil, errors.New("env registry root is required")
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	store := &Store{
		root:        abs,
		registry:    filepath.Join(abs, "registry.json"),
		valuesDir:   filepath.Join(abs, "values"),
		definitions: definitions,
	}
	if err := store.ensureDirs(); err != nil {
		return nil, err
	}
	return store, nil
}

func (s *Store) Root() string { return s.root }

func (s *Store) List() ([]SkillSummary, error) {
	reg, err := s.loadRegistry()
	if err != nil {
		return nil, err
	}
	defs := s.definitionMap("")
	for skill, skillDefs := range defs {
		if reg.Skills[skill].Vars == nil {
			item := reg.Skills[skill]
			item.Vars = map[string]varRegistry{}
			reg.Skills[skill] = item
		}
		for name, def := range skillDefs {
			item := reg.Skills[skill]
			if _, ok := item.Vars[name]; !ok {
				item.Vars[name] = varRegistry{Kind: def.Kind, Source: def.Source}
				reg.Skills[skill] = item
			}
		}
	}
	skills := make([]string, 0, len(reg.Skills))
	for skill := range reg.Skills {
		skills = append(skills, skill)
	}
	sort.Strings(skills)
	out := make([]SkillSummary, 0, len(skills))
	for _, skill := range skills {
		entries, err := s.inspectWithRegistry(reg, skill)
		if err != nil {
			return nil, err
		}
		if len(entries) > 0 {
			out = append(out, SkillSummary{Skill: skill, Vars: entries})
		}
	}
	return out, nil
}

func (s *Store) Inspect(skill string) ([]Entry, error) {
	if err := validateSkill(skill); err != nil {
		return nil, err
	}
	reg, err := s.loadRegistry()
	if err != nil {
		return nil, err
	}
	return s.inspectWithRegistry(reg, skill)
}

func (s *Store) Set(skill, name, kind, value string) (Entry, error) {
	if err := validateSkill(skill); err != nil {
		return Entry{}, err
	}
	if err := validateName(name); err != nil {
		return Entry{}, err
	}
	kind = normalizeKind(kind)
	if kind == "" {
		return Entry{}, fmt.Errorf("kind must be %s or %s", KindPlain, KindSecret)
	}
	reg, err := s.loadRegistry()
	if err != nil {
		return Entry{}, err
	}
	values, err := s.loadValues(skill)
	if err != nil {
		return Entry{}, err
	}
	if kind == KindSecret {
		delete(values.Env, name)
		values.Secrets[name] = value
	} else {
		delete(values.Secrets, name)
		values.Env[name] = value
	}
	now := time.Now().UTC()
	skillReg := reg.Skills[skill]
	if skillReg.Vars == nil {
		skillReg.Vars = map[string]varRegistry{}
	}
	skillReg.Vars[name] = varRegistry{Kind: kind, Source: "registry", UpdatedAt: &now}
	reg.Skills[skill] = skillReg
	if err := s.saveValues(skill, values); err != nil {
		return Entry{}, err
	}
	if err := s.saveRegistry(reg); err != nil {
		return Entry{}, err
	}
	entries, err := s.inspectWithRegistry(reg, skill)
	if err != nil {
		return Entry{}, err
	}
	for _, entry := range entries {
		if entry.Name == name {
			return entry, nil
		}
	}
	return Entry{}, fmt.Errorf("saved variable %s not found", name)
}

func (s *Store) Delete(skill, name string) (bool, error) {
	if err := validateSkill(skill); err != nil {
		return false, err
	}
	if err := validateName(name); err != nil {
		return false, err
	}
	reg, err := s.loadRegistry()
	if err != nil {
		return false, err
	}
	values, err := s.loadValues(skill)
	if err != nil {
		return false, err
	}
	deleted := false
	if _, ok := values.Env[name]; ok {
		delete(values.Env, name)
		deleted = true
	}
	if _, ok := values.Secrets[name]; ok {
		delete(values.Secrets, name)
		deleted = true
	}
	if skillReg, ok := reg.Skills[skill]; ok {
		if _, ok := skillReg.Vars[name]; ok {
			delete(skillReg.Vars, name)
			reg.Skills[skill] = skillReg
			deleted = true
		}
	}
	if err := s.saveValues(skill, values); err != nil {
		return false, err
	}
	if err := s.saveRegistry(reg); err != nil {
		return false, err
	}
	return deleted, nil
}

func (s *Store) EnvForSkill(skill string, definitions []Definition) (map[string]string, []string, error) {
	if err := validateSkill(skill); err != nil {
		return nil, nil, err
	}
	values, err := s.loadValues(skill)
	if err != nil {
		return nil, nil, err
	}
	env := map[string]string{}
	secretsByValue := map[string]struct{}{}
	for _, def := range definitions {
		if def.Skill != "" && def.Skill != skill {
			continue
		}
		if def.Kind == KindSecret {
			if value, ok := values.Secrets[def.Name]; ok {
				env[def.Name] = value
				if value != "" {
					secretsByValue[value] = struct{}{}
				}
			} else if value, ok := os.LookupEnv(def.Name); ok {
				env[def.Name] = value
				if value != "" {
					secretsByValue[value] = struct{}{}
				}
			}
			continue
		}
		if value, ok := values.Env[def.Name]; ok {
			env[def.Name] = value
		} else if value, ok := os.LookupEnv(def.Name); ok {
			env[def.Name] = value
		}
	}
	secrets := make([]string, 0, len(values.Secrets)+len(secretsByValue))
	for _, value := range values.Secrets {
		if value != "" {
			secretsByValue[value] = struct{}{}
		}
	}
	for value := range secretsByValue {
		secrets = append(secrets, value)
	}
	sort.Slice(secrets, func(i, j int) bool { return len(secrets[i]) > len(secrets[j]) })
	return env, secrets, nil
}

func (s *Store) RecordVerification(skill string, ok bool, message string) error {
	if err := validateSkill(skill); err != nil {
		return err
	}
	reg, err := s.loadRegistry()
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	skillReg := reg.Skills[skill]
	if skillReg.Vars == nil {
		skillReg.Vars = map[string]varRegistry{}
	}
	for name, item := range skillReg.Vars {
		item.LastVerifiedAt = &now
		item.VerifyOK = &ok
		item.VerifyMessage = message
		skillReg.Vars[name] = item
	}
	reg.Skills[skill] = skillReg
	return s.saveRegistry(reg)
}

func (s *Store) KnownDefinitions(skill string) []Definition {
	defs := s.definitionMap(skill)
	items := make([]Definition, 0)
	for _, byName := range defs {
		for _, def := range byName {
			items = append(items, def)
		}
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].Skill == items[j].Skill {
			return items[i].Name < items[j].Name
		}
		return items[i].Skill < items[j].Skill
	})
	return items
}

func (s *Store) inspectWithRegistry(reg registryFile, skill string) ([]Entry, error) {
	values, err := s.loadValues(skill)
	if err != nil {
		return nil, err
	}
	names := map[string]varRegistry{}
	if skillReg, ok := reg.Skills[skill]; ok {
		for name, item := range skillReg.Vars {
			names[name] = item
		}
	}
	for _, def := range s.KnownDefinitions(skill) {
		if def.Skill == skill {
			if _, ok := names[def.Name]; !ok {
				names[def.Name] = varRegistry{Kind: def.Kind, Source: def.Source}
			}
		}
	}
	for name := range values.Env {
		if _, ok := names[name]; !ok {
			names[name] = varRegistry{Kind: KindPlain, Source: "registry"}
		}
	}
	for name := range values.Secrets {
		if _, ok := names[name]; !ok {
			names[name] = varRegistry{Kind: KindSecret, Source: "registry"}
		}
	}
	ordered := make([]string, 0, len(names))
	for name := range names {
		ordered = append(ordered, name)
	}
	sort.Strings(ordered)
	out := make([]Entry, 0, len(ordered))
	for _, name := range ordered {
		item := names[name]
		kind := normalizeKind(item.Kind)
		if kind == "" {
			kind = KindPlain
		}
		value := ""
		configured := false
		source := item.Source
		if kind == KindSecret {
			value, configured = values.Secrets[name]
		} else {
			value, configured = values.Env[name]
		}
		if !configured {
			if envValue, ok := os.LookupEnv(name); ok {
				value = envValue
				configured = true
				if source == "" || source == "compat" || source == "manifest" {
					source = "process"
				}
			}
		}
		if source == "" {
			source = "registry"
		}
		entry := Entry{
			Skill:          skill,
			Name:           name,
			Kind:           kind,
			Source:         source,
			Configured:     configured,
			UpdatedAt:      item.UpdatedAt,
			LastVerifiedAt: item.LastVerifiedAt,
			VerifyOK:       item.VerifyOK,
			VerifyMessage:  item.VerifyMessage,
		}
		if configured {
			entry.Length = len(value)
			entry.SHA256Prefix = shaPrefix(value)
		}
		out = append(out, entry)
	}
	return out, nil
}

func (s *Store) definitionMap(skill string) map[string]map[string]Definition {
	result := map[string]map[string]Definition{}
	if s.definitions == nil {
		return result
	}
	for _, def := range s.definitions() {
		if skill != "" && def.Skill != skill {
			continue
		}
		def.Kind = normalizeKind(def.Kind)
		if def.Kind == "" || !envNamePattern.MatchString(def.Name) || strings.TrimSpace(def.Skill) == "" {
			continue
		}
		if def.Source == "" {
			def.Source = "manifest"
		}
		if result[def.Skill] == nil {
			result[def.Skill] = map[string]Definition{}
		}
		result[def.Skill][def.Name] = def
	}
	return result
}

func (s *Store) ensureDirs() error {
	if err := os.MkdirAll(s.root, 0o700); err != nil {
		return err
	}
	if err := os.Chmod(s.root, 0o700); err != nil {
		return err
	}
	if err := os.MkdirAll(s.valuesDir, 0o700); err != nil {
		return err
	}
	return os.Chmod(s.valuesDir, 0o700)
}

func (s *Store) loadRegistry() (registryFile, error) {
	reg := registryFile{Skills: map[string]skillRegistry{}}
	data, err := os.ReadFile(s.registry)
	if errors.Is(err, os.ErrNotExist) {
		return reg, nil
	}
	if err != nil {
		return reg, err
	}
	if len(strings.TrimSpace(string(data))) == 0 {
		return reg, nil
	}
	if err := json.Unmarshal(data, &reg); err != nil {
		return reg, err
	}
	if reg.Skills == nil {
		reg.Skills = map[string]skillRegistry{}
	}
	return reg, nil
}

func (s *Store) saveRegistry(reg registryFile) error {
	if reg.Skills == nil {
		reg.Skills = map[string]skillRegistry{}
	}
	data, err := json.MarshalIndent(reg, "", "  ")
	if err != nil {
		return err
	}
	return atomicWriteFile(s.registry, append(data, '\n'), 0o600)
}

func (s *Store) loadValues(skill string) (valuesFile, error) {
	values := valuesFile{Env: map[string]string{}, Secrets: map[string]string{}}
	data, err := os.ReadFile(s.valuesPath(skill))
	if errors.Is(err, os.ErrNotExist) {
		return values, nil
	}
	if err != nil {
		return values, err
	}
	if len(strings.TrimSpace(string(data))) == 0 {
		return values, nil
	}
	if err := json.Unmarshal(data, &values); err != nil {
		return values, err
	}
	if values.Env == nil {
		values.Env = map[string]string{}
	}
	if values.Secrets == nil {
		values.Secrets = map[string]string{}
	}
	return values, nil
}

func (s *Store) saveValues(skill string, values valuesFile) error {
	data, err := json.MarshalIndent(values, "", "  ")
	if err != nil {
		return err
	}
	return atomicWriteFile(s.valuesPath(skill), append(data, '\n'), 0o600)
}

func (s *Store) valuesPath(skill string) string {
	return filepath.Join(s.valuesDir, skill+".json")
}

func atomicWriteFile(path string, data []byte, mode os.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".tmp-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Chmod(mode); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}

func normalizeKind(kind string) string {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case KindPlain, "env":
		return KindPlain
	case KindSecret, "secrets":
		return KindSecret
	default:
		return ""
	}
}

func validateSkill(skill string) error {
	if strings.TrimSpace(skill) == "" || strings.Contains(skill, "/") || strings.Contains(skill, "..") {
		return fmt.Errorf("invalid skill %q", skill)
	}
	return nil
}

func validateName(name string) error {
	if !envNamePattern.MatchString(name) {
		return fmt.Errorf("invalid env name %q", name)
	}
	return nil
}

func shaPrefix(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])[:16]
}
