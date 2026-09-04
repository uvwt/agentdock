package scripts

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestCoreSkillBundleNormalizesTextLineEndings(t *testing.T) {
	python := ""
	for _, candidate := range []string{"python3", "python"} {
		path, err := exec.LookPath(candidate)
		if err != nil {
			continue
		}
		probe := exec.Command(path, "--version")
		probe.Dir = t.TempDir()
		output, err := probe.CombinedOutput()
		if err == nil && strings.Contains(string(output), "Python 3") {
			python = path
			break
		}
	}
	if python == "" {
		if os.Getenv("CI") != "" {
			t.Fatal("Python 3 is required to test the core Skill bundle builder in CI")
		}
		t.Skip("Python is required to test the core Skill bundle builder")
	}
	script, err := filepath.Abs("../../packaging/build-core-skill-bundle.py")
	if err != nil {
		t.Fatal(err)
	}

	build := func(lineEnding string) string {
		t.Helper()
		repoRoot := t.TempDir()
		for _, name := range []string{"agentdock-user-guide", "skill-authoring", "skill-installation", "skill-vetter-runtime"} {
			skillRoot := filepath.Join(repoRoot, "core-skills", name)
			if err := os.MkdirAll(skillRoot, 0o700); err != nil {
				t.Fatal(err)
			}
			document := "---\nname: " + name + "\ndescription: Test Skill.\nversion: 1.0.0\n---\n\n# Test\n"
			document = strings.ReplaceAll(document, "\n", lineEnding)
			if err := os.WriteFile(filepath.Join(skillRoot, "SKILL.md"), []byte(document), 0o600); err != nil {
				t.Fatal(err)
			}
			scriptBody := strings.ReplaceAll("print('test')\n", "\n", lineEnding)
			if err := os.WriteFile(filepath.Join(skillRoot, "run.py"), []byte(scriptBody), 0o600); err != nil {
				t.Fatal(err)
			}
		}
		output := filepath.Join(t.TempDir(), "bundle")
		command := exec.Command(python, script, "--repo-root", repoRoot, "--output", output)
		command.Dir = t.TempDir()
		if data, err := command.CombinedOutput(); err != nil {
			t.Fatalf("build core Skill bundle: %v\n%s", err, data)
		}
		return output
	}

	lfBundle := build("\n")
	crlfBundle := build("\r\n")
	for _, relative := range []string{
		"manifest.json",
		filepath.Join("packages", "agentdock-user-guide.zip"),
		filepath.Join("packages", "skill-authoring.zip"),
		filepath.Join("packages", "skill-installation.zip"),
		filepath.Join("packages", "skill-vetter-runtime.zip"),
	} {
		lfData, err := os.ReadFile(filepath.Join(lfBundle, relative))
		if err != nil {
			t.Fatal(err)
		}
		crlfData, err := os.ReadFile(filepath.Join(crlfBundle, relative))
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(lfData, crlfData) {
			t.Fatalf("core Skill bundle differs between LF and CRLF input: %s", relative)
		}
	}
}
