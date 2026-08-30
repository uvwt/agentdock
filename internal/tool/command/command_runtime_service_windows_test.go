//go:build windows

package command

import "testing"

func TestResolveWSLWorkdirAcceptsWindowsAndLinuxPaths(t *testing.T) {
	service, _ := newCommandTestService(t)

	windowsPath, err := service.resolveWSLWorkdir(map[string]any{"workdir": `D:\Project\synapse`}, "")
	if err != nil {
		t.Fatalf("resolve Windows path: %v", err)
	}
	if windowsPath != "/mnt/d/Project/synapse" {
		t.Fatalf("Windows path resolved to %q", windowsPath)
	}

	extendedPath, err := service.resolveWSLWorkdir(map[string]any{"workdir": `\\?\E:\Work`}, "")
	if err != nil {
		t.Fatalf("resolve extended Windows path: %v", err)
	}
	if extendedPath != "/mnt/e/Work" {
		t.Fatalf("extended Windows path resolved to %q", extendedPath)
	}

	linuxPath, err := service.resolveWSLWorkdir(map[string]any{"workdir": "/home/a/project"}, "")
	if err != nil {
		t.Fatalf("resolve Linux path: %v", err)
	}
	if linuxPath != "/home/a/project" {
		t.Fatalf("Linux path resolved to %q", linuxPath)
	}

	if _, err := service.resolveWSLWorkdir(map[string]any{"workdir": `\\server\share`}, ""); err == nil {
		t.Fatal("expected UNC path to be rejected")
	}
}

func TestPrepareCommandInvocationRejectsWSLDistributionForWindowsRuntime(t *testing.T) {
	service, _ := newCommandTestService(t)
	_, err := service.prepareCommandInvocation(map[string]any{
		"runtime":          "windows",
		"wsl_distribution": "Ubuntu",
	}, "Write-Output ok")
	if err == nil {
		t.Fatal("expected wsl_distribution validation error")
	}
}
