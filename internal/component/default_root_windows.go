//go:build windows

package component

import (
	"os"
	"path/filepath"
)

func DefaultRuntimeRoot() string {
	root := os.Getenv("LOCALAPPDATA")
	if root == "" {
		return ""
	}
	return filepath.Join(root, "AgentDock")
}
