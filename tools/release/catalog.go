package main

import (
	"fmt"
	"io"
	"os"
	"strings"
)

// Artifact 描述一次正式 Release 必须或可选发布的跨平台资产。
// 平台打包仍使用 codesign / notarytool / Inno；版本、清单和 checksum 规则集中在这里。
type Artifact struct {
	Name           string `json:"name"`
	Kind           string `json:"kind"`
	Platform       string `json:"platform,omitempty"`
	Arch           string `json:"arch,omitempty"`
	Required       bool   `json:"required"`
	PublicContract bool   `json:"public_contract"`
}

func verifyDist(dir string, stdout io.Writer) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	present := map[string]bool{}
	for _, entry := range entries {
		if !entry.IsDir() {
			present[entry.Name()] = true
		}
	}
	var missing []string
	for _, artifact := range ReleaseCatalog() {
		if !artifact.Required {
			continue
		}
		if !present[artifact.Name] {
			missing = append(missing, artifact.Name)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("dist missing required artifacts: %s", strings.Join(missing, ", "))
	}
	required := 0
	for _, artifact := range ReleaseCatalog() {
		if artifact.Required {
			required++
		}
	}
	fmt.Fprintf(stdout, "release dist verified: %d required artifacts present\n", required)
	return nil
}

func ReleaseCatalog() []Artifact {
	archives := []Artifact{
		{Name: "agentdock_linux_amd64.tar.gz", Kind: "binary-archive", Platform: "linux", Arch: "amd64", Required: true},
		{Name: "agentdock_linux_arm64.tar.gz", Kind: "binary-archive", Platform: "linux", Arch: "arm64", Required: true},
		{Name: "agentdock_darwin_amd64.tar.gz", Kind: "binary-archive", Platform: "darwin", Arch: "amd64", Required: true},
		{Name: "agentdock_darwin_arm64.tar.gz", Kind: "binary-archive", Platform: "darwin", Arch: "arm64", Required: true},
		{Name: "agentdock_windows_amd64.zip", Kind: "binary-archive", Platform: "windows", Arch: "amd64", Required: true},
		{Name: "agentdock_windows_arm64.zip", Kind: "binary-archive", Platform: "windows", Arch: "arm64", Required: true},
		{Name: "AgentDock-macos-universal.dmg", Kind: "disk-image", Platform: "darwin", Arch: "universal", Required: true},
		{Name: "AgentDockSetup-amd64.exe", Kind: "setup", Platform: "windows", Arch: "amd64", Required: true, PublicContract: true},
		{Name: "AgentDockSetup-arm64.exe", Kind: "setup", Platform: "windows", Arch: "arm64", Required: true, PublicContract: true},
	}
	scripts := []Artifact{
		{Name: "install.sh", Kind: "bootstrap", Platform: "unix", PublicContract: true, Required: true},
		{Name: "install.ps1", Kind: "bootstrap", Platform: "windows", PublicContract: true, Required: true},
		{Name: "install-linux-platform.sh", Kind: "runtime-adapter", Platform: "linux", PublicContract: true, Required: true},
		{Name: "install-macos-platform.sh", Kind: "runtime-adapter", Platform: "darwin", PublicContract: true, Required: true},
		{Name: "uninstall-linux.sh", Kind: "runtime-adapter", Platform: "linux", PublicContract: true, Required: true},
		{Name: "uninstall-macos.sh", Kind: "runtime-adapter", Platform: "darwin", PublicContract: true, Required: true},
		{Name: "uninstall-windows.ps1", Kind: "runtime-adapter", Platform: "windows", PublicContract: true, Required: true},
	}
	var catalog []Artifact
	catalog = append(catalog, archives...)
	for _, artifact := range archives {
		catalog = append(catalog, Artifact{
			Name:     artifact.Name + ".sha256",
			Kind:     "checksum",
			Platform: artifact.Platform,
			Arch:     artifact.Arch,
			Required: artifact.Required,
		})
	}
	catalog = append(catalog, scripts...)
	return catalog
}
