package resourcelibrary

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	skills "github.com/uvwt/agentdock/internal/skill"
)

const (
	ActionExportPrepare  = "export_prepare"
	ActionExportUpload   = "export_upload"
	ActionInstallPrepare = "install_prepare"
	ActionInstallCommit  = "install_commit"
	OperationInstall     = "install"
	OperationUpdate      = "update"

	purposeExport  = "export"
	purposeInstall = "install"
	// ChallengeTTL 是用户在 Cloud 上确认后回到设备的短窗口。过期必须重新预检。
	ChallengeTTL = 2 * time.Minute
	maxStaged    = 8
)

// DeviceIdentity 只保留资源库需要的配对身份。Device Token 不得进入响应。
type DeviceIdentity struct {
	Endpoint    string
	NodeID      string
	DeviceToken string
}

type SkillPreview struct {
	Valid         bool
	Name          string
	SourceDigest  string
	ContentDigest string
	Issues        []string
}

type SkillCommit struct {
	Name          string
	ContentDigest string
	Changed       bool
}

type PluginPreview struct {
	Valid         bool
	Name          string
	Version       string
	PackageDigest string
	ReviewToken   string
	Warnings      []string
	Issues        []string
	Format        string
}

type PluginCommit struct {
	Name          string
	Version       string
	PackageDigest string
	Changed       bool
}

// Deps 把网络和原生安装器留在资源库外面。资源库负责确认、摘要和一次性挑战。
type Deps struct {
	Now             func() time.Time
	Identity        func() (DeviceIdentity, error)
	Lookup          func(kind, name string) (string, error)
	SkillValidate   func(context.Context, string, string) (SkillPreview, error)
	SkillInstall    func(context.Context, string, string) (SkillCommit, error)
	PluginValidate  func(context.Context, string) (PluginPreview, error)
	PluginInstall   func(context.Context, string, string) (PluginCommit, error)
	PluginUpdate    func(context.Context, string, string) (PluginCommit, error)
	PluginInstalled func(string) (bool, error)
	Fetch           func(context.Context, string, string, string, string, int64) (string, error)
	Upload          func(context.Context, string, string, string, string, int64) error
}

type Service struct {
	root    string
	deps    Deps
	mu      sync.Mutex
	pending map[string]stagedPackage
}

type stagedPackage struct {
	Purpose       string
	Kind          string
	Name          string
	NodeID        string
	Operation     string
	ArchivePath   string
	ArchiveDigest string
	PackageDigest string
	ReviewToken   string
	Expires       time.Time
	Files         []FileEntry
	Warnings      []string
	Size          int64
}

type controlRequest struct {
	Action        string `json:"action"`
	Kind          string `json:"kind"`
	Name          string `json:"name"`
	NodeID        string `json:"node_id"`
	DownloadURL   string `json:"download_url"`
	ArchiveDigest string `json:"archive_digest"`
	PackageDigest string `json:"package_digest"`
	ReviewToken   string `json:"review_token"`
	Challenge     string `json:"challenge"`
	Operation     string `json:"operation"`
	UploadURL     string `json:"upload_url"`
	DownloadGrant string `json:"download_grant"`
}

func New(root string, deps Deps) (*Service, error) {
	root = strings.TrimSpace(root)
	if root == "" {
		return nil, failed("RESOURCE_LIBRARY_UNAVAILABLE", "validation", "resource library staging directory is required")
	}
	if err := os.MkdirAll(root, 0o700); err != nil {
		return nil, failed("RESOURCE_LIBRARY_UNAVAILABLE", "validation", "resource library staging directory cannot be created")
	}
	info, err := os.Lstat(root)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, failed("RESOURCE_LIBRARY_UNAVAILABLE", "validation", "resource library staging directory is not a regular directory")
	}
	if deps.Now == nil {
		deps.Now = time.Now
	}
	if deps.Fetch == nil {
		deps.Fetch = FetchPackage
	}
	if deps.Upload == nil {
		deps.Upload = UploadPackage
	}
	return &Service{root: root, deps: deps, pending: make(map[string]stagedPackage)}, nil
}

