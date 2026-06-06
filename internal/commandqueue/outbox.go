package commandqueue

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

type OutboxKind string

const (
	OutboxHeartbeat          OutboxKind = "heartbeat"
	OutboxCommandResult      OutboxKind = "command_result"
	OutboxRunResult          OutboxKind = "run_result"
	OutboxObservation        OutboxKind = "observation"
	OutboxInstallationResult OutboxKind = "installation_result"
)

type Envelope struct {
	ID          string          `json:"id"`
	Kind        OutboxKind      `json:"kind"`
	Key         string          `json:"key,omitempty"`
	Payload     json.RawMessage `json:"payload"`
	CreatedAt   time.Time       `json:"created_at"`
	Attempts    int             `json:"attempts"`
	LastError   string          `json:"last_error,omitempty"`
	LastAttempt *time.Time      `json:"last_attempt,omitempty"`
}

type Outbox struct {
	dir string
	now func() time.Time
	mu  sync.Mutex
}

func OpenOutbox(stateDir string) (*Outbox, error) {
	if stateDir == "" {
		return nil, errors.New("state directory is required")
	}
	dir := filepath.Join(stateDir, "outbox")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("create outbox: %w", err)
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		return nil, fmt.Errorf("secure outbox: %w", err)
	}
	return &Outbox{dir: dir, now: time.Now}, nil
}

func (o *Outbox) Put(kind OutboxKind, key string, payload any) (Envelope, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	b, err := json.Marshal(payload)
	if err != nil {
		return Envelope{}, fmt.Errorf("encode outbox payload: %w", err)
	}
	id, err := envelopeID(kind, key)
	if err != nil {
		return Envelope{}, err
	}
	env := Envelope{ID: id, Kind: kind, Key: key, Payload: b, CreatedAt: o.now().UTC()}
	if err := o.writeLocked(env); err != nil {
		return Envelope{}, err
	}
	return env, nil
}

func (o *Outbox) Pending(limit int) ([]Envelope, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	entries, err := os.ReadDir(o.dir)
	if err != nil {
		return nil, fmt.Errorf("read outbox: %w", err)
	}
	items := make([]Envelope, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		b, err := os.ReadFile(filepath.Join(o.dir, entry.Name()))
		if err != nil {
			return nil, fmt.Errorf("read outbox item %s: %w", entry.Name(), err)
		}
		var env Envelope
		if err := json.Unmarshal(b, &env); err != nil {
			return nil, fmt.Errorf("decode outbox item %s: %w", entry.Name(), err)
		}
		items = append(items, env)
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].CreatedAt.Equal(items[j].CreatedAt) {
			return items[i].ID < items[j].ID
		}
		return items[i].CreatedAt.Before(items[j].CreatedAt)
	})
	if limit > 0 && len(items) > limit {
		items = items[:limit]
	}
	return items, nil
}

func (o *Outbox) Ack(id string) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	if !validEnvelopeID(id) {
		return errors.New("invalid outbox id")
	}
	err := os.Remove(filepath.Join(o.dir, id+".json"))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

func (o *Outbox) MarkAttempt(env Envelope, uploadErr error) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	now := o.now().UTC()
	env.Attempts++
	env.LastAttempt = &now
	if uploadErr != nil {
		env.LastError = uploadErr.Error()
		if len(env.LastError) > 1024 {
			env.LastError = env.LastError[:1024]
		}
	}
	return o.writeLocked(env)
}

type Uploader func(context.Context, Envelope) error

func (o *Outbox) Drain(ctx context.Context, limit int, upload Uploader) (int, error) {
	items, err := o.Pending(limit)
	if err != nil {
		return 0, err
	}
	uploaded := 0
	for _, env := range items {
		if err := ctx.Err(); err != nil {
			return uploaded, err
		}
		if err := upload(ctx, env); err != nil {
			_ = o.MarkAttempt(env, err)
			return uploaded, err
		}
		if err := o.Ack(env.ID); err != nil {
			return uploaded, err
		}
		uploaded++
	}
	return uploaded, nil
}

func (o *Outbox) writeLocked(env Envelope) error {
	if !validEnvelopeID(env.ID) {
		return errors.New("invalid outbox id")
	}
	b, err := json.MarshalIndent(env, "", "  ")
	if err != nil {
		return err
	}
	path := filepath.Join(o.dir, env.ID+".json")
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return fmt.Errorf("write outbox item: %w", err)
	}
	if err := os.Chmod(tmp, 0o600); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

func envelopeID(kind OutboxKind, key string) (string, error) {
	if kind == "" {
		return "", errors.New("outbox kind is required")
	}
	if key == "" {
		random := make([]byte, 16)
		if _, err := rand.Read(random); err != nil {
			return "", fmt.Errorf("generate outbox id: %w", err)
		}
		key = hex.EncodeToString(random)
	}
	sum := sha256.Sum256([]byte(string(kind) + "\x00" + key))
	return hex.EncodeToString(sum[:16]), nil
}

func validEnvelopeID(id string) bool {
	if len(id) != 32 {
		return false
	}
	_, err := hex.DecodeString(id)
	return err == nil
}
