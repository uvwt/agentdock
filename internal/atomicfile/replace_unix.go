//go:build darwin || linux

package atomicfile

import "os"

func replaceFile(source, target string) error {
	return os.Rename(source, target)
}
