//go:build windows

package installer

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/uvwt/agentdock/internal/desktopruntime"
	"golang.org/x/sys/windows"
)

func launchWindowsTunnelProxy(runtimeRoot string) error {
	manifest, err := desktopruntime.Load(filepath.Join(runtimeRoot, "runtime.json"))
	if err != nil {
		return fmt.Errorf("load Windows runtime for Tunnel startup: %w", err)
	}
	trayBinary := strings.TrimSpace(manifest.TrayBinary)
	if trayBinary == "" {
		return fmt.Errorf("Windows runtime manifest is missing tray_binary")
	}
	command := exec.Command(trayBinary, "--start-tunnel", "--runtime-root", runtimeRoot)
	command.Dir = runtimeRoot
	command.SysProcAttr = &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: windows.CREATE_NEW_PROCESS_GROUP | windows.DETACHED_PROCESS,
	}
	if err := command.Start(); err != nil {
		return fmt.Errorf("start Windows Tunnel proxy: %w", err)
	}
	if err := command.Process.Release(); err != nil {
		return fmt.Errorf("release Windows Tunnel proxy: %w", err)
	}
	return nil
}
