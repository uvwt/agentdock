//go:build windows

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseWindowsPackageVersion(t *testing.T) {
	majorMinor, packageVersion, err := parseWindowsPackageVersion("2.1.3.0")
	if err != nil {
		t.Fatal(err)
	}
	if majorMinor != 0x00020001 {
		t.Fatalf("major/minor encoding = 0x%08X, want 0x00020001", majorMinor)
	}
	if packageVersion != 0x0002000100030000 {
		t.Fatalf("PACKAGE_VERSION encoding = 0x%016X, want 0x0002000100030000", packageVersion)
	}
}

func TestParseWindowsPackageVersionRejectsInvalidInput(t *testing.T) {
	for _, raw := range []string{"", "2.1.3", "0.1.3.0", "2.1.70000.0", "2.x.3.0"} {
		if _, _, err := parseWindowsPackageVersion(raw); err == nil {
			t.Fatalf("parseWindowsPackageVersion(%q) succeeded, want error", raw)
		}
	}
}

func TestWindowsAppRuntimeProbeRejectsUntrustedDLLBeforeLoad(t *testing.T) {
	path := filepath.Join(t.TempDir(), "Microsoft.WindowsAppRuntime.Bootstrap.dll")
	if err := os.WriteFile(path, []byte("not a signed DLL"), 0o600); err != nil {
		t.Fatal(err)
	}

	exitCode, err := runWindowsAppRuntimeProbe([]string{path, "2.1.3.0"})
	if exitCode != 2 {
		t.Fatalf("probe exit code = %d, want 2", exitCode)
	}
	if err == nil || !strings.Contains(err.Error(), "verify Windows App Runtime bootstrap DLL publisher") {
		t.Fatalf("probe error = %v, want publisher verification failure before DLL load", err)
	}
}
