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

	skills "github.com/uvwt/agentdock/internal/skill"
)

const (
	pluginFormatPortable = "portable"
	pluginFormatOpenAI   = "openai"
	pluginFormatClaude   = "claude"
)

// normalizeStagedPlugin keeps the public Plugin API format-agnostic. A local
// directory/ZIP is first snapshotted by source.go, then this function detects
// the package dialect and, when necessary, materializes one canonical Portable
// Plugin snapshot before review/install.
//
// The returned package is always loaded from the exact canonical tree that will
// be hashed, reviewed and eventually committed.
func (m *Manager) normalizeStagedPlugin(staged stagedPluginSource) (string, Package, func(), error) {
	format, err := detectPluginFormat(staged.Root)
	if err != nil {
		return "", Package{}, func() {}, err
	}
	if format == pluginFormatPortable {
		if staged.ImportProvenance != nil {
			canonical, err := m.store.TempPath("portable-canonical")
			if err != nil {
				return "", Package{}, func() {}, err
			}
			cleanup := func() { _ = os.RemoveAll(canonical) }
			if err := snapshotPluginTree(staged.Root, canonical, maxPluginExtractedBytes, maxPluginArchiveFiles); err != nil {
				cleanup()
				return "", Package{}, func() {}, pluginError("PLUGIN_FORMAT_CONVERSION_FAILED", "format.portable_snapshot", err)
			}
			if err := writePortableImportProvenance(canonical, staged.ImportProvenance); err != nil {
				cleanup()
				return "", Package{}, func() {}, pluginError("PLUGIN_FORMAT_CONVERSION_FAILED", "format.portable_provenance", err)
			}
			pkg, err := LoadPackage(canonical)
			if err != nil {
				cleanup()
				return "", Package{}, func() {}, err
			}
			return canonical, pkg, cleanup, nil
		}
		pkg, err := LoadPackage(staged.Root)
		if err != nil {
			return "", Package{}, func() {}, err
		}
		return staged.Root, pkg, func() {}, nil
	}

	sourceDigest, err := skills.DigestPackageContent(staged.Root)
	if err != nil {
		return "", Package{}, func() {}, pluginError("PLUGIN_FORMAT_CONVERSION_FAILED", "format.digest", err)
	}
	working, err := m.store.TempPath("normalize-working")
	if err != nil {
		return "", Package{}, func() {}, err
	}
	cleanupWorking := func() { _ = os.RemoveAll(working) }
	if err := snapshotPluginTree(staged.Root, working, maxPluginExtractedBytes, maxPluginArchiveFiles); err != nil {
		cleanupWorking()
		return "", Package{}, func() {}, pluginError("PLUGIN_FORMAT_CONVERSION_FAILED", "format.snapshot", err)
	}

	var conversionWarnings []string
	switch format {
	case pluginFormatOpenAI:
		conversionWarnings, err = normalizeOpenAIPlugin(staged.Root, working, sourceDigest, staged.ImportProvenance)
	case pluginFormatClaude:
		conversionWarnings, err = normalizeClaudePlugin(staged.Root, working, sourceDigest, staged.ImportProvenance)
	default:
		err = fmt.Errorf("unsupported Plugin format %q", format)
	}
	if err != nil {
		cleanupWorking()
		return "", Package{}, func() {}, pluginError("PLUGIN_FORMAT_CONVERSION_FAILED", "format."+format, err)
	}

	// Re-snapshot the converted tree so the exact review/install candidate is
	// independently subject to the same file-count, byte and symlink limits as
	// the original source.
	canonical, err := m.store.TempPath("canonical")
	if err != nil {
		cleanupWorking()
		return "", Package{}, func() {}, err
	}
	cleanup := func() {
		_ = os.RemoveAll(canonical)
		cleanupWorking()
	}
	if err := snapshotPluginTree(working, canonical, maxPluginExtractedBytes, maxPluginArchiveFiles); err != nil {
		cleanup()
		return "", Package{}, func() {}, pluginError("PLUGIN_FORMAT_CONVERSION_FAILED", "format.canonical_snapshot", err)
	}

	pkg, err := LoadPackage(canonical)
	if err != nil {
		cleanup()
		return "", Package{}, func() {}, err
	}
	pkg.Warnings = uniqueSortedStrings(append(pkg.Warnings, conversionWarnings...))
	pkg.Format = format
	return canonical, pkg, cleanup, nil
}

