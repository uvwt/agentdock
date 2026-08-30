package media

import (
	"github.com/uvwt/agentdock/internal/textutil"
	toolcore "github.com/uvwt/agentdock/internal/tool/core"
	"time"
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
func redactSecrets(value string, extraPatterns []string) string {
	return toolcore.RedactSecrets(value, extraPatterns)
}
func truncateBytes(data []byte, maxBytes int) (string, bool) {
	truncated := textutil.SafeTruncateBytes(data, maxBytes)
	return truncated.Text, truncated.Truncated
}
