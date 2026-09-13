//go:build windows

package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"

	"github.com/uvwt/agentdock/internal/installer"
)

func runInstallPrepareWindowsLegacy(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("agentdock install prepare-windows-legacy", flag.ContinueOnError)
	flags.SetOutput(stderr)
	var request installer.WindowsLegacyBootstrapRequest
	flags.StringVar(&request.InstallRoot, "install-root", "", "AgentDock Windows 安装根目录")
	flags.StringVar(&request.Version, "legacy-version", "", "当前 legacy 安装版本")
	flags.StringVar(&request.CorePath, "legacy-core", "", "当前 legacy Core 路径")
	flags.StringVar(&request.TrayPath, "legacy-tray", "", "当前 legacy Tray 路径")
	flags.StringVar(&request.PayloadDir, "payload-dir", "", "包含 Arbiter/WSL helper 的新 Release 载荷目录")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("未知参数：%s", flags.Arg(0))
	}
	result, err := installer.PrepareWindowsLegacyGeneration(ctx, request)
	if err != nil {
		return err
	}
	return json.NewEncoder(stdout).Encode(result)
}
