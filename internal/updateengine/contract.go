package updateengine

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode"
)

const SchemaVersion = 1

type State string

type Phase string

const (
	StateStaged      State = "staged"
	StateTrial       State = "trial"
	StateCommitted   State = "committed"
	StateRollingBack State = "rolling_back"
	StateRolledBack  State = "rolled_back"
	StateFailed      State = "failed"

	PhaseStage    Phase = "stage"
	PhaseVerify   Phase = "verify"
	PhaseActivate Phase = "activate"
	PhaseHealth   Phase = "health"
	PhaseCommit   Phase = "commit"
	PhaseRollback Phase = "rollback"
)

type Failure struct {
	Code    string    `json:"code"`
	Message string    `json:"message"`
	At      time.Time `json:"at"`
}

type WindowsPlan struct {
	InstallRoot        string   `json:"install_root"`
	SourceGeneration   string   `json:"source_generation"`
	TargetGeneration   string   `json:"target_generation"`
	HealthURLs         []string `json:"health_urls,omitempty"`
	TaskName           string   `json:"task_name,omitempty"`
	PrivilegeMode      string   `json:"privilege_mode,omitempty"`
	CoreWasRunning     bool     `json:"core_was_running,omitempty"`
	TrayWasRunning     bool     `json:"tray_was_running,omitempty"`
	TunnelWasRunning   bool     `json:"tunnel_was_running,omitempty"`
	ProgressUIHandoff  bool     `json:"progress_ui_handoff,omitempty"`
	BootstrapMigration bool     `json:"bootstrap_migration,omitempty"`
}

type MacOSPlan struct {
	SourceArbiterPath  string `json:"source_arbiter_path,omitempty"`
	TargetAppPath      string `json:"target_app_path"`
	TrialAppPath       string `json:"trial_app_path"`
	HandoffPath        string `json:"handoff_path,omitempty"`
	ResultPath         string `json:"result_path,omitempty"`
	ServiceStatePath   string `json:"service_state_path,omitempty"`
	HealthURL          string `json:"health_url,omitempty"`
	AppWasRunning      bool   `json:"app_was_running,omitempty"`
	CoreWasEnabled     bool   `json:"core_was_enabled,omitempty"`
	TunnelEnabled      bool   `json:"tunnel_enabled,omitempty"`
	BootstrapMigration bool   `json:"bootstrap_migration,omitempty"`
}

type Transaction struct {
	SchemaVersion   int          `json:"schema_version"`
	TransactionID   string       `json:"transaction_id"`
	Platform        string       `json:"platform"`
	SourceVersion   string       `json:"source_version"`
	TargetVersion   string       `json:"target_version"`
	ActiveVersion   string       `json:"active_version,omitempty"`
	FallbackVersion string       `json:"fallback_version,omitempty"`
	State           State        `json:"state"`
	Phase           Phase        `json:"phase"`
	StartedAt       time.Time    `json:"started_at"`
	UpdatedAt       time.Time    `json:"updated_at"`
	CompletedAt     *time.Time   `json:"completed_at,omitempty"`
	Failure         *Failure     `json:"failure,omitempty"`
	Warnings        []string     `json:"warnings,omitempty"`
	Windows         *WindowsPlan `json:"windows,omitempty"`
	MacOS           *MacOSPlan   `json:"macos,omitempty"`
}

type Result struct {
	SchemaVersion   int       `json:"schema_version"`
	TransactionID   string    `json:"transaction_id"`
	Platform        string    `json:"platform"`
	SourceVersion   string    `json:"source_version"`
	TargetVersion   string    `json:"target_version"`
	ActiveVersion   string    `json:"active_version,omitempty"`
	FallbackVersion string    `json:"fallback_version,omitempty"`
	State           State     `json:"state"`
	CompletedAt     time.Time `json:"completed_at"`
	Failure         *Failure  `json:"failure,omitempty"`
	Warnings        []string  `json:"warnings,omitempty"`
}

