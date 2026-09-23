package state

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/uvwt/agentdock/internal/fs/atomicfile"
)

const SwapTransactionSchemaVersion = 1

type SwapTransaction struct {
	SchemaVersion   int       `json:"schema_version"`
	Skill           string    `json:"skill"`
	Phase           string    `json:"phase"`
	PreviousDigest  string    `json:"previous_digest"`
	CandidateDigest string    `json:"candidate_digest"`
	CreatedAt       time.Time `json:"created_at"`
}

func (s *Store) SwapBackupPath(skill string) (string, error) {
	if err := validateIdentifier("skill", skill); err != nil {
		return "", err
	}
	return filepath.Join(s.tempRoot, "replace-"+skill+".backup"), nil
}

func (s *Store) SaveSwapTransaction(transaction SwapTransaction) error {
	if err := validateSwapTransaction(transaction); err != nil {
		return err
	}
	data, err := json.MarshalIndent(transaction, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return atomicfile.Write(filepath.Join(s.transactionRoot, transaction.Skill+".json"), data, 0o600)
}

func (s *Store) LoadSwapTransaction(skill string) (SwapTransaction, error) {
	if err := validateIdentifier("skill", skill); err != nil {
		return SwapTransaction{}, err
	}
	return readSwapTransaction(filepath.Join(s.transactionRoot, skill+".json"))
}

func (s *Store) DeleteSwapTransaction(skill string) error {
	if err := validateIdentifier("skill", skill); err != nil {
		return err
	}
	if err := os.Remove(filepath.Join(s.transactionRoot, skill+".json")); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

func (s *Store) ListSwapTransactions() ([]SwapTransaction, error) {
	entries, err := os.ReadDir(s.transactionRoot)
	if errors.Is(err, os.ErrNotExist) {
		return []SwapTransaction{}, nil
	}
	if err != nil {
		return nil, err
	}
	items := make([]SwapTransaction, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		transaction, err := readSwapTransaction(filepath.Join(s.transactionRoot, entry.Name()))
		if err != nil {
			return nil, err
		}
		items = append(items, transaction)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].Skill < items[j].Skill })
	return items, nil
}

func readSwapTransaction(path string) (SwapTransaction, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return SwapTransaction{}, err
	}
	var transaction SwapTransaction
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&transaction); err != nil {
		return SwapTransaction{}, fmt.Errorf("decode Skill swap transaction: %w", err)
	}
	if err := validateSwapTransaction(transaction); err != nil {
		return SwapTransaction{}, err
	}
	return transaction, nil
}

func validateSwapTransaction(transaction SwapTransaction) error {
	if transaction.SchemaVersion != SwapTransactionSchemaVersion {
		return fmt.Errorf("unsupported Skill swap transaction schema %d", transaction.SchemaVersion)
	}
	if err := validateIdentifier("skill", transaction.Skill); err != nil {
		return err
	}
	if transaction.Phase != "prepared" && transaction.Phase != "candidate_published" {
		return fmt.Errorf("invalid Skill swap transaction phase %q", transaction.Phase)
	}
	if transaction.PreviousDigest == "" || transaction.CandidateDigest == "" {
		return errors.New("Skill swap transaction digests are required")
	}
	if transaction.CreatedAt.IsZero() {
		return errors.New("Skill swap transaction created_at is required")
	}
	return nil
}
