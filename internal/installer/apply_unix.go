//go:build !windows

package installer

import "os"

func currentUnixUID() int {
	return os.Getuid()
}
