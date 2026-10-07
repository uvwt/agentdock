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
		"name: Resolve CI package version",
		"$releaseVersion = '1.0.0-ci.0'",
		"$releaseVersion = (& go run .\\tools\\release version $testTag).Trim()",
		"$coreVersion = (& go run .\\tools\\release core-version $releaseVersion).Trim()",
		"AGENTDOCK_RELEASE_VERSION=$releaseVersion",
		"AGENTDOCK_WINDOWS_VERSION=$windowsVersion",
		"internal/buildinfo.Version=$env:AGENTDOCK_RELEASE_VERSION",
		"set-version-info.ps1 -Version $env:AGENTDOCK_WINDOWS_VERSION -ProductVersion $env:AGENTDOCK_RELEASE_VERSION",
		"-p:InformationalVersion=$env:AGENTDOCK_RELEASE_VERSION",
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
		"name: Test cloudflared component lifecycle",
		"for ($attempt = 1; $attempt -le 5; $attempt++)",
		"Get-AuthenticodeSignature -LiteralPath $cloudflaredPath",
		".\\internal\\component\\catalog-v1.json",
		"cloudflared pinned SHA-256 mismatch",
		".\\scripts\\test\\test-windows-cloudflared-component.ps1",
		"-SignedCloudflaredBinary $cloudflaredPath",
		"-ArtifactUrl $cloudflaredUrl",
		"-ExpectedDigest $expectedDigest",
		"cmd/agentdock-wsl-helper",
		"internal/wslfilehelper",
		"scripts/test/testdata/fake-cloudflared",
		"internal/component",
		"tools/release",
	} {
		if !strings.Contains(workflow, want) {
			t.Fatalf("Windows Installer workflow must keep a safe pull-request gate; missing %q", want)
		}
	}
	if strings.Contains(workflow, "raw.githubusercontent.com/${{ github.repository }}/${{ github.sha }}/scripts/install/install.ps1") {
		t.Fatal("routine Windows installer validation must use the checked-out installer instead of refetching it over the network")
	}
	if strings.Contains(workflow, "cloudflared/releases/latest") {
		t.Fatal("Windows Installer validation must pin the cloudflared component version instead of downloading upstream latest")
	}
	if strings.Contains(workflow, "AMD64_CLOUDFLARED") || strings.Contains(workflow, "-CloudflaredBinary $env:") {
		t.Fatal("Windows Setup build must not carry cloudflared as an installer payload")
	}
	if strings.Contains(workflow, "Get-Content -LiteralPath '.\\internal\\buildinfo\\buildinfo.go'") ||
		strings.Contains(workflow, "Could not replace buildinfo.Version") {
		t.Fatal("Windows validation must inject test versions at link time instead of rewriting or parsing buildinfo.go")
	}
}

func TestReleaseWorkflowHasSignPathFoundationReviewPath(t *testing.T) {
	workflow := readWorkflow(t, "release.yml")
	for _, want := range []string{
		"signpath_review:",
		"name: SignPath Foundation review build",
		"actions: read",
		"uses: actions/upload-artifact@v7",
		"name: Submit SignPath test signing request",
		"uses: signpath/github-action-submit-signing-request@v3",
		"api-token: ${{ secrets.SIGNPATH_API_TOKEN }}",
		"project-slug: agentdock",
		"signing-policy-slug: test-signing",
		"artifact-configuration-slug: windows-binaries",
		"version: ${{ toJSON(steps.version.outputs.windows) }}",
		"-p:FileVersion='${{ steps.version.outputs.windows }}'",
		"-p:FileVersion=$windowsVersion",
		"803C2EBAEE1907BF990CA761A57ADF31AD78AA12",
		".\\packaging\\windows\\set-version-info.ps1",
		"-ProductVersion '${{ steps.version.outputs.release }}'",
		"-ProductVersion $releaseVersion",
		"**Code signing policy:** https://github.com/${{ github.repository }}/blob/main/docs/code-signing-policy.md",
	} {
		if !strings.Contains(workflow, want) {
			t.Fatalf("Release workflow must keep the SignPath Foundation review path; missing %q", want)
		}
	}
}

