//go:build windows

package desktopruntime

import (
	"os"
	"strings"
	"testing"
)

func TestWindowsDesktopRuntimeBackgroundCommandsUseNoConsoleConfigure(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		file   string
		anchor string
	}{
		{
			name:   "scheduled task query",
			file:   "service_startup_windows.go",
			anchor: "exec.CommandContext(ctx, \"schtasks.exe\", \"/Query\"",
		},
		{
			name:   "scheduled task mutation",
			file:   "service_startup_windows.go",
			anchor: "func runScheduledTaskCommand(ctx context.Context, args ...string) error",
		},
		{
			name:   "core detached launch",
			file:   "service_windows.go",
			anchor: "exec.Command(coreBinary, \"service\", \"launch-core\", \"--runtime-root\", runtimeRoot)",
		},
		{
			name:   "tunnel supervisor launch",
			file:   "tunnel_windows.go",
			anchor: "exec.Command(supervisorBinary, \"tunnel\", \"launch\", \"--runtime-root\", runtime.root)",
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			assertDesktopRuntimeCommandConfiguredNearby(t, testCase.file, testCase.anchor)
		})
	}
}

func assertDesktopRuntimeCommandConfiguredNearby(t *testing.T, fileName, anchor string) {
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
