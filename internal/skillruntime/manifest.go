package skillruntime

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
)

var (
	skillNamePattern  = regexp.MustCompile(`^[a-z][a-z0-9-]{1,62}$`)
	operationPattern  = regexp.MustCompile(`^[a-z][a-z0-9._-]*$`)
	semverPattern     = regexp.MustCompile(`^v?[0-9]+\.[0-9]+\.[0-9]+(?:[-+][0-9A-Za-z.-]+)?$`)
	envVarNamePattern = regexp.MustCompile(`^[A-Z_][A-Z0-9_]*$`)
)

func LoadManifest(packageDir string) (Manifest, error) {
	path := filepath.Join(packageDir, "agentdock.yaml")
	data, err := os.ReadFile(path)
	if err != nil {
		return Manifest{}, runtimeError(ErrManifestInvalid, "manifest.read", err)
	}
	manifest, err := ParseManifest(data)
	if err != nil {
		return Manifest{}, runtimeError(ErrManifestInvalid, "manifest.parse", err)
	}
	if err := ValidatePackageManifest(packageDir, manifest); err != nil {
		return Manifest{}, err
	}
	return manifest, nil
}

func ParseManifest(data []byte) (Manifest, error) {
	var raw map[string]any
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 {
		return Manifest{}, errors.New("agentdock.yaml is empty")
	}
	if trimmed[0] == '{' {
		if err := json.Unmarshal(trimmed, &raw); err != nil {
			return Manifest{}, fmt.Errorf("decode JSON-compatible YAML: %w", err)
		}
	} else {
		parsed, err := parseYAML(trimmed)
		if err != nil {
			return Manifest{}, err
		}
		raw = parsed
	}
	if hasDeprecatedPermissionsSecrets(raw) {
		return Manifest{}, errors.New("spec.permissions.secrets: deprecated; declare secret variables in spec.permissions.env with kind=secret")
	}
	encoded, err := json.Marshal(raw)
	if err != nil {
		return Manifest{}, err
	}
	dec := json.NewDecoder(bytes.NewReader(encoded))
	dec.DisallowUnknownFields()
	var manifest Manifest
	if err := dec.Decode(&manifest); err != nil {
		return Manifest{}, fmt.Errorf("decode manifest: %w", err)
	}
	if err := ValidateManifest(manifest); err != nil {
		return Manifest{}, err
	}
	return manifest, nil
}

func hasDeprecatedPermissionsSecrets(raw map[string]any) bool {
	spec, ok := raw["spec"].(map[string]any)
	if !ok {
		return false
	}
	permissions, ok := spec["permissions"].(map[string]any)
	if !ok {
		return false
	}
	_, exists := permissions["secrets"]
	return exists
}

