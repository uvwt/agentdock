package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

const (
	ProtocolVersion    = "2025-06-18"
	ServerName         = "agentdock"
	Version            = "0.3.0-go"
	PathModel          = "host"
	BrowserRunnerDir   = "browser-runner"
	BrowserArtifactDir = "browser-artifacts"
)

type Config struct {
	AgentDockHome       string
	AgentDockDefaultDir string
	Host                string
	Port                int
	AuthToken           string
	OAuthClientID       string
	OAuthServerURL      string
	LogLevel            string
	RecallEndpoint      string
	RecallToken         string
	RecallTimeoutMS     int
	NexusEndpoint       string
	NexusToken          string
	BrowserEnabled      bool
	Stdio               bool
}

func FromEnv() Config {
	return Config{
		Host:            getenv("AGENTDOCK_HOST", "127.0.0.1"),
		Port:            getenvInt("AGENTDOCK_PORT", 8765),
		AuthToken:       os.Getenv("AGENTDOCK_AUTH_TOKEN"),
		OAuthClientID:   os.Getenv("AGENTDOCK_OAUTH_CLIENT_ID"),
		OAuthServerURL:  os.Getenv("AGENTDOCK_SERVER_URL"),
		LogLevel:        getenv("AGENTDOCK_LOG_LEVEL", "info"),
		RecallEndpoint:  os.Getenv("AGENTDOCK_RECALL_ENDPOINT"),
		RecallToken:     os.Getenv("AGENTDOCK_RECALL_TOKEN"),
		RecallTimeoutMS: getenvInt("AGENTDOCK_RECALL_TIMEOUT_MS", 30000),
		NexusEndpoint:   getenv("AGENTDOCK_NEXUS_ENDPOINT", ""),
		NexusToken:      os.Getenv("AGENTDOCK_NEXUS_TOKEN"),
		BrowserEnabled:  getenvBool("AGENTDOCK_BROWSER_ENABLED", false),
		Stdio:           getenvBool("AGENTDOCK_STDIO", false),
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
	return nil
}

func (c Config) OAuthEnabled() bool {
	return c.OAuthClientID != ""
}

func (c Config) AuthRequired() bool {
	return c.AuthToken != "" || c.OAuthEnabled()
}

func (c Config) ValidateAuth() error {
	if !c.OAuthEnabled() {
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
		return fmt.Errorf("OAuth enabled by AGENTDOCK_OAUTH_CLIENT_ID but missing required environment variable(s): %s", strings.Join(missing, ", "))
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
