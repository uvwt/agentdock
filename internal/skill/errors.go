package skill

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

type ErrDocumentIdentityMismatch struct {
	Skill    string
	Document SkillDocument
}

func (e ErrDocumentIdentityMismatch) Error() string {
	return "SKILL.md name does not match managed Skill identity"
}

const (
	ErrInvalidPackage  = "INVALID_SKILL_PACKAGE"
	ErrDigestMismatch  = "SKILL_DIGEST_MISMATCH"
	ErrDocumentInvalid = "SKILL_DOCUMENT_INVALID"
	ErrInstallFailed   = "SKILL_INSTALL_FAILED"
	ErrUninstallFailed = "SKILL_UNINSTALL_FAILED"
	ErrPurgeFailed     = "SKILL_PURGE_FAILED"
)
