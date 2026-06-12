package artifactrelay

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type UploadClient interface {
	CreateDeviceArtifactUpload(context.Context, DeviceCredentials, CreateUploadRequest) (CreateUploadResult, error)
	UploadArtifact(context.Context, string, string, string, UploadManifest) (UploadCompletion, error)
}

type Sender struct {
	client      UploadClient
	credentials func() (DeviceCredentials, error)
	tempRoot    string
}

type SenderConfig struct {
	Client      UploadClient
	Credentials func() (DeviceCredentials, error)
	TempRoot    string
}

type SendRequest struct {
	SourcePath              string
	TargetDeviceIDs         []string
	Dispatch                bool
	RetentionSeconds        int64
	DeleteAfterAllDelivered bool
	ConflictPolicy          string
	Extract                 bool
	LogicalTarget           string
}

type SendResult struct {
	Artifact   ArtifactRecord   `json:"artifact"`
	Deliveries []DeliveryRecord `json:"deliveries"`
	Source     string           `json:"source"`
	Archive    bool             `json:"archive"`
	Encrypted  bool             `json:"encrypted"`
}

func NewSender(config SenderConfig) (*Sender, error) {
	if config.Client == nil || config.Credentials == nil {
		return nil, errors.New("artifact upload client and credentials are required")
	}
	root, err := filepath.Abs(strings.TrimSpace(config.TempRoot))
	if err != nil || strings.TrimSpace(config.TempRoot) == "" {
		return nil, errors.New("artifact temporary root is invalid")
	}
	if err := ensureSecureDirectory(root); err != nil {
		return nil, err
	}
	return &Sender{client: config.Client, credentials: config.Credentials, tempRoot: root}, nil
}

func (s *Sender) Send(ctx context.Context, request SendRequest) (SendResult, error) {
	credentials, err := s.credentials()
	if err != nil {
		return SendResult{}, err
	}
	if credentials.DeviceID == "" || credentials.DeviceToken == "" {
		return SendResult{}, errors.New("valid Nexus device credentials are required")
	}
	prepared, err := PrepareSource(request.SourcePath, s.tempRoot)
	if err != nil {
		return SendResult{}, err
	}
	defer prepared.Cleanup()
	dispatch := request.Dispatch
	create, err := s.client.CreateDeviceArtifactUpload(ctx, credentials, CreateUploadRequest{
		Filename: prepared.Filename, ContentType: prepared.ContentType, TargetDeviceIDs: request.TargetDeviceIDs,
		Dispatch: &dispatch, RetentionSeconds: request.RetentionSeconds, DeleteAfterAllDelivered: request.DeleteAfterAllDelivered,
		ConflictPolicy: request.ConflictPolicy, Extract: request.Extract, LogicalTarget: request.LogicalTarget,
	})
	if err != nil {
		return SendResult{}, err
	}
	if create.Artifact.ID == "" || create.UploadPath == "" || create.UploadToken == "" || len(create.Targets) == 0 {
		return SendResult{}, errors.New("Nexus artifact upload response is incomplete")
	}
	encrypted, err := os.CreateTemp(s.tempRoot, "artifact-encrypted-*.adr")
	if err != nil {
		return SendResult{}, err
	}
	encryptedPath := encrypted.Name()
	if err := encrypted.Close(); err != nil {
		os.Remove(encryptedPath)
		return SendResult{}, err
	}
	defer os.Remove(encryptedPath)
	cryptoResult, err := EncryptFile(prepared.Path, encryptedPath)
	if err != nil {
		return SendResult{}, err
	}
	defer zeroBytes(cryptoResult.FileKey)
	defer zeroBytes(cryptoResult.EphemeralPrivateKey)
	if cryptoResult.CipherSize > MaxCipherBytes {
		return SendResult{}, fmt.Errorf("encrypted artifact exceeds %d bytes", MaxCipherBytes)
	}
	wrapped := make([]WrappedKeyManifest, 0, len(create.Targets))
	for _, target := range create.Targets {
		wrappedKey, nonce, err := WrapFileKey(
			cryptoResult.EphemeralPrivateKey, target.X25519PublicKey, cryptoResult.FileKey,
			create.Artifact.ID, target.DeliveryID, target.TargetDeviceID,
		)
		if err != nil {
			return SendResult{}, fmt.Errorf("wrap file key for target %s: %w", target.TargetDeviceID, err)
		}
		wrapped = append(wrapped, WrappedKeyManifest{DeliveryID: target.DeliveryID, TargetDeviceID: target.TargetDeviceID, WrappedKey: wrappedKey, WrapNonce: nonce})
	}
	completion, err := s.client.UploadArtifact(ctx, create.UploadPath, create.UploadToken, encryptedPath, UploadManifest{
		FormatVersion: FormatVersion, CipherAlgorithm: CipherAlgorithm, PlainSize: cryptoResult.PlainSize,
		PlainSHA256: cryptoResult.PlainSHA256, EphemeralPublicKey: cryptoResult.EphemeralPublicKey, WrappedKeys: wrapped,
	})
	if err != nil {
		return SendResult{}, err
	}
	return SendResult{Artifact: completion.Artifact, Deliveries: completion.Deliveries, Source: prepared.Filename, Archive: prepared.Archive, Encrypted: true}, nil
}
