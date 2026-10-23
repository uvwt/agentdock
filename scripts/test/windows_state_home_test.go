package scripts

import (
	"context"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func TestWindowsStateHomeResolution(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows desktop state directory regression")
	}
	pwsh, err := exec.LookPath("pwsh")
	if err != nil {
		t.Skip("PowerShell 7 is required to compile the shared C# models")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	script := filepath.Join("..", "..", "desktop", "windows", "shared", "Tests", "StateHomeTests.ps1")
	output, err := exec.CommandContext(ctx, pwsh, "-NoProfile", "-File", script).CombinedOutput()
	if err != nil {
		t.Fatalf("state home behavior regression failed: %v\n%s", err, output)
	}
	t.Logf("%s", output)
}
