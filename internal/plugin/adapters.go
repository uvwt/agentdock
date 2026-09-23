package plugin

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type adapterHints struct {
	Name        string
	Version     string
	Description string
	Strict      *bool
	Skills      []string
	MCPServers  json.RawMessage
	Unsupported []string
	Warnings    []string
}

func (m *Manager) adaptStagedSource(staged stagedPluginSource, request SourceRequest) (string, Package, func(), error) {
	adapter, format, err := detectPluginAdapter(staged.Root, request.Adapter, staged.Hints)
	if err != nil {
		return "", Package{}, func() {}, err
	}
	if adapter == "portable" {
		pkg, err := LoadPackage(staged.Root)
		if err != nil {
			return "", Package{}, func() {}, err
		}
		pkg.Compatibility = Compatibility{
			DetectedFormat: format, Adapter: "portable",
			Supported:   []string{"plugin.json", "skills", "mcp"},
			Unsupported: append([]string(nil), pkg.Unsupported...),
			Warnings:    append([]string(nil), pkg.Warnings...),
		}
		return staged.Root, pkg, func() {}, nil
	}

	normalized, err := m.store.TempPath("adapted")
	if err != nil {
		return "", Package{}, func() {}, err
	}
	cleanup := func() { _ = os.RemoveAll(normalized) }
	if err := copyPluginTree(staged.Root, normalized); err != nil {
		cleanup()
		return "", Package{}, func() {}, pluginError("PLUGIN_ADAPTER_FAILED", "adapter.copy", err)
	}

	var compatibility Compatibility
	switch adapter {
	case "openai":
		compatibility, err = normalizeOpenAIPlugin(staged.Root, normalized, request, staged)
	case "claude":
		compatibility, err = normalizeClaudePlugin(staged.Root, normalized, request, staged)
	default:
		err = fmt.Errorf("unsupported Plugin adapter %q", adapter)
	}
	if err != nil {
		cleanup()
		return "", Package{}, func() {}, pluginError("PLUGIN_ADAPTER_FAILED", "adapter."+adapter, err)
	}
	pkg, err := LoadPackage(normalized)
	if err != nil {
		cleanup()
		return "", Package{}, func() {}, err
	}
	pkg.Unsupported = append(pkg.Unsupported, compatibility.Unsupported...)
	sort.Strings(pkg.Unsupported)
	pkg.Unsupported = uniqueStrings(pkg.Unsupported)
	pkg.Warnings = append(pkg.Warnings, compatibility.Warnings...)
	sort.Strings(pkg.Warnings)
	pkg.Warnings = uniqueStrings(pkg.Warnings)
	compatibility.Unsupported = append([]string(nil), pkg.Unsupported...)
	compatibility.Warnings = append([]string(nil), pkg.Warnings...)
	pkg.Compatibility = compatibility
	return normalized, pkg, cleanup, nil
}

