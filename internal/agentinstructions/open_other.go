//go:build !unix

package agentinstructions

import "os"

func instructionOpenFlags() int { return os.O_RDONLY }
