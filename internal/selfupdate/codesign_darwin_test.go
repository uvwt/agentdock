//go:build darwin

package selfupdate

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSignLocalDesktopReplacementUsesStableIdentityForWholeBundle(t *testing.T) {
	home := t.TempDir()
	fakeBin := filepath.Join(home, "bin")
	if err := os.MkdirAll(fakeBin, 0o700); err != nil {
		t.Fatal(err)
	}
	callsPath := filepath.Join(home, "codesign.calls")
	keychain := filepath.Join(home, "agentdock-codesign.keychain-db")
	if err := os.WriteFile(keychain, []byte("test"), 0o600); err != nil {
		t.Fatal(err)
	}

	securityScript := `#!/bin/sh
set -eu
if [ "${1:-}" = "find-identity" ]; then
  printf '  1) TESTHASH "test-identity"\n'
fi
`
	codesignScript := `#!/bin/sh
set -eu
printf '%s\n' "$*" >> "$SIGN_TEST_CODESIGN_CALLS"
if [ "${1:-}" = "-dv" ]; then
  last=""
  for arg in "$@"; do last="$arg"; done
  case "$last" in
    */AgentDockLoginHelper) identifier=com.uvwt.agentdock.login-helper ;;
    */agentdock) identifier=com.uvwt.agentdock.core ;;
    */cloudflared) identifier=com.uvwt.agentdock.cloudflared ;;
    *.app) identifier=com.uvwt.agentdock ;;
    *) exit 2 ;;
  esac
  printf 'Identifier=%s\n' "$identifier"
fi
`
	for name, content := range map[string]string{
		"security": securityScript,
		"codesign": codesignScript,
	} {
		path := filepath.Join(fakeBin, name)
		if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
			t.Fatal(err)
		}
	}

	t.Setenv("HOME", home)
	t.Setenv("PATH", fakeBin+":/usr/bin:/bin")
	t.Setenv("SIGN_TEST_CODESIGN_CALLS", callsPath)
	t.Setenv("AGENTDOCK_CODESIGN_IDENTITY", "test-identity")
	t.Setenv("AGENTDOCK_CODESIGN_KEYCHAIN", keychain)
	t.Setenv("AGENTDOCK_CODESIGN_KEYCHAIN_PASSWORD", "")
	t.Setenv("AGENTDOCK_CODESIGN_HOME", home)

	appPath := filepath.Join(home, "AgentDock.app")
	if err := signLocalDesktopReplacement(context.Background(), appPath); err != nil {
		t.Fatal(err)
	}
	calls, err := os.ReadFile(callsPath)
	if err != nil {
		t.Fatal(err)
	}

	var signCalls []string
	for _, line := range strings.Split(strings.TrimSpace(string(calls)), "\n") {
		if strings.HasPrefix(line, "--force ") {
			signCalls = append(signCalls, line)
		}
	}
	wantIdentifiers := []string{
		"com.uvwt.agentdock.login-helper",
		"com.uvwt.agentdock.core",
		"com.uvwt.agentdock.cloudflared",
		"com.uvwt.agentdock",
	}
	if len(signCalls) != len(wantIdentifiers) {
		t.Fatalf("stable sign calls = %d, want %d:\n%s", len(signCalls), len(wantIdentifiers), calls)
	}
	for index, identifier := range wantIdentifiers {
		call := signCalls[index]
		for _, required := range []string{
			"--keychain " + keychain,
			"--sign test-identity",
			"--timestamp=none",
			"--options runtime",
			"--identifier " + identifier,
		} {
			if !strings.Contains(call, required) {
				t.Fatalf("sign call %d missing %q: %s", index, required, call)
			}
		}
	}
	if !strings.Contains(string(calls), "--verify --deep --strict --verbose=2 "+appPath) {
		t.Fatalf("missing final deep verification:\n%s", calls)
	}
}

func TestSignLocalDesktopReplacementKeepsReleaseSignatureWithoutLocalIdentity(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("AGENTDOCK_CODESIGN_IDENTITY", "")
	t.Setenv("AGENTDOCK_CODESIGN_HOME", filepath.Join(home, "missing"))

	if err := signLocalDesktopReplacement(context.Background(), filepath.Join(home, "AgentDock.app")); err != nil {
		t.Fatalf("no local identity should leave the verified Release signature untouched: %v", err)
	}
}
