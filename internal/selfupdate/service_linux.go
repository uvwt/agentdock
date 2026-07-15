//go:build linux

package selfupdate

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

const linuxServiceName = "agentdock"

type systemdService struct {
	name string
}

func (service systemdService) Name() string {
	return "systemd " + service.name
}

func (service systemdService) Restart(ctx context.Context) error {
	output, err := exec.CommandContext(ctx, "systemctl", "restart", service.name).CombinedOutput()
	if err != nil {
		return fmt.Errorf("systemctl restart 失败: %w: %s", err, strings.TrimSpace(string(output)))
	}
	return nil
}

func detectManagedService(ctx context.Context, targetPath string) managedService {
	loadState, err := exec.CommandContext(ctx, "systemctl", "show", "--property=LoadState", "--value", linuxServiceName).Output()
	if err != nil || strings.TrimSpace(string(loadState)) != "loaded" {
		return nil
	}
	execStart, err := exec.CommandContext(ctx, "systemctl", "show", "--property=ExecStart", "--value", linuxServiceName).Output()
	if err != nil {
		return nil
	}
	cleanTarget := filepath.Clean(targetPath)
	if !strings.Contains(string(execStart), cleanTarget) && cleanTarget != "/opt/agentdock/bin/agentdock" {
		return nil
	}
	return systemdService{name: linuxServiceName}
}

func platformHealthCandidates(ctx context.Context, _ string) []string {
	pidOutput, err := exec.CommandContext(ctx, "systemctl", "show", "--property=MainPID", "--value", linuxServiceName).Output()
	if err != nil {
		return nil
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(pidOutput)))
	if err != nil || pid <= 0 {
		return nil
	}
	environment, err := os.ReadFile(filepath.Join("/proc", strconv.Itoa(pid), "environ"))
	if err != nil {
		return nil
	}
	for _, entry := range strings.Split(string(environment), "\x00") {
		if !strings.HasPrefix(entry, "AGENTDOCK_PORT=") {
			continue
		}
		port, parseErr := strconv.Atoi(strings.TrimPrefix(entry, "AGENTDOCK_PORT="))
		if parseErr == nil && port > 0 && port <= 65535 {
			return []string{fmt.Sprintf("http://127.0.0.1:%d/healthz", port)}
		}
	}
	return nil
}

func signLocalReplacement(context.Context, string) error {
	return nil
}
