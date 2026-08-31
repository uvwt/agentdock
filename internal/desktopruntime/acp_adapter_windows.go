//go:build windows

package desktopruntime

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type desktopACPAdapter struct {
	Command string
	Args    []string
}

type desktopACPAdapterPreset struct {
	executableNames []string
	args            []string
	npmPackage      string
	npmBin          string
}

func resolveDesktopACPAdapter(agent, runtimeRoot, configuredCommand string, configuredArgs []string) (desktopACPAdapter, error) {
	normalizedAgent := strings.ToLower(strings.TrimSpace(agent))
	if normalizedAgent == "custom" {
		if adapter, ok := resolveConfiguredACPAdapter(configuredCommand, configuredArgs); ok {
			return adapter, nil
		}
		return desktopACPAdapter{}, errors.New("自定义 Coding Agent 必须指定可直接执行的 ACP Adapter 绝对路径")
	}

	var preset desktopACPAdapterPreset
	switch normalizedAgent {
	case "codex":
		preset = desktopACPAdapterPreset{
			executableNames: []string{"codex-acp.exe", "codex-acp.com"},
			npmPackage:      "@agentclientprotocol/codex-acp",
			npmBin:          "codex-acp",
		}
	case "claude":
		preset = desktopACPAdapterPreset{
			executableNames: []string{"claude-agent-acp.exe", "claude-agent-acp.com"},
			npmPackage:      "@agentclientprotocol/claude-agent-acp",
			npmBin:          "claude-agent-acp",
		}
	case "grok":
		preset = desktopACPAdapterPreset{
			executableNames: []string{"grok.exe", "grok.com"},
			args:            []string{"agent", "stdio"},
		}
	default:
		return desktopACPAdapter{}, fmt.Errorf("不支持的 Coding Agent: %s", agent)
	}

	if adapter, ok := resolveConfiguredACPAdapter(configuredCommand, configuredArgs); ok {
		return adapter, nil
	}

	directories := desktopACPSearchDirectories(runtimeRoot)
	var candidates []string
	for _, name := range preset.executableNames {
		if path, err := exec.LookPath(name); err == nil {
			candidates = append(candidates, path)
		}
	}
	for _, directory := range directories {
		for _, name := range preset.executableNames {
			candidates = append(candidates, filepath.Join(directory, name))
		}
	}

	seen := map[string]struct{}{}
	for _, candidate := range candidates {
		absolute, ok := regularWindowsExecutable(candidate)
		if !ok {
			continue
		}
		key := strings.ToLower(absolute)
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		return desktopACPAdapter{Command: absolute, Args: append([]string(nil), preset.args...)}, nil
	}

	if adapter, ok := resolveNPMACPAdapter(preset, directories); ok {
		return adapter, nil
	}

	name := preset.executableNames[0]
	if preset.npmPackage != "" {
		return desktopACPAdapter{}, fmt.Errorf(
			"未找到可直接执行的 Coding Agent：%s；也未找到可由 Node.js 启动的 %s",
			name,
			preset.npmPackage,
		)
	}
	return desktopACPAdapter{}, errors.New("未找到可直接执行的 Coding Agent：" + name)
}

func resolveConfiguredACPAdapter(command string, args []string) (desktopACPAdapter, bool) {
	if !filepath.IsAbs(strings.TrimSpace(command)) {
		return desktopACPAdapter{}, false
	}
	absolute, ok := regularWindowsExecutable(command)
	if !ok {
		return desktopACPAdapter{}, false
	}
	configuredArgs := append([]string(nil), args...)
	if strings.EqualFold(filepath.Base(absolute), "node.exe") || strings.EqualFold(filepath.Base(absolute), "node.com") {
		if len(configuredArgs) == 0 {
			return desktopACPAdapter{}, false
		}
		entry, ok := regularFile(configuredArgs[0])
		if !ok {
			return desktopACPAdapter{}, false
		}
		configuredArgs[0] = entry
	}
	return desktopACPAdapter{Command: absolute, Args: configuredArgs}, true
}

