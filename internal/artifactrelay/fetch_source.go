package artifactrelay

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type FetchSourceClient interface {
	UploadArtifactFetch(context.Context, DeviceCredentials, string, string, string, FetchManifest) (FetchJob, error)
	ReportArtifactFetchResult(context.Context, DeviceCredentials, string, string, FetchResultRequest) error
}

type SourceFetcher struct {
	client       FetchSourceClient
	credentials  func() (DeviceCredentials, error)
	tempRoot     string
	denyPrefixes []string
	now          func() time.Time
}

type SourceFetcherConfig struct {
	Client              FetchSourceClient
	Credentials         func() (DeviceCredentials, error)
	TempRoot            string
	AdditionalDenyPaths []string
	StateDir            string
	EnvironmentFile     string
}

func NewSourceFetcher(config SourceFetcherConfig) (*SourceFetcher, error) {
	if config.Client == nil || config.Credentials == nil {
		return nil, errors.New("fetch source client and credentials are required")
	}
	root, err := filepath.Abs(strings.TrimSpace(config.TempRoot))
	if err != nil || strings.TrimSpace(config.TempRoot) == "" {
		return nil, errors.New("fetch source temporary root is invalid")
	}
	if err := ensureSecureDirectory(root); err != nil {
		return nil, err
	}
	prefixes := coreFetchDenyPrefixes(config.StateDir, config.EnvironmentFile)
	for _, raw := range config.AdditionalDenyPaths {
		value := strings.TrimSpace(raw)
		if value == "" || !filepath.IsAbs(value) {
			return nil, fmt.Errorf("artifact fetch deny path must be absolute: %q", raw)
		}
		prefixes = append(prefixes, filepath.Clean(value))
	}
	return &SourceFetcher{client: config.Client, credentials: config.Credentials, tempRoot: root, denyPrefixes: deduplicatePaths(prefixes), now: time.Now}, nil
}

func ParseFetchDenyJSON(value string) ([]string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return []string{}, nil
	}
	var paths []string
	if err := json.Unmarshal([]byte(value), &paths); err != nil {
		return nil, fmt.Errorf("decode AGENTDOCK_ARTIFACT_FETCH_DENY_JSON: %w", err)
	}
	return paths, nil
}

func (s *SourceFetcher) Fetch(ctx context.Context, raw json.RawMessage) (result any, err error) {
	var payload FetchCommandPayload
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, errors.New("artifact.fetch payload is invalid")
	}
	if err := validateFetchCommand(payload, s.now()); err != nil {
		return nil, err
	}
	credentials, err := s.credentials()
	if err != nil {
		return nil, err
	}
	reported := false
	defer func() {
		if err == nil || reported {
			return
		}
		_ = s.client.ReportArtifactFetchResult(context.WithoutCancel(ctx), credentials, payload.ResultPath, payload.UploadToken, FetchResultRequest{
			Status: FetchFailed, ErrorCode: "ARTIFACT_FETCH_SOURCE_FAILED", ErrorMessage: sanitizeError(err),
		})
	}()
	resolved, info, err := s.resolveAllowedSource(payload.SourcePath)
	if err != nil {
		return nil, err
	}
	if info.IsDir() && !payload.Archive {
		listing, err := s.listDirectory(resolved)
		if err != nil {
			return nil, err
		}
		if err := s.client.ReportArtifactFetchResult(ctx, credentials, payload.ResultPath, payload.UploadToken, FetchResultRequest{Status: FetchListed, Listing: listing}); err != nil {
			return nil, err
		}
		reported = true
		return map[string]any{"fetch_id": payload.FetchID, "status": FetchListed, "listing_count": len(listing)}, nil
	}
	prepared, err := PrepareSource(resolved, s.tempRoot)
	if err != nil {
		return nil, err
	}
	defer prepared.Cleanup()
	preparedInfo, err := os.Stat(prepared.Path)
	if err != nil {
		return nil, err
	}
	maxBytes := payload.MaxCipherBytes
	if maxBytes <= 0 || maxBytes > MaxCipherBytes {
		maxBytes = MaxCipherBytes
	}
	if preparedInfo.Size() > maxBytes {
		return nil, fmt.Errorf("artifact fetch source exceeds %d bytes", maxBytes)
	}
	encrypted, err := os.CreateTemp(s.tempRoot, "artifact-fetch-*.adr")
	if err != nil {
		return nil, err
	}
	encryptedPath := encrypted.Name()
	if err := encrypted.Close(); err != nil {
		_ = os.Remove(encryptedPath)
		return nil, err
	}
	defer os.Remove(encryptedPath)
	cryptoResult, err := EncryptFile(prepared.Path, encryptedPath)
	if err != nil {
		return nil, err
	}
	defer zeroBytes(cryptoResult.FileKey)
	defer zeroBytes(cryptoResult.EphemeralPrivateKey)
	if cryptoResult.CipherSize > maxBytes {
		return nil, fmt.Errorf("encrypted artifact fetch exceeds %d bytes", maxBytes)
	}
	wrappedKey, nonce, err := WrapFileKey(cryptoResult.EphemeralPrivateKey, payload.ReceiverPublicKey, cryptoResult.FileKey, payload.FetchID, payload.FetchID, payload.RequesterDeviceID)
	if err != nil {
		return nil, err
	}
	job, err := s.client.UploadArtifactFetch(ctx, credentials, payload.UploadPath, payload.UploadToken, encryptedPath, FetchManifest{
		FormatVersion: FormatVersion, CipherAlgorithm: CipherAlgorithm, Filename: prepared.Filename,
		ContentType: prepared.ContentType, PlainSize: cryptoResult.PlainSize, PlainSHA256: cryptoResult.PlainSHA256,
		EphemeralPublicKey: cryptoResult.EphemeralPublicKey, WrappedKey: wrappedKey, WrapNonce: nonce,
	})
	if err != nil {
		return nil, err
	}
	return map[string]any{"fetch_id": job.ID, "status": job.Status, "filename": job.Filename, "plain_size": job.PlainSize, "plain_sha256": job.PlainSHA256}, nil
}

