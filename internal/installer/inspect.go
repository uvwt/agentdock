package installer

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/uvwt/agentdock/internal/updateengine"
)

// Inspection 是 E2E 和脚本共用的安装结果校验视图。
// 断言逻辑留在 Go 里，Shell/PowerShell 只负责安排环境后调用 inspect。
type Inspection struct {
	StateRoot       string `json:"state_root"`
	HasTransaction  bool   `json:"has_transaction"`
	HasResult       bool   `json:"has_result"`
	State           string `json:"state,omitempty"`
	Version         string `json:"version,omitempty"`
	ActiveVersion   string `json:"active_version,omitempty"`
	LocalMCPURL     string `json:"local_mcp_url,omitempty"`
	PublicURL       string `json:"public_url,omitempty"`
	Healthy         bool   `json:"healthy"`
	ManifestPath    string `json:"manifest_path,omitempty"`
	HasUnixManifest bool   `json:"has_unix_manifest"`
	HasWinManifest  bool   `json:"has_windows_manifest"`
}

func Inspect(stateRoot string) (Inspection, error) {
	store, err := NewStore(stateRoot)
	if err != nil {
		return Inspection{}, err
	}
	inspection := Inspection{StateRoot: store.Root()}
	if _, err := os.Stat(store.TransactionPath()); err == nil {
		inspection.HasTransaction = true
	}
	result, err := store.ReadAuthoritativeResult()
	if err == nil {
		inspection.HasResult = true
		inspection.State = string(result.State)
		inspection.Version = result.Version
		inspection.ActiveVersion = result.ActiveVersion
		inspection.LocalMCPURL = result.LocalMCPURL
		inspection.PublicURL = result.PublicURL
		inspection.Healthy = result.Healthy
	}
	unixPath := filepath.Join(store.Root(), "desktop-runtime.json")
	winPath := filepath.Join(store.Root(), "runtime.json")
	if fileExists(unixPath) {
		inspection.HasUnixManifest = true
		inspection.ManifestPath = unixPath
	}
	if fileExists(winPath) {
		inspection.HasWinManifest = true
		inspection.ManifestPath = winPath
	}
	return inspection, nil
}

func AssertCommitted(inspection Inspection, wantVersion string) error {
	if !inspection.HasResult {
		return errors.New("missing install result")
	}
	if inspection.State != string(updateengine.StateCommitted) {
		return fmt.Errorf("install state is %s, want committed", inspection.State)
	}
	if wantVersion != "" && updateengine.NormalizeVersion(inspection.Version) != updateengine.NormalizeVersion(wantVersion) {
		return fmt.Errorf("installed version is %s, want %s", inspection.Version, wantVersion)
	}
	return nil
}

func AssertManifestPresent(inspection Inspection) error {
	if !inspection.HasUnixManifest && !inspection.HasWinManifest {
		return errors.New("runtime manifest is missing")
	}
	return nil
}

func FormatInspection(inspection Inspection) ([]byte, error) {
	return json.MarshalIndent(inspection, "", "  ")
}

func RequireInspection(stateRoot, wantVersion string) error {
	inspection, err := Inspect(stateRoot)
	if err != nil {
		return err
	}
	if err := AssertCommitted(inspection, wantVersion); err != nil {
		return err
	}
	if strings.TrimSpace(wantVersion) == "" {
		return AssertManifestPresent(inspection)
	}
	return AssertManifestPresent(inspection)
}
