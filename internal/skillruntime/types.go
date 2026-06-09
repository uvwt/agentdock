package skillruntime

import (
	"encoding/json"
	"time"

	"github.com/uvwt/agentdock/internal/skillstate"
)

const (
	ManifestAPIVersion = "agentdock.dev/v1"
	ManifestKind       = "Skill"
)

type Manifest struct {
	APIVersion string   `json:"apiVersion"`
	Kind       string   `json:"kind"`
	Metadata   Metadata `json:"metadata"`
	Spec       Spec     `json:"spec"`
}

type Metadata struct {
	Name        string `json:"name"`
	Version     string `json:"version"`
	DisplayName string `json:"displayName"`
	Description string `json:"description"`
	License     string `json:"license,omitempty"`
	Homepage    string `json:"homepage,omitempty"`
}

type Spec struct {
	Entrypoint    string        `json:"entrypoint"`
	Operations    []Operation   `json:"operations"`
	Compatibility Compatibility `json:"compatibility"`
	Permissions   Permissions   `json:"permissions"`
	Bindings      []string      `json:"bindings,omitempty"`
	Verification  []string      `json:"verification,omitempty"`
}

type Operation struct {
	Name           string         `json:"name"`
	Description    string         `json:"description"`
	InputSchema    map[string]any `json:"inputSchema"`
	OutputSchema   map[string]any `json:"outputSchema"`
	TimeoutSeconds int            `json:"timeoutSeconds"`
}

type Compatibility struct {
	Platforms     []string `json:"platforms"`
	Architectures []string `json:"architectures"`
	AgentDock     string   `json:"agentdock"`
}

type Permissions struct {
	Filesystem []string `json:"filesystem"`
	Network    []string `json:"network"`
	Env        []EnvVar `json:"env,omitempty"`
	Secrets    []string `json:"secrets"`
	Commands   []string `json:"commands"`
}

type EnvVar struct {
	Name string `json:"name"`
	Kind string `json:"kind"`
}

type InstallRequest struct {
	Source         string
	DigestSHA256   string
	Activate       bool
	Channel        skillstate.Channel
	MaxBytes       int64
	ConfirmedNoEnv bool
}

type InstallResult struct {
	Skill       string             `json:"skill"`
	Version     string             `json:"version"`
	Digest      string             `json:"digest"`
	InstalledAt time.Time          `json:"installed_at"`
	Activated   bool               `json:"activated"`
	Channel     skillstate.Channel `json:"channel,omitempty"`
	Path        string             `json:"path"`
}

type ValidateRequest struct {
	Source         string
	DigestSHA256   string
	MaxBytes       int64
	ConfirmedNoEnv bool
}

type ValidateIssue struct {
	Code    string `json:"code"`
	Stage   string `json:"stage"`
	Message string `json:"message"`
}

type CommandCheck struct {
	Command string `json:"command"`
	Found   bool   `json:"found"`
	Path    string `json:"path,omitempty"`
	Error   string `json:"error,omitempty"`
}

type ValidateResult struct {
	Valid                bool            `json:"valid"`
	Source               string          `json:"source"`
	Digest               string          `json:"digest,omitempty"`
	Manifest             Manifest        `json:"manifest,omitempty"`
	Env                  []EnvDefinition `json:"env,omitempty"`
	Commands             []CommandCheck  `json:"commands,omitempty"`
	Issues               []ValidateIssue `json:"issues"`
	RequiresNoEnvConfirm bool            `json:"requires_no_env_confirm,omitempty"`
}

type RunRequest struct {
	RunID     string             `json:"run_id,omitempty"`
	Skill     string             `json:"skill"`
	Version   string             `json:"version,omitempty"`
	Channel   skillstate.Channel `json:"channel,omitempty"`
	Operation string             `json:"operation"`
	Input     json.RawMessage    `json:"input"`
	Binding   string             `json:"binding,omitempty"`
	Timeout   time.Duration      `json:"-"`
	MaxOutput int                `json:"-"`
}

type RunResult struct {
	RunID           string          `json:"run_id,omitempty"`
	Skill           string          `json:"skill"`
	Version         string          `json:"version"`
	Operation       string          `json:"operation"`
	OK              bool            `json:"ok"`
	ExitCode        int             `json:"exit_code"`
	Output          json.RawMessage `json:"output,omitempty"`
	Stdout          string          `json:"stdout,omitempty"`
	Stderr          string          `json:"stderr,omitempty"`
	StdoutBytes     int64           `json:"stdout_bytes,omitempty"`
	StderrBytes     int64           `json:"stderr_bytes,omitempty"`
	StdoutTruncated bool            `json:"stdout_truncated,omitempty"`
	StderrTruncated bool            `json:"stderr_truncated,omitempty"`
	Truncated       bool            `json:"truncated,omitempty"`
	ErrorCode       string          `json:"error_code,omitempty"`
	Error           string          `json:"error,omitempty"`
	DurationMS      int64           `json:"duration_ms"`
	StartedAt       time.Time       `json:"started_at"`
	CompletedAt     time.Time       `json:"completed_at"`
}

type VerificationResult struct {
	Name    string `json:"name"`
	OK      bool   `json:"ok"`
	Message string `json:"message,omitempty"`
}

type RollbackResult struct {
	Skill        string               `json:"skill"`
	FromVersion  string               `json:"from_version"`
	ToVersion    string               `json:"to_version"`
	Verified     bool                 `json:"verified"`
	Verification []VerificationResult `json:"verification"`
}
