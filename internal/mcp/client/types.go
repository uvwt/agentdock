package client

import (
	"fmt"
	"regexp"
	"strings"
)

const (
	TransportStreamableHTTP = "streamable_http"
	TransportStdio          = "stdio"
	defaultTimeoutMS        = 30000
	maxTimeoutMS            = 300000
)

var (
	serverNamePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`)
	headerNamePattern = regexp.MustCompile(`^[!#$%&'*+.^_` + "`" + `|~0-9A-Za-z-]+$`)
	envNamePattern    = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
)

type ServerConfig struct {
	Name        string            `json:"name"`
	Description string            `json:"description"`
	Transport   string            `json:"transport"`
	URL         string            `json:"url,omitempty"`
	Command     string            `json:"command,omitempty"`
	Args        []string          `json:"args,omitempty"`
	Cwd         string            `json:"cwd,omitempty"`
	HeaderEnv   map[string]string `json:"header_env,omitempty"`
	EnvFromEnv  map[string]string `json:"env_from_env,omitempty"`
	RuntimeEnv  map[string]string `json:"-"`
	Enabled     bool              `json:"enabled"`
	TimeoutMS   int               `json:"timeout_ms,omitempty"`
}

type Tool struct {
	Name         string         `json:"name"`
	Title        string         `json:"title,omitempty"`
	Description  string         `json:"description,omitempty"`
	InputSchema  map[string]any `json:"inputSchema"`
	OutputSchema map[string]any `json:"outputSchema,omitempty"`
	Annotations  map[string]any `json:"annotations,omitempty"`
}

type ToolSummary struct {
	Name          string `json:"name"`
	QualifiedName string `json:"qualified_name"`
	Title         string `json:"title,omitempty"`
	Description   string `json:"description,omitempty"`
	Server        string `json:"server"`
}

type ServerSummary struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Transport   string `json:"transport"`
	Enabled     bool   `json:"enabled"`
	Status      string `json:"status"`
	ToolCount   int    `json:"tool_count"`
	LastError   string `json:"last_error,omitempty"`
	RefreshedAt string `json:"refreshed_at,omitempty"`
}

type Error struct {
	Code      string
	Message   string
	Retryable bool
	Details   map[string]any
	Cause     error
}

func (e *Error) Error() string { return e.Message }
func (e *Error) Unwrap() error { return e.Cause }

func newError(code, message string, retryable bool, details map[string]any, cause error) *Error {
	if details == nil {
		details = map[string]any{}
	}
	return &Error{Code: code, Message: message, Retryable: retryable, Details: details, Cause: cause}
}

func normalizeServerConfig(cfg ServerConfig) ServerConfig {
	cfg.Name = strings.TrimSpace(cfg.Name)
	cfg.Description = strings.TrimSpace(cfg.Description)
	cfg.Transport = strings.ToLower(strings.TrimSpace(cfg.Transport))
	cfg.URL = strings.TrimSpace(cfg.URL)
	cfg.Command = strings.TrimSpace(cfg.Command)
	cfg.Cwd = strings.TrimSpace(cfg.Cwd)
	if cfg.TimeoutMS == 0 {
		cfg.TimeoutMS = defaultTimeoutMS
	}
	cfg.Args = append([]string(nil), cfg.Args...)
	cfg.HeaderEnv = cloneStringMap(cfg.HeaderEnv)
	cfg.EnvFromEnv = cloneStringMap(cfg.EnvFromEnv)
	cfg.RuntimeEnv = cloneStringMap(cfg.RuntimeEnv)
	return cfg
}

func cloneStringMap(input map[string]string) map[string]string {
	if len(input) == 0 {
		return nil
	}
	out := make(map[string]string, len(input))
	for key, value := range input {
		out[strings.TrimSpace(key)] = strings.TrimSpace(value)
	}
	return out
}

func qualifiedToolName(server, tool string) string {
	return server + ":" + tool
}

func splitQualifiedToolName(name string) (string, string, error) {
	server, tool, ok := strings.Cut(strings.TrimSpace(name), ":")
	if !ok || strings.TrimSpace(server) == "" || strings.TrimSpace(tool) == "" {
		return "", "", fmt.Errorf("MCP tool name must use <server>:<tool>")
	}
	return strings.TrimSpace(server), strings.TrimSpace(tool), nil
}
