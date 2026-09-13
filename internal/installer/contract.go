package installer

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/uvwt/agentdock/internal/updateengine"
)

const SchemaVersion = 1

const (
	ActionInstall   Action = "install"
	ActionRepair    Action = "repair"
	ActionUninstall Action = "uninstall"
	// ActionCommit 把 defer-commit 留下的 trial 写成 committed。
	// 只有 OS adapter 确认外部状态已落地之后才能调用；Engine 自己不再抢先 commit。
	ActionCommit Action = "commit"
	// ActionAbandon 只改权威事务状态，不代替真实文件/注册表/服务回滚。
	// 调用方必须先完成外部 rollback：成功后写 rolled_back。
	// rollback 自身失败则带 RollbackFailed 写 failed/external_rollback_failed，
	// 这会阻断下一次自动 install；修复 OS adapter 状态后再 abandon 一次（不要带 --rollback-failed）才是恢复入口。
	ActionAbandon Action = "abandon"
)

const (
	// FailureRollbackFailed 是 Engine journal Restore 失败，下一次 install 可以重试 Restore。
	FailureRollbackFailed = "rollback_failed"
	// FailureExternalRollbackFailed 是 OS adapter（Task/Registry/PowerShell 等）回滚失败。
	// Engine journal 没有能力修好外部状态，禁止只凭 Restore 成功就放行下一次 install。
	FailureExternalRollbackFailed = "external_rollback_failed"
	FailureTrialInterrupted       = "trial-interrupted"
	FailureUninstallFailed        = "uninstall_failed"
	FailureAbandoned              = "abandoned"
)

type Action string

type Phase string

const (
	PhasePrepare  Phase = "prepare"
	PhaseVerify   Phase = "verify"
	PhaseStage    Phase = "stage"
	PhaseActivate Phase = "activate"
	PhaseStart    Phase = "start"
	PhaseHealth   Phase = "health"
	PhaseSkills   Phase = "skills"
	PhaseTunnel   Phase = "tunnel"
	PhaseCommit   Phase = "commit"
	PhaseRollback Phase = "rollback"
)

// OptionalString 把“未指定”和“空值”分开。
// 未指定：保留已有凭据，缺失且首次开启公网认证时才生成。
// 已指定：必须使用该值，空字符串非法。
type OptionalString struct {
	Set   bool
	Value string
}

func Specified(value string) OptionalString {
	return OptionalString{Set: true, Value: value}
}

type Request struct {
	Action Action

	InstallRoot        string
	RuntimeRoot        string
	RuntimeRootLiteral string
	PayloadDir         string
	BinaryPath         string
	LiveBinary         string
	SkillBundle        string
	Version            string
	Channel            string

	Host     string
	Port     int
	LogLevel string

	TunnelMode      string
	ServerURL       string
	TunnelToken     string
	TunnelTokenFile string
	CloudflaredPath string

	ServiceName     string
	ServiceUser     string
	ServiceGroup    string
	ServiceManager  string
	DataDir         string
	SystemdDir      string
	OpenRCDir       string
	LaunchAgentsDir string

	AuthToken        OptionalString
	OAuthPassword    OptionalString
	OAuthTokenSecret OptionalString
	RotateOAuth      bool

	RegisterService bool
	StartService    bool
	NonInteractive  bool

	PurgeConfig bool
	PurgeData   bool

	PrivilegeMode               string
	AgentDockHome               string
	AgentDockDefaultDir         string
	TaskName                    string
	StartupValueName            string
	TrayStartupValueName        string
	CloudflaredStartupValueName string

	SkipHealth      bool
	SkipSkills      bool
	SkipServiceUser bool
	NoPrivilege     bool

	// DeferCommit 让 install/repair 在 verify 完成后停在 trial。
	// Windows 脚本还要写 HKCU/Task/tray；那些完成之前不能出现 committed。
	DeferCommit bool
	// RollbackFailed 只给 abandon 用：外部 rollback 没做完，禁止宣称 rolled_back。
	RollbackFailed bool
	// TransactionID 绑定 commit/abandon 到明确的 install 事务，不能拿上一笔 result.json 冒充成功。
	TransactionID string
}

type Result struct {
	SchemaVersion   int                   `json:"schema_version"`
	TransactionID   string                `json:"transaction_id"`
	Platform        string                `json:"platform"`
	Action          Action                `json:"action"`
	State           updateengine.State    `json:"state"`
	Phase           Phase                 `json:"phase"`
	Version         string                `json:"version"`
	ActiveVersion   string                `json:"active_version,omitempty"`
	FallbackVersion string                `json:"fallback_version,omitempty"`
	LocalMCPURL     string                `json:"local_mcp_url,omitempty"`
	PublicURL       string                `json:"public_url,omitempty"`
	Healthy         bool                  `json:"healthy"`
	PrivilegeMode   string                `json:"privilege_mode,omitempty"`
	Failure         *updateengine.Failure `json:"failure,omitempty"`
	Warnings        []string              `json:"warnings,omitempty"`
	StartedAt       time.Time             `json:"started_at"`
	CompletedAt     time.Time             `json:"completed_at"`
}

