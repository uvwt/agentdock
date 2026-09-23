package plugin

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const maxCatalogBytes = 4 << 20

type catalogDefinition struct {
	Catalog
	entries map[string]catalogEntryDefinition
}

type catalogEntryDefinition struct {
	Public    CatalogEntry
	SourceRaw json.RawMessage
	Hints     adapterHints
}

func (m *Manager) LoadCatalog(ctx context.Context, request SourceRequest) (Catalog, error) {
	staged, definition, err := m.loadCatalogDefinition(ctx, request)
	if err != nil {
		return Catalog{}, err
	}
	defer staged.Cleanup()
	return definition.Catalog, nil
}

func (m *Manager) loadCatalogDefinition(ctx context.Context, request SourceRequest) (stagedPluginSource, catalogDefinition, error) {
	request.Ref = strings.TrimSpace(request.Ref)
	if request.Ref == "" {
		return stagedPluginSource{}, catalogDefinition{}, pluginError("PLUGIN_CATALOG_INVALID", "catalog.source", errors.New("catalog source is required"))
	}
	requestedCatalog := strings.ToLower(strings.TrimSpace(request.Catalog))
	if requestedCatalog == "" {
		requestedCatalog = "auto"
	}
	if requestedCatalog != "auto" && requestedCatalog != "openai" && requestedCatalog != "claude" {
		return stagedPluginSource{}, catalogDefinition{}, pluginError("PLUGIN_CATALOG_INVALID", "catalog.adapter", fmt.Errorf("unsupported catalog adapter %q", requestedCatalog))
	}

	baseRequest := request
	if baseRequest.Type == "" || baseRequest.Type == "auto" || baseRequest.Type == "catalog" {
		baseRequest.Type = inferPluginSourceType(baseRequest.Ref)
	}
	baseRequest.Adapter = "auto"
	baseRequest.Catalog = ""
	baseRequest.CatalogItem = ""
	baseRequest.Version = ""
	base, err := m.stagePluginSource(ctx, baseRequest)
	if err != nil {
		return stagedPluginSource{}, catalogDefinition{}, err
	}

	openAIPath := filepath.Join(base.Root, ".agents", "plugins", "marketplace.json")
	claudePath := filepath.Join(base.Root, ".claude-plugin", "marketplace.json")
	switch requestedCatalog {
	case "openai":
		if !regularFileExists(openAIPath) {
			base.Cleanup()
			return stagedPluginSource{}, catalogDefinition{}, pluginError("PLUGIN_CATALOG_INVALID", "catalog.detect", errors.New("OpenAI marketplace requires .agents/plugins/marketplace.json"))
		}
		definition, err := parseOpenAICatalog(openAIPath, request)
		if err != nil {
			base.Cleanup()
			return stagedPluginSource{}, catalogDefinition{}, err
		}
		return base, definition, nil
	case "claude":
		if !regularFileExists(claudePath) {
			base.Cleanup()
			return stagedPluginSource{}, catalogDefinition{}, pluginError("PLUGIN_CATALOG_INVALID", "catalog.detect", errors.New("Claude marketplace requires .claude-plugin/marketplace.json"))
		}
		definition, err := parseClaudeCatalog(claudePath, request)
		if err != nil {
			base.Cleanup()
			return stagedPluginSource{}, catalogDefinition{}, err
		}
		return base, definition, nil
	default:
		if regularFileExists(openAIPath) && regularFileExists(claudePath) {
			base.Cleanup()
			return stagedPluginSource{}, catalogDefinition{}, pluginError("PLUGIN_CATALOG_AMBIGUOUS", "catalog.detect", errors.New("source contains both OpenAI and Claude marketplace manifests; choose catalog adapter explicitly"))
		}
		if regularFileExists(openAIPath) {
			definition, err := parseOpenAICatalog(openAIPath, request)
			if err != nil {
				base.Cleanup()
				return stagedPluginSource{}, catalogDefinition{}, err
			}
			return base, definition, nil
		}
		if regularFileExists(claudePath) {
			definition, err := parseClaudeCatalog(claudePath, request)
			if err != nil {
				base.Cleanup()
				return stagedPluginSource{}, catalogDefinition{}, err
			}
			return base, definition, nil
		}
		base.Cleanup()
		return stagedPluginSource{}, catalogDefinition{}, pluginError("PLUGIN_CATALOG_INVALID", "catalog.detect", errors.New("no supported OpenAI or Claude marketplace manifest was found"))
	}
}