func detectPluginFormat(root string) (string, error) {
	hasPortable := regularFileExists(filepath.Join(root, "plugin.json"))
	hasOpenAI := regularFileExists(filepath.Join(root, ".codex-plugin", "plugin.json"))
	hasClaude := regularFileExists(filepath.Join(root, ".claude-plugin", "plugin.json"))

	// A root Portable manifest is authoritative. If it is malformed, normal
	// Portable validation should fail rather than silently falling back to a
	// vendor manifest in the same tree.
	if hasPortable {
		return pluginFormatPortable, nil
	}
	if hasOpenAI && hasClaude {
		return "", pluginError(
			"PLUGIN_FORMAT_AMBIGUOUS",
			"format.detect",
			errors.New("source contains both OpenAI and Claude Plugin manifests"),
		)
	}
	switch {
	case hasOpenAI:
		return pluginFormatOpenAI, nil
	case hasClaude:
		return pluginFormatClaude, nil
	default:
		return "", pluginError(
			"PLUGIN_FORMAT_NOT_FOUND",
			"format.detect",
			errors.New("no AgentDock Portable, OpenAI, or Claude Plugin manifest was found"),
		)
	}
}

func normalizeOpenAIPlugin(sourceRoot, normalizedRoot, sourceDigest string, importProvenance *Provenance) ([]string, error) {
	raw, err := readJSONObject(filepath.Join(sourceRoot, ".codex-plugin", "plugin.json"), maxManifestBytes)
	if err != nil {
		return nil, err
	}
	manifest, warnings, err := normalizeExternalManifest(raw, pluginFormatOpenAI, sourceDigest, importProvenance)
	if err != nil {
		return nil, err
	}

	if err := normalizeSkillPaths(sourceRoot, normalizedRoot, raw["skills"], nil); err != nil {
		return nil, err
	}
	mcpValues, err := collectExternalMCPValues(sourceRoot, raw["mcpServers"], ".mcp.json")
	if err != nil {
		return nil, err
	}
	mcpWarnings, err := writeNormalizedMCP(normalizedRoot, mcpValues, pluginFormatOpenAI)
	if err != nil {
		return nil, err
	}
	warnings = append(warnings, mcpWarnings...)

	warnings = append(warnings, detectExternalRootIssues(sourceRoot, pluginFormatOpenAI)...)
	if err := writePortableManifest(normalizedRoot, manifest); err != nil {
		return nil, err
	}
	return uniqueSortedStrings(warnings), nil
}

func normalizeClaudePlugin(sourceRoot, normalizedRoot, sourceDigest string, importProvenance *Provenance) ([]string, error) {
	raw, err := readJSONObject(filepath.Join(sourceRoot, ".claude-plugin", "plugin.json"), maxManifestBytes)
	if err != nil {
		return nil, err
	}
	manifest, warnings, err := normalizeExternalManifest(raw, pluginFormatClaude, sourceDigest, importProvenance)
	if err != nil {
		return nil, err
	}

	skillHints := []string(nil)
	if regularFileExists(filepath.Join(sourceRoot, "SKILL.md")) {
		skillHints = append(skillHints, ".")
	}
	if err := normalizeSkillPaths(sourceRoot, normalizedRoot, raw["skills"], skillHints); err != nil {
		return nil, err
	}
	mcpValues, err := collectExternalMCPValues(sourceRoot, raw["mcpServers"], ".mcp.json")
	if err != nil {
		return nil, err
	}
	mcpWarnings, err := writeNormalizedMCP(normalizedRoot, mcpValues, pluginFormatClaude)
	if err != nil {
		return nil, err
	}
	warnings = append(warnings, mcpWarnings...)

	warnings = append(warnings, detectExternalRootIssues(sourceRoot, pluginFormatClaude)...)
	if err := writePortableManifest(normalizedRoot, manifest); err != nil {
		return nil, err
	}
	return uniqueSortedStrings(warnings), nil
}

