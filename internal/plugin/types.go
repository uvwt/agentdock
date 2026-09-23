package plugin

import (
	"encoding/json"
	"time"
)

const (
	StateSchemaVersion = 1
	VersionLocal       = "local"
)

type Source struct {
	Type             string `json:"type"`
	Ref              string `json:"ref,omitempty"`
	Revision         string `json:"revision,omitempty"`
	Selector         string `json:"selector,omitempty"`
	Subdir           string `json:"subdir,omitempty"`
	Adapter          string `json:"adapter,omitempty"`
	Catalog          string `json:"catalog,omitempty"`
	CatalogItem      string `json:"catalog_item,omitempty"`
	ResolvedType     string `json:"resolved_type,omitempty"`
	ResolvedRef      string `json:"resolved_ref,omitempty"`
	ResolvedRevision string `json:"resolved_revision,omitempty"`
	ResolvedSubdir   string `json:"resolved_subdir,omitempty"`
}

type SourceRequest struct {
	Type        string `json:"type,omitempty"`
	Ref         string `json:"ref,omitempty"`
	GitRef      string `json:"git_ref,omitempty"`
	GitCommit   string `json:"git_commit,omitempty"`
	Subdir      string `json:"subdir,omitempty"`
	SHA256      string `json:"sha256,omitempty"`
	Adapter     string `json:"adapter,omitempty"`
	Version     string `json:"version,omitempty"`
	Catalog     string `json:"catalog,omitempty"`
	CatalogItem string `json:"catalog_item,omitempty"`
}

type Compatibility struct {
	DetectedFormat string   `json:"detected_format"`
	Adapter        string   `json:"adapter"`
	Supported      []string `json:"supported"`
	Unsupported    []string `json:"unsupported"`
	Warnings       []string `json:"warnings"`
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
	PackageDigest  string         `json:"package_digest"`
	Source         Source         `json:"source"`
	Enabled        bool           `json:"enabled"`
	InstalledAt    time.Time      `json:"installed_at"`
	Components     ComponentIndex `json:"components"`
	MCPStorageKeys []string       `json:"mcp_storage_keys,omitempty"`
	Compatibility  Compatibility  `json:"compatibility,omitempty"`
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
	Source        Source           `json:"source"`
	Skills        []SkillComponent `json:"skills"`
	MCP           []MCPReview      `json:"mcp"`
	Unsupported   []string         `json:"unsupported"`
	Warnings      []string         `json:"warnings"`
	Executables   []string         `json:"executables"`
	Issues        []string         `json:"issues"`
	Compatibility Compatibility    `json:"compatibility"`
}

type CatalogEntry struct {
	Name           string         `json:"name"`
	Description    string         `json:"description,omitempty"`
	Version        string         `json:"version,omitempty"`
	Category       string         `json:"category,omitempty"`
	Adapter        string         `json:"adapter"`
	Catalog        string         `json:"catalog"`
	Source         SourceRequest  `json:"source"`
	ResolvedSource *SourceRequest `json:"resolved_source,omitempty"`
	Strict         *bool          `json:"strict,omitempty"`
	Skills         []string       `json:"skills,omitempty"`
	Unsupported    []string       `json:"unsupported,omitempty"`
	Warnings       []string       `json:"warnings,omitempty"`
}

type Catalog struct {
	Name    string         `json:"name"`
	Adapter string         `json:"adapter"`
	Source  string         `json:"source"`
	Entries []CatalogEntry `json:"entries"`
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