func (s *Service) Handle(ctx context.Context, method string, body []byte) (map[string]any, error) {
	// 过期清扫放在请求处理之后，这样本次提交仍能区分“过期”和“从未签发”。
	defer s.expire()
	if method == "GET" {
		return s.capabilities(), nil
	}
	if method != "POST" {
		return nil, failed("METHOD_NOT_ALLOWED", "not_found", "resource library method is not allowed")
	}
	request, err := decodeControl(body)
	if err != nil {
		return nil, err
	}
	switch request.Action {
	case ActionExportPrepare:
		return s.exportPrepare(request)
	case ActionExportUpload:
		return s.exportUpload(ctx, request)
	case ActionInstallPrepare:
		return s.installPrepare(ctx, request)
	case ActionInstallCommit:
		return s.installCommit(ctx, request)
	default:
		return nil, failed("RESOURCE_LIBRARY_ACTION_UNSUPPORTED", "validation", "resource library action is not supported")
	}
}

func (s *Service) capabilities() map[string]any {
	return map[string]any{
		"ok":                              true,
		"supported":                       true,
		"actions":                         []string{ActionExportPrepare, ActionExportUpload, ActionInstallPrepare, ActionInstallCommit},
		"runtime_request_carries_archive": false,
		"file_transfer":                   "nexus_device_https_client",
		// 导出字节只通过 export_upload 离开设备。控制响应仍然只带 grant，不带 ZIP。
		"stream_upload_available": true,
		"limits": map[string]any{
			"skill_archive_bytes":   SkillArchiveLimit,
			"plugin_archive_bytes":  PluginArchiveLimit,
			"skill_extract_bytes":   SkillExtractLimit,
			"plugin_extract_bytes":  PluginExtractLimit,
			"max_files":             MaxPackageFiles,
			"control_body_bytes":    maxControlBody,
			"challenge_ttl_seconds": int(ChallengeTTL / time.Second),
		},
	}
}

func (s *Service) exportPrepare(request controlRequest) (map[string]any, error) {
	if request.DownloadURL != "" || request.UploadURL != "" || request.DownloadGrant != "" || request.Challenge != "" ||
		request.ArchiveDigest != "" || request.PackageDigest != "" || request.ReviewToken != "" || request.Operation != "" || request.NodeID != "" {
		return nil, failed("RESOURCE_LIBRARY_ACTION_INVALID", "validation", "export_prepare only accepts kind and name")
	}
	kind, name, err := normalizePackageRef(request.Kind, request.Name)
	if err != nil {
		return nil, err
	}
	if s.deps.Lookup == nil {
		return nil, failed("RESOURCE_LIBRARY_UNAVAILABLE", "validation", "managed package lookup is unavailable")
	}
	root, err := s.deps.Lookup(kind, name)
	if err != nil {
		return nil, err
	}
	if err := s.reserveSlot(); err != nil {
		return nil, err
	}
	destination, err := s.stagingPath("export")
	if err != nil {
		return nil, err
	}
	exported, err := ExportDirectory(root, kind, destination)
	if err != nil {
		_ = os.Remove(destination)
		return nil, err
	}
	// download_grant 仍只是本机暂存句柄。ZIP 离开设备的唯一路径是随后的 export_upload。
	grant, err := s.remember(stagedPackage{
		Purpose: purposeExport, Kind: kind, Name: name, Operation: "export",
		ArchivePath: destination, ArchiveDigest: exported.ArchiveDigest, PackageDigest: exported.ContentDigest,
		Expires: s.deps.Now().Add(ChallengeTTL), Files: exported.Files, Warnings: exported.Warnings, Size: exported.Size,
	})
	if err != nil {
		_ = os.Remove(destination)
		return nil, err
	}
	return map[string]any{
		"ok": true, "action": ActionExportPrepare, "kind": kind, "name": name,
		"archive_digest": exported.ArchiveDigest, "content_digest": exported.ContentDigest,
		"size": exported.Size, "files": exported.Files, "warnings": exported.Warnings,
		"download_grant": grant, "download_authorized": true, "expires_at": s.pendingExpiry(grant),
		"transfer": "local_grant_only", "stream_upload_available": true,
	}, nil
}

