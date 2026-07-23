//go:build !windows

package app

import "testing"

func TestNonWindowsExecCommandRejectsRuntimeOverride(t *testing.T) {
	runtime, _ := newCodeToolsRuntime(t)
	_, err := runtime.prepareCommandInvocation(map[string]any{"runtime": "wsl"}, "pwd")
	if err == nil {
		t.Fatal("expected non-Windows runtime override to be rejected")
	}
}
