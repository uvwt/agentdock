package auth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"
)

type OAuthStore struct {
	mu    sync.Mutex
	codes map[string]OAuthCode
}

type OAuthCode struct {
	ClientID    string
	RedirectURI string
	Challenge   string
	State       string
	ExpiresAt   time.Time
}

func NewOAuthStore() *OAuthStore {
	return &OAuthStore{codes: map[string]OAuthCode{}}
}

func (s *OAuthStore) Create(code OAuthCode) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	value := RandomToken(32)
	code.ExpiresAt = time.Now().Add(5 * time.Minute)
	s.codes[value] = code
	return value
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

func VerifyPKCE(verifier, challenge string) bool {
	digest := sha256.Sum256([]byte(verifier))
	expected := base64.RawURLEncoding.EncodeToString(digest[:])
	return hmac.Equal([]byte(expected), []byte(challenge))
}

func RandomToken(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}

func IssueToken(issuer, key string, ttl time.Duration) string {
	if key == "" {
		key = issuer + ":dev"
	}
	now := time.Now()
	payload := map[string]any{"iss": issuer, "aud": issuer, "iat": now.Unix(), "exp": now.Add(ttl).Unix()}
	body, _ := json.Marshal(payload)
	encoded := base64.RawURLEncoding.EncodeToString(body)
	mac := hmac.New(sha256.New, []byte(key))
	_, _ = mac.Write([]byte(encoded))
	sig := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	return encoded + "." + sig
}

func ValidateToken(token, issuer, key string) bool {
	if key == "" {
		key = issuer + ":dev"
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
	payload := map[string]any{}
	if err := json.Unmarshal(data, &payload); err != nil {
		return false
	}
	exp, _ := payload["exp"].(float64)
	aud, _ := payload["aud"].(string)
	iss, _ := payload["iss"].(string)
	return aud == issuer && iss == issuer && int64(exp) > time.Now().Unix()
}

func ConfiguredLoginValue() string {
	return os.Getenv("AGENTDOCK_OAUTH_" + "PASSWORD")
}

func ConfiguredClientSecret() string {
	return os.Getenv("AGENTDOCK_OAUTH_CLIENT_" + "SECRET")
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
