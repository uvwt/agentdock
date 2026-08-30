//go:build !windows

package file

import "testing"

func TestNonWindowsFileRuntimeRejectsWSLSelection(t *testing.T) {
	if _, err := selectFileRuntime(map[string]any{"runtime": "wsl"}); err == nil {
		t.Fatal("expected non-Windows file runtime override to be rejected")
	}
	if _, err := selectFileRuntime(map[string]any{"wsl_distribution": "Ubuntu"}); err == nil {
		t.Fatal("expected non-Windows WSL distribution to be rejected")
	}
}
