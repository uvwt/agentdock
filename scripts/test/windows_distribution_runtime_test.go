package scripts

import (
	"encoding/json"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type windowsRuntimeMetadata struct {
	SchemaVersion int `json:"schema_version"`
	DotNet        struct {
		MinimumVersion string `json:"minimum_version"`
		InstallVersion string `json:"install_version"`
		Artifacts      map[string]struct {
			URL string `json:"url"`
		} `json:"artifacts"`
	} `json:"dotnet_windows_desktop"`
	WindowsApp struct {
		MinimumVersion string `json:"minimum_version"`
		PackageName    string `json:"package_name"`
		Release        string `json:"release"`
		Artifacts      map[string]struct {
			URL string `json:"url"`
		} `json:"artifacts"`
	} `json:"windows_app_runtime"`
}

func TestWindowsControlPanelUsesFrameworkDependentRuntimes(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "desktop", "windows", "winui", "AgentDock.WinUI.csproj"))
	if err != nil {
		t.Fatalf("read WinUI project: %v", err)
	}
	project := string(data)
	for _, want := range []string{
		"<WindowsPackageType>None</WindowsPackageType>",
		"<WindowsAppSDKSelfContained>false</WindowsAppSDKSelfContained>",
		"<SelfContained>false</SelfContained>",
		`<PackageReference Include="Microsoft.WindowsAppSDK.WinUI" Version="2.1.0" />`,
		`<PackageReference Include="Microsoft.WindowsAppSDK.Runtime" Version="2.1.3" />`,
		`<PackageReference Include="Microsoft.WindowsAppSDK.Foundation" Version="2.0.21" />`,
		`<PackageReference Include="Microsoft.WindowsAppSDK.InteractiveExperiences" Version="2.0.13" />`,
	} {
		if !strings.Contains(project, want) {
			t.Fatalf("framework-dependent WinUI contract missing %q", want)
		}
	}
	for _, forbidden := range []string{
		`<PackageReference Include="Microsoft.WindowsAppSDK" `,
		`<PackageReference Include="Microsoft.WindowsAppSDK.AI"`,
		`<PackageReference Include="Microsoft.WindowsAppSDK.ML"`,
		"<WindowsAppSDKSelfContained>true</WindowsAppSDKSelfContained>",
		"<SelfContained>true</SelfContained>",
	} {
		if strings.Contains(project, forbidden) {
			t.Fatalf("WinUI project must not carry a private target runtime: found %q", forbidden)
		}
	}
}

func TestWindowsRuntimeMetadataUsesPinnedMicrosoftSources(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "packaging", "windows", "runtime-dependencies.json"))
	if err != nil {
		t.Fatalf("read runtime metadata: %v", err)
	}
	var metadata windowsRuntimeMetadata
	if err := json.Unmarshal(data, &metadata); err != nil {
		t.Fatalf("parse runtime metadata: %v", err)
	}
	if metadata.SchemaVersion != 1 {
		t.Fatalf("runtime metadata schema = %d, want 1", metadata.SchemaVersion)
	}
	if metadata.DotNet.MinimumVersion != "8.0.0" || metadata.DotNet.InstallVersion == "" {
		t.Fatalf("unexpected .NET runtime contract: minimum=%q install=%q", metadata.DotNet.MinimumVersion, metadata.DotNet.InstallVersion)
	}
	if metadata.WindowsApp.MinimumVersion != "2.1.3.0" ||
		metadata.WindowsApp.Release != "2.1.3" ||
		metadata.WindowsApp.PackageName != "Microsoft.WindowsAppRuntime.2" {
		t.Fatalf("unexpected Windows App Runtime contract: %+v", metadata.WindowsApp)
	}

	for arch, artifact := range metadata.DotNet.Artifacts {
		u, err := url.Parse(artifact.URL)
		if err != nil {
			t.Fatalf("parse .NET %s URL: %v", arch, err)
		}
		if u.Scheme != "https" || u.Host != "builds.dotnet.microsoft.com" {
			t.Fatalf(".NET %s must use Microsoft's fixed HTTPS distribution host: %s", arch, artifact.URL)
		}
		if !strings.Contains(u.Path, "/WindowsDesktop/"+metadata.DotNet.InstallVersion+"/") {
			t.Fatalf(".NET %s URL must pin install version %s: %s", arch, metadata.DotNet.InstallVersion, artifact.URL)
		}
		if strings.Contains(strings.ToLower(artifact.URL), "latest") {
			t.Fatalf(".NET %s URL must not track latest: %s", arch, artifact.URL)
		}
	}
	for arch, artifact := range metadata.WindowsApp.Artifacts {
		u, err := url.Parse(artifact.URL)
		if err != nil {
			t.Fatalf("parse Windows App Runtime %s URL: %v", arch, err)
		}
		if u.Scheme != "https" || u.Host != "aka.ms" {
			t.Fatalf("Windows App Runtime %s must use Microsoft's documented aka.ms distribution: %s", arch, artifact.URL)
		}
		assetArch := map[string]string{"amd64": "x64", "arm64": "arm64"}[arch]
		wantPath := "/windowsappsdk/2.1/" + metadata.WindowsApp.Release + "/windowsappruntimeinstall-" + assetArch + ".exe"
		if u.Path != wantPath || u.RawQuery != "" || u.Fragment != "" {
			t.Fatalf("Windows App Runtime %s URL must exactly pin release %s: %s", arch, metadata.WindowsApp.Release, artifact.URL)
		}
		if strings.Contains(strings.ToLower(artifact.URL), "latest") ||
			strings.Contains(artifact.URL, "download.nexusdock.co") ||
			strings.Contains(artifact.URL, "uvwt/agentdock") {
			t.Fatalf("Windows App Runtime %s URL must stay on the official fixed upstream: %s", arch, artifact.URL)
		}
	}
}

