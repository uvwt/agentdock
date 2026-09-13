//go:build darwin || linux

package config

import (
	"fmt"
	"os"
)

func validateACPCommandPlatform(path string, info os.FileInfo) error {
	if info.Mode().Perm()&0o111 == 0 {
		return fmt.Errorf("ACP command is not executable: %s", path)
	}
	return nil
}
