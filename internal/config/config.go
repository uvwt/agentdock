package config

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/uvwt/agentdock/internal/securepath"
)

const (
	ProtocolVersion    = "2025-06-18"
	ServerName         = "agentdock"
	Version            = "0.5.3"
	PathModel          = "host"
	BrowserRunnerDir   = "browser-runner"
	BrowserArtifactDir = "browser-artifacts"
	RecallTimeoutMS    = 30000
)

type Config struct {
	AgentDockHome       string
	AgentDockDefaultDir string
	Host                string
	Port                int
	AuthToken           string
	OAuthEnabled        bool
	OAuthServerURL      string
	LogLevel            string
	NexusEndpoint       string
	NexusToken          string
	BrowserEnabled      bool
	BrowserRunnerDir    string
	Stdio               bool
	TrustedProxyCIDRs   []string
}

func FromEnv() (Config, error) {
	port, err := getenvInt("AGENTDOCK_PORT", 8765)
	if err != nil {
		return Config{}, err
	}
	browserEnabled, err := getenvBool("AGENTDOCK_BROWSER_ENABLED", false)
	if err != nil {
		return Config{}, err
	}
	oauthEnabled, err := getenvBool("AGENTDOCK_OAUTH_ENABLED", false)
	if err != nil {
		return Config{}, err
	}
	stdio, err := getenvBool("AGENTDOCK_STDIO", false)
	if err != nil {
		return Config{}, err
	}
	return Config{
		Host:              getenv("AGENTDOCK_HOST", "127.0.0.1"),
		Port:              port,
		AuthToken:         os.Getenv("AGENTDOCK_AUTH_TOKEN"),
		OAuthEnabled:      oauthEnabled,
		OAuthServerURL:    os.Getenv("AGENTDOCK_SERVER_URL"),
		LogLevel:          getenv("AGENTDOCK_LOG_LEVEL", "info"),
		NexusEndpoint:     getenv("AGENTDOCK_NEXUS_ENDPOINT", ""),
		NexusToken:        os.Getenv("AGENTDOCK_NEXUS_TOKEN"),
		BrowserEnabled:    browserEnabled,
		BrowserRunnerDir:  os.Getenv("AGENTDOCK_BROWSER_RUNNER_DIR"),
		Stdio:             stdio,
		TrustedProxyCIDRs: splitCommaSeparated(os.Getenv("AGENTDOCK_TRUSTED_PROXY_CIDRS")),
	}, nil
}

func (c *Config) Normalize() error {
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("resolve user home for AgentDock directories: %w", err)
	}
	if strings.TrimSpace(home) == "" {
		return errors.New("resolve user home for AgentDock directories: home directory is empty")
	}
	if c.AgentDockHome == "" {
		c.AgentDockHome = filepath.Join(home, ".agentdock")
	}
	if c.AgentDockDefaultDir == "" {
		c.AgentDockDefaultDir = filepath.Join(home, "AgentDock")
	}
	paths := []struct {
		label string
		value *string
	}{
		{label: "AgentDockHome", value: &c.AgentDockHome},
		{label: "AgentDockDefaultDir", value: &c.AgentDockDefaultDir},
	}
	for _, path := range paths {
		cleaned := filepath.Clean(strings.TrimSpace(*path.value))
		if !filepath.IsAbs(cleaned) {
			return fmt.Errorf("%s must resolve to an absolute path: %s", path.label, cleaned)
		}
		if err := os.MkdirAll(cleaned, 0o700); err != nil {
			return fmt.Errorf("create %s %s: %w", path.label, cleaned, err)
		}
		info, err := os.Stat(cleaned)
		if err != nil {
			return fmt.Errorf("stat %s %s: %w", path.label, cleaned, err)
		}
		if !info.IsDir() {
			return fmt.Errorf("%s is not a directory: %s", path.label, cleaned)
		}
		if err := securepath.EnsurePrivate(cleaned); err != nil {
			return fmt.Errorf("secure %s %s: %w", path.label, cleaned, err)
		}
		*path.value = cleaned
	}
	if strings.TrimSpace(c.BrowserRunnerDir) == "" {
		c.BrowserRunnerDir = filepath.Join(c.AgentDockHome, BrowserRunnerDir)
	} else {
		c.BrowserRunnerDir = filepath.Clean(strings.TrimSpace(c.BrowserRunnerDir))
		if !filepath.IsAbs(c.BrowserRunnerDir) {
			return fmt.Errorf("BrowserRunnerDir must resolve to an absolute path: %s", c.BrowserRunnerDir)
		}
	}
	c.Host = strings.TrimSpace(c.Host)
	if c.Host == "" {
		c.Host = "127.0.0.1"
	}
	c.OAuthServerURL = strings.TrimSpace(c.OAuthServerURL)
	if c.Port == 0 {
		c.Port = 8765
	}
	if c.Port < 1 || c.Port > 65535 {
		return fmt.Errorf("port must be between 1 and 65535: %d", c.Port)
	}
	c.LogLevel = strings.ToLower(strings.TrimSpace(c.LogLevel))
	if c.LogLevel == "" {
		c.LogLevel = "info"
	}
	if c.LogLevel == "warning" {
		c.LogLevel = "warn"
	}
	switch c.LogLevel {
	case "debug", "info", "warn", "error":
	default:
		return fmt.Errorf("unsupported log level %q; expected debug, info, warn, or error", c.LogLevel)
	}
	networks := make([]string, 0, len(c.TrustedProxyCIDRs))
	seenNetworks := map[string]struct{}{}
	for _, raw := range c.TrustedProxyCIDRs {
		_, network, err := net.ParseCIDR(strings.TrimSpace(raw))
		if err != nil {
			return fmt.Errorf("AGENTDOCK_TRUSTED_PROXY_CIDRS contains invalid CIDR %q: %w", raw, err)
		}
		canonical := network.String()
		if _, exists := seenNetworks[canonical]; exists {
			continue
		}
		seenNetworks[canonical] = struct{}{}
		networks = append(networks, canonical)
	}
	c.TrustedProxyCIDRs = networks
	return nil
}

