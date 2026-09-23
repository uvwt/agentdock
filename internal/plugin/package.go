package plugin

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"

	skills "github.com/uvwt/agentdock/internal/skill"
)

const (
	maxManifestBytes = 1 << 20
	pluginSchemaURI  = "https://agent-plugins.org/schemas/1.0.0/plugin.schema.json"
)

var unsupportedManifestFields = map[string]string{
	"hooks":              "hooks",
	"agents":             "agents",
	"lsp":                "lsp",
	"monitors":           "monitors",
	"install":            "install scripts",
	"install_script":     "install scripts",
	"installScripts":     "install scripts",
	"dependencies":       "plugin dependencies",
	"pluginDependencies": "plugin dependencies",
}

func LoadPackage(root string) (Package, error) {
	root = filepath.Clean(strings.TrimSpace(root))
	if root == "." || root == "" {
		return Package{}, pluginError("PLUGIN_PACKAGE_INVALID", "package.root", errors.New("Plugin package root is required"))
	}
	info, err := os.Lstat(root)
	if err != nil {
		return Package{}, pluginError("PLUGIN_PACKAGE_INVALID", "package.root", err)
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return Package{}, pluginError("PLUGIN_PACKAGE_INVALID", "package.root", errors.New("Plugin package root must be a regular directory"))
	}
	treeUnsupported, err := validatePackageTree(root)
	if err != nil {
		return Package{}, err
	}

	manifest, unsupported, warnings, err := loadManifest(filepath.Join(root, "plugin.json"))
	if err != nil {
		return Package{}, err
	}
	components := ComponentIndex{Skills: []SkillComponent{}, MCP: []MCPComponent{}}

	skillRoot := filepath.Join(root, "skills")
	if entries, readErr := os.ReadDir(skillRoot); readErr == nil {
		for _, entry := range entries {
			if skills.IsIgnoredPackageMetadataPath(entry.Name()) {
				continue
			}
			if !entry.IsDir() || entry.Type()&os.ModeSymlink != 0 {
				return Package{}, pluginError("PLUGIN_PACKAGE_INVALID", "skills", fmt.Errorf("skills/%s must be a regular directory", entry.Name()))
			}
			componentRoot := filepath.Join(skillRoot, entry.Name())
			if err := skills.ValidatePackage(componentRoot); err != nil {
				return Package{}, pluginError("PLUGIN_SKILL_INVALID", "skills."+entry.Name(), err)
			}
			doc, err := skills.LoadSkillDocument(componentRoot)
			if err != nil {
				return Package{}, pluginError("PLUGIN_SKILL_INVALID", "skills."+entry.Name(), err)
			}
			if doc.Name != entry.Name() {
				return Package{}, pluginError("PLUGIN_SKILL_INVALID", "skills."+entry.Name(), fmt.Errorf("SKILL.md name %q must match directory name %q", doc.Name, entry.Name()))
			}
			digest, err := skills.DigestPackageContent(componentRoot)
			if err != nil {
				return Package{}, pluginError("PLUGIN_SKILL_INVALID", "skills."+entry.Name()+".digest", err)
			}
			components.Skills = append(components.Skills, SkillComponent{
				Name: doc.Name, Description: strings.TrimSpace(doc.Description),
				RelativePath:  filepath.ToSlash(filepath.Join("skills", entry.Name())),
				ContentDigest: digest,
			})
		}
	} else if !errors.Is(readErr, os.ErrNotExist) {
		return Package{}, pluginError("PLUGIN_PACKAGE_INVALID", "skills", readErr)
	}
	sort.Slice(components.Skills, func(i, j int) bool { return components.Skills[i].Name < components.Skills[j].Name })

	mcpPath := filepath.Join(root, "mcp.json")
	if _, statErr := os.Lstat(mcpPath); statErr == nil {
		mcp, mcpUnsupported, err := loadMCPFile(mcpPath, root, manifest.Name)
		if err != nil {
			return Package{}, err
		}
		components.MCP = mcp
		unsupported = append(unsupported, mcpUnsupported...)
	} else if !errors.Is(statErr, os.ErrNotExist) {
		return Package{}, pluginError("PLUGIN_MCP_INVALID", "mcp", statErr)
	}

	executables, err := collectExecutableFiles(root)
	if err != nil {
		return Package{}, pluginError("PLUGIN_PACKAGE_INVALID", "package.executables", err)
	}
	digest, err := skills.DigestPackageContent(root)
	if err != nil {
		return Package{}, pluginError("PLUGIN_PACKAGE_INVALID", "package.digest", err)
	}
	unsupported = append(unsupported, treeUnsupported...)
	sort.Strings(unsupported)
	unsupported = uniqueStrings(unsupported)
	sort.Strings(warnings)
	warnings = uniqueStrings(warnings)

	supported := []string{"metadata"}
	if len(components.Skills) > 0 {
		supported = append(supported, "skills")
	}
	if len(components.MCP) > 0 {
		supported = append(supported, "mcp")
	}
	sort.Strings(supported)
	compatibility := Compatibility{
		Format: "portable", Supported: supported,
		Unsupported: append([]string(nil), unsupported...),
		Warnings:    append([]string(nil), warnings...),
	}
	return Package{
		Root: root, Manifest: manifest, PackageDigest: digest,
		Components: components, Unsupported: unsupported, Warnings: warnings, Executables: executables,
		Compatibility: compatibility,
	}, nil
}

