package file

import (
	"os/exec"
	"path/filepath"
	"time"

	toolcore "github.com/uvwt/agentdock/internal/tool/core"
)

type Result = toolcore.Result
type ToolError = toolcore.ToolError

func boundedInt(value, fallback, minimum, maximum int) int {
	return toolcore.BoundedInt(value, fallback, minimum, maximum)
}
func boundedMilliseconds(value, fallback, maximum int) time.Duration {
	return toolcore.BoundedMilliseconds(value, fallback, maximum)
}
func toolError(code, message, category string) *ToolError {
	return toolcore.NewError(code, message, category)
}
func toolErrorDetails(code, message, category string, details map[string]any) *ToolError {
	return toolcore.NewErrorDetails(code, message, category, details)
}
func toolErrorCause(code, message, category string, details map[string]any, cause error) *ToolError {
	return toolcore.NewErrorCause(code, message, category, details, cause)
}

func runBoundedCombinedOutput(cmd *exec.Cmd, limit int) ([]byte, int64, bool, error) {
	return toolcore.RunBoundedCombinedOutput(cmd, limit)
}

func redactSecrets(value string, extraPatterns []string) string {
	return toolcore.RedactSecrets(value, extraPatterns)
}

func relativePathFromRoot(root, abs string) (string, error) {
	rel, err := filepath.Rel(root, abs)
	if err != nil {
		return "", err
	}
	return filepath.ToSlash(rel), nil
}