func (c Config) AuthRequired() bool {
	return c.AuthToken != "" || c.OAuthEnabled
}

func (c Config) ValidateAuth() error {
	// stdio 不开放网络监听；HTTP 模式只允许回环地址在无认证下启动。
	// AgentDock 暴露命令和文件写入能力，非回环无认证不是可接受的默认配置。
	if !c.Stdio && !c.AuthRequired() && !isLoopbackBindHost(c.Host) {
		return fmt.Errorf("non-loopback host %q requires AGENTDOCK_AUTH_TOKEN or OAuth", c.Host)
	}
	if !c.OAuthEnabled {
		return nil
	}
	missing := []string{}
	if c.OAuthServerURL == "" {
		missing = append(missing, "AGENTDOCK_SERVER_URL")
	}
	if os.Getenv("AGENTDOCK_OAUTH_PASSWORD") == "" {
		missing = append(missing, "AGENTDOCK_OAUTH_PASSWORD")
	}
	if os.Getenv("AGENTDOCK_OAUTH_TOKEN_SECRET") == "" {
		missing = append(missing, "AGENTDOCK_OAUTH_TOKEN_SECRET")
	}
	if len(missing) > 0 {
		return fmt.Errorf("OAuth enabled by AGENTDOCK_OAUTH_ENABLED but missing required environment variable(s): %s", strings.Join(missing, ", "))
	}
	password := os.Getenv("AGENTDOCK_OAUTH_PASSWORD")
	if len([]rune(password)) < 12 {
		return errors.New("AGENTDOCK_OAUTH_PASSWORD must contain at least 12 characters")
	}
	tokenSecret := os.Getenv("AGENTDOCK_OAUTH_TOKEN_SECRET")
	if len(tokenSecret) < 32 {
		return errors.New("AGENTDOCK_OAUTH_TOKEN_SECRET must contain at least 32 bytes")
	}
	serverURL, err := url.Parse(strings.TrimSpace(c.OAuthServerURL))
	if err != nil || serverURL.Scheme == "" || serverURL.Host == "" {
		return fmt.Errorf("AGENTDOCK_SERVER_URL must be an absolute HTTP(S) URL: %q", c.OAuthServerURL)
	}
	if serverURL.User != nil || serverURL.RawQuery != "" || serverURL.Fragment != "" {
		return fmt.Errorf("AGENTDOCK_SERVER_URL must not contain user info, a query, or a fragment: %q", c.OAuthServerURL)
	}
	if serverURL.Path != "" && serverURL.Path != "/" {
		return fmt.Errorf("AGENTDOCK_SERVER_URL must be an origin without a path: %q", c.OAuthServerURL)
	}
	if serverURL.Scheme == "https" {
		return nil
	}
	if serverURL.Scheme != "http" {
		return fmt.Errorf("AGENTDOCK_SERVER_URL must use https, or http for a loopback host: %q", c.OAuthServerURL)
	}
	hostname := strings.ToLower(serverURL.Hostname())
	if hostname != "localhost" {
		ip := net.ParseIP(hostname)
		if ip == nil || !ip.IsLoopback() {
			return fmt.Errorf("AGENTDOCK_SERVER_URL must use https for non-loopback hosts: %q", c.OAuthServerURL)
		}
	}
	return nil
}

func isLoopbackBindHost(host string) bool {
	host = strings.TrimSpace(strings.Trim(strings.TrimSpace(host), "[]"))
	if host == "" {
		return false
	}
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func splitCommaSeparated(value string) []string {
	parts := strings.Split(value, ",")
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		if cleaned := strings.TrimSpace(part); cleaned != "" {
			result = append(result, cleaned)
		}
	}
	return result
}

func getenv(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func getenvInt(key string, fallback int) (int, error) {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback, nil
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return 0, fmt.Errorf("parse %s as integer: %w", key, err)
	}
	return parsed, nil
}

func getenvBool(key string, fallback bool) (bool, error) {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback, nil
	}
	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return false, fmt.Errorf("parse %s as boolean: %w", key, err)
	}
	return parsed, nil
}
