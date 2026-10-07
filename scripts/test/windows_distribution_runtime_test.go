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
		MinimumVersion        string `json:"minimum_version"`
		PackageName           string `json:"package_name"`
		MainPackageName       string `json:"main_package_name"`
		SingletonPackageName  string `json:"singleton_package_name"`
		DdlmPackageNamePrefix string `json:"ddlm_package_name_prefix"`
		Release               string `json:"release"`
		Artifacts             map[string]struct {
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
		metadata.WindowsApp.PackageName != "Microsoft.WindowsAppRuntime.2" ||
		metadata.WindowsApp.MainPackageName != "MicrosoftCorporationII.WinAppRuntime.Main.2" ||
		metadata.WindowsApp.SingletonPackageName != "MicrosoftCorporationII.WinAppRuntime.Singleton" ||
		metadata.WindowsApp.DdlmPackageNamePrefix != "Microsoft.WinAppRuntime.DDLM.2." {
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
		`Source: "runtime-prerequisites.ps1"; Flags: dontcopy`,
		`Source: "runtime-bootstrap-probe.ps1"; Flags: dontcopy`,
		`Source: "runtime-dependencies.json"; Flags: dontcopy`,
	} {
		if !strings.Contains(setup, want) {
			t.Fatalf("Setup must embed runtime prerequisite metadata/script; missing %q", want)
		}
	}
	runtimeExec := strings.Index(code, "Exec(RuntimePowerShellPath, RuntimeParameters")
	generationExec := strings.Index(code, "Exec(PowerShellPath, Parameters")
	if runtimeExec < 0 || generationExec < 0 || runtimeExec >= generationExec {
		t.Fatal("Microsoft Runtime prerequisite must complete before install.ps1 can activate an AgentDock generation")
	}
	if !strings.Contains(code, "RuntimeInstallFailed") ||
		!strings.Contains(code, "RuntimeResultFilePath") ||
		!strings.Contains(code, "RuntimePowerShellPath := ExpandConstant('{sysnative}\\WindowsPowerShell\\v1.0\\powershell.exe')") ||
		!strings.Contains(code, "Exec(RuntimePowerShellPath, RuntimeParameters") ||
		!strings.Contains(code, "ExtractTemporaryFile('runtime-prerequisites.ps1')") ||
		!strings.Contains(code, "ExtractTemporaryFile('runtime-bootstrap-probe.ps1')") ||
		!strings.Contains(code, "' -PayloadArchivePath ' + QuoteArgument(OfflineArchivePath)") {
		t.Fatal("Setup must surface a structured Runtime bootstrap failure and pass the candidate payload to its canary")
	}
}

func TestWindowsRuntimeBootstrapScriptsRemainASCIIForPowerShell51(t *testing.T) {
	for _, rel := range []string{
		filepath.Join("packaging", "windows", "ensure-windows-runtimes.ps1"),
		filepath.Join("packaging", "windows", "runtime-prerequisites.ps1"),
		filepath.Join("packaging", "windows", "runtime-bootstrap-probe.ps1"),
		filepath.Join("scripts", "test", "test-windows-runtime-registration.ps1"),
	} {
		data, err := os.ReadFile(filepath.Join("..", "..", rel))
		if err != nil {
			t.Fatalf("read %s: %v", rel, err)
		}
		for i, b := range data {
			if b >= 0x80 {
				t.Fatalf("%s must remain ASCII for Windows PowerShell 5.1; non-ASCII byte at offset %d", rel, i)
			}
		}
	}
}

