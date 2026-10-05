package component

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/uvwt/agentdock/internal/fs/atomicfile"
)

const (
	CloudflaredName            = "cloudflared"
	activeSchemaVersion        = 1
	catalogSchemaVersion       = 1
	defaultCatalogURL          = "https://download.nexusdock.co/latest/agentdock-component-catalog.json"
	maxCatalogBytes      int64 = 2 << 20
	maxBinaryBytes       int64 = 256 << 20
)

var (
	ErrNotInstalled = errors.New("cloudflared component is not installed")
	ErrBroken       = errors.New("cloudflared component is broken")

	// 测试只替换这一层，以便用最小 fake binary 覆盖原子提交/repair；
	// 生产默认始终执行平台真实 trust verifier。
	platformTrustVerifier = verifyPlatformTrust
)

type Status struct {
	Component string `json:"component"`
	State     string `json:"state"`
	Installed bool   `json:"installed"`
	Ready     bool   `json:"ready"`
	Version   string `json:"version,omitempty"`
	Path      string `json:"path,omitempty"`
	SHA256    string `json:"sha256,omitempty"`
	Detail    string `json:"detail,omitempty"`
}

type ProgressEvent struct {
	Type  string `json:"type"`
	Stage string `json:"stage,omitempty"`
	Bytes int64  `json:"bytes,omitempty"`
	Total int64  `json:"total,omitempty"`
}

type activePointer struct {
	SchemaVersion int    `json:"schema_version"`
	Component     string `json:"component"`
	Version       string `json:"version"`
	SHA256        string `json:"sha256"`
}

type Catalog struct {
	SchemaVersion int                `json:"schema_version"`
	Components    []CatalogComponent `json:"components"`
}

type CatalogComponent struct {
	Component       string            `json:"component"`
	Version         string            `json:"version"`
	UpstreamVersion string            `json:"upstream_version"`
	UpstreamSource  string            `json:"upstream_source"`
	Artifacts       []CatalogArtifact `json:"artifacts"`
}

type CatalogArtifact struct {
	OS     string `json:"os"`
	Arch   string `json:"arch"`
	Format string `json:"format"`
	URL    string `json:"url"`
	SHA256 string `json:"sha256"`
}

type InstallOptions struct {
	RuntimeRoot string
	CatalogURL  string
	HTTPClient  *http.Client
	GOOS        string
	GOARCH      string
	Progress    func(ProgressEvent)
	LegacyPaths []string
}

type Store struct {
	runtimeRoot string
	root        string
}

func NewStore(runtimeRoot string) (*Store, error) {
	runtimeRoot = strings.TrimSpace(runtimeRoot)
	if runtimeRoot == "" {
		return nil, errors.New("component runtime root is required")
	}
	absolute, err := filepath.Abs(runtimeRoot)
	if err != nil {
		return nil, fmt.Errorf("resolve component runtime root: %w", err)
	}
	return &Store{runtimeRoot: absolute, root: filepath.Join(absolute, "components", CloudflaredName)}, nil
}

func (store *Store) Status() Status {
	status := Status{Component: CloudflaredName, State: "not_installed"}
	pointer, err := store.readActive()
	if errors.Is(err, os.ErrNotExist) {
		return status
	}
	if err != nil {
		status.State = "broken"
		status.Installed = true
		status.Detail = err.Error()
		return status
	}
	status.Installed = true
	status.Version = pointer.Version
	status.SHA256 = pointer.SHA256
	status.Path = store.binaryPath(pointer.Version)
	if err := validateManagedBinary(status.Path, pointer.SHA256); err != nil {
		status.State = "broken"
		status.Detail = err.Error()
		return status
	}
	status.State = "ready"
	status.Ready = true
	return status
}

func (store *Store) Resolve() (string, error) {
	status := store.Status()
	if status.Ready {
		return status.Path, nil
	}
	if status.State == "not_installed" {
		return "", ErrNotInstalled
	}
	if status.Detail == "" {
		return "", ErrBroken
	}
	return "", fmt.Errorf("%w: %s", ErrBroken, status.Detail)
}

