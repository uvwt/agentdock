package skillruntime_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/uvwt/agentdock/internal/skillruntime"
	"github.com/uvwt/agentdock/internal/skillstate"
)

func TestInstallRunSchemaAndSecretRedaction(t *testing.T) {
	rt, source := newRuntimeWithPackage(t, "1.0.0", `#!/bin/sh
cat >/dev/null
printf '{"message":"ok","secret":"%s"}\n' "$API_TOKEN"
printf 'debug token=%s\n' "$API_TOKEN" >&2
`)
	secret := "highly-sensitive-token"
	t.Setenv("AGENTDOCK_TEST_SECRET", secret)
	binding := `default: local
bindings:
  local:
    secrets:
      API_TOKEN: env:AGENTDOCK_TEST_SECRET
    env:
      EXTRA_FLAG: enabled
`
	if err := os.WriteFile(rt.Bindings.Path("demo-skill"), []byte(binding), 0o600); err != nil {
		t.Fatal(err)
	}
	install, err := rt.Install(context.Background(), skillruntime.InstallRequest{Source: source, Activate: true, Channel: skillstate.ChannelStable})
	if err != nil {
		t.Fatal(err)
	}
	if !install.Activated || install.Digest == "" {
		t.Fatalf("unexpected install result: %#v", install)
	}
	result, err := rt.Run(context.Background(), skillruntime.RunRequest{
		RunID:     "run-1",
		Skill:     "demo-skill",
		Operation: "echo",
		Input:     json.RawMessage(`{"message":"hello"}`),
	})
	if err != nil {
		t.Fatalf("run failed: %v; result=%#v", err, result)
	}
	if !result.OK || result.ExitCode != 0 {
		t.Fatalf("unexpected run result: %#v", result)
	}
	combined := result.Stdout + result.Stderr + string(result.Output)
	if strings.Contains(combined, secret) {
		t.Fatalf("secret leaked in result: %s", combined)
	}
	if !strings.Contains(combined, "[REDACTED]") {
		t.Fatalf("result was not redacted: %s", combined)
	}

	_, err = rt.Run(context.Background(), skillruntime.RunRequest{
		Skill:     "demo-skill",
		Operation: "echo",
		Input:     json.RawMessage(`{"unexpected":true}`),
	})
	assertRuntimeCode(t, err, skillruntime.ErrInputInvalid)
}

func TestRunTimeoutKillsOperation(t *testing.T) {
	rt, source := newRuntimeWithPackage(t, "1.0.0", `#!/bin/sh
sleep 10
printf '{"message":"late","secret":"none"}\n'
`)
	if err := writeBinding(rt.Bindings.Path("demo-skill")); err != nil {
		t.Fatal(err)
	}
	if _, err := rt.Install(context.Background(), skillruntime.InstallRequest{Source: source, Activate: true}); err != nil {
		t.Fatal(err)
	}
	started := time.Now()
	result, err := rt.Run(context.Background(), skillruntime.RunRequest{
		Skill:     "demo-skill",
		Operation: "echo",
		Input:     json.RawMessage(`{"message":"hello"}`),
		Timeout:   100 * time.Millisecond,
	})
	assertRuntimeCode(t, err, skillruntime.ErrTimeout)
	if time.Since(started) > 3*time.Second {
		t.Fatalf("timed out operation was not killed promptly: %s", time.Since(started))
	}
	if result.OK {
		t.Fatalf("timed out result reports success: %#v", result)
	}
}

func TestDigestMismatchDoesNotInstall(t *testing.T) {
	rt, source := newRuntimeWithPackage(t, "1.0.0", `#!/bin/sh
printf '{"message":"ok","secret":"none"}\n'
`)
	_, err := rt.Install(context.Background(), skillruntime.InstallRequest{Source: source, DigestSHA256: strings.Repeat("0", 64), Activate: true})
	assertRuntimeCode(t, err, skillruntime.ErrDigestMismatch)
	installed, checkErr := rt.State.IsInstalled("demo-skill", "1.0.0")
	if checkErr != nil {
		t.Fatal(checkErr)
	}
	if installed {
		t.Fatal("digest mismatch installed package")
	}
}

