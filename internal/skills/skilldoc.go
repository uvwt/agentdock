package skills

import (
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

var (
	skillNamePattern = regexp.MustCompile(`^[a-z][a-z0-9-]{1,62}$`)
	semverPattern    = regexp.MustCompile(`^v?[0-9]+\.[0-9]+\.[0-9]+(?:[-+][0-9A-Za-z.-]+)?$`)
)

func LoadSkillDocument(packageDir string) (SkillDocument, error) {
	data, err := os.ReadFile(filepath.Join(packageDir, "SKILL.md"))
	if err != nil {
		return SkillDocument{}, packageError(ErrDocumentInvalid, "document.read", err)
	}
	doc, err := ParseSkillDocument(data)
	if err != nil {
		return SkillDocument{}, packageError(ErrDocumentInvalid, "document.parse", err)
	}
	return doc, nil
}

func ParseSkillDocument(data []byte) (SkillDocument, error) {
	text := strings.ReplaceAll(string(data), "\r\n", "\n")
	lines := strings.Split(text, "\n")
	if len(lines) < 3 || strings.TrimSpace(lines[0]) != "---" {
		return SkillDocument{}, errors.New("SKILL.md must start with YAML frontmatter")
	}
	endLine := -1
	for i := 1; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == "---" {
			endLine = i
			break
		}
	}
	if endLine < 0 {
		return SkillDocument{}, errors.New("SKILL.md frontmatter must be closed by ---")
	}
	fields := parseSkillDocumentFrontmatter(strings.Join(lines[1:endLine], "\n"))
	doc := SkillDocument{
		Name:        strings.TrimSpace(fields["name"]),
		Description: strings.TrimSpace(fields["description"]),
		Version:     strings.TrimSpace(fields["version"]),
		Body:        strings.TrimSpace(strings.Join(lines[endLine+1:], "\n")),
	}
	var issues []string
	if !skillNamePattern.MatchString(doc.Name) {
		issues = append(issues, "name is required and must match ^[a-z][a-z0-9-]{1,62}$")
	}
	if doc.Description == "" {
		issues = append(issues, "description is required")
	}
	if !semverPattern.MatchString(doc.Version) {
		issues = append(issues, "version is required and must be semantic version")
	}
	if doc.Body == "" {
		issues = append(issues, "markdown body is required")
	}
	if len(issues) > 0 {
		return SkillDocument{}, errors.New(strings.Join(issues, "; "))
	}
	return doc, nil
}

func parseSkillDocumentFrontmatter(frontmatter string) map[string]string {
	fields := map[string]string{}
	lines := strings.Split(frontmatter, "\n")
	for index := 0; index < len(lines); index++ {
		raw := lines[index]
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") || hasLeadingWhitespace(raw) {
			continue
		}
		key, value, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		if key != "name" && key != "description" && key != "version" {
			continue
		}
		value = strings.TrimSpace(value)
		if value != "|" && value != ">" {
			fields[key] = strings.Trim(value, `"'`)
			continue
		}

		style := value
		parts := make([]string, 0)
		for index+1 < len(lines) && (strings.TrimSpace(lines[index+1]) == "" || hasLeadingWhitespace(lines[index+1])) {
			index++
			parts = append(parts, strings.TrimSpace(lines[index]))
		}
		if style == ">" {
			fields[key] = strings.TrimSpace(strings.Join(parts, " "))
		} else {
			fields[key] = strings.TrimSpace(strings.Join(parts, "\n"))
		}
	}
	return fields
}

func hasLeadingWhitespace(value string) bool {
	return value != "" && (value[0] == ' ' || value[0] == '\t')
}

func ValidatePackage(packageDir string) error {
	if _, err := os.Stat(filepath.Join(packageDir, "agentdock.yaml")); err == nil {
		return packageError(ErrInvalidPackage, "package.legacy_manifest", errors.New("agentdock.yaml is not supported; Skill packages are document-only"))
	} else if !os.IsNotExist(err) {
		return packageError(ErrInvalidPackage, "package.legacy_manifest", err)
	}
	return filepath.WalkDir(packageDir, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return packageError(ErrInvalidPackage, "package.symlink", errors.New("symlinks are not allowed in Skill packages"))
		}
		if !entry.IsDir() && entry.Name() == ".env" {
			return packageError(ErrInvalidPackage, "package.secret_file", errors.New(".env files are not allowed in Skill packages; store credentials under skill-data"))
		}
		return nil
	})
}
