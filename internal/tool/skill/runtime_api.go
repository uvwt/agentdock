package skill

import (
	"context"
	"path/filepath"
	"sort"
	"strings"

	skills "github.com/uvwt/agentdock/internal/skill"
)

type CapabilityItem struct {
	Name          string
	Description   string
	File          string
	SkillRef      string
	SourceType    string
	SourceID      string
	PluginName    string
	ContentDigest string
}

func (s *Service) CapabilityItems() ([]CapabilityItem, error) {
	names, err := s.state.ListSkills()
	if err != nil {
		return nil, err
	}
	items := make([]CapabilityItem, 0, len(names))
	for _, name := range names {
		resolved, release, err := s.Acquire(context.Background(), ManagedSkillRef(name))
		if err != nil {
			continue
		}
		if err := skills.ValidatePackage(resolved.Root); err != nil {
			release()
			continue
		}
		doc, docErr := skills.LoadSkillDocument(resolved.Root)
		digest, digestErr := managedContentDigest(resolved.Root)
		release()
		if docErr != nil || digestErr != nil || doc.Name != name {
			continue
		}
		items = append(items, CapabilityItem{
			Name: name, Description: strings.TrimSpace(doc.Description),
			File: resolved.SkillRef + "/SKILL.md", SkillRef: resolved.SkillRef,
			SourceType: resolved.SourceType, SourceID: resolved.SourceID,
			ContentDigest: digest,
		})
	}
	if s.plugins != nil {
		installed, err := s.plugins.List()
		if err != nil {
			return nil, err
		}
		for _, plugin := range installed {
			if !plugin.Enabled {
				continue
			}
			for _, component := range plugin.Components.Skills {
				resolved, release, err := s.Acquire(context.Background(), PluginSkillRef(plugin.Name, component.Name))
				if err != nil {
					continue
				}
				doc, docErr := skills.LoadSkillDocument(resolved.Root)
				release()
				if docErr != nil || doc.Name != component.Name {
					continue
				}
				items = append(items, CapabilityItem{
					Name: component.Name, Description: strings.TrimSpace(doc.Description),
					File: resolved.SkillRef + "/SKILL.md", SkillRef: resolved.SkillRef,
					SourceType: resolved.SourceType, SourceID: resolved.SourceID,
					PluginName: plugin.Name, ContentDigest: component.ContentDigest,
				})
			}
		}
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].Name != items[j].Name {
			return items[i].Name < items[j].Name
		}
		if items[i].SourceType != items[j].SourceType {
			return items[i].SourceType < items[j].SourceType
		}
		return items[i].SourceID < items[j].SourceID
	})
	return items, nil
}

func (s *Service) RuntimeSkills() (Result, error) {
	capabilities, err := s.CapabilityItems()
	if err != nil {
		return nil, err
	}
	skillsResult := make([]map[string]any, 0, len(capabilities))
	for _, capability := range capabilities {
		resolved, release, err := s.Acquire(context.Background(), capability.SkillRef)
		if err != nil {
			continue
		}
		files, filesErr := collectRuntimeSkillFiles(filepath.Clean(resolved.Root))
		release()
		if filesErr != nil {
			continue
		}
		item := map[string]any{
			"skill": resolved.Name, "name": capability.Name, "description": capability.Description,
			"skill_ref": resolved.SkillRef, "source_type": resolved.SourceType, "source_id": resolved.SourceID,
			"content_digest": capability.ContentDigest, "file_count": len(files),
		}
		if resolved.PluginName != "" {
			item["plugin_name"] = resolved.PluginName
		}
		skillsResult = append(skillsResult, item)
	}
	return Result{"action": "list", "count": len(skillsResult), "skills": skillsResult, "source": runtimeAPISource}, nil
}

func (s *Service) RuntimeSkill(skill string) (Result, error) {
	ref := runtimeSkillReference(skill)
	resolved, release, err := s.Acquire(context.Background(), ref)
	if err != nil {
		return nil, err
	}
	defer release()
	doc, err := skills.LoadSkillDocument(resolved.Root)
	if err != nil {
		return nil, skillToolError(err)
	}
	digest, err := runtimeSkillContentDigest(resolved)
	if err != nil {
		return nil, err
	}
	files, err := collectRuntimeSkillFiles(resolved.Root)
	if err != nil {
		return nil, err
	}
	result := Result{
		"action": "inspect", "skill": resolved.Name, "name": doc.Name, "description": doc.Description,
		"skill_ref": resolved.SkillRef, "source_type": resolved.SourceType, "source_id": resolved.SourceID,
		"content_digest": digest, "document": doc, "files": files,
		"file_count": len(files), "source": runtimeAPISource,
	}
	if resolved.PluginName != "" {
		result["plugin_name"] = resolved.PluginName
	}
	return result, nil
}

func runtimeSkillReference(value string) string {
	value = strings.TrimSpace(value)
	if strings.HasPrefix(value, "skill://") {
		return value
	}
	return ManagedSkillRef(value)
}

func runtimeSkillContentDigest(resolved ResolvedSkill) (string, error) {
	if resolved.ContentDigest != "" {
		return resolved.ContentDigest, nil
	}
	return managedContentDigest(resolved.Root)
}

func managedContentDigest(root string) (string, error) {
	digest, err := skills.DigestPackageContent(root)
	if err != nil {
		return "", toolErrorDetails("SKILL_PACKAGE_UNAVAILABLE", "failed to digest current managed Skill content", "runtime", map[string]any{"reason": err.Error()})
	}
	return digest, nil
}
