package plugin

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/uvwt/agentdock/internal/config"
)

const (
	maxMCPFileBytes = 1 << 20
	mcpSchemaURI    = "https://agent-plugins.org/schemas/1.0.0/mcp.schema.json"
)

var portableHeaderNamePattern = regexp.MustCompile(`^[!#$%&'*+.^_` + "`" + `|~0-9A-Za-z-]+$`)

type rawMCPFile struct {
	Schema     string                  `json:"$schema"`
	MCPServers map[string]rawMCPServer `json:"mcpServers"`
}

type rawMCPServer struct {
	Type    string            `json:"type"`
	URL     string            `json:"url,omitempty"`
	Command string            `json:"command,omitempty"`
	Args    []string          `json:"args,omitempty"`
	CWD     string            `json:"cwd,omitempty"`
	Env     map[string]string `json:"env,omitempty"`
	Headers map[string]string `json:"headers,omitempty"`
}

func loadMCPFile(path, packageRoot, pluginName string) ([]MCPComponent, []string, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, nil, pluginError("PLUGIN_MCP_INVALID", "mcp.read", err)
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, maxMCPFileBytes+1))
	if err != nil {
		return nil, nil, pluginError("PLUGIN_MCP_INVALID", "mcp.read", err)
	}
	if len(data) > maxMCPFileBytes {
		return nil, nil, pluginError("PLUGIN_MCP_INVALID", "mcp.read", fmt.Errorf("mcp.json exceeds %d bytes", maxMCPFileBytes))
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var raw rawMCPFile
	if err := decoder.Decode(&raw); err != nil {
		return nil, nil, pluginError("PLUGIN_MCP_INVALID", "mcp.decode", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return nil, nil, pluginError("PLUGIN_MCP_INVALID", "mcp.decode", errors.New("mcp.json contains trailing JSON"))
		}
		return nil, nil, pluginError("PLUGIN_MCP_INVALID", "mcp.decode", err)
	}
	if raw.Schema != mcpSchemaURI {
		return nil, nil, pluginError("PLUGIN_MCP_INVALID", "mcp.schema", fmt.Errorf("$schema must equal %s", mcpSchemaURI))
	}
	if raw.MCPServers == nil {
		return nil, nil, pluginError("PLUGIN_MCP_INVALID", "mcp.decode", errors.New("mcpServers object is required"))
	}

	names := make([]string, 0, len(raw.MCPServers))
	for name := range raw.MCPServers {
		names = append(names, name)
	}
	sort.Strings(names)
	components := make([]MCPComponent, 0, len(names))
	unsupported := make([]string, 0)
	for _, name := range names {
		component, unsupportedReason, err := normalizeMCPComponent(packageRoot, pluginName, name, raw.MCPServers[name])
		if err != nil {
			return nil, nil, pluginError("PLUGIN_MCP_INVALID", "mcp."+name, err)
		}
		if unsupportedReason != "" {
			unsupported = append(unsupported, unsupportedReason)
		}
		components = append(components, component)
	}
	sort.Strings(unsupported)
	return components, uniqueStrings(unsupported), nil
}

