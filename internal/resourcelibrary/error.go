package resourcelibrary

import "fmt"

// Error 是资源库控制面的失败。Category 与 Runtime API 的 validation / not_found 对齐。
type Error struct {
	Code     string
	Message  string
	Category string
}

func (e *Error) Error() string {
	if e == nil {
		return ""
	}
	return e.Message
}

func failed(code, category, message string) error {
	return &Error{Code: code, Category: category, Message: message}
}

func failedf(code, category, format string, args ...any) error {
	return failed(code, category, fmt.Sprintf(format, args...))
}
