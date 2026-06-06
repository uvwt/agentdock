package commandqueue

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	contracts "github.com/uvwt/agentdock/generated/nexuscontracts"
)

const commandDBVersion = 1

var ErrCommandNotFound = errors.New("command not found")

type Record struct {
	CommandID      string                   `json:"command_id"`
	IdempotencyKey string                   `json:"idempotency_key"`
	Type           string                   `json:"type"`
	Status         string                   `json:"status"`
	LeaseID        string                   `json:"lease_id,omitempty"`
	LeaseExpiresAt time.Time                `json:"lease_expires_at,omitempty"`
	StartedAt      time.Time                `json:"started_at,omitempty"`
	UpdatedAt      time.Time                `json:"updated_at"`
	Result         *contracts.CommandResult `json:"result,omitempty"`
}

func (r Record) Terminal() bool {
	switch r.Status {
	case StatusSucceeded, StatusFailed, StatusExpired, StatusCancelled:
		return true
	default:
		return false
	}
}

type commandDB struct {
	Version     int               `json:"version"`
	Commands    map[string]Record `json:"commands"`
	Idempotency map[string]string `json:"idempotency"`
}

type Store struct {
	path string
	now  func() time.Time
	mu   sync.Mutex
	db   commandDB
}

func OpenStore(stateDir string) (*Store, error) {
	if stateDir == "" {
		return nil, errors.New("state directory is required")
	}
	if err := os.MkdirAll(filepath.Join(stateDir, "locks"), 0o700); err != nil {
		return nil, fmt.Errorf("create command state directory: %w", err)
	}
	s := &Store{
		path: filepath.Join(stateDir, "commands.db"),
		now:  time.Now,
		db: commandDB{
			Version:     commandDBVersion,
			Commands:    map[string]Record{},
			Idempotency: map[string]string{},
		},
	}
	if err := s.load(); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *Store) load() error {
	b, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return s.persistLocked()
	}
	if err != nil {
		return fmt.Errorf("read command database: %w", err)
	}
	if len(b) == 0 {
		return errors.New("command database is empty")
	}
	if err := json.Unmarshal(b, &s.db); err != nil {
		return fmt.Errorf("decode command database: %w", err)
	}
	if s.db.Version != commandDBVersion {
		return fmt.Errorf("unsupported command database version %d", s.db.Version)
	}
	if s.db.Commands == nil {
		s.db.Commands = map[string]Record{}
	}
	if s.db.Idempotency == nil {
		s.db.Idempotency = map[string]string{}
	}
	return nil
}

// Begin atomically records a leased command as running. execute=false means
// the command is already in flight or has a durable terminal result and must
// not produce side effects again.
func (s *Store) Begin(lease contracts.CommandLease) (record Record, execute bool, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := s.now().UTC()
	cmd := lease.Command
	if cmd.Id == "" || cmd.IdempotencyKey == "" || lease.LeaseId == "" {
		return Record{}, false, errors.New("command id, idempotency key, and lease id are required")
	}
	leaseExpiresAt, err := time.Parse(time.RFC3339, lease.ExpiresAt)
	if err != nil {
		return Record{}, false, fmt.Errorf("invalid lease expires_at: %w", err)
	}

	if existingID, ok := s.db.Idempotency[cmd.IdempotencyKey]; ok {
		existing := s.db.Commands[existingID]
		if existing.Terminal() || existing.LeaseExpiresAt.After(now) {
			return existing, false, nil
		}
	}
	if existing, ok := s.db.Commands[cmd.Id]; ok {
		if existing.Terminal() || existing.LeaseExpiresAt.After(now) {
			return existing, false, nil
		}
	}

	record = Record{
		CommandID:      cmd.Id,
		IdempotencyKey: cmd.IdempotencyKey,
		Type:           cmd.Type,
		Status:         StatusRunning,
		LeaseID:        lease.LeaseId,
		LeaseExpiresAt: leaseExpiresAt,
		StartedAt:      now,
		UpdatedAt:      now,
	}
	s.db.Commands[cmd.Id] = record
	s.db.Idempotency[cmd.IdempotencyKey] = cmd.Id
	if err := s.persistLocked(); err != nil {
		return Record{}, false, err
	}
	return record, true, nil
}

func (s *Store) Renew(commandID, leaseID string, expiresAt time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	record, ok := s.db.Commands[commandID]
	if !ok {
		return ErrCommandNotFound
	}
	if record.LeaseID != leaseID {
		return errors.New("lease id mismatch")
	}
	if record.Terminal() {
		return errors.New("cannot renew terminal command")
	}
	record.LeaseExpiresAt = expiresAt
	record.UpdatedAt = s.now().UTC()
	s.db.Commands[commandID] = record
	return s.persistLocked()
}

func (s *Store) Complete(commandID string, result contracts.CommandResult) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	record, ok := s.db.Commands[commandID]
	if !ok {
		return ErrCommandNotFound
	}
	record.Status = result.Status
	record.Result = &result
	record.UpdatedAt = s.now().UTC()
	s.db.Commands[commandID] = record
	return s.persistLocked()
}

func (s *Store) Get(commandID string) (Record, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	record, ok := s.db.Commands[commandID]
	if !ok {
		return Record{}, ErrCommandNotFound
	}
	return record, nil
}

func (s *Store) persistLocked() error {
	b, err := json.MarshalIndent(s.db, "", "  ")
	if err != nil {
		return fmt.Errorf("encode command database: %w", err)
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return fmt.Errorf("write command database: %w", err)
	}
	if err := os.Chmod(tmp, 0o600); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("secure command database: %w", err)
	}
	if err := os.Rename(tmp, s.path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("replace command database: %w", err)
	}
	return nil
}