func (m *Manager) stageCatalogEntry(ctx context.Context, request SourceRequest) (stagedPluginSource, error) {
	if strings.TrimSpace(request.CatalogItem) == "" {
		return stagedPluginSource{}, pluginError("PLUGIN_CATALOG_INVALID", "catalog.item", errors.New("catalog_item is required"))
	}
	base, definition, err := m.loadCatalogDefinition(ctx, request)
	if err != nil {
		return stagedPluginSource{}, err
	}
	entry, ok := definition.entries[request.CatalogItem]
	if !ok {
		base.Cleanup()
		return stagedPluginSource{}, pluginError("PLUGIN_CATALOG_ITEM_NOT_FOUND", "catalog.item", fmt.Errorf("catalog entry %q was not found", request.CatalogItem))
	}
	if len(entry.Public.Unsupported) > 0 {
		// Keep staging possible for validate so the compatibility report can
		// show all issues. Unsupported catalog source kinds themselves cannot be staged.
		for _, item := range entry.Public.Unsupported {
			if strings.HasPrefix(item, "catalog source ") {
				base.Cleanup()
				return stagedPluginSource{}, pluginError("PLUGIN_CATALOG_SOURCE_UNSUPPORTED", "catalog.source", errors.New(item))
			}
		}
	}

	resolvedRequest, localRelative, err := catalogPluginSourceRequest(entry, request)
	if err != nil {
		base.Cleanup()
		return stagedPluginSource{}, err
	}
	var pluginSource stagedPluginSource
	if localRelative != "" {
		relative, err := normalizeExternalComponentPath(localRelative)
		if err != nil {
			base.Cleanup()
			return stagedPluginSource{}, pluginError("PLUGIN_CATALOG_INVALID", "catalog.local_source", err)
		}
		root, err := selectPluginSourceRoot(base.Root, relative)
		if err != nil {
			base.Cleanup()
			return stagedPluginSource{}, pluginError("PLUGIN_CATALOG_INVALID", "catalog.local_source", err)
		}
		pluginSource = stagedPluginSource{
			Root: root,
			Source: Source{
				Type: "catalog", Ref: request.Ref, Revision: base.Source.Revision,
				Selector: base.Source.Selector, Subdir: request.Subdir,
				ResolvedType: "local", ResolvedRef: relative, ResolvedRevision: base.Source.Revision,
			},
			Cleanup: base.Cleanup,
		}
	} else {
		pluginSource, err = m.stagePluginSource(ctx, resolvedRequest)
		if err != nil {
			base.Cleanup()
			return stagedPluginSource{}, err
		}
		underlying := pluginSource.Source
		underlyingCleanup := pluginSource.Cleanup
		pluginSource.Cleanup = func() {
			underlyingCleanup()
			base.Cleanup()
		}
		pluginSource.Source = Source{
			Type: "catalog", Ref: request.Ref, Revision: base.Source.Revision,
			Selector: base.Source.Selector, Subdir: request.Subdir,
			ResolvedType: underlying.Type, ResolvedRef: underlying.Ref,
			ResolvedRevision: underlying.Revision, ResolvedSubdir: underlying.Subdir,
		}
	}
	pluginSource.Source.Catalog = definition.Name
	pluginSource.Source.CatalogItem = entry.Public.Name
	pluginSource.Source.Adapter = entry.Public.Adapter
	pluginSource.Hints = entry.Hints
	pluginSource.Hints.Unsupported = append(pluginSource.Hints.Unsupported, entry.Public.Unsupported...)
	pluginSource.Hints.Warnings = append(pluginSource.Hints.Warnings, entry.Public.Warnings...)
	return pluginSource, nil
}

