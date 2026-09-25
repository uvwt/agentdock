//go:build windows

package installer

import (
	"os"
	"strings"
	"testing"
)

func TestWindowsOptionalUninstallCommandsUseNoConsolePolicy(t *testing.T) {
	source, err := os.ReadFile("uninstall.go")
	if err != nil {
		t.Fatalf("read uninstall.go: %v", err)
	}
	text := string(source)
	anchor := "func runOptionalCmd(ctx context.Context, name string, args ...string) error"
	start := strings.Index(text, anchor)
	if start < 0 {
		t.Fatalf("uninstall.go missing %q", anchor)
	}
	end := start + 500
	if end > len(text) {
		end = len(text)
	}
	if !strings.Contains(text[start:end], "processcontrol.ConfigureBackground(cmd)") {
		t.Fatal("runOptionalCmd must apply the Windows no-console background process policy")
	}
}
