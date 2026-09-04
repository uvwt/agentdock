package app

import (
	"os"
	"path/filepath"
	"sort"
	"strings"

	skills "github.com/uvwt/agentdock/internal/skill"
)

const (
	commonSkillIndexLimit       = 50
	commonSkillDescriptionBytes = 120
)

func commonSkillCapabilityIndex() (*capabilityCommonSkillIndex, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	root := filepath.Join(home, ".agents", "skills")
	entries, err := os.ReadDir(root)
	if err != nil {
		if os.IsNotExist(err) {
			return &capabilityCommonSkillIndex{Root: root, Items: []capabilityCommonSkillItem{}}, nil
		}
		return nil, err
	}

	items := make([]capabilityCommonSkillItem, 0, len(entries))
	for _, entry := range entries {
		packageDir := filepath.Join(root, entry.Name())
		info, statErr := os.Stat(packageDir)
		if statErr != nil || !info.IsDir() {
			continue
		}
		documentPath := filepath.Join(packageDir, "SKILL.md")
		data, readErr := os.ReadFile(documentPath)
		if readErr != nil {
			continue
		}
		metadata, parseErr := skills.ParseSkillMetadata(data)
		if parseErr != nil {
			continue
		}
		items = append(items, capabilityCommonSkillItem{
			Name:        metadata.Name,
			Description: truncateString(strings.TrimSpace(metadata.Description), commonSkillDescriptionBytes),
			File:        documentPath,
		})
	}

	// 文件系统遍历顺序不应影响启动 Context；按名称和路径稳定排序后再截断。
	sort.Slice(items, func(i, j int) bool {
		if items[i].Name == items[j].Name {
			return items[i].File < items[j].File
		}
		return items[i].Name < items[j].Name
	})
	total := len(items)
	truncated := total > commonSkillIndexLimit
	if truncated {
		items = items[:commonSkillIndexLimit]
	}
	return &capabilityCommonSkillIndex{Root: root, Total: total, Truncated: truncated, Items: items}, nil
}