func (s *Service) exportUpload(ctx context.Context, request controlRequest) (map[string]any, error) {
	if request.Kind != "" || request.Name != "" || request.NodeID != "" || request.DownloadURL != "" ||
		request.ArchiveDigest != "" || request.PackageDigest != "" || request.ReviewToken != "" ||
		request.Challenge != "" || request.Operation != "" {
		return nil, failed("RESOURCE_LIBRARY_ACTION_INVALID", "validation", "export_upload only accepts upload_url and download_grant")
	}
	uploadURL := strings.TrimSpace(request.UploadURL)
	grantID := strings.TrimSpace(request.DownloadGrant)
	if uploadURL == "" || grantID == "" {
		return nil, failed("RESOURCE_LIBRARY_ACTION_INVALID", "validation", "export_upload requires upload_url and download_grant")
	}
	identity, err := s.currentIdentity()
	if err != nil {
		return nil, err
	}
	// 先拒绝其他 Origin，避免未授权地址消耗 grant 或触发上传。
	if _, err := AuthorizeCloudUpload(identity.Endpoint, uploadURL); err != nil {
		return nil, err
	}
	staged, err := s.consumeExport(grantID)
	if err != nil {
		return nil, err
	}
	defer os.Remove(staged.ArchivePath)
	info, err := os.Lstat(staged.ArchivePath)
	if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() || info.Size() != staged.Size || staged.Size <= 0 {
		return nil, failed("ARCHIVE_CHANGED", "validation", "export archive changed before upload")
	}
	digest, err := skills.DigestFile(staged.ArchivePath)
	if err != nil || normalizeDigest(digest) != staged.ArchiveDigest {
		return nil, failed("ARCHIVE_CHANGED", "validation", "export archive digest changed before upload")
	}
	if s.deps.Upload == nil {
		return nil, failed("RESOURCE_LIBRARY_UNAVAILABLE", "validation", "package upload is unavailable")
	}
	if err := s.deps.Upload(ctx, identity.Endpoint, identity.DeviceToken, uploadURL, staged.ArchivePath, staged.Size); err != nil {
		var libraryErr *Error
		if errors.As(err, &libraryErr) {
			return nil, libraryErr
		}
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
			return nil, failed("UPLOAD_TIMEOUT", "validation", "package upload timed out")
		}
		return nil, failed("UPLOAD_FAILED", "validation", "package upload failed")
	}
	return map[string]any{
		"ok": true, "action": ActionExportUpload, "kind": staged.Kind, "name": staged.Name,
		"size": staged.Size, "archive_digest": staged.ArchiveDigest, "content_digest": staged.PackageDigest,
	}, nil
}

