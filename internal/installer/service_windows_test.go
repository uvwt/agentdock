//go:build windows

package installer

import (
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestCloudflaredProcessRunningUsesWindowsPathLookup(t *testing.T) {
	comspec := os.Getenv("COMSPEC")
	if comspec == "" {
		t.Fatal("COMSPEC is empty")
	}
	probe := filepath.Join(t.TempDir(), "cloudflared-probe.exe")
	copyFileForTest(t, comspec, probe)

	cmd := exec.Command(probe, "/c", "ping -n 30 127.0.0.1 >nul")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_ = cmd.Process.Kill()
		_, _ = cmd.Process.Wait()
	}()

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if cloudflaredProcessRunningAtPath(context.Background(), probe) {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("expected Windows child process to be found by executable path: %s", probe)
}

func copyFileForTest(t *testing.T, source, target string) {
	t.Helper()
	in, err := os.Open(source)
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	out, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		t.Fatal(err)
	}
	if err := out.Close(); err != nil {
		t.Fatal(err)
	}
}
