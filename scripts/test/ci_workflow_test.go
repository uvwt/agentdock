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

func TestWorkflowsUseCurrentActionMajors(t *testing.T) {
	expected := map[string]string{
		"uses: actions/checkout@":               "uses: actions/checkout@v5",
		"uses: actions/setup-go@":               "uses: actions/setup-go@v6",
		"uses: actions/setup-dotnet@":           "uses: actions/setup-dotnet@v5",
		"uses: github/codeql-action/init@":      "uses: github/codeql-action/init@v4",
		"uses: github/codeql-action/autobuild@": "uses: github/codeql-action/autobuild@v4",
		"uses: github/codeql-action/analyze@":   "uses: github/codeql-action/analyze@v4",
	}
	foundManagedAction := false
	for _, name := range []string{"ci.yml", "codeql.yml", "release.yml", "windows-installer.yml"} {
		workflow := readWorkflow(t, name)
		for _, line := range strings.Split(workflow, "\n") {
			trimmed := strings.TrimSpace(line)
			for prefix, want := range expected {
				if !strings.HasPrefix(trimmed, prefix) {
					continue
				}
				foundManagedAction = true
				if trimmed != want {
					t.Fatalf("workflow %s must use %q, got %q", name, want, trimmed)
				}
			}
		}
	}
	if !foundManagedAction {
		t.Fatal("expected workflows to use managed GitHub Actions")
	}
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
		"-InstallerPath .\\scripts\\install\\install.ps1",
		"name: Download and verify cloudflared compatibility payload",
		"for ($attempt = 1; $attempt -le 5; $attempt++)",
		"Get-AuthenticodeSignature -LiteralPath $cloudflaredPath",
	} {
		if !strings.Contains(workflow, want) {
			t.Fatalf("Windows Installer workflow must keep a safe pull-request gate; missing %q", want)
		}
	}
	if strings.Contains(workflow, "raw.githubusercontent.com/${{ github.repository }}/${{ github.sha }}/scripts/install/install.ps1") {
		t.Fatal("routine Windows installer validation must use the checked-out installer instead of refetching it over the network")
	}
}