func parseOpenAICatalog(path string, request SourceRequest) (catalogDefinition, error) {
	raw, err := readCatalogObject(path)
	if err != nil {
		return catalogDefinition{}, err
	}
	name, err := rawRequiredString(raw, "name")
	if err != nil {
		return catalogDefinition{}, err
	}
	entriesRaw, ok := raw["plugins"]
	if !ok {
		return catalogDefinition{}, pluginError("PLUGIN_CATALOG_INVALID", "catalog.plugins", errors.New("OpenAI marketplace plugins array is required"))
	}
	var entries []map[string]json.RawMessage
	if err := json.Unmarshal(entriesRaw, &entries); err != nil {
		return catalogDefinition{}, pluginError("PLUGIN_CATALOG_INVALID", "catalog.plugins", errors.New("OpenAI marketplace plugins must be an array of objects"))
	}
	definition := catalogDefinition{
		Catalog: Catalog{Name: name, Adapter: "openai", Source: request.Ref, Entries: []CatalogEntry{}},
		entries: make(map[string]catalogEntryDefinition),
	}
	for index, rawEntry := range entries {
		entryName, err := rawRequiredString(rawEntry, "name")
		if err != nil {
			return catalogDefinition{}, pluginError("PLUGIN_CATALOG_INVALID", fmt.Sprintf("catalog.plugins[%d].name", index), err)
		}
		if err := ValidateName(entryName); err != nil {
			return catalogDefinition{}, pluginError("PLUGIN_CATALOG_INVALID", fmt.Sprintf("catalog.plugins[%d].name", index), err)
		}
		if _, exists := definition.entries[entryName]; exists {
			return catalogDefinition{}, pluginError("PLUGIN_CATALOG_INVALID", fmt.Sprintf("catalog.plugins[%d].name", index), fmt.Errorf("duplicate catalog entry %q", entryName))
		}
		sourceRaw, ok := rawEntry["source"]
		if !ok || isJSONEmpty(sourceRaw) {
			return catalogDefinition{}, pluginError("PLUGIN_CATALOG_INVALID", fmt.Sprintf("catalog.plugins[%d].source", index), errors.New("source is required"))
		}
		public := CatalogEntry{
			Name: entryName, Adapter: "openai", Catalog: name,
			Source: SourceRequest{Type: "catalog", Ref: request.Ref, Adapter: "openai", Catalog: "openai", CatalogItem: entryName, GitRef: request.GitRef, GitCommit: request.GitCommit, Subdir: request.Subdir},
		}
		if value, ok := rawEntry["category"]; ok {
			_ = json.Unmarshal(value, &public.Category)
		}
		if value, ok := rawEntry["version"]; ok {
			_ = json.Unmarshal(value, &public.Version)
		}
		if value, ok := rawEntry["description"]; ok {
			_ = json.Unmarshal(value, &public.Description)
		}
		unsupported := collectCatalogUnsupported(rawEntry, map[string]bool{
			"name": true, "source": true, "policy": true, "category": true, "interface": true,
			"version": true, "description": true, "products": true,
		}, "OpenAI catalog field ")
		sourceUnsupported := catalogSourceUnsupported(sourceRaw, "openai")
		public.Unsupported = uniqueSortedStrings(append(unsupported, sourceUnsupported...))
		def := catalogEntryDefinition{
			Public: public, SourceRaw: sourceRaw,
			Hints: adapterHints{Name: entryName, Version: public.Version, Description: public.Description},
		}
		if len(sourceUnsupported) == 0 {
			resolvedSource, err := catalogResolvedSourceDescriptor(def, request)
			if err != nil {
				return catalogDefinition{}, pluginError("PLUGIN_CATALOG_INVALID", fmt.Sprintf("catalog.plugins[%d].source", index), err)
			}
			public.ResolvedSource = resolvedSource
			def.Public = public
		}
		definition.Entries = append(definition.Entries, public)
		definition.entries[entryName] = def
	}
	sort.Slice(definition.Entries, func(i, j int) bool { return definition.Entries[i].Name < definition.Entries[j].Name })
	return definition, nil
}

