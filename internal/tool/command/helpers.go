package command

import toolcore "github.com/uvwt/agentdock/internal/tool/core"

type Result = toolcore.Result
type ToolError = toolcore.ToolError

func boundedInt(value, fallback, minimum, maximum int) int {
	return toolcore.BoundedInt(value, fallback, minimum, maximum)
}
func toolError(code, message, category string) *ToolError {
	return toolcore.NewError(code, message, category)
}
func toolErrorDetails(code, message, category string, details map[string]any) *ToolError {
	return toolcore.NewErrorDetails(code, message, category, details)
}