type ActiveVersion struct {
	SchemaVersion   int       `json:"schema_version"`
	ActiveVersion   string    `json:"active_version"`
	FallbackVersion string    `json:"fallback_version,omitempty"`
	State           State     `json:"state"`
	TransactionID   string    `json:"transaction_id,omitempty"`
	UpdatedAt       time.Time `json:"updated_at"`
}

func NewTransaction(platform, sourceVersion, targetVersion string) (Transaction, error) {
	transactionID, err := NewTransactionID()
	if err != nil {
		return Transaction{}, err
	}
	now := time.Now().UTC()
	transaction := Transaction{
		SchemaVersion: SchemaVersion,
		TransactionID: transactionID,
		Platform:      strings.TrimSpace(platform),
		SourceVersion: NormalizeVersion(sourceVersion),
		TargetVersion: NormalizeVersion(targetVersion),
		State:         StateStaged,
		Phase:         PhaseStage,
		StartedAt:     now,
		UpdatedAt:     now,
	}
	if transaction.Platform != "windows" && transaction.Platform != "darwin" {
		return Transaction{}, fmt.Errorf("unsupported update transaction platform: %s", transaction.Platform)
	}
	if transaction.SourceVersion == "" || transaction.TargetVersion == "" {
		return Transaction{}, errors.New("update transaction source and target versions are required")
	}
	if err := ValidateVersion(transaction.SourceVersion); err != nil {
		return Transaction{}, fmt.Errorf("invalid source version: %w", err)
	}
	if err := ValidateVersion(transaction.TargetVersion); err != nil {
		return Transaction{}, fmt.Errorf("invalid target version: %w", err)
	}
	return transaction, nil
}

func NewTransactionID() (string, error) {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", fmt.Errorf("generate update transaction id: %w", err)
	}
	return hex.EncodeToString(raw[:]), nil
}

func NormalizeVersion(value string) string {
	value = strings.TrimSpace(value)
	value = strings.TrimPrefix(value, "v")
	if value == "" {
		return ""
	}
	return "v" + value
}

// ValidateVersion keeps version strings safe as a single generation-directory name on all supported platforms.
// NormalizeVersion intentionally remains comparison-only; callers that persist or join a version into a path must validate it.
func ValidateVersion(value string) error {
	normalized := NormalizeVersion(value)
	if normalized == "" {
		return errors.New("version is required")
	}
	segment := strings.TrimPrefix(normalized, "v")
	if strings.ContainsAny(segment, `/\<>:"|?*`) {
		return fmt.Errorf("version contains path-unsafe characters: %q", value)
	}
	for _, r := range segment {
		if unicode.IsControl(r) || unicode.IsSpace(r) {
			return fmt.Errorf("version contains whitespace or control characters: %q", value)
		}
	}
	// Windows normalizes trailing dots/spaces in path components. Spaces are rejected above;
	// reject a trailing dot explicitly so two textual versions cannot alias one directory.
	if strings.HasSuffix(segment, ".") {
		return fmt.Errorf("version cannot end with a dot: %q", value)
	}
	return nil
}

