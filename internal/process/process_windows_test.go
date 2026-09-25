//go:build windows

package process

import (
	"os/exec"
	"syscall"
	"testing"
	"time"

	"golang.org/x/sys/windows"
)

func TestConfigureHidesConsoleWindow(t *testing.T) {
	cmd := exec.Command("cmd.exe")

	Configure(cmd)

	if cmd.SysProcAttr == nil {
		t.Fatal("Configure did not initialize Windows process attributes")
	}
	if !cmd.SysProcAttr.HideWindow {
		t.Fatal("Configure did not hide the child process window")
	}
	if cmd.SysProcAttr.CreationFlags&windows.CREATE_NO_WINDOW == 0 {
		t.Fatalf("creation flags = %#x, want CREATE_NO_WINDOW", cmd.SysProcAttr.CreationFlags)
	}
}

func TestConfigureBackgroundUsesNoConsoleOnWindows(t *testing.T) {
	cmd := exec.Command("cmd.exe")

	ConfigureBackground(cmd)

	if cmd.SysProcAttr == nil || !cmd.SysProcAttr.HideWindow {
		t.Fatal("ConfigureBackground did not hide the Windows child process")
	}
	if cmd.SysProcAttr.CreationFlags&windows.CREATE_NO_WINDOW == 0 {
		t.Fatalf("creation flags = %#x, want CREATE_NO_WINDOW", cmd.SysProcAttr.CreationFlags)
	}
}

func TestConfigurePreservesExistingCreationFlags(t *testing.T) {
	existingFlags := uint32(windows.CREATE_NEW_PROCESS_GROUP | windows.DETACHED_PROCESS)
	cmd := exec.Command("cmd.exe")
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: existingFlags}

	Configure(cmd)

	wantFlags := existingFlags | uint32(windows.CREATE_NO_WINDOW)
	if cmd.SysProcAttr.CreationFlags != wantFlags {
		t.Fatalf("creation flags = %#x, want %#x", cmd.SysProcAttr.CreationFlags, wantFlags)
	}
}

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

func TestWindowsJobObjectCloseTerminatesAttachedProcess(t *testing.T) {
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
	if err := controller.Close(); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case <-done:
		// JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE only guarantees termination. Windows may
		// report a zero process exit code, so the contract here is bounded exit, not status.
	case <-time.After(5 * time.Second):
		t.Fatal("closing Windows Job Object did not terminate the process")
	}
}
