package skills

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"
)

func (m *Manager) Validate(ctx context.Context, req ValidateRequest) (ValidateResult, error) {
	if strings.TrimSpace(req.Source) == "" {
		return ValidateResult{}, packageError(ErrInvalidPackage, "source", errors.New("source is required"))
	}
	maxBytes := req.MaxBytes
	if maxBytes <= 0 {
		maxBytes = m.MaxDownload
	}
	work, err := m.State.TempPath("validate")
	if err != nil {
		return ValidateResult{}, packageError(ErrInstallFailed, "temp", err)
	}
	defer cleanupWorkingDirectory(work)

	result := ValidateResult{Source: safeSourceLabel(req.Source)}
	packageDir, digest, err := m.prepareSource(ctx, req.Source, work, maxBytes)
	if err != nil {
		result.addIssue(err)
		return result, nil
	}
	result.Digest = digest
	if expected := normalizeDigest(req.DigestSHA256); expected != "" && expected != digest {
		result.addIssue(packageError(ErrDigestMismatch, "digest", fmt.Errorf("expected %s, got %s", expected, digest)))
	}
	if err := ValidatePackage(packageDir); err != nil {
		result.addIssue(err)
	}
	doc, err := LoadSkillDocument(packageDir)
	if err != nil {
		result.addIssue(err)
	} else {
		result.Document = doc
	}
	result.Valid = len(result.Issues) == 0
	return result, nil
}

func (r *ValidateResult) addIssue(err error) {
	var packageErr *Error
	if errors.As(err, &packageErr) {
		r.Issues = append(r.Issues, ValidateIssue{Code: packageErr.Code, Stage: packageErr.Stage, Message: packageErr.Error()})
		return
	}
	r.Issues = append(r.Issues, ValidateIssue{Code: ErrInvalidPackage, Stage: "validate", Message: err.Error()})
}

func safeSourceLabel(source string) string {
	parsed, err := url.Parse(source)
	if err == nil && parsed.Scheme != "" && parsed.Host != "" {
		parsed.User = nil
		parsed.RawQuery = ""
		parsed.Fragment = ""
		return parsed.String()
	}
	return source
}

func cleanupWorkingDirectory(path string) {
	_ = os.RemoveAll(path)
}
