package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
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
		// launch-core 是桌面安装内部入口：先接管受限轮转日志，再恢复配置与 DPAPI 凭据，
		// 最后在当前进程直接运行服务，避免平台启动器长期参与日志写入和运行链路。
		logOutput, err := desktopruntime.OpenCoreLog(*runtimeRoot)
		if err != nil {
			return err
		}
		if logOutput != nil {
			defer logOutput.Close()
			stderr = logOutput
		}
		if err := desktopruntime.PrepareCoreEnvironment(*runtimeRoot); err != nil {
			if logOutput != nil {
				fmt.Fprintf(stderr, "agentdock: %v\n", err)
			}
			return err
		}
		err = runServer(ctx, nil, stderr)
		if err != nil && logOutput != nil {
			fmt.Fprintf(stderr, "agentdock: %v\n", err)
		}
		return err
	}
	return desktopruntime.RunServiceCommand(ctx, args, stdout, stderr)
}
