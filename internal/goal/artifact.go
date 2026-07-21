package goal

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/uvwt/agentdock/internal/atomicfile"
)

const (
	maxArtifactBytes     = 32 << 20
	maxArtifactNameBytes = 512
	artifactURIPrefix    = "artifact://sha256/"
)

// ArtifactStore is a content-addressed blob store under root/sha256/xx/....
type ArtifactStore struct {
	root string
	mu   sync.Mutex
}

// ArtifactMeta describes a stored blob.
type ArtifactMeta struct {
	SHA256      string    `json:"sha256"`
	URI         string    `json:"uri"`
	Filename    string    `json:"filename,omitempty"`
	ContentType string    `json:"content_type,omitempty"`
	Size        int64     `json:"size_bytes"`
	Kind        string    `json:"kind,omitempty"`
	Summary     string    `json:"summary,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
	Path        string    `json:"path,omitempty"` // relative storage path
}

// NewArtifactStore creates ~/.agentdock/artifacts style store.
func NewArtifactStore(root string) (*ArtifactStore, error) {
	if strings.TrimSpace(root) == "" {
		return nil, errors.New("artifact root is required")
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Join(abs, "sha256"), 0o700); err != nil {
		return nil, fmt.Errorf("create artifact root: %w", err)
	}
	if err := os.Chmod(abs, 0o700); err != nil {
		return nil, err
	}
	return &ArtifactStore{root: abs}, nil
}

func (s *ArtifactStore) Root() string {
	if s == nil {
		return ""
	}
	return s.root
}

// PutBytes stores content and returns metadata. Identical content is deduplicated.
func (s *ArtifactStore) PutBytes(filename string, data []byte, kind, summary, contentType string) (ArtifactMeta, error) {
	if s == nil {
		return ArtifactMeta{}, errors.New("artifact store is nil")
	}
	if len(data) == 0 {
		return ArtifactMeta{}, invalidInput("artifact data is empty")
	}
	if len(data) > maxArtifactBytes {
		return ArtifactMeta{}, invalidInput(fmt.Sprintf("artifact exceeds %d bytes", maxArtifactBytes))
	}
	filename = filepath.Base(strings.TrimSpace(filename))
	if err := validateTextLimit("artifact filename", filename, maxArtifactNameBytes); err != nil {
		return ArtifactMeta{}, err
	}
	if summary != "" && !utf8.ValidString(summary) {
		return ArtifactMeta{}, invalidInput("artifact summary is not valid UTF-8")
	}

	sum := sha256.Sum256(data)
	hexSum := hex.EncodeToString(sum[:])
	return s.putHashed(hexSum, filename, data, kind, summary, contentType)
}

// PutFile reads a local file into the store.
func (s *ArtifactStore) PutFile(path, kind, summary string) (ArtifactMeta, error) {
	if s == nil {
		return ArtifactMeta{}, errors.New("artifact store is nil")
	}
	path = strings.TrimSpace(path)
	if path == "" {
		return ArtifactMeta{}, invalidInput("path is required")
	}
	f, err := os.Open(path)
	if err != nil {
		return ArtifactMeta{}, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return ArtifactMeta{}, err
	}
	if info.IsDir() {
		return ArtifactMeta{}, invalidInput("artifact path must be a file")
	}
	if info.Size() > maxArtifactBytes {
		return ArtifactMeta{}, invalidInput(fmt.Sprintf("artifact exceeds %d bytes", maxArtifactBytes))
	}
	data, err := io.ReadAll(io.LimitReader(f, maxArtifactBytes+1))
	if err != nil {
		return ArtifactMeta{}, err
	}
	if len(data) > maxArtifactBytes {
		return ArtifactMeta{}, invalidInput(fmt.Sprintf("artifact exceeds %d bytes", maxArtifactBytes))
	}
	ct := "application/octet-stream"
	name := filepath.Base(path)
	return s.PutBytes(name, data, kind, summary, ct)
}

// GetMeta loads metadata for a sha256 hex digest or artifact:// URI.
func (s *ArtifactStore) GetMeta(ref string) (ArtifactMeta, error) {
	sum, err := normalizeArtifactRef(ref)
	if err != nil {
		return ArtifactMeta{}, err
	}
	metaPath := s.metaPath(sum)
	data, err := os.ReadFile(metaPath)
	if err != nil {
		if os.IsNotExist(err) {
			return ArtifactMeta{}, fmt.Errorf("artifact not found: %s", sum)
		}
		return ArtifactMeta{}, err
	}
	var meta ArtifactMeta
	if err := json.Unmarshal(data, &meta); err != nil {
		return ArtifactMeta{}, err
	}
	return meta, nil
}

// Open returns a reader for the blob.
func (s *ArtifactStore) Open(ref string) (*os.File, ArtifactMeta, error) {
	meta, err := s.GetMeta(ref)
	if err != nil {
		return nil, ArtifactMeta{}, err
	}
	f, err := os.Open(s.blobPath(meta.SHA256))
	if err != nil {
		return nil, ArtifactMeta{}, err
	}
	return f, meta, nil
}

// EvidenceFromMeta builds a goal evidence ref pointing at the artifact URI.
func EvidenceFromMeta(meta ArtifactMeta, criterionID string, data map[string]any) EvidenceRef {
	if data == nil {
		data = map[string]any{}
	}
	data["sha256"] = meta.SHA256
	data["size_bytes"] = meta.Size
	if criterionID != "" {
		data["criterion_id"] = criterionID
	}
	return EvidenceRef{
		Kind:      firstNonEmptyStr(meta.Kind, "artifact"),
		Summary:   firstNonEmptyStr(meta.Summary, meta.Filename, meta.SHA256[:12]),
		URI:       meta.URI,
		Data:      data,
		CreatedAt: meta.CreatedAt,
	}
}

func (s *ArtifactStore) putHashed(hexSum, filename string, data []byte, kind, summary, contentType string) (ArtifactMeta, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	blob := s.blobPath(hexSum)
	metaPath := s.metaPath(hexSum)
	if _, err := os.Stat(metaPath); err == nil {
		// already present
		var existing ArtifactMeta
		raw, err := os.ReadFile(metaPath)
		if err == nil && json.Unmarshal(raw, &existing) == nil {
			return existing, nil
		}
	}
	if err := os.MkdirAll(filepath.Dir(blob), 0o700); err != nil {
		return ArtifactMeta{}, err
	}
	if _, err := os.Stat(blob); err != nil {
		if err := atomicfile.Write(blob, data, 0o600); err != nil {
			return ArtifactMeta{}, err
		}
	}
	now := time.Now().UTC()
	meta := ArtifactMeta{
		SHA256:      hexSum,
		URI:         artifactURIPrefix + hexSum,
		Filename:    filename,
		ContentType: contentType,
		Size:        int64(len(data)),
		Kind:        strings.TrimSpace(kind),
		Summary:     strings.TrimSpace(summary),
		CreatedAt:   now,
		Path:        filepath.ToSlash(filepath.Join("sha256", hexSum[:2], hexSum)),
	}
	raw, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return ArtifactMeta{}, err
	}
	raw = append(raw, '\n')
	if err := atomicfile.Write(metaPath, raw, 0o600); err != nil {
		return ArtifactMeta{}, err
	}
	return meta, nil
}

func (s *ArtifactStore) blobPath(sum string) string {
	return filepath.Join(s.root, "sha256", sum[:2], sum)
}

func (s *ArtifactStore) metaPath(sum string) string {
	return filepath.Join(s.root, "sha256", sum[:2], sum+".json")
}

func normalizeArtifactRef(ref string) (string, error) {
	ref = strings.TrimSpace(ref)
	ref = strings.TrimPrefix(ref, artifactURIPrefix)
	ref = strings.TrimPrefix(ref, "sha256:")
	ref = strings.ToLower(ref)
	if len(ref) != 64 {
		return "", invalidInput("artifact ref must be sha256 hex or artifact://sha256/<hex>")
	}
	for _, r := range ref {
		if (r >= '0' && r <= '9') || (r >= 'a' && r <= 'f') {
			continue
		}
		return "", invalidInput("artifact ref is not hex")
	}
	return ref, nil
}
