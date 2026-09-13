//go:build windows

package desktopruntime

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"

	"github.com/uvwt/agentdock/internal/fs/atomicfile"
	"github.com/uvwt/agentdock/internal/fs/filelock"
)

const (
	tunnelTokenEntropy             = "agentdock.cloudflare.tunnel.v1"
	generatedCredentialLockTimeout = 30 * time.Second
)

type tunnelFiles struct {
	manifest       string
	mode           string
	serverURL      string
	namedServerURL string
	quickURL       string
	token          string
	stdoutLog      string
	stderrLog      string
}

type tunnelRuntime struct {
	manifest Manifest
	root     string
	settings controlPanelSettings
	files    tunnelFiles
	mode     string
}

func loadTunnelRuntime(runtimeRoot string) (tunnelRuntime, error) {
	manifest, root, err := loadDesktopManifest(runtimeRoot)
	if err != nil {
		return tunnelRuntime{}, err
	}
	settings, err := loadControlPanelSettings(root, manifest.Port)
	if err != nil {
		return tunnelRuntime{}, err
	}
	if strings.TrimSpace(manifest.CloudflaredBinary) == "" {
		manifest.CloudflaredBinary = filepath.Join(root, "bin", "cloudflared.exe")
	}
	files := tunnelFiles{
		manifest:       filepath.Join(root, "runtime.json"),
		mode:           filepath.Join(root, "cloudflared-mode.txt"),
		serverURL:      filepath.Join(root, "server-url.txt"),
		namedServerURL: filepath.Join(root, "named-server-url.txt"),
		quickURL:       filepath.Join(root, "quick-tunnel-url.txt"),
		token:          filepath.Join(root, "cloudflared-token.dpapi"),
		stdoutLog:      filepath.Join(root, "cloudflared.out.log"),
		stderrLog:      filepath.Join(root, "cloudflared.err.log"),
	}
	mode, err := readTunnelMode(files.mode, manifest.TunnelMode)
	if err != nil {
		return tunnelRuntime{}, err
	}
	return tunnelRuntime{manifest: manifest, root: root, settings: settings, files: files, mode: mode}, nil
}

func readTunnelMode(path, fallback string) (string, error) {
	mode, err := readTrimmedText(path)
	if err != nil {
		return "", err
	}
	if mode == "" {
		mode = fallback
	}
	mode = strings.ToLower(strings.TrimSpace(mode))
	if mode == "" {
		mode = "none"
	}
	if mode != "none" && mode != "quick" && mode != "named" {
		return "", fmt.Errorf("不支持的 Cloudflare Tunnel 模式：%s", mode)
	}
	return mode, nil
}

func (runtime tunnelRuntime) updateManifest(mode, publicURL string) error {
	runtime.manifest.Host = "127.0.0.1"
	runtime.manifest.Port = runtime.settings.Port
	runtime.manifest.LocalMCPURL = "http://127.0.0.1:" + strconv.Itoa(runtime.settings.Port) + "/mcp"
	runtime.manifest.TunnelMode = mode
	runtime.manifest.PublicURL = strings.TrimSpace(publicURL)
	return Save(runtime.files.manifest, runtime.manifest)
}

func writeRuntimeText(path, value string) error {
	if err := atomicfile.Write(path, []byte(strings.TrimSpace(value)), 0o600); err != nil {
		return fmt.Errorf("写入 %s 失败: %w", path, err)
	}
	return nil
}

func clearActivePublicURL(files tunnelFiles) error {
	if err := writeRuntimeText(files.serverURL, ""); err != nil {
		return err
	}
	if err := os.Remove(files.quickURL); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("删除 Quick Tunnel ready 文件失败: %w", err)
	}
	return nil
}

func preserveNamedServerURL(runtime tunnelRuntime) error {
	if runtime.mode != "named" {
		return nil
	}
	serverURL, err := readTrimmedText(runtime.files.serverURL)
	if err != nil {
		return err
	}
	if serverURL == "" {
		return nil
	}
	return writeRuntimeText(runtime.files.namedServerURL, serverURL)
}

