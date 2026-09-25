package app

import (
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	skills "github.com/uvwt/agentdock/internal/skill"
)

const (
	filesystemSkillIndexLimit       = 50
	filesystemSkillDescriptionBytes = 120
	filesystemSkillDocumentMaxBytes = 1 << 20
)

type filesystemSkillItem struct {
	Name        string
	Description string
	File        string
}

type filesystemSkillIndex struct {
	Items     []filesystemSkillItem
	Total     int
	Truncated bool
}

// scanCommonFilesystemSkills 保留全局 common Skill 的历史行为：
// package 目录和 SKILL.md 都允许通过 symlink 访问。
func scanCommonFilesystemSkills(root string) (filesystemSkillIndex, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		if os.IsNotExist(err) {
			return filesystemSkillIndex{Items: []filesystemSkillItem{}}, nil
		}
		return filesystemSkillIndex{}, err
	}

	index, err := indexFilesystemSkills(entries, func(entry os.DirEntry) (string, []byte, error) {
		packageDir := filepath.Join(root, entry.Name())
		info, err := os.Stat(packageDir)
		if err != nil || !info.IsDir() {
			return "", nil, os.ErrInvalid
		}
		documentPath := filepath.Join(packageDir, "SKILL.md")
		data, readErr := readCommonFilesystemSkillDocument(packageDir)
		if readErr != nil {
			return "", nil, readErr
		}
		return documentPath, data, nil
	})
	if err != nil {
		return filesystemSkillIndex{}, err
	}
	// common Skills 来自数量不可控的共享目录，继续保留历史的逐项 description 预算。
	for i := range index.Items {
		index.Items[i].Description = truncateString(index.Items[i].Description, filesystemSkillDescriptionBytes)
	}
	return index, nil
}

// scanWorkspaceFilesystemSkills 在扫描 .agents/skills 期间始终持有 workspace Root。
// 所有路径都从该 Root 相对解析，因此父目录 symlink 或 Windows reparse point
// 不能把扫描重定向到所选 workspace 之外。common Skill 继续走独立的历史兼容入口。
func scanWorkspaceFilesystemSkills(workspaceRoot string) (filesystemSkillIndex, error) {
	root, err := os.OpenRoot(workspaceRoot)
	if err != nil {
		return filesystemSkillIndex{}, err
	}
	defer root.Close()

	const skillRoot = ".agents/skills"
	dir, err := root.Open(filepath.FromSlash(skillRoot))
	if err != nil {
		if os.IsNotExist(err) {
			return filesystemSkillIndex{Items: []filesystemSkillItem{}}, nil
		}
		return filesystemSkillIndex{}, err
	}
	entries, err := dir.ReadDir(-1)
	closeErr := dir.Close()
	if err != nil {
		return filesystemSkillIndex{}, err
	}
	if closeErr != nil {
		return filesystemSkillIndex{}, closeErr
	}

	relativeSkillRoot := filepath.FromSlash(skillRoot)
	return indexFilesystemSkills(entries, func(entry os.DirEntry) (string, []byte, error) {
		packagePath := filepath.Join(relativeSkillRoot, entry.Name())
		info, err := root.Lstat(packagePath)
		if err != nil || !info.IsDir() {
			return "", nil, os.ErrInvalid
		}

		documentPath := filepath.Join(packagePath, "SKILL.md")
		before, err := root.Lstat(documentPath)
		if err != nil || !before.Mode().IsRegular() {
			return "", nil, os.ErrInvalid
		}
		file, err := root.Open(documentPath)
		if err != nil {
			return "", nil, err
		}
		defer file.Close()
		after, err := file.Stat()
		if err != nil || !after.Mode().IsRegular() || !os.SameFile(before, after) {
			return "", nil, os.ErrInvalid
		}
		data, err := readBoundedSkillDocument(file)
		if err != nil {
			return "", nil, err
		}
		return filepath.Join(workspaceRoot, documentPath), data, nil
	})
}

func indexFilesystemSkills(entries []os.DirEntry, load func(os.DirEntry) (string, []byte, error)) (filesystemSkillIndex, error) {
	items := make([]filesystemSkillItem, 0, len(entries))
	for _, entry := range entries {
		documentPath, data, err := load(entry)
		if err != nil {
			continue
		}
		metadata, parseErr := skills.ParseSkillMetadata(data)
		if parseErr != nil {
			continue
		}
		if metadata.Name != entry.Name() {
			continue
		}
		items = append(items, filesystemSkillItem{
			Name:        metadata.Name,
			Description: strings.TrimSpace(metadata.Description),
			File:        documentPath,
		})
	}

	sort.Slice(items, func(i, j int) bool {
		if items[i].Name == items[j].Name {
			return items[i].File < items[j].File
		}
		return items[i].Name < items[j].Name
	})
	index := filesystemSkillIndex{Items: items, Total: len(items)}
	if index.Total > filesystemSkillIndexLimit {
		index.Truncated = true
		index.Items = index.Items[:filesystemSkillIndexLimit]
	}
	return index, nil
}

func readCommonFilesystemSkillDocument(packageDir string) ([]byte, error) {
	file, err := os.Open(filepath.Join(packageDir, "SKILL.md"))
	if err != nil {
		return nil, err
	}
	defer file.Close()
	return readBoundedSkillDocument(file)
}

func readBoundedSkillDocument(file *os.File) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(file, filesystemSkillDocumentMaxBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > filesystemSkillDocumentMaxBytes {
		return nil, os.ErrInvalid
	}
	return data, nil
}
