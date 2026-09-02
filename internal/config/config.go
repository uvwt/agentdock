package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/uvwt/agentdock/internal/fs/securepath"
)

const (
	ProtocolVersion = "2026-08-11"
	ServerName      = "agentdock"
	PathModel       = "host"
	RecallTimeoutMS = 30000

	maxInstructionsFileBytes = 64 << 10

	defaultOAuthAccessTokenTTLSeconds = int64(time.Hour / time.Second)
	maxOAuthAccessTokenTTLSeconds     = int64(999999 * 24 * 60 * 60)
)

type Config struct {
	AgentDockHome                string
	AgentDockDefaultDir          string
	CommandEnvFromEnv            map[string]string
	Host                         string
	Port                         int
	AuthToken                    string
	OAuthEnabled                 bool
	OAuthServerURL               string
	OAuthAccessTokenTTLSeconds   int64
	OAuthAccessTokenNeverExpires bool
	LogLevel                     string
	NexusEndpoint                string
	NexusDeviceToken             string
	MCPAppsEnabled               bool
	BrowserEnabled               bool
	BrowserExecutablePath        string
	BrowserCDPURL                string
	BrowserReuseExistingCDP      bool
	ACPEnabled                   bool
	ACPAgentName                 string
	ACPCommand                   string
	ACPArgs                      []string
	ACPEnvFromEnv                map[string]string
	ACPMaxPrompts                int
	ACPInteractionMS             int
	Stdio                        bool
	TrustedProxyCIDRs            []string
	InstructionsFile             string
	Instructions                 string
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
	browserReuseExistingCDP, err := getenvBool("AGENTDOCK_BROWSER_REUSE_EXISTING_CDP", false)
	if err != nil {
		return Config{}, err
	}
	oauthEnabled, err := getenvBool("AGENTDOCK_OAUTH_ENABLED", false)
	if err != nil {
		return Config{}, err
	}
	oauthAccessTokenTTLSeconds, oauthAccessTokenNeverExpires, err := getenvOAuthAccessTokenTTL("AGENTDOCK_OAUTH_ACCESS_TOKEN_TTL", defaultOAuthAccessTokenTTLSeconds)
	if err != nil {
		return Config{}, err
	}
	stdio, err := getenvBool("AGENTDOCK_STDIO", false)
	if err != nil {
		return Config{}, err
	}
	mcpAppsEnabled, err := getenvBool("AGENTDOCK_MCP_APPS_ENABLED", true)
	if err != nil {
		return Config{}, err
	}
	commandEnvFromEnv, err := getenvStringMapJSON("AGENTDOCK_COMMAND_ENV_FROM_ENV_JSON")
	if err != nil {
		return Config{}, err
	}
	acpEnabled, err := getenvBool("AGENTDOCK_ACP_ENABLED", false)
	if err != nil {
		return Config{}, err
	}
	var acpArgs []string
	var acpEnvFromEnv map[string]string
	acpAgentName := "claude"
	acpCommand := ""
	acpMaxPrompts := 2
	acpInteractionMS := 300000
	if acpEnabled {
		acpAgentName = getenv("AGENTDOCK_ACP_AGENT", acpAgentName)
		acpCommand = os.Getenv("AGENTDOCK_ACP_COMMAND")
		acpArgs, err = getenvStringSliceJSON("AGENTDOCK_ACP_ARGS_JSON")
		if err != nil {
			return Config{}, err
		}
		acpEnvFromEnv, err = getenvStringMapJSON("AGENTDOCK_ACP_ENV_FROM_ENV_JSON")
		if err != nil {
			return Config{}, err
		}
		acpMaxPrompts, err = getenvInt("AGENTDOCK_ACP_MAX_CONCURRENT_PROMPTS", acpMaxPrompts)
		if err != nil {
			return Config{}, err
		}
		acpInteractionMS, err = getenvInt("AGENTDOCK_ACP_INTERACTION_TIMEOUT_MS", acpInteractionMS)
		if err != nil {
			return Config{}, err
		}
	}
	return Config{
		AgentDockHome:                strings.TrimSpace(os.Getenv("AGENTDOCK_HOME")),
		AgentDockDefaultDir:          strings.TrimSpace(os.Getenv("AGENTDOCK_DEFAULT_DIR")),
		CommandEnvFromEnv:            commandEnvFromEnv,
		Host:                         getenv("AGENTDOCK_HOST", "127.0.0.1"),
		Port:                         port,
		AuthToken:                    os.Getenv("AGENTDOCK_AUTH_TOKEN"),
		OAuthEnabled:                 oauthEnabled,
		OAuthServerURL:               os.Getenv("AGENTDOCK_SERVER_URL"),
		OAuthAccessTokenTTLSeconds:   oauthAccessTokenTTLSeconds,
		OAuthAccessTokenNeverExpires: oauthAccessTokenNeverExpires,
		LogLevel:                     getenv("AGENTDOCK_LOG_LEVEL", "info"),
		MCPAppsEnabled:               mcpAppsEnabled,
		BrowserEnabled:               browserEnabled,
		BrowserExecutablePath:        os.Getenv("AGENTDOCK_BROWSER_EXECUTABLE_PATH"),
		BrowserCDPURL:                strings.TrimSpace(os.Getenv("AGENTDOCK_BROWSER_CDP_URL")),
		BrowserReuseExistingCDP:      browserReuseExistingCDP,
		ACPEnabled:                   acpEnabled,
		ACPAgentName:                 acpAgentName,
		ACPCommand:                   acpCommand,
		ACPArgs:                      acpArgs,
		ACPEnvFromEnv:                acpEnvFromEnv,
		ACPMaxPrompts:                acpMaxPrompts,
		ACPInteractionMS:             acpInteractionMS,
		Stdio:                        stdio,
		TrustedProxyCIDRs:            splitCommaSeparated(os.Getenv("AGENTDOCK_TRUSTED_PROXY_CIDRS")),
		InstructionsFile:             strings.TrimSpace(os.Getenv("AGENTDOCK_INSTRUCTIONS_FILE")),
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
	c.BrowserExecutablePath = strings.TrimSpace(c.BrowserExecutablePath)
	if c.BrowserExecutablePath != "" {
		c.BrowserExecutablePath = filepath.Clean(c.BrowserExecutablePath)
		if !filepath.IsAbs(c.BrowserExecutablePath) {
			return fmt.Errorf("BrowserExecutablePath must resolve to an absolute path: %s", c.BrowserExecutablePath)
		}
	}
	c.BrowserCDPURL = strings.TrimSpace(c.BrowserCDPURL)
	if c.BrowserCDPURL != "" {
		parsed, err := url.Parse(c.BrowserCDPURL)
		if err != nil || parsed.Host == "" {
			return fmt.Errorf("BrowserCDPURL must be an absolute CDP endpoint URL: %s", c.BrowserCDPURL)
		}
		switch strings.ToLower(parsed.Scheme) {
		case "http", "https", "ws", "wss":
		default:
			return fmt.Errorf("BrowserCDPURL must use http, https, ws, or wss: %s", c.BrowserCDPURL)
		}
	}
	c.InstructionsFile = strings.TrimSpace(c.InstructionsFile)
	if c.InstructionsFile != "" {
		c.InstructionsFile = filepath.Clean(c.InstructionsFile)
		if !filepath.IsAbs(c.InstructionsFile) {
			return fmt.Errorf("InstructionsFile must resolve to an absolute path: %s", c.InstructionsFile)
		}

		// 先检查文件类型再打开，避免误配设备或命名管道时在 Open 阶段阻塞。
		info, err := os.Stat(c.InstructionsFile)
		if err != nil {
			return fmt.Errorf("stat InstructionsFile %s: %w", c.InstructionsFile, err)
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("InstructionsFile must be a regular file: %s", c.InstructionsFile)
		}
		if info.Size() > maxInstructionsFileBytes {
			return fmt.Errorf("InstructionsFile %s exceeds %d bytes", c.InstructionsFile, maxInstructionsFileBytes)
		}

		file, err := os.Open(c.InstructionsFile)
		if err != nil {
			return fmt.Errorf("open InstructionsFile %s: %w", c.InstructionsFile, err)
		}
		defer file.Close()

		// Stat 只能约束检查瞬间的文件大小；读取仍限制为 max+1，避免文件并发增长时突破边界。
		data, err := io.ReadAll(io.LimitReader(file, int64(maxInstructionsFileBytes)+1))
		if err != nil {
			return fmt.Errorf("read InstructionsFile %s: %w", c.InstructionsFile, err)
		}
		if len(data) > maxInstructionsFileBytes {
			return fmt.Errorf("InstructionsFile %s exceeds %d bytes", c.InstructionsFile, maxInstructionsFileBytes)
		}
		if !utf8.Valid(data) {
			return fmt.Errorf("InstructionsFile must contain valid UTF-8: %s", c.InstructionsFile)
		}
		c.Instructions = strings.TrimSpace(string(data))
		if c.Instructions == "" {
			return fmt.Errorf("InstructionsFile must contain non-empty instructions: %s", c.InstructionsFile)
		}
	}
	if err := validateEnvironmentMapping(c.CommandEnvFromEnv); err != nil {
		return fmt.Errorf("AGENTDOCK_COMMAND_ENV_FROM_ENV_JSON: %w", err)
	}
	if err := c.normalizeACP(); err != nil {
		return err
	}
	c.Host = strings.TrimSpace(c.Host)
	if c.Host == "" {
		c.Host = "127.0.0.1"
	}
	c.OAuthServerURL = strings.TrimSpace(c.OAuthServerURL)
	if c.OAuthAccessTokenNeverExpires {
		c.OAuthAccessTokenTTLSeconds = 0
	} else if c.OAuthAccessTokenTTLSeconds == 0 {
		c.OAuthAccessTokenTTLSeconds = defaultOAuthAccessTokenTTLSeconds
	}
	if !c.OAuthAccessTokenNeverExpires && (c.OAuthAccessTokenTTLSeconds < int64(time.Minute/time.Second) || c.OAuthAccessTokenTTLSeconds > maxOAuthAccessTokenTTLSeconds) {
		return fmt.Errorf(
			"AGENTDOCK_OAUTH_ACCESS_TOKEN_TTL must be between 1m and 999999d: %ds",
			c.OAuthAccessTokenTTLSeconds,
		)
	}
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

func (c *Config) normalizeACP() error {
	if !c.ACPEnabled {
		c.ACPAgentName = "claude"
		c.ACPCommand = ""
		c.ACPArgs = nil
		c.ACPEnvFromEnv = nil
		c.ACPMaxPrompts = 2
		c.ACPInteractionMS = 300000
		return nil
	}
	c.ACPAgentName = strings.TrimSpace(c.ACPAgentName)
	if c.ACPAgentName == "" {
		c.ACPAgentName = "claude"
	}
	if c.ACPMaxPrompts == 0 {
		c.ACPMaxPrompts = 2
	}
	if c.ACPInteractionMS == 0 {
		c.ACPInteractionMS = 300000
	}
	if !validACPAgentName(c.ACPAgentName) {
		return fmt.Errorf("AGENTDOCK_ACP_AGENT must be a 1-64 character identifier using letters, numbers, dot, underscore, or hyphen: %q", c.ACPAgentName)
	}
	if c.ACPMaxPrompts < 1 || c.ACPMaxPrompts > 8 {
		return fmt.Errorf("AGENTDOCK_ACP_MAX_CONCURRENT_PROMPTS must be between 1 and 8: %d", c.ACPMaxPrompts)
	}
	if c.ACPInteractionMS < 1000 || c.ACPInteractionMS > 3600000 {
		return fmt.Errorf("AGENTDOCK_ACP_INTERACTION_TIMEOUT_MS must be between 1000 and 3600000: %d", c.ACPInteractionMS)
	}
	if err := validateACPArguments(c.ACPArgs); err != nil {
		return fmt.Errorf("AGENTDOCK_ACP_ARGS_JSON: %w", err)
	}
	if err := validateEnvironmentMapping(c.ACPEnvFromEnv); err != nil {
		return fmt.Errorf("AGENTDOCK_ACP_ENV_FROM_ENV_JSON: %w", err)
	}
	c.ACPCommand = filepath.Clean(strings.TrimSpace(c.ACPCommand))
	if c.ACPCommand == "." || !filepath.IsAbs(c.ACPCommand) {
		return fmt.Errorf("AGENTDOCK_ACP_COMMAND must be an absolute executable path: %s", c.ACPCommand)
	}
	info, err := os.Stat(c.ACPCommand)
	if err != nil {
		return fmt.Errorf("stat AGENTDOCK_ACP_COMMAND %s: %w", c.ACPCommand, err)
	}
	if info.IsDir() {
		return fmt.Errorf("AGENTDOCK_ACP_COMMAND is not a file: %s", c.ACPCommand)
	}
	if err := validateACPCommandPlatform(c.ACPCommand, info); err != nil {
		return err
	}
	return nil
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

func getenvOAuthAccessTokenTTL(key string, fallback int64) (seconds int64, never bool, err error) {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback, false, nil
	}
	seconds, never, err = parseOAuthAccessTokenTTL(value)
	if err != nil {
		return 0, false, fmt.Errorf("parse %s as duration: %w", key, err)
	}
	return seconds, never, nil
}

// ValidateOAuthAccessTokenTTL validates an explicit access-token lifetime using
// the same syntax and bounds as AGENTDOCK_OAUTH_ACCESS_TOKEN_TTL.
func ValidateOAuthAccessTokenTTL(value string) error {
	value = strings.TrimSpace(value)
	if value == "" {
		return errors.New("value is empty")
	}
	seconds, never, err := parseOAuthAccessTokenTTL(value)
	if err != nil {
		return err
	}
	if !never && (seconds < int64(time.Minute/time.Second) || seconds > maxOAuthAccessTokenTTLSeconds) {
		return fmt.Errorf("value must be between 1m and 999999d: %ds", seconds)
	}
	return nil
}

func parseOAuthAccessTokenTTL(value string) (seconds int64, never bool, err error) {
	value = strings.TrimSpace(value)
	if strings.EqualFold(value, "never") {
		return 0, true, nil
	}
	if strings.HasSuffix(strings.ToLower(value), "d") {
		days, err := strconv.ParseInt(strings.TrimSpace(value[:len(value)-1]), 10, 64)
		if err != nil || days <= 0 || days > maxOAuthAccessTokenTTLSeconds/(24*60*60) {
			return 0, false, fmt.Errorf("invalid day count %q", value)
		}
		return days * 24 * 60 * 60, false, nil
	}
	parsed, err := time.ParseDuration(value)
	if err != nil {
		return 0, false, err
	}
	if parsed%time.Second != 0 {
		return 0, false, errors.New("value must use whole seconds")
	}
	return int64(parsed / time.Second), false, nil
}

func getenvStringSliceJSON(key string) ([]string, error) {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return nil, nil
	}
	var result []string
	if err := json.Unmarshal([]byte(value), &result); err != nil {
		return nil, fmt.Errorf("parse %s as JSON string array: %w", key, err)
	}
	if err := validateACPArguments(result); err != nil {
		return nil, fmt.Errorf("%s: %w", key, err)
	}
	return result, nil
}

func validateACPArguments(arguments []string) error {
	if len(arguments) > 128 {
		return fmt.Errorf("contains %d arguments; maximum is 128", len(arguments))
	}
	totalBytes := 0
	for _, argument := range arguments {
		if strings.ContainsRune(argument, 0) {
			return errors.New("contains an argument with an invalid NUL byte")
		}
		totalBytes += len(argument)
	}
	if totalBytes > 64<<10 {
		return errors.New("argument payload exceeds 65536 bytes")
	}
	return nil
}

func validACPAgentName(value string) bool {
	if len(value) == 0 || len(value) > 64 {
		return false
	}
	for _, char := range value {
		switch {
		case char >= 'a' && char <= 'z':
		case char >= 'A' && char <= 'Z':
		case char >= '0' && char <= '9':
		case char == '.', char == '_', char == '-':
		default:
			return false
		}
	}
	return true
}
