package recall

import (
	"io"

	toolcore "github.com/uvwt/agentdock/internal/tool/core"
)

type Result = toolcore.Result
type ToolError = toolcore.ToolError

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
