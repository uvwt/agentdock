//go:build !windows

package command

import "testing"

func TestSetPlatformCommandEnvPreservesCaseOutsideWindows(t *testing.T) {
	env := map[string]string{"PATH": "base"}
	setPlatformCommandEnv(env, "Path", "explicit")

	if env["PATH"] != "base" {
		t.Fatalf("PATH = %q, want %q", env["PATH"], "base")
	}
	if env["Path"] != "explicit" {
		t.Fatalf("Path = %q, want %q", env["Path"], "explicit")
	}
}
