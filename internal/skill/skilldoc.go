package skill

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/uvwt/agentdock/internal/skillspec"

	"gopkg.in/yaml.v3"
)

type skillFrontmatter struct {
	Name          string         `yaml:"name"`
	Description   string         `yaml:"description"`
	License       string         `yaml:"license"`
	Compatibility string         `yaml:"compatibility"`
	Metadata      map[string]any `yaml:"metadata"`
	AllowedTools  any            `yaml:"allowed-tools"`
}

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

func ParseSkillMetadata(data []byte) (SkillMetadata, error) {
	doc, err := ParseSkillDocument(data)
	if err != nil {
		return SkillMetadata{}, err
	}
	return SkillMetadata{Name: doc.Name, Description: doc.Description}, nil
}

func ParseSkillDocument(data []byte) (SkillDocument, error) {
	frontmatter, body, err := splitSkillDocument(data)
	if err != nil {
		return SkillDocument{}, err
	}

	var fields skillFrontmatter
	decoder := yaml.NewDecoder(bytes.NewReader(frontmatter))
	if err := decoder.Decode(&fields); err != nil {
		return SkillDocument{}, fmt.Errorf("decode SKILL.md frontmatter: %w", err)
	}

	metadata, metadataErr := skillspec.NormalizeMetadata(fields.Metadata)
	allowedTools, toolsErr := skillspec.NormalizeAllowedTools(fields.AllowedTools)
	doc := SkillDocument{
		Name:          strings.TrimSpace(fields.Name),
		Description:   strings.TrimSpace(fields.Description),
		License:       strings.TrimSpace(fields.License),
		Compatibility: strings.TrimSpace(fields.Compatibility),
		Metadata:      metadata,
		AllowedTools:  allowedTools,
		Body:          body,
	}
	var issues []string
	if err := skillspec.ValidateName(doc.Name); err != nil {
		issues = append(issues, err.Error())
	}
	if err := skillspec.ValidateDescription(doc.Description); err != nil {
		issues = append(issues, err.Error())
	}
	if err := skillspec.ValidateCompatibility(doc.Compatibility); err != nil {
		issues = append(issues, err.Error())
	}
	if metadataErr != nil {
		issues = append(issues, metadataErr.Error())
	}
	if toolsErr != nil {
		issues = append(issues, toolsErr.Error())
	}
	if doc.Body == "" {
		issues = append(issues, "markdown body is required")
	}
	if len(issues) > 0 {
		return SkillDocument{}, errors.New(strings.Join(issues, "; "))
	}
	return doc, nil
}

func splitSkillDocument(data []byte) ([]byte, string, error) {
	text := strings.ReplaceAll(string(data), "\r\n", "\n")
	lines := strings.Split(text, "\n")
	if len(lines) < 3 || strings.TrimSpace(lines[0]) != "---" {
		return nil, "", errors.New("SKILL.md must start with YAML frontmatter")
	}
	endLine := -1
	for i := 1; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == "---" {
			endLine = i
			break
		}
	}
	if endLine < 0 {
		return nil, "", errors.New("SKILL.md frontmatter must be closed by ---")
	}
	frontmatter := []byte(strings.Join(lines[1:endLine], "\n"))
	body := strings.TrimSpace(strings.Join(lines[endLine+1:], "\n"))
	return frontmatter, body, nil
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
		if !entry.IsDir() {
			info, err := entry.Info()
			if err != nil {
				return packageError(ErrInvalidPackage, "package.file_type", err)
			}
			if !info.Mode().IsRegular() {
				return packageError(ErrInvalidPackage, "package.file_type", errors.New("special files are not allowed in Skill packages"))
			}
		}
		if !entry.IsDir() && entry.Name() == ".env" {
			return packageError(ErrInvalidPackage, "package.secret_file", errors.New(".env files are not allowed in Skill packages; store credentials in the managed Skill environment"))
		}
		if !entry.IsDir() && entry.Name() == ".agentdock-install.json" {
			return packageError(ErrInvalidPackage, "package.reserved_file", errors.New(".agentdock-install.json is reserved legacy AgentDock metadata and is not valid Skill package content"))
		}
		return nil
	})
}
