//go:build linux

package wslfilehelper

import "errors"

type ToolFailure struct {
	Code    string
	Message string
	Details map[string]any
}

func (e *ToolFailure) Error() string { return e.Message }

func fail(code, message string, details map[string]any) error {
	if details == nil {
		details = map[string]any{}
	}
	return &ToolFailure{Code: code, Message: message, Details: details}
}

func failureFrom(err error) *ToolFailure {
	var failure *ToolFailure
	if errors.As(err, &failure) {
		return failure
	}
	return &ToolFailure{Code: "WSL_FILE_RUNTIME_ERROR", Message: err.Error(), Details: map[string]any{"type": "runtime"}}
}