func ValidateManifest(m Manifest) error {
	var issues []string
	add := func(path, message string) { issues = append(issues, path+": "+message) }
	if m.APIVersion != ManifestAPIVersion {
		add("apiVersion", "must be "+ManifestAPIVersion)
	}
	if m.Kind != ManifestKind {
		add("kind", "must be Skill")
	}
	if !skillNamePattern.MatchString(m.Metadata.Name) {
		add("metadata.name", "invalid skill name")
	}
	if !semverPattern.MatchString(m.Metadata.Version) {
		add("metadata.version", "must be semantic version")
	}
	if strings.TrimSpace(m.Metadata.DisplayName) == "" {
		add("metadata.displayName", "is required")
	}
	if strings.TrimSpace(m.Metadata.Description) == "" {
		add("metadata.description", "is required")
	}
	runtimeName := strings.ToLower(strings.TrimSpace(m.Spec.Runtime))
	if runtimeName != "" && runtimeName != RuntimeBinary && runtimeName != RuntimePython && runtimeName != RuntimeNode && runtimeName != RuntimePowerShell {
		add("spec.runtime", "must be binary, python, node or powershell")
	}
	if contains(m.Spec.Compatibility.Platforms, "windows") && runtimeName == "" {
		add("spec.runtime", "is required when Windows compatibility is declared")
	}
	if err := validateRelativePackagePath(m.Spec.Entrypoint); err != nil {
		add("spec.entrypoint", err.Error())
	}
	if len(m.Spec.Operations) == 0 {
		add("spec.operations", "at least one operation is required")
	}
	seenOps := map[string]struct{}{}
	for i, op := range m.Spec.Operations {
		base := fmt.Sprintf("spec.operations[%d]", i)
		if !operationPattern.MatchString(op.Name) {
			add(base+".name", "invalid operation name")
		}
		if _, ok := seenOps[op.Name]; ok {
			add(base+".name", "duplicate operation")
		}
		seenOps[op.Name] = struct{}{}
		if strings.TrimSpace(op.Description) == "" {
			add(base+".description", "is required")
		}
		if op.TimeoutSeconds < 1 || op.TimeoutSeconds > 86400 {
			add(base+".timeoutSeconds", "must be between 1 and 86400")
		}
		if err := ValidateJSONSchemaDocument(op.InputSchema); err != nil {
			add(base+".inputSchema", err.Error())
		}
		if err := ValidateJSONSchemaDocument(op.OutputSchema); err != nil {
			add(base+".outputSchema", err.Error())
		}
	}
	if len(m.Spec.Compatibility.Platforms) == 0 {
		add("spec.compatibility.platforms", "at least one platform is required")
	}
	for i, value := range m.Spec.Compatibility.Platforms {
		if value != "darwin" && value != "linux" && value != "windows" {
			add(fmt.Sprintf("spec.compatibility.platforms[%d]", i), "must be darwin, linux or windows")
		}
	}
	if len(m.Spec.Compatibility.Architectures) == 0 {
		add("spec.compatibility.architectures", "at least one architecture is required")
	}
	for i, value := range m.Spec.Compatibility.Architectures {
		if value != "arm64" && value != "amd64" {
			add(fmt.Sprintf("spec.compatibility.architectures[%d]", i), "must be arm64 or amd64")
		}
	}
	seenEnv := map[string]struct{}{}
	for i, env := range m.Spec.Permissions.Env {
		base := fmt.Sprintf("spec.permissions.env[%d]", i)
		if !envVarNamePattern.MatchString(env.Name) {
			add(base+".name", "invalid env name")
		}
		kind := strings.ToLower(strings.TrimSpace(env.Kind))
		if kind != "plain" && kind != "secret" {
			add(base+".kind", "must be plain or secret")
		}
		if _, ok := seenEnv[env.Name]; ok {
			add(base+".name", "duplicate env name")
		}
		seenEnv[env.Name] = struct{}{}
	}
	for i, command := range m.Spec.Permissions.Commands {
		if command == "" || filepath.Base(command) != command {
			add(fmt.Sprintf("spec.permissions.commands[%d]", i), "must be a command basename")
		}
	}
	if len(issues) > 0 {
		sort.Strings(issues)
		return errors.New(strings.Join(issues, "; "))
	}
	return nil
}

func ValidatePackageManifest(packageDir string, m Manifest) error {
	entrypoint, err := safePackageJoin(packageDir, m.Spec.Entrypoint)
	if err != nil {
		return runtimeError(ErrManifestInvalid, "manifest.entrypoint", err)
	}
	info, err := os.Stat(entrypoint)
	if err != nil {
		return runtimeError(ErrManifestInvalid, "manifest.entrypoint", err)
	}
	if info.IsDir() {
		return runtimeError(ErrManifestInvalid, "manifest.entrypoint", errors.New("entrypoint must be a file"))
	}
	if !contains(m.Spec.Compatibility.Platforms, runtime.GOOS) || !contains(m.Spec.Compatibility.Architectures, runtime.GOARCH) {
		return runtimeError(ErrIncompatible, "compatibility", fmt.Errorf("package supports %v/%v, node is %s/%s", m.Spec.Compatibility.Platforms, m.Spec.Compatibility.Architectures, runtime.GOOS, runtime.GOARCH))
	}
	return nil
}

func hasWindowsDrivePrefix(value string) bool {
	if len(value) < 2 || value[1] != ':' {
		return false
	}
	letter := value[0]
	return (letter >= 'A' && letter <= 'Z') || (letter >= 'a' && letter <= 'z')
}

