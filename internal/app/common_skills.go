package app

import (
	"os"
	"path/filepath"
)

func commonSkillCapabilityIndex() (*capabilityCommonSkillIndex, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	root := filepath.Join(home, ".agents", "skills")
	index, err := scanCommonFilesystemSkills(root)
	if err != nil {
		return nil, err
	}
	items := make([]capabilityCommonSkillItem, 0, len(index.Items))
	for _, item := range index.Items {
		items = append(items, capabilityCommonSkillItem{
			Name: item.Name, Description: item.Description, File: item.File,
		})
	}
	return &capabilityCommonSkillIndex{Root: root, Total: index.Total, Truncated: index.Truncated, Items: items}, nil
}