func TestReleaseWorkflowUsesGitTagAsSingleReleaseVersionSource(t *testing.T) {
	workflow := readWorkflow(t, "release.yml")
	for _, want := range []string{
		`release_metadata="$(go run ./tools/release release-metadata "$RELEASE_TAG")"`,
		`version="$(jq -r '.version' <<<"$release_metadata")"`,
		`version: ${{ steps.source.outputs.version }}`,
		`AGENTDOCK_RELEASE_VERSION: ${{ needs.source.outputs.version }}`,
		`internal/buildinfo.Version=${AGENTDOCK_RELEASE_VERSION}`,
		`$releaseVersion = '${{ needs.source.outputs.version }}'`,
		`packaging/macos/build-app.sh '${{ needs.source.outputs.version }}'`,
		`BUILD_VERSION=${{ needs.source.outputs.version }}`,
	} {
		if !strings.Contains(workflow, want) {
			t.Fatalf("Release workflow must derive build versions from the validated Git tag; missing %q", want)
		}
	}
	for _, forbidden := range []string{
		"go run ./tools/release verify-version",
		"go run ./tools/release version).Trim()",
	} {
		if strings.Contains(workflow, forbidden) {
			t.Fatalf("Release workflow must not treat source buildinfo as release authority: found %q", forbidden)
		}
	}
}

func TestReleaseWorkflowComparesStagedPowerShellInstallerToGitBlob(t *testing.T) {
	workflow := readWorkflow(t, "release.yml")
	for _, want := range []string{
		"$sourceCommit = '${{ needs.source.outputs.commit }}'",
		"git rev-parse \"${sourceCommit}:scripts/install/install.ps1\"",
		"git hash-object --no-filters -- $installer",
		"Staged install.ps1 does not match source blob",
	} {
		if !strings.Contains(workflow, want) {
			t.Fatalf("Release workflow must compare the staged PowerShell installer to the immutable Git blob; missing %q", want)
		}
	}
	if strings.Contains(workflow, "Get-FileHash -LiteralPath '.\\\\scripts\\\\install\\\\install.ps1'") {
		t.Fatal("Release workflow must not compare staged install.ps1 to a Windows checkout whose line endings may be rewritten")
	}
}

func TestReleaseWorkflowPublishesDraftByReleaseID(t *testing.T) {
	workflow := readWorkflow(t, "release.yml")
	for _, want := range []string{
		"release_id: ${{ steps.release.outputs.id }}",
		"id: release",
		`.name == \"$RELEASE_TAG\"`,
		"RELEASE_ID: ${{ needs.stage-release.outputs.release_id }}",
		`gh api --method PATCH "repos/${{ github.repository }}/releases/$RELEASE_ID"`,
		`-f target_commitish="$SOURCE_COMMIT"`,
		"-F draft=false",
		"PRERELEASE: ${{ needs.source.outputs.prerelease }}",
		`-F prerelease="$PRERELEASE"`,
		`-f make_latest="$make_latest"`,
	} {
		if !strings.Contains(workflow, want) {
			t.Fatalf("Release workflow must publish the validated draft by immutable release ID; missing %q", want)
		}
	}
	if strings.Contains(workflow, `gh release edit "$RELEASE_TAG"`) {
		t.Fatal("Release workflow must not publish a draft by tag because GitHub exposes draft releases as untagged")
	}
}

func TestWindowsTrayCarriesSignPathMetadataFromMSBuild(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "desktop", "windows", "winui", "AgentDock.WinUI.csproj"))
	if err != nil {
		t.Fatalf("read Windows control panel project: %v", err)
	}
	project := string(data)
	for _, want := range []string{
		"<Product>AgentDock</Product>",
		"<Company>AgentDock</Company>",
		"<Copyright>Copyright AgentDock contributors</Copyright>",
		"<AssemblyName>agentdock-tray</AssemblyName>",
		"<IncludeSourceRevisionInInformationalVersion>false</IncludeSourceRevisionInInformationalVersion>",
	} {
		if !strings.Contains(project, want) {
			t.Fatalf("Windows tray project must carry SignPath metadata; missing %q", want)
		}
	}
}

