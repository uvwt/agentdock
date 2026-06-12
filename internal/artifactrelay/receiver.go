package artifactrelay

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

type TransferClient interface {
	DownloadArtifact(context.Context, DeviceCredentials, string, string, io.Writer) (DownloadResult, error)
	ReportArtifactResult(context.Context, DeviceCredentials, string, string, DeliveryResultRequest) error
}

type Receiver struct {
	client      TransferClient
	credentials func() (DeviceCredentials, error)
	privateKey  []byte
	inboxRoot   string
	targets     map[string]string
	now         func() time.Time
}

type ReceiverConfig struct {
	Client      TransferClient
	Credentials func() (DeviceCredentials, error)
	PrivateKey  []byte
	InboxRoot   string
	Targets     map[string]string
}

var safeID = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$`)

func NewReceiver(config ReceiverConfig) (*Receiver, error) {
	if config.Client == nil || config.Credentials == nil || len(config.PrivateKey) != 32 {
		return nil, errors.New("artifact client, credentials, and X25519 private key are required")
	}
	root, err := filepath.Abs(strings.TrimSpace(config.InboxRoot))
	if err != nil || strings.TrimSpace(config.InboxRoot) == "" {
		return nil, errors.New("artifact inbox root is invalid")
	}
	if err := ensureSecureDirectory(root); err != nil {
		return nil, err
	}
	targets := map[string]string{}
	for name, path := range config.Targets {
		if name == "inbox" || !safeID.MatchString(name) {
			return nil, fmt.Errorf("invalid artifact target %q", name)
		}
		absolute, err := filepath.Abs(strings.TrimSpace(path))
		if err != nil || strings.TrimSpace(path) == "" {
			return nil, fmt.Errorf("invalid path for artifact target %q", name)
		}
		if err := ensureSecureDirectory(absolute); err != nil {
			return nil, fmt.Errorf("prepare artifact target %q: %w", name, err)
		}
		targets[name] = absolute
	}
	return &Receiver{client: config.Client, credentials: config.Credentials, privateKey: append([]byte(nil), config.PrivateKey...), inboxRoot: root, targets: targets, now: time.Now}, nil
}

func ParseTargetsJSON(value string) (map[string]string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return map[string]string{}, nil
	}
	var targets map[string]string
	if err := json.Unmarshal([]byte(value), &targets); err != nil {
		return nil, fmt.Errorf("decode AGENTDOCK_ARTIFACT_TARGETS_JSON: %w", err)
	}
	if targets == nil {
		targets = map[string]string{}
	}
	return targets, nil
}

func (r *Receiver) Pull(ctx context.Context, raw json.RawMessage) (result PullResult, err error) {
	var payload PullPayload
	if err := json.Unmarshal(raw, &payload); err != nil {
		return result, errors.New("artifact.pull payload is invalid")
	}
	if err := validatePullPayload(payload, r.now()); err != nil {
		return result, err
	}
	credentials, err := r.credentials()
	if err != nil {
		return result, err
	}
	if credentials.DeviceID == "" || credentials.DeviceToken == "" {
		return result, errors.New("valid Nexus device credentials are required")
	}
	defer func() {
		if err == nil {
			return
		}
		_ = r.client.ReportArtifactResult(context.WithoutCancel(ctx), credentials, payload.ResultPath, payload.DownloadToken, DeliveryResultRequest{
			Status: "failed", ErrorCode: "ARTIFACT_PULL_FAILED", ErrorMessage: sanitizeError(err),
		})
	}()

	deliveryDir := filepath.Join(r.inboxRoot, payload.DeliveryID)
	if err := ensureChildDirectory(r.inboxRoot, deliveryDir); err != nil {
		return result, err
	}
	encryptedPart := filepath.Join(deliveryDir, "payload.adr.part")
	decryptedPart := filepath.Join(deliveryDir, "payload.plain.part")
	_ = os.Remove(encryptedPart)
	_ = os.Remove(decryptedPart)
	defer os.Remove(encryptedPart)
	defer os.Remove(decryptedPart)

	encrypted, err := os.OpenFile(encryptedPart, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return result, fmt.Errorf("create encrypted artifact staging file: %w", err)
	}
	hash := sha256.New()
	download, downloadErr := r.client.DownloadArtifact(ctx, credentials, payload.DownloadPath, payload.DownloadToken, io.MultiWriter(encrypted, hash))
	closeErr := encrypted.Close()
	if downloadErr != nil {
		return result, downloadErr
	}
	if closeErr != nil {
		return result, closeErr
	}
	actualCipherSHA := hex.EncodeToString(hash.Sum(nil))
	if download.Bytes != payload.CipherSize || download.Bytes > MaxCipherBytes {
		return result, errors.New("encrypted artifact size mismatch")
	}
	if !equalDigest(actualCipherSHA, payload.CipherSHA256) {
		return result, errors.New("encrypted artifact SHA-256 mismatch")
	}
	if download.CipherSHA256 != "" && !equalDigest(download.CipherSHA256, payload.CipherSHA256) {
		return result, errors.New("artifact download metadata mismatch")
	}

	fileKey, err := UnwrapFileKey(r.privateKey, payload.EphemeralPublicKey, payload.WrappedKey, payload.WrapNonce, payload.ArtifactID, payload.DeliveryID, credentials.DeviceID)
	if err != nil {
		return result, err
	}
	decrypt, err := DecryptFile(encryptedPart, decryptedPart, fileKey)
	zeroBytes(fileKey)
	if err != nil {
		return result, err
	}
	if decrypt.PlainSize != payload.PlainSize || !equalDigest(decrypt.PlainSHA256, payload.PlainSHA256) {
		return result, errors.New("decrypted artifact integrity check failed")
	}

	base, err := r.targetBase(payload.LogicalTarget, deliveryDir)
	if err != nil {
		return result, err
	}
	var finalPath string
	if payload.Extract {
		if !strings.HasSuffix(strings.ToLower(payload.Filename), ".tar.gz") && !strings.HasSuffix(strings.ToLower(payload.Filename), ".tgz") {
			return result, errors.New("automatic extraction only supports tar.gz archives")
		}
		name := strings.TrimSuffix(strings.TrimSuffix(payload.Filename, ".gz"), ".tar")
		name = strings.TrimSuffix(name, ".tgz")
		if name == "" {
			name = "extracted"
		}
		candidate, err := resolveConflict(base, name, payload.ConflictPolicy, true)
		if err != nil {
			return result, err
		}
		tempExtract := filepath.Join(base, "."+payload.DeliveryID+".extract.part")
		_ = os.RemoveAll(tempExtract)
		if err := ExtractTarGzip(decryptedPart, tempExtract, MaxExtractedFiles, MaxExtractedBytes); err != nil {
			_ = os.RemoveAll(tempExtract)
			return result, err
		}
		if payload.ConflictPolicy == "overwrite" {
			_ = os.RemoveAll(candidate)
		}
		if err := os.Rename(tempExtract, candidate); err != nil {
			_ = os.RemoveAll(tempExtract)
			return result, fmt.Errorf("publish extracted artifact: %w", err)
		}
		finalPath = candidate
	} else {
		candidate, err := resolveConflict(base, payload.Filename, payload.ConflictPolicy, false)
		if err != nil {
			return result, err
		}
		if err := publishFile(decryptedPart, candidate, payload.ConflictPolicy); err != nil {
			return result, err
		}
		finalPath = candidate
	}
	_ = os.Remove(encryptedPart)
	result = PullResult{ArtifactID: payload.ArtifactID, DeliveryID: payload.DeliveryID, Path: finalPath, PlainSize: decrypt.PlainSize, PlainSHA256: decrypt.PlainSHA256, Extracted: payload.Extract, CompletedAt: r.now().UTC()}
	if err := r.client.ReportArtifactResult(ctx, credentials, payload.ResultPath, payload.DownloadToken, DeliveryResultRequest{Status: "completed", LocalPath: finalPath}); err != nil {
		return PullResult{}, err
	}
	return result, nil
}

func (r *Receiver) targetBase(logicalTarget, deliveryDir string) (string, error) {
	logicalTarget = strings.TrimSpace(logicalTarget)
	if logicalTarget == "" || logicalTarget == "inbox" {
		return deliveryDir, nil
	}
	base, ok := r.targets[logicalTarget]
	if !ok {
		return "", fmt.Errorf("artifact logical target %q is not configured", logicalTarget)
	}
	return base, nil
}

func validatePullPayload(payload PullPayload, now time.Time) error {
	for name, value := range map[string]string{
		"artifact_id": payload.ArtifactID, "delivery_id": payload.DeliveryID, "filename": payload.Filename,
		"cipher_sha256": payload.CipherSHA256, "plain_sha256": payload.PlainSHA256,
		"ephemeral_public_key": payload.EphemeralPublicKey, "wrapped_key": payload.WrappedKey, "wrap_nonce": payload.WrapNonce,
		"download_token": payload.DownloadToken, "download_path": payload.DownloadPath, "result_path": payload.ResultPath,
	} {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("artifact.pull %s is required", name)
		}
	}
	if filepath.Base(payload.Filename) != payload.Filename || strings.ContainsAny(payload.Filename, `/\\`) {
		return errors.New("artifact filename is unsafe")
	}
	if !safeID.MatchString(payload.ArtifactID) || !safeID.MatchString(payload.DeliveryID) {
		return errors.New("artifact or delivery id is invalid")
	}
	if payload.CipherSize <= 0 || payload.CipherSize > MaxCipherBytes || payload.PlainSize < 0 {
		return errors.New("artifact sizes are invalid")
	}
	if !validDigest(payload.CipherSHA256) || !validDigest(payload.PlainSHA256) {
		return errors.New("artifact digest is invalid")
	}
	if payload.ConflictPolicy != "reject" && payload.ConflictPolicy != "rename" && payload.ConflictPolicy != "overwrite" {
		return errors.New("artifact conflict policy is invalid")
	}
	expiresAt, err := time.Parse(time.RFC3339Nano, payload.ExpiresAt)
	if err != nil || !now.Before(expiresAt) {
		return errors.New("artifact delivery is expired")
	}
	return nil
}

func ensureSecureDirectory(path string) error {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		if err := os.MkdirAll(path, 0o700); err != nil {
			return err
		}
		return os.Chmod(path, 0o700)
	}
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return errors.New("artifact directory is not a real directory")
	}
	return nil
}

func ensureChildDirectory(root, child string) error {
	rootAbs, _ := filepath.Abs(root)
	childAbs, _ := filepath.Abs(child)
	relative, err := filepath.Rel(rootAbs, childAbs)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(os.PathSeparator)) {
		return errors.New("artifact delivery path escapes inbox")
	}
	return ensureSecureDirectory(childAbs)
}

func resolveConflict(base, name, policy string, directory bool) (string, error) {
	if filepath.Base(name) != name || name == "." || name == ".." || strings.ContainsAny(name, `/\\`) {
		return "", errors.New("artifact destination name is unsafe")
	}
	candidate := filepath.Join(base, name)
	info, err := os.Lstat(candidate)
	if errors.Is(err, os.ErrNotExist) {
		return candidate, nil
	}
	if err != nil {
		return "", err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return "", errors.New("artifact destination is a symbolic link")
	}
	switch policy {
	case "reject":
		return "", errors.New("artifact destination already exists")
	case "overwrite":
		if directory != info.IsDir() {
			return "", errors.New("artifact destination type conflicts with existing path")
		}
		return candidate, nil
	case "rename":
		for i := 1; i <= 9999; i++ {
			candidate = filepath.Join(base, renamed(name, i, directory))
			if _, err := os.Lstat(candidate); errors.Is(err, os.ErrNotExist) {
				return candidate, nil
			} else if err != nil {
				return "", err
			}
		}
		return "", errors.New("cannot allocate an artifact destination name")
	default:
		return "", errors.New("artifact conflict policy is invalid")
	}
}

func renamed(name string, index int, directory bool) string {
	if directory {
		return fmt.Sprintf("%s-%d", name, index)
	}
	extension := filepath.Ext(name)
	base := strings.TrimSuffix(name, extension)
	return fmt.Sprintf("%s-%d%s", base, index, extension)
}

func publishFile(source, target, policy string) error {
	if err := ensureSecureDirectory(filepath.Dir(target)); err != nil {
		return err
	}
	temp, err := os.CreateTemp(filepath.Dir(target), ".artifact-publish-*.part")
	if err != nil {
		return err
	}
	tempPath := temp.Name()
	defer os.Remove(tempPath)
	if err := temp.Chmod(0o600); err != nil {
		temp.Close()
		return err
	}
	input, err := os.Open(source)
	if err != nil {
		temp.Close()
		return err
	}
	_, copyErr := io.Copy(temp, input)
	inputCloseErr := input.Close()
	syncErr := temp.Sync()
	closeErr := temp.Close()
	if copyErr != nil {
		return copyErr
	}
	if inputCloseErr != nil {
		return inputCloseErr
	}
	if syncErr != nil {
		return syncErr
	}
	if closeErr != nil {
		return closeErr
	}
	if policy == "overwrite" {
		if info, err := os.Lstat(target); err == nil {
			if info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
				return errors.New("artifact overwrite target is unsafe")
			}
			if err := os.Remove(target); err != nil {
				return err
			}
		}
	}
	if err := os.Rename(tempPath, target); err != nil {
		return fmt.Errorf("publish artifact file: %w", err)
	}
	return nil
}

func validDigest(value string) bool {
	decoded, err := hex.DecodeString(strings.TrimSpace(value))
	return err == nil && len(decoded) == sha256.Size
}
func equalDigest(a, b string) bool {
	return equalBytes([]byte(strings.ToLower(strings.TrimSpace(a))), []byte(strings.ToLower(strings.TrimSpace(b))))
}
func sanitizeError(err error) string {
	if err == nil {
		return ""
	}
	value := err.Error()
	if len(value) > 1024 {
		value = value[:1024]
	}
	return value
}
func zeroBytes(value []byte) {
	for i := range value {
		value[i] = 0
	}
}
