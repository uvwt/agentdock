//go:build windows

package authenticode

import (
	"context"
	"crypto/x509"
	"crypto/x509/pkix"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"testing"
)

func TestMicrosoftCertificatePublisherPolicy(t *testing.T) {
	tests := []struct {
		name string
		cert *x509.Certificate
		want bool
	}{
		{name: "organization", cert: &x509.Certificate{Subject: pkix.Name{Organization: []string{"Microsoft Corporation"}}}, want: true},
		{name: "common name", cert: &x509.Certificate{Subject: pkix.Name{CommonName: "Microsoft Corporation"}}, want: true},
		{name: "case insensitive", cert: &x509.Certificate{Subject: pkix.Name{Organization: []string{"microsoft corporation"}}}, want: true},
		{name: "different publisher", cert: &x509.Certificate{Subject: pkix.Name{Organization: []string{"Contoso Ltd"}}}},
		{name: "nil certificate", cert: nil},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := isMicrosoftCertificate(test.cert); got != test.want {
				t.Fatalf("isMicrosoftCertificate() = %t, want %t", got, test.want)
			}
		})
	}
}

func TestVerifyFileOrSameSignerPolicy(t *testing.T) {
	otherTrustFailure := syscall.Errno(0x800B010A)

	tests := []struct {
		name            string
		targetErr       error
		referenceErr    error
		targetSigner    string
		referenceSigner string
		wantErr         bool
	}{
		{
			name: "trusted target",
		},
		{
			name:            "same self-signed publisher",
			targetErr:       errCertUntrustedRoot,
			referenceErr:    errCertUntrustedRoot,
			targetSigner:    "AABBCC",
			referenceSigner: "aabbcc",
		},
		{
			name:            "different self-signed publisher",
			targetErr:       errCertUntrustedRoot,
			referenceErr:    errCertUntrustedRoot,
			targetSigner:    "AABBCC",
			referenceSigner: "DDEEFF",
			wantErr:         true,
		},
		{
			name:      "target trust failure is not untrusted root",
			targetErr: otherTrustFailure,
			wantErr:   true,
		},
		{
			name:         "installed reference is not same untrusted-root mode",
			targetErr:    errCertUntrustedRoot,
			referenceErr: nil,
			wantErr:      true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			verify := func(path string) error {
				if path == "target" {
					return test.targetErr
				}
				return test.referenceErr
			}
			thumbprint := func(path string) (string, error) {
				if path == "target" {
					return test.targetSigner, nil
				}
				return test.referenceSigner, nil
			}
			err := verifyFileOrSameSignerWith("target", "reference", verify, thumbprint)
			if test.wantErr && err == nil {
				t.Fatal("expected verification error")
			}
			if !test.wantErr && err != nil {
				t.Fatalf("unexpected verification error: %v", err)
			}
		})
	}
}

func TestVerifyFileOrSameSignerPropagatesSignerReadFailure(t *testing.T) {
	readErr := errors.New("cannot read signer")
	err := verifyFileOrSameSignerWith(
		"target",
		"reference",
		func(string) error { return errCertUntrustedRoot },
		func(string) (string, error) { return "", readErr },
	)
	if !errors.Is(err, readErr) {
		t.Fatalf("expected signer read error, got %v", err)
	}
}

func TestSelfSignedSameSignerIntegration(t *testing.T) {
	if os.Getenv("AGENTDOCK_AUTHENTICODE_INTEGRATION") != "1" {
		t.Skip("set AGENTDOCK_AUTHENTICODE_INTEGRATION=1 to run self-signed Authenticode integration")
	}
	signTool := findSignToolForTest(t)
	testExecutable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	password := "agentdock-authenticode-test"
	pfxA, thumbA := createSelfSignedCodeSigningCertificateForTest(t, root, "A", password)
	t.Cleanup(func() { removeTestCertificate(thumbA) })
	pfxB, thumbB := createSelfSignedCodeSigningCertificateForTest(t, root, "B", password)
	t.Cleanup(func() { removeTestCertificate(thumbB) })

	referencePath := filepath.Join(root, "reference.exe")
	targetPath := filepath.Join(root, "target.exe")
	otherSignerPath := filepath.Join(root, "other-signer.exe")
	for _, path := range []string{referencePath, targetPath, otherSignerPath} {
		copyFileForTest(t, testExecutable, path)
	}
	signFileForTest(t, signTool, pfxA, password, referencePath)
	signFileForTest(t, signTool, pfxA, password, targetPath)
	signFileForTest(t, signTool, pfxB, password, otherSignerPath)

	ctx := context.Background()
	if err := VerifyFileOrSameSigner(ctx, targetPath, referencePath); err != nil {
		t.Fatalf("same self-signed publisher was rejected: %v", err)
	}
	if err := VerifyFileOrSameSigner(ctx, otherSignerPath, referencePath); err == nil {
		t.Fatal("different self-signed publisher was accepted")
	}
}

