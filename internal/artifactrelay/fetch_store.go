package artifactrelay

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type FetchStore struct {
	root string
	mu   sync.Mutex
}

func OpenFetchStore(root string) (*FetchStore, error) {
	absolute, err := filepath.Abs(strings.TrimSpace(root))
	if err != nil || strings.TrimSpace(root) == "" {
		return nil, errors.New("artifact fetch state root is invalid")
	}
	if err := os.MkdirAll(absolute, 0o700); err != nil {
		return nil, fmt.Errorf("create fetch state root: %w", err)
	}
	if err := os.Chmod(absolute, 0o700); err != nil {
		return nil, fmt.Errorf("secure fetch state root: %w", err)
	}
	return &FetchStore{root: absolute}, nil
}

func (s *FetchStore) Save(state FetchLocalState) error {
	if !safeID.MatchString(state.FetchID) {
		return errors.New("fetch id is invalid")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	dir := filepath.Join(s.root, state.FetchID)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	encoded, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	return atomicWrite(filepath.Join(dir, "state.json"), append(encoded, '\n'), 0o600)
}

func (s *FetchStore) Load(fetchID string) (FetchLocalState, error) {
	if !safeID.MatchString(fetchID) {
		return FetchLocalState{}, errors.New("fetch id is invalid")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	data, err := os.ReadFile(filepath.Join(s.root, fetchID, "state.json"))
	if errors.Is(err, os.ErrNotExist) {
		return FetchLocalState{}, errors.New("local artifact fetch state not found")
	}
	if err != nil {
		return FetchLocalState{}, err
	}
	var state FetchLocalState
	if err := json.Unmarshal(data, &state); err != nil {
		return FetchLocalState{}, fmt.Errorf("decode fetch state: %w", err)
	}
	if state.FetchID != fetchID {
		return FetchLocalState{}, errors.New("artifact fetch state id mismatch")
	}
	return state, nil
}

func (s *FetchStore) Delete(fetchID string) error {
	if !safeID.MatchString(fetchID) {
		return errors.New("fetch id is invalid")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return os.RemoveAll(filepath.Join(s.root, fetchID))
}

func (s *FetchStore) OutputDir(fetchID string) (string, error) {
	if !safeID.MatchString(fetchID) {
		return "", errors.New("fetch id is invalid")
	}
	dir := filepath.Join(s.root, fetchID, "output")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	return dir, os.Chmod(dir, 0o700)
}

func (s *FetchStore) ResolveOutput(fetchID, token string, now time.Time) (FetchLocalState, error) {
	state, err := s.Load(fetchID)
	if err != nil {
		return FetchLocalState{}, err
	}
	if state.OutputTokenDigest == "" || state.OutputTokenExpiresAt == nil || !now.Before(*state.OutputTokenExpiresAt) {
		return FetchLocalState{}, errors.New("artifact fetch output token expired")
	}
	actual := fetchTokenDigest(token)
	if len(actual) != len(state.OutputTokenDigest) || subtle.ConstantTimeCompare([]byte(actual), []byte(state.OutputTokenDigest)) != 1 {
		return FetchLocalState{}, errors.New("artifact fetch output token is invalid")
	}
	path, err := filepath.Abs(state.OutputPath)
	if err != nil {
		return FetchLocalState{}, err
	}
	root := filepath.Join(s.root, fetchID, "output")
	relative, err := filepath.Rel(root, path)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(os.PathSeparator)) {
		return FetchLocalState{}, errors.New("artifact fetch output escapes state root")
	}
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return FetchLocalState{}, errors.New("artifact fetch output is unavailable")
	}
	return state, nil
}

func fetchTokenDigest(value string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(value)))
	return hex.EncodeToString(sum[:])
}