type Transaction struct {
	SchemaVersion   int                   `json:"schema_version"`
	TransactionID   string                `json:"transaction_id"`
	Platform        string                `json:"platform"`
	Action          Action                `json:"action"`
	SourceVersion   string                `json:"source_version,omitempty"`
	TargetVersion   string                `json:"target_version"`
	ActiveVersion   string                `json:"active_version,omitempty"`
	FallbackVersion string                `json:"fallback_version,omitempty"`
	State           updateengine.State    `json:"state"`
	Phase           Phase                 `json:"phase"`
	InstallRoot     string                `json:"install_root"`
	RuntimeRoot     string                `json:"runtime_root"`
	StartedAt       time.Time             `json:"started_at"`
	UpdatedAt       time.Time             `json:"updated_at"`
	CompletedAt     *time.Time            `json:"completed_at,omitempty"`
	Failure         *updateengine.Failure `json:"failure,omitempty"`
	Warnings        []string              `json:"warnings,omitempty"`
}

func (request Request) StateRoot() string {
	if strings.TrimSpace(request.RuntimeRoot) != "" {
		return request.RuntimeRoot
	}
	return request.InstallRoot
}

func normalizeRequest(request Request) (Request, error) {
	if request.Action == "" {
		request.Action = ActionInstall
	}
	switch request.Action {
	case ActionInstall, ActionRepair, ActionUninstall, ActionAbandon, ActionCommit:
	default:
		return Request{}, fmt.Errorf("不支持的安装动作：%s", request.Action)
	}
	request.InstallRoot = strings.TrimSpace(request.InstallRoot)
	request.RuntimeRoot = strings.TrimSpace(request.RuntimeRoot)
	if request.InstallRoot == "" {
		return Request{}, errors.New("install-root 不能为空")
	}
	if request.RuntimeRoot == "" {
		request.RuntimeRoot = request.InstallRoot
	}
	if request.Port != 0 && (request.Port < 1 || request.Port > 65535) {
		return Request{}, errors.New("端口必须是 1-65535")
	}
	// LogLevel / TunnelMode 空字符串表示未指定，activate 时保留已有 env。
	// 不能在这里填 info/none，否则 repair 会把公网模式和日志级别重置掉。
	switch request.TunnelMode {
	case "", "none", "quick", "named":
	default:
		return Request{}, fmt.Errorf("不支持的 Tunnel 模式：%s", request.TunnelMode)
	}
	if request.AuthToken.Set && strings.TrimSpace(request.AuthToken.Value) == "" {
		return Request{}, errors.New("auth-token 已指定时不能为空；未指定请省略该标志以保留或按需生成")
	}
	if request.OAuthPassword.Set && strings.TrimSpace(request.OAuthPassword.Value) == "" {
		return Request{}, errors.New("oauth-password 已指定时不能为空")
	}
	if request.OAuthTokenSecret.Set && strings.TrimSpace(request.OAuthTokenSecret.Value) == "" {
		return Request{}, errors.New("oauth-token-secret 已指定时不能为空")
	}
	if request.ServiceName == "" {
		request.ServiceName = "agentdock"
	}
	if request.ServiceManager == "" {
		request.ServiceManager = "auto"
	}
	if request.Channel == "" {
		request.Channel = "official"
	}
	if request.Version != "" {
		request.Version = updateengine.NormalizeVersion(request.Version)
	}
	return request, nil
}

func newTransaction(request Request, platform, sourceVersion string) (Transaction, error) {
	transactionID, err := newTransactionID()
	if err != nil {
		return Transaction{}, err
	}
	now := time.Now().UTC()
	target := strings.TrimSpace(request.Version)
	if target == "" && request.Action != ActionUninstall {
		target = "unknown"
	}
	return Transaction{
		SchemaVersion: SchemaVersion,
		TransactionID: transactionID,
		Platform:      platform,
		Action:        request.Action,
		SourceVersion: sourceVersion,
		TargetVersion: target,
		State:         updateengine.StateStaged,
		Phase:         PhasePrepare,
		InstallRoot:   request.InstallRoot,
		RuntimeRoot:   request.RuntimeRoot,
		StartedAt:     now,
		UpdatedAt:     now,
	}, nil
}

func newTransactionID() (string, error) {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", fmt.Errorf("generate install transaction id: %w", err)
	}
	return hex.EncodeToString(raw[:]), nil
}

func currentPlatform() string {
	return runtime.GOOS
}

func healthURL(host string, port int) string {
	host = strings.TrimSpace(host)
	switch host {
	case "", "0.0.0.0", "::", "[::]":
		host = "127.0.0.1"
	}
	if strings.Contains(host, ":") && !strings.HasPrefix(host, "[") {
		host = "[" + host + "]"
	}
	return fmt.Sprintf("http://%s:%d/healthz", host, port)
}

func localMCPURL(host string, port int) string {
	return strings.TrimSuffix(healthURL(host, port), "/healthz") + "/mcp"
}

func absPath(path string) (string, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return "", errors.New("path is empty")
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	return filepath.Clean(absolute), nil
}