func detectPluginAdapter(root, requested string, hints adapterHints) (adapter, format string, err error) {
	requested = strings.ToLower(strings.TrimSpace(requested))
	if requested == "" {
		requested = "auto"
	}
	hasPortable := regularFileExists(filepath.Join(root, "plugin.json"))
	hasOpenAI := regularFileExists(filepath.Join(root, ".codex-plugin", "plugin.json"))
	hasClaude := regularFileExists(filepath.Join(root, ".claude-plugin", "plugin.json"))
	if !hasPortable && hasOpenAI && hasClaude && requested == "auto" {
		return "", "", pluginError("PLUGIN_ADAPTER_AMBIGUOUS", "adapter.detect", errors.New("source contains both OpenAI and Claude fallback manifests; select adapter explicitly"))
	}
	if requested == "portable" {
		if !hasPortable {
			return "", "", pluginError("PLUGIN_ADAPTER_NOT_FOUND", "adapter.detect", errors.New("portable adapter requires root plugin.json"))
		}
		return "portable", "portable-agent-plugin", nil
	}
	if requested == "openai" {
		if hasPortable {
			return "portable", "portable-agent-plugin", nil
		}
		if !hasOpenAI {
			return "", "", pluginError("PLUGIN_ADAPTER_NOT_FOUND", "adapter.detect", errors.New("OpenAI adapter requires .codex-plugin/plugin.json fallback"))
		}
		return "openai", "openai-codex", nil
	}
	if requested == "claude" {
		if hasClaude {
			return "claude", "claude-plugin", nil
		}
		if hints.Strict != nil && !*hints.Strict && hints.Name != "" {
			return "claude", "claude-marketplace-skill-bundle", nil
		}
		return "", "", pluginError("PLUGIN_ADAPTER_NOT_FOUND", "adapter.detect", errors.New("Claude adapter requires .claude-plugin/plugin.json or a strict:false marketplace skill bundle"))
	}
	if requested != "auto" {
		return "", "", pluginError("PLUGIN_ADAPTER_NOT_FOUND", "adapter.detect", fmt.Errorf("unsupported adapter %q", requested))
	}
	switch {
	case hasPortable:
		return "portable", "portable-agent-plugin", nil
	case hasOpenAI:
		return "openai", "openai-codex", nil
	case hasClaude:
		return "claude", "claude-plugin", nil
	case hints.Strict != nil && !*hints.Strict && hints.Name != "":
		return "claude", "claude-marketplace-skill-bundle", nil
	default:
		return "", "", pluginError("PLUGIN_ADAPTER_NOT_FOUND", "adapter.detect", errors.New("no supported portable/OpenAI/Claude Plugin manifest was found"))
	}
}

func normalizeOpenAIPlugin(sourceRoot, normalizedRoot string, request SourceRequest, staged stagedPluginSource) (Compatibility, error) {
	manifestPath := filepath.Join(sourceRoot, ".codex-plugin", "plugin.json")
	raw, err := readJSONObject(manifestPath, maxManifestBytes)
	if err != nil {
		return Compatibility{}, err
	}
	compat := Compatibility{
		DetectedFormat: "openai-codex", Adapter: "openai",
		Supported:   []string{"metadata", "skills", "mcp"},
		Unsupported: append([]string(nil), staged.Hints.Unsupported...),
		Warnings:    append([]string(nil), staged.Hints.Warnings...),
	}
	manifest, manifestUnsupported, warnings, err := normalizeExternalManifest(raw, request, staged.Source, staged.Hints, "openai")
	if err != nil {
		return Compatibility{}, err
	}
	compat.Unsupported = append(compat.Unsupported, manifestUnsupported...)
	compat.Warnings = append(compat.Warnings, warnings...)

	if err := normalizeSkillPaths(sourceRoot, normalizedRoot, raw["skills"], staged.Hints.Skills); err != nil {
		return Compatibility{}, err
	}
	mcpValues, err := collectExternalMCPValues(sourceRoot, raw["mcpServers"], ".mcp.json")
	if err != nil {
		return Compatibility{}, err
	}
	mcpUnsupported, err := writeNormalizedMCP(normalizedRoot, mcpValues, "openai")
	if err != nil {
		return Compatibility{}, err
	}
	compat.Unsupported = append(compat.Unsupported, mcpUnsupported...)
	compat.Unsupported = append(compat.Unsupported, detectExternalRootUnsupported(sourceRoot, "openai")...)
	if err := writePortableManifest(normalizedRoot, manifest); err != nil {
		return Compatibility{}, err
	}
	normalizeCompatibility(&compat)
	return compat, nil
}

