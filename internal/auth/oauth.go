package auth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/uvwt/agentdock/internal/atomicfile"
)

const (
	shortClientPrefix = "adcr_"
	oauthStateVersion = 1
	maxOAuthStateSize = 1 << 20
	maxOAuthGrants    = 1024
	maxOAuthClients   = 1024
	maxOAuthCodes     = 1024

	oauthClientIdleTTL       = 180 * 24 * time.Hour
	oauthClientTouchInterval = 24 * time.Hour
)

type OAuthStore struct {
	mu         sync.Mutex
	codes      map[string]OAuthCode
	grants     map[string]OAuthGrant
	clients    map[string]OAuthClientRegistration
	statePath  string
	signingKey string
}

type OAuthCode struct {
	ClientID    string
	RedirectURI string
	Challenge   string
	Resource    string
	GrantID     string
	Redeemed    bool
	ExpiresAt   time.Time
}

type OAuthGrant struct {
	ClientID          string `json:"client_id"`
	Resource          string `json:"resource"`
	CurrentGeneration uint64 `json:"current_generation"`
	ExpiresAt         int64  `json:"expires_at"`
	Revoked           bool   `json:"revoked,omitempty"`
}

type OAuthClientRegistration struct {
	ClientName   string   `json:"client_name,omitempty"`
	RedirectURIs []string `json:"redirect_uris"`
	GrantTypes   []string `json:"grant_types"`
	IssuedAt     int64    `json:"issued_at"`
	LastUsedAt   int64    `json:"last_used_at,omitempty"`
}

type oauthState struct {
	Version int                                `json:"version"`
	Grants  map[string]OAuthGrant              `json:"grants"`
	Clients map[string]OAuthClientRegistration `json:"clients,omitempty"`
}

type tokenClaims struct {
	Issuer    string `json:"iss"`
	Audience  string `json:"aud"`
	IssuedAt  int64  `json:"iat"`
	ExpiresAt int64  `json:"exp"`
	GrantID   string `json:"grant_id"`
}

type refreshTokenClaims struct {
	GrantID    string `json:"grant_id"`
	Generation uint64 `json:"generation"`
	Nonce      string `json:"nonce"`
}

func NewOAuthStore() *OAuthStore {
	key, err := RandomToken(32)
	if err != nil {
		panic(fmt.Sprintf("generate in-memory OAuth signing key: %v", err))
	}
	return &OAuthStore{
		codes:      map[string]OAuthCode{},
		grants:     map[string]OAuthGrant{},
		clients:    map[string]OAuthClientRegistration{},
		signingKey: key,
	}
}

