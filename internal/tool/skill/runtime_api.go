package skill

import (
	"strings"

	skills "github.com/uvwt/agentdock/internal/skill"
)

type CapabilityItem struct {
	Name        string
	Description string
	File        string
	Bundled     bool
	Enabled     bool
}

func (s *Service) CapabilityItems() ([]CapabilityItem, error) {
	names, err := s.state.ListSkills()
	if err != nil {
		return nil, err
	}
	items := make([]CapabilityItem, 0, len(names))
	for _, name := range names {
		item, found, itemErr := s.CapabilityItem(name)
		if itemErr != nil {
			return nil, itemErr
		}
		if !found || !item.Enabled {
			continue
		}
		if err := s.ensureAvailable(name); err != nil {
			continue
		}
		items = append(items, item)
	}
	return items, nil
}

// CapabilityItem returns one installed document Skill without applying the
// plugin-level overlay. plugin_load calls it only after validating the plugin.
func (s *Service) CapabilityItem(name string) (CapabilityItem, bool, error) {
	name = strings.TrimSpace(name)
	packageDir, resolveErr := s.state.Resolve(name, "")
	if resolveErr != nil || skills.ValidatePackage(packageDir) != nil {
		return CapabilityItem{}, false, nil
	}
	doc, loadErr := skills.LoadSkillDocument(packageDir)
	if loadErr != nil {
		return CapabilityItem{}, false, nil
	}
	bundled, err := s.state.IsBundled(name)
	if err != nil {
		return CapabilityItem{}, false, err
	}
	enabled, err := s.baseEnabled(name)
	if err != nil {
		return CapabilityItem{}, false, err
	}
	return CapabilityItem{
		Name: name, Description: strings.TrimSpace(doc.Description), File: "skill://" + name + "/SKILL.md",
		Bundled: bundled, Enabled: enabled,
	}, true, nil
}

func (s *Service) RuntimeSkills() (Result, error) {
	result, err := s.list()
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
		selection, selectionErr := s.state.Snapshot(skill)
		if selectionErr != nil {
			return nil, skillToolError(selectionErr)
		}
		item["enabled"] = !selection.Disabled
	}
	result["source"] = runtimeAPISource
	return result, nil
}

func (s *Service) RuntimeSkill(skill string) (Result, error) {
	result, err := s.inspect(InspectRequest{Skill: skill})
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