func normalizeMCPComponent(packageRoot, pluginName, name string, raw rawMCPServer) (MCPComponent, string, error) {
	name = strings.TrimSpace(name)
	if err := validateComponentName(name); err != nil {
		return MCPComponent{}, "", err
	}
	component := MCPComponent{
		Name:           name,
		Description:    pluginName + " Plugin MCP: " + name,
		URL:            strings.TrimSpace(raw.URL),
		Command:        strings.TrimSpace(raw.Command),
		Args:           append([]string(nil), raw.Args...),
		CWD:            strings.TrimSpace(raw.CWD),
		Environment:    cloneStringMap(raw.Env),
		Headers:        cloneStringMap(raw.Headers),
		TimeoutMS:      30000,
		RuntimeName:    RuntimeMCPName(pluginName, name),
		StorageKey:     RuntimeMCPName(pluginName, name),
		RelativeSource: "mcp.json",
	}

	switch strings.TrimSpace(raw.Type) {
	case "stdio":
		component.Transport = "stdio"
		if component.Command == "" {
			return MCPComponent{}, "", errors.New("stdio command is required")
		}
		if component.URL != "" || len(component.Headers) > 0 {
			return MCPComponent{}, "", errors.New("HTTP-only fields are not allowed for stdio MCP")
		}
		if err := validatePortableCommand(packageRoot, component.Command); err != nil {
			return MCPComponent{}, "", err
		}
		if err := normalizePortableEnvironment(&component); err != nil {
			return MCPComponent{}, "", err
		}
		if err := validatePortableCWD(component.CWD); err != nil {
			return MCPComponent{}, "", err
		}
		return component, "", nil

	case "streamable-http":
		component.Transport = "streamable_http"
		if component.URL == "" {
			return MCPComponent{}, "", errors.New("streamable-http url is required")
		}
		if component.Command != "" || len(component.Args) > 0 || component.CWD != "" || len(component.Environment) > 0 {
			return MCPComponent{}, "", errors.New("stdio-only fields are not allowed for streamable-http MCP")
		}
		if err := validatePortableRemoteURL(component.URL); err != nil {
			return MCPComponent{}, "", err
		}
		if err := normalizePortableHeaders(&component); err != nil {
			return MCPComponent{}, "", err
		}
		return component, "", nil

	case "sse":
		component.Transport = "sse"
		if component.URL == "" {
			return MCPComponent{}, "", errors.New("sse url is required")
		}
		if component.Command != "" || len(component.Args) > 0 || component.CWD != "" || len(component.Environment) > 0 {
			return MCPComponent{}, "", errors.New("stdio-only fields are not allowed for sse MCP")
		}
		if err := validatePortableRemoteURL(component.URL); err != nil {
			return MCPComponent{}, "", err
		}
		if err := normalizePortableHeaders(&component); err != nil {
			return MCPComponent{}, "", err
		}
		return component, "MCP server " + name + " uses unsupported sse transport", nil

	default:
		return MCPComponent{}, "", fmt.Errorf("unsupported MCP transport %q", raw.Type)
	}
}

func validatePortableCommand(root, command string) error {
	if filepath.IsAbs(command) {
		return errors.New("Plugin MCP command must be a bare executable or begin with ./")
	}
	if strings.Contains(command, "${") {
		return errors.New("placeholder expansion is not allowed in Plugin MCP command")
	}
	if strings.ContainsAny(command, "/\\") {
		if !strings.HasPrefix(filepath.ToSlash(command), "./") {
			return errors.New("Plugin-relative MCP command must begin with ./")
		}
		return validateRelativePackageReference(root, strings.TrimPrefix(filepath.ToSlash(command), "./"), false)
	}
	if command == "." || command == ".." || strings.TrimSpace(command) != command {
		return errors.New("invalid Plugin MCP command")
	}
	return nil
}

func validatePortableCWD(cwd string) error {
	if cwd == "" {
		return nil
	}
	syntheticRoot := filepath.Join(string(filepath.Separator), "plugin-root")
	syntheticData := filepath.Join(string(filepath.Separator), "plugin-data")
	var expanded string
	switch {
	case strings.HasPrefix(cwd, "./"):
		expanded = filepath.Join(syntheticRoot, filepath.FromSlash(strings.TrimPrefix(cwd, "./")))
	case cwd == "${PLUGIN_ROOT}":
		expanded = syntheticRoot
	case strings.HasPrefix(cwd, "${PLUGIN_ROOT}/"):
		expanded = filepath.Join(syntheticRoot, filepath.FromSlash(strings.TrimPrefix(cwd, "${PLUGIN_ROOT}/")))
	case cwd == "${PLUGIN_DATA}":
		expanded = syntheticData
	case strings.HasPrefix(cwd, "${PLUGIN_DATA}/"):
		expanded = filepath.Join(syntheticData, filepath.FromSlash(strings.TrimPrefix(cwd, "${PLUGIN_DATA}/")))
	default:
		return errors.New("Plugin MCP cwd must begin with ./, ${PLUGIN_ROOT}, or ${PLUGIN_DATA}")
	}
	base := syntheticRoot
	if strings.HasPrefix(cwd, "${PLUGIN_DATA}") {
		base = syntheticData
	}
	relative, err := filepath.Rel(base, filepath.Clean(expanded))
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) || filepath.IsAbs(relative) {
		return errors.New("Plugin MCP cwd escapes its declared portable root")
	}
	return nil
}

func validatePortableRemoteURL(value string) error {
	parsed, err := url.Parse(value)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return errors.New("remote MCP url must be an absolute HTTP(S) URL")
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return errors.New("remote MCP url must use http or https")
	}
	if parsed.User != nil || parsed.Fragment != "" || parsed.RawQuery != "" {
		return errors.New("remote MCP url must not contain credentials, query parameters, or fragments; use env-backed headers for credentials")
	}
	if parsed.Scheme == "http" && !isLoopbackHost(parsed.Hostname()) {
		return errors.New("non-loopback remote MCP endpoints must use https")
	}
	return nil
}

