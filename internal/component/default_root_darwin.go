//go:build darwin

package component

import (
	"os"
	"path/filepath"
)

func DefaultRuntimeRoot() string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return ""
	}
	return filepath.Join(home, "Library", "Application Support", "AgentDock")
}
