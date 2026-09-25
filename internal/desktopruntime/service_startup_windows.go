//go:build windows

package desktopruntime

import (
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"unicode/utf16"

	"golang.org/x/sys/windows/registry"

	processcontrol "github.com/uvwt/agentdock/internal/process"
)

const windowsRunKey = `Software\Microsoft\Windows\CurrentVersion\Run`

type scheduledTaskXML struct {
	Settings struct {
		Enabled *bool `xml:"Enabled"`
	} `xml:"Settings"`
}

func platformSetAutostart(ctx context.Context, runtimeRoot, component string, enabled bool) error {
	manifest, root, err := loadDesktopManifest(runtimeRoot)
	if err != nil {
		return err
	}

	switch component {
	case "core":
		if manifest.UsesScheduledTask() {
			action := "/Disable"
			if enabled {
				action = "/Enable"
			}
			return runScheduledTaskCommand(ctx, "/Change", "/TN", scheduledTaskPath(manifest.AgentDockTaskName), action)
		}
		name := defaultString(manifest.StartupValueName, "AgentDock")
		if !enabled {
			return removeRunValue(name)
		}
		return setRunValue(name, coreStartupCommand(manifest, root))
	case "tray":
		name := defaultString(manifest.TrayStartupValueName, "AgentDockTray")
		if !enabled {
			return removeRunValue(name)
		}
		if strings.TrimSpace(manifest.TrayBinary) == "" {
			return errors.New("Windows runtime manifest 缺少 tray_binary")
		}
		return setRunValue(name, quoteWindowsArgument(manifest.TrayBinary)+" --background")
	default:
		return fmt.Errorf("不支持的开机启动组件：%s", component)
	}
}

func coreAutostartEnabled(ctx context.Context, manifest Manifest) (bool, error) {
	if manifest.UsesScheduledTask() {
		command := exec.CommandContext(ctx, "schtasks.exe", "/Query", "/TN", scheduledTaskPath(manifest.AgentDockTaskName), "/XML")
		processcontrol.Configure(command)
		output, err := command.Output()
		if err != nil {
			return false, err
		}
		task, err := parseScheduledTaskXML(output)
		if err != nil {
			return false, err
		}
		// Task Scheduler 省略 Enabled 时使用 schema 默认值 true。
		return task.Settings.Enabled == nil || *task.Settings.Enabled, nil
	}
	return runValuePresent(defaultString(manifest.StartupValueName, "AgentDock"))
}

func parseScheduledTaskXML(output []byte) (scheduledTaskXML, error) {
	decoded, err := decodeScheduledTaskXML(output)
	if err != nil {
		return scheduledTaskXML{}, err
	}
	decoder := xml.NewDecoder(bytes.NewReader(decoded))
	// schtasks 会保留 UTF-16 声明；字节已在上一步转换为 UTF-8。
	decoder.CharsetReader = func(charset string, input io.Reader) (io.Reader, error) {
		if strings.EqualFold(strings.TrimSpace(charset), "utf-16") {
			return input, nil
		}
		return nil, fmt.Errorf("不支持的计划任务 XML 编码：%s", charset)
	}
	var task scheduledTaskXML
	if err := decoder.Decode(&task); err != nil {
		return scheduledTaskXML{}, err
	}
	return task, nil
}

func decodeScheduledTaskXML(output []byte) ([]byte, error) {
	if len(output) < 2 {
		return output, nil
	}
	littleEndian := output[0] == 0xff && output[1] == 0xfe
	bigEndian := output[0] == 0xfe && output[1] == 0xff
	if !littleEndian && !bigEndian {
		return output, nil
	}
	payload := output[2:]
	if len(payload)%2 != 0 {
		return nil, errors.New("计划任务 XML 的 UTF-16 字节数无效")
	}
	codeUnits := make([]uint16, len(payload)/2)
	for index := range codeUnits {
		if littleEndian {
			codeUnits[index] = uint16(payload[index*2]) | uint16(payload[index*2+1])<<8
		} else {
			codeUnits[index] = uint16(payload[index*2])<<8 | uint16(payload[index*2+1])
		}
	}
	return []byte(string(utf16.Decode(codeUnits))), nil
}

func runScheduledTaskCommand(ctx context.Context, args ...string) error {
	command := exec.CommandContext(ctx, "schtasks.exe", args...)
	processcontrol.Configure(command)
	output, err := command.CombinedOutput()
	if err != nil {
		message := strings.TrimSpace(string(output))
		if message == "" {
			return fmt.Errorf("schtasks %s 失败: %w", strings.Join(args, " "), err)
		}
		return fmt.Errorf("schtasks %s 失败: %w: %s", strings.Join(args, " "), err, message)
	}
	return nil
}

func scheduledTaskPath(name string) string {
	return `\` + strings.TrimLeft(defaultString(name, "AgentDock"), `\`)
}

func coreStartupCommand(manifest Manifest, runtimeRoot string) string {
	if strings.TrimSpace(manifest.TrayBinary) != "" {
		return quoteWindowsArgument(manifest.TrayBinary) + " --start-core --runtime-root " + quoteWindowsArgument(runtimeRoot)
	}
	return quoteWindowsArgument(manifest.AgentDockBinary) + " service start --runtime-root " + quoteWindowsArgument(runtimeRoot)
}

func quoteWindowsArgument(value string) string {
	return `"` + strings.ReplaceAll(value, `"`, `\"`) + `"`
}

func setRunValue(name, value string) error {
	key, _, err := registry.CreateKey(registry.CURRENT_USER, windowsRunKey, registry.SET_VALUE)
	if err != nil {
		return fmt.Errorf("打开 Windows 开机启动注册表失败: %w", err)
	}
	defer key.Close()
	if err := key.SetStringValue(name, value); err != nil {
		return fmt.Errorf("写入 Windows 开机启动注册表失败: %w", err)
	}
	return nil
}

func removeRunValue(name string) error {
	key, err := registry.OpenKey(registry.CURRENT_USER, windowsRunKey, registry.SET_VALUE)
	if errors.Is(err, registry.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("打开 Windows 开机启动注册表失败: %w", err)
	}
	defer key.Close()
	if err := key.DeleteValue(name); err != nil && !errors.Is(err, registry.ErrNotExist) {
		return fmt.Errorf("删除 Windows 开机启动注册表失败: %w", err)
	}
	return nil
}

func runValuePresent(name string) (bool, error) {
	key, err := registry.OpenKey(registry.CURRENT_USER, windowsRunKey, registry.QUERY_VALUE)
	if errors.Is(err, registry.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	defer key.Close()
	value, _, err := key.GetStringValue(name)
	if errors.Is(err, registry.ErrNotExist) {
		return false, nil
	}
	return strings.TrimSpace(value) != "", err
}

func defaultString(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return strings.TrimSpace(value)
}