func validateRelativePackagePath(value string) error {
	if strings.TrimSpace(value) == "" {
		return errors.New("is required")
	}
	if strings.HasPrefix(value, "/") || filepath.IsAbs(value) || strings.Contains(value, `\`) || hasWindowsDrivePrefix(value) {
		return errors.New("must be a slash-separated relative path")
	}
	for _, part := range strings.Split(value, "/") {
		if part == "" || part == "." || part == ".." {
			return errors.New("must not contain empty, dot or parent segments")
		}
	}
	return nil
}

func safePackageJoin(root, rel string) (string, error) {
	if err := validateRelativePackagePath(rel); err != nil {
		return "", err
	}
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	joined := filepath.Join(rootAbs, filepath.FromSlash(rel))
	realRoot, err := filepath.EvalSymlinks(rootAbs)
	if err != nil {
		return "", err
	}
	realJoined, err := filepath.EvalSymlinks(joined)
	if err != nil {
		return "", err
	}
	if realJoined != realRoot && !strings.HasPrefix(realJoined, realRoot+string(os.PathSeparator)) {
		return "", errors.New("path escapes package root")
	}
	return realJoined, nil
}

func contains(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}

type yamlLine struct {
	indent int
	text   string
	line   int
}

func parseYAML(data []byte) (map[string]any, error) {
	lines, err := tokenizeYAML(data)
	if err != nil {
		return nil, err
	}
	if len(lines) == 0 {
		return nil, errors.New("agentdock.yaml is empty")
	}
	if strings.HasPrefix(lines[0].text, "-") {
		return nil, errors.New("agentdock.yaml root must be a mapping")
	}
	value, next, err := parseYAMLBlock(lines, 0, lines[0].indent)
	if err != nil {
		return nil, err
	}
	if next != len(lines) {
		return nil, fmt.Errorf("line %d: unexpected content", lines[next].line)
	}
	root, ok := value.(map[string]any)
	if !ok {
		return nil, errors.New("agentdock.yaml root must be a mapping")
	}
	return root, nil
}

func tokenizeYAML(data []byte) ([]yamlLine, error) {
	rawLines := strings.Split(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n")
	result := make([]yamlLine, 0, len(rawLines))
	for i, raw := range rawLines {
		if strings.ContainsRune(raw, '\t') {
			return nil, fmt.Errorf("line %d: tabs are not allowed", i+1)
		}
		trimmedRight := strings.TrimRight(raw, " \r")
		trimmed := strings.TrimSpace(trimmedRight)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") || trimmed == "---" || trimmed == "..." {
			continue
		}
		if strings.Contains(trimmed, "&") || strings.Contains(trimmed, "*") || strings.HasPrefix(trimmed, "!") {
			return nil, fmt.Errorf("line %d: YAML aliases, anchors and tags are not supported", i+1)
		}
		indent := len(trimmedRight) - len(strings.TrimLeft(trimmedRight, " "))
		if indent%2 != 0 {
			return nil, fmt.Errorf("line %d: indentation must use multiples of two spaces", i+1)
		}
		result = append(result, yamlLine{indent: indent, text: stripYAMLComment(strings.TrimLeft(trimmedRight, " ")), line: i + 1})
	}
	return result, nil
}

func stripYAMLComment(value string) string {
	inSingle, inDouble := false, false
	for i, r := range value {
		switch r {
		case '\'':
			if !inDouble {
				inSingle = !inSingle
			}
		case '"':
			if !inSingle && (i == 0 || value[i-1] != '\\') {
				inDouble = !inDouble
			}
		case '#':
			if !inSingle && !inDouble && (i == 0 || value[i-1] == ' ') {
				return strings.TrimSpace(value[:i])
			}
		}
	}
	return strings.TrimSpace(value)
}

func parseYAMLBlock(lines []yamlLine, index, indent int) (any, int, error) {
	if index >= len(lines) {
		return nil, index, errors.New("unexpected end of YAML")
	}
	if lines[index].indent != indent {
		return nil, index, fmt.Errorf("line %d: unexpected indentation", lines[index].line)
	}
	if strings.HasPrefix(lines[index].text, "- ") || lines[index].text == "-" {
		return parseYAMLList(lines, index, indent)
	}
	return parseYAMLMap(lines, index, indent)
}

func parseYAMLMap(lines []yamlLine, index, indent int) (map[string]any, int, error) {
	result := map[string]any{}
	for index < len(lines) && lines[index].indent == indent && !strings.HasPrefix(lines[index].text, "-") {
		line := lines[index]
		key, rest, ok := splitYAMLPair(line.text)
		if !ok || key == "" {
			return nil, index, fmt.Errorf("line %d: expected key: value", line.line)
		}
		if _, exists := result[key]; exists {
			return nil, index, fmt.Errorf("line %d: duplicate key %q", line.line, key)
		}
		index++
		if rest == "" {
			if index >= len(lines) || lines[index].indent <= indent {
				result[key] = map[string]any{}
				continue
			}
			child, next, err := parseYAMLBlock(lines, index, lines[index].indent)
			if err != nil {
				return nil, index, err
			}
			result[key], index = child, next
			continue
		}
		value, err := parseYAMLScalar(rest)
		if err != nil {
			return nil, index, fmt.Errorf("line %d: %w", line.line, err)
		}
		result[key] = value
	}
	return result, index, nil
}

func parseYAMLList(lines []yamlLine, index, indent int) ([]any, int, error) {
	var result []any
	for index < len(lines) && lines[index].indent == indent && (strings.HasPrefix(lines[index].text, "- ") || lines[index].text == "-") {
		line := lines[index]
		rest := strings.TrimSpace(strings.TrimPrefix(line.text, "-"))
		index++
		if rest == "" {
			if index >= len(lines) || lines[index].indent <= indent {
				result = append(result, nil)
				continue
			}
			child, next, err := parseYAMLBlock(lines, index, lines[index].indent)
			if err != nil {
				return nil, index, err
			}
			result, index = append(result, child), next
			continue
		}
		if key, firstRest, ok := splitYAMLPair(rest); ok {
			item := map[string]any{}
			if firstRest == "" {
				if index >= len(lines) || lines[index].indent <= indent {
					item[key] = map[string]any{}
				} else {
					child, next, err := parseYAMLBlock(lines, index, lines[index].indent)
					if err != nil {
						return nil, index, err
					}
					item[key], index = child, next
				}
			} else {
				value, err := parseYAMLScalar(firstRest)
				if err != nil {
					return nil, index, fmt.Errorf("line %d: %w", line.line, err)
				}
				item[key] = value
			}
			if index < len(lines) && lines[index].indent > indent {
				extra, next, err := parseYAMLMap(lines, index, lines[index].indent)
				if err != nil {
					return nil, index, err
				}
				for k, v := range extra {
					if _, exists := item[k]; exists {
						return nil, index, fmt.Errorf("line %d: duplicate key %q", lines[index].line, k)
					}
					item[k] = v
				}
				index = next
			}
			result = append(result, item)
			continue
		}
		value, err := parseYAMLScalar(rest)
		if err != nil {
			return nil, index, fmt.Errorf("line %d: %w", line.line, err)
		}
		result = append(result, value)
	}
	return result, index, nil
}

func splitYAMLPair(value string) (string, string, bool) {
	inSingle, inDouble, depth := false, false, 0
	for i, r := range value {
		switch r {
		case '\'':
			if !inDouble {
				inSingle = !inSingle
			}
		case '"':
			if !inSingle && (i == 0 || value[i-1] != '\\') {
				inDouble = !inDouble
			}
		case '[', '{':
			if !inSingle && !inDouble {
				depth++
			}
		case ']', '}':
			if !inSingle && !inDouble {
				depth--
			}
		case ':':
			if !inSingle && !inDouble && depth == 0 {
				return strings.TrimSpace(value[:i]), strings.TrimSpace(value[i+1:]), true
			}
		}
	}
	return "", "", false
}

func parseYAMLScalar(value string) (any, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", nil
	}
	if strings.HasPrefix(value, "{") || strings.HasPrefix(value, "[") {
		var decoded any
		if err := json.Unmarshal([]byte(value), &decoded); err == nil {
			return decoded, nil
		}
		if strings.HasPrefix(value, "[") && strings.HasSuffix(value, "]") {
			inner := strings.TrimSpace(value[1 : len(value)-1])
			if inner == "" {
				return []any{}, nil
			}
			parts := strings.Split(inner, ",")
			items := make([]any, 0, len(parts))
			for _, part := range parts {
				item, err := parseYAMLScalar(strings.TrimSpace(part))
				if err != nil {
					return nil, err
				}
				items = append(items, item)
			}
			return items, nil
		}
		return nil, errors.New("inline mappings must use JSON syntax")
	}
	if strings.HasPrefix(value, "\"") {
		var decoded string
		if err := json.Unmarshal([]byte(value), &decoded); err != nil {
			return nil, fmt.Errorf("invalid quoted string: %w", err)
		}
		return decoded, nil
	}
	if strings.HasPrefix(value, "'") {
		if len(value) < 2 || !strings.HasSuffix(value, "'") {
			return nil, errors.New("unterminated single-quoted string")
		}
		return strings.ReplaceAll(value[1:len(value)-1], "''", "'"), nil
	}
	switch value {
	case "true":
		return true, nil
	case "false":
		return false, nil
	case "null", "~":
		return nil, nil
	}
	if number, err := strconv.Atoi(value); err == nil {
		return number, nil
	}
	return value, nil
}
