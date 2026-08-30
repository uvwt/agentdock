//go:build windows

package app

import "testing"

func TestWindowsExecCommandSchemaExposesWSLRuntime(t *testing.T) {
	properties := testInputSchema("exec_command")["properties"].(map[string]any)
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
