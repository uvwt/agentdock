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
	// MarkHealthy 只给外层 OS adapter 的 commit 使用：adapter 已经完成自己的 health-check，
	// Engine 不重复探测，只把这个已验证事实投影到权威 Result。
	MarkHealthy bool
	// RollbackFailed 只给 abandon 用：外部 rollback 没做完，禁止宣称 rolled_back。
	RollbackFailed bool
	// TransactionID 绑定 commit/abandon 到明确的 install 事务，不能拿上一笔 result.json 冒充成功。
	TransactionID string
}

type Result struct {
	SchemaVersion   int                `json:"schema_version"`
	TransactionID   string             `json:"transaction_id"`
	Platform        string             `json:"platform"`
	Action          Action             `json:"action"`
	State           updateengine.State `json:"state"`
	Phase           Phase              `json:"phase"`
	Version         string             `json:"version"`
	ActiveVersion   string             `json:"active_version,omitempty"`
	FallbackVersion string             `json:"fallback_version,omitempty"`
	LocalMCPURL     string             `json:"local_mcp_url,omitempty"`
	PublicURL       string             `json:"public_url,omitempty"`
	Healthy         bool               `json:"healthy"`
	PrivilegeMode   string             `json:"privilege_mode,omitempty"`
	// TaskName 只在 uninstall Result 里返回：engine 解析后的计划任务名是 Windows
	// adapter 删除任务的单一来源，脚本不得再自行解析 runtime.json 重复判定。
	TaskName    string                `json:"task_name,omitempty"`
	Failure     *updateengine.Failure `json:"failure,omitempty"`
	Warnings    []string              `json:"warnings,omitempty"`
	StartedAt   time.Time             `json:"started_at"`
	CompletedAt time.Time             `json:"completed_at"`
}

type Transaction struct {
	SchemaVersion   int                `json:"schema_version"`
	TransactionID   string             `json:"transaction_id"`
	Platform        string             `json:"platform"`
	Action          Action             `json:"action"`
	SourceVersion   string             `json:"source_version,omitempty"`
	TargetVersion   string             `json:"target_version"`
	ActiveVersion   string             `json:"active_version,omitempty"`
	FallbackVersion string             `json:"fallback_version,omitempty"`
	State           updateengine.State `json:"state"`
	Phase           Phase              `json:"phase"`
	InstallRoot     string             `json:"install_root"`
	RuntimeRoot     string             `json:"runtime_root"`
	// PurgeConfig / PurgeData 是 uninstall 的事务意图。trial 重入必须逐项匹配，
	// 否则同一 transaction 会在第二次请求下执行比首次承诺更强或更弱的清理。
	PurgeConfig         bool                  `json:"purge_config,omitempty"`
	PurgeData           bool                  `json:"purge_data,omitempty"`
	ServiceUser         string                `json:"service_user,omitempty"`
	AgentDockHome       string                `json:"agentdock_home,omitempty"`
	AgentDockDefaultDir string                `json:"agentdock_default_dir,omitempty"`
	StartedAt           time.Time             `json:"started_at"`
	UpdatedAt           time.Time             `json:"updated_at"`
	CompletedAt         *time.Time            `json:"completed_at,omitempty"`
	Failure             *updateengine.Failure `json:"failure,omitempty"`
	Warnings            []string              `json:"warnings,omitempty"`
}

// ensureUninstallIntentMatches 冻结 uninstall trial 的事务意图：retry 的
// install-root 与 purge 标志必须与首次创建时完全一致。任何漂移都要明确拒绝，
// 让操作者用原始参数恢复，而不是在旧事务上执行新的清理语义。
func ensureUninstallIntentMatches(transaction Transaction, request Request) error {
	if !sameInstallPath(transaction.InstallRoot, request.InstallRoot) {
		return fmt.Errorf(
			"uninstall 事务 %s 的 install-root 意图不匹配：事务=%s，本次=%s；请用原始 install-root 恢复该 trial",
			transaction.TransactionID, transaction.InstallRoot, request.InstallRoot)
	}
	if !sameInstallPath(transaction.RuntimeRoot, request.RuntimeRoot) {
		return fmt.Errorf(
			"uninstall 事务 %s 的 runtime-root 意图不匹配：事务=%s，本次=%s；请用原始 runtime-root 恢复该 trial",
			transaction.TransactionID, transaction.RuntimeRoot, request.RuntimeRoot)
	}
	if transaction.PurgeConfig != request.PurgeConfig || transaction.PurgeData != request.PurgeData {
		return fmt.Errorf(
			"uninstall 事务 %s 的清理意图不匹配：事务 purge-config=%t purge-data=%t，本次 purge-config=%t purge-data=%t；请用与首次卸载相同的清理标志重跑",
			transaction.TransactionID,
			transaction.PurgeConfig, transaction.PurgeData,
			request.PurgeConfig, request.PurgeData)
	}
	if transaction.PurgeData {
		if !sameInstallPath(transaction.AgentDockHome, request.AgentDockHome) {
			return fmt.Errorf(
				"uninstall 事务 %s 的 agentdock-home 清理目标不匹配：事务=%s，本次=%s；请用原始清理路径恢复该 trial",
				transaction.TransactionID, transaction.AgentDockHome, request.AgentDockHome)
		}
		if !sameInstallPath(transaction.AgentDockDefaultDir, request.AgentDockDefaultDir) {
			return fmt.Errorf(
				"uninstall 事务 %s 的 agentdock-default-dir 清理目标不匹配：事务=%s，本次=%s；请用原始清理路径恢复该 trial",
				transaction.TransactionID, transaction.AgentDockDefaultDir, request.AgentDockDefaultDir)
		}
	}
	return nil
}

