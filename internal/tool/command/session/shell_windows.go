//go:build windows

package session

import (
	"context"
	"os/exec"
)

const powershellUTF8Prefix = "try { [Console]::InputEncoding=[System.Text.UTF8Encoding]::new($false); [Console]::OutputEncoding=[System.Text.UTF8Encoding]::new($false); $OutputEncoding=[System.Text.UTF8Encoding]::new($false) } catch {}\n"

func shellCommand(ctx context.Context, command string) *exec.Cmd {
	for _, name := range []string{"pwsh.exe", "powershell.exe"} {
		if path, err := exec.LookPath(name); err == nil {
			return exec.CommandContext(ctx, path, "-NoLogo", "-NoProfile", "-NonInteractive", "-Command", powershellUTF8Prefix+command)
		}
	}
	return exec.CommandContext(ctx, "cmd.exe", "/D", "/S", "/C", command)
}