func desktopACPSearchDirectories(runtimeRoot string) []string {
	var directories []string
	if userHome, err := os.UserHomeDir(); err == nil {
		directories = append(directories, filepath.Join(userHome, ".local", "bin"))
	}
	if appData := strings.TrimSpace(os.Getenv("APPDATA")); appData != "" {
		directories = append(directories, filepath.Join(appData, "npm"))
	}
	if localAppData := strings.TrimSpace(os.Getenv("LOCALAPPDATA")); localAppData != "" {
		directories = append(directories,
			filepath.Join(localAppData, "Programs", "Grok"),
			filepath.Join(localAppData, "Microsoft", "WinGet", "Links"),
			filepath.Join(localAppData, "npm"),
		)
	}
	directories = append(directories, filepath.Join(runtimeRoot, "bin"))
	directories = append(directories, filepath.SplitList(os.Getenv("PATH"))...)

	seen := map[string]struct{}{}
	unique := make([]string, 0, len(directories))
	for _, directory := range directories {
		trimmed := strings.TrimSpace(directory)
		if trimmed == "" {
			continue
		}
		absolute, err := filepath.Abs(trimmed)
		if err != nil {
			continue
		}
		absolute = filepath.Clean(absolute)
		key := strings.ToLower(absolute)
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		unique = append(unique, absolute)
	}
	return unique
}

func resolveNPMACPAdapter(preset desktopACPAdapterPreset, directories []string) (desktopACPAdapter, bool) {
	if preset.npmPackage == "" || preset.npmBin == "" {
		return desktopACPAdapter{}, false
	}
	node, ok := resolveNodeExecutable(directories)
	if !ok {
		return desktopACPAdapter{}, false
	}
	packagePath := filepath.FromSlash(preset.npmPackage)
	for _, directory := range directories {
		packageRoot := filepath.Join(directory, "node_modules", packagePath)
		entry, ok := readNPMBinEntry(packageRoot, preset.npmBin)
		if !ok {
			continue
		}
		args := make([]string, 0, 1+len(preset.args))
		args = append(args, entry)
		args = append(args, preset.args...)
		return desktopACPAdapter{Command: node, Args: args}, true
	}
	return desktopACPAdapter{}, false
}

func resolveNodeExecutable(directories []string) (string, bool) {
	var candidates []string
	for _, name := range []string{"node.exe", "node.com"} {
		if path, err := exec.LookPath(name); err == nil {
			candidates = append(candidates, path)
		}
	}
	for _, directory := range directories {
		candidates = append(candidates,
			filepath.Join(directory, "node.exe"),
			filepath.Join(directory, "node.com"),
		)
	}
	for _, candidate := range candidates {
		if absolute, ok := regularWindowsExecutable(candidate); ok {
			return absolute, true
		}
	}
	return "", false
}

func readNPMBinEntry(packageRoot, binName string) (string, bool) {
	manifestData, err := os.ReadFile(filepath.Join(packageRoot, "package.json"))
	if err != nil {
		return "", false
	}
	var manifest struct {
		Bin json.RawMessage `json:"bin"`
	}
	if err := json.Unmarshal(manifestData, &manifest); err != nil || len(manifest.Bin) == 0 {
		return "", false
	}

	var relativeEntry string
	if err := json.Unmarshal(manifest.Bin, &relativeEntry); err != nil {
		var entries map[string]string
		if err := json.Unmarshal(manifest.Bin, &entries); err != nil {
			return "", false
		}
		relativeEntry = entries[binName]
	}
	relativeEntry = strings.TrimSpace(relativeEntry)
	if relativeEntry == "" || filepath.IsAbs(relativeEntry) {
		return "", false
	}

	absoluteRoot, err := filepath.Abs(packageRoot)
	if err != nil {
		return "", false
	}
	absoluteEntry, err := filepath.Abs(filepath.Join(absoluteRoot, filepath.FromSlash(relativeEntry)))
	if err != nil {
		return "", false
	}
	relativeToRoot, err := filepath.Rel(absoluteRoot, absoluteEntry)
	if err != nil || relativeToRoot == ".." || strings.HasPrefix(relativeToRoot, ".."+string(filepath.Separator)) {
		return "", false
	}
	return regularFile(absoluteEntry)
}

func regularWindowsExecutable(path string) (string, bool) {
	absolute, ok := regularFile(path)
	if !ok {
		return "", false
	}
	switch strings.ToLower(filepath.Ext(absolute)) {
	case ".exe", ".com":
		return absolute, true
	default:
		// npm 在 Windows 同时生成 .cmd、.ps1 和无扩展名 Unix shim，均不能作为 ACP 直连入口。
		return "", false
	}
}

func regularFile(path string) (string, bool) {
	if strings.TrimSpace(path) == "" {
		return "", false
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", false
	}
	absolute = filepath.Clean(absolute)
	info, err := os.Stat(absolute)
	if err != nil || !info.Mode().IsRegular() {
		return "", false
	}
	return absolute, true
}
