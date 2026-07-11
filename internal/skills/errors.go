package skills

import "fmt"

type Error struct {
	Code  string
	Stage string
	Err   error
}

func (e *Error) Error() string {
	if e.Err == nil {
		return fmt.Sprintf("%s at %s", e.Code, e.Stage)
	}
	return fmt.Sprintf("%s at %s: %v", e.Code, e.Stage, e.Err)
}

func (e *Error) Unwrap() error { return e.Err }

func packageError(code, stage string, err error) error {
	return &Error{Code: code, Stage: stage, Err: err}
}

const (
	ErrInvalidPackage      = "INVALID_SKILL_PACKAGE"
	ErrDigestMismatch      = "SKILL_DIGEST_MISMATCH"
	ErrDocumentInvalid     = "SKILL_DOCUMENT_INVALID"
	ErrInstallFailed       = "SKILL_INSTALL_FAILED"
	ErrRollbackUnavailable = "SKILL_ROLLBACK_UNAVAILABLE"
)