func parseClaudeCatalog(path string, request SourceRequest) (catalogDefinition, error) {
	raw, err := readCatalogObject(path)
	if err != nil {
		return catalogDefinition{}, err
	}
	name, err := rawRequiredString(raw, "name")
	if err != nil {
		return catalogDefinition{}, err
	}
	entriesRaw, ok := raw["plugins"]
	if !ok {
		return catalogDefinition{}, pluginError("PLUGIN_CATALOG_INVALID", "catalog.plugins", errors.New("Claude marketplace plugins array is required"))
	}
	var entries []map[string]json.RawMessage
	if err := json.Unmarshal(entriesRaw, &entries); err != nil {
		return catalogDefinition{}, pluginError("PLUGIN_CATALOG_INVALID", "catalog.plugins", errors.New("Claude marketplace plugins must be an array of objects"))
	}
	definition := catalogDefinition{
		Catalog: Catalog{Name: name, Adapter: "claude", Source: request.Ref, Entries: []CatalogEntry{}},
		entries: make(map[string]catalogEntryDefinition),
	}
	for index, rawEntry := range entries {
		entryName, err := rawRequiredString(rawEntry, "name")
		if err != nil {
			return catalogDefinition{}, pluginError("PLUGIN_CATALOG_INVALID", fmt.Sprintf("catalog.plugins[%d].name", index), err)
		}
		if err := ValidateName(entryName); err != nil {
			return catalogDefinition{}, pluginError("PLUGIN_CATALOG_INVALID", fmt.Sprintf("catalog.plugins[%d].name", index), err)
		}
		if _, exists := definition.entries[entryName]; exists {
			return catalogDefinition{}, pluginError("PLUGIN_CATALOG_INVALID", fmt.Sprintf("catalog.plugins[%d].name", index), fmt.Errorf("duplicate catalog entry %q", entryName))
		}
		sourceRaw, ok := rawEntry["source"]
		if !ok || isJSONEmpty(sourceRaw) {
			return catalogDefinition{}, pluginError("PLUGIN_CATALOG_INVALID", fmt.Sprintf("catalog.plugins[%d].source", index), errors.New("source is required"))
		}
		strict := true
		if value, ok := rawEntry["strict"]; ok {
			if err := json.Unmarshal(value, &strict); err != nil {
				return catalogDefinition{}, pluginError("PLUGIN_CATALOG_INVALID", fmt.Sprintf("catalog.plugins[%d].strict", index), errors.New("strict must be boolean"))
			}
		}
		public := CatalogEntry{
			Name: entryName, Adapter: "claude", Catalog: name, Strict: &strict,
			Source: SourceRequest{Type: "catalog", Ref: request.Ref, Adapter: "claude", Catalog: "claude", CatalogItem: entryName, GitRef: request.GitRef, GitCommit: request.GitCommit, Subdir: request.Subdir},
		}
		for key, target := range map[string]*string{
			"description": &public.Description, "version": &public.Version, "category": &public.Category,
		} {
			if value, ok := rawEntry[key]; ok {
				_ = json.Unmarshal(value, target)
			}
		}
		skills := []string{}
		if value, ok := rawEntry["skills"]; ok && !isJSONEmpty(value) {
			skills, err = decodeStringOrArray(value, "skills")
			if err != nil {
				return catalogDefinition{}, pluginError("PLUGIN_CATALOG_INVALID", fmt.Sprintf("catalog.plugins[%d].skills", index), err)
			}
			public.Skills = append([]string(nil), skills...)
		}
		unsupported := collectCatalogUnsupported(rawEntry, map[string]bool{
			"name": true, "source": true, "displayName": true, "description": true, "version": true,
			"author": true, "homepage": true, "repository": true, "license": true, "keywords": true,
			"metadata": true, "category": true, "tags": true, "strict": true, "relevance": true,
			"defaultEnabled": true, "skills": true, "mcpServers": true,
		}, "Claude catalog component ")
		for _, behavior := range []string{"commands", "agents", "hooks", "workflows", "outputStyles", "output-styles", "themes", "monitors", "lspServers"} {
			if value, ok := rawEntry[behavior]; ok && !isJSONEmpty(value) {
				unsupported = append(unsupported, "Claude catalog component "+behavior)
			}
		}
		sourceUnsupported := catalogSourceUnsupported(sourceRaw, "claude")
		public.Unsupported = uniqueSortedStrings(append(unsupported, sourceUnsupported...))
		hints := adapterHints{
			Name: entryName, Version: public.Version, Description: public.Description,
			Strict: &strict, Skills: skills,
		}
		if value, ok := rawEntry["mcpServers"]; ok {
			hints.MCPServers = append(json.RawMessage(nil), value...)
		}
		if !strict {
			hints.Warnings = append(hints.Warnings, "Claude marketplace strict:false entry defines the Plugin component set")
		}
		def := catalogEntryDefinition{Public: public, SourceRaw: sourceRaw, Hints: hints}
		if len(sourceUnsupported) == 0 {
			resolvedSource, err := catalogResolvedSourceDescriptor(def, request)
			if err != nil {
				return catalogDefinition{}, pluginError("PLUGIN_CATALOG_INVALID", fmt.Sprintf("catalog.plugins[%d].source", index), err)
			}
			public.ResolvedSource = resolvedSource
			def.Public = public
		}
		definition.Entries = append(definition.Entries, public)
		definition.entries[entryName] = def
	}
	sort.Slice(definition.Entries, func(i, j int) bool { return definition.Entries[i].Name < definition.Entries[j].Name })
	return definition, nil
}

