package main

import (
	"context"
	"errors"
	"flag"
	"io"
	"strings"

	"github.com/uvwt/agentdock/internal/desktopruntime"
)

func runServiceCommand(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	if len(args) > 0 && args[0] == "launch-core" {
		flags := flag.NewFlagSet("agentdock service launch-core", flag.ContinueOnError)
		flags.SetOutput(stderr)
		runtimeRoot := flags.String("runtime-root", desktopruntime.DefaultRuntimeRoot(), "AgentDock 桌面运行目录")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		if flags.NArg() != 0 || strings.TrimSpace(*runtimeRoot) == "" {
			return errors.New("用法：agentdock service launch-core --runtime-root <目录>")
		}
		// launch-core 是桌面安装内部入口：先由原生代码恢复配置与 DPAPI 凭据，
		// 再在当前进程直接运行服务，避免 PowerShell 启动脚本长期参与运行链路。
		if err := desktopruntime.PrepareCoreEnvironment(*runtimeRoot); err != nil {
			return err
		}
		return runServer(ctx, nil, stderr)
	}
	return desktopruntime.RunServiceCommand(ctx, args, stdout, stderr)
}
