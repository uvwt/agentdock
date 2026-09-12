package desktopruntime

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/uvwt/agentdock/internal/fs/atomicfile"
	"github.com/uvwt/agentdock/internal/updateengine"
)

const SchemaVersion = 1

type Manifest struct {
	SchemaVersion               int    `json:"schema_version"`
	InstallRoot                 string `json:"install_root,omitempty"`
	AgentDockHome               string `json:"agentdock_home,omitempty"`
	AgentDockDefaultDir         string `json:"agentdock_default_dir,omitempty"`
	AgentDockBinary             string `json:"agentdock_binary"`
	TrayBinary                  string `json:"tray_binary,omitempty"`
	AgentDockLauncher           string `json:"agentdock_launcher,omitempty"`
	AgentDockTaskName           string `json:"agentdock_task_name,omitempty"`
	PrivilegeMode               string `json:"privilege_mode,omitempty"`
	CloudflaredBinary           string `json:"cloudflared_binary,omitempty"`
	CloudflaredLauncher         string `json:"cloudflared_launcher,omitempty"`
	StartupValueName            string `json:"startup_value_name,omitempty"`
	TrayStartupValueName        string `json:"tray_startup_value_name,omitempty"`
	CloudflaredStartupValueName string `json:"cloudflared_startup_value_name,omitempty"`
	Host                        string `json:"host"`
	Port                        int    `json:"port"`
	LocalMCPURL                 string `json:"local_mcp_url"`
	TunnelMode                  string `json:"tunnel_mode"`
	PublicURL                   string `json:"public_url,omitempty"`
	InstallChannel              string `json:"install_channel"`
}

func PathForBinary(binaryPath string) string {
	// 新 Windows generation 位于 <root>/versions/<version>/agentdock-core.exe。
	// runtime.json 仍属于稳定安装根，而不是某个可回收 generation。
	if strings.EqualFold(filepath.Base(binaryPath), updateengine.GenerationCoreName) {
		generationDir := filepath.Dir(binaryPath)
		versionsDir := filepath.Dir(generationDir)
		if strings.EqualFold(filepath.Base(versionsDir), "versions") {
			return filepath.Join(filepath.Dir(versionsDir), "runtime.json")
		}
	}
	return filepath.Join(filepath.Dir(filepath.Dir(binaryPath)), "runtime.json")
}

func Load(path string) (Manifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Manifest{}, err
	}
	var manifest Manifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return Manifest{}, fmt.Errorf("parse Windows runtime manifest: %w", err)
	}
	if err := manifest.Validate(); err != nil {
		return Manifest{}, err
	}
	runtimeRoot, err := filepath.Abs(filepath.Dir(path))
	if err != nil {
		return Manifest{}, fmt.Errorf("resolve Windows runtime root: %w", err)
	}
	manifest.resolveRuntimePaths(runtimeRoot)
	return manifest, nil
}

func Save(path string, manifest Manifest) error {
	if err := manifest.Validate(); err != nil {
		return err
	}
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return fmt.Errorf("encode Windows runtime manifest: %w", err)
	}
	data = append(data, '\n')
	if err := atomicfile.Write(path, data, 0o600); err != nil {
		return fmt.Errorf("write Windows runtime manifest: %w", err)
	}
	return nil
}

func LoadForBinary(binaryPath string) (Manifest, error) {
	manifest, err := Load(PathForBinary(binaryPath))
	if err != nil {
		return Manifest{}, err
	}
	if samePath(manifest.AgentDockBinary, binaryPath) {
		return manifest, nil
	}
	// generation core 通过 stable shim 进入运行时，因此 manifest.agentdock_binary
	// 应该始终指向稳定入口。这里只接受 active-version.json 明确选中的 core，
	// 避免任意旧 generation 伪装成当前运行时。
	if strings.EqualFold(filepath.Base(binaryPath), updateengine.GenerationCoreName) {
		root := filepath.Dir(PathForBinary(binaryPath))
		store, storeErr := updateengine.NewStore(root)
		if storeErr == nil {
			active, activeErr := store.ReadActive()
			layout, layoutErr := updateengine.NewWindowsLayout(root)
			if activeErr == nil && layoutErr == nil && samePath(layout.GenerationCore(active.ActiveVersion), binaryPath) {
				return manifest, nil
			}
		}
	}
	return Manifest{}, errors.New("Windows runtime manifest belongs to another AgentDock binary")
}

