package recall

import (
	"io"

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
func mapArg(args map[string]any, key string) map[string]any { return toolcore.MapArg(args, key) }
func toolError(code, message, category string) *ToolError {
	return toolcore.NewError(code, message, category)
}
func toolErrorDetails(code, message, category string, details map[string]any) *ToolError {
	return toolcore.NewErrorDetails(code, message, category, details)
}
func toolErrorCause(code, message, category string, details map[string]any, cause error) *ToolError {
	return toolcore.NewErrorCause(code, message, category, details, cause)
}
func readBoundedBody(reader io.Reader, maxBytes int64) ([]byte, error) {
	return toolcore.ReadBoundedBody(reader, maxBytes)
}
func remarshal(input, output any) error { return toolcore.Remarshal(input, output) }
