package command

import (
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
func mapArg(args map[string]any, key string) map[string]any { return toolcore.MapArg(args, key) }
func boundedInt(value, fallback, minimum, maximum int) int {
	return toolcore.BoundedInt(value, fallback, minimum, maximum)
}
func toolError(code, message, category string) *ToolError {
	return toolcore.NewError(code, message, category)
}
func toolErrorDetails(code, message, category string, details map[string]any) *ToolError {
	return toolcore.NewErrorDetails(code, message, category, details)
}