func isLoopbackHost(host string) bool {
	if strings.EqualFold(strings.TrimSpace(host), "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func normalizePortableEnvironment(component *MCPComponent) error {
	static := make(map[string]string)
	bindings := make(map[string]string)
	required := append([]string(nil), component.RequiredEnv...)
	for key, value := range component.Environment {
		if err := validateEnvName(key); err != nil {
			return err
		}
		if config.IsReservedPluginEnvironmentKey(key) {
			return fmt.Errorf("environment variable %s is reserved by the Plugin runtime", key)
		}
		if envName, ok := exactEnvironmentReference(value); ok && !config.IsReservedPluginEnvironmentKey(envName) {
			bindings[key] = envName
			required = append(required, envName)
			continue
		}
		if sensitiveEnvironmentName(key) {
			return fmt.Errorf("sensitive environment variable %s must use a ${ENV_NAME} binding instead of a literal value", key)
		}
		static[key] = value
	}
	if len(static) == 0 {
		static = nil
	}
	if len(bindings) == 0 {
		bindings = nil
	}
	sort.Strings(required)
	component.Environment = static
	component.EnvBindings = bindings
	component.RequiredEnv = uniqueStrings(required)
	return nil
}

func normalizePortableHeaders(component *MCPComponent) error {
	static := make(map[string]string)
	bindings := make(map[string]string)
	required := append([]string(nil), component.RequiredEnv...)
	for rawName, value := range component.Headers {
		name := strings.TrimSpace(rawName)
		if !portableHeaderNamePattern.MatchString(name) {
			return fmt.Errorf("invalid HTTP header name %q", name)
		}
		if strings.ContainsRune(value, '\r') || strings.ContainsRune(value, '\n') {
			return fmt.Errorf("HTTP header %q contains a newline", name)
		}
		if envName, ok := exactEnvironmentReference(value); ok && !config.IsReservedPluginEnvironmentKey(envName) {
			bindings[name] = envName
			required = append(required, envName)
			continue
		}
		switch strings.ToLower(name) {
		case "authorization", "proxy-authorization", "cookie", "set-cookie":
			return fmt.Errorf("credential header %q must use a ${ENV_NAME} binding instead of a literal value", name)
		}
		static[name] = value
	}
	if len(static) == 0 {
		static = nil
	}
	if len(bindings) == 0 {
		bindings = nil
	}
	sort.Strings(required)
	component.Headers = static
	component.HeaderEnv = bindings
	component.RequiredEnv = uniqueStrings(required)
	return nil
}

func exactEnvironmentReference(value string) (string, bool) {
	value = strings.TrimSpace(value)
	if len(value) < 4 || !strings.HasPrefix(value, "${") || !strings.HasSuffix(value, "}") {
		return "", false
	}
	name := value[2 : len(value)-1]
	if strings.ContainsAny(name, "${}/\\") || validateEnvName(name) != nil {
		return "", false
	}
	return name, true
}

func sensitiveEnvironmentName(name string) bool {
	normalized := strings.ToUpper(strings.TrimSpace(name))
	for _, marker := range []string{"TOKEN", "SECRET", "PASSWORD", "PASSWD", "API_KEY", "APIKEY", "CREDENTIAL", "AUTH"} {
		if strings.Contains(normalized, marker) {
			return true
		}
	}
	return false
}

func cloneStringMap(values map[string]string) map[string]string {
	if len(values) == 0 {
		return nil
	}
	out := make(map[string]string, len(values))
	for key, value := range values {
		out[key] = value
	}
	return out
}

func validateRelativePackageReference(root, value string, allowDirectory bool) error {
	clean := filepath.Clean(filepath.FromSlash(strings.TrimSpace(value)))
	if clean == "." || clean == "" || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) || filepath.IsAbs(clean) {
		return fmt.Errorf("path %q escapes the Plugin package", value)
	}
	target := filepath.Join(root, clean)
	relative, err := filepath.Rel(root, target)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) || filepath.IsAbs(relative) {
		return fmt.Errorf("path %q escapes the Plugin package", value)
	}
	info, err := os.Lstat(target)
	if err != nil {
		return fmt.Errorf("path %q is unavailable: %w", value, err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("path %q cannot be a symlink", value)
	}
	if allowDirectory {
		if !info.IsDir() {
			return fmt.Errorf("path %q must be a directory", value)
		}
		return nil
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("path %q must be a regular file", value)
	}
	return nil
}