func NewPersistentOAuthStore(path, signingKey string) (*OAuthStore, error) {
	path = strings.TrimSpace(path)
	signingKey = strings.TrimSpace(signingKey)
	if path == "" {
		return nil, errors.New("OAuth state path is required")
	}
	if signingKey == "" {
		return nil, errors.New("OAuth signing key is required")
	}
	store := NewOAuthStore()
	store.statePath = path
	store.signingKey = signingKey
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return store, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read OAuth state: %w", err)
	}
	if len(data) > maxOAuthStateSize {
		return nil, fmt.Errorf("OAuth state exceeds %d bytes", maxOAuthStateSize)
	}
	var state oauthState
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, fmt.Errorf("decode OAuth state: %w", err)
	}
	if state.Version != oauthStateVersion {
		return nil, fmt.Errorf("unsupported OAuth state version %d", state.Version)
	}
	if state.Grants == nil {
		state.Grants = map[string]OAuthGrant{}
	}
	if state.Clients == nil {
		state.Clients = map[string]OAuthClientRegistration{}
	}
	now := time.Now().Unix()
	pruned := false
	for grantID, grant := range state.Grants {
		if !validStoredGrant(grantID, grant, now) {
			delete(state.Grants, grantID)
			pruned = true
		}
	}
	expiredClientIDs := map[string]struct{}{}
	for clientID, registration := range state.Clients {
		if registration.LastUsedAt == 0 {
			// v1 早期状态没有 last_used_at；升级时给予完整空闲窗口，避免立即踢掉仍在使用的客户端。
			registration.LastUsedAt = now
			state.Clients[clientID] = registration
			pruned = true
		}
		if !validStoredClient(clientID, registration, now) || clientRegistrationExpired(registration, now) {
			delete(state.Clients, clientID)
			expiredClientIDs[clientID] = struct{}{}
			pruned = true
		}
	}
	if len(expiredClientIDs) > 0 {
		for grantID, grant := range state.Grants {
			if _, expired := expiredClientIDs[grant.ClientID]; expired {
				delete(state.Grants, grantID)
			}
		}
	}
	if len(state.Grants) > maxOAuthGrants {
		return nil, fmt.Errorf("OAuth grant state contains %d entries, maximum is %d", len(state.Grants), maxOAuthGrants)
	}
	if len(state.Clients) > maxOAuthClients {
		return nil, fmt.Errorf("OAuth client state contains %d entries, maximum is %d", len(state.Clients), maxOAuthClients)
	}
	store.grants = state.Grants
	store.clients = state.Clients
	if pruned {
		store.mu.Lock()
		err = store.persistStateLocked()
		store.mu.Unlock()
		if err != nil {
			return nil, err
		}
	}
	return store, nil
}