func catalogPluginSourceRequest(entry catalogEntryDefinition, catalogRequest SourceRequest) (SourceRequest, string, error) {
	raw := entry.SourceRaw
	var relative string
	if err := json.Unmarshal(raw, &relative); err == nil {
		if !strings.HasPrefix(strings.TrimSpace(relative), "./") {
			return SourceRequest{}, "", pluginError("PLUGIN_CATALOG_INVALID", "catalog.entry.source", errors.New("relative catalog source must start with ./"))
		}
		return SourceRequest{}, relative, nil
	}
	var object map[string]json.RawMessage
	if err := json.Unmarshal(raw, &object); err != nil {
		return SourceRequest{}, "", pluginError("PLUGIN_CATALOG_INVALID", "catalog.entry.source", errors.New("catalog source must be a relative path or source object"))
	}
	kind, err := rawRequiredString(object, "source")
	if err != nil {
		return SourceRequest{}, "", err
	}
	kind = strings.ToLower(strings.TrimSpace(kind))
	request := SourceRequest{Adapter: entry.Public.Adapter, Version: entry.Public.Version}
	switch kind {
	case "local":
		path, err := rawRequiredString(object, "path")
		if err != nil {
			return SourceRequest{}, "", err
		}
		if !strings.HasPrefix(strings.TrimSpace(path), "./") {
			return SourceRequest{}, "", errors.New("local catalog source path must start with ./")
		}
		return SourceRequest{}, path, nil
	case "github":
		repo, err := rawRequiredString(object, "repo")
		if err != nil {
			return SourceRequest{}, "", err
		}
		if !validGitHubCatalogRepo(repo) {
			return SourceRequest{}, "", errors.New("GitHub catalog repo must use a safe owner/repo identifier")
		}
		request.Type = "git"
		request.Ref = "https://github.com/" + repo + ".git"
		request.GitRef = rawOptionalString(object, "ref")
		request.GitCommit = rawOptionalString(object, "sha")
	case "url":
		request.Type = "git"
		request.Ref, err = rawRequiredString(object, "url")
		if err != nil {
			return SourceRequest{}, "", err
		}
		request.GitRef = rawOptionalString(object, "ref")
		request.GitCommit = rawOptionalString(object, "sha")
	case "git-subdir":
		request.Type = "git"
		request.Ref, err = rawRequiredString(object, "url")
		if err != nil {
			return SourceRequest{}, "", err
		}
		request.Subdir, err = rawRequiredString(object, "path")
		if err != nil {
			return SourceRequest{}, "", err
		}
		request.GitRef = rawOptionalString(object, "ref")
		request.GitCommit = rawOptionalString(object, "sha")
	case "archive":
		request.Type = "archive"
		request.Ref, err = rawRequiredString(object, "url")
		if err != nil {
			return SourceRequest{}, "", err
		}
		request.SHA256 = rawOptionalString(object, "sha256")
	default:
		return SourceRequest{}, "", pluginError("PLUGIN_CATALOG_SOURCE_UNSUPPORTED", "catalog.entry.source", fmt.Errorf("catalog source %s is not supported by AgentDock P3", kind))
	}
	return request, "", nil
}