func loadManifest(path string) (Manifest, []string, []string, error) {
	file, err := os.Open(path)
	if err != nil {
		return Manifest{}, nil, nil, pluginError("PLUGIN_MANIFEST_INVALID", "manifest.read", err)
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, maxManifestBytes+1))
	if err != nil {
		return Manifest{}, nil, nil, pluginError("PLUGIN_MANIFEST_INVALID", "manifest.read", err)
	}
	if len(data) > maxManifestBytes {
		return Manifest{}, nil, nil, pluginError("PLUGIN_MANIFEST_INVALID", "manifest.read", fmt.Errorf("plugin.json exceeds %d bytes", maxManifestBytes))
	}
	var raw map[string]json.RawMessage
	decoder := json.NewDecoder(bytes.NewReader(data))
	if err := decoder.Decode(&raw); err != nil {
		return Manifest{}, nil, nil, pluginError("PLUGIN_MANIFEST_INVALID", "manifest.decode", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return Manifest{}, nil, nil, pluginError("PLUGIN_MANIFEST_INVALID", "manifest.decode", errors.New("plugin.json contains trailing JSON"))
		}
		return Manifest{}, nil, nil, pluginError("PLUGIN_MANIFEST_INVALID", "manifest.decode", err)
	}

	readString := func(name string) (string, error) {
		value, ok := raw[name]
		if !ok {
			return "", nil
		}
		var text string
		if err := json.Unmarshal(value, &text); err != nil {
			return "", fmt.Errorf("%s must be a string", name)
		}
		return strings.TrimSpace(text), nil
	}
	schema, err := readString("$schema")
	if err != nil || schema != pluginSchemaURI {
		if err == nil {
			err = fmt.Errorf("$schema must equal %s", pluginSchemaURI)
		}
		return Manifest{}, nil, nil, pluginError("PLUGIN_MANIFEST_INVALID", "manifest.schema", err)
	}
	name, err := readString("name")
	if err != nil {
		return Manifest{}, nil, nil, pluginError("PLUGIN_MANIFEST_INVALID", "manifest.name", err)
	}
	if err := ValidateName(name); err != nil {
		return Manifest{}, nil, nil, pluginError("PLUGIN_MANIFEST_INVALID", "manifest.name", err)
	}
	version, err := readString("version")
	if err != nil {
		return Manifest{}, nil, nil, pluginError("PLUGIN_MANIFEST_INVALID", "manifest.version", err)
	}
	warnings := make([]string, 0)
	if version == "" {
		version = VersionLocal
		warnings = append(warnings, "plugin.json omits version; Portable Plugin is treated as version=local")
	}
	if err := ValidateVersion(version); err != nil {
		return Manifest{}, nil, nil, pluginError("PLUGIN_MANIFEST_INVALID", "manifest.version", err)
	}
	description, err := readString("description")
	if err != nil {
		return Manifest{}, nil, nil, pluginError("PLUGIN_MANIFEST_INVALID", "manifest.description", err)
	}

	manifest := Manifest{Schema: schema, Name: name, Version: version, Description: description}
	if manifest.Homepage, err = readString("homepage"); err != nil {
		return Manifest{}, nil, nil, pluginError("PLUGIN_MANIFEST_INVALID", "manifest.homepage", err)
	}
	if manifest.Repository, err = readString("repository"); err != nil {
		return Manifest{}, nil, nil, pluginError("PLUGIN_MANIFEST_INVALID", "manifest.repository", err)
	}
	if manifest.License, err = readString("license"); err != nil {
		return Manifest{}, nil, nil, pluginError("PLUGIN_MANIFEST_INVALID", "manifest.license", err)
	}
	if value, ok := raw["keywords"]; ok {
		if err := json.Unmarshal(value, &manifest.Keywords); err != nil {
			return Manifest{}, nil, nil, pluginError("PLUGIN_MANIFEST_INVALID", "manifest.keywords", errors.New("keywords must be an array of strings"))
		}
	}
	if value, ok := raw["author"]; ok {
		var authorRaw map[string]json.RawMessage
		if err := json.Unmarshal(value, &authorRaw); err != nil {
			return Manifest{}, nil, nil, pluginError("PLUGIN_MANIFEST_INVALID", "manifest.author", errors.New("author must be an object"))
		}
		author := &ManifestAuthor{}
		for key, field := range map[string]*string{"name": &author.Name, "email": &author.Email, "url": &author.URL} {
			value, exists := authorRaw[key]
			if !exists {
				continue
			}
			if err := json.Unmarshal(value, field); err != nil {
				return Manifest{}, nil, nil, pluginError("PLUGIN_MANIFEST_INVALID", "manifest.author", fmt.Errorf("author.%s must be a string", key))
			}
		}
		for key := range authorRaw {
			if key != "name" && key != "email" && key != "url" {
				return Manifest{}, nil, nil, pluginError("PLUGIN_MANIFEST_INVALID", "manifest.author", fmt.Errorf("unknown author field %q", key))
			}
		}
		manifest.Author = author
	}
	if value, ok := raw["provenance"]; ok && !isJSONEmpty(value) {
		var provenance Provenance
		decoder := json.NewDecoder(bytes.NewReader(value))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&provenance); err != nil {
			return Manifest{}, nil, nil, pluginError("PLUGIN_MANIFEST_INVALID", "manifest.provenance", fmt.Errorf("invalid provenance: %w", err))
		}
		var trailing any
		if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
			if err == nil {
				err = errors.New("provenance contains trailing JSON")
			}
			return Manifest{}, nil, nil, pluginError("PLUGIN_MANIFEST_INVALID", "manifest.provenance", err)
		}
		if err := validateProvenance(&provenance); err != nil {
			return Manifest{}, nil, nil, pluginError("PLUGIN_MANIFEST_INVALID", "manifest.provenance", err)
		}
		manifest.Provenance = &provenance
	}
	extensionUnsupported := make([]string, 0)
	if value, ok := raw["extensions"]; ok {
		var extensions map[string]json.RawMessage
		if err := json.Unmarshal(value, &extensions); err != nil {
			return Manifest{}, nil, nil, pluginError("PLUGIN_MANIFEST_INVALID", "manifest.extensions", errors.New("extensions must be an object"))
		}
		for namespace, value := range extensions {
			var object map[string]any
			if err := json.Unmarshal(value, &object); err != nil || object == nil {
				return Manifest{}, nil, nil, pluginError("PLUGIN_MANIFEST_INVALID", "manifest.extensions", fmt.Errorf("extension %q must be an object", namespace))
			}
			if len(object) > 0 {
				extensionUnsupported = append(extensionUnsupported, "extension "+namespace)
			}
		}
		manifest.Extensions = extensions
	}

	known := map[string]struct{}{
		"$schema": {}, "name": {}, "version": {}, "description": {}, "author": {},
		"homepage": {}, "repository": {}, "license": {}, "keywords": {}, "provenance": {}, "extensions": {},
	}
	unsupported := append([]string(nil), extensionUnsupported...)
	for key, value := range raw {
		if _, ok := known[key]; ok {
			continue
		}
		if label, ok := unsupportedManifestFields[key]; ok && !isJSONEmpty(value) {
			unsupported = append(unsupported, label)
			continue
		}
		if (key == "secrets" || key == "credentials") && !isJSONEmpty(value) {
			unsupported = append(unsupported, "manifest "+key)
			continue
		}
		unsupported = append(unsupported, "unknown plugin.json field "+key)
	}
	sort.Strings(unsupported)
	unsupported = uniqueStrings(unsupported)
	sort.Strings(warnings)
	return manifest, unsupported, warnings, nil
}

