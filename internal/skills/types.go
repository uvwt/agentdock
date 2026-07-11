package skills

import (
	"time"

	"github.com/uvwt/agentdock/internal/skillstate"
)

type SkillDocument struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Version     string `json:"version"`
	Body        string `json:"body,omitempty"`
}

type InstallRequest struct {
	Source       string
	DigestSHA256 string
	Activate     bool
	Channel      skillstate.Channel
	MaxBytes     int64
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
	Source       string
	DigestSHA256 string
	MaxBytes     int64
}

type ValidateIssue struct {
	Code    string `json:"code"`
	Stage   string `json:"stage"`
	Message string `json:"message"`
}

type ValidateResult struct {
	Valid    bool            `json:"valid"`
	Source   string          `json:"source"`
	Digest   string          `json:"digest,omitempty"`
	Document SkillDocument   `json:"document,omitempty"`
	Issues   []ValidateIssue `json:"issues"`
}

type RollbackResult struct {
	Skill       string `json:"skill"`
	FromVersion string `json:"from_version"`
	ToVersion   string `json:"to_version"`
	Verified    bool   `json:"verified"`
}