func TestRollbackVerifiesAndRestoresOnFailure(t *testing.T) {
	root := t.TempDir()
	state, err := skillstate.New(filepath.Join(root, "skills"))
	if err != nil {
		t.Fatal(err)
	}
	bindings, err := skillruntime.NewBindingStore(filepath.Join(root, "skill-bindings"))
	if err != nil {
		t.Fatal(err)
	}
	rt, err := skillruntime.New(state, bindings)
	if err != nil {
		t.Fatal(err)
	}
	for _, version := range []string{"1.0.0", "2.0.0"} {
		source := createPackage(t, filepath.Join(root, "source-"+version), version, `#!/bin/sh
printf '{"message":"ok","secret":"none"}\n'
`)
		if _, err := rt.Install(context.Background(), skillruntime.InstallRequest{Source: source, Activate: true, Channel: skillstate.ChannelStable}); err != nil {
			t.Fatal(err)
		}
	}
	failedVerifier := skillruntime.VerifierFunc(func(context.Context, string, string, []string) ([]skillruntime.VerificationResult, error) {
		return []skillruntime.VerificationResult{{Name: "smoke", OK: false, Message: "failed"}}, nil
	})
	_, err = rt.Rollback(context.Background(), "demo-skill", skillstate.ChannelStable, failedVerifier)
	assertRuntimeCode(t, err, skillruntime.ErrRollbackVerify)
	active, err := state.ActiveVersion("demo-skill")
	if err != nil {
		t.Fatal(err)
	}
	if active != "2.0.0" {
		t.Fatalf("failed rollback did not restore version 2.0.0: %s", active)
	}
	successVerifier := skillruntime.VerifierFunc(func(context.Context, string, string, []string) ([]skillruntime.VerificationResult, error) {
		return []skillruntime.VerificationResult{{Name: "smoke", OK: true}}, nil
	})
	result, err := rt.Rollback(context.Background(), "demo-skill", skillstate.ChannelStable, successVerifier)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Verified || result.ToVersion != "1.0.0" {
		t.Fatalf("unexpected rollback result: %#v", result)
	}
}

func newRuntimeWithPackage(t *testing.T, version, script string) (*skillruntime.Runtime, string) {
	t.Helper()
	root := t.TempDir()
	state, err := skillstate.New(filepath.Join(root, "skills"))
	if err != nil {
		t.Fatal(err)
	}
	bindings, err := skillruntime.NewBindingStore(filepath.Join(root, "skill-bindings"))
	if err != nil {
		t.Fatal(err)
	}
	rt, err := skillruntime.New(state, bindings)
	if err != nil {
		t.Fatal(err)
	}
	return rt, createPackage(t, filepath.Join(root, "source"), version, script)
}

func createPackage(t *testing.T, dir, version, script string) string {
	t.Helper()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	manifest := `apiVersion: agentdock.dev/v1
kind: Skill
metadata:
  name: demo-skill
  version: ` + version + `
  displayName: Demo Skill
  description: Runtime integration test skill
spec:
  entrypoint: run.sh
  operations:
    - name: echo
      description: Echo validated JSON
      inputSchema: {"type":"object","required":["message"],"properties":{"message":{"type":"string"}},"additionalProperties":false}
      outputSchema: {"type":"object","required":["message","secret"],"properties":{"message":{"type":"string"},"secret":{"type":"string"}},"additionalProperties":false}
      timeoutSeconds: 5
  compatibility:
    platforms: [` + runtime.GOOS + `]
    architectures: [` + runtime.GOARCH + `]
    agentdock: ">=1.0.0"
  permissions:
    filesystem: []
    network: []
    secrets: [API_TOKEN]
    commands: [sh]
  bindings: [local]
  verification: [smoke]
`
	if err := os.WriteFile(filepath.Join(dir, "agentdock.yaml"), []byte(manifest), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "run.sh"), []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	return dir
}

func writeBinding(path string) error {
	return os.WriteFile(path, []byte(`default: local
bindings:
  local:
    secrets:
      API_TOKEN: harmless-test-value
`), 0o600)
}

func assertRuntimeCode(t *testing.T, err error, code string) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected runtime error %s", code)
	}
	var runtimeErr *skillruntime.Error
	if !errors.As(err, &runtimeErr) {
		t.Fatalf("error %T is not a runtime error: %v", err, err)
	}
	if runtimeErr.Code != code {
		t.Fatalf("error code = %s, want %s: %v", runtimeErr.Code, code, err)
	}
}
