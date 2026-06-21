package skillruntime

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type SkillDocument struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Body        string `json:"body,omitempty"`
}

func LoadSkillDocument(packageDir string) (SkillDocument, error) {
	data, err := os.ReadFile(filepath.Join(packageDir, "SKILL.md"))
	if err != nil {
		return SkillDocument{}, runtimeError(ErrManifestInvalid, "skilldoc", err)
	}
	doc, err := ParseSkillDocument(data)
	if err != nil {
		return SkillDocument{}, runtimeError(ErrManifestInvalid, "skilldoc", err)
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
	frontmatter := strings.Join(lines[1:endLine], "\n")
	body := strings.TrimSpace(strings.Join(lines[endLine+1:], "\n"))
	fields := parseSkillDocumentFrontmatter(frontmatter)
	doc := SkillDocument{
		Name:        strings.TrimSpace(fields["name"]),
		Description: strings.TrimSpace(fields["description"]),
		Body:        body,
	}
	var issues []string
	if doc.Name == "" {
		issues = append(issues, "name is required")
	} else if !skillNamePattern.MatchString(doc.Name) {
		issues = append(issues, "name is invalid")
	}
	if doc.Description == "" {
		issues = append(issues, "description is required")
	}
	if doc.Body == "" {
		issues = append(issues, "markdown body is required")
	}
	if len(issues) > 0 {
		return SkillDocument{}, errors.New(strings.Join(issues, "; "))
	}
	return doc, nil
}

func ValidateSkillDocument(packageDir string, manifest Manifest) error {
	doc, err := LoadSkillDocument(packageDir)
	if err != nil {
		return err
	}
	if doc.Name != manifest.Metadata.Name {
		return runtimeError(ErrManifestInvalid, "skilldoc", fmt.Errorf("SKILL.md name %q must match agentdock.yaml metadata.name %q", doc.Name, manifest.Metadata.Name))
	}
	return nil
}

func parseSkillDocumentFrontmatter(frontmatter string) map[string]string {
	fields := map[string]string{}
	for _, line := range strings.Split(frontmatter, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		value = strings.Trim(value, `"'`)
		if key == "name" || key == "description" {
			fields[key] = value
		}
	}
	return fields
}