func (store *Store) ImportLegacy(ctx context.Context, source string) (Status, error) {
	source = strings.TrimSpace(source)
	if source == "" {
		return Status{}, errors.New("legacy cloudflared path is required")
	}
	info, err := os.Lstat(source)
	if err != nil {
		return Status{}, fmt.Errorf("read legacy cloudflared: %w", err)
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return Status{}, errors.New("legacy cloudflared must be a regular non-symlink file")
	}
	if err := platformTrustVerifier(ctx, source, true); err != nil {
		return Status{}, err
	}
	version, err := readCloudflaredVersion(ctx, source)
	if err != nil {
		return Status{}, err
	}
	digest, err := fileSHA256(source)
	if err != nil {
		return Status{}, err
	}
	if err := store.installFromFile(ctx, source, version, digest); err != nil {
		return Status{}, err
	}
	return store.Status(), nil
}

func (store *Store) Install(ctx context.Context, options InstallOptions) (Status, error) {
	if current := store.Status(); current.Ready {
		return current, nil
	}
	for _, legacy := range options.LegacyPaths {
		legacy = strings.TrimSpace(legacy)
		if legacy == "" {
			continue
		}
		if _, err := os.Lstat(legacy); err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return Status{}, err
		}
		report(options.Progress, ProgressEvent{Type: "stage", Stage: "importing"})
		status, err := store.ImportLegacy(ctx, legacy)
		if err != nil {
			return Status{}, fmt.Errorf("import legacy cloudflared: %w", err)
		}
		report(options.Progress, ProgressEvent{Type: "completed", Stage: "ready"})
		return status, nil
	}
	return store.installCatalogVersion(ctx, options)
}

func (store *Store) Update(ctx context.Context, options InstallOptions) (Status, error) {
	return store.installCatalogVersion(ctx, options)
}

func (store *Store) Uninstall() error {
	_, err := store.readActive()
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read active cloudflared before uninstall: %w", err)
	}
	if err := os.Remove(store.activePath()); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove cloudflared active pointer: %w", err)
	}
	// active.json 是唯一运行选择点。先退役 active，再尽力回收历史版本；被系统占用的
	// 旧 binary 宁可保留，也不能让清理失败把一个已卸载组件重新变成 active。
	versionsDir := filepath.Join(store.root, "versions")
	entries, readErr := os.ReadDir(versionsDir)
	if readErr != nil && !errors.Is(readErr, os.ErrNotExist) {
		return readErr
	}
	for _, entry := range entries {
		if entry.IsDir() {
			_ = os.RemoveAll(filepath.Join(versionsDir, entry.Name()))
		}
	}
	return nil
}

func (store *Store) installCatalogVersion(ctx context.Context, options InstallOptions) (Status, error) {
	client := options.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 2 * time.Minute}
	}
	catalogURL := strings.TrimSpace(options.CatalogURL)
	if catalogURL == "" {
		catalogURL = defaultCatalogURL
	}
	goos := strings.TrimSpace(options.GOOS)
	if goos == "" {
		goos = runtime.GOOS
	}
	goarch := strings.TrimSpace(options.GOARCH)
	if goarch == "" {
		goarch = runtime.GOARCH
	}
	report(options.Progress, ProgressEvent{Type: "stage", Stage: "catalog"})
	catalogData, err := download(ctx, client, catalogURL, maxCatalogBytes, nil)
	if err != nil {
		return Status{}, fmt.Errorf("download component catalog: %w", err)
	}
	entry, artifact, err := selectCloudflaredArtifact(catalogData, goos, goarch)
	if err != nil {
		return Status{}, err
	}
	// catalog SHA-256 描述 upstream 下载物；active.json SHA-256 描述最终安装 binary。
	// macOS 上游是 tgz，所以相同版本健康时不能再把两个 digest 当成同一个值比较。
	if current := store.Status(); current.Ready && current.Version == entry.Version {
		return current, nil
	}
	report(options.Progress, ProgressEvent{Type: "stage", Stage: "downloading"})
	data, err := download(ctx, client, artifact.URL, maxBinaryBytes, func(done, total int64) {
		report(options.Progress, ProgressEvent{Type: "progress", Stage: "downloading", Bytes: done, Total: total})
	})
	if err != nil {
		return Status{}, fmt.Errorf("download cloudflared component: %w", err)
	}
	report(options.Progress, ProgressEvent{Type: "stage", Stage: "verifying"})
	actual := sha256.Sum256(data)
	actualHex := hex.EncodeToString(actual[:])
	if !strings.EqualFold(actualHex, artifact.SHA256) {
		return Status{}, fmt.Errorf("cloudflared SHA-256 mismatch: got %s, want %s", actualHex, artifact.SHA256)
	}
	if err := store.installArtifact(ctx, data, entry.Version, artifact.Format); err != nil {
		return Status{}, err
	}
	status := store.Status()
	if !status.Ready {
		return Status{}, fmt.Errorf("cloudflared component did not become ready: %s", status.Detail)
	}
	report(options.Progress, ProgressEvent{Type: "completed", Stage: "ready"})
	return status, nil
}

