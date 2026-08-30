//go:build windows

package command

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/uvwt/agentdock/internal/config"
	"github.com/uvwt/agentdock/internal/envstore"
)

func newWindowsCommandEnvTestService(t *testing.T) *Service {
	t.Helper()
	home := t.TempDir()
	envs, err := envstore.New(home)
	if err != nil {
		t.Fatal(err)
	}
	return New(
		func() config.Config { return config.Config{AgentDockHome: home, AgentDockDefaultDir: home} },
		nil,
		envs,
		nil,
		nil,
	)
}

func windowsCommandEnvValues(t *testing.T, svc *Service) map[string]string {
	t.Helper()
	got, err := svc.CommandEnv("", nil)
	if err != nil {
		t.Fatal(err)
	}
	values := map[string]string{}
	for _, entry := range got {
		key, value, ok := strings.Cut(entry, "=")
		if ok {
			values[strings.ToUpper(key)] = value
		}
	}
	return values
}

func TestWindowsCommandEnvPlatformKeyContract(t *testing.T) {
	want := []string{
		"SYSTEMROOT",
		"WINDIR",
		"SYSTEMDRIVE",
		"COMSPEC",
		"PATHEXT",
		"WSLENV",
		"USERPROFILE",
		"HOMEDRIVE",
		"HOMEPATH",
		"APPDATA",
		"LOCALAPPDATA",
		"PROGRAMDATA",
		"PROGRAMFILES",
		"PROGRAMFILES(X86)",
		"PROGRAMW6432",
		"ALLUSERSPROFILE",
		"PROCESSOR_ARCHITECTURE",
		"PROCESSOR_ARCHITEW6432",
		"NUMBER_OF_PROCESSORS",
		"USERNAME",
		"OS",
	}
	if got := platformCommandEnvKeys(); !reflect.DeepEqual(got, want) {
		t.Fatalf("platformCommandEnvKeys() = %#v, want %#v", got, want)
	}
}

func TestCommandEnvPreservesWindowsPlatformBaseline(t *testing.T) {
	svc := newWindowsCommandEnvTestService(t)
	for _, key := range platformCommandEnvKeys() {
		t.Setenv(key, "sentinel-"+key)
	}

	values := windowsCommandEnvValues(t, svc)
	for _, key := range platformCommandEnvKeys() {
		want := os.Getenv(key)
		if want == "" {
			if _, ok := values[key]; ok {
				t.Fatalf("%s should be absent when the host value is empty", key)
			}
			continue
		}
		if values[key] != want {
			t.Fatalf("%s = %q, want current host value %q", key, values[key], want)
		}
	}
}

func TestCommandEnvDoesNotInheritCodeInjectionVariables(t *testing.T) {
	svc := newWindowsCommandEnvTestService(t)
	blocked := []string{
		"PYTHONPATH",
		"PYTHONHOME",
		"NODE_OPTIONS",
		"NODE_PATH",
		"PSMODULEPATH",
		"CLASSPATH",
		"JAVA_TOOL_OPTIONS",
	}
	for _, key := range blocked {
		t.Setenv(key, "must-not-inherit")
	}

	values := windowsCommandEnvValues(t, svc)
	for _, key := range blocked {
		if _, ok := values[key]; ok {
			t.Fatalf("host code-injection variable %s leaked into command environment", key)
		}
	}
}

func TestCommandEnvOverridesWindowsKeysCaseInsensitively(t *testing.T) {
	svc := newWindowsCommandEnvTestService(t)
	t.Setenv("PATH", `C:\host-bin`)

	got, err := svc.CommandEnv("", map[string]string{"Path": `C:\explicit-bin`})
	if err != nil {
		t.Fatal(err)
	}
	values := map[string][]string{}
	for _, entry := range got {
		key, value, ok := strings.Cut(entry, "=")
		if ok {
			upper := strings.ToUpper(key)
			values[upper] = append(values[upper], value)
		}
	}
	if paths := values["PATH"]; !reflect.DeepEqual(paths, []string{`C:\explicit-bin`}) {
		t.Fatalf("PATH entries = %#v, want one explicit override", paths)
	}
}

func TestCommandEnvOverridesExplicitEnvWinsCaseInsensitively(t *testing.T) {
	svc := newWindowsCommandEnvTestService(t)
	scope := envstore.Scope{Kind: envstore.ScopeSkill, Name: "mixed-case-env"}
	if err := svc.envs.Set(scope, "Path", `C:\skill-bin`); err != nil {
		t.Fatal(err)
	}

	overrides, err := svc.commandEnvOverrides("mixed-case-env", map[string]string{"PATH": `C:\explicit-bin`})
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"PATH": `C:\explicit-bin`}
	if !reflect.DeepEqual(overrides, want) {
		t.Fatalf("commandEnvOverrides() = %#v, want %#v", overrides, want)
	}
}

func TestCommandEnvRepairsWindowsSystemDriveAndProgramData(t *testing.T) {
	svc := newWindowsCommandEnvTestService(t)
	t.Setenv("SYSTEMDRIVE", "")
	t.Setenv("PROGRAMDATA", "")
	t.Setenv("SYSTEMROOT", `Q:\Windows`)
	t.Setenv("WINDIR", "")

	values := windowsCommandEnvValues(t, svc)
	if values["SYSTEMDRIVE"] != "Q:" {
		t.Fatalf("SYSTEMDRIVE = %q, want %q", values["SYSTEMDRIVE"], "Q:")
	}
	if values["PROGRAMDATA"] != `Q:\ProgramData` {
		t.Fatalf("PROGRAMDATA = %q, want %q", values["PROGRAMDATA"], `Q:\ProgramData`)
	}
}

func TestCommandEnvExpandsSystemDriveInCmd(t *testing.T) {
	svc := newWindowsCommandEnvTestService(t)
	values := windowsCommandEnvValues(t, svc)
	if values["SYSTEMDRIVE"] == "" {
		t.Fatal("command environment missing SYSTEMDRIVE")
	}
	cmdPath, err := exec.LookPath("cmd.exe")
	if err != nil {
		t.Fatalf("find cmd.exe: %v", err)
	}

	cmd := exec.Command(cmdPath, "/d", "/s", "/c", "echo %SystemDrive%")
	cmd.Dir = t.TempDir()
	commandEnv, err := svc.CommandEnv("", nil)
	if err != nil {
		t.Fatal(err)
	}
	cmd.Env = commandEnv
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("run cmd.exe: %v\n%s", err, output)
	}
	if got := strings.TrimSpace(string(output)); got != values["SYSTEMDRIVE"] {
		t.Fatalf("cmd.exe expanded SystemDrive to %q, want %q", got, values["SYSTEMDRIVE"])
	}
}

func TestCommandEnvUsesAgentDockTempOnWindows(t *testing.T) {
	svc := newWindowsCommandEnvTestService(t)
	t.Setenv("TEMP", `C:\host-temp`)
	t.Setenv("TMP", `C:\host-tmp`)

	values := windowsCommandEnvValues(t, svc)
	want := filepath.Join(svc.config().AgentDockHome, "tmp")
	for _, key := range []string{"TEMP", "TMP", "TMPDIR"} {
		if values[key] != want {
			t.Fatalf("%s = %q, want %q", key, values[key], want)
		}
	}
}