func normalizeHTTPSOrigin(value string) (string, error) {
	candidate := strings.TrimSpace(strings.TrimRight(value, "/"))
	if candidate == "" {
		return "", errors.New("固定域名模式需要 HTTPS 公网地址")
	}
	parsed, err := url.Parse(candidate)
	if err != nil || !parsed.IsAbs() || parsed.Scheme != "https" || parsed.Host == "" {
		return "", fmt.Errorf("公网地址必须是完整 HTTPS Origin：%s", value)
	}
	path := strings.TrimRight(parsed.EscapedPath(), "/")
	if parsed.User != nil || (path != "" && !strings.EqualFold(path, "/mcp")) || parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", fmt.Errorf("公网地址不能包含路径、查询参数、片段或用户信息：%s", value)
	}
	return "https://" + parsed.Host, nil
}

func readSecretFile(path string) (string, error) {
	if strings.TrimSpace(path) == "" {
		return "", nil
	}
	info, err := os.Stat(path)
	if err != nil {
		return "", fmt.Errorf("读取临时凭据文件失败: %w", err)
	}
	if !info.Mode().IsRegular() || info.Size() > 64*1024 {
		return "", errors.New("临时凭据文件无效")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("读取临时凭据文件失败: %w", err)
	}
	value := strings.TrimSpace(string(data))
	if value == "" {
		return "", errors.New("临时凭据文件为空")
	}
	return value, nil
}

// readOrCreateProtectedText 读取可自动轮换的 DPAPI 凭据。锁必须覆盖读取、解密、
// 生成和持久化整个流程；否则两个 launch-core 并发恢复时，进程可能拿到不同于磁盘最终值的凭据。
func readOrCreateProtectedText(path, entropy string, byteCount int, name string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), generatedCredentialLockTimeout)
	defer cancel()
	release, err := filelock.Acquire(ctx, path+".lock")
	if err != nil {
		return "", fmt.Errorf("锁定 %s 失败: %w", name, err)
	}
	defer release()

	// 获得锁后重新读取。等待锁期间，另一个进程可能已经完成了创建或损坏恢复。
	data, err := os.ReadFile(path)
	if err == nil && strings.TrimSpace(string(data)) != "" {
		if value, decryptErr := readProtectedText(path, entropy); decryptErr == nil && strings.TrimSpace(value) != "" {
			return value, nil
		}
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return "", fmt.Errorf("读取 %s 失败: %w", name, err)
	}
	value, err := randomHex(byteCount)
	if err != nil {
		return "", fmt.Errorf("生成 %s 失败: %w", name, err)
	}
	if err := writeProtectedText(path, value, entropy); err != nil {
		return "", fmt.Errorf("保存 %s 失败: %w", name, err)
	}
	return value, nil
}

func ensureDesktopCredentials(runtimeRoot string) error {
	credentials := []struct {
		name    string
		path    string
		entropy string
		bytes   int
	}{
		{name: "Bearer Token", path: filepath.Join(runtimeRoot, "auth-token.dpapi"), entropy: "agentdock.startup.v1", bytes: 32},
		{name: "OAuth 密码", path: filepath.Join(runtimeRoot, "oauth-password.dpapi"), entropy: "agentdock.oauth.password.v1", bytes: 12},
		{name: "OAuth 签名密钥", path: filepath.Join(runtimeRoot, "oauth-token-secret.dpapi"), entropy: "agentdock.oauth.secret.v1", bytes: 32},
	}
	for _, credential := range credentials {
		if _, err := readOrCreateProtectedText(credential.path, credential.entropy, credential.bytes, credential.name); err != nil {
			return err
		}
	}
	return nil
}

func randomHex(byteCount int) (string, error) {
	data := make([]byte, byteCount)
	if _, err := rand.Read(data); err != nil {
		return "", err
	}
	return hex.EncodeToString(data), nil
}

func writeProtectedText(path, value, entropy string) error {
	plain := []byte(value)
	if len(plain) == 0 {
		return errors.New("DPAPI 明文不能为空")
	}
	entropyBytes := []byte(entropy)
	input := windows.DataBlob{Size: uint32(len(plain)), Data: &plain[0]}
	optionalEntropy := windows.DataBlob{Size: uint32(len(entropyBytes)), Data: &entropyBytes[0]}
	var output windows.DataBlob
	if err := windows.CryptProtectData(&input, nil, &optionalEntropy, 0, nil, 0, &output); err != nil {
		return err
	}
	defer windows.LocalFree(windows.Handle(uintptr(unsafe.Pointer(output.Data))))
	protected := unsafe.Slice(output.Data, int(output.Size))
	encoded := base64.StdEncoding.EncodeToString(append([]byte(nil), protected...))
	return atomicfile.Write(path, []byte(encoded), 0o600)
}
