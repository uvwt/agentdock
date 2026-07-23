package command

import (
	"context"
	"reflect"
	"testing"
)

func TestBuildWSLCommandArgsKeepsCommandAsOneArgument(t *testing.T) {
	args := buildWSLCommandArgs(
		"Ubuntu",
		"/mnt/d/Project/synapse",
		`printf '%s' "$TOKEN"`,
	)
	want := []string{
		"--distribution", "Ubuntu",
		"--cd", "/mnt/d/Project/synapse",
		"--exec", "bash", "-lc", `printf '%s' "$TOKEN"`,
	}
	if !reflect.DeepEqual(args, want) {
		t.Fatalf("buildWSLCommandArgs() = %#v, want %#v", args, want)
	}
}

func TestBuildWSLCommandArgsUsesDefaultDistributionWithoutEmptyFlags(t *testing.T) {
	args := buildWSLCommandArgs("", "~", "pwd")
	want := []string{"--cd", "~", "--exec", "bash", "-lc", "pwd"}
	if !reflect.DeepEqual(args, want) {
		t.Fatalf("buildWSLCommandArgs() = %#v, want %#v", args, want)
	}
}

func TestBuildWSLProcessEnvForwardsValuesWithoutPuttingThemInArgs(t *testing.T) {
	env := buildWSLProcessEnv(
		[]string{"PATH=C:\\Windows\\System32", "WSLENV=EXISTING/p", "existing=old"},
		map[string]string{"TOKEN": "forwarded value", "EXISTING": "new"},
	)
	want := []string{
		"EXISTING=new",
		"PATH=C:\\Windows\\System32",
		"TOKEN=forwarded value",
		"WSLENV=EXISTING/p:TOKEN",
	}
	if !reflect.DeepEqual(env, want) {
		t.Fatalf("buildWSLProcessEnv() = %#v, want %#v", env, want)
	}
}

func TestBuildWSLProcessEnvHonorsExplicitWSLEnv(t *testing.T) {
	env := buildWSLProcessEnv(
		[]string{"CUSTOM=base", "WSLENV=OLD"},
		map[string]string{"WSLENV": "CUSTOM/u", "TOKEN": "forwarded"},
	)
	want := []string{"CUSTOM=base", "TOKEN=forwarded", "WSLENV=CUSTOM/u:TOKEN"}
	if !reflect.DeepEqual(env, want) {
		t.Fatalf("buildWSLProcessEnv() = %#v, want %#v", env, want)
	}
}

func TestWindowsPathToWSL(t *testing.T) {
	tests := []struct {
		name string
		path string
		want string
		ok   bool
	}{
		{name: "反斜杠盘符路径", path: `D:\Project\synapse`, want: "/mnt/d/Project/synapse", ok: true},
		{name: "正斜杠盘符路径", path: `C:/Users/a`, want: "/mnt/c/Users/a", ok: true},
		{name: "扩展盘符路径", path: `\\?\E:\Work`, want: "/mnt/e/Work", ok: true},
		{name: "盘符根目录", path: `F:\`, want: "/mnt/f", ok: true},
		{name: "Linux 路径", path: `/home/a/project`, ok: false},
		{name: "相对路径", path: `Project\synapse`, ok: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := windowsPathToWSL(tt.path)
			if got != tt.want || ok != tt.ok {
				t.Fatalf("windowsPathToWSL(%q) = (%q, %v), want (%q, %v)", tt.path, got, ok, tt.want, tt.ok)
			}
		})
	}
}

func TestWSLCommandFactoryPreservesHostProcessSettings(t *testing.T) {
	factory := newWSLCommandFactory(
		`C:\Windows\System32\wsl.exe`,
		[]string{"--exec", "bash", "-lc", "pwd"},
		[]string{"PATH=C:\\Windows\\System32"},
		`C:\Users\a\AgentDock`,
	)
	cmd := factory(context.Background())
	if cmd.Path != `C:\Windows\System32\wsl.exe` {
		t.Fatalf("cmd.Path = %q", cmd.Path)
	}
	if !reflect.DeepEqual(cmd.Args[1:], []string{"--exec", "bash", "-lc", "pwd"}) {
		t.Fatalf("cmd.Args = %#v", cmd.Args)
	}
	if !reflect.DeepEqual(cmd.Env, []string{"PATH=C:\\Windows\\System32"}) {
		t.Fatalf("cmd.Env = %#v", cmd.Env)
	}
	if cmd.Dir != `C:\Users\a\AgentDock` {
		t.Fatalf("cmd.Dir = %q", cmd.Dir)
	}
}
