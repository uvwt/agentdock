package browser

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
)

func (s *Service) closeSession(req CloseRequest) (CloseResult, error) {
	sess, err := s.removeSession(req.SessionID)
	if err != nil {
		return CloseResult{}, err
	}
	sess.stop()
	s.releaseSessionProfile(sess)
	return CloseResult{SessionID: req.SessionID, Closed: true}, nil
}

func (s *Service) cleanupStale(req CleanupRequest) CleanupResult {
	maxAge := req.MaxAge
	if maxAge <= 0 {
		maxAge = defaultStaleAge
	}
	cutoff := s.now().Add(-maxAge)

	s.mu.Lock()
	var stale []*session
	for id, sess := range s.sessions {
		sess.mu.Lock()
		lastActivity := sess.lastActivity
		sess.mu.Unlock()
		if lastActivity.After(cutoff) {
			continue
		}
		delete(s.sessions, id)
		stale = append(stale, sess)
	}
	s.mu.Unlock()

	removed := make([]string, 0, len(stale))
	for _, sess := range stale {
		removed = append(removed, sess.id)
		sess.stop()
		s.releaseSessionProfile(sess)
	}
	sort.Strings(removed)
	return CleanupResult{RemovedCount: len(removed), RemovedSessions: removed}
}

func (s *Service) Close() error {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	s.closed = true
	sessions := make([]*session, 0, len(s.sessions))
	for _, sess := range s.sessions {
		sessions = append(sessions, sess)
	}
	s.sessions = make(map[string]*session)
	s.mu.Unlock()

	for _, sess := range sessions {
		sess.stop()
		s.releaseSessionProfile(sess)
	}
	s.mu.Lock()
	s.profiles = make(map[string]string)
	s.mu.Unlock()
	return nil
}

func (s *Service) getSession(id string) (*session, error) {
	id = strings.TrimSpace(id)
	s.mu.Lock()
	defer s.mu.Unlock()
	if sess := s.sessions[id]; sess != nil {
		return sess, nil
	}
	return nil, browserError(ErrSessionNotFound, "browser session was not found", "session", &ErrorDetails{SessionID: id}, nil)
}

func (s *Service) removeSession(id string) (*session, error) {
	id = strings.TrimSpace(id)
	s.mu.Lock()
	defer s.mu.Unlock()
	sess := s.sessions[id]
	if sess == nil {
		return nil, browserError(ErrSessionNotFound, "browser session was not found", "session", &ErrorDetails{SessionID: id}, nil)
	}
	delete(s.sessions, id)
	return sess, nil
}

func (s *Service) releaseSessionProfile(sess *session) {
	if sess == nil || sess.profileID == "" {
		return
	}
	s.mu.Lock()
	if s.profiles[sess.profileID] == sess.id {
		delete(s.profiles, sess.profileID)
	}
	s.mu.Unlock()
}

func (s *Service) reserveProfile(profileID string) (string, bool, error) {
	root := filepath.Join(s.cfg.AgentDockHome, "browser")
	if err := os.MkdirAll(root, 0o700); err != nil {
		return "", false, browserError(ErrLaunchFailed, "create browser state directory", "browser_launch", nil, err)
	}
	if profileID == "" {
		tempRoot := filepath.Join(root, "tmp")
		if err := os.MkdirAll(tempRoot, 0o700); err != nil {
			return "", false, browserError(ErrLaunchFailed, "create temporary browser directory", "browser_launch", nil, err)
		}
		return tempRoot, true, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if sessionID := s.profiles[profileID]; sessionID != "" {
		return "", false, browserError(ErrProfileInUse, "browser profile is already in use", "profile", &ErrorDetails{ProfileID: profileID, SessionID: sessionID}, nil)
	}
	// 先占位，避免两个并发 start 同时穿过 profile 检查。
	s.profiles[profileID] = "<starting>"
	return filepath.Join(root, "profiles", profileID), false, nil
}

func (s *Service) releaseProfile(profileID, profileDir string, temporary bool) {
	if profileID != "" {
		s.mu.Lock()
		if s.profiles[profileID] == "<starting>" {
			delete(s.profiles, profileID)
		}
		s.mu.Unlock()
	}
	if temporary && strings.Contains(profileDir, filepath.Join("browser", "tmp")) {
		_ = os.RemoveAll(profileDir)
	}
}
