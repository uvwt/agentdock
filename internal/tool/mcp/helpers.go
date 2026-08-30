package mcp

import toolcore "github.com/uvwt/agentdock/internal/tool/core"

type Result = toolcore.Result
type ToolError = toolcore.ToolError

func boundedInt(value, fallback, minimum, maximum int) int {
	return toolcore.BoundedInt(value, fallback, minimum, maximum)
}
func toolErrorDetails(code, message, category string, details map[string]any) *ToolError {
	return toolcore.NewErrorDetails(code, message, category, details)
}
func toolErrorCause(code, message, category string, details map[string]any, cause error) *ToolError {
	return toolcore.NewErrorCause(code, message, category, details, cause)
}
