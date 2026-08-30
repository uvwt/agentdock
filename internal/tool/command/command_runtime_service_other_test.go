//go:build !windows

package command

import "testing"

func TestNonWindowsExecCommandRejectsRuntimeOverride(t *testing.T) {
	service, _ := newCommandTestService(t)
	_, err := service.prepareCommandInvocation(map[string]any{"runtime": "wsl"}, "pwd")
	if err == nil {
		t.Fatal("expected non-Windows runtime override to be rejected")
	}
}