func (store *Store) installArtifact(ctx context.Context, data []byte, version, format string) error {
	if err := validateVersion(version); err != nil {
		return err
	}
	if err := os.MkdirAll(store.root, 0o700); err != nil {
		return err
	}
	staging, err := os.MkdirTemp(store.root, ".staging-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(staging)

	staged := filepath.Join(staging, binaryFilename(runtime.GOOS))
	switch format {
	case "binary":
		if err := os.WriteFile(staged, data, 0o700); err != nil {
			return err
		}
		if err := os.Chmod(staged, 0o700); err != nil {
			return err
		}
	case "tgz":
		if runtime.GOOS != "darwin" {
			return fmt.Errorf("cloudflared tgz artifact is unsupported on %s", runtime.GOOS)
		}
		if err := extractCloudflaredTGZ(data, staged); err != nil {
			return err
		}
	default:
		return fmt.Errorf("unsupported cloudflared artifact format %q", format)
	}

	// active.json 只记录最终落盘 binary 的 digest；下载 archive 的 digest 已在解包前校验。
	binaryDigest, err := fileSHA256(staged)
	if err != nil {
		return err
	}
	return store.commitStaged(ctx, staged, version, binaryDigest)
}

func extractCloudflaredTGZ(data []byte, target string) error {
	gzipReader, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("open cloudflared tgz: %w", err)
	}
	defer gzipReader.Close()

	tarReader := tar.NewReader(gzipReader)
	found := false
	for {
		header, err := tarReader.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return fmt.Errorf("read cloudflared tgz: %w", err)
		}

		name := strings.TrimSpace(header.Name)
		// 官方 tgz 契约只有根目录下单个 cloudflared 普通文件。即便最终写入目标路径由我们控制，
		// 也拒绝任何需要 path clean 才能变成 cloudflared 的名称，避免接受 traversal 语义。
		if name != "cloudflared" {
			return fmt.Errorf("cloudflared tgz contains unexpected or unsafe entry %q", header.Name)
		}
		if found {
			return errors.New("cloudflared tgz contains duplicate cloudflared entries")
		}
		if header.Typeflag != tar.TypeReg && header.Typeflag != tar.TypeRegA {
			return fmt.Errorf("cloudflared tgz entry must be a regular file, got type %d", header.Typeflag)
		}
		if header.Size < 0 || header.Size > maxBinaryBytes {
			return errors.New("cloudflared tgz binary is unexpectedly large")
		}

		output, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o700)
		if err != nil {
			return err
		}
		written, copyErr := io.Copy(output, io.LimitReader(tarReader, maxBinaryBytes+1))
		syncErr := output.Sync()
		closeErr := output.Close()
		if copyErr != nil {
			return copyErr
		}
		if written != header.Size || written > maxBinaryBytes {
			return errors.New("cloudflared tgz binary size is invalid")
		}
		if syncErr != nil {
			return syncErr
		}
		if closeErr != nil {
			return closeErr
		}
		if err := os.Chmod(target, 0o700); err != nil {
			return err
		}
		found = true
	}
	if !found {
		return errors.New("cloudflared tgz does not contain cloudflared")
	}
	return nil
}