func (s *SourceFetcher) resolveAllowedSource(source string) (string, os.FileInfo, error) {
	source = strings.TrimSpace(source)
	if source == "" || !filepath.IsAbs(source) {
		return "", nil, errors.New("artifact fetch source must be an absolute path")
	}
	clean := filepath.Clean(source)
	if s.denied(clean) {
		return "", nil, errors.New("artifact fetch source is protected")
	}
	originalInfo, err := os.Lstat(clean)
	if err != nil {
		return "", nil, fmt.Errorf("stat artifact fetch source: %w", err)
	}
	if originalInfo.Mode()&os.ModeSymlink != 0 {
		return "", nil, errors.New("artifact fetch source cannot be a symbolic link")
	}
	resolved, err := filepath.EvalSymlinks(clean)
	if err != nil {
		return "", nil, fmt.Errorf("resolve artifact fetch source: %w", err)
	}
	resolved, err = filepath.Abs(resolved)
	if err != nil {
		return "", nil, err
	}
	if s.denied(resolved) {
		return "", nil, errors.New("artifact fetch source resolves into a protected path")
	}
	info, err := os.Lstat(resolved)
	if err != nil {
		return "", nil, err
	}
	if info.Mode()&os.ModeSymlink != 0 || (!info.Mode().IsRegular() && !info.IsDir()) {
		return "", nil, errors.New("artifact fetch source must be a regular file or directory")
	}
	return resolved, info, nil
}

func (s *SourceFetcher) listDirectory(root string) ([]FetchEntry, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, err
	}
	if len(entries) > 1000 {
		entries = entries[:1000]
	}
	result := make([]FetchEntry, 0, len(entries))
	for _, entry := range entries {
		path := filepath.Join(root, entry.Name())
		info, err := entry.Info()
		if err != nil || info.Mode()&os.ModeSymlink != 0 || s.denied(path) {
			continue
		}
		kind := "file"
		if info.IsDir() {
			kind = "directory"
		} else if !info.Mode().IsRegular() {
			continue
		}
		result = append(result, FetchEntry{Name: entry.Name(), Path: path, Type: kind, Size: info.Size(), ModifiedAt: info.ModTime().UTC()})
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Name < result[j].Name })
	return result, nil
}

func (s *SourceFetcher) denied(path string) bool {
	clean := filepath.Clean(path)
	lowerBase := strings.ToLower(filepath.Base(clean))
	for _, name := range []string{".env", "agentdock.env", "id_rsa", "id_ed25519", "credentials", "login data", "cookies"} {
		if lowerBase == name {
			return true
		}
	}
	for _, prefix := range s.denyPrefixes {
		relative, err := filepath.Rel(prefix, clean)
		if err == nil && (relative == "." || (relative != ".." && !strings.HasPrefix(relative, ".."+string(os.PathSeparator)))) {
			return true
		}
	}
	return false
}

func coreFetchDenyPrefixes(stateDir, envFile string) []string {
	paths := []string{"/etc/shadow", "/etc/sudoers", "/private/etc/master.passwd", "/Library/Keychains", "/private/var/db"}
	if home, err := os.UserHomeDir(); err == nil {
		paths = append(paths, filepath.Join(home, ".ssh"), filepath.Join(home, ".gnupg"), filepath.Join(home, ".aws"), filepath.Join(home, ".kube"), filepath.Join(home, "Library", "Keychains"))
	}
	if stateDir != "" {
		paths = append(paths, filepath.Join(stateDir, "device"), filepath.Join(stateDir, "device-key"), filepath.Join(stateDir, "artifact-key"), filepath.Join(stateDir, "fetches"), filepath.Join(stateDir, "env"))
	}
	if envFile != "" {
		paths = append(paths, envFile)
	}
	return paths
}

func deduplicatePaths(values []string) []string {
	seen := map[string]struct{}{}
	result := []string{}
	for _, value := range values {
		clean := filepath.Clean(value)
		if _, ok := seen[clean]; ok {
			continue
		}
		seen[clean] = struct{}{}
		result = append(result, clean)
	}
	return result
}

func validateFetchCommand(payload FetchCommandPayload, now time.Time) error {
	if !safeID.MatchString(payload.FetchID) || !safeID.MatchString(payload.RequesterDeviceID) {
		return errors.New("artifact.fetch identifiers are invalid")
	}
	if !filepath.IsAbs(payload.SourcePath) || payload.ReceiverPublicKey == "" || payload.UploadToken == "" || payload.UploadPath == "" || payload.ResultPath == "" {
		return errors.New("artifact.fetch payload is incomplete")
	}
	if !validEndpointPath(payload.UploadPath) || !validEndpointPath(payload.ResultPath) {
		return errors.New("artifact.fetch endpoint is invalid")
	}
	expiresAt, err := time.Parse(time.RFC3339Nano, payload.ExpiresAt)
	if err != nil || !now.Before(expiresAt) {
		return errors.New("artifact.fetch request is expired")
	}
	return nil
}

func validEndpointPath(value string) bool {
	return strings.HasPrefix(value, "/v1/") && !strings.Contains(value, "..")
}
