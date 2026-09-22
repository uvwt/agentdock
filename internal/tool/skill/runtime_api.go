package skill

import (
	"context"
	"path/filepath"
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
	return items, nil
}

func (s *Service) RuntimeSkills() (Result, error) {
	names, err := s.state.ListSkills()
	if err != nil {
		return nil, skillToolError(err)
	}
	skillsResult := make([]map[string]any, 0, len(names))
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
		files, filesErr := collectRuntimeSkillFiles(filepath.Clean(resolved.Root))
		release()
		if docErr != nil || digestErr != nil || filesErr != nil || doc.Name != name {
			continue
		}
		skillsResult = append(skillsResult, map[string]any{
			"skill": name, "name": name, "description": strings.TrimSpace(doc.Description),
			"skill_ref": resolved.SkillRef, "source_type": resolved.SourceType, "source_id": resolved.SourceID,
			"content_digest": digest, "file_count": len(files),
		})
	}
	return Result{"action": "list", "count": len(skillsResult), "skills": skillsResult, "source": runtimeAPISource}, nil
}

func (s *Service) RuntimeSkill(skill string) (Result, error) {
	ref := ManagedSkillRef(strings.TrimSpace(skill))
	resolved, release, err := s.Acquire(context.Background(), ref)
	if err != nil {
		return nil, err
	}
	defer release()
	doc, err := skills.LoadSkillDocument(resolved.Root)
	if err != nil {
		return nil, skillToolError(err)
	}
	digest, err := managedContentDigest(resolved.Root)
	if err != nil {
		return nil, err
	}
	files, err := collectRuntimeSkillFiles(resolved.Root)
	if err != nil {
		return nil, err
	}
	return Result{
		"action": "inspect", "skill": resolved.Name, "name": doc.Name, "description": doc.Description,
		"skill_ref": resolved.SkillRef, "source_type": resolved.SourceType, "source_id": resolved.SourceID,
		"content_digest": digest, "document": doc, "files": files,
		"file_count": len(files), "source": runtimeAPISource,
	}, nil
}

func managedContentDigest(root string) (string, error) {
	digest, err := skills.DigestPackageContent(root)
	if err != nil {
		return "", toolErrorDetails("SKILL_PACKAGE_UNAVAILABLE", "failed to digest current managed Skill content", "runtime", map[string]any{"reason": err.Error()})
	}
	return digest, nil
}
