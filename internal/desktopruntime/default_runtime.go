package desktopruntime

import (
	"os"
	"strings"
)

func configuredRuntimeRoot() string {
	return strings.TrimSpace(os.Getenv("AGENTDOCK_RUNTIME_ROOT"))
}
