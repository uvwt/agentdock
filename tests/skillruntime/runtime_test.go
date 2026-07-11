//go:build !windows

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

func TestInstallRequiresNoEnvConfirmation(t *testing.T) {
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
	source := createPackageWithSecrets(t, filepath.Join(root, "source"), "no-env-skill", "1.0.0", nil, `#!/bin/sh
printf '{}\n'
`)

	_, err = rt.Install(context.Background(), skillruntime.InstallRequest{Source: source, Activate: true})
	assertRuntimeCode(t, err, skillruntime.ErrManifestInvalid)
	var runtimeErr *skillruntime.Error
	if !errors.As(err, &runtimeErr) || runtimeErr.Stage != "manifest.env" {
		t.Fatalf("error stage = %#v, want manifest.env: %v", runtimeErr, err)
	}
	installed, checkErr := rt.State.IsInstalled("no-env-skill", "1.0.0")
	if checkErr != nil {
		t.Fatal(checkErr)
	}
	if installed {
		t.Fatal("no-env package installed without confirmation")
	}

	if _, err := rt.Install(context.Background(), skillruntime.InstallRequest{Source: source, Activate: true, ConfirmedNoEnv: true}); err != nil {
		t.Fatal(err)
	}
}

func TestInstallDoesNotInferLegacyCompatEnvDefinitions(t *testing.T) {
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
	source := createPackageWithSecrets(t, filepath.Join(root, "source"), "baidu-netdisk", "1.0.0", nil, `#!/bin/sh
printf '{}\n'
`)

	_, err = rt.Install(context.Background(), skillruntime.InstallRequest{Source: source, Activate: true})
	assertRuntimeCode(t, err, skillruntime.ErrManifestInvalid)
	var runtimeErr *skillruntime.Error
	if !errors.As(err, &runtimeErr) || runtimeErr.Stage != "manifest.env" {
		t.Fatalf("error stage = %#v, want manifest.env: %v", runtimeErr, err)
	}
}

func TestInstallAndRunUsesManifestEnvDefinitions(t *testing.T) {
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
	rt.EnvProvider = manifestEnvProvider{}
	source := createPackageWithEnv(t, filepath.Join(root, "source"), `#!/bin/sh
printf '{"message":"%s","secret":"%s"}\n' "$DEMO_BASE_URL" "$DEMO_API_TOKEN"
`)

	install, err := rt.Install(context.Background(), skillruntime.InstallRequest{Source: source, Activate: true, Channel: skillstate.ChannelStable})
	if err != nil {
		t.Fatal(err)
	}
	if !install.Activated {
		t.Fatalf("manifest env package was not activated: %#v", install)
	}
	packageDir, err := rt.State.InstalledPath("manifest-env-skill", "1.0.0")
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := skillruntime.LoadManifest(packageDir)
	if err != nil {
		t.Fatal(err)
	}
	definitions := skillruntime.EnvDefinitionsForManifest(manifest)
	byName := map[string]skillruntime.EnvDefinition{}
	for _, def := range definitions {
		byName[def.Name] = def
	}
	for name, kind := range map[string]string{"DEMO_BASE_URL": "plain", "DEMO_API_TOKEN": "secret"} {
		def, ok := byName[name]
		if !ok {
			t.Fatalf("missing manifest env definition %s", name)
		}
		if def.Kind != kind || def.Source != "manifest" {
			t.Fatalf("%s definition = %#v, want kind=%s source=manifest", name, def, kind)
		}
	}

	result, err := rt.Run(context.Background(), skillruntime.RunRequest{
		Skill:     "manifest-env-skill",
		Operation: "echo",
		Input:     json.RawMessage(`{"message":"hello"}`),
	})
	if err != nil {
		t.Fatalf("run failed: %v; result=%#v", err, result)
	}
	if string(result.Output) != `{"message":"https://example.test","secret":"[REDACTED]"}` {
		t.Fatalf("unexpected redacted output: %s", result.Output)
	}
	combined := result.Stdout + result.Stderr + string(result.Output)
	if strings.Contains(combined, "manifest-secret-value") {
		t.Fatalf("manifest env secret leaked: %s", combined)
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

type manifestEnvProvider struct{}

func (manifestEnvProvider) EnvForSkill(skill string, definitions []skillruntime.EnvDefinition) (map[string]string, []string, error) {
	env := map[string]string{}
	secrets := []string{}
	for _, def := range definitions {
		if def.Skill != skill {
			continue
		}
		switch def.Name {
		case "DEMO_BASE_URL":
			env[def.Name] = "https://example.test"
		case "DEMO_API_TOKEN":
			env[def.Name] = "manifest-secret-value"
			secrets = append(secrets, "manifest-secret-value")
		}
	}
	return env, secrets, nil
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
	return createPackageWithSecrets(t, dir, "demo-skill", version, []string{"API_TOKEN"}, script)
}

func writeSkillDoc(t *testing.T, dir, name string) {
	t.Helper()
	doc := `---
name: ` + name + `
description: Use this test skill in runtime integration tests.
---

# Test Skill

This package exists to verify Skill Runtime install, run, validation, and rollback behavior.
`
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(doc), 0o600); err != nil {
		t.Fatal(err)
	}
}

func createPackageWithSecrets(t *testing.T, dir, name, version string, secrets []string, script string) string {
	t.Helper()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	envList := "[]"
	if len(secrets) > 0 {
		var b strings.Builder
		b.WriteString("\n")
		for _, name := range secrets {
			b.WriteString("      - name: ")
			b.WriteString(name)
			b.WriteString("\n        kind: secret\n")
		}
		envList = b.String()
	}
	manifest := `apiVersion: agentdock.dev/v1
kind: Skill
metadata:
  name: ` + name + `
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
    env: ` + envList + `
    commands: [sh]
  bindings: [local]
  verification: [smoke]
`
	if err := os.WriteFile(filepath.Join(dir, "agentdock.yaml"), []byte(manifest), 0o600); err != nil {
		t.Fatal(err)
	}
	writeSkillDoc(t, dir, name)
	if err := os.WriteFile(filepath.Join(dir, "run.sh"), []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	return dir
}

func createPackageWithEnv(t *testing.T, dir, script string) string {
	t.Helper()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	manifest := `apiVersion: agentdock.dev/v1
kind: Skill
metadata:
  name: manifest-env-skill
  version: 1.0.0
  displayName: Manifest Env Skill
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
    env:
      - name: DEMO_BASE_URL
        kind: plain
      - name: DEMO_API_TOKEN
        kind: secret
    commands: [sh]
  verification: [smoke]
`
	if err := os.WriteFile(filepath.Join(dir, "agentdock.yaml"), []byte(manifest), 0o600); err != nil {
		t.Fatal(err)
	}
	writeSkillDoc(t, dir, "manifest-env-skill")
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
