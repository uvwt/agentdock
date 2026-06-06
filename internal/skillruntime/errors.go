package skillruntime

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

func runtimeError(code, stage string, err error) error {
	return &Error{Code: code, Stage: stage, Err: err}
}

const (
	ErrInvalidPackage      = "INVALID_SKILL_PACKAGE"
	ErrDigestMismatch      = "SKILL_DIGEST_MISMATCH"
	ErrManifestInvalid     = "SKILL_MANIFEST_INVALID"
	ErrIncompatible        = "SKILL_INCOMPATIBLE"
	ErrDependencyMissing   = "SKILL_DEPENDENCY_MISSING"
	ErrInstallFailed       = "SKILL_INSTALL_FAILED"
	ErrOperationMissing    = "SKILL_OPERATION_MISSING"
	ErrInputInvalid        = "SKILL_INPUT_INVALID"
	ErrOutputInvalid       = "SKILL_OUTPUT_INVALID"
	ErrPermissionDenied    = "SKILL_PERMISSION_DENIED"
	ErrBindingInvalid      = "SKILL_BINDING_INVALID"
	ErrSecretMissing       = "SKILL_SECRET_MISSING"
	ErrTimeout             = "SKILL_TIMEOUT"
	ErrOutputLimit         = "SKILL_OUTPUT_LIMIT"
	ErrExecutionFailed     = "SKILL_EXECUTION_FAILED"
	ErrRollbackUnavailable = "SKILL_ROLLBACK_UNAVAILABLE"
	ErrRollbackVerify      = "SKILL_ROLLBACK_VERIFY_FAILED"
)
