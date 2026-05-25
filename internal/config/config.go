package config

import (
	"os"
	"strconv"
)

const (
	ProtocolVersion = "2025-06-18"
	ServerName      = "coding-tools-mcp"
	Version         = "0.3.0-go"

	ProfileFull              = "full"
	ProfileReadOnly          = "read-only"
	ProfileCompatReadOnlyAll = "compat-readonly-all"

	SandboxModeLandlock = "landlock"
	SandboxModeNone     = "none"
)

type Config struct {
	Workspace                     string
	Host                          string
	Port                          int
	AuthToken                     string
	OAuthClientID                 string
	OAuthServerURL                string
	ToolProfile                   string
	LogLevel                      string
	SandboxMode                   string
	ConnectorDir                  string
	EnableViewImage               bool
	Stdio                         bool
	DangerouslySkipAllPermissions bool
}

func FromEnv() Config {
	return Config{
		Workspace:                     getenv("CODING_TOOLS_MCP_WORKSPACE", "."),
		Host:                          getenv("CODING_TOOLS_MCP_HOST", "127.0.0.1"),
		Port:                          getenvInt("CODING_TOOLS_MCP_PORT", 8765),
		AuthToken:                     os.Getenv("CODING_TOOLS_MCP_AUTH_TOKEN"),
		OAuthClientID:                 os.Getenv("CODING_TOOLS_MCP_OAUTH_CLIENT_ID"),
		OAuthServerURL:                os.Getenv("CODING_TOOLS_MCP_SERVER_URL"),
		ToolProfile:                   getenv("CODING_TOOLS_MCP_TOOL_PROFILE", ProfileFull),
		LogLevel:                      getenv("CODING_TOOLS_MCP_LOG_LEVEL", "info"),
		SandboxMode:                   getenv("CODING_TOOLS_MCP_SANDBOX_MODE", SandboxModeLandlock),
		ConnectorDir:                  getenv("CODING_TOOLS_MCP_CONNECTOR_DIR", ".mcp/connectors"),
		EnableViewImage:               getenvBool("CODING_TOOLS_MCP_ENABLE_VIEW_IMAGE", true),
		Stdio:                         getenvBool("CODING_TOOLS_MCP_STDIO", false),
		DangerouslySkipAllPermissions: getenvBool("CODING_TOOLS_MCP_SKIP_PERMISSION_PROMPTS", false),
	}
}

func (c *Config) Normalize() {
	switch c.ToolProfile {
	case ProfileReadOnly, ProfileCompatReadOnlyAll:
	default:
		c.ToolProfile = ProfileFull
	}
	if c.Host == "" {
		c.Host = "127.0.0.1"
	}
	if c.Port == 0 {
		c.Port = 8765
	}
	if c.LogLevel == "" {
		c.LogLevel = "info"
	}
	if c.ConnectorDir == "" {
		c.ConnectorDir = ".mcp/connectors"
	}
	switch c.SandboxMode {
	case SandboxModeLandlock, SandboxModeNone:
	default:
		c.SandboxMode = SandboxModeLandlock
	}
}

func getenv(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func getenvInt(key string, fallback int) int {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return fallback
	}
	return parsed
}

func getenvBool(key string, fallback bool) bool {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return fallback
	}
	return parsed
}
