package client

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/uvwt/agentdock/internal/fs/atomicfile"
	"github.com/uvwt/agentdock/internal/fs/filelock"
)

const (
	registryVersion      = 1
	maxRegistryFileBytes = 1 << 20
)

type registryFile struct {
	Version int            `json:"version"`
	Servers []ServerConfig `json:"servers"`
}

type store struct {
	path     string
	lockPath string
}

func newStore(agentDockHome string) *store {
	root := filepath.Join(agentDockHome, "mcp")
	return &store{path: filepath.Join(root, "servers.json"), lockPath: filepath.Join(root, ".store.lock")}
}

func (s *store) load() (map[string]ServerConfig, error) {
	release, err := s.acquire()
	if err != nil {
		return nil, err
	}
	defer release()
	return s.loadUnlocked()
}

func (s *store) loadUnlocked() (map[string]ServerConfig, error) {
	registryHandle, err := os.Open(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return map[string]ServerConfig{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read dynamic MCP registry: %w", err)
	}
	defer registryHandle.Close()
	data, err := io.ReadAll(io.LimitReader(registryHandle, maxRegistryFileBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read dynamic MCP registry: %w", err)
	}
	if len(data) > maxRegistryFileBytes {
		return nil, fmt.Errorf("dynamic MCP registry exceeds %d bytes", maxRegistryFileBytes)
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var file registryFile
	if err := decoder.Decode(&file); err != nil {
		return nil, fmt.Errorf("decode dynamic MCP registry: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return nil, errors.New("decode dynamic MCP registry: trailing JSON value")
		}
		return nil, fmt.Errorf("decode dynamic MCP registry trailing data: %w", err)
	}
	if file.Version != registryVersion {
		return nil, fmt.Errorf("unsupported dynamic MCP registry version %d", file.Version)
	}
	servers := make(map[string]ServerConfig, len(file.Servers))
	for _, raw := range file.Servers {
		cfg := normalizeServerConfig(raw)
		if err := validateServerConfig(cfg); err != nil {
			return nil, fmt.Errorf("validate dynamic MCP server %q: %w", cfg.Name, err)
		}
		if _, exists := servers[cfg.Name]; exists {
			return nil, fmt.Errorf("duplicate dynamic MCP server %q", cfg.Name)
		}
		servers[cfg.Name] = cfg
	}
	return servers, nil
}

func (s *store) saveUnlocked(servers map[string]ServerConfig) error {
	names := make([]string, 0, len(servers))
	for name := range servers {
		names = append(names, name)
	}
	sort.Strings(names)
	file := registryFile{Version: registryVersion, Servers: make([]ServerConfig, 0, len(names))}
	for _, name := range names {
		file.Servers = append(file.Servers, servers[name])
	}
	data, err := json.MarshalIndent(file, "", "  ")
	if err != nil {
		return fmt.Errorf("encode dynamic MCP registry: %w", err)
	}
	data = append(data, '\n')
	if len(data) > maxRegistryFileBytes {
		return fmt.Errorf("dynamic MCP registry exceeds %d bytes", maxRegistryFileBytes)
	}
	if err := atomicfile.Write(s.path, data, 0o600); err != nil {
		return fmt.Errorf("write dynamic MCP registry: %w", err)
	}
	return nil
}

func (s *store) update(mutator func(map[string]ServerConfig) error) (map[string]ServerConfig, error) {
	release, err := s.acquire()
	if err != nil {
		return nil, err
	}
	defer release()
	servers, err := s.loadUnlocked()
	if err != nil {
		return nil, err
	}
	if err := mutator(servers); err != nil {
		return nil, err
	}
	if err := s.saveUnlocked(servers); err != nil {
		return nil, err
	}
	return servers, nil
}

func (s *store) acquire() (func(), error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	release, err := filelock.Acquire(ctx, s.lockPath)
	if err != nil {
		return nil, fmt.Errorf("lock dynamic MCP registry: %w", err)
	}
	return release, nil
}

func validateServerConfig(cfg ServerConfig) error {
	if !serverNamePattern.MatchString(cfg.Name) {
		return fmt.Errorf("name must match %s", serverNamePattern.String())
	}
	if cfg.Description == "" {
		return errors.New("description is required")
	}
	if cfg.TimeoutMS < 1 || cfg.TimeoutMS > maxTimeoutMS {
		return fmt.Errorf("timeout_ms must be between 1 and %d", maxTimeoutMS)
	}
	for header, envName := range cfg.HeaderEnv {
		if strings.TrimSpace(header) == "" || strings.TrimSpace(envName) == "" {
			return errors.New("header_env keys and values must be non-empty")
		}
		if !headerNamePattern.MatchString(header) {
			return fmt.Errorf("invalid HTTP header name %q", header)
		}
		if isReservedMCPHeader(header) {
			return fmt.Errorf("header_env may not override reserved HTTP header %q", header)
		}
		if !envNamePattern.MatchString(envName) {
			return fmt.Errorf("invalid host environment variable name %q", envName)
		}
	}
	for childName, hostName := range cfg.EnvFromEnv {
		if strings.TrimSpace(childName) == "" || strings.TrimSpace(hostName) == "" {
			return errors.New("env_from_env keys and values must be non-empty")
		}
		if !envNamePattern.MatchString(childName) || !envNamePattern.MatchString(hostName) {
			return fmt.Errorf("invalid environment variable mapping %q -> %q", childName, hostName)
		}
	}

	switch cfg.Transport {
	case TransportStreamableHTTP:
		if cfg.URL == "" {
			return errors.New("url is required for streamable_http")
		}
		parsed, err := url.Parse(cfg.URL)
		if err != nil || parsed.Scheme == "" || parsed.Host == "" {
			return fmt.Errorf("url must be an absolute HTTP(S) URL: %q", cfg.URL)
		}
		if parsed.Scheme != "http" && parsed.Scheme != "https" {
			return fmt.Errorf("url must use http or https: %q", cfg.URL)
		}
		if parsed.User != nil || parsed.Fragment != "" {
			return fmt.Errorf("url must not contain user info or a fragment: %q", cfg.URL)
		}
		if cfg.Command != "" || len(cfg.Args) > 0 || cfg.Cwd != "" || len(cfg.EnvFromEnv) > 0 {
			return errors.New("stdio-only fields are not allowed for streamable_http")
		}
	case TransportStdio:
		if cfg.Command == "" {
			return errors.New("command is required for stdio")
		}
		if cfg.URL != "" || len(cfg.HeaderEnv) > 0 {
			return errors.New("HTTP-only fields are not allowed for stdio")
		}
		if cfg.Cwd != "" && !filepath.IsAbs(cfg.Cwd) {
			return errors.New("cwd must be an absolute path")
		}
	default:
		return fmt.Errorf("unsupported transport %q", cfg.Transport)
	}
	return nil
}

func isReservedMCPHeader(name string) bool {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "accept", "connection", "content-length", "content-type", "host", "mcp-protocol-version", "mcp-session-id", "transfer-encoding", "user-agent":
		return true
	default:
		return false
	}
}
