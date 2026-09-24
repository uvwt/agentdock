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
	"gopkg.in/yaml.v3"
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

	var compatibility Compatibility
	switch format {
	case pluginFormatOpenAI:
		compatibility, err = normalizeOpenAIPlugin(staged.Root, working, sourceDigest)
	case pluginFormatClaude:
		compatibility, err = normalizeClaudePlugin(staged.Root, working, sourceDigest)
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
	pkg.Unsupported = uniqueSortedStrings(append(pkg.Unsupported, compatibility.Unsupported...))
	pkg.Warnings = uniqueSortedStrings(append(pkg.Warnings, compatibility.Warnings...))
	compatibility.Unsupported = append([]string(nil), pkg.Unsupported...)
	compatibility.Warnings = append([]string(nil), pkg.Warnings...)
	normalizeCompatibility(&compatibility)
	pkg.Compatibility = compatibility
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

func normalizeOpenAIPlugin(sourceRoot, normalizedRoot, sourceDigest string) (Compatibility, error) {
	raw, err := readJSONObject(filepath.Join(sourceRoot, ".codex-plugin", "plugin.json"), maxManifestBytes)
	if err != nil {
		return Compatibility{}, err
	}
	compatibility := Compatibility{
		Format:    pluginFormatOpenAI,
		Supported: []string{"metadata", "skills", "mcp"},
	}
	manifest, unsupported, warnings, err := normalizeExternalManifest(raw, pluginFormatOpenAI, sourceDigest)
	if err != nil {
		return Compatibility{}, err
	}
	compatibility.Unsupported = append(compatibility.Unsupported, unsupported...)
	compatibility.Warnings = append(compatibility.Warnings, warnings...)

	if err := normalizeSkillPaths(sourceRoot, normalizedRoot, raw["skills"], nil); err != nil {
		return Compatibility{}, err
	}
	mcpValues, err := collectExternalMCPValues(sourceRoot, raw["mcpServers"], ".mcp.json")
	if err != nil {
		return Compatibility{}, err
	}
	mcpUnsupported, mcpWarnings, err := writeNormalizedMCP(normalizedRoot, mcpValues, pluginFormatOpenAI)
	if err != nil {
		return Compatibility{}, err
	}
	compatibility.Unsupported = append(compatibility.Unsupported, mcpUnsupported...)
	compatibility.Warnings = append(compatibility.Warnings, mcpWarnings...)

	rootUnsupported, rootWarnings := detectExternalRootIssues(sourceRoot, pluginFormatOpenAI)
	compatibility.Unsupported = append(compatibility.Unsupported, rootUnsupported...)
	compatibility.Warnings = append(compatibility.Warnings, rootWarnings...)
	if err := writePortableManifest(normalizedRoot, manifest); err != nil {
		return Compatibility{}, err
	}
	if err := removeConsumedExternalArtifacts(normalizedRoot, pluginFormatOpenAI); err != nil {
		return Compatibility{}, err
	}
	normalizeCompatibility(&compatibility)
	return compatibility, nil
}

func normalizeClaudePlugin(sourceRoot, normalizedRoot, sourceDigest string) (Compatibility, error) {
	raw, err := readJSONObject(filepath.Join(sourceRoot, ".claude-plugin", "plugin.json"), maxManifestBytes)
	if err != nil {
		return Compatibility{}, err
	}
	compatibility := Compatibility{
		Format:    pluginFormatClaude,
		Supported: []string{"metadata", "skills", "mcp"},
	}
	manifest, unsupported, warnings, err := normalizeExternalManifest(raw, pluginFormatClaude, sourceDigest)
	if err != nil {
		return Compatibility{}, err
	}
	compatibility.Unsupported = append(compatibility.Unsupported, unsupported...)
	compatibility.Warnings = append(compatibility.Warnings, warnings...)

	skillHints := []string(nil)
	if regularFileExists(filepath.Join(sourceRoot, "SKILL.md")) {
		skillHints = append(skillHints, ".")
	}
	if err := normalizeSkillPaths(sourceRoot, normalizedRoot, raw["skills"], skillHints); err != nil {
		return Compatibility{}, err
	}
	if err := normalizeClaudeSkillDocuments(normalizedRoot); err != nil {
		return Compatibility{}, err
	}
	mcpValues, err := collectExternalMCPValues(sourceRoot, raw["mcpServers"], ".mcp.json")
	if err != nil {
		return Compatibility{}, err
	}
	mcpUnsupported, mcpWarnings, err := writeNormalizedMCP(normalizedRoot, mcpValues, pluginFormatClaude)
	if err != nil {
		return Compatibility{}, err
	}
	compatibility.Unsupported = append(compatibility.Unsupported, mcpUnsupported...)
	compatibility.Warnings = append(compatibility.Warnings, mcpWarnings...)

	rootUnsupported, rootWarnings := detectExternalRootIssues(sourceRoot, pluginFormatClaude)
	compatibility.Unsupported = append(compatibility.Unsupported, rootUnsupported...)
	compatibility.Warnings = append(compatibility.Warnings, rootWarnings...)
	if err := writePortableManifest(normalizedRoot, manifest); err != nil {
		return Compatibility{}, err
	}
	if err := removeConsumedExternalArtifacts(normalizedRoot, pluginFormatClaude); err != nil {
		return Compatibility{}, err
	}
	normalizeCompatibility(&compatibility)
	return compatibility, nil
}

func normalizeExternalManifest(raw map[string]json.RawMessage, format, sourceDigest string) (Manifest, []string, []string, error) {
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
	if err := ValidateName(name); err != nil {
		return Manifest{}, nil, nil, fmt.Errorf("external Plugin name: %w", err)
	}
	version, err := readString("version")
	if err != nil {
		return Manifest{}, nil, nil, err
	}
	version = strings.TrimPrefix(strings.TrimSpace(version), "v")
	warnings := make([]string, 0)
	if version == "" {
		version = VersionLocal
		warnings = append(warnings, format+" Plugin omits version; AgentDock treats it as version=local")
	}
	if err := ValidateVersion(version); err != nil {
		return Manifest{}, nil, nil, err
	}
	description, err := readString("description")
	if err != nil {
		return Manifest{}, nil, nil, err
	}

	manifest := Manifest{
		Schema:      pluginSchemaURI,
		Name:        name,
		Version:     version,
		Description: description,
	}
	for field, target := range map[string]*string{
		"homepage":   &manifest.Homepage,
		"repository": &manifest.Repository,
		"license":    &manifest.License,
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

	origin := strings.TrimSpace(manifest.Repository)
	if origin == "" {
		origin = strings.TrimSpace(manifest.Homepage)
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
		Format:   format,
		Adapted:  true,
	}

	metadataFields := map[string]bool{
		"name": true, "version": true, "description": true, "author": true,
		"homepage": true, "repository": true, "license": true, "keywords": true,
		"$schema": true,
	}
	componentSupported := map[string]bool{"skills": true, "mcpServers": true}
	metadataIgnored := map[string]bool{}
	omittedWithWarning := map[string]string{"commands": "commands"}
	unsupportedLabels := map[string]string{}
	if format == pluginFormatOpenAI {
		metadataIgnored = map[string]bool{"interface": true}
		unsupportedLabels = map[string]string{
			"hooks": "hooks", "agents": "agents", "apps": "apps",
			"lspServers": "lsp", "monitors": "monitors",
			"dependencies": "plugin dependencies",
		}
	} else {
		metadataIgnored = map[string]bool{
			"displayName": true, "metadata": true, "defaultEnabled": true,
		}
		unsupportedLabels = map[string]string{
			"agents": "agents", "workflows": "workflows", "hooks": "hooks",
			"outputStyles": "output styles", "output-styles": "output styles",
			"themes": "themes", "monitors": "monitors", "lspServers": "lsp",
			"experimental": "experimental components", "userConfig": "user config",
			"channels": "channels", "dependencies": "plugin dependencies",
		}
	}

	unsupported := make([]string, 0)
	for key, value := range raw {
		if isJSONEmpty(value) || metadataFields[key] || componentSupported[key] {
			continue
		}
		if metadataIgnored[key] {
			warnings = append(warnings, fmt.Sprintf("%s manifest metadata field %s is not used by AgentDock runtime", format, key))
			continue
		}
		if label, ok := omittedWithWarning[key]; ok {
			warnings = append(warnings, fmt.Sprintf("%s component %s is not supported and was omitted during conversion", format, label))
			continue
		}
		if label, ok := unsupportedLabels[key]; ok {
			unsupported = append(unsupported, label)
			continue
		}
		unsupported = append(unsupported, "unrecognized "+format+" manifest field "+key)
	}
	return manifest, uniqueSortedStrings(unsupported), uniqueSortedStrings(warnings), nil
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

func normalizeClaudeSkillDocuments(root string) error {
	skillsRoot := filepath.Join(root, "skills")
	entries, err := os.ReadDir(skillsRoot)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if !entry.IsDir() || entry.Type()&os.ModeSymlink != 0 {
			continue
		}
		path := filepath.Join(skillsRoot, entry.Name(), "SKILL.md")
		if !regularFileExists(path) {
			continue
		}
		if err := normalizeClaudeSkillAllowedTools(path); err != nil {
			return fmt.Errorf("normalize Claude Skill %q: %w", entry.Name(), err)
		}
	}
	return nil
}

func normalizeClaudeSkillAllowedTools(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	text := strings.ReplaceAll(string(data), "\r\n", "\n")
	lines := strings.Split(text, "\n")
	if len(lines) < 3 || strings.TrimSpace(lines[0]) != "---" {
		return errors.New("SKILL.md must start with YAML frontmatter")
	}
	endLine := -1
	for index := 1; index < len(lines); index++ {
		if strings.TrimSpace(lines[index]) == "---" {
			endLine = index
			break
		}
	}
	if endLine < 0 {
		return errors.New("SKILL.md frontmatter must be closed by ---")
	}

	var document yaml.Node
	if err := yaml.Unmarshal([]byte(strings.Join(lines[1:endLine], "\n")), &document); err != nil {
		return fmt.Errorf("decode SKILL.md frontmatter: %w", err)
	}
	if len(document.Content) != 1 || document.Content[0].Kind != yaml.MappingNode {
		return errors.New("SKILL.md frontmatter must be a YAML mapping")
	}

	frontmatter := document.Content[0]
	changed := false
	for index := 0; index+1 < len(frontmatter.Content); index += 2 {
		if frontmatter.Content[index].Value != "allowed-tools" {
			continue
		}
		value := frontmatter.Content[index+1]
		if value.Kind != yaml.SequenceNode {
			continue
		}
		tools := make([]string, 0, len(value.Content))
		for _, item := range value.Content {
			if item.Kind != yaml.ScalarNode || item.Tag != "!!str" {
				return errors.New("allowed-tools array items must be strings")
			}
			tool := strings.TrimSpace(item.Value)
			if tool == "" {
				return errors.New("allowed-tools array items must be non-empty strings")
			}
			tools = append(tools, tool)
		}

		// Claude Plugin 允许 YAML 数组；AgentDock 内部 Portable Skill 统一保存为单行字符串。
		value.Kind = yaml.ScalarNode
		value.Tag = "!!str"
		value.Value = strings.Join(tools, " ")
		value.Content = nil
		value.Style = 0
		changed = true
	}
	if !changed {
		return nil
	}

	normalizedFrontmatter, err := yaml.Marshal(&document)
	if err != nil {
		return err
	}
	output := "---\n" + string(normalizedFrontmatter) + "---\n" + strings.Join(lines[endLine+1:], "\n")
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	return os.WriteFile(path, []byte(output), info.Mode().Perm())
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

func writeNormalizedMCP(root string, values []json.RawMessage, format string) ([]string, []string, error) {
	if len(values) == 0 {
		_ = os.Remove(filepath.Join(root, "mcp.json"))
		return nil, nil, nil
	}
	servers := map[string]json.RawMessage{}
	unsupported := make([]string, 0)
	warnings := make([]string, 0)
	for _, raw := range values {
		var object map[string]json.RawMessage
		if err := json.Unmarshal(raw, &object); err != nil {
			return nil, nil, errors.New("external MCP configuration must be a JSON object")
		}
		container := object
		if value, ok := object["mcpServers"]; ok {
			container = map[string]json.RawMessage{}
			if err := json.Unmarshal(value, &container); err != nil {
				return nil, nil, errors.New("mcpServers must be an object")
			}
		}
		for name, configRaw := range container {
			if name == "$schema" {
				continue
			}
			if _, exists := servers[name]; exists {
				return nil, nil, fmt.Errorf("duplicate external MCP server %q", name)
			}
			normalized, itemUnsupported, itemWarnings, err := normalizeExternalMCPServer(name, configRaw, format)
			if err != nil {
				return nil, nil, err
			}
			servers[name] = normalized
			unsupported = append(unsupported, itemUnsupported...)
			warnings = append(warnings, itemWarnings...)
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
			return nil, nil, err
		}
		serverOutput[name] = value
	}
	if len(serverOutput) == 0 {
		_ = os.Remove(filepath.Join(root, "mcp.json"))
		return uniqueSortedStrings(unsupported), uniqueSortedStrings(warnings), nil
	}
	data, err := json.MarshalIndent(output, "", "  ")
	if err != nil {
		return nil, nil, err
	}
	data = append(data, '\n')
	if err := os.WriteFile(filepath.Join(root, "mcp.json"), data, 0o600); err != nil {
		return nil, nil, err
	}
	return uniqueSortedStrings(unsupported), uniqueSortedStrings(warnings), nil
}

func normalizeExternalMCPServer(name string, raw json.RawMessage, format string) (json.RawMessage, []string, []string, error) {
	var object map[string]json.RawMessage
	if err := json.Unmarshal(raw, &object); err != nil {
		return nil, nil, nil, fmt.Errorf("MCP server %s must be an object", name)
	}
	if format == pluginFormatClaude {
		object = replaceClaudePlaceholdersInObject(object)
	}

	known := map[string]bool{
		"type": true, "url": true, "command": true, "args": true,
		"cwd": true, "env": true, "headers": true,
	}
	metadata := map[string]bool{"note": true, "description": true}
	unsupported := make([]string, 0)
	warnings := make([]string, 0)
	for key, value := range object {
		if known[key] || isJSONEmpty(value) {
			continue
		}
		if metadata[key] {
			warnings = append(warnings, fmt.Sprintf("MCP server %s metadata field %s is not used by AgentDock runtime", name, key))
			delete(object, key)
			continue
		}
		unsupported = append(unsupported, fmt.Sprintf("MCP server %s field %s", name, key))
		delete(object, key)
	}

	var transport string
	if value, ok := object["type"]; ok && !isJSONEmpty(value) {
		if err := json.Unmarshal(value, &transport); err != nil {
			return nil, nil, nil, fmt.Errorf("MCP server %s type must be a string", name)
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
		return nil, nil, nil, fmt.Errorf("MCP server %s uses unsupported transport %q", name, transport)
	}
	object["type"], _ = json.Marshal(transport)
	data, err := json.Marshal(object)
	if err != nil {
		return nil, nil, nil, err
	}
	return data, uniqueSortedStrings(unsupported), uniqueSortedStrings(warnings), nil
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

// removeConsumedExternalArtifacts keeps the reviewed snapshot canonical: vendor
// manifests/configs that were already translated must not survive as inert
// second sources of truth. commands/ is deliberately omitted from runtime
// semantics, so it is removed after the conversion warning is recorded.
func removeConsumedExternalArtifacts(root, format string) error {
	paths := []string{".mcp.json", "commands"}
	switch format {
	case pluginFormatOpenAI:
		paths = append(paths, ".codex-plugin")
	case pluginFormatClaude:
		paths = append(paths, ".claude-plugin")
	default:
		return fmt.Errorf("cannot clean artifacts for unsupported Plugin format %q", format)
	}
	for _, relative := range paths {
		path := filepath.Join(root, filepath.FromSlash(relative))
		if err := os.RemoveAll(path); err != nil {
			return fmt.Errorf("remove converted Plugin artifact %s: %w", relative, err)
		}
	}
	return nil
}

func detectExternalRootIssues(root, format string) ([]string, []string) {
	unsupportedPaths := map[string]string{
		"agents": "agents", "hooks": "hooks", "hooks.json": "hooks",
		".lsp.json": "lsp", "monitors": "monitors", "monitors.json": "monitors",
	}
	warningPaths := map[string]string{"commands": "commands"}
	if format == pluginFormatClaude {
		unsupportedPaths["workflows"] = "workflows"
		unsupportedPaths["output-styles"] = "output styles"
		unsupportedPaths["themes"] = "themes"
		unsupportedPaths["bin"] = "bin PATH executables"
		unsupportedPaths["settings.json"] = "settings"
	}
	if format == pluginFormatOpenAI {
		unsupportedPaths[".app.json"] = "apps"
	}

	unsupported := make([]string, 0)
	warnings := make([]string, 0)
	for path, label := range unsupportedPaths {
		if _, err := os.Lstat(filepath.Join(root, path)); err == nil {
			unsupported = append(unsupported, label)
		}
	}
	for path, label := range warningPaths {
		if _, err := os.Lstat(filepath.Join(root, path)); err == nil {
			warnings = append(warnings, fmt.Sprintf("%s component %s is not supported and was omitted during conversion", format, label))
		}
	}
	return uniqueSortedStrings(unsupported), uniqueSortedStrings(warnings)
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

func normalizeCompatibility(compatibility *Compatibility) {
	if compatibility == nil {
		return
	}
	compatibility.Supported = uniqueSortedStrings(compatibility.Supported)
	compatibility.Unsupported = uniqueSortedStrings(compatibility.Unsupported)
	compatibility.Warnings = uniqueSortedStrings(compatibility.Warnings)
}
