//go:build windows

package command

import (
	"strings"
	"testing"

	"github.com/uvwt/agentdock/internal/config"
	"github.com/uvwt/agentdock/internal/envstore"
)

func TestCommandEnvPreservesWindowsProgramFilesVariables(t *testing.T) {
	home := t.TempDir()
	envs, err := envstore.New(home)
	if err != nil {
		t.Fatal(err)
	}
	svc := New(
		func() config.Config { return config.Config{AgentDockHome: home, AgentDockDefaultDir: home} },
		nil,
		envs,
		NewSessionStore(),
		nil,
		nil,
		nil,
	)

	t.Setenv("PROGRAMFILES", `C:\Program Files`)
	t.Setenv("PROGRAMW6432", `C:\Program Files`)
	got, err := svc.CommandEnv("", nil)
	if err != nil {
		t.Fatal(err)
	}
	values := map[string]string{}
	for _, entry := range got {
		key, value, ok := strings.Cut(entry, "=")
		if ok {
			values[key] = value
		}
	}
	for _, key := range []string{"PROGRAMFILES", "PROGRAMW6432"} {
		if values[key] != `C:\Program Files` {
			t.Fatalf("%s = %q, want %q", key, values[key], `C:\Program Files`)
		}
	}
}