func catalogResolvedSourceDescriptor(entry catalogEntryDefinition, catalogRequest SourceRequest) (*SourceRequest, error) {
	resolved, localRelative, err := catalogPluginSourceRequest(entry, catalogRequest)
	if err != nil {
		return nil, err
	}
	if localRelative != "" {
		return &SourceRequest{
			Type: "local", Ref: localRelative, Adapter: entry.Public.Adapter, Version: entry.Public.Version,
		}, nil
	}
	return &resolved, nil
}

func validGitHubCatalogRepo(repo string) bool {
	parts := strings.Split(strings.TrimSpace(repo), "/")
	if len(parts) != 2 {
		return false
	}
	for _, part := range parts {
		if part == "" || part == "." || part == ".." || len(part) > 100 {
			return false
		}
		for _, r := range part {
			if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') ||
				r == '.' || r == '_' || r == '-' {
				continue
			}
			return false
		}
	}
	return true
}

func catalogSourceUnsupported(raw json.RawMessage, adapter string) []string {
	var relative string
	if json.Unmarshal(raw, &relative) == nil {
		return nil
	}
	var object map[string]json.RawMessage
	if json.Unmarshal(raw, &object) != nil {
		return []string{"catalog source invalid"}
	}
	kind := strings.ToLower(rawOptionalString(object, "source"))
	switch kind {
	case "local", "github", "url", "git-subdir", "archive":
		return nil
	case "":
		return []string{"catalog source invalid"}
	default:
		return []string{"catalog source " + kind}
	}
}

func collectCatalogUnsupported(raw map[string]json.RawMessage, allowed map[string]bool, prefix string) []string {
	items := make([]string, 0)
	for key, value := range raw {
		if allowed[key] || isJSONEmpty(value) {
			continue
		}
		items = append(items, prefix+key)
	}
	return uniqueSortedStrings(items)
}

func readCatalogObject(path string) (map[string]json.RawMessage, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, pluginError("PLUGIN_CATALOG_INVALID", "catalog.read", err)
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, maxCatalogBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxCatalogBytes {
		return nil, pluginError("PLUGIN_CATALOG_INVALID", "catalog.read", fmt.Errorf("catalog exceeds %d bytes", maxCatalogBytes))
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	var raw map[string]json.RawMessage
	if err := decoder.Decode(&raw); err != nil {
		return nil, pluginError("PLUGIN_CATALOG_INVALID", "catalog.decode", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return nil, pluginError("PLUGIN_CATALOG_INVALID", "catalog.decode", errors.New("catalog contains trailing JSON"))
	}
	return raw, nil
}

func rawRequiredString(raw map[string]json.RawMessage, key string) (string, error) {
	value, ok := raw[key]
	if !ok {
		return "", fmt.Errorf("%s is required", key)
	}
	var text string
	if err := json.Unmarshal(value, &text); err != nil || strings.TrimSpace(text) == "" {
		return "", fmt.Errorf("%s must be a non-empty string", key)
	}
	return strings.TrimSpace(text), nil
}

func rawOptionalString(raw map[string]json.RawMessage, key string) string {
	value, ok := raw[key]
	if !ok {
		return ""
	}
	var text string
	if json.Unmarshal(value, &text) != nil {
		return ""
	}
	return strings.TrimSpace(text)
}
