package updateengine

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
)

const (
	StableCoreShimName    = "agentdock.exe"
	StableTrayShimName    = "agentdock-tray.exe"
	GenerationCoreName    = "agentdock-core.exe"
	GenerationTrayName    = "agentdock-tray.exe"
	GenerationArbiterName = "agentdock-arbiter.exe"
)

type WindowsLayout struct {
	Root string
}

func NewWindowsLayout(root string) (WindowsLayout, error) {
	root = strings.TrimSpace(root)
	if root == "" {
		return WindowsLayout{}, errors.New("Windows update layout root is required")
	}
	absolute, err := filepath.Abs(root)
	if err != nil {
		return WindowsLayout{}, err
	}
	return WindowsLayout{Root: filepath.Clean(absolute)}, nil
}

func (layout WindowsLayout) BinDir() string      { return filepath.Join(layout.Root, "bin") }
func (layout WindowsLayout) VersionsDir() string { return filepath.Join(layout.Root, "versions") }
func (layout WindowsLayout) CoreShim() string {
	return filepath.Join(layout.BinDir(), StableCoreShimName)
}
func (layout WindowsLayout) TrayShim() string {
	return filepath.Join(layout.BinDir(), StableTrayShimName)
}
func (layout WindowsLayout) GenerationDir(version string) string {
	return filepath.Join(layout.VersionsDir(), NormalizeVersion(version))
}
func (layout WindowsLayout) GenerationCore(version string) string {
	return filepath.Join(layout.GenerationDir(version), GenerationCoreName)
}
func (layout WindowsLayout) GenerationTray(version string) string {
	return filepath.Join(layout.GenerationDir(version), GenerationTrayName)
}
func (layout WindowsLayout) GenerationArbiter(version string) string {
	return filepath.Join(layout.GenerationDir(version), GenerationArbiterName)
}
func (layout WindowsLayout) GenerationSkills(version string) string {
	return filepath.Join(layout.GenerationDir(version), "core-skills")
}

func (layout WindowsLayout) IsGenerationBinary(path string) bool {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return false
	}
	relative, err := filepath.Rel(layout.VersionsDir(), absolute)
	if err != nil || relative == "." || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return false
	}
	parts := strings.Split(filepath.Clean(relative), string(filepath.Separator))
	if len(parts) != 2 {
		return false
	}
	name := strings.ToLower(parts[1])
	return name == GenerationCoreName || name == GenerationTrayName || name == GenerationArbiterName
}

func (layout WindowsLayout) EnsureBase() error {
	for _, directory := range []string{layout.BinDir(), layout.VersionsDir(), filepath.Join(layout.Root, "update", "results")} {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			return err
		}
	}
	return nil
}
