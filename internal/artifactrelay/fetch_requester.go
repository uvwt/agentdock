package artifactrelay

import (
	"context"
	"crypto/ecdh"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type FetchRequesterClient interface {
	CreateDeviceArtifactFetch(context.Context, DeviceCredentials, CreateFetchRequest) (CreateFetchResult, error)
	GetDeviceArtifactFetch(context.Context, DeviceCredentials, string, string) (FetchJob, error)
	DownloadArtifactFetch(context.Context, DeviceCredentials, string, string, io.Writer) (DownloadResult, error)
	ConfirmArtifactFetchMounted(context.Context, DeviceCredentials, string, string) (FetchJob, error)
}

type FetchRequester struct {
	client      FetchRequesterClient
	credentials func() (DeviceCredentials, error)
	store       *FetchStore
	now         func() time.Time
}

type FetchRequesterConfig struct {
	Client      FetchRequesterClient
	Credentials func() (DeviceCredentials, error)
	Store       *FetchStore
}

type FetchCreateInput struct {
	SourceDeviceID   string
	SourcePath       string
	Archive          bool
	RetentionSeconds int64
}

type FetchDownloadOutput struct {
	FetchID         string      `json:"fetch_id"`
	Status          FetchStatus `json:"status"`
	FilePath        string      `json:"file_path"`
	FileName        string      `json:"file_name"`
	MIMEType        string      `json:"mime_type"`
	Size            int64       `json:"size"`
	SHA256          string      `json:"sha256"`
	OutputToken     string      `json:"-"`
	OutputExpiresAt time.Time   `json:"output_expires_at"`
}

func NewFetchRequester(config FetchRequesterConfig) (*FetchRequester, error) {
	if config.Client == nil || config.Credentials == nil || config.Store == nil {
		return nil, errors.New("fetch requester client, credentials, and store are required")
	}
	return &FetchRequester{client: config.Client, credentials: config.Credentials, store: config.Store, now: time.Now}, nil
}

func (r *FetchRequester) Create(ctx context.Context, input FetchCreateInput) (FetchJob, error) {
	credentials, err := r.credentials()
	if err != nil {
		return FetchJob{}, err
	}
	private, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		return FetchJob{}, fmt.Errorf("generate fetch receiver key: %w", err)
	}
	result, err := r.client.CreateDeviceArtifactFetch(ctx, credentials, CreateFetchRequest{
		SourceDeviceID: input.SourceDeviceID, SourcePath: input.SourcePath, Archive: input.Archive,
		ReceiverPublicKey: base64.RawURLEncoding.EncodeToString(private.PublicKey().Bytes()), RetentionSeconds: input.RetentionSeconds,
	})
	if err != nil {
		return FetchJob{}, err
	}
	state := FetchLocalState{
		FetchID: result.Fetch.ID, RequesterDeviceID: credentials.DeviceID,
		PrivateKey: base64.RawURLEncoding.EncodeToString(private.Bytes()), DownloadToken: result.DownloadToken,
		CreatedAt: r.now().UTC(),
	}
	if err := r.store.Save(state); err != nil {
		return FetchJob{}, err
	}
	return result.Fetch, nil
}

func (r *FetchRequester) Status(ctx context.Context, fetchID string) (FetchJob, error) {
	state, err := r.store.Load(fetchID)
	if err != nil {
		return FetchJob{}, err
	}
	credentials, err := r.credentials()
	if err != nil {
		return FetchJob{}, err
	}
	if credentials.DeviceID != state.RequesterDeviceID {
		return FetchJob{}, errors.New("artifact fetch belongs to another local device identity")
	}
	return r.client.GetDeviceArtifactFetch(ctx, credentials, fetchID, state.DownloadToken)
}

