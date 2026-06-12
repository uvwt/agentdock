package artifactrelay

import "time"

const (
	FormatVersion     = "ADR1"
	CipherAlgorithm   = "AES-256-GCM-CHUNKED"
	DefaultChunkSize  = 1 << 20
	MaxCipherBytes    = int64(500 << 20)
	MaxExtractedBytes = int64(2 << 30)
	MaxExtractedFiles = 10000
)

type DeviceCredentials struct {
	DeviceID    string
	DeviceToken string
}

type UploadTarget struct {
	DeliveryID      string `json:"delivery_id"`
	TargetDeviceID  string `json:"target_device_id"`
	X25519PublicKey string `json:"x25519_public_key"`
}

type ArtifactRecord struct {
	ID           string `json:"id"`
	Filename     string `json:"filename"`
	Status       string `json:"status"`
	ExpiresAt    string `json:"expires_at"`
	PlainSize    int64  `json:"plain_size"`
	PlainSHA256  string `json:"plain_sha256"`
	CipherSize   int64  `json:"cipher_size"`
	CipherSHA256 string `json:"cipher_sha256"`
}

type DeliveryRecord struct {
	ID             string `json:"id"`
	ArtifactID     string `json:"artifact_id"`
	TargetDeviceID string `json:"target_device_id"`
	Status         string `json:"status"`
	LocalPath      string `json:"local_path,omitempty"`
	ErrorCode      string `json:"error_code,omitempty"`
	ErrorMessage   string `json:"error_message,omitempty"`
}

type CreateUploadRequest struct {
	Filename                string   `json:"filename"`
	ContentType             string   `json:"content_type,omitempty"`
	TargetDeviceIDs         []string `json:"target_device_ids"`
	Dispatch                *bool    `json:"dispatch,omitempty"`
	RetentionSeconds        int64    `json:"retention_seconds,omitempty"`
	DeleteAfterAllDelivered bool     `json:"delete_after_all_delivered,omitempty"`
	ConflictPolicy          string   `json:"conflict_policy,omitempty"`
	Extract                 bool     `json:"extract,omitempty"`
	LogicalTarget           string   `json:"logical_target,omitempty"`
}

type CreateUploadResult struct {
	Artifact    ArtifactRecord   `json:"artifact"`
	Deliveries  []DeliveryRecord `json:"deliveries"`
	Targets     []UploadTarget   `json:"targets"`
	UploadToken string           `json:"upload_token"`
	UploadPath  string           `json:"upload_path"`
}

type WrappedKeyManifest struct {
	DeliveryID     string `json:"delivery_id"`
	TargetDeviceID string `json:"target_device_id"`
	WrappedKey     string `json:"wrapped_key"`
	WrapNonce      string `json:"wrap_nonce"`
}

type UploadManifest struct {
	FormatVersion      string               `json:"format_version"`
	CipherAlgorithm    string               `json:"cipher_algorithm"`
	PlainSize          int64                `json:"plain_size"`
	PlainSHA256        string               `json:"plain_sha256"`
	EphemeralPublicKey string               `json:"ephemeral_public_key"`
	WrappedKeys        []WrappedKeyManifest `json:"wrapped_keys"`
}

type UploadCompletion struct {
	Artifact   ArtifactRecord   `json:"artifact"`
	Deliveries []DeliveryRecord `json:"deliveries"`
}

type PullPayload struct {
	ArtifactID         string `json:"artifact_id"`
	DeliveryID         string `json:"delivery_id"`
	Filename           string `json:"filename"`
	ContentType        string `json:"content_type"`
	CipherSize         int64  `json:"cipher_size"`
	CipherSHA256       string `json:"cipher_sha256"`
	PlainSize          int64  `json:"plain_size"`
	PlainSHA256        string `json:"plain_sha256"`
	EphemeralPublicKey string `json:"ephemeral_public_key"`
	WrappedKey         string `json:"wrapped_key"`
	WrapNonce          string `json:"wrap_nonce"`
	DownloadToken      string `json:"download_token"`
	DownloadPath       string `json:"download_path"`
	ResultPath         string `json:"result_path"`
	ExpiresAt          string `json:"expires_at"`
	ConflictPolicy     string `json:"conflict_policy"`
	Extract            bool   `json:"extract"`
	LogicalTarget      string `json:"logical_target"`
}

type DownloadResult struct {
	Bytes        int64
	CipherSHA256 string
	PlainSHA256  string
}

type DeliveryResultRequest struct {
	Status       string `json:"status"`
	LocalPath    string `json:"local_path,omitempty"`
	ErrorCode    string `json:"error_code,omitempty"`
	ErrorMessage string `json:"error_message,omitempty"`
}

type PullResult struct {
	ArtifactID  string    `json:"artifact_id"`
	DeliveryID  string    `json:"delivery_id"`
	Path        string    `json:"path"`
	PlainSize   int64     `json:"plain_size"`
	PlainSHA256 string    `json:"plain_sha256"`
	Extracted   bool      `json:"extracted"`
	CompletedAt time.Time `json:"completed_at"`
}
