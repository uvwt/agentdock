//go:build darwin

package component

import (
	"os"
	"path/filepath"
	"strings"
)

func LegacyPaths(runtimeRoot string) []string {
	var paths []string
	if executable, err := os.Executable(); err == nil {
		if resolved, resolveErr := filepath.EvalSymlinks(executable); resolveErr == nil {
			executable = resolved
		}
		if strings.Contains(filepath.ToSlash(executable), ".app/Contents/Helpers/") {
			paths = append(paths, filepath.Join(filepath.Dir(executable), "cloudflared"))
		}
	}
	return uniquePaths(paths)
}
