package envregistry

import (
	"fmt"
	"os"
	"regexp"
	"strings"
	"time"
)

type MigrationResult struct {
	EnvFile       string   `json:"env_file"`
	BackupFile    string   `json:"backup_file,omitempty"`
	Migrated      []Entry  `json:"migrated"`
	RemovedKeys   []string `json:"removed_keys"`
	Changed       bool     `json:"changed"`
	RestartNeeded bool     `json:"restart_needed"`
}

var exportPattern = regexp.MustCompile(`^\s*(?:export\s+)?([A-Z_][A-Z0-9_]*)=(.*)$`)

func (s *Store) MigrateFromEnvFile(path string) (MigrationResult, error) {
	result := MigrationResult{EnvFile: path}
	data, err := os.ReadFile(path)
	if err != nil {
		return result, err
	}
	defs := s.KnownDefinitions("")
	byName := map[string]Definition{}
	for _, def := range defs {
		byName[def.Name] = def
	}
	lines := strings.SplitAfter(string(data), "\n")
	out := make([]string, 0, len(lines))
	removed := map[string]bool{}
	for _, line := range lines {
		raw := strings.TrimRight(line, "\n")
		match := exportPattern.FindStringSubmatch(raw)
		if len(match) != 3 {
			out = append(out, line)
			continue
		}
		key := match[1]
		def, ok := byName[key]
		if !ok {
			out = append(out, line)
			continue
		}
		value := parseEnvValue(match[2])
		entry, err := s.Set(def.Skill, key, def.Kind, value)
		if err != nil {
			return result, err
		}
		result.Migrated = append(result.Migrated, entry)
		removed[key] = true
		result.Changed = true
	}
	if !result.Changed {
		return result, nil
	}
	for key := range removed {
		result.RemovedKeys = append(result.RemovedKeys, key)
	}
	backup := fmt.Sprintf("%s.bak.%s", path, time.Now().UTC().Format("20060102T150405Z"))
	if err := atomicWriteFile(backup, data, 0o600); err != nil {
		return result, err
	}
	newData := []byte(strings.Join(out, ""))
	if err := atomicWriteFile(path, newData, 0o600); err != nil {
		return result, err
	}
	result.BackupFile = backup
	result.RestartNeeded = true
	return result, nil
}

func parseEnvValue(raw string) string {
	value := strings.TrimSpace(raw)
	if len(value) >= 2 {
		quote := value[0]
		if (quote == '"' || quote == '\'') && value[len(value)-1] == quote {
			value = value[1 : len(value)-1]
		}
	}
	if unquoted, err := strconvUnquote(value); err == nil {
		return unquoted
	}
	return value
}

func strconvUnquote(value string) (string, error) {
	if len(value) >= 2 && value[0] == '"' && value[len(value)-1] == '"' {
		var out strings.Builder
		escaped := false
		for _, ch := range value[1 : len(value)-1] {
			if escaped {
				switch ch {
				case 'n':
					out.WriteByte('\n')
				case 't':
					out.WriteByte('\t')
				default:
					out.WriteRune(ch)
				}
				escaped = false
				continue
			}
			if ch == '\\' {
				escaped = true
				continue
			}
			out.WriteRune(ch)
		}
		return out.String(), nil
	}
	return value, nil
}
