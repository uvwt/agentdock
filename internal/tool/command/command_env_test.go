package command

import (
	"strings"
	"testing"

	"github.com/uvwt/agentdock/internal/config"
	"github.com/uvwt/agentdock/internal/envstore"
)

func TestCommandEnvExplicitPathOverridesPlatformDefault(t *testing.T) {
	home := t.TempDir()
	envs, err := envstore.New(home)
	if err != nil {
		t.Fatal(err)
	}
	svc := New(
		func() config.Config { return config.Config{AgentDockHome: home, AgentDockDefaultDir: home} },
		nil,
		envs,
		nil,
		nil,
	)

	t.Setenv("PATH", "/usr/bin:/bin")
	got, err := svc.CommandEnv("", map[string]string{"PATH": "/custom/bin"})
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range got {
		if strings.HasPrefix(entry, "PATH=") {
			if entry != "PATH=/custom/bin" {
				t.Fatalf("PATH override = %q, want %q", entry, "PATH=/custom/bin")
			}
			return
		}
	}
	t.Fatal("command environment missing PATH")
}
