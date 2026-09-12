package installer

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	"github.com/uvwt/agentdock/internal/desktopruntime"
	"github.com/uvwt/agentdock/internal/envstore"
	"github.com/uvwt/agentdock/internal/fs/atomicfile"
	"github.com/uvwt/agentdock/internal/updateengine"
)

type activatedInstall struct {
	LocalMCPURL   string
	PublicURL     string
	PrivilegeMode string
	ActiveVersion string
	Warnings      []string
}

type unixRuntimeManifest struct {
	SchemaVersion     int    `json:"schema_version"`
	ServiceManager    string `json:"service_manager"`
	ServiceName       string `json:"service_name"`
	TunnelServiceName string `json:"tunnel_service_name"`
	AgentDockBinary   string `json:"agentdock_binary"`
	CloudflaredBinary string `json:"cloudflared_binary"`
	EnvironmentFile   string `json:"environment_file"`
	TunnelEnvironment string `json:"tunnel_environment"`
}

func activateInstall(ctx context.Context, request Request, staged stagedInstall) (activatedInstall, error) {
	switch runtime.GOOS {
	case "windows":
		return activateWindows(ctx, request, staged)
	case "darwin":
		return activateDarwin(ctx, request, staged)
	default:
		return activateLinux(ctx, request, staged)
	}
}

func rollbackInstall(ctx context.Context, request Request, staged stagedInstall) error {
	if staged.Journal == nil {
		return errors.New("rollback journal is missing")
	}
	return staged.Journal.Restore(ctx, request)
}

func switchLiveBinary(staged stagedInstall) error {
	if staged.Binary == "" || staged.LiveBinary == "" {
		return nil
	}
	if filepath.Clean(staged.Binary) == filepath.Clean(staged.LiveBinary) {
		return nil
	}
	if err := staged.Journal.Snapshot(staged.LiveBinary); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(staged.LiveBinary), 0o755); err != nil {
		return err
	}
	tmp := staged.LiveBinary + ".new"
	if err := copyTree(staged.Binary, tmp, 0o755); err != nil {
		return err
	}
	if err := os.Rename(tmp, staged.LiveBinary); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