func normalizeClaudePlugin(sourceRoot, normalizedRoot string, request SourceRequest, staged stagedPluginSource) (Compatibility, error) {
	compat := Compatibility{
		DetectedFormat: "claude-plugin", Adapter: "claude",
		Supported:   []string{"metadata", "skills", "mcp"},
		Unsupported: append([]string(nil), staged.Hints.Unsupported...),
		Warnings:    append([]string(nil), staged.Hints.Warnings...),
	}
	var raw map[string]json.RawMessage
	manifestPath := filepath.Join(sourceRoot, ".claude-plugin", "plugin.json")
	if regularFileExists(manifestPath) {
		var err error
		raw, err = readJSONObject(manifestPath, maxManifestBytes)
		if err != nil {
			return Compatibility{}, err
		}
	} else {
		if staged.Hints.Strict == nil || *staged.Hints.Strict || staged.Hints.Name == "" {
			return Compatibility{}, errors.New("Claude manifest is missing and source is not a strict:false marketplace bundle")
		}
		compat.DetectedFormat = "claude-marketplace-skill-bundle"
		raw = map[string]json.RawMessage{}
	}
	manifest, manifestUnsupported, warnings, err := normalizeExternalManifest(raw, request, staged.Source, staged.Hints, "claude")
	if err != nil {
		return Compatibility{}, err
	}
	compat.Unsupported = append(compat.Unsupported, manifestUnsupported...)
	compat.Warnings = append(compat.Warnings, warnings...)

	if err := normalizeSkillPaths(sourceRoot, normalizedRoot, raw["skills"], staged.Hints.Skills); err != nil {
		return Compatibility{}, err
	}
	mcpValues, err := collectExternalMCPValues(sourceRoot, raw["mcpServers"], ".mcp.json")
	if err != nil {
		return Compatibility{}, err
	}
	if len(staged.Hints.MCPServers) > 0 && !isJSONEmpty(staged.Hints.MCPServers) {
		mcpValues = append(mcpValues, staged.Hints.MCPServers)
	}
	mcpUnsupported, err := writeNormalizedMCP(normalizedRoot, mcpValues, "claude")
	if err != nil {
		return Compatibility{}, err
	}
	compat.Unsupported = append(compat.Unsupported, mcpUnsupported...)
	compat.Unsupported = append(compat.Unsupported, detectExternalRootUnsupported(sourceRoot, "claude")...)
	if err := writePortableManifest(normalizedRoot, manifest); err != nil {
		return Compatibility{}, err
	}
	normalizeCompatibility(&compat)
	return compat, nil
}

