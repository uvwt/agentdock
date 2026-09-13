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
	"sort"
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
	generatedCredentialRetryDelay  = 50 * time.Millisecond
	generatedCredentialRetryCount  = 3
	generatedCredentialBackupLimit = 3
	credentialOwnerSIDFile         = "credential-owner-sid.txt"
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
// 备份、生成和持久化整个流程；否则并发恢复可能让进程值与最终磁盘值不一致。
func readOrCreateProtectedText(path, entropy string, byteCount int, name string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), generatedCredentialLockTimeout)
	defer cancel()
	release, err := filelock.Acquire(ctx, path+".lock")
	if err != nil {
		return "", fmt.Errorf("锁定 %s 失败: %w", name, err)
	}
	defer release()

	data, err := os.ReadFile(path)
	if err == nil && strings.TrimSpace(string(data)) != "" {
		if value, decryptErr := readProtectedTextWithRetry(path, entropy); decryptErr == nil {
			if err := bindCredentialOwnerSID(path); err != nil {
				return "", fmt.Errorf("记录 %s Windows 用户绑定失败: %w", name, err)
			}
			return value, nil
		}
		if err := validateCredentialRecoveryUser(path); err != nil {
			return "", fmt.Errorf("%s 无法自动恢复: %w", name, err)
		}
		if err := bindCredentialOwnerSID(path); err != nil {
			return "", fmt.Errorf("记录 %s Windows 用户绑定失败: %w", name, err)
		}
		if err := backupUnreadableProtectedText(path, data); err != nil {
			return "", fmt.Errorf("备份不可读的 %s 失败: %w", name, err)
		}
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return "", fmt.Errorf("读取 %s 失败: %w", name, err)
	} else {
		if err := validateCredentialRecoveryUser(path); err != nil {
			return "", fmt.Errorf("%s 无法自动创建: %w", name, err)
		}
		if err := bindCredentialOwnerSID(path); err != nil {
			return "", fmt.Errorf("记录 %s Windows 用户绑定失败: %w", name, err)
		}
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

func readProtectedTextWithRetry(path, entropy string) (string, error) {
	var lastErr error
	for attempt := 0; attempt < generatedCredentialRetryCount; attempt++ {
		value, err := readProtectedText(path, entropy)
		if err == nil && strings.TrimSpace(value) != "" {
			return value, nil
		}
		if err == nil {
			err = errors.New("DPAPI 明文为空")
		}
		lastErr = err
		if attempt+1 < generatedCredentialRetryCount {
			time.Sleep(generatedCredentialRetryDelay)
		}
	}
	return "", lastErr
}

func validateCredentialRecoveryUser(path string) error {
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return fmt.Errorf("读取当前 Windows 用户 SID 失败: %w", err)
	}
	currentSID := user.User.Sid.String()
	markerPath := filepath.Join(filepath.Dir(path), credentialOwnerSIDFile)
	marker, err := readTrimmedText(markerPath)
	if err != nil {
		return fmt.Errorf("读取凭据用户绑定失败: %w", err)
	}
	if marker != "" {
		if !strings.EqualFold(marker, currentSID) {
			return fmt.Errorf("当前 Windows 用户 SID %s 与凭据绑定 SID 不一致", currentSID)
		}
		return nil
	}

	// 旧安装没有 SID marker。仅当 DACL 明确包含当前用户 SID 时才允许自动恢复；
	// 只通过 Administrators 组获得访问权，不能证明 CurrentUser DPAPI 上下文一致。
	target := path
	if _, err := os.Stat(target); errors.Is(err, os.ErrNotExist) {
		target = filepath.Dir(path)
	} else if err != nil {
		return fmt.Errorf("检查凭据路径失败: %w", err)
	}
	matched, err := fileACLContainsSID(target, user.User.Sid)
	if err != nil {
		return fmt.Errorf("检查凭据 Windows ACL 失败: %w", err)
	}
	if !matched {
		return errors.New("无法确认当前 Windows 用户拥有此凭据，拒绝自动覆盖")
	}
	return nil
}

func fileACLContainsSID(path string, sid *windows.SID) (bool, error) {
	descriptor, err := windows.GetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		return false, err
	}
	dacl, _, err := descriptor.DACL()
	if err != nil {
		return false, err
	}
	if dacl == nil {
		return false, nil
	}
	for i := uint16(0); i < dacl.AceCount; i++ {
		var ace *windows.ACCESS_ALLOWED_ACE
		if err := windows.GetAce(dacl, uint32(i), &ace); err != nil {
			return false, err
		}
		if ace.Header.AceType != windows.ACCESS_ALLOWED_ACE_TYPE {
			continue
		}
		aceSID := (*windows.SID)(unsafe.Pointer(&ace.SidStart))
		if aceSID.Equals(sid) {
			return true, nil
		}
	}
	return false, nil
}

func bindCredentialOwnerSID(path string) error {
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return err
	}
	currentSID := user.User.Sid.String()
	markerPath := filepath.Join(filepath.Dir(path), credentialOwnerSIDFile)
	existing, err := readTrimmedText(markerPath)
	if err != nil {
		return err
	}
	if existing != "" && !strings.EqualFold(existing, currentSID) {
		return fmt.Errorf("凭据绑定 SID %s 与当前 Windows 用户 SID %s 不一致", existing, currentSID)
	}
	if strings.EqualFold(existing, currentSID) {
		return nil
	}
	return atomicfile.Write(markerPath, []byte(currentSID), 0o600)
}

func backupUnreadableProtectedText(path string, data []byte) error {
	if len(data) == 0 {
		return nil
	}
	backupPath := path + ".unreadable-" + time.Now().UTC().Format("20060102T150405.000000000Z") + ".bak"
	if err := atomicfile.Write(backupPath, append([]byte(nil), data...), 0o600); err != nil {
		return err
	}
	matches, err := filepath.Glob(path + ".unreadable-*.bak")
	if err != nil {
		return err
	}
	sort.Strings(matches)
	for len(matches) > generatedCredentialBackupLimit {
		if err := os.Remove(matches[0]); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		matches = matches[1:]
	}
	return nil
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
