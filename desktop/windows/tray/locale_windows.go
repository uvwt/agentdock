//go:build windows

package tray

import (
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
)

const localeNameMaxLength = 85

var procGetUserDefaultLocaleName = windows.NewLazySystemDLL("kernel32.dll").NewProc("GetUserDefaultLocaleName")

type trayText struct {
	TooltipStopped       string
	TooltipRunning       string
	TooltipUnavailable   string
	StatusStopped        string
	StatusRunning        string
	StatusUnavailable    string
	CopyLocalMCP         string
	CopyPublicMCP        string
	RefreshQuickURL      string
	StartAgentDock       string
	RestartAgentDock     string
	InstallUpdate        string
	OpenRuntimeFolder    string
	OpenDocumentation    string
	ExitTray             string
	CopyLocalFailed      string
	CopyLocalSucceeded   string
	CopyPublicFailed     string
	CopyPublicSucceeded  string
	RefreshQuickFailed   string
	RefreshQuickStarted  string
	StartFailed          string
	StartStarted         string
	RestartFailed        string
	RestartStarted       string
	UpdateFailed         string
	ClipboardNoAddress   string
	ClipboardAllocFailed string
	ClipboardLockFailed  string
	ClipboardBusy        string
	ClipboardClearFailed string
	ClipboardWriteFailed string
	RuntimeIncomplete    string
	CoreUnavailablePlain string
	CoreUnavailable      string
	NativeCommandFailed  string
	QuickTunnelNotActive string
	UpdateProgramFailed  string
}

var trayTextEnglish = trayText{
	TooltipStopped:       "AgentDock: Stopped",
	TooltipRunning:       "AgentDock: Running",
	TooltipUnavailable:   "AgentDock: Status unavailable",
	StatusStopped:        "Status: Stopped",
	StatusRunning:        "Status: Running",
	StatusUnavailable:    "Status: Unavailable",
	CopyLocalMCP:         "Copy local MCP address",
	CopyPublicMCP:        "Copy public MCP address",
	RefreshQuickURL:      "Regenerate temporary public address",
	StartAgentDock:       "Start AgentDock",
	RestartAgentDock:     "Restart AgentDock",
	InstallUpdate:        "Check for and install updates",
	OpenRuntimeFolder:    "Open runtime folder",
	OpenDocumentation:    "Open documentation",
	ExitTray:             "Exit tray",
	CopyLocalFailed:      "Failed to copy local MCP address: %v",
	CopyLocalSucceeded:   "Local MCP address copied.",
	CopyPublicFailed:     "Failed to copy public MCP address: %v",
	CopyPublicSucceeded:  "Public MCP address copied.",
	RefreshQuickFailed:   "Failed to regenerate temporary address: %v",
	RefreshQuickStarted:  "Generating a new temporary public address. The tray will update automatically when it is ready.",
	StartFailed:          "Failed to start AgentDock: %v",
	StartStarted:         "Starting AgentDock.",
	RestartFailed:        "Failed to restart AgentDock: %v",
	RestartStarted:       "Restarting AgentDock.",
	UpdateFailed:         "Failed to launch updater: %v",
	ClipboardNoAddress:   "No address is available to copy",
	ClipboardAllocFailed: "failed to allocate clipboard memory: %v",
	ClipboardLockFailed:  "failed to lock clipboard memory: %v",
	ClipboardBusy:        "The clipboard is being used by another application",
	ClipboardClearFailed: "failed to clear clipboard: %v",
	ClipboardWriteFailed: "failed to write clipboard: %v",
	RuntimeIncomplete:    "AgentDock runtime information is incomplete",
	CoreUnavailablePlain: "AgentDock core is unavailable",
	CoreUnavailable:      "AgentDock core is unavailable: %v",
	NativeCommandFailed:  "native control command failed: %v: %s",
	QuickTunnelNotActive: "A temporary public address is not currently in use",
	UpdateProgramFailed:  "failed to launch updater; ShellExecute error code %d",
}

var trayTextChinese = trayText{
	TooltipStopped:       "AgentDock: 未运行",
	TooltipRunning:       "AgentDock: 运行中",
	TooltipUnavailable:   "AgentDock: 状态不可用",
	StatusStopped:        "状态：未运行",
	StatusRunning:        "状态：运行中",
	StatusUnavailable:    "状态：不可用",
	CopyLocalMCP:         "复制本地 MCP 地址",
	CopyPublicMCP:        "复制公网 MCP 地址",
	RefreshQuickURL:      "重新生成临时公网地址",
	StartAgentDock:       "启动 AgentDock",
	RestartAgentDock:     "重启 AgentDock",
	InstallUpdate:        "检查并安装更新",
	OpenRuntimeFolder:    "打开运行目录",
	OpenDocumentation:    "打开使用文档",
	ExitTray:             "退出托盘",
	CopyLocalFailed:      "复制本地 MCP 地址失败：%v",
	CopyLocalSucceeded:   "本地 MCP 地址已复制。",
	CopyPublicFailed:     "复制公网 MCP 地址失败：%v",
	CopyPublicSucceeded:  "公网 MCP 地址已复制。",
	RefreshQuickFailed:   "重新生成临时地址失败：%v",
	RefreshQuickStarted:  "正在生成新的临时公网地址，完成后托盘会自动更新。",
	StartFailed:          "启动 AgentDock 失败：%v",
	StartStarted:         "正在启动 AgentDock。",
	RestartFailed:        "重启 AgentDock 失败：%v",
	RestartStarted:       "正在重启 AgentDock。",
	UpdateFailed:         "启动更新失败：%v",
	ClipboardNoAddress:   "没有可复制的地址",
	ClipboardAllocFailed: "分配剪贴板内存失败: %v",
	ClipboardLockFailed:  "锁定剪贴板内存失败: %v",
	ClipboardBusy:        "剪贴板正被其他程序占用",
	ClipboardClearFailed: "清空剪贴板失败: %v",
	ClipboardWriteFailed: "写入剪贴板失败: %v",
	RuntimeIncomplete:    "AgentDock 运行信息不完整",
	CoreUnavailablePlain: "AgentDock 核心不可用",
	CoreUnavailable:      "AgentDock 核心不可用: %v",
	NativeCommandFailed:  "原生控制命令失败: %v: %s",
	QuickTunnelNotActive: "当前未使用临时公网地址",
	UpdateProgramFailed:  "启动更新程序失败，ShellExecute 错误码 %d",
}

func currentTrayText() trayText {
	var localeName [localeNameMaxLength]uint16
	result, _, _ := procGetUserDefaultLocaleName.Call(
		uintptr(unsafe.Pointer(&localeName[0])),
		uintptr(len(localeName)),
	)
	if result == 0 {
		return trayTextEnglish
	}
	if isSimplifiedChineseLocale(windows.UTF16ToString(localeName[:])) {
		return trayTextChinese
	}
	return trayTextEnglish
}

func isSimplifiedChineseLocale(value string) bool {
	locale := strings.ToLower(strings.TrimSpace(value))
	return locale == "zh" || locale == "zh-cn" || locale == "zh-sg" || locale == "zh-hans" || strings.HasPrefix(locale, "zh-hans-")
}