// sameInstallPath 比较安装路径。Windows 文件系统大小写不敏感，其余平台逐字节比较。
func sameInstallPath(left, right string) bool {
	left = filepath.Clean(strings.TrimSpace(left))
	right = filepath.Clean(strings.TrimSpace(right))
	if runtime.GOOS == "windows" {
		return strings.EqualFold(left, right)
	}
	return left == right
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
	request.AgentDockHome = strings.TrimSpace(request.AgentDockHome)
	request.AgentDockDefaultDir = strings.TrimSpace(request.AgentDockDefaultDir)
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
	// purge-data 蕴含 purge-config（CLI 层同样蕴含）；在引擎层归一化，
	// 保证 uninstall 事务意图无论从哪个入口进来都是同一份。
	if request.PurgeData {
		request.PurgeConfig = true
		if err := validatePurgeDataTargets(request); err != nil {
			return Request{}, err
		}
	}
	if request.Version != "" {
		if err := updateengine.ValidateVersion(request.Version); err != nil {
			return Request{}, fmt.Errorf("版本无效：%w", err)
		}
		request.Version = updateengine.NormalizeVersion(request.Version)
	}
	return request, nil
}

// validatePurgeDataTargets 是所有递归删除目标的统一安全边界。
// install/runtime root 同样会进入 os.RemoveAll，不能只保护用户数据目录。
func validatePurgeDataTargets(request Request) error {
	for _, target := range []struct {
		name string
		path string
	}{
		{name: "install-root", path: request.InstallRoot},
		{name: "runtime-root", path: request.RuntimeRoot},
	} {
		if err := validatePurgeDataTarget(target.name, target.path); err != nil {
			return err
		}
	}
	for _, target := range []struct {
		name string
		path string
	}{
		{name: "agentdock-home", path: request.AgentDockHome},
		{name: "agentdock-default-dir", path: request.AgentDockDefaultDir},
	} {
		if err := validatePurgeDataTarget(target.name, target.path, request.InstallRoot, request.RuntimeRoot); err != nil {
			return err
		}
	}
	return nil
}

func validatePurgeDataTarget(name, target string, protected ...string) error {
	target = strings.TrimSpace(target)
	if target == "" {
		return nil
	}
	if !filepath.IsAbs(target) {
		return fmt.Errorf("%s 清理目标必须是绝对路径：%s", name, target)
	}
	clean := filepath.Clean(target)
	root := filepath.VolumeName(clean) + string(filepath.Separator)
	if sameInstallPath(clean, root) {
		return fmt.Errorf("%s 清理目标不能是文件系统根目录：%s", name, clean)
	}
	if isDangerousPurgeDataRoot(clean) {
		return fmt.Errorf("%s 清理目标过于宽泛，拒绝递归删除危险路径：%s", name, clean)
	}
	for _, guarded := range protected {
		guarded = strings.TrimSpace(guarded)
		if guarded == "" {
			continue
		}
		rel, err := filepath.Rel(clean, filepath.Clean(guarded))
		if err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return fmt.Errorf("%s 清理目标不能包含 installer root %s：%s", name, guarded, clean)
		}
	}
	return nil
}

// isDangerousPurgeDataRoot 拒绝把系统顶层目录或整个用户主目录当成
// AgentDock 的 user-data 清理目标。允许的目标应当至少是主目录下的具体
// AgentDock 子目录（例如 ~/.agentdock），而不是 /Users/alice 本身。
func isDangerousPurgeDataRoot(path string) bool {
	root := filepath.VolumeName(path) + string(filepath.Separator)
	rel, err := filepath.Rel(root, path)
	if err != nil || rel == "." {
		return true
	}
	parts := strings.FieldsFunc(rel, func(r rune) bool { return r == '/' || r == '\\' })
	if len(parts) == 1 {
		switch strings.ToLower(parts[0]) {
		case "applications", "bin", "boot", "dev", "etc", "home", "lib", "lib64", "library", "opt", "private", "proc", "program files", "programdata", "root", "run", "sbin", "srv", "system", "sys", "tmp", "users", "usr", "var", "volumes", "windows":
			return true
		}
	}
	if len(parts) == 2 {
		switch strings.ToLower(parts[0]) {
		case "home", "users":
			return true
		}
	}
	return false
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
		SchemaVersion:       SchemaVersion,
		TransactionID:       transactionID,
		Platform:            platform,
		Action:              request.Action,
		SourceVersion:       sourceVersion,
		TargetVersion:       target,
		State:               updateengine.StateStaged,
		Phase:               PhasePrepare,
		InstallRoot:         request.InstallRoot,
		RuntimeRoot:         request.RuntimeRoot,
		PurgeConfig:         request.PurgeConfig,
		PurgeData:           request.PurgeData,
		ServiceUser:         request.ServiceUser,
		AgentDockHome:       request.AgentDockHome,
		AgentDockDefaultDir: request.AgentDockDefaultDir,
		StartedAt:           now,
		UpdatedAt:           now,
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
