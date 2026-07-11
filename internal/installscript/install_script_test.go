package installscript

import (
	"os"
	"strings"
	"testing"
)

func TestInstallLinuxWritesExplicitNexusDockToken(t *testing.T) {
	data, err := os.ReadFile("../../scripts/install-linux.sh")
	if err != nil {
		t.Fatalf("read install-linux.sh: %v", err)
	}
	script := string(data)
	checks := []string{
		"local nexus_token=\"$7\"",
		"printf 'AGENTDOCK_NEXUS_TOKEN=%s\\n' \"$nexus_token\"",
		"NexusDock API 是否需要 token？",
		"nexus_token=\"$(prompt_secret 'NexusDock token')\"",
	}
	for _, want := range checks {
		if !strings.Contains(script, want) {
			t.Fatalf("install-linux.sh missing NexusDock token handling: %s", want)
		}
	}
	for _, removed := range []string{"AGENTDOCK_NEXUS_DEVICE_NAME", "AGENTDOCK_NEXUS_HEARTBEAT_SECONDS", "Nexus 设备名"} {
		if strings.Contains(script, removed) {
			t.Fatalf("install-linux.sh still contains removed device-agent config %q", removed)
		}
	}
}

func TestInstallWindowsUsesChecksumsDPAPIAndCurrentUserStartup(t *testing.T) {
	data, err := os.ReadFile("../../scripts/install-windows.ps1")
	if err != nil {
		t.Fatalf("read install-windows.ps1: %v", err)
	}
	script := string(data)
	for _, want := range []string{
		"agentdock_windows_$architecture.zip",
		"Get-FileHash -LiteralPath $archive -Algorithm SHA256",
		"Stop-AgentDockForUpgrade -BinaryPath $destinationBinary",
		"Get-Process -Name 'agentdock'",
		"Copy-Item -LiteralPath $destinationBinary -Destination $binaryBackup -Force",
		"Install-AgentDockBinary -SourceBinary $sourceBinary -DestinationBinary $destinationBinary",
		"Test-Path -LiteralPath $tokenPath -PathType Leaf",
		"Copy-Item -LiteralPath $binaryBackup -Destination $destinationBinary -Force",
		"DataProtectionScope]::CurrentUser",
		"HKCU:\\Software\\Microsoft\\Windows\\CurrentVersion\\Run",
		"New-ItemProperty -Path $runKey -Name $runValueName",
		"Start-AgentDockLauncher -LauncherPath $launcherPath",
		"http://127.0.0.1:$Port/healthz",
	} {
		if !strings.Contains(script, want) {
			t.Fatalf("install-windows.ps1 missing %q", want)
		}
	}
	stopCall := strings.Index(script, "Stop-AgentDockForUpgrade -BinaryPath $destinationBinary")
	replaceCall := strings.Index(script, "Install-AgentDockBinary -SourceBinary $sourceBinary -DestinationBinary $destinationBinary")
	if stopCall < 0 || replaceCall < 0 || stopCall > replaceCall {
		t.Fatal("install-windows.ps1 must stop the running instance before replacing agentdock.exe")
	}
	backupCall := strings.Index(script, "Copy-Item -LiteralPath $destinationBinary -Destination $binaryBackup -Force")
	if backupCall < stopCall || backupCall > replaceCall {
		t.Fatal("install-windows.ps1 must back up the stopped binary before replacement")
	}

	const securityAssemblyLoad = "Add-Type -AssemblyName System.Security"
	if got := strings.Count(script, securityAssemblyLoad); got != 2 {
		t.Fatalf("install-windows.ps1 must load System.Security in the installer and generated launcher; got %d occurrences", got)
	}
	if strings.Contains(script, "RunLevel Highest") {
		t.Fatal("Windows installer should not require elevated startup")
	}
	for _, forbidden := range []string{
		"Set-PrivateAcl",
		"Get-Acl",
		"Set-Acl",
		"icacls.exe",
		"$icaclsArguments",
		"$AclSelfTest",
		"SetSecurityDescriptorSddlForm(",
		"$sddl",
		"Register-ScheduledTask",
		"Start-ScheduledTask",
		"Stop-ScheduledTask",
		"Get-ScheduledTask",
		"Unregister-ScheduledTask",
	} {
		if strings.Contains(script, forbidden) {
			t.Fatalf("install-windows.ps1 still contains removed privileged startup or ACL code %q", forbidden)
		}
	}
	for _, incompatible := range []string{
		"RandomNumberGenerator]::Fill",
		"Convert]::ToHexString",
		`Replace(\"`,
	} {
		if strings.Contains(script, incompatible) {
			t.Fatalf("install-windows.ps1 contains Windows PowerShell 5.1 incompatible syntax %q", incompatible)
		}
	}
}
