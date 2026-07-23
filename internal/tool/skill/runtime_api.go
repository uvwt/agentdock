package skill

import (
	"strings"

	"github.com/uvwt/agentdock/internal/skills"
)

type CapabilityItem struct {
	Name        string
	Description string
	File        string
	Bundled     bool
}

func (s *Service) CapabilityItems() ([]CapabilityItem, error) {
	names, err := s.state.ListSkills()
	if err != nil {
		return nil, err
	}
	bundledNames, err := s.state.BundledSkills()
	if err != nil {
		return nil, err
	}
	bundled := make(map[string]struct{}, len(bundledNames))
	for _, name := range bundledNames {
		bundled[name] = struct{}{}
	}
	items := make([]CapabilityItem, 0, len(names))
	for _, name := range names {
		packageDir, resolveErr := s.state.Resolve(name, "")
		if resolveErr != nil || skills.ValidatePackage(packageDir) != nil {
			continue
		}
		doc, loadErr := skills.LoadSkillDocument(packageDir)
		if loadErr != nil {
			continue
		}
		_, isBundled := bundled[name]
		items = append(items, CapabilityItem{Name: name, Description: strings.TrimSpace(doc.Description), File: "skill://" + name + "/SKILL.md", Bundled: isBundled})
	}
	return items, nil
}

func (s *Service) RuntimeSkills() (Result, error) {
	result, err := s.List()
	if err != nil {
		return nil, err
	}
	items, _ := result["skills"].([]map[string]any)
	for _, item := range items {
		skill, _ := item["skill"].(string)
		version, _ := item["active_version"].(string)
		if strings.TrimSpace(skill) == "" || strings.TrimSpace(version) == "" {
			continue
		}
		packageDir, err := s.state.InstalledPath(skill, version)
		if err != nil {
			return nil, skillToolError(err)
		}
		document, err := skills.LoadSkillDocument(packageDir)
		if err != nil {
			return nil, skillToolError(err)
		}
		files, err := collectRuntimeSkillFiles(packageDir)
		if err != nil {
			return nil, err
		}
		item["name"] = document.Name
		item["description"] = document.Description
		item["file_count"] = len(files)
	}
	result["source"] = runtimeAPISource
	return result, nil
}

func (s *Service) RuntimeSkill(skill string) (Result, error) {
	result, err := s.Inspect(map[string]any{"skill": skill})
	if err != nil {
		return nil, err
	}
	result["source"] = runtimeAPISource
	result["files"] = []runtimeSkillFile{}
	result["file_count"] = 0
	version, _ := result["version"].(string)
	if strings.TrimSpace(version) == "" {
		return result, nil
	}
	packageDir, err := s.state.InstalledPath(skill, version)
	if err != nil {
		return nil, skillToolError(err)
	}
	files, err := collectRuntimeSkillFiles(packageDir)
	if err != nil {
		return nil, err
	}
	result["files"] = files
	result["file_count"] = len(files)
	return result, nil
}