func normalizeExternalManifest(raw map[string]json.RawMessage, format, sourceDigest string, importProvenance *Provenance) (Manifest, []string, error) {
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
		return Manifest{}, nil, err
	}
	if err := ValidateName(name); err != nil {
		return Manifest{}, nil, fmt.Errorf("external Plugin name: %w", err)
	}
	version, err := readString("version")
	if err != nil {
		return Manifest{}, nil, err
	}
	warnings := make([]string, 0)
	if version == "" {
		version = VersionLocal
		warnings = append(warnings, format+" Plugin omits version; AgentDock treats it as version=local")
	}
	if err := ValidateVersion(version); err != nil {
		return Manifest{}, nil, err
	}
	description, err := readString("description")
	if err != nil {
		return Manifest{}, nil, err
	}

	manifest := Manifest{Name: name, Version: version, Description: description}

	if importProvenance != nil {
		provenance := *importProvenance
		manifest.Provenance = &provenance
	} else {
		origin := optionalJSONString(raw["repository"])
		if origin == "" {
			origin = optionalJSONString(raw["homepage"])
		}
		if origin == "" {
			origin = strings.TrimSpace(sourceDigest)
		}
		revision := ""
		if origin != sourceDigest {
			revision = sourceDigest
		}
		manifest.Provenance = &Provenance{
			Origin:   origin,
			Revision: revision,
		}
	}

	inert := map[string]string{"commands": "commands"}
	if format == pluginFormatOpenAI {
		inert = map[string]string{
			"commands": "commands",
			"hooks":    "hooks", "agents": "agents", "apps": "apps",
			"lspServers": "lsp", "monitors": "monitors",
			"dependencies": "plugin dependencies",
		}
	} else {
		inert = map[string]string{
			"commands": "commands",
			"agents":   "agents", "workflows": "workflows", "hooks": "hooks",
			"outputStyles": "output styles", "output-styles": "output styles",
			"themes": "themes", "monitors": "monitors", "lspServers": "lsp",
			"experimental": "experimental components", "userConfig": "user config",
			"channels": "channels", "dependencies": "plugin dependencies",
		}
	}

	for key, value := range raw {
		if label, ok := inert[key]; ok && !isJSONEmpty(value) {
			warnings = append(warnings, fmt.Sprintf("%s component %s is preserved but not executed by AgentDock", format, label))
		}
	}
	return manifest, uniqueSortedStrings(warnings), nil
}

