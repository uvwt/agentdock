//go:build unix

package agentinstructions

import (
	"os"
	"syscall"
)

func instructionOpenFlags() int {
	// A regular file can be replaced between Lstat and OpenFile. Do not block
	// on a substituted FIFO or follow a newly substituted leaf symlink.
	return os.O_RDONLY | syscall.O_NONBLOCK | syscall.O_NOFOLLOW
}