func findSignToolForTest(t *testing.T) string {
	t.Helper()
	var matches []string
	for _, root := range []string{os.Getenv("ProgramFiles(x86)"), os.Getenv("ProgramFiles")} {
		if strings.TrimSpace(root) == "" {
			continue
		}
		found, err := filepath.Glob(filepath.Join(root, "Windows Kits", "10", "bin", "*", "x64", "signtool.exe"))
		if err != nil {
			t.Fatal(err)
		}
		matches = append(matches, found...)
	}
	if len(matches) == 0 {
		t.Skip("signtool.exe is unavailable")
	}
	sort.Strings(matches)
	return matches[len(matches)-1]
}

func createSelfSignedCodeSigningCertificateForTest(
	t *testing.T,
	root string,
	suffix string,
	password string,
) (string, string) {
	t.Helper()
	pfxPath := filepath.Join(root, "signer-"+suffix+".pfx")
	subject := fmt.Sprintf("CN=AgentDock Authenticode Test %s %d", suffix, os.Getpid())
	const script = `$ErrorActionPreference='Stop'
$cert = $null
try {
    $secure = ConvertTo-SecureString -String $env:AGENTDOCK_TEST_CERT_PASSWORD -AsPlainText -Force
    $cert = New-SelfSignedCertificate -Type CodeSigningCert -Subject $env:AGENTDOCK_TEST_CERT_SUBJECT -CertStoreLocation 'Cert:\CurrentUser\My' -KeyExportPolicy Exportable -NotAfter (Get-Date).AddHours(1)
    Export-PfxCertificate -Cert $cert -FilePath $env:AGENTDOCK_TEST_CERT_PFX -Password $secure | Out-Null
    [Console]::Out.Write($cert.Thumbprint)
} catch {
    if ($cert) {
        Remove-Item -LiteralPath ('Cert:\CurrentUser\My\' + $cert.Thumbprint) -Force -ErrorAction SilentlyContinue
    }
    throw
}`
	command := exec.Command(
		"powershell.exe",
		"-NoLogo",
		"-NoProfile",
		"-NonInteractive",
		"-ExecutionPolicy",
		"Bypass",
		"-Command",
		script,
	)
	command.Env = append(
		os.Environ(),
		"AGENTDOCK_TEST_CERT_PFX="+pfxPath,
		"AGENTDOCK_TEST_CERT_PASSWORD="+password,
		"AGENTDOCK_TEST_CERT_SUBJECT="+subject,
	)
	command.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("create self-signed Authenticode certificate: %v: %s", err, strings.TrimSpace(string(output)))
	}
	thumbprint := strings.TrimSpace(string(output))
	if thumbprint == "" {
		t.Fatal("self-signed Authenticode certificate thumbprint is empty")
	}
	return pfxPath, thumbprint
}

func removeTestCertificate(thumbprint string) {
	if strings.TrimSpace(thumbprint) == "" {
		return
	}
	const script = `Remove-Item -LiteralPath ('Cert:\CurrentUser\My\' + $env:AGENTDOCK_TEST_CERT_THUMBPRINT) -Force -ErrorAction SilentlyContinue`
	command := exec.Command(
		"powershell.exe",
		"-NoLogo",
		"-NoProfile",
		"-NonInteractive",
		"-ExecutionPolicy",
		"Bypass",
		"-Command",
		script,
	)
	command.Env = append(os.Environ(), "AGENTDOCK_TEST_CERT_THUMBPRINT="+thumbprint)
	command.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	_ = command.Run()
}

func copyFileForTest(t *testing.T, source, target string) {
	t.Helper()
	data, err := os.ReadFile(source)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, data, 0o700); err != nil {
		t.Fatal(err)
	}
}

func signFileForTest(t *testing.T, signTool, pfxPath, password, path string) {
	t.Helper()
	command := exec.Command(signTool, "sign", "/fd", "SHA256", "/f", pfxPath, "/p", password, path)
	command.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("sign test executable: %v: %s", err, strings.TrimSpace(string(output)))
	}
}