func normalizeExternalManifest(raw map[string]json.RawMessage, request SourceRequest, source Source, hints adapterHints, adapter string) (Manifest, []string, []string, error) {
	readString := func(name string) (string, error) {
		value, ok := raw[name]
		if !ok || isJSONEmpty(value) {
			return "", nil
		}
		var text string
		if err := json.Unmarshal(value, &text); err != nil {
			return "", fmt.Errorf("%s must be a string", name)
		}
		return strings.TrimSpace(text), nil
	}
	name, err := readString("name")
	if err != nil {
		return Manifest{}, nil, nil, err
	}
	if name == "" {
		name = strings.TrimSpace(hints.Name)
	}
	if err := ValidateName(name); err != nil {
		return Manifest{}, nil, nil, fmt.Errorf("external Plugin name: %w", err)
	}
	version, err := readString("version")
	if err != nil {
		return Manifest{}, nil, nil, err
	}
	if version == "" {
		version = firstNonEmptyString(hints.Version, request.Version)
	}
	version = strings.TrimPrefix(strings.TrimSpace(version), "v")
	warnings := make([]string, 0)
	if version == "" {
		if source.Type == "local" {
			version = VersionLocal
			warnings = append(warnings, "external local Plugin omits version; AgentDock treats it as version=local")
		} else if candidate := semverFromGitRef(request.GitRef); candidate != "" {
			version = candidate
			warnings = append(warnings, "external Plugin version derived from SemVer git_ref")
		} else {
			return Manifest{}, nil, nil, errors.New("external Plugin has no SemVer version; provide source_version or a SemVer git_ref")
		}
	}
	if err := ValidateVersion(version); err != nil {
		return Manifest{}, nil, nil, err
	}
	description, err := readString("description")
	if err != nil {
		return Manifest{}, nil, nil, err
	}
	if description == "" {
		description = strings.TrimSpace(hints.Description)
	}
	manifest := Manifest{Schema: pluginSchemaURI, Name: name, Version: version, Description: description}
	for field, target := range map[string]*string{
		"homepage": &manifest.Homepage, "repository": &manifest.Repository, "license": &manifest.License,
	} {
		value, err := readString(field)
		if err != nil {
			return Manifest{}, nil, nil, err
		}
		*target = value
	}
	if value, ok := raw["keywords"]; ok && !isJSONEmpty(value) {
		if err := json.Unmarshal(value, &manifest.Keywords); err != nil {
			return Manifest{}, nil, nil, errors.New("keywords must be an array of strings")
		}
	}
	if value, ok := raw["author"]; ok && !isJSONEmpty(value) {
		var author ManifestAuthor
		if err := json.Unmarshal(value, &author); err != nil {
			return Manifest{}, nil, nil, errors.New("author must contain string name/email/url fields")
		}
		manifest.Author = &author
	}

	metadataFields := map[string]bool{
		"name": true, "version": true, "description": true, "author": true, "homepage": true,
		"repository": true, "license": true, "keywords": true, "$schema": true,
	}
	componentSupported := map[string]bool{"skills": true, "mcpServers": true}
	metadataIgnored := map[string]bool{}
	unsupportedLabels := map[string]string{}
	if adapter == "openai" {
		metadataIgnored = map[string]bool{"interface": true}
		unsupportedLabels = map[string]string{
			"hooks": "hooks", "agents": "agents", "commands": "commands", "apps": "apps",
			"lspServers": "lsp", "monitors": "monitors", "dependencies": "plugin dependencies",
		}
	} else {
		metadataIgnored = map[string]bool{
			"displayName": true, "metadata": true, "defaultEnabled": true,
		}
		unsupportedLabels = map[string]string{
			"commands": "commands", "agents": "agents", "workflows": "workflows", "hooks": "hooks",
			"outputStyles": "output styles", "lspServers": "lsp", "experimental": "experimental components",
			"userConfig": "user config", "channels": "channels", "dependencies": "plugin dependencies",
		}
	}
	unsupported := make([]string, 0)
	for key, value := range raw {
		if isJSONEmpty(value) || metadataFields[key] || componentSupported[key] {
			continue
		}
		if metadataIgnored[key] {
			warnings = append(warnings, fmt.Sprintf("%s manifest metadata field %s is not used by AgentDock runtime", adapter, key))
			continue
		}
		if label, ok := unsupportedLabels[key]; ok {
			unsupported = append(unsupported, label)
			continue
		}
		unsupported = append(unsupported, "unrecognized "+adapter+" manifest field "+key)
	}
	sort.Strings(unsupported)
	sort.Strings(warnings)
	return manifest, uniqueStrings(unsupported), uniqueStrings(warnings), nil
}

