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
		"name: Detect container-relevant changes",
		"if: steps.changes.outputs.relevant == 'true'",
		".dockerignore",
		"docker-compose.yml",
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
		"cmd/agentdock-wsl-helper",
		"internal/wslfilehelper",
		"scripts/test/testdata/fake-cloudflared",
	} {
		if !strings.Contains(workflow, want) {
			t.Fatalf("Windows Installer workflow must keep a safe pull-request gate; missing %q", want)
		}
	}
	if strings.Contains(workflow, "raw.githubusercontent.com/${{ github.repository }}/${{ github.sha }}/scripts/install/install.ps1") {
		t.Fatal("routine Windows installer validation must use the checked-out installer instead of refetching it over the network")
	}
}

func TestReleaseWorkflowGatesBeforePublication(t *testing.T) {
	workflow := readWorkflow(t, "release.yml")
	for _, want := range []string{
		"name: Validate release source",
		"name: Prepare release candidate",
		"name: Verify staged Linux release",
		"name: Verify staged macOS release",
		"name: Verify staged Windows release",
		"name: Release pre-publication gate",
		"name: Stage draft GitHub Release",
		"draft: true",
		"name: Publish containers",
		"org.opencontainers.image.revision=${{ needs.source.outputs.commit }}",
		"name: Verify versioned containers",
		"name: Run versioned GHCR images",
		"name: Run versioned Docker Hub images",
		"name: Promote aliases and publish GitHub Release",
		"name: Promote validated mutable aliases",
		"docker buildx imagetools create --tag",
		"needs: [source, stage-release, verify-container]",
		"group: release-publication",
		"release_api_error=\"$RUNNER_TEMP/release-api-error.log\"",
		"HTTP 404",
	} {
		if !strings.Contains(workflow, want) {
			t.Fatalf("Release workflow must gate public resources behind staged validation; missing %q", want)
		}
	}

	order := []string{
		"  prepare-release:",
		"  verify-windows-release:",
		"  release-gate:",
		"  stage-release:",
		"  publish-container:",
		"  verify-container:",
		"  publish-release:",
	}
	last := -1
	for _, marker := range order {
		index := strings.Index(workflow, marker)
		if index <= last {
			t.Fatalf("Release workflow has unsafe publication order around %q", marker)
		}
		last = index
	}

	if strings.Contains(workflow, "type=raw,value=latest") ||
		strings.Contains(workflow, "type=raw,value=dev-latest") ||
		strings.Contains(workflow, "type=raw,value=browser-latest") {
		t.Fatal("container build/push must not move mutable aliases before versioned registry images pass verification")
	}
	if strings.Contains(workflow, "releases/tags/$RELEASE_TAG\" --jq .draft 2>/dev/null || true") {
		t.Fatal("published-release immutability check must fail closed on API, auth, and network errors")
	}
	if count := strings.Count(workflow, "ref: ${{ github.event.inputs.tag || github.ref }}"); count != 1 {
		t.Fatalf("only the source validator may resolve the release tag directly; got %d tag checkouts", count)
	}
	if !strings.Contains(workflow, "ref: ${{ needs.source.outputs.commit }}") {
		t.Fatal("release jobs after source validation must pin checkout to the validated commit")
	}
}

func TestRoutineCIOnlyRunsPreMergeAndCancelsSupersededRuns(t *testing.T) {
	for _, name := range []string{"ci.yml", "windows-installer.yml"} {
		workflow := readWorkflow(t, name)
		if strings.Contains(workflow, "push:\n    branches: [main]") ||
			strings.Contains(workflow, "push:\n    branches:\n      - main") {
			t.Fatalf("workflow %s must not repeat the protected PR gate after merge", name)
		}
		if !strings.Contains(workflow, "cancel-in-progress: true") {
			t.Fatalf("workflow %s must cancel superseded PR runs", name)
		}
	}
}

func TestWindowsLegacyMigrationE2EIsIndependentFromPublishedLatest(t *testing.T) {
	data, err := os.ReadFile("test-windows-legacy-online-migration.ps1")
	if err != nil {
		t.Fatalf("read Windows legacy migration E2E: %v", err)
	}
	script := strings.ReplaceAll(string(data), "\r\n", "\n")
	for _, want := range []string{
		"AgentDockLegacyMigration",
		"[Threading.Mutex]::new(",
		"$migrationGate.ReleaseMutex()",
		"__repair-desktop-runtime --local-archive",
	} {
		if !strings.Contains(script, want) {
			t.Fatalf("legacy migration E2E must isolate automatic latest-version repair; missing %q", want)
		}
	}
	if strings.Contains(script, "Wait-NoDesktopRepairProcess") {
		t.Fatal("legacy migration E2E must not depend on GitHub latest making background repair exit")
	}
}

func TestCodeQLKeepsDefaultBranchAndScheduledScanning(t *testing.T) {
	workflow := readWorkflow(t, "codeql.yml")
	for _, want := range []string{
		"push:\n    branches: [main]",
		"pull_request:\n    branches: [main]",
		"schedule:",
		"name: Analyze Go",
	} {
		if !strings.Contains(workflow, want) {
			t.Fatalf("CodeQL must keep default-branch security coverage; missing %q", want)
		}
	}
}