func (store *Store) installFromFile(ctx context.Context, source, version, digest string) error {
	if err := validateVersion(version); err != nil {
		return err
	}
	if err := os.MkdirAll(store.root, 0o700); err != nil {
		return err
	}
	staging, err := os.MkdirTemp(store.root, ".staging-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(staging)
	staged := filepath.Join(staging, binaryFilename(runtime.GOOS))
	if err := copyFile(source, staged); err != nil {
		return err
	}
	return store.commitStaged(ctx, staged, version, digest)
}

func (store *Store) commitStaged(ctx context.Context, staged, version, digest string) error {
	if err := validateSHA256(digest); err != nil {
		return err
	}
	if got, err := fileSHA256(staged); err != nil {
		return err
	} else if !strings.EqualFold(got, digest) {
		return errors.New("staged cloudflared digest changed before commit")
	}
	if err := platformTrustVerifier(ctx, staged, false); err != nil {
		return err
	}
	actualVersion, err := readCloudflaredVersion(ctx, staged)
	if err != nil {
		return err
	}
	if actualVersion != version {
		return fmt.Errorf("cloudflared version is %s, catalog expects %s", actualVersion, version)
	}
	targetDir := filepath.Join(store.root, "versions", version)
	target := filepath.Join(targetDir, binaryFilename(runtime.GOOS))
	if currentDigest, err := fileSHA256(target); err == nil && strings.EqualFold(currentDigest, digest) {
		return store.writeActive(version, digest)
	}
	if err := os.MkdirAll(filepath.Dir(targetDir), 0o700); err != nil {
		return err
	}
	stagingDir := filepath.Dir(staged)
	if _, err := os.Lstat(targetDir); errors.Is(err, os.ErrNotExist) {
		if err := os.Rename(stagingDir, targetDir); err != nil {
			return fmt.Errorf("commit cloudflared version directory: %w", err)
		}
		// 版本目录完整落盘后，active.json 才是最后提交点；失败时旧 active 不会改变。
		return store.writeActive(version, digest)
	} else if err != nil {
		return err
	}

	// 同一版本目录内容损坏时允许 repair，但绝不原地覆盖正在运行的 binary。
	// 先把旧目录完整移到同卷 backup，再把已验证 staging 原子改名为目标；active.json
	// 仍是最后提交点。任何一步失败都恢复旧目录，使旧 active pointer 始终可解释。
	backupDir, err := os.MkdirTemp(filepath.Dir(targetDir), ".repair-backup-*")
	if err != nil {
		return fmt.Errorf("allocate cloudflared repair backup: %w", err)
	}
	if err := os.Remove(backupDir); err != nil {
		return fmt.Errorf("prepare cloudflared repair backup: %w", err)
	}
	if err := os.Rename(targetDir, backupDir); err != nil {
		return fmt.Errorf("move existing cloudflared version to repair backup: %w", err)
	}
	restore := func(cause error) error {
		_ = os.RemoveAll(targetDir)
		if restoreErr := os.Rename(backupDir, targetDir); restoreErr != nil {
			return fmt.Errorf("%w; restore cloudflared repair backup: %v", cause, restoreErr)
		}
		return cause
	}
	if err := os.Rename(stagingDir, targetDir); err != nil {
		return restore(fmt.Errorf("commit repaired cloudflared version directory: %w", err))
	}
	if err := store.writeActive(version, digest); err != nil {
		return restore(fmt.Errorf("activate repaired cloudflared version: %w", err))
	}
	if err := os.RemoveAll(backupDir); err != nil {
		// active 已经原子指向完整的新版本，旧 backup 清理失败不能反向破坏成功提交。
		return nil
	}
	return nil
}

func (store *Store) readActive() (activePointer, error) {
	data, err := os.ReadFile(store.activePath())
	if err != nil {
		return activePointer{}, err
	}
	var pointer activePointer
	if err := json.Unmarshal(data, &pointer); err != nil {
		return activePointer{}, fmt.Errorf("parse cloudflared active pointer: %w", err)
	}
	if pointer.SchemaVersion != activeSchemaVersion || pointer.Component != CloudflaredName {
		return activePointer{}, errors.New("cloudflared active pointer schema/component is invalid")
	}
	if err := validateVersion(pointer.Version); err != nil {
		return activePointer{}, err
	}
	if err := validateSHA256(pointer.SHA256); err != nil {
		return activePointer{}, err
	}
	return pointer, nil
}

func (store *Store) writeActive(version, digest string) error {
	pointer := activePointer{SchemaVersion: activeSchemaVersion, Component: CloudflaredName, Version: version, SHA256: strings.ToLower(digest)}
	data, err := json.MarshalIndent(pointer, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return atomicfile.Write(store.activePath(), data, 0o600)
}

func (store *Store) activePath() string {
	return filepath.Join(store.root, "active.json")
}

func (store *Store) binaryPath(version string) string {
	return filepath.Join(store.root, "versions", version, binaryFilename(runtime.GOOS))
}

func binaryFilename(goos string) string {
	if goos == "windows" {
		return "cloudflared.exe"
	}
	return "cloudflared"
}

func validateManagedBinary(path, digest string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return errors.New("active cloudflared is not a regular non-symlink file")
	}
	actual, err := fileSHA256(path)
	if err != nil {
		return err
	}
	if !strings.EqualFold(actual, digest) {
		return fmt.Errorf("active cloudflared SHA-256 mismatch: got %s, want %s", actual, digest)
	}
	return nil
}

func validateVersion(version string) error {
	version = strings.TrimSpace(version)
	if version == "" || version == "." || version == ".." || filepath.Base(version) != version ||
		strings.Contains(version, "/") || strings.Contains(version, "\\") || strings.ContainsRune(version, 0) {
		return fmt.Errorf("invalid cloudflared component version %q", version)
	}
	return nil
}

func validateSHA256(digest string) error {
	digest = strings.TrimSpace(digest)
	if len(digest) != sha256.Size*2 {
		return errors.New("cloudflared SHA-256 has invalid length")
	}
	if _, err := hex.DecodeString(digest); err != nil {
		return errors.New("cloudflared SHA-256 is invalid")
	}
	return nil
}

func fileSHA256(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return "", err
	}
	if info.Size() > maxBinaryBytes {
		return "", errors.New("cloudflared binary is unexpectedly large")
	}
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func copyFile(source, target string) error {
	input, err := os.Open(source)
	if err != nil {
		return err
	}
	defer input.Close()
	output, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o700)
	if err != nil {
		return err
	}
	closed := false
	defer func() {
		if !closed {
			_ = output.Close()
		}
	}()
	written, err := io.Copy(output, io.LimitReader(input, maxBinaryBytes+1))
	if err != nil {
		return err
	}
	if written > maxBinaryBytes {
		return errors.New("legacy cloudflared binary is unexpectedly large")
	}
	if err := output.Sync(); err != nil {
		return err
	}
	if err := output.Close(); err != nil {
		return err
	}
	closed = true
	return os.Chmod(target, 0o700)
}

