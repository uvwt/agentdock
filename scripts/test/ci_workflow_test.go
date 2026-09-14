package scripts

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func readWorkflow(t *testing.T, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", ".github", "workflows", name))
	if err != nil {
		t.Fatalf("read workflow %s: %v", name, err)
	}
	return strings.ReplaceAll(string(data), "\r\n", "\n")
}

func TestCIWorkflowUsesFreshBoundedGoTests(t *testing.T) {
	workflow := readWorkflow(t, "ci.yml")
	for _, want := range []string{
		"timeout-minutes: 20",
		"go test ./... -count=1 -timeout=3m",
		"name: ACP prompt and steering race regression",
		"-count=20",
		"-timeout=90s",
		"go test -race ./... -count=1 -timeout=3m",
		"go test -race -tags browser_integration ./internal/tool/browser ./internal/app -count=1 -timeout=3m",
		"timeout-minutes: 15",
	} {
		if !strings.Contains(workflow, want) {
			t.Fatalf("CI workflow must keep bounded non-cached validation; missing %q", want)
		}
	}
}

func TestWindowsInstallerWorkflowHasAlwaysPresentPullRequestGate(t *testing.T) {
	workflow := readWorkflow(t, "windows-installer.yml")
	for _, want := range []string{
		"pull_request:\n    branches:\n      - main",
		"name: Detect Windows installer changes",
		"fetch-depth: 0",
		"git diff --name-only \"$BASE_SHA\" \"$HEAD_SHA\"",
		"name: Validate installer on Windows PowerShell 5.1",
		"needs: changes",
		"if: needs.changes.outputs.relevant == 'true'",
		"timeout-minutes: 30",
		"name: Windows Installer gate",
		"needs: [changes, validate]",
		"if: always()",
		"CHANGES_RESULT: ${{ needs.changes.result }}",
		"VALIDATE_RESULT: ${{ needs.validate.result }}",
		"github.event_name == 'workflow_dispatch' && inputs.test_tag != ''",
	} {
		if !strings.Contains(workflow, want) {
			t.Fatalf("Windows Installer workflow must keep a safe pull-request gate; missing %q", want)
		}
	}
}
