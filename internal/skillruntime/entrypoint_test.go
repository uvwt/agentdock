package skillruntime

import (
	"strings"
	"testing"
)

func TestPowerShellEntrypointEnablesUTF8AndQuotesPath(t *testing.T) {
	manifest := Manifest{Spec: Spec{Runtime: RuntimePowerShell}}
	command, err := entrypointCommand(manifest, `C:\Skill's Folder\run.ps1`)
	if err != nil {
		// Non-Windows development machines may not have pwsh. Candidate resolution is
		// already exercised by Windows CI, so only assert command details when present.
		t.Skipf("PowerShell unavailable: %v", err)
	}
	joined := strings.Join(command.Args, " ")
	for _, want := range []string{"-Command", "OutputEncoding", `C:\Skill''s Folder\run.ps1`} {
		if !strings.Contains(joined, want) {
			t.Fatalf("PowerShell command %q missing %q", joined, want)
		}
	}
}

func TestPythonRuntimeEnvironmentForcesUTF8(t *testing.T) {
	environment := applyRuntimeEnvironment(Manifest{Spec: Spec{Runtime: RuntimePython}}, []string{"PATH=/bin", "PYTHONUTF8=0"})
	joined := strings.Join(environment, "\n")
	if !strings.Contains(joined, "PYTHONUTF8=1") || !strings.Contains(joined, "PYTHONIOENCODING=utf-8") {
		t.Fatalf("environment = %q", joined)
	}
}