func activateLinux(ctx context.Context, request Request, staged stagedInstall) (activatedInstall, error) {
	_ = ctx
	envFile := filepath.Join(request.RuntimeRoot, "agentdock.env")
	manifestPath := filepath.Join(request.RuntimeRoot, "desktop-runtime.json")
	if err := os.MkdirAll(request.RuntimeRoot, 0o700); err != nil {
		return activatedInstall{}, err
	}
	if err := staged.Journal.Snapshot(envFile); err != nil {
		return activatedInstall{}, err
	}
	if err := staged.Journal.Snapshot(manifestPath); err != nil {
		return activatedInstall{}, err
	}
	if err := switchLiveBinary(staged); err != nil {
		return activatedInstall{}, err
	}
	if err := writeCoreEnvironment(envFile, request); err != nil {
		return activatedInstall{}, err
	}

	manager := request.ServiceManager
	if manager == "auto" {
		manager = detectLinuxServiceManager(request)
	}
	tunnelName := request.ServiceName + "-cloudflared"
	cloudflared := strings.TrimSpace(request.CloudflaredPath)
	if cloudflared == "" {
		cloudflared = filepath.Join(request.InstallRoot, "bin", "cloudflared")
	}
	tunnelEnv := filepath.Join(request.RuntimeRoot, "cloudflared.env")
	manifest := unixRuntimeManifest{
		SchemaVersion:     1,
		ServiceManager:    manager,
		ServiceName:       request.ServiceName,
		TunnelServiceName: tunnelName,
		AgentDockBinary:   staged.LiveBinary,
		CloudflaredBinary: cloudflared,
		EnvironmentFile:   envFile,
		TunnelEnvironment: tunnelEnv,
	}
	if err := writeUnixManifest(filepath.Join(request.RuntimeRoot, "desktop-runtime.json"), manifest); err != nil {
		return activatedInstall{}, err
	}

	serviceUser := request.ServiceUser
	if serviceUser == "" {
		serviceUser = "root"
	}
	serviceGroup := request.ServiceGroup
	if serviceGroup == "" {
		serviceGroup = serviceUser
	}

	switch manager {
	case "systemd":
		systemdDir := request.SystemdDir
		if systemdDir == "" {
			systemdDir = "/etc/systemd/system"
		}
		unitPath := filepath.Join(systemdDir, request.ServiceName+".service")
		if err := staged.Journal.Snapshot(unitPath); err != nil {
			return activatedInstall{}, err
		}
		if err := writeSystemdUnit(
			unitPath,
			request.ServiceName, serviceUser, serviceGroup,
			request.InstallRoot, envFile, request.RuntimeRoot,
		); err != nil {
			return activatedInstall{}, err
		}
	case "openrc":
		openRCDir := request.OpenRCDir
		if openRCDir == "" {
			openRCDir = "/etc/init.d"
		}
		initPath := filepath.Join(openRCDir, request.ServiceName)
		if err := staged.Journal.Snapshot(initPath); err != nil {
			return activatedInstall{}, err
		}
		if err := writeOpenRCService(
			initPath,
			request.ServiceName, serviceUser, serviceGroup,
			request.InstallRoot, envFile, request.RuntimeRoot,
		); err != nil {
			return activatedInstall{}, err
		}
	}

	if request.TunnelMode == "quick" || request.TunnelMode == "named" {
		if err := staged.Journal.Snapshot(tunnelEnv); err != nil {
			return activatedInstall{}, err
		}
		port := request.Port
		if port == 0 {
			if values, err := envstore.ParseFile(envFile); err == nil {
				port, _ = strconv.Atoi(values["AGENTDOCK_PORT"])
			}
		}
		if port == 0 {
			port = 8765
		}
		target := fmt.Sprintf("http://127.0.0.1:%d", port)
		token := strings.TrimSpace(request.TunnelToken)
		if token == "" && request.TunnelTokenFile != "" {
			data, err := os.ReadFile(request.TunnelTokenFile)
			if err != nil {
				return activatedInstall{}, fmt.Errorf("read tunnel token: %w", err)
			}
			token = strings.TrimSpace(string(data))
		}
		if err := writeTunnelEnvironment(tunnelEnv, request.TunnelMode, target, token); err != nil {
			return activatedInstall{}, err
		}
		binary := staged.LiveBinary
		if binary == "" {
			binary = staged.Binary
		}
		dataDir := request.DataDir
		if dataDir == "" {
			dataDir = request.InstallRoot
		}
		switch manager {
		case "systemd":
			systemdDir := request.SystemdDir
			if systemdDir == "" {
				systemdDir = "/etc/systemd/system"
			}
			unitPath := filepath.Join(systemdDir, tunnelName+".service")
			if err := staged.Journal.Snapshot(unitPath); err != nil {
				return activatedInstall{}, err
			}
			if err := writeSystemdTunnelUnit(
				unitPath,
				serviceUser, serviceGroup, dataDir, tunnelEnv, binary, request.RuntimeRoot,
			); err != nil {
				return activatedInstall{}, err
			}
		case "openrc":
			openRCDir := request.OpenRCDir
			if openRCDir == "" {
				openRCDir = "/etc/init.d"
			}
			initPath := filepath.Join(openRCDir, tunnelName)
			if err := staged.Journal.Snapshot(initPath); err != nil {
				return activatedInstall{}, err
			}
			if err := writeOpenRCTunnelService(
				initPath,
				tunnelName, serviceUser, serviceGroup, dataDir, tunnelEnv, binary, request.RuntimeRoot,
			); err != nil {
				return activatedInstall{}, err
			}
		}
	} else if request.TunnelMode == "none" {
		if err := staged.Journal.Snapshot(tunnelEnv); err != nil {
			return activatedInstall{}, err
		}
		_ = os.Remove(tunnelEnv)
		switch manager {
		case "systemd":
			systemdDir := request.SystemdDir
			if systemdDir == "" {
				systemdDir = "/etc/systemd/system"
			}
			unitPath := filepath.Join(systemdDir, tunnelName+".service")
			if err := staged.Journal.Snapshot(unitPath); err != nil {
				return activatedInstall{}, err
			}
			_ = os.Remove(unitPath)
		case "openrc":
			openRCDir := request.OpenRCDir
			if openRCDir == "" {
				openRCDir = "/etc/init.d"
			}
			initPath := filepath.Join(openRCDir, tunnelName)
			if err := staged.Journal.Snapshot(initPath); err != nil {
				return activatedInstall{}, err
			}
			_ = os.Remove(initPath)
		}
	}

	return resultFromEnv(envFile, request)
}

