//go:build windows

package processcontrol

import (
	"os/exec"
	"testing"
	"time"
)

func TestWindowsJobObjectTerminatesAttachedProcess(t *testing.T) {
	cmd := exec.Command("powershell.exe", "-NoLogo", "-NoProfile", "-NonInteractive", "-Command", "Start-Sleep -Seconds 30")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	controller, err := Attach(cmd)
	if err != nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		t.Fatal(err)
	}
	if err := controller.Terminate(); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("terminated process exited successfully")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Windows Job Object did not terminate the process")
	}
	if err := controller.Close(); err != nil {
		t.Fatal(err)
	}
}
