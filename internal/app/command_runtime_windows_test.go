//go:build windows

package app

import "testing"

func TestWindowsExecCommandSchemaExposesWSLRuntime(t *testing.T) {
	properties := InputSchema("exec_command")["properties"].(map[string]any)
	runtimeProperty, ok := properties["runtime"].(map[string]any)
	if !ok {
		t.Fatalf("exec_command schema is missing runtime: %#v", properties)
	}
	enum, ok := runtimeProperty["enum"].([]string)
	if !ok || len(enum) != 2 || enum[0] != "windows" || enum[1] != "wsl" {
		t.Fatalf("runtime enum = %#v", runtimeProperty["enum"])
	}
	if _, ok := properties["wsl_distribution"]; !ok {
		t.Fatalf("exec_command schema is missing wsl_distribution: %#v", properties)
	}
}

func TestResolveWSLWorkdirAcceptsWindowsAndLinuxPaths(t *testing.T) {
	runtime, _ := newCodeToolsRuntime(t)

	windowsPath, err := runtime.resolveWSLWorkdir(map[string]any{"workdir": `D:\Project\synapse`}, "")
	if err != nil {
		t.Fatalf("resolve Windows path: %v", err)
	}
	if windowsPath != "/mnt/d/Project/synapse" {
		t.Fatalf("Windows path resolved to %q", windowsPath)
	}

	extendedPath, err := runtime.resolveWSLWorkdir(map[string]any{"workdir": `\\?\E:\Work`}, "")
	if err != nil {
		t.Fatalf("resolve extended Windows path: %v", err)
	}
	if extendedPath != "/mnt/e/Work" {
		t.Fatalf("extended Windows path resolved to %q", extendedPath)
	}

	linuxPath, err := runtime.resolveWSLWorkdir(map[string]any{"workdir": "/home/a/project"}, "")
	if err != nil {
		t.Fatalf("resolve Linux path: %v", err)
	}
	if linuxPath != "/home/a/project" {
		t.Fatalf("Linux path resolved to %q", linuxPath)
	}

	if _, err := runtime.resolveWSLWorkdir(map[string]any{"workdir": `\\server\share`}, ""); err == nil {
		t.Fatal("expected UNC path to be rejected")
	}
}

func TestPrepareCommandInvocationRejectsWSLDistributionForWindowsRuntime(t *testing.T) {
	runtime, _ := newCodeToolsRuntime(t)
	_, err := runtime.prepareCommandInvocation(map[string]any{
		"runtime":          "windows",
		"wsl_distribution": "Ubuntu",
	}, "Write-Output ok")
	if err == nil {
		t.Fatal("expected wsl_distribution validation error")
	}
}