func readCloudflaredVersion(ctx context.Context, path string) (string, error) {
	verifyCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	output, err := exec.CommandContext(verifyCtx, path, "--version").CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("run cloudflared --version: %w: %s", err, strings.TrimSpace(string(output)))
	}
	line := strings.TrimSpace(strings.SplitN(string(output), "\n", 2)[0])
	fields := strings.Fields(line)
	if len(fields) < 3 || fields[0] != "cloudflared" || fields[1] != "version" {
		return "", fmt.Errorf("unrecognized cloudflared version output %q", line)
	}
	version := strings.TrimSpace(fields[2])
	if err := validateVersion(version); err != nil {
		return "", err
	}
	return version, nil
}

func selectCloudflaredArtifact(data []byte, goos, goarch string) (CatalogComponent, CatalogArtifact, error) {
	var catalog Catalog
	if err := json.Unmarshal(data, &catalog); err != nil {
		return CatalogComponent{}, CatalogArtifact{}, fmt.Errorf("parse component catalog: %w", err)
	}
	if catalog.SchemaVersion != catalogSchemaVersion {
		return CatalogComponent{}, CatalogArtifact{}, fmt.Errorf("unsupported component catalog schema: %d", catalog.SchemaVersion)
	}
	for _, entry := range catalog.Components {
		if entry.Component != CloudflaredName {
			continue
		}
		if err := validateVersion(entry.Version); err != nil {
			return CatalogComponent{}, CatalogArtifact{}, err
		}
		if strings.TrimSpace(entry.UpstreamVersion) == "" || strings.TrimSpace(entry.UpstreamSource) == "" {
			return CatalogComponent{}, CatalogArtifact{}, errors.New("cloudflared catalog entry is missing upstream provenance")
		}
		for _, artifact := range entry.Artifacts {
			if artifact.OS != goos || artifact.Arch != goarch {
				continue
			}
			if err := validateSHA256(artifact.SHA256); err != nil {
				return CatalogComponent{}, CatalogArtifact{}, err
			}
			if err := validateCloudflaredUpstreamArtifact(entry, artifact); err != nil {
				return CatalogComponent{}, CatalogArtifact{}, err
			}
			return entry, artifact, nil
		}
		return CatalogComponent{}, CatalogArtifact{}, fmt.Errorf("component catalog has no cloudflared artifact for %s/%s", goos, goarch)
	}
	return CatalogComponent{}, CatalogArtifact{}, errors.New("component catalog does not contain cloudflared")
}

