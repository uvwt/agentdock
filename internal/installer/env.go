package installer

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/uvwt/agentdock/internal/envstore"
	"github.com/uvwt/agentdock/internal/fs/atomicfile"
)

// hydrateExistingRuntime 把调用方省略的运行时选择补成当前已提交配置。
// install/repair 的空字段表示“保留”，不能要求 shell adapter 先读 env 再把
// mode/token/server URL 回填给 Engine；否则同一安装状态会有两个决策源。
func hydrateExistingRuntime(request Request) (Request, error) {
	coreEnv := filepath.Join(request.RuntimeRoot, "agentdock.env")
	tunnelEnv := filepath.Join(request.RuntimeRoot, "cloudflared.env")

	core, err := envstore.ParseFile(coreEnv)
	if err != nil {
		// hydration 只负责补全“省略=保留”的已有选择，不能成为新的事务外失败点。
		// 损坏/不可读的 env 仍由 activate 阶段的权威写入路径报错并触发 journal rollback。
		core = map[string]string{}
	}
	tunnel, err := envstore.ParseFile(tunnelEnv)
	if err != nil {
		tunnel = map[string]string{}
	}

	if strings.TrimSpace(request.TunnelMode) == "" {
		mode := strings.TrimSpace(tunnel["AGENTDOCK_TUNNEL_MODE"])
		switch mode {
		case "none", "quick", "named":
			request.TunnelMode = mode
		case "":
			// 首次安装没有 Tunnel 状态时保持“未指定”；activate 会按本机模式落盘。
		default:
			return Request{}, fmt.Errorf("已有 Tunnel 模式无效: %s", mode)
		}
	}
	if request.TunnelMode == "named" {
		if strings.TrimSpace(request.ServerURL) == "" {
			request.ServerURL = strings.TrimSpace(core["AGENTDOCK_SERVER_URL"])
		}
		if strings.TrimSpace(request.TunnelToken) == "" && strings.TrimSpace(request.TunnelTokenFile) == "" {
			request.TunnelToken = strings.TrimSpace(tunnel["TUNNEL_TOKEN"])
		}
	}
	return request, nil
}

var alwaysRemovedEnvKeys = []string{
	"AGENTDOCK_NEXUS_ENDPOINT",
	"AGENTDOCK_NEXUS_TOKEN",
}

func writeCoreEnvironment(path string, request Request) error {
	existing := map[string]string{}
	if values, err := envstore.ParseFile(path); err == nil {
		existing = values
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}

	for _, key := range alwaysRemovedEnvKeys {
		delete(existing, key)
	}

	if strings.TrimSpace(request.Host) != "" {
		existing["AGENTDOCK_HOST"] = request.Host
	} else if strings.TrimSpace(existing["AGENTDOCK_HOST"]) == "" {
		existing["AGENTDOCK_HOST"] = "127.0.0.1"
	}
	if request.Port != 0 {
		existing["AGENTDOCK_PORT"] = fmt.Sprintf("%d", request.Port)
	} else if strings.TrimSpace(existing["AGENTDOCK_PORT"]) == "" {
		existing["AGENTDOCK_PORT"] = "8765"
	}
	if strings.TrimSpace(request.LogLevel) != "" {
		existing["AGENTDOCK_LOG_LEVEL"] = request.LogLevel
	} else if strings.TrimSpace(existing["AGENTDOCK_LOG_LEVEL"]) == "" {
		existing["AGENTDOCK_LOG_LEVEL"] = "info"
	}

	token, err := resolveSecret(existing["AGENTDOCK_AUTH_TOKEN"], request.AuthToken, false, 32)
	if err != nil {
		return fmt.Errorf("auth token: %w", err)
	}
	existing["AGENTDOCK_AUTH_TOKEN"] = token

	switch request.TunnelMode {
	case "named":
		existing["AGENTDOCK_SERVER_URL"] = strings.TrimSpace(request.ServerURL)
	case "quick":
		// Quick Tunnel 在拿到临时地址前必须把 Origin 写成空字符串，不能沿用上次 Named 域名。
		existing["AGENTDOCK_SERVER_URL"] = strings.TrimSpace(request.ServerURL)
	case "none":
		delete(existing, "AGENTDOCK_SERVER_URL")
	}

	if err := applyOAuth(existing, request); err != nil {
		return err
	}
	return atomicfile.Write(path, marshalInstallEnv(existing), 0o600)
}