func TestWindowsVersionInfoScriptCoversSignPathMetadata(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "packaging", "windows", "set-version-info.ps1"))
	if err != nil {
		t.Fatalf("read Windows VersionInfo script: %v", err)
	}
	script := strings.ReplaceAll(string(data), "\r\n", "\n")
	for _, want := range []string{
		"github.com/tc-hib/go-winres@v0.3.3",
		"go env GOHOSTOS",
		"go env GOHOSTARCH",
		"$env:GOOS = $goHostOS",
		"$env:GOARCH = $goHostArch",
		"CompanyName = 'AgentDock'",
		"ProductName = 'AgentDock'",
		"'agentdock-tray.exe' = 'AgentDock'",
		"ProductVersion = $displayProductVersion",
		"FileVersion = $windowsVersion",
		"LegalCopyright = $copyright",
		"$originalFilenames = @{",
		"'agentdock-tray.exe' = 'agentdock-tray.dll'",
		"OriginalFilename = $expectedOriginalFilename",
		"'agentdock.exe'",
		"'agentdock-tray.exe'",
		"'agentdock-arbiter.exe'",
		"'agentdock-shim.exe'",
		"'agentdock-tray-shim.exe'",
		"RT_GROUP_ICON",
		"assets\\agentdock.ico",
		"VersionInfo must be applied before Authenticode signing",
	} {
		if !strings.Contains(script, want) {
			t.Fatalf("Windows VersionInfo script must enforce SignPath metadata; missing %q", want)
		}
	}
	if strings.Contains(script, "AgentDock Control Panel") {
		t.Fatal("Windows VersionInfo must expose AgentDock instead of the legacy Control Panel name")
	}
}

func TestWindowsInstallerEmbedsBrandIconIntoStableTrayShim(t *testing.T) {
	workflow := readWorkflow(t, "windows-installer.yml")
	for _, want := range []string{
		"-Path (Join-Path $distRoot 'agentdock-tray-shim.exe')",
		"-Path .\\dist\\agentdock-tray-shim.exe",
		"-Path (Join-Path $arm64Dir 'agentdock-tray-shim.exe')",
	} {
		if !strings.Contains(workflow, want) {
			t.Fatalf("Windows installer workflow must brand the stable Tray shim before packaging: %q", want)
		}
	}
}

