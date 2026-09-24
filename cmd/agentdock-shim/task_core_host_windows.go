//go:build windows

package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	processctl "github.com/uvwt/agentdock/internal/process"
	"github.com/uvwt/agentdock/internal/startupdiag"
	"github.com/uvwt/agentdock/internal/updateengine"
)

// runTaskCoreHost 是 elevated Core 模式下长期运行的计划任务入口。
// 由稳定 GUI shim 直接拥有 generation Core，让 Task Scheduler 只绑定稳定进程边界；
// 安装、修复、更新和回滚期间都不会长期占用可替换的 versioned WPF 可执行文件。
func runTaskCoreHost(args []string) (int, error) {
	hostStartedAt := time.Now()
	flags := flag.NewFlagSet("task-core-host", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	runtimeRootFlag := flags.String("runtime-root", "", "AgentDock runtime root")
	if err := flags.Parse(args); err != nil {
		return 1, err
	}
	if flags.NArg() != 0 || strings.TrimSpace(*runtimeRootFlag) == "" {
		return 1, errors.New("task core host requires --runtime-root")
	}

	executable, err := os.Executable()
	if err != nil {
		return 1, fmt.Errorf("resolve AgentDock task host entry: %w", err)
	}
	executable, err = filepath.Abs(executable)
	if err != nil {
		return 1, fmt.Errorf("resolve AgentDock task host entry path: %w", err)
	}
	if !strings.EqualFold(filepath.Base(executable), updateengine.StableTrayShimName) {
		return 1, fmt.Errorf("task core host requires the stable GUI entry %s", updateengine.StableTrayShimName)
	}

	runtimeRoot, err := filepath.Abs(strings.TrimSpace(*runtimeRootFlag))
	if err != nil {
		return 1, fmt.Errorf("resolve task core host runtime root: %w", err)
	}
	stableRoot := filepath.Dir(filepath.Dir(executable))
	if !sameWindowsPath(runtimeRoot, stableRoot) {
		return 1, fmt.Errorf("task core host runtime root %s does not match stable entry root %s", runtimeRoot, stableRoot)
	}
	_ = startupdiag.Append(runtimeRoot, "task_core_host", "entry", hostStartedAt)

	resolveStartedAt := time.Now()
	store, err := updateengine.NewStore(runtimeRoot)
	if err != nil {
		return 1, err
	}
	layout, err := updateengine.NewWindowsLayout(runtimeRoot)
	if err != nil {
		return 1, err
	}
	active, err := resolveActiveWithRecovery(runtimeRoot, store, layout)
	if err != nil {
		return 1, err
	}
	coreBinary := layout.GenerationCore(active.ActiveVersion)
	if info, err := os.Stat(coreBinary); err != nil || info.IsDir() {
		if err == nil {
			err = errors.New("path is a directory")
		}
		return 1, fmt.Errorf("resolve active AgentDock Core %s: %w", coreBinary, err)
	}
	_ = startupdiag.Append(
		runtimeRoot,
		"task_core_host",
		"target_resolve",
		resolveStartedAt,
		slog.String("active_version", active.ActiveVersion),
	)

	command := exec.Command(coreBinary, "service", "launch-core", "--runtime-root", runtimeRoot)
	command.Dir = runtimeRoot
	processctl.Configure(command)
	nullFile, err := os.OpenFile(os.DevNull, os.O_RDWR, 0)
	if err != nil {
		return 1, fmt.Errorf("open null device for task core host: %w", err)
	}
	defer nullFile.Close()
	command.Stdin = nullFile
	command.Stdout = nullFile
	command.Stderr = nullFile

	spawnStartedAt := time.Now()
	if err := command.Start(); err != nil {
		_ = startupdiag.Append(runtimeRoot, "task_core_host", "core_spawn", spawnStartedAt, slog.String("result", "error"))
		return 1, fmt.Errorf("start active AgentDock Core from task host: %w", err)
	}
	_ = startupdiag.Append(
		runtimeRoot,
		"task_core_host",
		"core_spawn",
		spawnStartedAt,
		slog.String("result", "ok"),
		slog.Int("core_pid", command.Process.Pid),
		slog.String("active_version", active.ActiveVersion),
	)
	controller, err := processctl.Attach(command)
	if err != nil {
		_ = command.Process.Kill()
		_ = command.Wait()
		return 1, fmt.Errorf("attach AgentDock Core to task host Job Object: %w", err)
	}
	defer controller.Close()

	if err := command.Wait(); err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return exitErr.ExitCode(), nil
		}
		return 1, fmt.Errorf("wait for AgentDock Core task process: %w", err)
	}
	return 0, nil
}
