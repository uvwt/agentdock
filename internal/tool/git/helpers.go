package git

import (
	"os/exec"
	"time"

	toolcore "github.com/uvwt/agentdock/internal/tool/core"
)

type Result = toolcore.Result

type ToolError = toolcore.ToolError

func stringArg(args map[string]any, key, fallback string) string {
	return toolcore.StringArg(args, key, fallback)
}

func intArg(args map[string]any, key string, fallback int) int {
	return toolcore.IntArg(args, key, fallback)
}

func boolArg(args map[string]any, key string, fallback bool) bool {
	return toolcore.BoolArg(args, key, fallback)
}

func stringSliceArg(args map[string]any, key string) []string {
	return toolcore.StringSliceArg(args, key)
}

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

func redactSecrets(value string, extraPatterns []string) string {
	return toolcore.RedactSecrets(value, extraPatterns)
}

func runBoundedCombinedOutput(cmd *exec.Cmd, limit int) ([]byte, int64, bool, error) {
	return toolcore.RunBoundedCombinedOutput(cmd, limit)
}

func readBoundedBody(reader interface{ Read([]byte) (int, error) }, maxBytes int64) ([]byte, error) {
	return toolcore.ReadBoundedBody(reader, maxBytes)
}

func shouldSkipDir(name string) bool {
	switch name {
	case ".git", ".hg", ".svn", "node_modules", "vendor", "dist", "build", ".cache", "__pycache__":
		return true
	default:
		return false
	}
}