func activateDarwin(ctx context.Context, request Request, staged stagedInstall) (activatedInstall, error) {
	_ = ctx
	if err := os.MkdirAll(request.RuntimeRoot, 0o700); err != nil {
		return activatedInstall{}, err
	}
	envFile := filepath.Join(request.RuntimeRoot, "agentdock.env")
	if err := staged.Journal.Snapshot(envFile); err != nil {
		return activatedInstall{}, err
	}
	if err := switchLiveBinary(staged); err != nil {
		return activatedInstall{}, err
	}
	if err := writeCoreEnvironment(envFile, request); err != nil {
		return activatedInstall{}, err
	}
	if err := staged.Journal.Snapshot(filepath.Join(request.RuntimeRoot, "desktop-runtime.json")); err != nil {
		return activatedInstall{}, err
	}
	workDir := request.DataDir
	if workDir == "" {
		workDir = filepath.Join(request.RuntimeRoot, "AgentDock")
	}
	if err := os.MkdirAll(workDir, 0o700); err != nil {
		return activatedInstall{}, err
	}
	agentsDir := request.LaunchAgentsDir
	if agentsDir == "" && request.RegisterService {
		home, _ := os.UserHomeDir()
		agentsDir = filepath.Join(home, "Library", "LaunchAgents")
	}
	if request.RegisterService {
		home, homeErr := os.UserHomeDir()
		if homeErr != nil {
			return activatedInstall{}, homeErr
		}
		logDir := filepath.Join(home, "Library", "Logs", "AgentDock")
		if err := os.MkdirAll(logDir, 0o700); err != nil {
			return activatedInstall{}, err
		}
		for _, name := range []string{"agentdock.out.log", "agentdock.err.log"} {
			path := filepath.Join(logDir, name)
			file, err := os.OpenFile(path, os.O_CREATE|os.O_RDONLY, 0o600)
			if err != nil {
				return activatedInstall{}, err
			}
			_ = file.Close()
		}
		plistPath := filepath.Join(agentsDir, darwinCLICoreLabel+".plist")
		if err := staged.Journal.Snapshot(plistPath); err != nil {
			return activatedInstall{}, err
		}
		runtimeRoot := request.RuntimeRootLiteral
		if runtimeRoot == "" {
			runtimeRoot = request.RuntimeRoot
		}
		if err := writeLaunchAgent(
			plistPath,
			staged.LiveBinary,
			runtimeRoot,
			workDir,
		); err != nil {
			return activatedInstall{}, err
		}
	}
	cloudflared := strings.TrimSpace(request.CloudflaredPath)
	if cloudflared == "" {
		cloudflared = filepath.Join(filepath.Dir(staged.LiveBinary), "cloudflared")
	}
	if err := writeUnixManifest(filepath.Join(request.RuntimeRoot, "desktop-runtime.json"), unixRuntimeManifest{
		SchemaVersion:     1,
		ServiceManager:    "launchd",
		ServiceName:       darwinCLICoreLabel,
		TunnelServiceName: darwinCLITunnelLabel,
		AgentDockBinary:   staged.LiveBinary,
		CloudflaredBinary: cloudflared,
		EnvironmentFile:   envFile,
		TunnelEnvironment: filepath.Join(request.RuntimeRoot, "cloudflared.env"),
	}); err != nil {
		return activatedInstall{}, err
	}
	if err := applyDarwinTunnel(request, staged, envFile, agentsDir, workDir); err != nil {
		return activatedInstall{}, err
	}
	return resultFromEnv(envFile, request)
}