func (r *FetchRequester) Download(ctx context.Context, fetchID string) (FetchDownloadOutput, error) {
	state, err := r.store.Load(fetchID)
	if err != nil {
		return FetchDownloadOutput{}, err
	}
	credentials, err := r.credentials()
	if err != nil {
		return FetchDownloadOutput{}, err
	}
	job, err := r.client.GetDeviceArtifactFetch(ctx, credentials, fetchID, state.DownloadToken)
	if err != nil {
		return FetchDownloadOutput{}, err
	}
	if job.Status != FetchReady {
		return FetchDownloadOutput{}, fmt.Errorf("artifact fetch is not ready: %s", job.Status)
	}
	if filepath.Base(job.Filename) != job.Filename || strings.ContainsAny(job.Filename, `/\\`) || job.Filename == "" {
		return FetchDownloadOutput{}, errors.New("artifact fetch filename is unsafe")
	}
	outputDir, err := r.store.OutputDir(fetchID)
	if err != nil {
		return FetchDownloadOutput{}, err
	}
	encrypted, err := os.CreateTemp(outputDir, ".fetch-*.adr.part")
	if err != nil {
		return FetchDownloadOutput{}, err
	}
	encryptedPath := encrypted.Name()
	hash := sha256.New()
	download, downloadErr := r.client.DownloadArtifactFetch(ctx, credentials, fetchID, state.DownloadToken, io.MultiWriter(encrypted, hash))
	closeErr := encrypted.Close()
	defer os.Remove(encryptedPath)
	if downloadErr != nil {
		return FetchDownloadOutput{}, downloadErr
	}
	if closeErr != nil {
		return FetchDownloadOutput{}, closeErr
	}
	actualCipherSHA := hex.EncodeToString(hash.Sum(nil))
	if download.Bytes != job.CipherSize || download.Bytes > MaxCipherBytes || !equalDigest(actualCipherSHA, job.CipherSHA256) {
		return FetchDownloadOutput{}, errors.New("artifact fetch ciphertext integrity check failed")
	}
	privateBytes, err := base64.RawURLEncoding.DecodeString(state.PrivateKey)
	if err != nil || len(privateBytes) != 32 {
		return FetchDownloadOutput{}, errors.New("local artifact fetch private key is invalid")
	}
	fileKey, err := UnwrapFileKey(privateBytes, job.EphemeralPublicKey, job.WrappedKey, job.WrapNonce, job.ID, job.ID, credentials.DeviceID)
	zeroBytes(privateBytes)
	if err != nil {
		return FetchDownloadOutput{}, err
	}
	outputPath := filepath.Join(outputDir, job.Filename)
	decryptedPart := outputPath + ".part"
	_ = os.Remove(decryptedPart)
	decrypt, err := DecryptFile(encryptedPath, decryptedPart, fileKey)
	zeroBytes(fileKey)
	if err != nil {
		return FetchDownloadOutput{}, err
	}
	defer os.Remove(decryptedPart)
	if decrypt.PlainSize != job.PlainSize || !equalDigest(decrypt.PlainSHA256, job.PlainSHA256) {
		return FetchDownloadOutput{}, errors.New("artifact fetch plaintext integrity check failed")
	}
	if info, err := os.Lstat(outputPath); err == nil {
		if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			return FetchDownloadOutput{}, errors.New("artifact fetch output path is unsafe")
		}
		if err := os.Remove(outputPath); err != nil {
			return FetchDownloadOutput{}, err
		}
	}
	if err := os.Rename(decryptedPart, outputPath); err != nil {
		return FetchDownloadOutput{}, err
	}
	if err := os.Chmod(outputPath, 0o600); err != nil {
		return FetchDownloadOutput{}, err
	}
	outputToken, err := randomFetchToken(32)
	if err != nil {
		return FetchDownloadOutput{}, err
	}
	expires := r.now().UTC().Add(time.Hour)
	if job.ExpiresAt.Before(expires) {
		expires = job.ExpiresAt
	}
	state.OutputPath = outputPath
	state.OutputName = job.Filename
	state.OutputMIME = job.ContentType
	state.OutputTokenDigest = fetchTokenDigest(outputToken)
	state.OutputTokenExpiresAt = &expires
	if err := r.store.Save(state); err != nil {
		return FetchDownloadOutput{}, err
	}
	return FetchDownloadOutput{FetchID: fetchID, Status: job.Status, FilePath: outputPath, FileName: job.Filename, MIMEType: job.ContentType, Size: decrypt.PlainSize, SHA256: decrypt.PlainSHA256, OutputToken: outputToken, OutputExpiresAt: expires}, nil
}

func (r *FetchRequester) ConfirmMounted(ctx context.Context, fetchID string) (FetchJob, error) {
	state, err := r.store.Load(fetchID)
	if err != nil {
		return FetchJob{}, err
	}
	if state.OutputPath == "" || state.OutputTokenDigest == "" {
		return FetchJob{}, errors.New("artifact fetch has no local output")
	}
	info, err := os.Lstat(state.OutputPath)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return FetchJob{}, errors.New("artifact fetch local output is unavailable")
	}
	credentials, err := r.credentials()
	if err != nil {
		return FetchJob{}, err
	}
	job, err := r.client.ConfirmArtifactFetchMounted(ctx, credentials, fetchID, state.DownloadToken)
	if err != nil {
		return FetchJob{}, err
	}
	if err := r.store.Delete(fetchID); err != nil {
		return FetchJob{}, err
	}
	return job, nil
}

func randomFetchToken(size int) (string, error) {
	value := make([]byte, size)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(value), nil
}
