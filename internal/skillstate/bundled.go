package skillstate

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/uvwt/agentdock/internal/atomicfile"
)

const (
	bundledSkillsFile = "bundled-skills.json"
	bundledSkillsLock = "_bundled_skills"
)

type bundledSkillsDocument struct {
	Skills []string `json:"skills"`
}

// BundledSkills 返回当前由 AgentDock 随附管理的 Skill 名称。
// 版本和摘要仍由 installed/ 与 state/ 负责，避免在清单中重复保存。
func (s *Store) BundledSkills() ([]string, error) {
	data, err := os.ReadFile(filepath.Join(s.root, bundledSkillsFile))
	if os.IsNotExist(err) {
		return []string{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read bundled skills: %w", err)
	}
	var document bundledSkillsDocument
	if err := json.Unmarshal(data, &document); err != nil {
		return nil, fmt.Errorf("decode bundled skills: %w", err)
	}
	return normalizeBundledSkills(document.Skills)
}

func (s *Store) IsBundled(skill string) (bool, error) {
	if err := validateIdentifier("skill", skill); err != nil {
		return false, err
	}
	names, err := s.BundledSkills()
	if err != nil {
		return false, err
	}
	index := sort.SearchStrings(names, skill)
	return index < len(names) && names[index] == skill, nil
}

// ReplaceBundledSkills 原子替换内置清单。调用方应在所有随附 Skill
// 安装成功后再提交名单，避免留下“已内置但未安装”的半完成状态。
func (s *Store) ReplaceBundledSkills(ctx context.Context, skills []string) error {
	names, err := normalizeBundledSkills(skills)
	if err != nil {
		return err
	}
	release, err := s.acquire(ctx, bundledSkillsLock)
	if err != nil {
		return err
	}
	defer release()

	data, err := json.MarshalIndent(bundledSkillsDocument{Skills: names}, "", "  ")
	if err != nil {
		return fmt.Errorf("encode bundled skills: %w", err)
	}
	if err := atomicfile.Write(filepath.Join(s.root, bundledSkillsFile), data, 0o600); err != nil {
		return fmt.Errorf("replace bundled skills: %w", err)
	}
	return nil
}

func normalizeBundledSkills(skills []string) ([]string, error) {
	seen := make(map[string]struct{}, len(skills))
	names := make([]string, 0, len(skills))
	for _, skill := range skills {
		if err := validateIdentifier("skill", skill); err != nil {
			return nil, err
		}
		if _, exists := seen[skill]; exists {
			continue
		}
		seen[skill] = struct{}{}
		names = append(names, skill)
	}
	sort.Strings(names)
	return names, nil
}