// ActiveCoreBinary 返回当前真正承载 Core 的 generation 二进制。
// 旧布局没有 active-version.json 时回退 manifest.agentdock_binary，保持迁移兼容。
func ActiveCoreBinary(runtimeRoot string, manifest Manifest) string {
	store, err := updateengine.NewStore(runtimeRoot)
	if err != nil {
		return manifest.AgentDockBinary
	}
	active, err := store.ReadActive()
	if err != nil {
		return manifest.AgentDockBinary
	}
	layout, err := updateengine.NewWindowsLayout(runtimeRoot)
	if err != nil {
		return manifest.AgentDockBinary
	}
	path := layout.GenerationCore(active.ActiveVersion)
	if regularFileExists(path) {
		return path
	}
	return manifest.AgentDockBinary
}

// resolveRuntimePaths 以 runtime.json 的实际目录作为当前安装根。
// Windows 打包应用可能重定向文件写入，却保留安装时记录的旧绝对路径；
// 因此所有由 AgentDock 管理的安装路径都必须先按真实根目录收敛。
func (manifest *Manifest) resolveRuntimePaths(runtimeRoot string) {
	runtimeRoot = filepath.Clean(runtimeRoot)
	recordedRoot := strings.TrimSpace(manifest.InstallRoot)
	manifest.InstallRoot = runtimeRoot
	manifest.AgentDockBinary = resolveRuntimeManagedFile(
		recordedRoot,
		runtimeRoot,
		manifest.AgentDockBinary,
		filepath.Join(runtimeRoot, "bin", "agentdock.exe"),
		true,
	)
	manifest.TrayBinary = resolveRuntimeManagedFile(
		recordedRoot,
		runtimeRoot,
		manifest.TrayBinary,
		filepath.Join(runtimeRoot, "bin", "agentdock-tray.exe"),
		false,
	)
	manifest.AgentDockLauncher = resolveRuntimeManagedFile(
		recordedRoot,
		runtimeRoot,
		manifest.AgentDockLauncher,
		filepath.Join(runtimeRoot, "start-agentdock.ps1"),
		false,
	)
	manifest.CloudflaredBinary = resolveRuntimeManagedFile(
		recordedRoot,
		runtimeRoot,
		manifest.CloudflaredBinary,
		filepath.Join(runtimeRoot, "bin", "cloudflared.exe"),
		false,
	)
	manifest.CloudflaredLauncher = resolveRuntimeManagedFile(
		recordedRoot,
		runtimeRoot,
		manifest.CloudflaredLauncher,
		filepath.Join(runtimeRoot, "start-cloudflared.ps1"),
		false,
	)
}

func resolveRuntimeManagedFile(recordedRoot, runtimeRoot, recordedPath, fallbackPath string, required bool) string {
	recordedPath = strings.TrimSpace(recordedPath)
	if recordedPath == "" {
		if required || regularFileExists(fallbackPath) {
			return filepath.Clean(fallbackPath)
		}
		return ""
	}

	candidate := recordedPath
	managedPath := pathWithinRoot(runtimeRoot, recordedPath)
	if recordedRoot != "" && filepath.IsAbs(recordedRoot) && pathWithinRoot(recordedRoot, recordedPath) {
		managedPath = true
		if !samePath(recordedRoot, runtimeRoot) {
			relative, err := filepath.Rel(recordedRoot, recordedPath)
			if err == nil {
				candidate = filepath.Join(runtimeRoot, relative)
			}
		}
	}

	if regularFileExists(candidate) {
		return filepath.Clean(candidate)
	}
	if managedPath && regularFileExists(fallbackPath) {
		return filepath.Clean(fallbackPath)
	}
	return filepath.Clean(candidate)
}

