//go:build windows

package desktopruntime

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestResolveDesktopACPAdapterUsesNodeForNPMShim(t *testing.T) {
	testRoot := t.TempDir()
	npmBin := filepath.Join(testRoot, "npm")
	nodeBin := filepath.Join(testRoot, "node")
	runtimeRoot := filepath.Join(testRoot, "runtime")
	setIsolatedDesktopACPEnvironment(t, testRoot, npmBin, nodeBin)

	nodePath := writeTestFile(t, filepath.Join(nodeBin, "node.exe"), "node")
	writeTestFile(t, filepath.Join(npmBin, "codex-acp"), "#!/bin/sh\n")
	writeTestFile(t, filepath.Join(npmBin, "codex-acp.cmd"), "@echo off\r\n")
	packageRoot := filepath.Join(npmBin, "node_modules", "@agentclientprotocol", "codex-acp")
	writeTestFile(t, filepath.Join(packageRoot, "package.json"), `{"bin":{"codex-acp":"dist/index.js"}}`)
	entryPath := writeTestFile(t, filepath.Join(packageRoot, "dist", "index.js"), "console.log('acp')\n")

	adapter, err := resolveDesktopACPAdapter("codex", runtimeRoot, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if adapter.Command != nodePath {
		t.Fatalf("Command = %q, want %q", adapter.Command, nodePath)
	}
	if !reflect.DeepEqual(adapter.Args, []string{entryPath}) {
		t.Fatalf("Args = %#v, want %#v", adapter.Args, []string{entryPath})
	}
}

func TestResolveDesktopACPAdapterPreservesConfiguredNodeEntry(t *testing.T) {
	testRoot := t.TempDir()
	setIsolatedDesktopACPEnvironment(t, testRoot)

	nodePath := writeTestFile(t, filepath.Join(testRoot, "node.exe"), "node")
	entryPath := writeTestFile(t, filepath.Join(testRoot, "adapter", "index.js"), "console.log('acp')\n")
	configuredArgs := []string{entryPath, "--test"}

	adapter, err := resolveDesktopACPAdapter("codex", filepath.Join(testRoot, "runtime"), nodePath, configuredArgs)
	if err != nil {
		t.Fatal(err)
	}
	if adapter.Command != nodePath {
		t.Fatalf("Command = %q, want %q", adapter.Command, nodePath)
	}
	if !reflect.DeepEqual(adapter.Args, configuredArgs) {
		t.Fatalf("Args = %#v, want %#v", adapter.Args, configuredArgs)
	}
}

func TestResolveDesktopACPAdapterCustomUsesOnlyExplicitCommand(t *testing.T) {
	testRoot := t.TempDir()
	bin := filepath.Join(testRoot, "bin")
	setIsolatedDesktopACPEnvironment(t, testRoot, bin)

	adapterPath := writeTestFile(t, filepath.Join(bin, "custom-acp.exe"), "adapter")
	adapter, err := resolveDesktopACPAdapter("custom", filepath.Join(testRoot, "runtime"), adapterPath, []string{"--test"})
	if err != nil {
		t.Fatal(err)
	}
	if adapter.Command != adapterPath || !reflect.DeepEqual(adapter.Args, []string{"--test"}) {
		t.Fatalf("adapter = %#v, want command=%q args=[--test]", adapter, adapterPath)
	}

	if _, err := resolveDesktopACPAdapter("custom", filepath.Join(testRoot, "runtime"), "", nil); err == nil {
		t.Fatal("custom adapter unexpectedly auto-discovered an executable")
	}
	if _, err := resolveDesktopACPAdapter("custom", filepath.Join(testRoot, "runtime"), `bin\\custom-acp.exe`, nil); err == nil {
		t.Fatal("custom adapter accepted a relative command")
	}
}

func TestResolveDesktopACPAdapterRejectsNPMShimsWithoutNodePackage(t *testing.T) {
	testRoot := t.TempDir()
	npmBin := filepath.Join(testRoot, "npm")
	setIsolatedDesktopACPEnvironment(t, testRoot, npmBin)

	writeTestFile(t, filepath.Join(npmBin, "codex-acp"), "#!/bin/sh\n")
	writeTestFile(t, filepath.Join(npmBin, "codex-acp.cmd"), "@echo off\r\n")

	_, err := resolveDesktopACPAdapter("codex", filepath.Join(testRoot, "runtime"), "", nil)
	if err == nil {
		t.Fatal("expected npm shims without Node package entry to be rejected")
	}
	if !strings.Contains(err.Error(), "@agentclientprotocol/codex-acp") {
		t.Fatalf("error = %q, want npm package hint", err)
	}
}

func TestReadNPMBinEntryRejectsPathEscape(t *testing.T) {
	packageRoot := filepath.Join(t.TempDir(), "package")
	writeTestFile(t, filepath.Join(packageRoot, "package.json"), `{"bin":{"codex-acp":"../outside.js"}}`)
	writeTestFile(t, filepath.Join(filepath.Dir(packageRoot), "outside.js"), "console.log('outside')\n")

	if entry, ok := readNPMBinEntry(packageRoot, "codex-acp"); ok {
		t.Fatalf("readNPMBinEntry() = %q, true; want path escape rejection", entry)
	}
}

func setIsolatedDesktopACPEnvironment(t *testing.T, testRoot string, pathEntries ...string) {
	t.Helper()
	t.Setenv("PATH", strings.Join(pathEntries, string(os.PathListSeparator)))
	t.Setenv("PATHEXT", ".COM;.EXE;.BAT;.CMD")
	t.Setenv("APPDATA", filepath.Join(testRoot, "appdata"))
	t.Setenv("LOCALAPPDATA", filepath.Join(testRoot, "localappdata"))
	t.Setenv("USERPROFILE", filepath.Join(testRoot, "user"))
	t.Setenv("HOME", filepath.Join(testRoot, "user"))
}

func writeTestFile(t *testing.T, path, content string) string {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
		t.Fatal(err)
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		t.Fatal(err)
	}
	return filepath.Clean(absolute)
}
