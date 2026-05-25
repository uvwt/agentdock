package config

import (
	"os"
	"strconv"
)

const (
	ProtocolVersion = "2025-06-18"
	ServerName      = "agentdock"
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
	AgentDockDir                  string
	ConnectorDir                  string
	BrowserEnabled                bool
	BrowserRunnerDir              string
	BrowserArtifactDir            string
	EnableViewImage               bool
	Stdio                         bool
	DangerouslySkipAllPermissions bool
}

func FromEnv() Config {
	return Config{
		Workspace:                     getenv("AGENTDOCK_WORKSPACE", "."),
		Host:                          getenv("AGENTDOCK_HOST", "127.0.0.1"),
		Port:                          getenvInt("AGENTDOCK_PORT", 8765),
		AuthToken:                     os.Getenv("AGENTDOCK_AUTH_TOKEN"),
		OAuthClientID:                 os.Getenv("AGENTDOCK_OAUTH_CLIENT_ID"),
		OAuthServerURL:                os.Getenv("AGENTDOCK_SERVER_URL"),
		ToolProfile:                   getenv("AGENTDOCK_TOOL_PROFILE", ProfileFull),
		LogLevel:                      getenv("AGENTDOCK_LOG_LEVEL", "info"),
		SandboxMode:                   getenv("AGENTDOCK_SANDBOX_MODE", SandboxModeLandlock),
		AgentDockDir:                  getenv("AGENTDOCK_DIR", "AgentDock"),
		ConnectorDir:                  getenv("AGENTDOCK_CONNECTOR_DIR", "connectors"),
		BrowserEnabled:                getenvBool("AGENTDOCK_BROWSER_ENABLED", false),
		BrowserRunnerDir:              getenv("AGENTDOCK_BROWSER_RUNNER_DIR", "browser-runner"),
		BrowserArtifactDir:            getenv("AGENTDOCK_BROWSER_ARTIFACT_DIR", "browser-artifacts"),
		EnableViewImage:               getenvBool("AGENTDOCK_ENABLE_VIEW_IMAGE", true),
		Stdio:                         getenvBool("AGENTDOCK_STDIO", false),
		DangerouslySkipAllPermissions: getenvBool("AGENTDOCK_SKIP_PERMISSION_PROMPTS", false),
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
	if c.AgentDockDir == "" {
		c.AgentDockDir = "AgentDock"
	}
	if c.ConnectorDir == "" {
		c.ConnectorDir = "connectors"
	}
	if c.BrowserRunnerDir == "" {
		c.BrowserRunnerDir = "browser-runner"
	}
	if c.BrowserArtifactDir == "" {
		c.BrowserArtifactDir = "browser-artifacts"
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
