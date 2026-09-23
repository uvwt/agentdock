package scripts

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMacOSUpdateRecoversCoreBeforeTransactionHandoff(t *testing.T) {
	root := filepath.Join("..", "..", "desktop", "macos", "AgentDockApp", "Sources")

	appDelegateData, err := os.ReadFile(filepath.Join(root, "AppDelegate.swift"))
	if err != nil {
		t.Fatalf("read AppDelegate.swift: %v", err)
	}
	appDelegate := string(appDelegateData)
	restoreIndex := strings.Index(appDelegate, "restoreBackgroundServiceRegistrationsForUpdate(")
	recoverIndex := strings.Index(appDelegate, "recoverBackgroundServicesAfterUpdate(")
	handoffIndex := strings.Index(appDelegate, "DesktopUpdateHandoff(")
	if restoreIndex < 0 || recoverIndex < 0 || handoffIndex < 0 {
		t.Fatalf("update recovery contract missing: restore=%d recover=%d handoff=%d", restoreIndex, recoverIndex, handoffIndex)
	}
	if !(restoreIndex < recoverIndex && recoverIndex < handoffIndex) {
		t.Fatalf("Core recovery must run after registration restore and before handoff: restore=%d recover=%d handoff=%d", restoreIndex, recoverIndex, handoffIndex)
	}
	if strings.Contains(appDelegate, "if !hasTransaction {\n                    warnings = await service.recoverBackgroundServicesAfterUpdate") {
		t.Fatal("transaction-aware updates must not bypass Core recovery before handoff")
	}

	serviceData, err := os.ReadFile(filepath.Join(root, "ServiceController.swift"))
	if err != nil {
		t.Fatalf("read ServiceController.swift: %v", err)
	}
	service := string(serviceData)
	start := strings.Index(service, "func recoverBackgroundServicesAfterUpdate(")
	if start < 0 {
		t.Fatal("recoverBackgroundServicesAfterUpdate not found")
	}
	end := strings.Index(service[start:], "func reregisterBackgroundServices(")
	if end < 0 {
		t.Fatal("recoverBackgroundServicesAfterUpdate terminator not found")
	}
	body := service[start : start+end]
	for _, want := range []string{
		"waitForHealth(configuration: configuration, timeout: 10)",
		"try await restart()",
		"warnings.append(error.localizedDescription)",
		"waitForTunnelProcess()",
		"try restartTunnel()",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("bounded background-service update recovery missing %q", want)
		}
	}
	if strings.Count(body, "try await restart()") != 1 {
		t.Fatal("Core update recovery must attempt at most one automatic re-registration")
	}
	if strings.Count(body, "try restartTunnel()") != 1 {
		t.Fatal("Tunnel update recovery must attempt at most one automatic re-registration")
	}
	for _, want := range []string{
		"func restartTunnel() throws",
		"func waitForTunnelProcess(timeout: TimeInterval = 10) async -> Bool",
		"func waitForStableLaunchdProcess(label: String, timeout: TimeInterval) -> Bool",
	} {
		if !strings.Contains(service, want) {
			t.Fatalf("Tunnel process recovery contract missing %q", want)
		}
	}
}