func (s *OAuthStore) Create(code OAuthCode) (string, error) {
	value, err := RandomToken(32)
	if err != nil {
		return "", fmt.Errorf("generate authorization code: %w", err)
	}
	if code.GrantID == "" {
		code.GrantID, err = RandomToken(24)
		if err != nil {
			return "", fmt.Errorf("generate authorization grant ID: %w", err)
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	s.pruneExpiredCodesLocked(now)
	if len(s.codes) >= maxOAuthCodes {
		return "", fmt.Errorf("OAuth authorization code limit %d reached", maxOAuthCodes)
	}
	code.ExpiresAt = now.Add(5 * time.Minute)
	s.codes[value] = code
	return value, nil
}

func (s *OAuthStore) pruneExpiredCodesLocked(now time.Time) {
	for raw, code := range s.codes {
		if !code.ExpiresAt.After(now) {
			delete(s.codes, raw)
		}
	}
}

// Redeem validates every authorization-code binding while holding the store
// lock and consumes the code only after all checks succeed.
func (s *OAuthStore) Redeem(raw, clientID, redirectURI, verifier, resource string) (OAuthCode, bool, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	code, ok := s.codes[strings.TrimSpace(raw)]
	if !ok || time.Now().After(code.ExpiresAt) {
		delete(s.codes, strings.TrimSpace(raw))
		return OAuthCode{}, false, false
	}
	if code.Redeemed {
		return code, false, true
	}
	if code.ClientID != strings.TrimSpace(clientID) ||
		code.RedirectURI != strings.TrimSpace(redirectURI) ||
		!EquivalentResourceURI(code.Resource, resource) ||
		!VerifyPKCE(verifier, code.Challenge) {
		return OAuthCode{}, false, false
	}
	code.Redeemed = true
	s.codes[strings.TrimSpace(raw)] = code
	return code, true, false
}

func (s *OAuthStore) ActivateGrant(clientID, resource, grantID string, ttl time.Duration) error {
	clientID, resource, grantID = strings.TrimSpace(clientID), strings.TrimSpace(resource), strings.TrimSpace(grantID)
	if clientID == "" || resource == "" || grantID == "" || ttl <= 0 {
		return errors.New("client ID, resource, grant ID, and positive TTL are required")
	}
	now := time.Now()
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pruneExpiredGrantsLocked(now.Unix())
	if existing, ok := s.grants[grantID]; ok {
		if existing.Revoked {
			return errors.New("authorization grant is revoked")
		}
		return errors.New("authorization grant already exists")
	}
	if len(s.grants) >= maxOAuthGrants {
		return fmt.Errorf("OAuth grant limit %d reached", maxOAuthGrants)
	}
	s.grants[grantID] = OAuthGrant{ClientID: clientID, Resource: resource, ExpiresAt: now.Add(ttl).Unix()}
	if err := s.persistStateLocked(); err != nil {
		delete(s.grants, grantID)
		return err
	}
	return nil
}

func (s *OAuthStore) IssueRefreshToken(clientID, resource, grantID string, ttl time.Duration) (string, error) {
	clientID, resource, grantID = strings.TrimSpace(clientID), strings.TrimSpace(resource), strings.TrimSpace(grantID)
	if clientID == "" || resource == "" || grantID == "" || ttl <= 0 {
		return "", errors.New("client ID, resource, grant ID, and positive TTL are required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	s.pruneExpiredGrantsLocked(now.Unix())
	grant, ok := s.grants[grantID]
	if !ok || grant.Revoked || grant.ClientID != clientID || !EquivalentResourceURI(grant.Resource, resource) || grant.CurrentGeneration != 0 {
		return "", errors.New("authorization grant is not eligible for refresh token issuance")
	}
	raw, err := s.signRefreshToken(refreshTokenClaims{GrantID: grantID, Generation: 1})
	if err != nil {
		return "", err
	}
	previous := grant
	grant.CurrentGeneration = 1
	grant.ExpiresAt = now.Add(ttl).Unix()
	s.grants[grantID] = grant
	if err := s.persistStateLocked(); err != nil {
		s.grants[grantID] = previous
		return "", err
	}
	return raw, nil
}

func (s *OAuthStore) RotateRefreshToken(raw, clientID, requestedResource string, ttl time.Duration) (string, string, string, bool, error) {
	clientID, requestedResource = strings.TrimSpace(clientID), strings.TrimSpace(requestedResource)
	if strings.TrimSpace(raw) == "" || clientID == "" || requestedResource == "" || ttl <= 0 {
		return "", "", "", false, nil
	}
	claims, ok := s.verifyRefreshToken(raw)
	if !ok {
		return "", "", "", false, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	s.pruneExpiredGrantsLocked(now.Unix())
	grant, ok := s.grants[claims.GrantID]
	if !ok || grant.Revoked || grant.ClientID != clientID || !EquivalentResourceURI(grant.Resource, requestedResource) {
		return "", "", "", false, nil
	}
	if claims.Generation != grant.CurrentGeneration {
		grant.Revoked = true
		s.grants[claims.GrantID] = grant
		if err := s.persistStateLocked(); err != nil {
			grant.Revoked = false
			s.grants[claims.GrantID] = grant
			return "", "", "", false, err
		}
		return "", "", "", false, nil
	}
	nextGeneration := grant.CurrentGeneration + 1
	newRaw, err := s.signRefreshToken(refreshTokenClaims{GrantID: claims.GrantID, Generation: nextGeneration})
	if err != nil {
		return "", "", "", false, err
	}
	previous := grant
	grant.CurrentGeneration = nextGeneration
	grant.ExpiresAt = now.Add(ttl).Unix()
	s.grants[claims.GrantID] = grant
	if err := s.persistStateLocked(); err != nil {
		s.grants[claims.GrantID] = previous
		return "", "", "", false, err
	}
	return newRaw, grant.Resource, claims.GrantID, true, nil
}

func (s *OAuthStore) RevokeGrant(grantID string, ttl time.Duration) error {
	grantID = strings.TrimSpace(grantID)
	if grantID == "" || ttl <= 0 {
		return errors.New("grant ID and positive revocation TTL are required")
	}
	now := time.Now()
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pruneExpiredGrantsLocked(now.Unix())
	previous, existed := s.grants[grantID]
	if !existed && len(s.grants) >= maxOAuthGrants {
		return fmt.Errorf("OAuth grant limit %d reached", maxOAuthGrants)
	}
	grant := previous
	grant.Revoked = true
	if grant.ExpiresAt < now.Add(ttl).Unix() {
		grant.ExpiresAt = now.Add(ttl).Unix()
	}
	s.grants[grantID] = grant
	if err := s.persistStateLocked(); err != nil {
		if existed {
			s.grants[grantID] = previous
		} else {
			delete(s.grants, grantID)
		}
		return err
	}
	return nil
}

func (s *OAuthStore) GrantActive(grantID string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	grant, ok := s.grants[strings.TrimSpace(grantID)]
	return ok && !grant.Revoked && grant.ExpiresAt > time.Now().Unix()
}

func (s *OAuthStore) pruneExpiredGrantsLocked(now int64) {
	for grantID, grant := range s.grants {
		if grant.ExpiresAt <= now {
			delete(s.grants, grantID)
		}
	}
}

func (s *OAuthStore) signRefreshToken(claims refreshTokenClaims) (string, error) {
	nonce, err := RandomToken(24)
	if err != nil {
		return "", fmt.Errorf("generate refresh token nonce: %w", err)
	}
	claims.Nonce = nonce
	body, err := json.Marshal(claims)
	if err != nil {
		return "", fmt.Errorf("encode refresh token: %w", err)
	}
	encoded := base64.RawURLEncoding.EncodeToString(body)
	mac := hmac.New(sha256.New, []byte(s.signingKey))
	_, _ = mac.Write([]byte("refresh:" + encoded))
	return encoded + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil)), nil
}

func (s *OAuthStore) verifyRefreshToken(raw string) (refreshTokenClaims, bool) {
	parts := strings.Split(strings.TrimSpace(raw), ".")
	if len(parts) != 2 {
		return refreshTokenClaims{}, false
	}
	mac := hmac.New(sha256.New, []byte(s.signingKey))
	_, _ = mac.Write([]byte("refresh:" + parts[0]))
	expected := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	if !hmac.Equal([]byte(expected), []byte(parts[1])) {
		return refreshTokenClaims{}, false
	}
	data, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return refreshTokenClaims{}, false
	}
	var claims refreshTokenClaims
	if json.Unmarshal(data, &claims) != nil || claims.GrantID == "" || claims.Generation == 0 || claims.Nonce == "" {
		return refreshTokenClaims{}, false
	}
	return claims, true
}

func (s *OAuthStore) persistStateLocked() error {
	if s.statePath == "" {
		return nil
	}
	state := oauthState{Version: oauthStateVersion, Grants: s.grants, Clients: s.clients}
	data, err := json.Marshal(state)
	if err != nil {
		return fmt.Errorf("encode OAuth state: %w", err)
	}
	if len(data) > maxOAuthStateSize {
		return fmt.Errorf("OAuth state exceeds %d bytes", maxOAuthStateSize)
	}
	if err := atomicfile.Write(s.statePath, data, 0o600); err != nil {
		return fmt.Errorf("persist OAuth state: %w", err)
	}
	return nil
}

func validStoredGrant(grantID string, grant OAuthGrant, now int64) bool {
	decoded, err := base64.RawURLEncoding.DecodeString(grantID)
	return err == nil && len(decoded) == 24 &&
		grant.ExpiresAt > now &&
		(grant.Revoked || grant.ClientID != "" && grant.Resource != "")
}

func validStoredClient(clientID string, registration OAuthClientRegistration, now int64) bool {
	if !strings.HasPrefix(clientID, shortClientPrefix) {
		return false
	}
	raw := strings.TrimPrefix(clientID, shortClientPrefix)
	decoded, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil || len(decoded) != 24 || base64.RawURLEncoding.EncodeToString(decoded) != raw {
		return false
	}
	if len(registration.RedirectURIs) == 0 || len(registration.GrantTypes) == 0 {
		return false
	}
	if registration.IssuedAt <= 0 || registration.IssuedAt > now+60 {
		return false
	}
	if registration.LastUsedAt <= 0 || registration.LastUsedAt < registration.IssuedAt || registration.LastUsedAt > now+60 {
		return false
	}
	if len(registration.ClientName) > 200 {
		return false
	}
	return len(uniqueNonEmptyStrings(registration.RedirectURIs)) == len(registration.RedirectURIs) &&
		len(uniqueNonEmptyStrings(registration.GrantTypes)) == len(registration.GrantTypes)
}

func (s *OAuthStore) RegisterClient(clientName string, redirectURIs, grantTypes []string) (string, error) {
	now := time.Now()
	registration := OAuthClientRegistration{
		ClientName:   strings.TrimSpace(clientName),
		RedirectURIs: uniqueNonEmptyStrings(redirectURIs),
		GrantTypes:   uniqueNonEmptyStrings(grantTypes),
		IssuedAt:     now.Unix(),
		LastUsedAt:   now.Unix(),
	}
	if len(registration.RedirectURIs) == 0 {
		return "", errors.New("at least one redirect URI is required")
	}
	if len(registration.GrantTypes) == 0 {
		return "", errors.New("at least one grant type is required")
	}
	if len(registration.ClientName) > 200 {
		return "", errors.New("client name exceeds 200 characters")
	}
	randomValue, err := RandomToken(24)
	if err != nil {
		return "", fmt.Errorf("generate OAuth client ID: %w", err)
	}
	clientID := shortClientPrefix + randomValue

	s.mu.Lock()
	defer s.mu.Unlock()
	previousClients := cloneClientRegistrations(s.clients)
	previousGrants := cloneOAuthGrants(s.grants)
	previousCodes := cloneOAuthCodes(s.codes)
	s.pruneExpiredClientsLocked(now.Unix())
	if len(s.clients) >= maxOAuthClients {
		s.clients = previousClients
		s.grants = previousGrants
		s.codes = previousCodes
		return "", fmt.Errorf("OAuth client limit %d reached", maxOAuthClients)
	}
	s.clients[clientID] = registration
	if err := s.persistStateLocked(); err != nil {
		s.clients = previousClients
		s.grants = previousGrants
		s.codes = previousCodes
		return "", err
	}
	return clientID, nil
}

func (s *OAuthStore) PruneExpiredClients(now time.Time) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	previousClients := cloneClientRegistrations(s.clients)
	previousGrants := cloneOAuthGrants(s.grants)
	previousCodes := cloneOAuthCodes(s.codes)
	removed := s.pruneExpiredClientsLocked(now.Unix())
	if removed == 0 {
		return 0, nil
	}
	if err := s.persistStateLocked(); err != nil {
		s.clients = previousClients
		s.grants = previousGrants
		s.codes = previousCodes
		return 0, err
	}
	return removed, nil
}

func (s *OAuthStore) pruneExpiredClientsLocked(now int64) int {
	expired := make(map[string]struct{})
	for clientID, registration := range s.clients {
		if clientRegistrationExpired(registration, now) {
			delete(s.clients, clientID)
			expired[clientID] = struct{}{}
		}
	}
	if len(expired) == 0 {
		return 0
	}
	for grantID, grant := range s.grants {
		if _, remove := expired[grant.ClientID]; remove {
			delete(s.grants, grantID)
		}
	}
	for raw, code := range s.codes {
		if _, remove := expired[code.ClientID]; remove {
			delete(s.codes, raw)
		}
	}
	return len(expired)
}

func clientRegistrationExpired(registration OAuthClientRegistration, now int64) bool {
	lastUsed := registration.LastUsedAt
	if lastUsed == 0 {
		lastUsed = registration.IssuedAt
	}
	return lastUsed <= 0 || now-lastUsed >= int64(oauthClientIdleTTL/time.Second)
}

func (s *OAuthStore) ValidateClientID(clientID string) bool {
	_, ok := s.clientRegistration(clientID)
	return ok
}

func (s *OAuthStore) ValidateClientRedirect(clientID, redirectURI string) bool {
	redirectURI = strings.TrimSpace(redirectURI)
	registration, ok := s.clientRegistration(clientID)
	if !ok {
		return false
	}
	for _, registered := range registration.RedirectURIs {
		if registered == redirectURI {
			return true
		}
	}
	return false
}

func (s *OAuthStore) ClientAllowsGrant(clientID, grantType string) bool {
	grantType = strings.TrimSpace(grantType)
	registration, ok := s.clientRegistration(clientID)
	if !ok {
		return false
	}
	for _, registered := range registration.GrantTypes {
		if registered == grantType {
			return true
		}
	}
	return false
}

func (s *OAuthStore) ClientRegistration(clientID string) (OAuthClientRegistration, bool) {
	return s.clientRegistration(clientID)
}

func (s *OAuthStore) clientRegistration(clientID string) (OAuthClientRegistration, bool) {
	clientID = strings.TrimSpace(clientID)
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now().Unix()
	previousClients := cloneClientRegistrations(s.clients)
	previousGrants := cloneOAuthGrants(s.grants)
	previousCodes := cloneOAuthCodes(s.codes)
	if s.pruneExpiredClientsLocked(now) > 0 {
		if err := s.persistStateLocked(); err != nil {
			s.clients = previousClients
			s.grants = previousGrants
			s.codes = previousCodes
			slog.Warn("persist OAuth client cleanup failed", "error", err)
		}
	}
	registration, ok := s.clients[clientID]
	if !ok {
		return OAuthClientRegistration{}, false
	}
	if now-registration.LastUsedAt >= int64(oauthClientTouchInterval/time.Second) {
		previous := registration
		registration.LastUsedAt = now
		s.clients[clientID] = registration
		if err := s.persistStateLocked(); err != nil {
			s.clients[clientID] = previous
			registration = previous
			slog.Warn("persist OAuth client last use failed", "client_id", clientID, "error", err)
		}
	}
	registration.RedirectURIs = append([]string(nil), registration.RedirectURIs...)
	registration.GrantTypes = append([]string(nil), registration.GrantTypes...)
	return registration, true
}

func cloneClientRegistrations(input map[string]OAuthClientRegistration) map[string]OAuthClientRegistration {
	cloned := make(map[string]OAuthClientRegistration, len(input))
	for clientID, registration := range input {
		registration.RedirectURIs = append([]string(nil), registration.RedirectURIs...)
		registration.GrantTypes = append([]string(nil), registration.GrantTypes...)
		cloned[clientID] = registration
	}
	return cloned
}

func cloneOAuthGrants(input map[string]OAuthGrant) map[string]OAuthGrant {
	cloned := make(map[string]OAuthGrant, len(input))
	for grantID, grant := range input {
		cloned[grantID] = grant
	}
	return cloned
}

func cloneOAuthCodes(input map[string]OAuthCode) map[string]OAuthCode {
	cloned := make(map[string]OAuthCode, len(input))
	for raw, code := range input {
		cloned[raw] = code
	}
	return cloned
}

func VerifyPKCE(verifier, challenge string) bool {
	if !validPKCEVerifier(verifier) || !ValidPKCEChallenge(challenge) {
		return false
	}
	digest := sha256.Sum256([]byte(verifier))
	expected := base64.RawURLEncoding.EncodeToString(digest[:])
	return hmac.Equal([]byte(expected), []byte(challenge))
}

func ValidPKCEChallenge(challenge string) bool {
	if len(challenge) != 43 {
		return false
	}
	decoded, err := base64.RawURLEncoding.DecodeString(challenge)
	return err == nil && len(decoded) == sha256.Size && base64.RawURLEncoding.EncodeToString(decoded) == challenge
}

func validPKCEVerifier(verifier string) bool {
	if len(verifier) < 43 || len(verifier) > 128 {
		return false
	}
	for _, char := range verifier {
		if char >= 'a' && char <= 'z' || char >= 'A' && char <= 'Z' || char >= '0' && char <= '9' {
			continue
		}
		switch char {
		case '-', '.', '_', '~':
		default:
			return false
		}
	}
	return true
}

func RandomToken(n int) (string, error) {
	if n <= 0 {
		return "", errors.New("token byte length must be positive")
	}
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("read cryptographic randomness: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func uniqueNonEmptyStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	clean := make([]string, 0, len(values))
	for _, raw := range values {
		value := strings.TrimSpace(raw)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		clean = append(clean, value)
	}
	return clean
}

func IssueToken(issuer, audience, grantID, key string, ttl time.Duration) (string, error) {
	if key == "" {
		return "", errors.New("token signing key is required")
	}
	if issuer == "" {
		return "", errors.New("token issuer is required")
	}
	if audience == "" {
		return "", errors.New("token audience is required")
	}
	if grantID == "" {
		return "", errors.New("token grant ID is required")
	}
	if ttl <= 0 {
		return "", errors.New("token TTL must be positive")
	}
	now := time.Now()
	claims := tokenClaims{
		Issuer:    issuer,
		Audience:  audience,
		IssuedAt:  now.Unix(),
		ExpiresAt: now.Add(ttl).Unix(),
		GrantID:   grantID,
	}
	body, err := json.Marshal(claims)
	if err != nil {
		return "", fmt.Errorf("encode token claims: %w", err)
	}
	encoded := base64.RawURLEncoding.EncodeToString(body)
	mac := hmac.New(sha256.New, []byte(key))
	_, _ = mac.Write([]byte(encoded))
	sig := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	return encoded + "." + sig, nil
}

func ValidateToken(token, issuer, audience, key string) (string, bool) {
	if key == "" {
		return "", false
	}
	parts := strings.Split(token, ".")
	if len(parts) != 2 {
		return "", false
	}
	mac := hmac.New(sha256.New, []byte(key))
	_, _ = mac.Write([]byte(parts[0]))
	expected := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	if !hmac.Equal([]byte(expected), []byte(parts[1])) {
		return "", false
	}
	data, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return "", false
	}
	var claims tokenClaims
	if err := json.Unmarshal(data, &claims); err != nil {
		return "", false
	}
	now := time.Now().Unix()
	valid := claims.Audience == audience &&
		claims.Issuer == issuer &&
		claims.GrantID != "" &&
		claims.IssuedAt > 0 &&
		claims.IssuedAt <= now+60 &&
		claims.ExpiresAt > now &&
		claims.ExpiresAt > claims.IssuedAt
	return claims.GrantID, valid
}

func ConfiguredLoginValue() string {
	return os.Getenv("AGENTDOCK_OAUTH_PASSWORD")
}

func ConstantTimeEqual(a, b string) bool {
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}

func AppendQuery(raw string, values url.Values) string {
	u, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	q := u.Query()
	for key, list := range values {
		for _, value := range list {
			q.Add(key, value)
		}
	}
	u.RawQuery = q.Encode()
	return u.String()
}

func EquivalentResourceURI(left, right string) bool {
	leftURL, leftOK := canonicalResourceURI(left)
	rightURL, rightOK := canonicalResourceURI(right)
	return leftOK && rightOK && leftURL == rightURL
}

func canonicalResourceURI(raw string) (string, bool) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Scheme == "" || parsed.Host == "" || parsed.User != nil || parsed.Fragment != "" {
		return "", false
	}
	parsed.Scheme = strings.ToLower(parsed.Scheme)
	hostname := strings.ToLower(parsed.Hostname())
	port := parsed.Port()
	if (parsed.Scheme == "https" && port == "443") || (parsed.Scheme == "http" && port == "80") {
		port = ""
	}
	if strings.Contains(hostname, ":") {
		hostname = "[" + hostname + "]"
	}
	parsed.Host = hostname
	if port != "" {
		parsed.Host += ":" + port
	}
	return parsed.String(), true
}
