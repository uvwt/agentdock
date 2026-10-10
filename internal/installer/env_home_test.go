package installer

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 安装的服务环境必须使用请求中的状态目录，不依赖 service user 的默认 HOME。
func TestWriteCoreEnvironmentKeepsServiceDataDirectories(t *testing.T) {
	envFile := filepath.Join(t.TempDir(), "agentdock.env")
	home := filepath.Join(t.TempDir(), "custom-state")
	work := filepath.Join(t.TempDir(), "custom-work")

	if err := writeCoreEnvironment(envFile, Request{
		AgentDockHome:       home,
		AgentDockDefaultDir: work,
	}); err != nil {
		t.Fatal(err)
	}
	check := func() {
		t.Helper()
		data, err := os.ReadFile(envFile)
		if err != nil {
			t.Fatal(err)
		}
		for _, want := range []string{
			"AGENTDOCK_HOME=" + home,
			"AGENTDOCK_DEFAULT_DIR=" + work,
		} {
			if !strings.Contains(string(data), want+"\n") {
				t.Fatalf("Core service env missing %q: %s", want, data)
			}
		}
	}
	check()

	// 不传目录的 repair 不允许将其重置为服务用户的默认家目录。
	if err := writeCoreEnvironment(envFile, Request{}); err != nil {
		t.Fatal(err)
	}
	check()
}
