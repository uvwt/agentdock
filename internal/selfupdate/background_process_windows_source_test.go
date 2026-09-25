//go:build windows

package selfupdate

import (
	"os"
	"strings"
	"testing"
)

func TestWindowsSelfUpdateBackgroundCommandsUseNoConsoleConfigure(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		file   string
		anchor string
	}{
		{
			name:   "update finalize helper",
			file:   "apply_windows.go",
			anchor: "exec.Command(helperPath, \"__update-finalize\", planPath)",
		},
		{
			name:   "selfupdate console command",
			file:   "apply_windows.go",
			anchor: "func runWindowsCommand(ctx context.Context, name string, args ...string) error",
		},
		{
			name:   "legacy migration helper",
			file:   "legacy_migration_windows.go",
			anchor: "exec.Command(helperPath, windowsLegacyMigrationCommand, planPath)",
		},
		{
			name:   "legacy migration core restart",
			file:   "legacy_migration_windows.go",
			anchor: "exec.CommandContext(ctx, corePath, \"service\", \"start\", \"--runtime-root\", plan.RuntimeRoot)",
		},
		{
			name:   "generation arbiter",
			file:   "generation_windows.go",
			anchor: "exec.CommandContext(ctx, sourceArbiter, \"--root\", root, \"--transaction-id\", transaction.TransactionID)",
		},
		{
			name:   "launcher powershell",
			file:   "apply_windows.go",
			anchor: "exec.Command(\"powershell.exe\", \"-NoLogo\", \"-NoProfile\", \"-NonInteractive\", \"-WindowStyle\", \"Hidden\", \"-ExecutionPolicy\", \"Bypass\", \"-File\", plan.LauncherPath)",
		},
		{
			name:   "cleanup powershell",
			file:   "apply_windows.go",
			anchor: "\"$path=$env:AGENTDOCK_UPDATE_CLEANUP_DIR;",
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			assertWindowsCommandConfiguredNearby(t, testCase.file, testCase.anchor)
		})
	}
}

func assertWindowsCommandConfiguredNearby(t *testing.T, fileName, anchor string) {
	t.Helper()

	source, err := os.ReadFile(fileName)
	if err != nil {
		t.Fatalf("读取 %s 失败: %v", fileName, err)
	}
	text := string(source)
	anchorIndex := strings.Index(text, anchor)
	if anchorIndex < 0 {
		t.Fatalf("%s 未找到启动点 %q", fileName, anchor)
	}

	const nearbyBytes = 700
	end := anchorIndex + nearbyBytes
	if end > len(text) {
		end = len(text)
	}
	if !strings.Contains(text[anchorIndex:end], "processcontrol.Configure(command)") {
		t.Fatalf("%s 的启动点 %q 未通过 processcontrol.Configure 收口", fileName, anchor)
	}
}
