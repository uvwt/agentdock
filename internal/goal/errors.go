package goal

import (
	"errors"
	"fmt"
)

var (
	ErrGoalNotFound     = errors.New("goal not found")
	ErrStateConflict    = errors.New("goal state conflict")
	ErrLeaseRequired    = errors.New("reasoning lease required")
	ErrLeaseExpired     = errors.New("reasoning lease expired")
	ErrLeaseMismatch    = errors.New("reasoning lease mismatch")
	ErrInvalidStatus    = errors.New("invalid goal status transition")
	ErrInvalidInput     = errors.New("invalid goal input")
	ErrBudgetExceeded   = errors.New("goal budget exceeded")
	ErrPolicyDenied     = errors.New("goal policy denied")
	ErrVerifyFailed     = errors.New("goal verification failed")
	ErrApprovalRequired = errors.New("goal approval required")
)

// ConflictError carries structured conflict details for MCP mapping.
type ConflictError struct {
	Code            string
	Message         string
	ExpectedVersion int
	CurrentVersion  int
	GoalID          string
	LeaseID         string
	ActiveLeaseID   string
}

func (e *ConflictError) Error() string {
	if e.Message != "" {
		return e.Message
	}
	return ErrStateConflict.Error()
}

func (e *ConflictError) Unwrap() error { return ErrStateConflict }

func stateConflict(msg string, goal *Goal, expectedVersion int, leaseID string) error {
	err := &ConflictError{
		Code:            "STATE_CONFLICT",
		Message:         msg,
		ExpectedVersion: expectedVersion,
		GoalID:          "",
		LeaseID:         leaseID,
	}
	if goal != nil {
		err.GoalID = goal.ID
		err.CurrentVersion = goal.CapsuleVersion
		if goal.ActiveLease != nil {
			err.ActiveLeaseID = goal.ActiveLease.LeaseID
		}
	}
	return err
}

func invalidInput(msg string) error {
	return fmt.Errorf("%w: %s", ErrInvalidInput, msg)
}
