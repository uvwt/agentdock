package app

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func setUserHomeForTest(t *testing.T, home string) {
	t.Helper()
	// os.UserHomeDir 在 Unix 读取 HOME，在 Windows 读取 USERPROFILE。
	// 测试同时设置两者，避免平台差异让用例误扫 CI runner 的真实用户目录。
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
}

func TestCommonSkillCapabilityIndexListsValidSkillsInStableOrder(t *testing.T) {
	home := t.TempDir()
	setUserHomeForTest(t, home)
	root := filepath.Join(home, ".agents", "skills")
	writeCommonSkillForTest(t, root, "z-dir", "z-skill", "Z skill description.")
	writeCommonSkillForTest(t, root, "a-dir", "a-skill", strings.Repeat("A", commonSkillDescriptionBytes+40))
	writeCommonSkillFileForTest(t, filepath.Join(root, "invalid", "SKILL.md"), "---\nname: invalid\ndescription:\n---\n\n# Invalid\n")

	index, err := commonSkillCapabilityIndex()
	if err != nil {
		t.Fatal(err)
	}
	if index.Root != root || index.Total != 2 || index.Truncated || len(index.Items) != 2 {
		t.Fatalf("unexpected common Skill index: %#v", index)
	}
	if index.Items[0].Name != "a-skill" || index.Items[1].Name != "z-skill" {
		t.Fatalf("common Skills are not stably sorted: %#v", index.Items)
	}
	if index.Items[0].File != filepath.Join(root, "a-dir", "SKILL.md") {
		t.Fatalf("common Skill file path = %q", index.Items[0].File)
	}
	if len(index.Items[0].Description) > commonSkillDescriptionBytes {
		t.Fatalf("description was not truncated: %q", index.Items[0].Description)
	}
}

func TestCommonSkillCapabilityIndexTruncatesWithoutDroppingTotal(t *testing.T) {
	home := t.TempDir()
	setUserHomeForTest(t, home)
	root := filepath.Join(home, ".agents", "skills")
	for index := 0; index < commonSkillIndexLimit+3; index++ {
		name := fmt.Sprintf("skill-%02d", index)
		writeCommonSkillForTest(t, root, name, name, "Common skill.")
	}

	got, err := commonSkillCapabilityIndex()
	if err != nil {
		t.Fatal(err)
	}
	if got.Total != commonSkillIndexLimit+3 || !got.Truncated || len(got.Items) != commonSkillIndexLimit {
		t.Fatalf("unexpected truncated index: %#v", got)
	}
	if got.Items[0].Name != "skill-00" || got.Items[len(got.Items)-1].Name != "skill-49" {
		t.Fatalf("truncation must happen after stable sort: first=%q last=%q", got.Items[0].Name, got.Items[len(got.Items)-1].Name)
	}
}

func TestCommonSkillCapabilityIndexMissingRootIsEmpty(t *testing.T) {
	home := t.TempDir()
	setUserHomeForTest(t, home)

	index, err := commonSkillCapabilityIndex()
	if err != nil {
		t.Fatal(err)
	}
	if index.Total != 0 || index.Truncated || len(index.Items) != 0 {
		t.Fatalf("missing common Skill root should be an empty index: %#v", index)
	}
}

func writeCommonSkillForTest(t *testing.T, root, directory, name, description string) {
	t.Helper()
	content := "---\nname: " + name + "\ndescription: " + description + "\n---\n\n# " + name + "\n\nFollow this workflow.\n"
	writeCommonSkillFileForTest(t, filepath.Join(root, directory, "SKILL.md"), content)
}

func writeCommonSkillFileForTest(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}
