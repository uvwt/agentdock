package plugin

import "fmt"

type Error struct {
	Code  string
	Stage string
	Err   error
}

func (e *Error) Error() string {
	if e == nil {
		return ""
	}
	if e.Stage == "" {
		return e.Err.Error()
	}
	return fmt.Sprintf("%s: %v", e.Stage, e.Err)
}

func (e *Error) Unwrap() error { return e.Err }

func pluginError(code, stage string, err error) error {
	return &Error{Code: code, Stage: stage, Err: err}
}