func applyDarwinTunnel(request Request, staged stagedInstall, envFile, agentsDir, workDir string) error {
	tunnelEnv := filepath.Join(request.RuntimeRoot, "cloudflared.env")
	legacyStart := filepath.Join(request.RuntimeRoot, "start-cloudflared.sh")
	tunnelPlist := ""
	if agentsDir != "" {
		tunnelPlist = filepath.Join(agentsDir, darwinCLITunnelLabel+".plist")
	}
	if request.TunnelMode == "none" {
		if err := staged.Journal.Snapshot(tunnelEnv); err != nil {
			return err
		}
		if tunnelPlist != "" {
			if err := staged.Journal.Snapshot(tunnelPlist); err != nil {
				return err
			}
			_ = os.Remove(tunnelPlist)
		}
		_ = os.Remove(tunnelEnv)
		_ = os.Remove(legacyStart)
		return nil
	}
	if request.TunnelMode != "quick" && request.TunnelMode != "named" {
		return nil
	}
	if err := staged.Journal.Snapshot(tunnelEnv); err != nil {
		return err
	}
	port := request.Port
	if port == 0 {
		if values, err := envstore.ParseFile(envFile); err == nil {
			port, _ = strconv.Atoi(values["AGENTDOCK_PORT"])
		}
	}
	if port == 0 {
		port = 8765
	}
	token := strings.TrimSpace(request.TunnelToken)
	if token == "" && request.TunnelTokenFile != "" {
		data, err := os.ReadFile(request.TunnelTokenFile)
		if err != nil {
			return fmt.Errorf("read tunnel token: %w", err)
		}
		token = strings.TrimSpace(string(data))
	}
	target := fmt.Sprintf("http://127.0.0.1:%d", port)
	if err := writeTunnelEnvironment(tunnelEnv, request.TunnelMode, target, token); err != nil {
		return err
	}
	if !request.RegisterService || tunnelPlist == "" {
		return nil
	}
	if err := staged.Journal.Snapshot(tunnelPlist); err != nil {
		return err
	}
	runtimeRoot := request.RuntimeRootLiteral
	if runtimeRoot == "" {
		runtimeRoot = request.RuntimeRoot
	}
	if err := writeTunnelLaunchAgent(tunnelPlist, staged.LiveBinary, runtimeRoot, workDir); err != nil {
		return err
	}
	_ = os.Remove(legacyStart)
	return nil
}

func activateWindows(ctx context.Context, request Request, staged stagedInstall) (activatedInstall, error) {
	_ = ctx
	if staged.WindowsLayout == nil {
		return activatedInstall{}, fmt.Errorf("Windows generation 尚未准备")
	}
	layout := *staged.WindowsLayout
	if err := layout.EnsureBase(); err != nil {
		return activatedInstall{}, err
	}
	if err := staged.Journal.Snapshot(filepath.Join(request.InstallRoot, "runtime.json")); err != nil {
		return activatedInstall{}, err
	}

	host := request.Host
	port := request.Port
	tunnelMode := request.TunnelMode
	publicURL := strings.TrimSpace(request.ServerURL)
	privilege := request.PrivilegeMode
	home := request.AgentDockHome
	defaultDir := request.AgentDockDefaultDir
	channel := request.Channel
	// repair / 省略标志时必须保留已有 runtime.json，不能把 host/port/tunnel 重置成默认值。
	if existing, err := desktopruntime.Load(filepath.Join(request.InstallRoot, "runtime.json")); err == nil {
		if host == "" {
			host = existing.Host
		}
		if port == 0 {
			port = existing.Port
		}
		if tunnelMode == "" {
			tunnelMode = existing.TunnelMode
		}
		if publicURL == "" && (tunnelMode == "named" || tunnelMode == "quick") {
			publicURL = existing.PublicURL
		}
		if privilege == "" {
			privilege = existing.PrivilegeMode
		}
		if home == "" {
			home = existing.AgentDockHome
		}
		if defaultDir == "" {
			defaultDir = existing.AgentDockDefaultDir
		}
		if channel == "" {
			channel = existing.InstallChannel
		}
	}
	if privilege == "" {
		privilege = "standard"
	}
	if host == "" {
		host = "127.0.0.1"
	}
	if port == 0 {
		port = 8765
	}
	if tunnelMode == "" {
		tunnelMode = "none"
	}
	binDir := layout.BinDir()
	coreShim := filepath.Join(request.PayloadDir, "agentdock-shim.exe")
	trayShim := filepath.Join(request.PayloadDir, "agentdock-tray-shim.exe")
	if request.PayloadDir != "" {
		if fileExists(coreShim) {
			if err := copyTree(coreShim, layout.CoreShim(), 0o755); err != nil {
				return activatedInstall{}, err
			}
		}
		if fileExists(trayShim) {
			if err := copyTree(trayShim, layout.TrayShim(), 0o755); err != nil {
				return activatedInstall{}, err
			}
		}
		managerSrc := filepath.Join(request.PayloadDir, "manage-windows.ps1")
		if fileExists(managerSrc) {
			if err := copyTree(managerSrc, filepath.Join(request.InstallRoot, "installer", "manage-windows.ps1"), 0o644); err != nil {
				return activatedInstall{}, err
			}
		}
	}

	activeVersion := request.Version
	if store, err := updateengine.NewStore(request.InstallRoot); err == nil {
		if active, err := store.ReadActive(); err == nil && strings.TrimSpace(active.ActiveVersion) != "" {
			// generation pointer 由 Update Engine / 首次 bootstrap 拥有。
			// Installer trial 不得改写成 target committed，否则 shim 恢复只认识 update/transaction.json。
			activeVersion = active.ActiveVersion
		}
	}

	manifest := desktopruntime.Manifest{
		SchemaVersion:               1,
		InstallRoot:                 request.InstallRoot,
		AgentDockHome:               home,
		AgentDockDefaultDir:         defaultDir,
		AgentDockBinary:             layout.CoreShim(),
		TrayBinary:                  layout.TrayShim(),
		AgentDockLauncher:           filepath.Join(request.InstallRoot, "start-agentdock.ps1"),
		AgentDockTaskName:           defaultString(request.TaskName, "AgentDock"),
		PrivilegeMode:               privilege,
		CloudflaredBinary:           filepath.Join(binDir, "cloudflared.exe"),
		CloudflaredLauncher:         filepath.Join(request.InstallRoot, "start-cloudflared.ps1"),
		StartupValueName:            "AgentDock",
		TrayStartupValueName:        "AgentDockTray",
		CloudflaredStartupValueName: "AgentDockCloudflared",
		Host:                        host,
		Port:                        port,
		LocalMCPURL:                 localMCPURL(host, port),
		TunnelMode:                  tunnelMode,
		PublicURL:                   publicURL,
		InstallChannel:              channel,
	}
	if err := desktopruntime.Save(filepath.Join(request.InstallRoot, "runtime.json"), manifest); err != nil {
		return activatedInstall{}, err
	}

	return activatedInstall{
		LocalMCPURL:   localMCPURL(host, port),
		PublicURL:     publicURL,
		PrivilegeMode: privilege,
		ActiveVersion: activeVersion,
	}, nil
}