func normalizeSkillPaths(sourceRoot, normalizedRoot string, manifestRaw json.RawMessage, hinted []string) error {
	paths := make([]string, 0)
	if len(manifestRaw) > 0 && !isJSONEmpty(manifestRaw) {
		parsed, err := decodeStringOrArray(manifestRaw, "skills")
		if err != nil {
			return err
		}
		paths = append(paths, parsed...)
	}
	paths = append(paths, hinted...)
	seenPaths := make(map[string]struct{})
	for _, rawPath := range paths {
		relative, err := normalizeExternalComponentPath(rawPath)
		if err != nil {
			return fmt.Errorf("skills path %q: %w", rawPath, err)
		}
		if _, seen := seenPaths[relative]; seen {
			continue
		}
		seenPaths[relative] = struct{}{}
		if relative == "skills" || relative == "skills/" {
			continue
		}
		source, err := resolvePluginSourcePath(sourceRoot, relative, true)
		if err != nil {
			return fmt.Errorf("skills path %q must stay inside the Plugin source as a regular directory: %w", rawPath, err)
		}
		if regularFileExists(filepath.Join(source, "SKILL.md")) {
			if err := copyExternalSkillDirectory(source, filepath.Join(normalizedRoot, "skills", filepath.Base(source))); err != nil {
				return err
			}
			continue
		}
		entries, err := os.ReadDir(source)
		if err != nil {
			return err
		}
		for _, entry := range entries {
			if !entry.IsDir() || entry.Type()&os.ModeSymlink != 0 || !regularFileExists(filepath.Join(source, entry.Name(), "SKILL.md")) {
				continue
			}
			if err := copyExternalSkillDirectory(filepath.Join(source, entry.Name()), filepath.Join(normalizedRoot, "skills", entry.Name())); err != nil {
				return err
			}
		}
	}
	return nil
}

func copyExternalSkillDirectory(source, destination string) error {
	if info, err := os.Lstat(destination); err == nil {
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("normalized Skill destination is not a regular directory: %s", destination)
		}
		return fmt.Errorf("external Skill conflicts with existing Skill directory %s", filepath.Base(destination))
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(destination), 0o700); err != nil {
		return err
	}
	return copyPluginTree(source, destination)
}

func normalizeExternalComponentPath(value string) (string, error) {
	value = strings.TrimSpace(strings.ReplaceAll(value, "\\", "/"))
	value = strings.TrimPrefix(value, "./")
	return cleanPluginRelativePath(value)
}

