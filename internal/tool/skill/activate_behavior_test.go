package skill

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	skills "github.com/uvwt/agentdock/internal/skill"
)

func TestSkillPackageActivateInstalledVersion(t *testing.T) {
	runtime, _ := newSkillTestService(t)
	for _, version := range []string{"1.0.0", "1.1.0"} {
		packageDir, err := runtime.state.InstalledPath("demo-skill", version)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(packageDir, 0o700); err != nil {
			t.Fatal(err)
		}
		document := "---\nname: demo-skill\ndescription: Demo Skill.\nversion: " + version + "\n---\n\n# Demo\n"
		if err := os.WriteFile(filepath.Join(packageDir, "SKILL.md"), []byte(document), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := runtime.state.Activate(context.Background(), "demo-skill", "1.0.0"); err != nil {
		t.Fatal(err)
	}

	response, err := runtime.Package(context.Background(), map[string]any{
		"action":  "activate",
		"skill":   "demo-skill",
		"version": "1.1.0",
	})
	if err != nil {
		t.Fatal(err)
	}
	result, ok := response["result"].(skills.ActivateResult)
	if !ok || result.FromVersion != "1.0.0" || result.ToVersion != "1.1.0" {
		t.Fatalf("unexpected activate result: %#v", response)
	}
}