func (transaction Transaction) Validate() error {
	if transaction.SchemaVersion != SchemaVersion {
		return fmt.Errorf("unsupported update transaction schema: %d", transaction.SchemaVersion)
	}
	if strings.TrimSpace(transaction.TransactionID) == "" {
		return errors.New("update transaction id is required")
	}
	if transaction.Platform != "windows" && transaction.Platform != "darwin" {
		return fmt.Errorf("unsupported update transaction platform: %s", transaction.Platform)
	}
	if err := ValidateVersion(transaction.SourceVersion); err != nil {
		return fmt.Errorf("invalid transaction source version: %w", err)
	}
	if err := ValidateVersion(transaction.TargetVersion); err != nil {
		return fmt.Errorf("invalid transaction target version: %w", err)
	}
	if strings.TrimSpace(transaction.ActiveVersion) != "" {
		if err := ValidateVersion(transaction.ActiveVersion); err != nil {
			return fmt.Errorf("invalid transaction active version: %w", err)
		}
	}
	if strings.TrimSpace(transaction.FallbackVersion) != "" {
		if err := ValidateVersion(transaction.FallbackVersion); err != nil {
			return fmt.Errorf("invalid transaction fallback version: %w", err)
		}
	}
	if !validState(transaction.State) {
		return fmt.Errorf("unsupported update transaction state: %s", transaction.State)
	}
	if !validPhase(transaction.Phase) {
		return fmt.Errorf("unsupported update transaction phase: %s", transaction.Phase)
	}
	if transaction.StartedAt.IsZero() || transaction.UpdatedAt.IsZero() {
		return errors.New("update transaction timestamps are required")
	}
	if transaction.Platform == "windows" && transaction.Windows == nil {
		return errors.New("windows update transaction requires windows plan")
	}
	if transaction.Platform == "darwin" && transaction.MacOS == nil {
		return errors.New("macOS update transaction requires macOS plan")
	}
	return nil
}

func (result Result) Validate() error {
	if result.SchemaVersion != SchemaVersion {
		return fmt.Errorf("unsupported update result schema: %d", result.SchemaVersion)
	}
	if strings.TrimSpace(result.TransactionID) == "" {
		return errors.New("update result transaction id is required")
	}
	if result.Platform != "windows" && result.Platform != "darwin" {
		return fmt.Errorf("unsupported update result platform: %s", result.Platform)
	}
	if err := ValidateVersion(result.SourceVersion); err != nil {
		return fmt.Errorf("invalid result source version: %w", err)
	}
	if err := ValidateVersion(result.TargetVersion); err != nil {
		return fmt.Errorf("invalid result target version: %w", err)
	}
	if strings.TrimSpace(result.ActiveVersion) != "" {
		if err := ValidateVersion(result.ActiveVersion); err != nil {
			return fmt.Errorf("invalid result active version: %w", err)
		}
	}
	if strings.TrimSpace(result.FallbackVersion) != "" {
		if err := ValidateVersion(result.FallbackVersion); err != nil {
			return fmt.Errorf("invalid result fallback version: %w", err)
		}
	}
	if result.State != StateCommitted && result.State != StateRolledBack && result.State != StateFailed {
		return fmt.Errorf("update result is not terminal: %s", result.State)
	}
	if result.CompletedAt.IsZero() {
		return errors.New("update result completion timestamp is required")
	}
	return nil
}

func (active ActiveVersion) Validate() error {
	if active.SchemaVersion != SchemaVersion {
		return fmt.Errorf("unsupported active-version schema: %d", active.SchemaVersion)
	}
	if err := ValidateVersion(active.ActiveVersion); err != nil {
		return fmt.Errorf("invalid active version: %w", err)
	}
	if strings.TrimSpace(active.FallbackVersion) != "" {
		if err := ValidateVersion(active.FallbackVersion); err != nil {
			return fmt.Errorf("invalid fallback version: %w", err)
		}
	}
	if active.State != StateCommitted && active.State != StateTrial {
		return fmt.Errorf("unsupported active-version state: %s", active.State)
	}
	if active.State == StateTrial && strings.TrimSpace(active.TransactionID) == "" {
		return errors.New("trial active-version requires transaction id")
	}
	if active.UpdatedAt.IsZero() {
		return errors.New("active-version timestamp is required")
	}
	return nil
}

func validState(state State) bool {
	switch state {
	case StateStaged, StateTrial, StateCommitted, StateRollingBack, StateRolledBack, StateFailed:
		return true
	default:
		return false
	}
}

func validPhase(phase Phase) bool {
	switch phase {
	case PhaseStage, PhaseVerify, PhaseActivate, PhaseHealth, PhaseCommit, PhaseRollback:
		return true
	default:
		return false
	}
}