func optionalJSONString(raw json.RawMessage) string {
	var value string
	if len(raw) == 0 || json.Unmarshal(raw, &value) != nil {
		return ""
	}
	return strings.TrimSpace(value)
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

		// skills/ is already in the Portable location because the whole source
		// tree is copied before conversion.
		if relative == "skills" || relative == "skills/" {
			continue
		}
		source := sourceRoot
		if relative != "." {
			source, err = resolvePluginSourcePath(sourceRoot, relative, true)
			if err != nil {
				return fmt.Errorf("skills path %q must stay inside the Plugin source as a regular directory: %w", rawPath, err)
			}
		}
		if regularFileExists(filepath.Join(source, "SKILL.md")) {
			doc, err := skills.LoadSkillDocument(source)
			if err != nil {
				return fmt.Errorf("load external Skill %q: %w", rawPath, err)
			}
			if err := copyExternalSkillDirectory(source, filepath.Join(normalizedRoot, "skills", doc.Name)); err != nil {
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
	return snapshotPluginTree(source, destination, maxPluginExtractedBytes, maxPluginArchiveFiles)
}

func normalizeExternalComponentPath(value string) (string, error) {
	value = strings.TrimSpace(strings.ReplaceAll(value, "\\", "/"))
	if value == "." || value == "./" {
		return ".", nil
	}
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

func writeNormalizedMCP(root string, values []json.RawMessage, format string) ([]string, error) {
	if len(values) == 0 {
		_ = os.Remove(filepath.Join(root, "mcp.json"))
		return nil, nil
	}
	servers := map[string]json.RawMessage{}
	warnings := make([]string, 0)
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
			normalized, itemWarnings, err := normalizeExternalMCPServer(name, configRaw, format)
			if err != nil {
				return nil, err
			}
			warnings = append(warnings, itemWarnings...)
			if len(normalized) > 0 {
				servers[name] = normalized
			}
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
		return uniqueSortedStrings(warnings), nil
	}
	data, err := json.MarshalIndent(output, "", "  ")
	if err != nil {
		return nil, err
	}
	data = append(data, '\n')
	if err := os.WriteFile(filepath.Join(root, "mcp.json"), data, 0o600); err != nil {
		return nil, err
	}
	return uniqueSortedStrings(warnings), nil
}

func normalizeExternalMCPServer(name string, raw json.RawMessage, format string) (json.RawMessage, []string, error) {
	var object map[string]json.RawMessage
	if err := json.Unmarshal(raw, &object); err != nil {
		return nil, []string{fmt.Sprintf("%s MCP server %s is preserved but not activated because its configuration is not an object", format, name)}, nil
	}
	if format == pluginFormatClaude {
		object = replaceClaudePlaceholdersInObject(object)
	}

	known := map[string]bool{
		"type": true, "url": true, "command": true, "args": true,
		"cwd": true, "env": true, "headers": true,
	}
	metadata := map[string]bool{"note": true, "description": true}
	warnings := make([]string, 0)
	unknownRuntime := make([]string, 0)
	for key, value := range object {
		if known[key] || isJSONEmpty(value) {
			continue
		}
		if metadata[key] {
			warnings = append(warnings, fmt.Sprintf("MCP server %s metadata field %s is not used by AgentDock runtime", name, key))
			delete(object, key)
			continue
		}
		unknownRuntime = append(unknownRuntime, key)
	}
	if len(unknownRuntime) > 0 {
		sort.Strings(unknownRuntime)
		warnings = append(warnings, fmt.Sprintf("%s MCP server %s is preserved but not activated because AgentDock does not interpret fields: %s", format, name, strings.Join(unknownRuntime, ", ")))
		return nil, uniqueSortedStrings(warnings), nil
	}

	var transport string
	if value, ok := object["type"]; ok && !isJSONEmpty(value) {
		if err := json.Unmarshal(value, &transport); err != nil {
			return nil, []string{fmt.Sprintf("%s MCP server %s is preserved but not activated because type is not a string", format, name)}, nil
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
		return nil, []string{fmt.Sprintf("%s MCP server %s is preserved but not activated because SSE transport is not supported", format, name)}, nil
	default:
		return nil, []string{fmt.Sprintf("%s MCP server %s is preserved but not activated because transport %q is not supported", format, name, transport)}, nil
	}
	object["type"], _ = json.Marshal(transport)
	data, err := json.Marshal(object)
	if err != nil {
		return nil, nil, err
	}
	return data, uniqueSortedStrings(warnings), nil
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

func writePortableImportProvenance(root string, imported *Provenance) error {
	raw, err := readJSONObject(filepath.Join(root, "plugin.json"), maxManifestBytes)
	if err != nil {
		return err
	}
	provenance := *imported
	encoded, err := json.Marshal(provenance)
	if err != nil {
		return err
	}
	raw["provenance"] = encoded
	data, err := json.MarshalIndent(raw, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return os.WriteFile(filepath.Join(root, "plugin.json"), data, 0o600)
}

func detectExternalRootIssues(root, format string) []string {
	inertPaths := map[string]string{
		"agents": "agents", "hooks": "hooks", "hooks.json": "hooks",
		".lsp.json": "lsp", "monitors": "monitors", "monitors.json": "monitors",
		"commands": "commands",
	}
	if format == pluginFormatClaude {
		inertPaths["workflows"] = "workflows"
		inertPaths["output-styles"] = "output styles"
		inertPaths["themes"] = "themes"
		inertPaths["bin"] = "bin PATH executables"
		inertPaths["settings.json"] = "settings"
	}
	if format == pluginFormatOpenAI {
		inertPaths[".app.json"] = "apps"
	}

	warnings := make([]string, 0)
	for path, label := range inertPaths {
		if _, err := os.Lstat(filepath.Join(root, path)); err == nil {
			warnings = append(warnings, fmt.Sprintf("%s component %s is preserved but not executed by AgentDock", format, label))
		}
	}
	return uniqueSortedStrings(warnings)
}

func resolvePluginSourcePath(root, relative string, wantDirectory bool) (string, error) {
	clean, err := cleanPluginRelativePath(relative)
	if err != nil {
		return "", err
	}
	root = filepath.Clean(root)
	handle, err := os.OpenRoot(root)
	if err != nil {
		return "", err
	}
	defer handle.Close()

	current := ""
	parts := strings.Split(filepath.FromSlash(clean), string(filepath.Separator))
	var info os.FileInfo
	for index, part := range parts {
		if current == "" {
			current = part
		} else {
			current = filepath.Join(current, part)
		}
		info, err = handle.Lstat(current)
		if err != nil {
			return "", err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return "", fmt.Errorf("path contains symlink component %q", filepath.ToSlash(current))
		}
		if index < len(parts)-1 && !info.IsDir() {
			return "", fmt.Errorf("path component %q is not a directory", filepath.ToSlash(current))
		}
	}
	if wantDirectory {
		if info == nil || !info.IsDir() {
			return "", errors.New("path is not a directory")
		}
	} else if info == nil || !info.Mode().IsRegular() {
		return "", errors.New("path is not a regular file")
	}
	return filepath.Join(root, filepath.FromSlash(clean)), nil
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

func uniqueSortedStrings(values []string) []string {
	sort.Strings(values)
	return uniqueStrings(values)
}
