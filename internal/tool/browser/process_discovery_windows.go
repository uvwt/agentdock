//go:build windows

package browser

import (
	"context"
	"os/exec"
	"strings"

	processcontrol "github.com/uvwt/agentdock/internal/process"
)

const browserProcessPowerShell = `$ErrorActionPreference='Stop'; Get-CimInstance Win32_Process | Where-Object { $_.Name -match '^(chrome|chromium|msedge)\.exe$' -and $_.CommandLine } | ForEach-Object { $_.CommandLine }`

func browserProcessCommandLines(ctx context.Context) ([]string, error) {
	command := exec.CommandContext(ctx, "powershell.exe", "-NoProfile", "-NonInteractive", "-Command", browserProcessPowerShell)
	processcontrol.Configure(command)
	output, err := command.Output()
	if err != nil {
		return nil, err
	}
	lines := strings.Split(strings.ReplaceAll(string(output), "\r\n", "\n"), "\n")
	result := make([]string, 0, len(lines))
	for _, line := range lines {
		if line = strings.TrimSpace(line); line != "" {
			result = append(result, line)
		}
	}
	return result, nil
}
