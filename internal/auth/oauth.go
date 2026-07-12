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
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/uvwt/agentdock/internal/atomicfile"
)

const (
	shortClientPrefix   = "adcr_"
	oauthStateVersion   = 2
	maxRefreshStateSize = 1 << 20
	maxRefreshTokens    = 1024
	maxOAuthClients     = 1024
)

type OAuthStore struct {
	mu            sync.Mutex
	codes         map[string]OAuthCode
	refreshTokens map[string]OAuthRefreshToken
	clients       map[string]OAuthClientRegistration
	refreshPath   string
}

type OAuthCode struct {
	ClientID    string
	RedirectURI string
	Challenge   string
	State       string
	Resource    string
	ExpiresAt   time.Time
}

type OAuthRefreshToken struct {
	ClientID  string `json:"client_id"`
	Resource  string `json:"resource"`
	IssuedAt  int64  `json:"issued_at"`
	ExpiresAt int64  `json:"expires_at"`
}

type OAuthClientRegistration struct {
	ClientName   string   `json:"client_name,omitempty"`
	RedirectURIs []string `json:"redirect_uris"`
	GrantTypes   []string `json:"grant_types"`
	IssuedAt     int64    `json:"issued_at"`
}

type oauthState struct {
	Version int                                `json:"version"`
	Tokens  map[string]OAuthRefreshToken       `json:"tokens"`
	Clients map[string]OAuthClientRegistration `json:"clients,omitempty"`
}

type tokenClaims struct {
	Issuer    string `json:"iss"`
	Audience  string `json:"aud"`
	IssuedAt  int64  `json:"iat"`
	ExpiresAt int64  `json:"exp"`
}

func NewOAuthStore() *OAuthStore {
	return &OAuthStore{
		codes:         map[string]OAuthCode{},
		refreshTokens: map[string]OAuthRefreshToken{},
		clients:       map[string]OAuthClientRegistration{},
	}
}

func NewPersistentOAuthStore(path string) (*OAuthStore, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil, errors.New("OAuth refresh token state path is required")
	}
	store := NewOAuthStore()
	store.refreshPath = path
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return store, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read OAuth refresh token state: %w", err)
	}
	if len(data) > maxRefreshStateSize {
		return nil, fmt.Errorf("OAuth refresh token state exceeds %d bytes", maxRefreshStateSize)
	}
	var state oauthState
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, fmt.Errorf("decode OAuth refresh token state: %w", err)
	}
	if state.Version != 1 && state.Version != oauthStateVersion {
		return nil, fmt.Errorf("unsupported OAuth refresh token state version %d", state.Version)
	}
	if state.Tokens == nil {
		state.Tokens = map[string]OAuthRefreshToken{}
	}
	if state.Clients == nil {
		state.Clients = map[string]OAuthClientRegistration{}
	}
	now := time.Now().Unix()
	pruned := false
	for digest, token := range state.Tokens {
		if !validStoredRefreshToken(digest, token, now) {
			delete(state.Tokens, digest)
			pruned = true
		}
	}
	for clientID, registration := range state.Clients {
		if !validStoredClient(clientID, registration, now) {
			delete(state.Clients, clientID)
			pruned = true
		}
	}
	if len(state.Tokens) > maxRefreshTokens {
		return nil, fmt.Errorf("OAuth refresh token state contains %d entries, maximum is %d", len(state.Tokens), maxRefreshTokens)
	}
	if len(state.Clients) > maxOAuthClients {
		return nil, fmt.Errorf("OAuth client state contains %d entries, maximum is %d", len(state.Clients), maxOAuthClients)
	}
	store.refreshTokens = state.Tokens
	store.clients = state.Clients
	if pruned || state.Version != oauthStateVersion {
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
	s.mu.Lock()
	defer s.mu.Unlock()
	code.ExpiresAt = time.Now().Add(5 * time.Minute)
	s.codes[value] = code
	return value, nil
}

func (s *OAuthStore) Consume(code string) (OAuthCode, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	value, ok := s.codes[code]
	if !ok || time.Now().After(value.ExpiresAt) {
		delete(s.codes, code)
		return OAuthCode{}, false
	}
	delete(s.codes, code)
	return value, true
}

func (s *OAuthStore) IssueRefreshToken(clientID, resource string, ttl time.Duration) (string, error) {
	clientID = strings.TrimSpace(clientID)
	resource = strings.TrimSpace(resource)
	if clientID == "" {
		return "", errors.New("refresh token client ID is required")
	}
	if resource == "" {
		return "", errors.New("refresh token resource is required")
	}
	if ttl <= 0 {
		return "", errors.New("refresh token TTL must be positive")
	}
	raw, err := RandomToken(48)
	if err != nil {
		return "", fmt.Errorf("generate refresh token: %w", err)
	}
	now := time.Now()
	entry := OAuthRefreshToken{
		ClientID: clientID, Resource: resource,
		IssuedAt: now.Unix(), ExpiresAt: now.Add(ttl).Unix(),
	}
	digest := refreshTokenDigest(raw)

	s.mu.Lock()
	defer s.mu.Unlock()
	s.pruneExpiredRefreshTokensLocked(now.Unix())
	if len(s.refreshTokens) >= maxRefreshTokens {
		return "", fmt.Errorf("OAuth refresh token limit %d reached", maxRefreshTokens)
	}
	s.refreshTokens[digest] = entry
	if err := s.persistStateLocked(); err != nil {
		delete(s.refreshTokens, digest)
		return "", err
	}
	return raw, nil
}