func stageWindowsPayload(request Request, journal *rollbackJournal) (stagedInstall, error) {
	layout, err := updateengine.NewWindowsLayout(request.InstallRoot)
	if err != nil {
		return stagedInstall{}, err
	}
	// 必须在 target trial pointer 发布之前记录旧 Core/Tunnel 运行态，否则 status 会解析到
	// 新 generation，失败回滚时就无法知道是否应重启 known-good source。
	if err := snapshotWindowsRuntimeState(request, journal); err != nil {
		return stagedInstall{}, err
	}
	// Windows generation 只能有一个事务 owner：
	// 升级走 Update Engine，首次发布走 Setup bootstrap 或下面的 publish。
	// 已有 active-version / generation 时 Installer 只附着，禁止再删再写同一目录。
	if strings.TrimSpace(request.PayloadDir) == "" || windowsGenerationAlreadyOwned(request) {
		return attachWindowsGeneration(request, journal, layout)
	}
	return publishWindowsGeneration(request, journal, layout)
}

func windowsGenerationAlreadyOwned(request Request) bool {
	// 只有 committed pointer 且它已经是本次目标版本时，Installer 才能附着。
	// 不同版本升级必须由 Installer 自己发布 target trial + source fallback，不能先让
	// Update Engine 把 target committed 后再继续 OS adapter，否则后续失败无法原子回退。
	active := windowsCommittedGeneration(request)
	if active == "" {
		return false
	}
	target := updateengine.NormalizeVersion(request.Version)
	return target == "" || updateengine.NormalizeVersion(active) == target
}

