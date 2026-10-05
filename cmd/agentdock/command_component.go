package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/uvwt/agentdock/internal/component"
	"github.com/uvwt/agentdock/internal/desktopruntime"
)

func runComponentCommand(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	if len(args) < 2 {
		return errors.New("用法：agentdock component <status|install|update|uninstall> cloudflared")
	}
	action := strings.ToLower(strings.TrimSpace(args[0]))
	name := strings.ToLower(strings.TrimSpace(args[1]))
	if name != component.CloudflaredName {
		return fmt.Errorf("不支持的 component：%s", name)
	}

	flags := flag.NewFlagSet("agentdock component "+action+" "+name, flag.ContinueOnError)
	flags.SetOutput(stderr)
	runtimeRoot := flags.String("runtime-root", component.DefaultRuntimeRoot(), "AgentDock 桌面运行目录")
	jsonOutput := flags.Bool("json", false, "输出 JSON")
	progressJSON := flags.Bool("progress-json", false, "输出 JSON 进度事件")
	catalogURL := flags.String("catalog-url", "", "component catalog URL（测试/受管发布覆盖）")
	source := flags.String("source", "", "legacy component source")
	if err := flags.Parse(args[2:]); err != nil {
		return err
	}
	if flags.NArg() != 0 || strings.TrimSpace(*runtimeRoot) == "" {
		return errors.New("component 参数无效")
	}
	store, err := component.NewStore(*runtimeRoot)
	if err != nil {
		return err
	}

	encodeStatus := func(status component.Status) error {
		if *jsonOutput || *progressJSON {
			return json.NewEncoder(stdout).Encode(status)
		}
		if status.Ready {
			fmt.Fprintf(stdout, "cloudflared %s 已安装\n", status.Version)
		} else {
			fmt.Fprintf(stdout, "cloudflared：%s\n", status.State)
		}
		return nil
	}
	progress := func(component.ProgressEvent) {}
	if *progressJSON {
		encoder := json.NewEncoder(stdout)
		progress = func(event component.ProgressEvent) {
			_ = encoder.Encode(event)
		}
	}
	options := component.InstallOptions{
		RuntimeRoot: *runtimeRoot,
		CatalogURL:  strings.TrimSpace(*catalogURL),
		Progress:    progress,
		LegacyPaths: component.LegacyPaths(*runtimeRoot),
	}

	switch action {
	case "status":
		if *progressJSON || strings.TrimSpace(*source) != "" || strings.TrimSpace(*catalogURL) != "" {
			return errors.New("status 不接受 --progress-json、--source 或 --catalog-url")
		}
		return encodeStatus(store.Status())
	case "install":
		if strings.TrimSpace(*source) != "" {
			return errors.New("install 不接受 --source")
		}
		status, err := store.Install(ctx, options)
		if err != nil {
			return err
		}
		return encodeStatus(status)
	case "update":
		if strings.TrimSpace(*source) != "" {
			return errors.New("update 不接受 --source")
		}
		status, err := store.Update(ctx, options)
		if err != nil {
			return err
		}
		return encodeStatus(status)
	case "uninstall":
		if *progressJSON || strings.TrimSpace(*source) != "" || strings.TrimSpace(*catalogURL) != "" {
			return errors.New("uninstall 不接受 --progress-json、--source 或 --catalog-url")
		}
		tunnel, err := desktopruntime.TunnelStatusForRuntime(ctx, *runtimeRoot)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("卸载前读取 Tunnel 状态失败: %w", err)
		}
		if err == nil && (tunnel.Running || tunnel.Mode == "quick" || tunnel.Mode == "named") {
			return errors.New("cloudflared 正被 Cloudflare Tunnel 配置使用；请先将 Tunnel 切换为 none")
		}
		if err := store.Uninstall(); err != nil {
			return err
		}
		return encodeStatus(store.Status())
	case "__import-legacy":
		if strings.TrimSpace(*source) == "" {
			return errors.New("__import-legacy 需要 --source")
		}
		status, err := store.ImportLegacy(ctx, *source)
		if err != nil {
			return err
		}
		return encodeStatus(status)
	default:
		return errors.New("用法：agentdock component <status|install|update|uninstall> cloudflared")
	}
}
