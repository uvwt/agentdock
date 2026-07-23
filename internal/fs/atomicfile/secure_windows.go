//go:build windows

package atomicfile

import (
	"os"

	"github.com/uvwt/agentdock/internal/fs/securepath"
)

func secureWrittenFile(path string, _ os.FileMode) error {
	return securepath.EnsurePrivate(path)
}
