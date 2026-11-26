package scripts

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestWindowsUpdateProgressProtocol(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows desktop update progress regression")
	}
	if pwsh, err := exec.LookPath("pwsh"); err == nil {
		ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
		defer cancel()
		script := filepath.Join("..", "..", "desktop", "windows", "shared", "Tests", "UpdateProgressTests.ps1")
		output, err := exec.CommandContext(ctx, pwsh, "-NoProfile", "-File", script).CombinedOutput()
		if err != nil {
			t.Fatalf("update progress regression via pwsh failed: %v\n%s", err, output)
		}
		t.Logf("%s", output)
		return
	}

	dotnet, err := exec.LookPath("dotnet")
	if err != nil {
		t.Skip("PowerShell 7 or .NET SDK is required to compile the shared C# models")
	}

	modelsPath, err := filepath.Abs(filepath.Join("..", "..", "desktop", "windows", "shared", "Models", "RuntimeModels.cs"))
	if err != nil {
		t.Fatalf("resolve RuntimeModels.cs path: %v", err)
	}
	scriptPath := filepath.Join("..", "..", "desktop", "windows", "shared", "Tests", "UpdateProgressTests.ps1")
	scriptData, err := os.ReadFile(scriptPath)
	if err != nil {
		t.Fatalf("read UpdateProgressTests.ps1: %v", err)
	}
	testClass, ok := extractHereString(string(scriptData))
	if !ok {
		t.Fatalf("failed to extract C# test class from %s", scriptPath)
	}

	tempDir := t.TempDir()
	csproj := `<Project Sdk="Microsoft.NET.Sdk">
  <PropertyGroup>
    <OutputType>Exe</OutputType>
    <TargetFramework>net8.0</TargetFramework>
    <Nullable>enable</Nullable>
    <ImplicitUsings>enable</ImplicitUsings>
  </PropertyGroup>
  <ItemGroup>
    <Compile Include="` + modelsPath + `" Link="RuntimeModels.cs" />
  </ItemGroup>
</Project>
`
	program := "namespace AgentDock.ControlPanel;\n" + testClass + "\npublic static class Program { public static void Main() => UpdateProgressRegressionTests.Run(); }\n"
	if err := os.WriteFile(filepath.Join(tempDir, "UpdateProgressTests.csproj"), []byte(csproj), 0o600); err != nil {
		t.Fatalf("write csproj: %v", err)
	}
	if err := os.WriteFile(filepath.Join(tempDir, "Program.cs"), []byte(program), 0o600); err != nil {
		t.Fatalf("write Program.cs: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, dotnet, "run", "--project", filepath.Join(tempDir, "UpdateProgressTests.csproj"), "-v", "q")
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("update progress regression via dotnet failed: %v\n%s", err, output)
	}
	t.Logf("%s", output)
}

func extractHereString(script string) (string, bool) {
	normalized := strings.ReplaceAll(script, "\r\n", "\n")
	startMarker := "$testSource = @'\n"
	start := strings.Index(normalized, startMarker)
	if start < 0 {
		return "", false
	}
	rest := normalized[start+len(startMarker):]
	end := strings.Index(rest, "\n'@")
	if end < 0 {
		return "", false
	}
	return rest[:end], true
}
