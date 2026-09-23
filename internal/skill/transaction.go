package skill

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	skillstate "github.com/uvwt/agentdock/internal/skill/state"
)

func (m *Manager) recoverInterruptedSwaps() error {
	transactions, err := m.State.ListSwapTransactions()
	if err != nil {
		return fmt.Errorf("load Skill swap transactions: %w", err)
	}
	for _, transaction := range transactions {
		ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
		release, lockErr := m.State.AcquireWrite(ctx, transaction.Skill)
		cancel()
		if lockErr != nil {
			if errors.Is(lockErr, context.DeadlineExceeded) {
				// Another live process still owns the Skill lifecycle writer.
				continue
			}
			return fmt.Errorf("lock Skill swap transaction %s: %w", transaction.Skill, lockErr)
		}
		recoverErr := m.recoverSwapLocked(transaction.Skill)
		release()
		if recoverErr != nil {
			return fmt.Errorf("recover Skill swap %s: %w", transaction.Skill, recoverErr)
		}
	}
	return nil
}

func (m *Manager) recoverSwapLocked(skill string) error {
	transaction, err := m.State.LoadSwapTransaction(skill)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	destination, err := m.State.SkillPath(skill)
	if err != nil {
		return err
	}
	backup, err := m.State.SwapBackupPath(skill)
	if err != nil {
		return err
	}

	switch transaction.Phase {
	case "prepared":
		backupDigest, backupExists, err := installedContentDigest(backup)
		if err != nil {
			return fmt.Errorf("inspect Skill swap backup: %w", err)
		}
		currentDigest, currentExists, err := installedContentDigest(destination)
		if err != nil {
			return fmt.Errorf("inspect Skill current content: %w", err)
		}

		if backupExists {
			if backupDigest != transaction.PreviousDigest {
				return errors.New("Skill swap backup does not match Previous digest")
			}
			if currentExists {
				switch currentDigest {
				case transaction.PreviousDigest:
					// Rollback already completed before the process died. Keep the
					// verified Previous and discard only the duplicate backup.
					if err := os.RemoveAll(backup); err != nil {
						return fmt.Errorf("remove recovered Skill backup: %w", err)
					}
				case transaction.CandidateDigest:
					if err := os.RemoveAll(destination); err != nil {
						return fmt.Errorf("remove uncommitted Skill candidate: %w", err)
					}
					if err := os.Rename(backup, destination); err != nil {
						return fmt.Errorf("restore interrupted Skill current content: %w", err)
					}
				default:
					return errors.New("Skill current content does not match prepared swap journal")
				}
			} else if err := os.Rename(backup, destination); err != nil {
				return fmt.Errorf("restore interrupted Skill current content: %w", err)
			}
		} else {
			if !currentExists || currentDigest != transaction.PreviousDigest {
				return errors.New("prepared Skill swap has neither a valid Previous current package nor a recoverable backup")
			}
		}

		restoredDigest, restored, err := installedContentDigest(destination)
		if err != nil {
			return fmt.Errorf("verify restored Skill current content: %w", err)
		}
		if !restored || restoredDigest != transaction.PreviousDigest {
			return errors.New("restored Skill current content does not match Previous digest")
		}

	case "candidate_published":
		currentDigest, currentExists, err := installedContentDigest(destination)
		if err != nil {
			return fmt.Errorf("inspect published Skill candidate: %w", err)
		}
		if !currentExists || currentDigest != transaction.CandidateDigest {
			return errors.New("published Skill candidate does not match durable swap transaction")
		}
		backupDigest, backupExists, err := installedContentDigest(backup)
		if err != nil {
			return fmt.Errorf("inspect committed Skill swap backup: %w", err)
		}
		if backupExists {
			if backupDigest != transaction.PreviousDigest {
				return errors.New("committed Skill backup does not match Previous digest")
			}
			if err := os.RemoveAll(backup); err != nil {
				return fmt.Errorf("remove committed Skill swap backup: %w", err)
			}
		}
	default:
		return fmt.Errorf("unsupported Skill swap phase %q", transaction.Phase)
	}
	return m.State.DeleteSwapTransaction(skill)
}

func newSkillSwapTransaction(skill, previousDigest, candidateDigest string) skillstate.SwapTransaction {
	return skillstate.SwapTransaction{
		SchemaVersion:   skillstate.SwapTransactionSchemaVersion,
		Skill:           skill,
		Phase:           "prepared",
		PreviousDigest:  previousDigest,
		CandidateDigest: candidateDigest,
		CreatedAt:       time.Now().UTC(),
	}
}