func collectExternalMCPValues(root string, field json.RawMessage, defaultPath string) ([]json.RawMessage, error) {
	values := make([]json.RawMessage, 0)
	seenPaths := make(map[string]struct{})
	addPath := func(rawPath string) error {
		relative, err := normalizeExternalComponentPath(rawPath)
		if err != nil {
			return err
		}
		if _, exists := seenPaths[relative]; exists {
			return nil
		}
		path, err := resolvePluginSourcePath(root, relative, false)
		if err != nil {
			return fmt.Errorf("MCP config path %q must stay inside the Plugin source as a regular file: %w", rawPath, err)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		values = append(values, data)
		seenPaths[relative] = struct{}{}
		return nil
	}
	defaultRelative := strings.TrimPrefix(strings.TrimSpace(defaultPath), "./")
	if defaultRelative != "" && regularFileExists(filepath.Join(root, filepath.FromSlash(defaultRelative))) {
		if err := addPath(defaultRelative); err != nil {
			return nil, err
		}
	}
	if len(field) == 0 || isJSONEmpty(field) {
		return values, nil
	}
	var singlePath string
	if err := json.Unmarshal(field, &singlePath); err == nil {
		if err := addPath(singlePath); err != nil {
			return nil, err
		}
		return values, nil
	}
	var items []json.RawMessage
	if err := json.Unmarshal(field, &items); err == nil {
		for _, item := range items {
			var path string
			if err := json.Unmarshal(item, &path); err == nil {
				if err := addPath(path); err != nil {
					return nil, err
				}
				continue
			}
			var object map[string]json.RawMessage
			if err := json.Unmarshal(item, &object); err != nil || object == nil {
				return nil, errors.New("mcpServers array items must be paths or objects")
			}
			values = append(values, append(json.RawMessage(nil), item...))
		}
		return values, nil
	}
	var object map[string]json.RawMessage
	if err := json.Unmarshal(field, &object); err != nil || object == nil {
		return nil, errors.New("mcpServers must be a path, array, or object")
	}
	values = append(values, append(json.RawMessage(nil), field...))
	return values, nil
}

func writeNormalizedMCP(root string, values []json.RawMessage, adapter string) ([]string, error) {
	if len(values) == 0 {
		_ = os.Remove(filepath.Join(root, "mcp.json"))
		return nil, nil
	}
	servers := map[string]json.RawMessage{}
	unsupported := make([]string, 0)
	for _, raw := range values {
		var object map[string]json.RawMessage
		if err := json.Unmarshal(raw, &object); err != nil {
			return nil, errors.New("external MCP configuration must be a JSON object")
		}
		container := object
		if value, ok := object["mcpServers"]; ok {
			container = map[string]json.RawMessage{}
			if err := json.Unmarshal(value, &container); err != nil {
				return nil, errors.New("mcpServers must be an object")
			}
		}
		for name, configRaw := range container {
			if name == "$schema" {
				continue
			}
			if _, exists := servers[name]; exists {
				return nil, fmt.Errorf("duplicate external MCP server %q", name)
			}
			normalized, itemUnsupported, err := normalizeExternalMCPServer(name, configRaw, adapter)
			if err != nil {
				return nil, err
			}
			servers[name] = normalized
			unsupported = append(unsupported, itemUnsupported...)
		}
	}
	output := map[string]any{"$schema": mcpSchemaURI, "mcpServers": map[string]any{}}
	serverOutput := output["mcpServers"].(map[string]any)
	names := make([]string, 0, len(servers))
	for name := range servers {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		var value any
		if err := json.Unmarshal(servers[name], &value); err != nil {
			return nil, err
		}
		serverOutput[name] = value
	}
	if len(serverOutput) == 0 {
		_ = os.Remove(filepath.Join(root, "mcp.json"))
		return uniqueSortedStrings(unsupported), nil
	}
	data, err := json.MarshalIndent(output, "", "  ")
	if err != nil {
		return nil, err
	}
	data = append(data, '\n')
	if err := os.WriteFile(filepath.Join(root, "mcp.json"), data, 0o600); err != nil {
		return nil, err
	}
	return uniqueSortedStrings(unsupported), nil
}

func normalizeExternalMCPServer(name string, raw json.RawMessage, adapter string) (json.RawMessage, []string, error) {
	var object map[string]json.RawMessage
	if err := json.Unmarshal(raw, &object); err != nil {
		return nil, nil, fmt.Errorf("MCP server %s must be an object", name)
	}
	if adapter == "claude" {
		object = replaceClaudePlaceholdersInObject(object)
	}
	known := map[string]bool{
		"type": true, "url": true, "command": true, "args": true, "cwd": true, "env": true, "headers": true,
	}
	unsupported := make([]string, 0)
	for key, value := range object {
		if known[key] || isJSONEmpty(value) {
			continue
		}
		unsupported = append(unsupported, fmt.Sprintf("MCP server %s field %s", name, key))
		delete(object, key)
	}
	var transport string
	if value, ok := object["type"]; ok && !isJSONEmpty(value) {
		if err := json.Unmarshal(value, &transport); err != nil {
			return nil, nil, fmt.Errorf("MCP server %s type must be a string", name)
		}
	}
	transport = strings.ToLower(strings.TrimSpace(transport))
	if transport == "" {
		if !isJSONEmpty(object["command"]) {
			transport = "stdio"
		} else if !isJSONEmpty(object["url"]) {
			transport = "streamable-http"
		}
	}
	switch transport {
	case "http", "streamable_http", "streamable-http":
		transport = "streamable-http"
	case "stdio":
	case "sse":
		unsupported = append(unsupported, "MCP server "+name+" uses unsupported sse transport")
	default:
		return nil, nil, fmt.Errorf("MCP server %s uses unsupported transport %q", name, transport)
	}
	object["type"], _ = json.Marshal(transport)
	data, err := json.Marshal(object)
	if err != nil {
		return nil, nil, err
	}
	return data, uniqueSortedStrings(unsupported), nil
}

func replaceClaudePlaceholdersInObject(input map[string]json.RawMessage) map[string]json.RawMessage {
	out := make(map[string]json.RawMessage, len(input))
	for key, raw := range input {
		out[key] = replaceClaudePlaceholdersRaw(raw)
	}
	return out
}

func replaceClaudePlaceholdersRaw(raw json.RawMessage) json.RawMessage {
	var value any
	if json.Unmarshal(raw, &value) != nil {
		return raw
	}
	value = replaceClaudePlaceholdersValue(value)
	data, err := json.Marshal(value)
	if err != nil {
		return raw
	}
	return data
}

func replaceClaudePlaceholdersValue(value any) any {
	switch typed := value.(type) {
	case string:
		typed = strings.ReplaceAll(typed, "${CLAUDE_PLUGIN_ROOT}", "${PLUGIN_ROOT}")
		typed = strings.ReplaceAll(typed, "${CLAUDE_PLUGIN_DATA}", "${PLUGIN_DATA}")
		return typed
	case []any:
		for index := range typed {
			typed[index] = replaceClaudePlaceholdersValue(typed[index])
		}
		return typed
	case map[string]any:
		for key := range typed {
			typed[key] = replaceClaudePlaceholdersValue(typed[key])
		}
		return typed
	default:
		return value
	}
}

func writePortableManifest(root string, manifest Manifest) error {
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return os.WriteFile(filepath.Join(root, "plugin.json"), data, 0o600)
}

func detectExternalRootUnsupported(root, adapter string) []string {
	paths := map[string]string{
		"agents": "agents", "commands": "commands", "hooks.json": "hooks",
		".lsp.json": "lsp", "monitors.json": "monitors",
	}
	if adapter == "openai" {
		paths[".app.json"] = "apps"
	}
	unsupported := make([]string, 0)
	for path, label := range paths {
		if _, err := os.Lstat(filepath.Join(root, path)); err == nil {
			unsupported = append(unsupported, label)
		}
	}
	return uniqueSortedStrings(unsupported)
}

func readJSONObject(path string, maxBytes int64) (map[string]json.RawMessage, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, maxBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > maxBytes {
		return nil, fmt.Errorf("%s exceeds %d bytes", filepath.Base(path), maxBytes)
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	var object map[string]json.RawMessage
	if err := decoder.Decode(&object); err != nil {
		return nil, err
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return nil, errors.New("JSON file contains trailing values")
	}
	if object == nil {
		return nil, errors.New("JSON file must contain an object")
	}
	return object, nil
}

func decodeStringOrArray(raw json.RawMessage, field string) ([]string, error) {
	var text string
	if err := json.Unmarshal(raw, &text); err == nil {
		return []string{text}, nil
	}
	var items []string
	if err := json.Unmarshal(raw, &items); err != nil {
		return nil, fmt.Errorf("%s must be a string or array of strings", field)
	}
	return items, nil
}

func regularFileExists(path string) bool {
	info, err := os.Lstat(path)
	return err == nil && info.Mode().IsRegular() && info.Mode()&os.ModeSymlink == 0
}

func semverFromGitRef(value string) string {
	value = strings.TrimSpace(value)
	value = strings.TrimPrefix(value, "refs/tags/")
	value = strings.TrimPrefix(value, "v")
	if ValidateVersion(value) == nil && value != VersionLocal {
		return value
	}
	return ""
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func uniqueSortedStrings(values []string) []string {
	sort.Strings(values)
	return uniqueStrings(values)
}

func normalizeCompatibility(compatibility *Compatibility) {
	if compatibility == nil {
		return
	}
	compatibility.Supported = uniqueSortedStrings(compatibility.Supported)
	compatibility.Unsupported = uniqueSortedStrings(compatibility.Unsupported)
	compatibility.Warnings = uniqueSortedStrings(compatibility.Warnings)
}
