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
	TransactionID   string `json:"transaction_id,omitempty"`
	Action          string `json:"action,omitempty"`
	Version         string `json:"version,omitempty"`
	ActiveVersion   string `json:"active_version,omitempty"`
	LocalMCPURL     string `json:"local_mcp_url,omitempty"`
	PublicURL       string `json:"public_url,omitempty"`
	Healthy         bool   `json:"healthy"`
	ManifestPath    string `json:"manifest_path,omitempty"`
	HasUnixManifest bool   `json:"has_unix_manifest"`
	HasWinManifest  bool   `json:"has_windows_manifest"`
	// Windows generation pointer 与 self-update 事务的权威评估。
	// Setup/卸载脚本必须消费这些结构化结论，不得自行解析 active-version.json
	// 或 update/transaction.json 再解释状态。pointer 缺失时是 missing，
	// 存在但读不出来/结构非法时是 invalid，其余透传 pointer 内的 state。
	PointerState             string `json:"pointer_state,omitempty"`
	PointerActiveVersion     string `json:"pointer_active_version,omitempty"`
	PointerFallbackVersion   string `json:"pointer_fallback_version,omitempty"`
	PendingUpdateTransaction bool   `json:"pending_update_transaction"`
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
		inspection.TransactionID = result.TransactionID
		inspection.Action = string(result.Action)
		inspection.Version = result.Version
		inspection.ActiveVersion = result.ActiveVersion
		inspection.LocalMCPURL = result.LocalMCPURL
		inspection.PublicURL = result.PublicURL
		inspection.Healthy = result.Healthy
	}
	// Windows 安装里 install-root 与 runtime-root 是同一个目录，pointer 与
	// update 事务都挂在这个根下；pointer 评估失败不能让整个 inspect 失败，
	// 脚本依赖 pointer_state=missing/invalid 自己决定是否继续。
	updateStore, updateErr := updateengine.NewStore(store.Root())
	if updateErr == nil {
		if active, err := updateStore.ReadActive(); err == nil {
			inspection.PointerState = string(active.State)
			inspection.PointerActiveVersion = active.ActiveVersion
			inspection.PointerFallbackVersion = active.FallbackVersion
		} else if os.IsNotExist(err) {
			inspection.PointerState = "missing"
		} else {
			inspection.PointerState = "invalid"
		}
		if _, err := os.Stat(updateStore.TransactionPath()); err == nil {
			inspection.PendingUpdateTransaction = true
		}
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