func (s *Service) installPrepare(ctx context.Context, request controlRequest) (map[string]any, error) {
	kind, name, err := normalizePackageRef(request.Kind, request.Name)
	if err != nil {
		return nil, err
	}
	identity, err := s.pairedIdentity(request.NodeID)
	if err != nil {
		return nil, err
	}
	archiveDigest := normalizeDigest(request.ArchiveDigest)
	// package_digest 是可选的预期内容摘要。Cloud 不能重算 Skill/Plugin 原生 digest，
	// 所以挑战里绑定的是设备 validate 成功后的实际摘要；调用方提供时期望必须一致。
	expectedPackageDigest := normalizeDigest(request.PackageDigest)
	if archiveDigest == "" || strings.TrimSpace(request.DownloadURL) == "" {
		return nil, failed("RESOURCE_LIBRARY_ACTION_INVALID", "validation", "install_prepare requires download_url and archive_digest")
	}
	if request.Challenge != "" || request.ReviewToken != "" || request.Operation != "" || request.UploadURL != "" || request.DownloadGrant != "" {
		return nil, failed("RESOURCE_LIBRARY_ACTION_INVALID", "validation", "install_prepare cannot carry a challenge, review token, operation, or upload grant")
	}
	// 先做来源判断，避免未授权 URL 进入下载客户端。Fetch 会再判断一次。
	if _, err := AuthorizeCloudDownload(identity.Endpoint, request.DownloadURL); err != nil {
		return nil, err
	}
	if err := s.reserveSlot(); err != nil {
		return nil, err
	}
	destination, err := s.stagingPath("install")
	if err != nil {
		return nil, err
	}
	downloaded, err := s.deps.Fetch(ctx, identity.Endpoint, identity.DeviceToken, request.DownloadURL, destination, archiveLimit(kind))
	if err != nil {
		_ = os.Remove(destination)
		return nil, err
	}
	downloaded = normalizeDigest(downloaded)
	if downloaded != archiveDigest {
		_ = os.Remove(destination)
		return nil, failed("DIGEST_MISMATCH", "validation", "downloaded archive digest does not match the requested digest")
	}
	if err := InspectArchive(destination, kind); err != nil {
		_ = os.Remove(destination)
		return nil, err
	}

	var reviewToken string
	var operation string
	var packageDigest string
	var review map[string]any
	switch kind {
	case KindSkill:
		if s.deps.SkillValidate == nil {
			_ = os.Remove(destination)
			return nil, failed("RESOURCE_LIBRARY_UNAVAILABLE", "validation", "Skill validator is unavailable")
		}
		preview, err := s.deps.SkillValidate(ctx, destination, archiveDigest)
		if err != nil {
			_ = os.Remove(destination)
			return nil, err
		}
		review = map[string]any{
			"valid": preview.Valid, "name": preview.Name, "source_digest": normalizeDigest(preview.SourceDigest),
			"content_digest": normalizeDigest(preview.ContentDigest), "issues": preview.Issues,
		}
		if !preview.Valid {
			_ = os.Remove(destination)
			return map[string]any{"ok": true, "action": ActionInstallPrepare, "valid": false, "kind": kind, "name": name, "review": review}, nil
		}
		if preview.Name != name || normalizeDigest(preview.SourceDigest) != archiveDigest {
			_ = os.Remove(destination)
			return nil, failed("DIGEST_MISMATCH", "validation", "Skill candidate does not match the requested name or digest")
		}
		packageDigest = normalizeDigest(preview.ContentDigest)
		if packageDigest == "" || (expectedPackageDigest != "" && expectedPackageDigest != packageDigest) {
			_ = os.Remove(destination)
			return nil, failed("DIGEST_MISMATCH", "validation", "Skill content digest does not match the expected digest")
		}
		operation = OperationInstall
	default:
		if s.deps.PluginValidate == nil || s.deps.PluginInstalled == nil {
			_ = os.Remove(destination)
			return nil, failed("RESOURCE_LIBRARY_UNAVAILABLE", "validation", "Plugin validator is unavailable")
		}
		preview, err := s.deps.PluginValidate(ctx, destination)
		if err != nil {
			_ = os.Remove(destination)
			return nil, err
		}
		review = map[string]any{
			"valid": preview.Valid, "name": preview.Name, "version": preview.Version,
			"package_digest": normalizeDigest(preview.PackageDigest), "review_token": preview.ReviewToken,
			"warnings": preview.Warnings, "issues": preview.Issues, "format": preview.Format,
		}
		if !preview.Valid {
			_ = os.Remove(destination)
			return map[string]any{"ok": true, "action": ActionInstallPrepare, "valid": false, "kind": kind, "name": name, "review": review}, nil
		}
		if preview.Name != name || strings.TrimSpace(preview.ReviewToken) == "" {
			_ = os.Remove(destination)
			return nil, failed("DIGEST_MISMATCH", "validation", "Plugin candidate does not match the requested name or digest")
		}
		packageDigest = normalizeDigest(preview.PackageDigest)
		if packageDigest == "" || (expectedPackageDigest != "" && expectedPackageDigest != packageDigest) {
			_ = os.Remove(destination)
			return nil, failed("DIGEST_MISMATCH", "validation", "Plugin content digest does not match the expected digest")
		}
		installed, err := s.deps.PluginInstalled(name)
		if err != nil {
			_ = os.Remove(destination)
			return nil, err
		}
		operation = OperationInstall
		if installed {
			operation = OperationUpdate
		}
		reviewToken = preview.ReviewToken
	}

	challenge, err := s.remember(stagedPackage{
		Purpose: purposeInstall, Kind: kind, Name: name, NodeID: identity.NodeID, Operation: operation,
		ArchivePath: destination, ArchiveDigest: archiveDigest, PackageDigest: packageDigest, ReviewToken: reviewToken,
		Expires: s.deps.Now().Add(ChallengeTTL),
	})
	if err != nil {
		_ = os.Remove(destination)
		return nil, err
	}
	return map[string]any{
		"ok": true, "action": ActionInstallPrepare, "valid": true, "kind": kind, "name": name,
		"operation": operation, "archive_digest": archiveDigest, "package_digest": packageDigest,
		"challenge": challenge, "expires_at": s.pendingExpiry(challenge), "review": review,
	}, nil
}