func applyOAuth(existing map[string]string, request Request) error {
	// 公网认证只在“这次确实会暴露 Origin”时开启。
	// Quick Tunnel 在 --no-start / 尚无临时地址时不得把 OAUTH_ENABLED 写成 true。
	public := false
	switch request.TunnelMode {
	case "named":
		public = true
	case "quick":
		public = strings.TrimSpace(request.ServerURL) != ""
	case "":
		public = strings.EqualFold(strings.TrimSpace(existing["AGENTDOCK_OAUTH_ENABLED"]), "true")
	}

	if public {
		existing["AGENTDOCK_OAUTH_ENABLED"] = "true"
	} else if request.TunnelMode != "" {
		existing["AGENTDOCK_OAUTH_ENABLED"] = "false"
	}

	generate := 0
	if public {
		generate = 12
	}
	password, err := resolveSecret(existing["AGENTDOCK_OAUTH_PASSWORD"], request.OAuthPassword, request.RotateOAuth, generate)
	if err != nil {
		return fmt.Errorf("oauth password: %w", err)
	}
	secretBytes := 0
	if public {
		secretBytes = 32
	}
	secret, err := resolveSecret(existing["AGENTDOCK_OAUTH_TOKEN_SECRET"], request.OAuthTokenSecret, request.RotateOAuth, secretBytes)
	if err != nil {
		return fmt.Errorf("oauth token secret: %w", err)
	}
	if public && (password == "" || secret == "") {
		return errors.New("公网认证缺少 OAuth 凭据；请提供已有值或 --rotate-oauth")
	}
	if password != "" {
		existing["AGENTDOCK_OAUTH_PASSWORD"] = password
	}
	if secret != "" {
		existing["AGENTDOCK_OAUTH_TOKEN_SECRET"] = secret
	}
	return nil
}

func resolveSecret(existing string, specified OptionalString, rotate bool, generateIfMissing int) (string, error) {
	if specified.Set {
		return strings.TrimSpace(specified.Value), nil
	}
	if rotate {
		if generateIfMissing <= 0 {
			generateIfMissing = 32
		}
		return randomHex(generateIfMissing)
	}
	if strings.TrimSpace(existing) != "" {
		return existing, nil
	}
	if generateIfMissing > 0 {
		return randomHex(generateIfMissing)
	}
	return "", nil
}

func writeTunnelEnvironment(path, mode, target, token string) error {
	values := map[string]string{
		"AGENTDOCK_TUNNEL_MODE":   mode,
		"AGENTDOCK_TUNNEL_TARGET": target,
		"TUNNEL_TOKEN":            token,
	}
	return atomicfile.Write(path, marshalInstallEnv(values), 0o600)
}

func marshalInstallEnv(values map[string]string) []byte {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	var output strings.Builder
	for _, key := range keys {
		output.WriteString(key)
		output.WriteByte('=')
		output.WriteString(encodeEnvValue(values[key]))
		output.WriteByte('\n')
	}
	return []byte(output.String())
}

func encodeEnvValue(value string) string {
	if value == "" {
		return "''"
	}
	for i := 0; i < len(value); i++ {
		c := value[i]
		safe := (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') ||
			c == '.' || c == '_' || c == '-' || c == '/' || c == ':' || c == '@' || c == '+'
		if !safe {
			return backslashEnv(value)
		}
	}
	return value
}

func backslashEnv(value string) string {
	var output strings.Builder
	for _, r := range value {
		switch r {
		case ' ', '\t', '\'', '"', '\\', '$', '`', '\n':
			output.WriteByte('\\')
		}
		output.WriteRune(r)
	}
	return output.String()
}