func validateProvenance(provenance *Provenance) error {
	if provenance == nil {
		return nil
	}
	provenance.Origin = strings.TrimSpace(provenance.Origin)
	provenance.Revision = strings.TrimSpace(provenance.Revision)
	provenance.Subdir = strings.TrimSpace(strings.ReplaceAll(provenance.Subdir, "\\", "/"))
	provenance.Format = strings.TrimSpace(provenance.Format)
	for field, value := range map[string]string{
		"origin": provenance.Origin, "revision": provenance.Revision,
		"subdir": provenance.Subdir, "format": provenance.Format,
	} {
		if containsControlCharacter(value) {
			return fmt.Errorf("provenance.%s must not contain control characters", field)
		}
	}
	if provenance.Origin == "" {
		return errors.New("provenance.origin is required")
	}
	parsedOrigin, err := url.Parse(provenance.Origin)
	if err != nil {
		return fmt.Errorf("provenance.origin is invalid: %w", err)
	}
	if parsedOrigin.Scheme == "http" || parsedOrigin.Scheme == "https" {
		if parsedOrigin.User != nil {
			return errors.New("provenance.origin HTTP(S) URL must not contain userinfo")
		}
		if parsedOrigin.RawQuery != "" || parsedOrigin.Fragment != "" {
			return errors.New("provenance.origin HTTP(S) URL must not contain query parameters or fragments")
		}
	} else if parsedOrigin.User != nil {
		if _, hasPassword := parsedOrigin.User.Password(); hasPassword {
			return errors.New("provenance.origin must not contain a password")
		}
	}
	if provenance.Subdir != "" {
		clean, err := cleanPluginRelativePath(provenance.Subdir)
		if err != nil {
			return fmt.Errorf("provenance.subdir: %w", err)
		}
		provenance.Subdir = clean
	}
	return nil
}

