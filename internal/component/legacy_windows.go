//go:build windows

package component

import "path/filepath"

func LegacyPaths(runtimeRoot string) []string {
	return uniquePaths([]string{filepath.Join(runtimeRoot, "bin", "cloudflared.exe")})
}