func TestCodeSigningPolicyMeetsFoundationDisclosureRequirements(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "docs", "code-signing-policy.md"))
	if err != nil {
		t.Fatalf("read code signing policy: %v", err)
	}
	policy := strings.ReplaceAll(string(data), "\r\n", "\n")
	for _, want := range []string{
		"# Code signing policy",
		"Free code signing provided by [SignPath.io](https://signpath.io), certificate by [SignPath Foundation](https://signpath.org).",
		"**Authors / committers:**",
		"**Reviewers:**",
		"**Approvers:**",
		"AgentDock does not include usage analytics or telemetry.",
		"`cloudflared`",
	} {
		if !strings.Contains(policy, want) {
			t.Fatalf("Code signing policy must keep SignPath Foundation disclosures; missing %q", want)
		}
	}

	readme, err := os.ReadFile(filepath.Join("..", "..", "README.md"))
	if err != nil {
		t.Fatalf("read README: %v", err)
	}
	if !strings.Contains(string(readme), "[Code signing policy](./docs/code-signing-policy.md)") {
		t.Fatal("README home page must link the Code signing policy")
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
		"name: Prepare user-facing GitHub assets",
		"go run ./tools/release prepare-github-release dist github-release",
		"github-release/*",
		"draft: true",
		"name: Publish containers",
		"org.opencontainers.image.revision=${{ needs.source.outputs.commit }}",
		"name: Verify versioned containers",
		"name: Run versioned GHCR images",
		"name: Run versioned Docker Hub images",
		"name: Publish immutable Release to R2",
		"name: Retain previous stable, publish component repository and latest.json",
		"name: Promote aliases and publish GitHub Release",
		"name: Promote validated mutable aliases",
		"if: needs.source.outputs.prerelease != 'true'",
		"prerelease: ${{ steps.source.outputs.prerelease }}",
		"core_version: ${{ steps.source.outputs.core_version }}",
		`aws s3 cp dist/agentdock-component-catalog.json "s3://$R2_BUCKET/components/v1/catalog.json"`,
		`component_url="${R2_PUBLIC_BASE_URL%/}/components/v1/catalog.json?run=${GITHUB_RUN_ID}-${GITHUB_RUN_ATTEMPT}"`,
		`go run ./tools/release release-kind "$existing_latest_tag"`,
		`go run ./tools/release retention-plan`,
		"docker buildx imagetools create --tag",
		"needs: [source, prepare-release, stage-release, verify-container]",
		"needs: [source, stage-release, mirror-r2]",
		"--metadata \"sha256=$sha256\"",
		"Immutable R2 object already exists with a different SHA-256",
		"aws s3 rm \"s3://$R2_BUCKET/releases/$tag/\"",
		`jq -r '.delete_tags[]' "$RUNNER_TEMP/r2-retention-plan.json"`,
		"group: release-publication",
		"release_api_error=\"$RUNNER_TEMP/release-api-error.log\"",
		"HTTP 404",
		"LEGACY_GITHUB_LATEST_TAG: ${{ vars.LEGACY_GITHUB_LATEST_TAG }}",
		`elif [[ "$RELEASE_TAG" == "v1.0.0" && -z "$LEGACY_GITHUB_LATEST_TAG" ]]; then`,
		"v1.0.0 requires LEGACY_GITHUB_LATEST_TAG",
		`legacy_release="$(gh api "repos/${{ github.repository }}/releases/tags/$LEGACY_GITHUB_LATEST_TAG")"`,
		`latest_tag="$(gh api "repos/${{ github.repository }}/releases/latest" --jq .tag_name)"`,
		`test "$latest_tag" = "$LEGACY_GITHUB_LATEST_TAG"`,
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
		"  mirror-r2:",
	}
	last := -1
	for _, marker := range order {
		index := strings.Index(workflow, marker)
		if index <= last {
			t.Fatalf("Release workflow has unsafe publication order around %q", marker)
		}
		last = index
	}
	if strings.Contains(workflow, "prepare-distribution") {
		t.Fatal("release packaging must not rewrite bootstrap scripts for version-specific downloads")
	}
	if strings.Contains(workflow, `sh "$installer" --version "$RELEASE_TAG"`) {
		t.Fatal("staged release verification must use the latest-only installer contract")
	}
	for _, want := range []string{
		`AGENTDOCK_NONINTERACTIVE=true`,
		`AGENTDOCK_NEXUS_MODE=none`,
		`AGENTDOCK_TUNNEL_MODE=none`,
		`AGENTDOCK_INSTALLER_BASE_URL="$release_base"`,
		`sh "$installer"`,
		`installed_core_version="$("$RUNNER_TEMP/bin/agentdock" --version)"`,
		`[[ "$installed_core_version" == *"AgentDock $RELEASE_TAG"* ]]`,
	} {
		if !strings.Contains(workflow, want) {
			t.Fatalf("staged macOS verification must install the candidate via latest-only bootstrap; missing %q", want)
		}
	}
	for _, forbidden := range []string{
		"Previous stable R2 prefix is missing; keep existing release prefixes unchanged",
		`[[ "$tag" == "$previous_tag" ]] && continue`,
	} {
		if strings.Contains(workflow, forbidden) {
			t.Fatalf("R2 retention must derive previous stable from stored release prefixes; found obsolete contract %q", forbidden)
		}
	}
	cleanupIndex := strings.Index(workflow, `aws s3 rm "s3://$R2_BUCKET/releases/$tag/"`)
	publishComponentIndex := strings.Index(workflow, `aws s3 cp dist/agentdock-component-catalog.json "s3://$R2_BUCKET/components/v1/catalog.json"`)
	publishLatestIndex := strings.Index(workflow, `aws s3 cp dist/latest.json "s3://$R2_BUCKET/latest.json"`)
	if cleanupIndex < 0 || publishComponentIndex < 0 || publishLatestIndex < 0 ||
		cleanupIndex > publishComponentIndex || publishComponentIndex > publishLatestIndex {
		t.Fatal("R2 stable promotion must finish retention and component repository publication before latest.json moves")
	}

	stageStart := strings.Index(workflow, "  stage-release:")
	stageEnd := strings.Index(workflow, "  publish-container:")
	if stageStart < 0 || stageEnd <= stageStart {
		t.Fatal("release workflow is missing the stage-release boundary")
	}
	stageRelease := workflow[stageStart:stageEnd]
	for _, forbidden := range []string{
		"dist/*.tar.gz",
		"dist/*.zip",
		"dist/*.dmg",
		"dist/*.exe",
		"dist/*.sh",
		"dist/*.ps1",
		"dist/agentdock-component-catalog.json",
	} {
		if strings.Contains(stageRelease, forbidden) {
			t.Fatalf("GitHub Release must upload the curated user-facing subset instead of %q", forbidden)
		}
	}

	if strings.Contains(workflow, "type=raw,value=latest") ||
		strings.Contains(workflow, "type=raw,value=dev-latest") ||
		strings.Contains(workflow, "type=raw,value=browser-latest") {
		t.Fatal("container build/push must not move mutable aliases before versioned registry images pass verification")
	}
	if strings.Contains(workflow, "releases/tags/$RELEASE_TAG\" --jq .draft 2>/dev/null || true") {
		t.Fatal("published-release immutability check must fail closed on API, auth, and network errors")
	}
	if !strings.Contains(workflow, `if [[ "$PRERELEASE" != "true" && -n "$LEGACY_GITHUB_LATEST_TAG" ]]; then`) {
		t.Fatal("stable publication must preserve the configured legacy GitHub Latest bridge")
	}
	if !strings.Contains(workflow, `elif [[ "$RELEASE_TAG" == "v1.0.0" && -z "$LEGACY_GITHUB_LATEST_TAG" ]]; then`) {
		t.Fatal("v1.0.0 publication must fail closed when the legacy GitHub Latest bridge is not configured")
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

func TestWindowsReleaseTestsReadMachineVersionMetadata(t *testing.T) {
	for _, name := range []string{
		"test-windows-legacy-online-migration.ps1",
		"test-windows-release-backcompat.ps1",
	} {
		data, err := os.ReadFile(name)
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		script := strings.ReplaceAll(string(data), "\r\n", "\n")
		if !strings.Contains(script, "version --json") {
			t.Fatalf("%s must read the complete SemVer from machine-readable version metadata", name)
		}
		if strings.Contains(script, "^AgentDock v(?<version>[0-9]+\\.[0-9]+\\.[0-9]+)") {
			t.Fatalf("%s must not truncate prerelease versions to the numeric SemVer core", name)
		}
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

func TestR2RetentionMaintenanceWorkflowIsFailClosed(t *testing.T) {
	workflow := readWorkflow(t, "r2-retention.yml")
	for _, want := range []string{
		"workflow_dispatch:",
		"expected_current_tag:",
		"apply:",
		"group: release-publication",
		"cancel-in-progress: false",
		"ref: main",
		"test \"$(go run ./tools/release release-kind \"$EXPECTED_CURRENT_TAG\")\" = \"stable\"",
		`test "$current_tag" = "$EXPECTED_CURRENT_TAG"`,
		"go run ./tools/release retention-plan",
		"if: ${{ inputs.apply }}",
		`aws s3 rm "s3://$R2_BUCKET/releases/$tag/"`,
		`jq -e '.delete_tags | length == 0'`,
		`test "$(jq -r '.tag_name // empty' "$RUNNER_TEMP/public-latest.json")" = "$EXPECTED_CURRENT_TAG"`,
	} {
		if !strings.Contains(workflow, want) {
			t.Fatalf("R2 retention maintenance workflow is missing fail-closed contract %q", want)
		}
	}
}