func containsControlCharacter(value string) bool {
	for _, char := range value {
		if char < 0x20 || char == 0x7f {
			return true
		}
	}
	return false
}

func validatePackageTree(root string) ([]string, error) {
	unsupportedRoots := map[string]string{
		"hooks": "hooks", "agents": "agents", "lsp": "lsp", "monitors": "monitors",
	}
	unsupported := make([]string, 0)
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return pluginError("PLUGIN_PACKAGE_INVALID", "package.walk", walkErr)
		}
		if path == root {
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return pluginError("PLUGIN_PACKAGE_INVALID", "package.symlink", fmt.Errorf("symlink is not allowed: %s", path))
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return pluginError("PLUGIN_PACKAGE_INVALID", "package.path", err)
		}
		relative = filepath.ToSlash(relative)
		if skills.IsIgnoredPackageMetadataPath(relative) {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if entry.IsDir() {
			if label, exists := unsupportedRoots[filepath.ToSlash(relative)]; exists {
				unsupported = append(unsupported, label)
				return filepath.SkipDir
			}
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return pluginError("PLUGIN_PACKAGE_INVALID", "package.file_type", err)
		}
		if !info.Mode().IsRegular() {
			return pluginError("PLUGIN_PACKAGE_INVALID", "package.file_type", fmt.Errorf("special files are not allowed: %s", relative))
		}
		if entry.Name() == ".env" {
			return pluginError("PLUGIN_PACKAGE_INVALID", "package.secret_file", errors.New(".env files are not allowed in Plugin packages"))
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(unsupported)
	return uniqueStrings(unsupported), nil
}

func collectExecutableFiles(root string) ([]string, error) {
	items := make([]string, 0)
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if path == root {
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("symlink is not allowed: %s", path)
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		relative = filepath.ToSlash(relative)
		if skills.IsIgnoredPackageMetadataPath(relative) {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if entry.IsDir() {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return nil
		}
		ext := strings.ToLower(filepath.Ext(entry.Name()))
		if info.Mode().Perm()&0o111 != 0 || ext == ".exe" || ext == ".cmd" || ext == ".bat" || ext == ".ps1" {
			items = append(items, relative)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(items)
	return uniqueStrings(items), nil
}

func isJSONEmpty(raw json.RawMessage) bool {
	text := strings.TrimSpace(string(raw))
	switch text {
	case "", "null", "false", "0", "\"\"", "[]", "{}":
		return true
	default:
		return false
	}
}

func uniqueStrings(values []string) []string {
	if len(values) < 2 {
		return values
	}
	out := values[:0]
	last := ""
	for _, value := range values {
		if value == last {
			continue
		}
		out = append(out, value)
		last = value
	}
	return out
}
