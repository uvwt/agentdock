package tools

type ToolError struct {
	Code      string         `json:"code"`
	Message   string         `json:"message"`
	Category  string         `json:"category"`
	Retryable bool           `json:"retryable"`
	Details   map[string]any `json:"details,omitempty"`
}

func (e *ToolError) Error() string { return e.Message }

func toolError(code, message, category string) *ToolError {
	return &ToolError{Code: code, Message: message, Category: category, Details: map[string]any{}}
}

func toolErrorDetails(code, message, category string, details map[string]any) *ToolError {
	return &ToolError{Code: code, Message: message, Category: category, Details: details}
}
