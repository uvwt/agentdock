package browser

import (
	"encoding/json"
	"errors"

	toolcore "github.com/uvwt/agentdock/internal/tool/core"
)

func browserInvalid(message string, details map[string]any) *Error {
	typed := &ErrorDetails{}
	hasDetails := false
	if details != nil {
		if value, ok := details["field"].(string); ok {
			typed.Field = value
			hasDetails = true
		}
		if value, ok := details["reason"].(string); ok {
			typed.Reason = value
			hasDetails = true
		}
		if value, ok := details["selector"].(string); ok {
			typed.Selector = value
			hasDetails = true
		}
		if value, ok := details["browser"].(string); ok {
			typed.Browser = Kind(value)
			hasDetails = true
		}
		if value, ok := details["action"].(string); ok {
			typed.Action = value
			hasDetails = true
		}
		if value, ok := integerValue(details["index"]); ok {
			index := int(value)
			typed.ItemIndex = &index
			hasDetails = true
		}
		if value, ok := integerValue(details["count"]); ok {
			typed.Count = int(value)
			hasDetails = true
		}
	}
	if !hasDetails {
		typed = nil
	}
	return &Error{Code: ErrActionInvalid, Message: message, Phase: "validation", Details: typed}
}

func browserIntPointer(value int) *int { return &value }

func browserFailure(err error) toolcore.Result {
	var browserErr *Error
	if !errors.As(err, &browserErr) {
		browserErr = &Error{Code: ErrCDPFailed, Message: "browser operation failed", Phase: "runtime", Cause: err}
	}
	errorValue := map[string]any{"code": browserErr.Code, "message": browserErr.Message, "phase": browserErr.Phase}
	if browserErr.Details != nil {
		errorValue["details"] = browserErr.Details
	}
	return toolcore.Result{"browser_ok": false, "code": browserErr.Code, "error": errorValue}
}

func browserResultMap(value any) toolcore.Result {
	encoded, _ := json.Marshal(value)
	result := toolcore.Result{}
	_ = json.Unmarshal(encoded, &result)
	return result
}
