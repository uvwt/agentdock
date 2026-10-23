package scripts

import (
	"os/exec"
	"runtime"
	"strings"
	"testing"
)

func TestWindowsCustomProfileIdentifiers(t *testing.T) {
	page := readWindowsNativeFile(t, "winui", "CapabilitiesPage.xaml.cs")
	if !strings.Contains(page, "AcpProfileId.Create(displayName, _settings.AcpProfiles.Select(profile => profile.Id))") {
		t.Fatal("custom profile creation must use the identifier generator validated against the runtime contract")
	}
	if runtime.GOOS != "windows" {
		t.Skip("runs the production C# identifier generator with Windows PowerShell")
	}
	output, err := exec.Command("powershell", "-NoProfile", "-NonInteractive", "-File", "../../desktop/windows/winui/Tests/ProfileIdTests.ps1").CombinedOutput()
	if err != nil {
		t.Fatalf("custom profile identifier regression: %v\n%s", err, output)
	}
}
