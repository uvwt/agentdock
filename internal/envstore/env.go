package envstore

import (
	"os"
	"sort"
)

var minimalSystemKeys = []string{
	"PATH",
	"HOME",
	"TMPDIR",
	"LANG",
	"LC_ALL",
	"SSL_CERT_FILE",
	"SSL_CERT_DIR",
}

func MinimalSystemEnv() map[string]string {
	env := make(map[string]string, len(minimalSystemKeys))
	for _, key := range minimalSystemKeys {
		if value := os.Getenv(key); value != "" {
			env[key] = value
		}
	}
	if env["HOME"] == "" {
		if home, err := os.UserHomeDir(); err == nil && home != "" {
			env["HOME"] = home
		}
	}
	if env["TMPDIR"] == "" {
		env["TMPDIR"] = os.TempDir()
	}
	return env
}

func Merge(base map[string]string, overlays ...map[string]string) map[string]string {
	merged := make(map[string]string, len(base))
	for key, value := range base {
		merged[key] = value
	}
	for _, overlay := range overlays {
		for key, value := range overlay {
			merged[key] = value
		}
	}
	return merged
}

func Format(values map[string]string) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	formatted := make([]string, 0, len(keys))
	for _, key := range keys {
		formatted = append(formatted, key+"="+values[key])
	}
	return formatted
}
