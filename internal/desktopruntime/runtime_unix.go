//go:build darwin || linux

package desktopruntime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/uvwt/agentdock/internal/envstore"
	"github.com/uvwt/agentdock/internal/fs/atomicfile"
)

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

func darwinExecutableFromAppBundle(executable string) bool {
	return strings.Contains(filepath.ToSlash(executable), ".app/Contents/")
}

func loadUnixRuntime(runtimeRoot string) (unixRuntimeManifest, string, error) {
	root, err := filepath.Abs(strings.TrimSpace(runtimeRoot))
	if err != nil || root == "" {
		return unixRuntimeManifest{}, "", errors.New("runtime-root 无效")
	}
	home, _ := os.UserHomeDir()
	agentDockBinary := filepath.Join(home, ".local", "bin", "agentdock")
	cloudflaredBinary := filepath.Join(home, ".local", "bin", "cloudflared")
	serviceName := "agentdock"
	tunnelServiceName := "agentdock-cloudflared"
	serviceManager := ""
	executable := ""
	if resolved, err := os.Executable(); err == nil {
		if eval, evalErr := filepath.EvalSymlinks(resolved); evalErr == nil {
			resolved = eval
		}
		executable = resolved
	}
	fromApp := runtime.GOOS == "darwin" && darwinExecutableFromAppBundle(executable)
	if runtime.GOOS == "darwin" {
		if fromApp {
			// App Bundle 的 Core/cloudflared 由 SMAppService 注册，路径必须跟随 Helper。
			if executable != "" {
				agentDockBinary = executable
				cloudflaredBinary = filepath.Join(filepath.Dir(executable), "cloudflared")
			}
			serviceManager = "smappservice"
			serviceName = "com.uvwt.agentdock.core"
			tunnelServiceName = "com.uvwt.agentdock.tunnel"
		} else {
			// CLI 安装把 LaunchAgent 写在用户目录，标签是 com.uvwt.agentdock / .cloudflared。
			if executable != "" {
				agentDockBinary = executable
				cloudflaredBinary = filepath.Join(filepath.Dir(executable), "cloudflared")
			}
			serviceManager = "launchd"
			serviceName = "com.uvwt.agentdock"
			tunnelServiceName = "com.uvwt.agentdock.cloudflared"
		}
	}
	manifest := unixRuntimeManifest{
		SchemaVersion:     1,
		ServiceManager:    serviceManager,
		ServiceName:       serviceName,
		TunnelServiceName: tunnelServiceName,
		AgentDockBinary:   agentDockBinary,
		CloudflaredBinary: cloudflaredBinary,
		EnvironmentFile:   filepath.Join(root, "agentdock.env"),
		TunnelEnvironment: filepath.Join(root, "cloudflared.env"),
	}
	// App Bundle 不允许外部清单改写已签名 Helper 路径。CLI / Linux 读取 desktop-runtime.json。
	if runtime.GOOS != "darwin" || !fromApp {
		data, readErr := os.ReadFile(filepath.Join(root, "desktop-runtime.json"))
		if readErr == nil {
			if err := json.Unmarshal(data, &manifest); err != nil {
				return unixRuntimeManifest{}, "", fmt.Errorf("解析桌面运行清单失败: %w", err)
			}
		} else if !errors.Is(readErr, os.ErrNotExist) {
			return unixRuntimeManifest{}, "", fmt.Errorf("读取桌面运行清单失败: %w", readErr)
		}
	}
	if manifest.AgentDockBinary == "" || manifest.EnvironmentFile == "" {
		return unixRuntimeManifest{}, "", errors.New("桌面运行清单缺少核心路径")
	}
	return manifest, root, nil
}

func platformPrepareCoreEnvironment(runtimeRoot string) error {
	manifest, root, err := loadUnixRuntime(runtimeRoot)
	if err != nil {
		return err
	}
	values, err := envstore.ParseFile(manifest.EnvironmentFile)
	if err != nil {
		// Linux systemd 可通过 EnvironmentFile 在降权前注入配置；服务用户无权读取
		// root:root 0600 文件时，保留已经注入的环境变量。
		if runtime.GOOS != "linux" || os.Getenv("AGENTDOCK_AUTH_TOKEN") == "" {
			return err
		}
		values = map[string]string{}
	}
	for key, value := range values {
		if err := os.Setenv(key, value); err != nil {
			return fmt.Errorf("设置 %s 失败: %w", key, err)
		}
	}
	if err := os.Setenv("AGENTDOCK_RUNTIME_ROOT", root); err != nil {
		return err
	}
	return platformPrepareLaunchEnvironment("core")
}

func loadCoreEnvironment(runtimeRoot string) (unixRuntimeManifest, string, map[string]string, error) {
	manifest, root, err := loadUnixRuntime(runtimeRoot)
	if err != nil {
		return unixRuntimeManifest{}, "", nil, err
	}
	values, err := envstore.ParseFile(manifest.EnvironmentFile)
	if err != nil {
		return unixRuntimeManifest{}, "", nil, err
	}
	return manifest, root, values, nil
}

func writeEnvironment(path string, values map[string]string) error {
	if err := atomicfile.Write(path, envstore.Marshal(values), 0o600); err != nil {
		return fmt.Errorf("写入环境变量文件失败: %w", err)
	}
	return nil
}

func healthURL(values map[string]string) string {
	host := strings.TrimSpace(values["AGENTDOCK_HOST"])
	if host == "" || host == "0.0.0.0" {
		host = "127.0.0.1"
	} else if host == "::" || host == "[::]" {
		host = "::1"
	}
	port, err := strconv.Atoi(values["AGENTDOCK_PORT"])
	if err != nil || port < 1 || port > 65535 {
		port = 8765
	}
	return "http://" + net.JoinHostPort(host, strconv.Itoa(port)) + "/healthz"
}

func healthy(ctx context.Context, values map[string]string) bool {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, healthURL(values), nil)
	if err != nil {
		return false
	}
	client := http.Client{Timeout: 2 * time.Second}
	response, err := client.Do(request)
	if err != nil {
		return false
	}
	defer response.Body.Close()
	return response.StatusCode == http.StatusOK
}

func waitHealthy(ctx context.Context, values map[string]string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if healthy(ctx, values) {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(500 * time.Millisecond):
		}
	}
	return errors.New("AgentDock 健康检查超时")
}

func runCommand(ctx context.Context, name string, args ...string) (string, error) {
	command := exec.CommandContext(ctx, name, args...)
	output, err := command.CombinedOutput()
	if err != nil {
		message := strings.TrimSpace(string(output))
		if message == "" {
			message = err.Error()
		}
		return message, fmt.Errorf("%s: %s", filepath.Base(name), message)
	}
	return strings.TrimSpace(string(output)), nil
}