func validateCloudflaredUpstreamArtifact(entry CatalogComponent, artifact CatalogArtifact) error {
	if entry.UpstreamVersion != entry.Version {
		return errors.New("cloudflared catalog upstream version must match component version")
	}
	expectedSource := "https://github.com/cloudflare/cloudflared/releases/tag/" + entry.Version
	if strings.TrimSpace(entry.UpstreamSource) != expectedSource {
		return errors.New("cloudflared catalog upstream source is not the pinned Cloudflare release")
	}

	expectedFormat := "binary"
	expectedName := ""
	switch {
	case artifact.OS == "windows" && artifact.Arch == "amd64":
		expectedName = "cloudflared-windows-amd64.exe"
	case artifact.OS == "darwin" && (artifact.Arch == "amd64" || artifact.Arch == "arm64"):
		expectedFormat = "tgz"
		expectedName = "cloudflared-darwin-" + artifact.Arch + ".tgz"
	default:
		return fmt.Errorf("unsupported cloudflared artifact platform %s/%s", artifact.OS, artifact.Arch)
	}
	if artifact.Format != expectedFormat {
		return fmt.Errorf("cloudflared artifact %s/%s format is %q, want %q", artifact.OS, artifact.Arch, artifact.Format, expectedFormat)
	}

	parsed, err := url.Parse(strings.TrimSpace(artifact.URL))
	if err != nil || parsed.Scheme != "https" || parsed.Host != "github.com" || parsed.RawQuery != "" || parsed.Fragment != "" {
		return errors.New("cloudflared artifact URL must use the pinned Cloudflare GitHub HTTPS release")
	}
	expectedPath := "/cloudflare/cloudflared/releases/download/" + entry.Version + "/" + expectedName
	if parsed.Path != expectedPath {
		return fmt.Errorf("cloudflared artifact URL is not the pinned official asset: %s", artifact.URL)
	}
	return nil
}

func download(ctx context.Context, client *http.Client, rawURL string, limit int64, progress func(int64, int64)) ([]byte, error) {
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || (parsed.Scheme != "https" && parsed.Scheme != "http") || parsed.Host == "" {
		return nil, errors.New("component download URL is invalid")
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("User-Agent", "agentdock-component")
	response, err := client.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", response.StatusCode)
	}
	if response.ContentLength > limit {
		return nil, fmt.Errorf("component download exceeds %d bytes", limit)
	}
	reader := io.LimitReader(response.Body, limit+1)
	data := make([]byte, 0)
	buffer := make([]byte, 64*1024)
	var total int64
	for {
		n, readErr := reader.Read(buffer)
		if n > 0 {
			total += int64(n)
			if total > limit {
				return nil, fmt.Errorf("component download exceeds %d bytes", limit)
			}
			data = append(data, buffer[:n]...)
			if progress != nil {
				progress(total, response.ContentLength)
			}
		}
		if readErr != nil {
			if errors.Is(readErr, io.EOF) {
				break
			}
			return nil, readErr
		}
	}
	return data, nil
}

func report(progress func(ProgressEvent), event ProgressEvent) {
	if progress != nil {
		progress(event)
	}
}