func (s *Service) installCommit(ctx context.Context, request controlRequest) (map[string]any, error) {
	kind, name, err := normalizePackageRef(request.Kind, request.Name)
	if err != nil {
		return nil, err
	}
	identity, err := s.pairedIdentity(request.NodeID)
	if err != nil {
		return nil, err
	}
	if request.DownloadURL != "" || request.UploadURL != "" || request.DownloadGrant != "" {
		return nil, failed("RESOURCE_LIBRARY_ACTION_INVALID", "validation", "install_commit only accepts the prepared confirmation")
	}
	// 先消耗挑战，再读文件。并发的第二次提交会直接失败，不能再安装一次。
	staged, err := s.consumeInstall(request, identity.NodeID, kind, name)
	if err != nil {
		return nil, err
	}
	defer os.Remove(staged.ArchivePath)
	currentDigest, err := skills.DigestFile(staged.ArchivePath)
	if err != nil || normalizeDigest(currentDigest) != staged.ArchiveDigest {
		return nil, failed("ARCHIVE_CHANGED", "validation", "staged archive changed after review")
	}
	if err := InspectArchive(staged.ArchivePath, kind); err != nil {
		return nil, err
	}
	switch kind {
	case KindSkill:
		if s.deps.SkillValidate == nil || s.deps.SkillInstall == nil {
			return nil, failed("RESOURCE_LIBRARY_UNAVAILABLE", "validation", "Skill installer is unavailable")
		}
		preview, err := s.deps.SkillValidate(ctx, staged.ArchivePath, staged.ArchiveDigest)
		if err != nil {
			return nil, err
		}
		if !preview.Valid || preview.Name != name || normalizeDigest(preview.ContentDigest) != staged.PackageDigest {
			return nil, failed("ARCHIVE_CHANGED", "validation", "Skill candidate changed after review")
		}
		installed, err := s.deps.SkillInstall(ctx, staged.ArchivePath, staged.ArchiveDigest)
		if err != nil {
			return nil, err
		}
		if installed.Name != name || normalizeDigest(installed.ContentDigest) != staged.PackageDigest {
			return nil, failed("DIGEST_MISMATCH", "validation", "installed Skill digest does not match the reviewed digest")
		}
		return map[string]any{
			"ok": true, "action": ActionInstallCommit, "kind": kind, "name": installed.Name,
			"operation": staged.Operation, "changed": installed.Changed, "package_digest": staged.PackageDigest,
		}, nil
	default:
		if s.deps.PluginValidate == nil {
			return nil, failed("RESOURCE_LIBRARY_UNAVAILABLE", "validation", "Plugin validator is unavailable")
		}
		preview, err := s.deps.PluginValidate(ctx, staged.ArchivePath)
		if err != nil {
			return nil, err
		}
		// review_token 只标识预检时的内容。内容变了就必须重新 validate，不能沿用旧令牌。
		if !preview.Valid || preview.Name != name || preview.ReviewToken != staged.ReviewToken || normalizeDigest(preview.PackageDigest) != staged.PackageDigest {
			return nil, failed("ARCHIVE_CHANGED", "validation", "Plugin candidate changed after review")
		}
		var installed PluginCommit
		if staged.Operation == OperationUpdate {
			if s.deps.PluginUpdate == nil {
				return nil, failed("RESOURCE_LIBRARY_UNAVAILABLE", "validation", "Plugin updater is unavailable")
			}
			installed, err = s.deps.PluginUpdate(ctx, staged.ArchivePath, staged.ReviewToken)
		} else {
			if s.deps.PluginInstall == nil {
				return nil, failed("RESOURCE_LIBRARY_UNAVAILABLE", "validation", "Plugin installer is unavailable")
			}
			installed, err = s.deps.PluginInstall(ctx, staged.ArchivePath, staged.ReviewToken)
		}
		if err != nil {
			return nil, err
		}
		if installed.Name != name || normalizeDigest(installed.PackageDigest) != staged.PackageDigest {
			return nil, failed("DIGEST_MISMATCH", "validation", "installed Plugin digest does not match the reviewed digest")
		}
		return map[string]any{
			"ok": true, "action": ActionInstallCommit, "kind": kind, "name": installed.Name,
			"version": installed.Version, "operation": staged.Operation, "changed": installed.Changed,
			"package_digest": staged.PackageDigest,
		}, nil
	}
}

