package nexusclient

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type DeviceState struct {
	DeviceID       string     `json:"device_id"`
	DeviceToken    string     `json:"device_token"`
	TokenExpiresAt *time.Time `json:"token_expires_at,omitempty"`
	EnrolledAt     time.Time  `json:"enrolled_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
	Revoked        bool       `json:"revoked,omitempty"`
}

func (s DeviceState) Valid(now time.Time) bool {
	if s.DeviceID == "" || s.DeviceToken == "" || s.Revoked {
		return false
	}
	return s.TokenExpiresAt == nil || s.TokenExpiresAt.After(now)
}

type StateStore struct {
	path string
	now  func() time.Time
	mu   sync.Mutex
}

func OpenStateStore(stateDir string) (*StateStore, error) {
	if stateDir == "" {
		return nil, errors.New("state directory is required")
	}
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		return nil, fmt.Errorf("create nexus state directory: %w", err)
	}
	if err := os.Chmod(stateDir, 0o700); err != nil {
		return nil, fmt.Errorf("secure nexus state directory: %w", err)
	}
	return &StateStore{path: filepath.Join(stateDir, "device.json"), now: time.Now}, nil
}

func (s *StateStore) Load() (DeviceState, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	b, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return DeviceState{}, nil
	}
	if err != nil {
		return DeviceState{}, fmt.Errorf("read device state: %w", err)
	}
	var state DeviceState
	if err := json.Unmarshal(b, &state); err != nil {
		return DeviceState{}, fmt.Errorf("decode device state: %w", err)
	}
	return state, nil
}

func (s *StateStore) Save(state DeviceState) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now().UTC()
	if state.EnrolledAt.IsZero() {
		state.EnrolledAt = now
	}
	state.UpdatedAt = now
	b, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("encode device state: %w", err)
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return fmt.Errorf("write device state: %w", err)
	}
	if err := os.Chmod(tmp, 0o600); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("secure device state: %w", err)
	}
	if err := os.Rename(tmp, s.path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("replace device state: %w", err)
	}
	return nil
}

func (s *StateStore) Revoke() error {
	state, err := s.Load()
	if err != nil {
		return err
	}
	state.Revoked = true
	state.DeviceToken = ""
	state.TokenExpiresAt = nil
	return s.Save(state)
}