func publishWindowsGeneration(request Request, journal *rollbackJournal, layout updateengine.WindowsLayout) (stagedInstall, error) {
	if err := layout.EnsureBase(); err != nil {
		return stagedInstall{}, err
	}
	version := request.Version
	if version == "" {
		version = updateengine.NormalizeVersion("0.0.0")
	}
	staging := filepath.Join(layout.VersionsDir(), ".bootstrap-"+version)
	if err := os.MkdirAll(staging, 0o700); err != nil {
		return stagedInstall{}, err
	}
	payload := request.PayloadDir
	copies := []struct{ src, dst string }{
		{filepath.Join(payload, "agentdock.exe"), filepath.Join(staging, updateengine.GenerationCoreName)},
		{filepath.Join(payload, "agentdock-tray.exe"), filepath.Join(staging, updateengine.GenerationTrayName)},
		{filepath.Join(payload, "agentdock-arbiter.exe"), filepath.Join(staging, updateengine.GenerationArbiterName)},
	}
	for _, item := range copies {
		if !fileExists(item.src) {
			return stagedInstall{}, fmt.Errorf("payload 缺少 %s", filepath.Base(item.src))
		}
		if err := copyTree(item.src, item.dst, 0o755); err != nil {
			return stagedInstall{}, err
		}
	}
	skillsSrc := filepath.Join(payload, "share", "agentdock", "core-skills")
	if dirExists(skillsSrc) {
		if err := copyTree(skillsSrc, filepath.Join(staging, "core-skills"), 0o644); err != nil {
			return stagedInstall{}, err
		}
	}
	wslSrc := filepath.Join(payload, "wsl-helper")
	if dirExists(wslSrc) {
		if err := copyTree(wslSrc, filepath.Join(staging, "wsl-helper"), 0o755); err != nil {
			return stagedInstall{}, err
		}
	}
	generation := layout.GenerationDir(version)
	if err := journal.Snapshot(generation); err != nil {
		return stagedInstall{}, err
	}
	if err := os.RemoveAll(generation); err != nil && !os.IsNotExist(err) {
		return stagedInstall{}, err
	}
	if err := os.Rename(staging, generation); err != nil {
		return stagedInstall{}, err
	}
	if err := journal.NoteCreated(generation); err != nil {
		return stagedInstall{}, err
	}
	staged := stagedInstall{
		Binary:        layout.GenerationCore(version),
		WindowsLayout: &layout,
		GenerationDir: generation,
		Journal:       journal,
	}
	if skillDir := filepath.Join(generation, "core-skills"); dirExists(skillDir) {
		staged.SkillBundle = skillDir
	}
	// Installer 是本次 target generation 的唯一事务 owner。已有 committed source 时把它
	// 写进 fallback，外层 Task/Registry 适配器即使在 Engine commit 之后失败，也能依据
	// TransactionID 把 pointer 原子恢复到 known-good source。
	activePath := filepath.Join(request.InstallRoot, "active-version.json")
	fallbackVersion := ""
	if store, err := updateengine.NewStore(request.InstallRoot); err == nil {
		if active, err := store.ReadActive(); err == nil && active.State == updateengine.StateCommitted {
			fallbackVersion = active.ActiveVersion
		}
	}
	if err := journal.Snapshot(activePath); err != nil {
		return stagedInstall{}, err
	}
	if err := writeWindowsActivePointer(request.InstallRoot, updateengine.ActiveVersion{
		SchemaVersion:   updateengine.SchemaVersion,
		ActiveVersion:   version,
		FallbackVersion: fallbackVersion,
		State:           updateengine.StateTrial,
		TransactionID:   journal.TransactionID,
	}); err != nil {
		return stagedInstall{}, err
	}
	return staged, nil
}

func attachWindowsGeneration(request Request, journal *rollbackJournal, layout updateengine.WindowsLayout) (stagedInstall, error) {
	if err := layout.EnsureBase(); err != nil {
		return stagedInstall{}, err
	}
	version := strings.TrimSpace(request.Version)
	if version == "" {
		if store, err := updateengine.NewStore(request.InstallRoot); err == nil {
			if active, err := store.ReadActive(); err == nil {
				version = active.ActiveVersion
			}
		}
	}
	if version == "" {
		version = existingVersion(request)
	}
	core := ""
	generation := ""
	if version != "" {
		generation = layout.GenerationDir(version)
		if fileExists(layout.GenerationCore(version)) {
			core = layout.GenerationCore(version)
		}
	}
	if core == "" && fileExists(strings.TrimSpace(request.BinaryPath)) {
		core = request.BinaryPath
	}
	if core == "" && fileExists(layout.CoreShim()) {
		core = layout.CoreShim()
	}
	if core == "" {
		return stagedInstall{}, fmt.Errorf("找不到已安装的 Windows generation 或 shim")
	}
	staged := stagedInstall{
		Binary:        core,
		WindowsLayout: &layout,
		GenerationDir: generation,
		Journal:       journal,
	}
	if skillDir := filepath.Join(generation, "core-skills"); dirExists(skillDir) {
		staged.SkillBundle = skillDir
	}
	return staged, nil
}

func detectLinuxServiceManager(request Request) string {
	if request.SystemdDir != "" {
		return "systemd"
	}
	if request.OpenRCDir != "" {
		return "openrc"
	}
	if _, err := os.Stat("/run/systemd/system"); err == nil {
		return "systemd"
	}
	if _, err := os.Stat("/sbin/openrc-run"); err == nil {
		return "openrc"
	}
	return "none"
}