func TestWindowsRuntimeBootstrapVerifiesPlatformTrust(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "packaging", "windows", "ensure-windows-runtimes.ps1"))
	if err != nil {
		t.Fatalf("read Runtime bootstrap: %v", err)
	}
	helperData, err := os.ReadFile(filepath.Join("..", "..", "packaging", "windows", "runtime-prerequisites.ps1"))
	if err != nil {
		t.Fatalf("read Runtime prerequisite helper: %v", err)
	}
	probeData, err := os.ReadFile(filepath.Join("..", "..", "packaging", "windows", "runtime-bootstrap-probe.ps1"))
	if err != nil {
		t.Fatalf("read Runtime bootstrap probe helper: %v", err)
	}
	shimData, err := os.ReadFile(filepath.Join("..", "..", "cmd", "agentdock-shim", "main_windows.go"))
	if err != nil {
		t.Fatalf("read Windows shim probe host: %v", err)
	}
	bootstrap := string(data)
	helper := string(helperData)
	probeHelper := string(probeData)
	shim := string(shimData)
	if !strings.Contains(bootstrap, ". $prerequisiteLibraryPath") ||
		!strings.Contains(bootstrap, ". $bootstrapProbeLibraryPath") ||
		!strings.Contains(bootstrap, "runtime-prerequisites.ps1") ||
		!strings.Contains(bootstrap, "runtime-bootstrap-probe.ps1") {
		t.Fatal("Runtime bootstrap must load both side-effect-free helpers from its own directory")
	}
	script := bootstrap + "\n" + helper + "\n" + probeHelper
	for _, want := range []string{
		"Get-AuthenticodeSignature",
		"SignatureStatus]::Valid",
		"Microsoft Corporation",
		"aka.ms",
		"windowsappruntimeinstall-$assetArchitecture.exe",
		"-TargetArchitecture $TargetArchitecture",
		"Get-WindowsAppRuntimeProbePayload",
		"agentdock-shim.exe",
		"'--windows-app-runtime-probe'",
		"Microsoft.WindowsAppRuntime.Bootstrap.dll",
		"Get-WindowsAppRuntimeInstallerUri -RuntimeVersion $repairVersion",
		"-Artifacts $metadata.windows_app_runtime.artifacts",
		"-PinnedVersion $windowsAppRelease",
		"coherent-package-diagnostic=",
		"Microsoft.WindowsDesktop.App",
		"InstalledVersions\\$dotnetArchitecture",
		"RegistryView]::Registry64",
		"RegistryView]::Registry32",
		"-TargetArchitecture $Architecture",
		"Start-Process -FilePath $Path -ArgumentList $Arguments -Verb RunAs -Wait -PassThru",
		"Start-Process -FilePath $Path -ArgumentList $Arguments -Wait -PassThru",
	} {
		if !strings.Contains(script, want) {
			t.Fatalf("Runtime bootstrap trust/detection contract missing %q", want)
		}
	}

	for _, want := range []string{
		`windowsAppRuntimeProbeFlag = "--windows-app-runtime-probe"`,
		`windows.LoadDLL(bootstrapDLL)`,
		`FindProc("MddBootstrapInitialize2")`,
		`FindProc("MddBootstrapShutdown")`,
		`packageVersion := values[0]<<48 | values[1]<<32 | values[2]<<16 | values[3]`,
	} {
		if !strings.Contains(shim, want) {
			t.Fatalf("Windows shim Runtime probe contract missing %q", want)
		}
	}
	if strings.Contains(probeHelper, "Add-Type -TypeDefinition") || strings.Contains(probeHelper, "csc.exe") {
		t.Fatal("Runtime bootstrap probe must not compile C# at install time")
	}
	if strings.Contains(script, "Get-AppxPackage -AllUsers") || strings.Contains(script, "-IncludeAllUsers") {
		t.Fatal("Windows App Runtime repair must not let other Windows users influence the current-user version decision")
	}
	verifyPublisher := strings.Index(shim, "authenticode.VerifyMicrosoftFile")
	loadBootstrap := strings.Index(shim, "windows.LoadDLL(bootstrapDLL)")
	if verifyPublisher < 0 || loadBootstrap < 0 || verifyPublisher >= loadBootstrap {
		t.Fatal("Windows shim must verify Microsoft's Authenticode publisher before loading the bootstrap DLL")
	}

	if !strings.Contains(script, "Install-Dependency -Uri $dotnetUri -Arguments @('/install', '/quiet', '/norestart') -Dependency $activeDependency -RequireElevation") {
		t.Fatal(".NET machine runtime install must keep its elevation boundary")
	}
	if strings.Contains(script, "Install-Dependency -Uri $windowsAppUri -Arguments @('--quiet') -Dependency $activeDependency -RequireElevation") {
		t.Fatal("Windows App Runtime must install in the interactive user context instead of an alternate elevated identity")
	}
	installerCall := strings.Index(bootstrap, "Install-Dependency -Uri $windowsAppUri -Arguments @('--quiet') -Dependency $activeDependency")
	firstProbe := strings.Index(bootstrap, "$probe = Test-WindowsAppRuntimeBootstrap")
	secondProbe := strings.Index(bootstrap, "$probeAfterInstall = Test-WindowsAppRuntimeBootstrap")
	diagnosticPackages := strings.Index(bootstrap, "$coherentPackages = Test-WindowsAppRuntime")
	if firstProbe < 0 || installerCall < 0 || secondProbe < 0 || diagnosticPackages < 0 ||
		firstProbe >= installerCall || installerCall >= secondProbe || secondProbe >= diagnosticPackages {
		t.Fatal("Windows App Runtime must probe real bootstrap capability first, install only on failure, then use package enumeration only as post-failure diagnostics")
	}
	if strings.Contains(script, "--repair") || strings.Contains(script, "--force") {
		t.Fatal("Windows App Runtime bootstrap must not redeploy or force-close users of an otherwise healthy Framework package")
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
		"'control-panel/Microsoft.WindowsAppRuntime.Bootstrap.dll'",
		"'wsl-helper/manifest.json'",
		"'wsl-helper/agentdock-wsl-helper-linux-amd64'",
		"'wsl-helper/agentdock-wsl-helper-linux-arm64'",
	} {
		if !strings.Contains(string(builder), want) {
			t.Fatalf("Windows first-party payload must keep WSL helper; missing %q", want)
		}
	}
}
