package plugin

import (
	"encoding/json"
	"time"
)

const (
	StateSchemaVersion = 2
	VersionLocal       = "local"
)

// Provenance records where an imported Portable Plugin originally came from.
// It is part of plugin.json, so it is covered by the package digest and review token.
type Provenance struct {
	Origin   string `json:"origin"`
	Revision string `json:"revision,omitempty"`
	Subdir   string `json:"subdir,omitempty"`
	Format   string `json:"format,omitempty"`
	Adapted  bool   `json:"adapted,omitempty"`
}

type Compatibility struct {
	Format      string   `json:"format"`
	Supported   []string `json:"supported"`
	Unsupported []string `json:"unsupported"`
	Warnings    []string `json:"warnings"`
}

type ManifestAuthor struct {
	Name  string `json:"name,omitempty"`
	Email string `json:"email,omitempty"`
	URL   string `json:"url,omitempty"`
}

type Manifest struct {
	Schema      string                     `json:"$schema"`
	Name        string                     `json:"name"`
	Version     string                     `json:"version"`
	Description string                     `json:"description,omitempty"`
	Author      *ManifestAuthor            `json:"author,omitempty"`
	Homepage    string                     `json:"homepage,omitempty"`
	Repository  string                     `json:"repository,omitempty"`
	License     string                     `json:"license,omitempty"`
	Keywords    []string                   `json:"keywords,omitempty"`
	Provenance  *Provenance                `json:"provenance,omitempty"`
	Extensions  map[string]json.RawMessage `json:"extensions,omitempty"`
}

type SkillComponent struct {
	Name          string `json:"name"`
	Description   string `json:"description"`
	RelativePath  string `json:"path"`
	ContentDigest string `json:"content_digest"`
}

type MCPComponent struct {
	Name           string            `json:"name"`
	Description    string            `json:"description"`
	Transport      string            `json:"transport"`
	URL            string            `json:"url,omitempty"`
	Command        string            `json:"command,omitempty"`
	Args           []string          `json:"args,omitempty"`
	CWD            string            `json:"cwd,omitempty"`
	Environment    map[string]string `json:"env,omitempty"`
	Headers        map[string]string `json:"headers,omitempty"`
	HeaderEnv      map[string]string `json:"header_env,omitempty"`
	EnvBindings    map[string]string `json:"env_bindings,omitempty"`
	RequiredEnv    []string          `json:"required_env,omitempty"`
	TimeoutMS      int               `json:"timeout_ms,omitempty"`
	RuntimeName    string            `json:"runtime_name"`
	StorageKey     string            `json:"storage_key"`
	RelativeSource string            `json:"source"`
}

type ComponentIndex struct {
	Skills []SkillComponent `json:"skills,omitempty"`
	MCP    []MCPComponent   `json:"mcp,omitempty"`
}

type MCPReview struct {
	Name             string   `json:"name"`
	Description      string   `json:"description"`
	Transport        string   `json:"transport"`
	URL              string   `json:"url,omitempty"`
	Command          string   `json:"command,omitempty"`
	CWD              string   `json:"cwd,omitempty"`
	EnvironmentNames []string `json:"environment_names,omitempty"`
	HeaderNames      []string `json:"header_names,omitempty"`
	RuntimeName      string   `json:"runtime_name"`
	StorageKey       string   `json:"storage_key"`
}

type State struct {
	SchemaVersion  int            `json:"schema_version"`
	Name           string         `json:"name"`
	Version        string         `json:"version"`
	Description    string         `json:"description,omitempty"`
	PackageDigest  string         `json:"package_digest"`
	Provenance     *Provenance    `json:"provenance,omitempty"`
	Enabled        bool           `json:"enabled"`
	InstalledAt    time.Time      `json:"installed_at"`
	Components     ComponentIndex `json:"components"`
	MCPStorageKeys []string       `json:"mcp_storage_keys,omitempty"`
	Compatibility  Compatibility  `json:"compatibility,omitempty"`
}

const ActivationTransactionSchemaVersion = 2

type ActivationTransaction struct {
	SchemaVersion    int       `json:"schema_version"`
	Name             string    `json:"name"`
	OwnerID          string    `json:"owner_id"`
	Kind             string    `json:"kind"`
	Phase            string    `json:"phase"`
	Previous         *State    `json:"previous,omitempty"`
	Candidate        State     `json:"candidate"`
	LocalReplacement bool      `json:"local_replacement,omitempty"`
	CreatedAt        time.Time `json:"created_at"`
}

type Package struct {
	Root          string
	Manifest      Manifest
	PackageDigest string
	Components    ComponentIndex
	Unsupported   []string
	Warnings      []string
	Executables   []string
	Compatibility Compatibility
}

type Review struct {
	Valid         bool             `json:"valid"`
	Name          string           `json:"name,omitempty"`
	Version       string           `json:"version,omitempty"`
	Description   string           `json:"description,omitempty"`
	PackageDigest string           `json:"package_digest,omitempty"`
	ReviewToken   string           `json:"review_token,omitempty"`
	Provenance    *Provenance      `json:"provenance,omitempty"`
	Skills        []SkillComponent `json:"skills"`
	MCP           []MCPReview      `json:"mcp"`
	Unsupported   []string         `json:"unsupported"`
	Warnings      []string         `json:"warnings"`
	Executables   []string         `json:"executables"`
	Issues        []string         `json:"issues"`
	Compatibility Compatibility    `json:"compatibility"`
}

type Installed struct {
	State
	Root string `json:"root"`
}

type ChangeResult struct {
	Action          string `json:"action"`
	Name            string `json:"name"`
	Version         string `json:"version,omitempty"`
	PreviousVersion string `json:"previous_version,omitempty"`
	PackageDigest   string `json:"package_digest,omitempty"`
	Changed         bool   `json:"changed"`
	Enabled         bool   `json:"enabled"`
	DataPolicy      string `json:"data_policy,omitempty"`
}