func TestWindowsSetupBootstrapsRuntimesBeforeGenerationActivation(t *testing.T) {
	setupData, err := os.ReadFile(filepath.Join("..", "..", "packaging", "windows", "AgentDock.iss"))
	if err != nil {
		t.Fatalf("read AgentDock.iss: %v", err)
	}
	codeData, err := os.ReadFile(filepath.Join("..", "..", "packaging", "windows", "includes", "code.iss"))
	if err != nil {
		t.Fatalf("read code.iss: %v", err)
	}
	setup := string(setupData)
	code := string(codeData)

	for _, want := range []string{
		`Source: "ensure-windows-runtimes.ps1"; Flags: dontcopy`,
		`Source: "runtime-dependencies.json"; Flags: dontcopy`,
	} {
		if !strings.Contains(setup, want) {
			t.Fatalf("Setup must embed runtime prerequisite metadata/script; missing %q", want)
		}
	}
	runtimeExec := strings.Index(code, "Exec(PowerShellPath, RuntimeParameters")
	generationExec := strings.Index(code, "Exec(PowerShellPath, Parameters")
	if runtimeExec < 0 || generationExec < 0 || runtimeExec >= generationExec {
		t.Fatal("Microsoft Runtime prerequisite must complete before install.ps1 can activate an AgentDock generation")
	}
	if !strings.Contains(code, "RuntimeInstallFailed") ||
		!strings.Contains(code, "RuntimeResultFilePath") {
		t.Fatal("Setup must surface a structured Runtime bootstrap failure")
	}
}

func TestWindowsRuntimeBootstrapVerifiesPlatformTrust(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "packaging", "windows", "ensure-windows-runtimes.ps1"))
	if err != nil {
		t.Fatalf("read Runtime bootstrap: %v", err)
	}
	script := string(data)
	for _, want := range []string{
		"Get-AuthenticodeSignature",
		"SignatureStatus]::Valid",
		"Microsoft Corporation",
		"aka.ms",
		"windowsappruntimeinstall-$assetArchitecture.exe",
		"-TargetArchitecture $TargetArchitecture",
		"Get-AppxPackage -Name $PackageName",
		"Microsoft.WindowsDesktop.App",
		"InstalledVersions\\$dotnetArchitecture",
		"RegistryView]::Registry64",
		"RegistryView]::Registry32",
		"-TargetArchitecture $Architecture",
		"Start-Process -FilePath $Path -ArgumentList $Arguments -Verb RunAs -Wait -PassThru",
		"after installation",
	} {
		if !strings.Contains(script, want) {
			t.Fatalf("Runtime bootstrap trust/detection contract missing %q", want)
		}
	}
	for _, forbidden := range []string{
		"download.nexusdock.co",
		"releases/latest",
		"cloudflared",
	} {
		if strings.Contains(script, forbidden) {
			t.Fatalf("Runtime bootstrap must not use AgentDock mirrors or unrelated dependencies: found %q", forbidden)
		}
	}
}

func TestWindowsReleaseBuildsStayFrameworkDependentAndKeepWSLHelper(t *testing.T) {
	for _, rel := range []string{
		filepath.Join(".github", "workflows", "windows-installer.yml"),
		filepath.Join(".github", "workflows", "release.yml"),
	} {
		data, err := os.ReadFile(filepath.Join("..", "..", rel))
		if err != nil {
			t.Fatalf("read %s: %v", rel, err)
		}
		workflow := string(data)
		if strings.Contains(workflow, "--self-contained true") {
			t.Fatalf("%s must not force WinUI back to self-contained deployment", rel)
		}
		if !strings.Contains(workflow, "--self-contained false") {
			t.Fatalf("%s must explicitly publish WinUI framework-dependent", rel)
		}
	}

	builder, err := os.ReadFile(filepath.Join("..", "..", "packaging", "windows", "build-windows-setup.ps1"))
	if err != nil {
		t.Fatalf("read Windows Setup builder: %v", err)
	}
	for _, want := range []string{
		"'wsl-helper/manifest.json'",
		"'wsl-helper/agentdock-wsl-helper-linux-amd64'",
		"'wsl-helper/agentdock-wsl-helper-linux-arm64'",
	} {
		if !strings.Contains(string(builder), want) {
			t.Fatalf("Windows first-party payload must keep WSL helper; missing %q", want)
		}
	}
}