func writeUnixManifest(path string, manifest unixRuntimeManifest) error {
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return atomicfile.Write(path, data, 0o644)
}

func writeWindowsActivePointer(installRoot string, active updateengine.ActiveVersion) error {
	store, err := updateengine.NewStore(installRoot)
	if err != nil {
		return err
	}
	return store.WriteActive(active)
}

func commitWindowsActivePointer(installRoot, transactionID string) error {
	store, err := updateengine.NewStore(installRoot)
	if err != nil {
		return err
	}
	active, err := store.ReadActive()
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if active.State == updateengine.StateCommitted {
		return nil
	}
	if active.State != updateengine.StateTrial {
		return fmt.Errorf("active generation state=%s，不能随 install commit 收敛", active.State)
	}
	if strings.TrimSpace(transactionID) != "" && active.TransactionID != transactionID {
		return fmt.Errorf("active generation trial %s 与 install 事务 %s 不一致", active.TransactionID, transactionID)
	}
	// 保留 TransactionID，崩溃后 recover 能认出“本事务已经 committed pointer”，
	// 从而把 trial 事务补写成 committed，而不是按中断安装回滚 generation。
	return store.WriteActive(updateengine.ActiveVersion{
		SchemaVersion:   updateengine.SchemaVersion,
		ActiveVersion:   active.ActiveVersion,
		FallbackVersion: active.FallbackVersion,
		State:           updateengine.StateCommitted,
		TransactionID:   transactionID,
	})
}

func windowsPointerCommittedBy(installRoot, transactionID string) (bool, error) {
	transactionID = strings.TrimSpace(transactionID)
	if transactionID == "" {
		return false, nil
	}
	store, err := updateengine.NewStore(installRoot)
	if err != nil {
		return false, err
	}
	active, err := store.ReadActive()
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	return active.State == updateengine.StateCommitted && active.TransactionID == transactionID, nil
}

func releaseWindowsTrialPointer(installRoot, transactionID string) error {
	store, err := updateengine.NewStore(installRoot)
	if err != nil {
		return err
	}
	active, err := store.ReadActive()
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	want := strings.TrimSpace(transactionID)
	if want == "" || active.TransactionID != want {
		return nil
	}
	if active.State != updateengine.StateTrial && active.State != updateengine.StateCommitted {
		return nil
	}
	if fallback := updateengine.NormalizeVersion(active.FallbackVersion); fallback != "" {
		return store.WriteActive(updateengine.ActiveVersion{
			SchemaVersion: updateengine.SchemaVersion,
			ActiveVersion: fallback,
			State:         updateengine.StateCommitted,
		})
	}
	if err := os.Remove(store.ActivePath()); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func readActivatedInstall(request Request) (activatedInstall, error) {
	if runtime.GOOS == "windows" {
		manifest, err := desktopruntime.Load(filepath.Join(request.RuntimeRoot, "runtime.json"))
		if err != nil {
			return activatedInstall{}, err
		}
		return activatedInstall{
			LocalMCPURL:   manifest.LocalMCPURL,
			PublicURL:     manifest.PublicURL,
			PrivilegeMode: manifest.PrivilegeMode,
			ActiveVersion: windowsCommittedGeneration(request),
		}, nil
	}
	probe := request
	probe.ServerURL = ""
	probe.Host = ""
	probe.Port = 0
	return resultFromEnv(filepath.Join(request.RuntimeRoot, "agentdock.env"), probe)
}

func resultFromEnv(envFile string, request Request) (activatedInstall, error) {
	host := request.Host
	port := request.Port
	if values, err := envstore.ParseFile(envFile); err == nil {
		if host == "" {
			host = values["AGENTDOCK_HOST"]
		}
		if port == 0 {
			port, _ = strconv.Atoi(values["AGENTDOCK_PORT"])
		}
		if request.ServerURL == "" {
			request.ServerURL = values["AGENTDOCK_SERVER_URL"]
		}
	}
	if host == "" {
		host = "127.0.0.1"
	}
	if port == 0 {
		port = 8765
	}
	return activatedInstall{
		LocalMCPURL:   localMCPURL(host, port),
		PublicURL:     strings.TrimSpace(request.ServerURL),
		ActiveVersion: request.Version,
	}, nil
}

func defaultString(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}