func pathWithinRoot(root, path string) bool {
	if strings.TrimSpace(root) == "" || strings.TrimSpace(path) == "" || !filepath.IsAbs(root) || !filepath.IsAbs(path) {
		return false
	}
	relative, err := filepath.Rel(root, path)
	if err != nil || relative == "." || relative == ".." {
		return false
	}
	return !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}

func regularFileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func (manifest Manifest) Validate() error {
	if manifest.SchemaVersion != SchemaVersion {
		return fmt.Errorf("unsupported Windows runtime manifest schema: %d", manifest.SchemaVersion)
	}
	if strings.TrimSpace(manifest.AgentDockBinary) == "" || !filepath.IsAbs(manifest.AgentDockBinary) {
		return errors.New("Windows runtime manifest requires an absolute agentdock_binary")
	}
	for name, path := range map[string]string{
		"agentdock_home":        manifest.AgentDockHome,
		"agentdock_default_dir": manifest.AgentDockDefaultDir,
	} {
		path = strings.TrimSpace(path)
		if path != "" && !filepath.IsAbs(path) {
			return fmt.Errorf("Windows runtime manifest %s must be absolute", name)
		}
	}
	if manifest.PrivilegeMode != "" && manifest.PrivilegeMode != "standard" && manifest.PrivilegeMode != "elevated" {
		return fmt.Errorf("unsupported Windows privilege mode: %s", manifest.PrivilegeMode)
	}
	if manifest.PrivilegeMode == "elevated" && strings.TrimSpace(manifest.AgentDockTaskName) == "" {
		return errors.New("elevated Windows runtime requires agentdock_task_name")
	}
	if manifest.Port < 1 || manifest.Port > 65535 {
		return errors.New("Windows runtime manifest port must be between 1 and 65535")
	}
	if strings.TrimSpace(manifest.Host) == "" {
		return errors.New("Windows runtime manifest host is required")
	}
	if err := validateHTTPURL(manifest.LocalMCPURL, false); err != nil {
		return fmt.Errorf("invalid local_mcp_url: %w", err)
	}
	if manifest.TunnelMode != "none" && manifest.TunnelMode != "quick" && manifest.TunnelMode != "named" {
		return fmt.Errorf("unsupported Windows tunnel mode: %s", manifest.TunnelMode)
	}
	if manifest.TunnelMode == "none" && strings.TrimSpace(manifest.PublicURL) != "" {
		return errors.New("public_url must be empty when tunnel_mode is none")
	}
	if manifest.TunnelMode == "named" {
		if err := validateHTTPURL(manifest.PublicURL, true); err != nil {
			return fmt.Errorf("invalid public_url: %w", err)
		}
	}
	// Quick Tunnel 地址要等 cloudflared ready 才有。trial 阶段允许先写 quick 且 public_url 为空，
	// 避免安装器为了通过校验把 tunnel-mode 改成 none，导致 Engine 根本不启动 Tunnel。
	if manifest.TunnelMode == "quick" && strings.TrimSpace(manifest.PublicURL) != "" {
		if err := validateHTTPURL(manifest.PublicURL, true); err != nil {
			return fmt.Errorf("invalid public_url: %w", err)
		}
	}
	return nil
}

func (manifest Manifest) UsesScheduledTask() bool {
	return manifest.PrivilegeMode == "elevated" && strings.TrimSpace(manifest.AgentDockTaskName) != ""
}

func (manifest Manifest) HealthURL() string {
	return fmt.Sprintf("http://%s:%d/healthz", manifest.Host, manifest.Port)
}

func validateHTTPURL(raw string, requireHTTPS bool) error {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return err
	}
	if parsed.Host == "" || parsed.User != nil || parsed.Fragment != "" {
		return errors.New("URL must contain only a valid HTTP origin and path")
	}
	if requireHTTPS && parsed.Scheme != "https" {
		return errors.New("URL must use https")
	}
	if !requireHTTPS && parsed.Scheme != "http" && parsed.Scheme != "https" {
		return errors.New("URL must use http or https")
	}
	return nil
}

func samePath(left, right string) bool {
	left = strings.TrimRight(filepath.Clean(left), `\/`)
	right = strings.TrimRight(filepath.Clean(right), `\/`)
	return strings.EqualFold(left, right)
}
