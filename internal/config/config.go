package config

import (
	"os"
	"strconv"
)

const (
	ProtocolVersion = "2025-06-18"
	ServerName      = "agentdock"
	Version         = "0.3.0-go"

	ProfileUnified           = "unified"
	ProfileReadOnly          = "read-only"
	ProfileCompatReadOnlyAll = "compat-readonly-all"

	ModeSandboxed = "sandboxed"
	ModeHost      = "host"

	SandboxModeLandlock = "landlock"
	SandboxModeNone     = "none"

	PathPolicyWorkspace = "workspace"
	PathPolicyHost      = "host"
)

type Config struct {
	Workspace                     string
	Mode                          string
	Host                          string
	Port                          int
	AuthToken                     string
	OAuthClientID                 string
	OAuthServerURL                string
	ToolProfile                   string
	LogLevel                      string
	SandboxMode                   string
	PathPolicy                    string
	AgentDockDir                  string
	MemoryEndpoint                string
	MemoryToken                   string
	MemoryLoginUser               string
	MemoryLoginValue              string
	MemoryTimeoutMS               int
	NexusEndpoint                 string
	NexusDeviceName               string
	NexusStateDir                 string
	NexusHeartbeatSeconds         int
	ArtifactTargetsJSON           string
	ArtifactFetchEnabled          bool
	ArtifactFetchDenyJSON         string
	BrowserEnabled                bool
	BrowserRunnerDir              string
	BrowserArtifactDir            string
	DesktopEnabled                bool
	DesktopArtifactDir            string
	EnableViewImage               bool
	Stdio                         bool
	DangerouslySkipAllPermissions bool
}

func FromEnv() Config {
	return Config{
		Workspace:                     getenv("AGENTDOCK_WORKSPACE", "."),
		Mode:                          os.Getenv("AGENTDOCK_MODE"),
		Host:                          getenv("AGENTDOCK_HOST", "127.0.0.1"),
		Port:                          getenvInt("AGENTDOCK_PORT", 8765),
		AuthToken:                     os.Getenv("AGENTDOCK_AUTH_TOKEN"),
		OAuthClientID:                 os.Getenv("AGENTDOCK_OAUTH_CLIENT_ID"),
		OAuthServerURL:                os.Getenv("AGENTDOCK_SERVER_URL"),
		ToolProfile:                   getenv("AGENTDOCK_TOOL_PROFILE", ProfileUnified),
		LogLevel:                      getenv("AGENTDOCK_LOG_LEVEL", "info"),
		SandboxMode:                   os.Getenv("AGENTDOCK_SANDBOX_MODE"),
		PathPolicy:                    os.Getenv("AGENTDOCK_PATH_POLICY"),
		AgentDockDir:                  getenv("AGENTDOCK_DIR", "AgentDock"),
		MemoryEndpoint:                firstNonEmpty(os.Getenv("AGENTDOCK_RECALL_ENDPOINT"), os.Getenv("AGENTDOCK_MEMORY_ENDPOINT")),
		MemoryToken:                   firstNonEmpty(os.Getenv("AGENTDOCK_RECALL_TOKEN"), os.Getenv("RECALLDOCK_AUTH_TOKEN"), os.Getenv("AGENTDOCK_MEMORY_TOKEN"), os.Getenv("MEMORYDOCK_AUTH_TOKEN")),
		MemoryLoginUser:               firstNonEmpty(os.Getenv("AGENTDOCK_RECALL_LOGIN_USER"), os.Getenv("AGENTDOCK_MEMORY_LOGIN_USER")),
		MemoryLoginValue:              firstNonEmpty(os.Getenv("AGENTDOCK_RECALL_LOGIN_VALUE"), os.Getenv("AGENTDOCK_MEMORY_LOGIN_VALUE")),
		MemoryTimeoutMS:               firstNonZeroInt(getenvInt("AGENTDOCK_RECALL_TIMEOUT_MS", 0), getenvInt("AGENTDOCK_MEMORY_TIMEOUT_MS", 30000)),
		NexusEndpoint:                 getenv("AGENTDOCK_NEXUS_ENDPOINT", ""),
		NexusDeviceName:               getenv("AGENTDOCK_NEXUS_DEVICE_NAME", ""),
		NexusStateDir:                 getenv("AGENTDOCK_NEXUS_STATE_DIR", ""),
		NexusHeartbeatSeconds:         getenvInt("AGENTDOCK_NEXUS_HEARTBEAT_SECONDS", 30),
		ArtifactTargetsJSON:           getenv("AGENTDOCK_ARTIFACT_TARGETS_JSON", ""),
		ArtifactFetchEnabled:          getenvBool("AGENTDOCK_ARTIFACT_FETCH_ENABLED", false),
		ArtifactFetchDenyJSON:         getenv("AGENTDOCK_ARTIFACT_FETCH_DENY_JSON", ""),
		BrowserEnabled:                getenvBool("AGENTDOCK_BROWSER_ENABLED", false),
		BrowserRunnerDir:              getenv("AGENTDOCK_BROWSER_RUNNER_DIR", "browser-runner"),
		BrowserArtifactDir:            getenv("AGENTDOCK_BROWSER_ARTIFACT_DIR", "browser-artifacts"),
		DesktopEnabled:                getenvBool("AGENTDOCK_DESKTOP_ENABLED", false),
		DesktopArtifactDir:            getenv("AGENTDOCK_DESKTOP_ARTIFACT_DIR", "desktop-artifacts"),
		EnableViewImage:               getenvBool("AGENTDOCK_ENABLE_VIEW_IMAGE", true),
		Stdio:                         getenvBool("AGENTDOCK_STDIO", false),
		DangerouslySkipAllPermissions: getenvBool("AGENTDOCK_SKIP_PERMISSION_PROMPTS", false),
	}
}

func (c *Config) Normalize() {
	if c.Mode == "" {
		if c.PathPolicy == PathPolicyHost {
			c.Mode = ModeHost
		} else {
			c.Mode = ModeSandboxed
		}
	}
	switch c.Mode {
	case ModeSandboxed, ModeHost:
	default:
		c.Mode = ModeSandboxed
	}
	switch c.ToolProfile {
	case ProfileReadOnly, ProfileCompatReadOnlyAll:
	default:
		c.ToolProfile = ProfileUnified
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
	if c.MemoryTimeoutMS <= 0 {
		c.MemoryTimeoutMS = 30000
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
	if c.DesktopArtifactDir == "" {
		c.DesktopArtifactDir = "desktop-artifacts"
	}
	if c.SandboxMode == "" {
		if c.Mode == ModeHost {
			c.SandboxMode = SandboxModeNone
		} else {
			c.SandboxMode = SandboxModeLandlock
		}
	}
	switch c.SandboxMode {
	case SandboxModeLandlock, SandboxModeNone:
	default:
		if c.Mode == ModeHost {
			c.SandboxMode = SandboxModeNone
		} else {
			c.SandboxMode = SandboxModeLandlock
		}
	}
	if c.PathPolicy == "" {
		if c.Mode == ModeHost {
			c.PathPolicy = PathPolicyHost
		} else {
			c.PathPolicy = PathPolicyWorkspace
		}
	}
	switch c.PathPolicy {
	case PathPolicyWorkspace, PathPolicyHost:
	default:
		if c.Mode == ModeHost {
			c.PathPolicy = PathPolicyHost
		} else {
			c.PathPolicy = PathPolicyWorkspace
		}
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

func firstNonZeroInt(values ...int) int {
	for _, value := range values {
		if value != 0 {
			return value
		}
	}
	return 0
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
