package installer

import (
	"path/filepath"
	"testing"

	"github.com/uvwt/agentdock/internal/envstore"
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
		values, err := envstore.ParseFile(envFile)
		if err != nil {
			t.Fatal(err)
		}
		for key, want := range map[string]string{
			"AGENTDOCK_HOME":        home,
			"AGENTDOCK_DEFAULT_DIR": work,
		} {
			if got := values[key]; got != want {
				t.Errorf("Core service env %s = %q, want %q", key, got, want)
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
