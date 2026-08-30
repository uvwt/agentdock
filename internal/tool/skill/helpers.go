package skill

import toolcore "github.com/uvwt/agentdock/internal/tool/core"

type Result = toolcore.Result
type ToolError = toolcore.ToolError

func toolErrorDetails(code, message, category string, details map[string]any) *ToolError {
	return toolcore.NewErrorDetails(code, message, category, details)
}
func toolErrorCause(code, message, category string, details map[string]any, cause error) *ToolError {
	return toolcore.NewErrorCause(code, message, category, details, cause)
}