func (s *OAuthStore) RotateRefreshToken(raw, clientID, requestedResource string, ttl time.Duration) (string, string, bool, error) {
	raw = strings.TrimSpace(raw)
	clientID = strings.TrimSpace(clientID)
	requestedResource = strings.TrimSpace(requestedResource)
	if raw == "" || clientID == "" || ttl <= 0 {
		return "", "", false, nil
	}
	newRaw, err := RandomToken(48)
	if err != nil {
		return "", "", false, fmt.Errorf("generate rotated refresh token: %w", err)
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	s.pruneExpiredRefreshTokensLocked(now.Unix())
	oldDigest := refreshTokenDigest(raw)
	entry, ok := s.refreshTokens[oldDigest]
	if !ok || entry.ClientID != clientID {
		return "", "", false, nil
	}
	if requestedResource != "" && requestedResource != entry.Resource {
		return "", "", false, nil
	}
	newEntry := OAuthRefreshToken{
		ClientID: entry.ClientID, Resource: entry.Resource,
		IssuedAt: now.Unix(), ExpiresAt: now.Add(ttl).Unix(),
	}
	newDigest := refreshTokenDigest(newRaw)
	delete(s.refreshTokens, oldDigest)
	s.refreshTokens[newDigest] = newEntry
	if err := s.persistStateLocked(); err != nil {
		delete(s.refreshTokens, newDigest)
		s.refreshTokens[oldDigest] = entry
		return "", "", false, err
	}
	return newRaw, entry.Resource, true, nil
}

func (s *OAuthStore) pruneExpiredRefreshTokensLocked(now int64) {
	for digest, token := range s.refreshTokens {
		if token.ExpiresAt <= now {
			delete(s.refreshTokens, digest)
		}
	}
}

func (s *OAuthStore) persistStateLocked() error {
	if s.refreshPath == "" {
		return nil
	}
	state := oauthState{Version: oauthStateVersion, Tokens: s.refreshTokens, Clients: s.clients}
	data, err := json.Marshal(state)
	if err != nil {
		return fmt.Errorf("encode OAuth refresh token state: %w", err)
	}
	if len(data) > maxRefreshStateSize {
		return fmt.Errorf("OAuth refresh token state exceeds %d bytes", maxRefreshStateSize)
	}
	if err := atomicfile.Write(s.refreshPath, data, 0o600); err != nil {
		return fmt.Errorf("persist OAuth refresh token state: %w", err)
	}
	return nil
}

func validStoredRefreshToken(digest string, token OAuthRefreshToken, now int64) bool {
	decoded, err := base64.RawURLEncoding.DecodeString(digest)
	return err == nil && len(decoded) == sha256.Size &&
		token.ClientID != "" && token.Resource != "" &&
		token.IssuedAt > 0 && token.IssuedAt <= now+60 &&
		token.ExpiresAt > now && token.ExpiresAt > token.IssuedAt
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
	if len(registration.ClientName) > 200 {
		return false
	}
	return len(uniqueNonEmptyStrings(registration.RedirectURIs)) == len(registration.RedirectURIs) &&
		len(uniqueNonEmptyStrings(registration.GrantTypes)) == len(registration.GrantTypes)
}

func (s *OAuthStore) RegisterClient(clientName string, redirectURIs, grantTypes []string) (string, error) {
	registration := OAuthClientRegistration{
		ClientName:   strings.TrimSpace(clientName),
		RedirectURIs: uniqueNonEmptyStrings(redirectURIs),
		GrantTypes:   uniqueNonEmptyStrings(grantTypes),
		IssuedAt:     time.Now().Unix(),
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
	if len(s.clients) >= maxOAuthClients {
		return "", fmt.Errorf("OAuth client limit %d reached", maxOAuthClients)
	}
	s.clients[clientID] = registration
	if err := s.persistStateLocked(); err != nil {
		delete(s.clients, clientID)
		return "", err
	}
	return clientID, nil
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
	s.mu.Lock()
	defer s.mu.Unlock()
	registration, ok := s.clients[strings.TrimSpace(clientID)]
	if !ok {
		return OAuthClientRegistration{}, false
	}
	registration.RedirectURIs = append([]string(nil), registration.RedirectURIs...)
	registration.GrantTypes = append([]string(nil), registration.GrantTypes...)
	return registration, true
}

func refreshTokenDigest(raw string) string {
	digest := sha256.Sum256([]byte(raw))
	return base64.RawURLEncoding.EncodeToString(digest[:])
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

func IssueToken(issuer, audience, key string, ttl time.Duration) (string, error) {
	if key == "" {
		return "", errors.New("token signing key is required")
	}
	if issuer == "" {
		return "", errors.New("token issuer is required")
	}
	if audience == "" {
		return "", errors.New("token audience is required")
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

func ValidateToken(token, issuer, audience, key string) bool {
	if key == "" {
		return false
	}
	parts := strings.Split(token, ".")
	if len(parts) != 2 {
		return false
	}
	mac := hmac.New(sha256.New, []byte(key))
	_, _ = mac.Write([]byte(parts[0]))
	expected := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	if !hmac.Equal([]byte(expected), []byte(parts[1])) {
		return false
	}
	data, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return false
	}
	var claims tokenClaims
	if err := json.Unmarshal(data, &claims); err != nil {
		return false
	}
	now := time.Now().Unix()
	return claims.Audience == audience &&
		claims.Issuer == issuer &&
		claims.IssuedAt > 0 &&
		claims.IssuedAt <= now+60 &&
		claims.ExpiresAt > now &&
		claims.ExpiresAt > claims.IssuedAt
}

func ConfiguredLoginValue() string {
	return os.Getenv("AGENTDOCK_OAUTH_" + "PASSWORD")
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