func (s *Service) consumeInstall(request controlRequest, nodeID, kind, name string) (stagedPackage, error) {
	challenge := strings.TrimSpace(request.Challenge)
	if challenge == "" {
		return stagedPackage{}, failed("CHALLENGE_INVALID", "validation", "install challenge is required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	staged, ok := s.pending[challenge]
	if !ok || staged.Purpose != purposeInstall {
		return stagedPackage{}, failed("CHALLENGE_INVALID", "validation", "install challenge is unknown")
	}
	if !s.deps.Now().Before(staged.Expires) {
		_ = os.Remove(staged.ArchivePath)
		delete(s.pending, challenge)
		return stagedPackage{}, failed("CHALLENGE_EXPIRED", "validation", "install challenge has expired")
	}
	if staged.Kind != kind || staged.Name != name || staged.NodeID != nodeID || staged.Operation != strings.TrimSpace(request.Operation) ||
		staged.ArchiveDigest != normalizeDigest(request.ArchiveDigest) || staged.PackageDigest != normalizeDigest(request.PackageDigest) {
		return stagedPackage{}, failed("CHALLENGE_MISMATCH", "validation", "install challenge does not match this confirmation")
	}
	if kind == KindPlugin && staged.ReviewToken != strings.TrimSpace(request.ReviewToken) {
		return stagedPackage{}, failed("REVIEW_TOKEN_MISMATCH", "validation", "review_token does not match the prepared Plugin review")
	}
	if kind == KindSkill && strings.TrimSpace(request.ReviewToken) != "" {
		return stagedPackage{}, failed("REVIEW_TOKEN_MISMATCH", "validation", "Skill install does not accept a review_token")
	}
	delete(s.pending, challenge)
	return staged, nil
}

func (s *Service) consumeExport(grantID string) (stagedPackage, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	staged, ok := s.pending[grantID]
	if !ok || staged.Purpose != purposeExport {
		return stagedPackage{}, failed("GRANT_INVALID", "validation", "download grant is unknown")
	}
	if !s.deps.Now().Before(staged.Expires) {
		_ = os.Remove(staged.ArchivePath)
		delete(s.pending, grantID)
		return stagedPackage{}, failed("GRANT_EXPIRED", "validation", "download grant has expired")
	}
	delete(s.pending, grantID)
	return staged, nil
}

func (s *Service) currentIdentity() (DeviceIdentity, error) {
	if s.deps.Identity == nil {
		return DeviceIdentity{}, failed("NEXUS_NOT_PAIRED", "validation", "Nexus device identity is unavailable")
	}
	identity, err := s.deps.Identity()
	if err != nil || strings.TrimSpace(identity.Endpoint) == "" || strings.TrimSpace(identity.NodeID) == "" || strings.TrimSpace(identity.DeviceToken) == "" {
		return DeviceIdentity{}, failed("NEXUS_NOT_PAIRED", "validation", "AgentDock is not paired with Nexus")
	}
	return identity, nil
}

func (s *Service) pairedIdentity(nodeID string) (DeviceIdentity, error) {
	identity, err := s.currentIdentity()
	if err != nil {
		return DeviceIdentity{}, err
	}
	if strings.TrimSpace(nodeID) == "" || strings.TrimSpace(nodeID) != identity.NodeID {
		return DeviceIdentity{}, failed("NODE_MISMATCH", "validation", "node_id does not match the paired device")
	}
	return identity, nil
}

func normalizePackageRef(kind, name string) (string, string, error) {
	kind = strings.TrimSpace(kind)
	name = strings.TrimSpace(name)
	if kind != KindSkill && kind != KindPlugin {
		return "", "", failed("PACKAGE_KIND_INVALID", "validation", "package kind must be skill or plugin")
	}
	if name == "" || strings.Contains(name, "/") || strings.Contains(name, `\`) || name == "." || name == ".." {
		return "", "", failed("PACKAGE_NAME_INVALID", "validation", "package name is invalid")
	}
	return kind, name, nil
}

func decodeControl(body []byte) (controlRequest, error) {
	if len(body) > maxControlBody {
		return controlRequest{}, failed("RESOURCE_LIBRARY_BODY_TOO_LARGE", "validation", "resource library control body is too large")
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	var request controlRequest
	if err := decoder.Decode(&request); err != nil {
		return controlRequest{}, failed("RESOURCE_LIBRARY_BODY_INVALID", "validation", "resource library control body is invalid")
	}
	var extra any
	if err := decoder.Decode(&extra); !errorsIsEOF(err) {
		return controlRequest{}, failed("RESOURCE_LIBRARY_BODY_INVALID", "validation", "resource library control body must contain one JSON object")
	}
	request.Action = strings.TrimSpace(request.Action)
	return request, nil
}

func errorsIsEOF(err error) bool {
	return err == io.EOF
}

func normalizeDigest(value string) string {
	value = strings.TrimSpace(strings.ToLower(value))
	if value == "" {
		return ""
	}
	if !strings.HasPrefix(value, "sha256:") {
		value = "sha256:" + value
	}
	return value
}

func (s *Service) reserveSlot() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.pending) >= maxStaged {
		return failed("RESOURCE_LIBRARY_BUSY", "validation", "too many resource library operations are pending")
	}
	return nil
}

func (s *Service) stagingPath(prefix string) (string, error) {
	name, err := randomToken()
	if err != nil {
		return "", err
	}
	return filepath.Join(s.root, prefix+"-"+name+".zip"), nil
}

func (s *Service) remember(staged stagedPackage) (string, error) {
	token, err := randomToken()
	if err != nil {
		return "", err
	}
	if staged.Purpose == purposeInstall {
		token = "rlc1_" + token
	} else {
		token = "rlg1_" + token
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.pending) >= maxStaged {
		return "", failed("RESOURCE_LIBRARY_BUSY", "validation", "too many resource library operations are pending")
	}
	s.pending[token] = staged
	return token, nil
}

func (s *Service) pendingExpiry(token string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	staged, ok := s.pending[token]
	if !ok {
		return ""
	}
	return staged.Expires.UTC().Format(time.RFC3339)
}

func (s *Service) expire() {
	now := s.deps.Now()
	s.mu.Lock()
	defer s.mu.Unlock()
	for token, staged := range s.pending {
		if now.Before(staged.Expires) {
			continue
		}
		_ = os.Remove(staged.ArchivePath)
		delete(s.pending, token)
	}
}

func randomToken() (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", failed("RESOURCE_LIBRARY_UNAVAILABLE", "validation", "resource library token cannot be created")
	}
	return hex.EncodeToString(raw), nil
}
