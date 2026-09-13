package acp

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	acpruntime "github.com/uvwt/agentdock/internal/acp"
)

type Service struct {
	managers  map[string]*acpruntime.Manager
	defaultID string
}

func NewMulti(defaultID string, managers map[string]*acpruntime.Manager) *Service {
	copied := make(map[string]*acpruntime.Manager, len(managers))
	for id, manager := range managers {
		if manager != nil {
			copied[id] = manager
		}
	}
	return &Service{managers: copied, defaultID: strings.TrimSpace(defaultID)}
}

func (s *Service) Close() error {
	if s == nil || len(s.managers) == 0 {
		return nil
	}
	ids := make([]string, 0, len(s.managers))
	for id := range s.managers {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	var closeErrors []error
	for _, id := range ids {
		if err := s.managers[id].Close(); err != nil {
			closeErrors = append(closeErrors, fmt.Errorf("close ACP profile %s: %w", id, err))
		}
	}
	return errors.Join(closeErrors...)
}

func (s *Service) managerFor(profileID string) (*acpruntime.Manager, string, error) {
	if s == nil || len(s.managers) == 0 {
		return nil, "", validationError("ACP_NOT_CONFIGURED", "ACP runtime is not configured", nil)
	}
	profileID = strings.TrimSpace(profileID)
	if profileID == "" {
		profileID = s.defaultID
	}
	manager := s.managers[profileID]
	if manager == nil {
		return nil, "", validationError("ACP_PROFILE_NOT_FOUND", "ACP profile was not found", map[string]any{"profile_id": profileID})
	}
	return manager, profileID, nil
}
