package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
)

const (
	ProtocolVersion = "2025-06-18"
	ServerName      = "agentdock"
	Version         = "0.3.0-go"
	PathModel       = "host"
)

type Config struct {
	AgentDockHome                 string
	AgentDockDefaultDir           string
	Host                          string
	Port                          int
	AuthToken                     string
	OAuthClientID                 string
	OAuthServerURL                string
	LogLevel                      string
	RecallEndpoint                string
	RecallToken                   string
	RecallLoginUser               string
	RecallLoginValue              string
	RecallTimeoutMS               int
	NexusEndpoint                 string
	NexusToken                    string
	NexusDeviceName               string
	NexusHeartbeatSeconds         int
	BrowserEnabled                bool
	BrowserRunnerDir              string
	BrowserArtifactDir            string
	EnableViewImage               bool
	Stdio                         bool
	DangerouslySkipAllPermissions bool
}

func FromEnv() Config {
	return Config{
		Host:                          getenv("AGENTDOCK_HOST", "127.0.0.1"),
		Port:                          getenvInt("AGENTDOCK_PORT", 8765),
		AuthToken:                     os.Getenv("AGENTDOCK_AUTH_TOKEN"),
		OAuthClientID:                 os.Getenv("AGENTDOCK_OAUTH_CLIENT_ID"),
		OAuthServerURL:                os.Getenv("AGENTDOCK_SERVER_URL"),
		LogLevel:                      getenv("AGENTDOCK_LOG_LEVEL", "info"),
		RecallEndpoint:                os.Getenv("AGENTDOCK_RECALL_ENDPOINT"),
		RecallToken:                   firstNonEmpty(os.Getenv("AGENTDOCK_RECALL_TOKEN"), os.Getenv("RECALLDOCK_AUTH_TOKEN")),
		RecallLoginUser:               os.Getenv("AGENTDOCK_RECALL_LOGIN_USER"),
		RecallLoginValue:              os.Getenv("AGENTDOCK_RECALL_LOGIN_VALUE"),
		RecallTimeoutMS:               getenvInt("AGENTDOCK_RECALL_TIMEOUT_MS", 30000),
		NexusEndpoint:                 getenv("AGENTDOCK_NEXUS_ENDPOINT", ""),
		NexusToken:                    firstNonEmpty(os.Getenv("AGENTDOCK_NEXUS_TOKEN"), os.Getenv("NEXUS_AUTH_TOKEN"), os.Getenv("RECALLDOCK_AUTH_TOKEN"), os.Getenv("AGENTDOCK_RECALL_TOKEN")),
		NexusDeviceName:               getenv("AGENTDOCK_NEXUS_DEVICE_NAME", ""),
		NexusHeartbeatSeconds:         getenvInt("AGENTDOCK_NEXUS_HEARTBEAT_SECONDS", 30),
		BrowserEnabled:                getenvBool("AGENTDOCK_BROWSER_ENABLED", false),
		BrowserRunnerDir:              getenv("AGENTDOCK_BROWSER_RUNNER_DIR", "browser-runner"),
		BrowserArtifactDir:            getenv("AGENTDOCK_BROWSER_ARTIFACT_DIR", "browser-artifacts"),
		EnableViewImage:               getenvBool("AGENTDOCK_ENABLE_VIEW_IMAGE", true),
		Stdio:                         getenvBool("AGENTDOCK_STDIO", false),
		DangerouslySkipAllPermissions: getenvBool("AGENTDOCK_SKIP_PERMISSION_PROMPTS", false),
	}
}

func (c *Config) Normalize() error {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return fmt.Errorf("resolve user home for AgentDock directories: %w", err)
	}
	if c.AgentDockHome == "" {
		c.AgentDockHome = filepath.Join(home, ".agentdock")
	}
	if c.AgentDockDefaultDir == "" {
		c.AgentDockDefaultDir = filepath.Join(home, "AgentDock")
	}
	for label, value := range map[string]string{"AgentDockHome": c.AgentDockHome, "AgentDockDefaultDir": c.AgentDockDefaultDir} {
		if !filepath.IsAbs(value) {
			return fmt.Errorf("%s must resolve to an absolute path: %s", label, value)
		}
		if err := os.MkdirAll(value, 0o700); err != nil {
			return fmt.Errorf("create %s %s: %w", label, value, err)
		}
		info, err := os.Stat(value)
		if err != nil {
			return fmt.Errorf("stat %s %s: %w", label, value, err)
		}
		if !info.IsDir() {
			return fmt.Errorf("%s is not a directory: %s", label, value)
		}
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
	if c.RecallTimeoutMS <= 0 {
		c.RecallTimeoutMS = 30000
	}
	if c.NexusHeartbeatSeconds <= 0 {
		c.NexusHeartbeatSeconds = 30
	}
	if c.BrowserRunnerDir == "" {
		c.BrowserRunnerDir = "browser-runner"
	}
	if c.BrowserArtifactDir == "" {
		c.BrowserArtifactDir = "browser-artifacts"
	}
	return nil
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

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
