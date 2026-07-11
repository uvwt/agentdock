package skillruntime

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

func (r *Runtime) Validate(ctx context.Context, req ValidateRequest) (ValidateResult, error) {
	if strings.TrimSpace(req.Source) == "" {
		return ValidateResult{}, runtimeError(ErrInvalidPackage, "source", errors.New("source is required"))
	}
	maxBytes := req.MaxBytes
	if maxBytes <= 0 {
		maxBytes = r.MaxDownload
	}
	if maxBytes <= 0 {
		maxBytes = 128 << 20
	}
	work, err := r.State.TempPath("validate")
	if err != nil {
		return ValidateResult{}, runtimeError(ErrInstallFailed, "temp", err)
	}
	defer cleanupWorkingDirectory(work)

	result := ValidateResult{Source: safeSourceLabel(req.Source)}
	packageDir, digest, err := r.prepareSource(ctx, req.Source, work, maxBytes)
	if err != nil {
		result.addIssue(err)
		result.Valid = false
		return result, nil
	}
	result.Digest = digest
	if expected := normalizeDigest(req.DigestSHA256); expected != "" && expected != digest {
		result.addIssue(runtimeError(ErrDigestMismatch, "digest", fmt.Errorf("expected %s, got %s", expected, digest)))
	}

	manifest, err := parsePackageManifest(packageDir)
	if err != nil {
		result.addIssue(err)
		result.Valid = false
		return result, nil
	}
	result.Manifest = manifest
	if err := ValidateSkillDocument(packageDir, manifest); err != nil {
		result.addIssue(err)
	}
	result.Env = EnvDefinitionsForManifest(manifest)
	result.Commands = checkManifestCommands(manifest)
	if err := ValidatePackageManifest(packageDir, manifest); err != nil {
		result.addIssue(err)
	}
	if err := validateInstallEnvDeclarations(manifest, req.ConfirmedNoEnv); err != nil {
		result.RequiresNoEnvConfirm = true
		result.addIssue(err)
	}
	for _, check := range result.Commands {
		if !check.Found {
			result.addIssue(runtimeError(ErrDependencyMissing, "dependency", fmt.Errorf("command %s: %s", check.Command, check.Error)))
		}
	}
	result.Valid = len(result.Issues) == 0
	return result, nil
}

func parsePackageManifest(packageDir string) (Manifest, error) {
	data, err := os.ReadFile(filepath.Join(packageDir, "agentdock.yaml"))
	if err != nil {
		return Manifest{}, runtimeError(ErrManifestInvalid, "manifest.read", err)
	}
	manifest, err := ParseManifest(data)
	if err != nil {
		return Manifest{}, runtimeError(ErrManifestInvalid, "manifest.parse", err)
	}
	return manifest, nil
}

func (r *ValidateResult) addIssue(err error) {
	var runtimeErr *Error
	if errors.As(err, &runtimeErr) {
		r.Issues = append(r.Issues, ValidateIssue{
			Code:    runtimeErr.Code,
			Stage:   runtimeErr.Stage,
			Message: runtimeErr.Error(),
		})
		return
	}
	r.Issues = append(r.Issues, ValidateIssue{
		Code:    "SKILL_VALIDATE_FAILED",
		Stage:   "validate",
		Message: err.Error(),
	})
}

func checkManifestCommands(manifest Manifest) []CommandCheck {
	checks := make([]CommandCheck, 0, len(manifest.Spec.Permissions.Commands))
	for _, command := range manifest.Spec.Permissions.Commands {
		path, err := exec.LookPath(command)
		check := CommandCheck{Command: command, Found: err == nil, Path: path}
		if err != nil {
			check.Error = err.Error()
		}
		checks = append(checks, check)
	}
	return checks
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
