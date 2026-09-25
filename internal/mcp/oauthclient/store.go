package oauthclient

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/uvwt/agentdock/internal/fs/atomicfile"
	"github.com/uvwt/agentdock/internal/fs/filelock"
	"github.com/uvwt/agentdock/internal/fs/securepath"
	"golang.org/x/oauth2"
)

const storeSchemaVersion = 1

var storageKeyPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)

type ClientRecord struct {
	Issuer                  string    `json:"issuer"`
	RedirectURL             string    `json:"redirect_url"`
	ClientID                string    `json:"client_id"`
	ClientSecret            string    `json:"client_secret,omitempty"`
	TokenEndpointAuthMethod string    `json:"token_endpoint_auth_method,omitempty"`
	ClientIDIssuedAt        time.Time `json:"client_id_issued_at,omitempty"`
	ClientSecretExpiresAt   time.Time `json:"client_secret_expires_at,omitempty"`
	LastUsedAt              time.Time `json:"last_used_at,omitempty"`
}

type Grant struct {
	SchemaVersion   int       `json:"schema_version"`
	Endpoint        string    `json:"endpoint"`
	Resource        string    `json:"resource"`
	Issuer          string    `json:"issuer"`
	RedirectURL     string    `json:"redirect_url"`
	RegistrationKey string    `json:"registration_key"`
	TokenURL        string    `json:"token_url"`
	AccessToken     string    `json:"access_token"`
	RefreshToken    string    `json:"refresh_token,omitempty"`
	TokenType       string    `json:"token_type,omitempty"`
	Expiry          time.Time `json:"expiry,omitempty"`
	GrantedScopes   []string  `json:"granted_scopes,omitempty"`
	UpdatedAt       time.Time `json:"updated_at"`
}

type clientFile struct {
	SchemaVersion int                     `json:"schema_version"`
	Clients       map[string]ClientRecord `json:"clients"`
}

type store struct {
	root        string
	clientsPath string
}

func newStore(agentDockHome string) (*store, error) {
	home := filepath.Clean(strings.TrimSpace(agentDockHome))
	if home == "." || !filepath.IsAbs(home) {
		return nil, errors.New("AgentDockHome must be an absolute path")
	}
	root := filepath.Join(home, "data", "mcp")
	if err := os.MkdirAll(root, 0o700); err != nil {
		return nil, fmt.Errorf("create MCP OAuth data directory: %w", err)
	}
	if err := securepath.EnsurePrivate(root); err != nil {
		return nil, fmt.Errorf("secure MCP OAuth data directory: %w", err)
	}
	return &store{root: root, clientsPath: filepath.Join(root, "clients.json")}, nil
}

func (s *store) grantPath(storageKey string) (string, error) {
	storageKey = strings.TrimSpace(storageKey)
	if !storageKeyPattern.MatchString(storageKey) || storageKey == "clients" {
		return "", fmt.Errorf("invalid MCP OAuth storage key %q", storageKey)
	}
	// Prefix every grant filename so Windows device names such as CON/NUL remain valid.
	// The stable storage key itself is still preserved inside the runtime identity.
	return filepath.Join(s.root, "grant-"+storageKey+".json"), nil
}

func (s *store) loadGrant(storageKey string) (Grant, error) {
	path, err := s.grantPath(storageKey)
	if err != nil {
		return Grant{}, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return Grant{}, err
	}
	var grant Grant
	if err := json.Unmarshal(data, &grant); err != nil {
		return Grant{}, fmt.Errorf("decode MCP OAuth grant: %w", err)
	}
	if grant.SchemaVersion != storeSchemaVersion {
		return Grant{}, fmt.Errorf("unsupported MCP OAuth grant schema version %d", grant.SchemaVersion)
	}
	return grant, nil
}

func (s *store) saveGrant(storageKey string, grant Grant) error {
	path, err := s.grantPath(storageKey)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	release, err := filelock.Acquire(ctx, path+".lock")
	if err != nil {
		return fmt.Errorf("lock MCP OAuth grant: %w", err)
	}
	defer release()
	grant.SchemaVersion = storeSchemaVersion
	grant.UpdatedAt = time.Now().UTC()
	data, err := json.MarshalIndent(grant, "", "  ")
	if err != nil {
		return fmt.Errorf("encode MCP OAuth grant: %w", err)
	}
	data = append(data, '\n')
	if err := atomicfile.Write(path, data, 0o600); err != nil {
		return fmt.Errorf("persist MCP OAuth grant: %w", err)
	}
	return securepath.EnsurePrivate(path)
}

func (s *store) removeGrant(storageKey string) error {
	path, err := s.grantPath(storageKey)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	release, err := filelock.Acquire(ctx, path+".lock")
	if err != nil {
		return fmt.Errorf("lock MCP OAuth grant: %w", err)
	}
	defer release()
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove MCP OAuth grant: %w", err)
	}
	return nil
}

func registrationKey(issuer, redirectURL string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(issuer) + "\x00" + strings.TrimSpace(redirectURL)))
	return hex.EncodeToString(sum[:])
}

func (s *store) loadClient(key string) (ClientRecord, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	release, err := filelock.Acquire(ctx, s.clientsPath+".lock")
	if err != nil {
		return ClientRecord{}, fmt.Errorf("lock MCP OAuth clients: %w", err)
	}
	defer release()
	clients, err := s.loadClientsUnlocked()
	if err != nil {
		return ClientRecord{}, err
	}
	client, ok := clients.Clients[key]
	if !ok {
		return ClientRecord{}, os.ErrNotExist
	}
	return client, nil
}

func (s *store) saveClient(key string, client ClientRecord) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	release, err := filelock.Acquire(ctx, s.clientsPath+".lock")
	if err != nil {
		return fmt.Errorf("lock MCP OAuth clients: %w", err)
	}
	defer release()
	clients, err := s.loadClientsUnlocked()
	if err != nil {
		return err
	}
	client.LastUsedAt = time.Now().UTC()
	clients.Clients[key] = client
	data, err := json.MarshalIndent(clients, "", "  ")
	if err != nil {
		return fmt.Errorf("encode MCP OAuth clients: %w", err)
	}
	data = append(data, '\n')
	if err := atomicfile.Write(s.clientsPath, data, 0o600); err != nil {
		return fmt.Errorf("persist MCP OAuth clients: %w", err)
	}
	return securepath.EnsurePrivate(s.clientsPath)
}

func (s *store) touchClient(key string) {
	client, err := s.loadClient(key)
	if err != nil {
		return
	}
	_ = s.saveClient(key, client)
}

func (s *store) loadClientsUnlocked() (clientFile, error) {
	data, err := os.ReadFile(s.clientsPath)
	if errors.Is(err, os.ErrNotExist) {
		return clientFile{SchemaVersion: storeSchemaVersion, Clients: make(map[string]ClientRecord)}, nil
	}
	if err != nil {
		return clientFile{}, fmt.Errorf("read MCP OAuth clients: %w", err)
	}
	var clients clientFile
	if err := json.Unmarshal(data, &clients); err != nil {
		return clientFile{}, fmt.Errorf("decode MCP OAuth clients: %w", err)
	}
	if clients.SchemaVersion != storeSchemaVersion {
		return clientFile{}, fmt.Errorf("unsupported MCP OAuth clients schema version %d", clients.SchemaVersion)
	}
	if clients.Clients == nil {
		clients.Clients = make(map[string]ClientRecord)
	}
	return clients, nil
}

func tokenFromGrant(grant Grant) *oauth2.Token {
	return &oauth2.Token{
		AccessToken:  grant.AccessToken,
		RefreshToken: grant.RefreshToken,
		TokenType:    grant.TokenType,
		Expiry:       grant.Expiry,
	}
}

func grantTokenChanged(grant Grant, token *oauth2.Token) bool {
	if token == nil {
		return false
	}
	return grant.AccessToken != token.AccessToken ||
		grant.RefreshToken != token.RefreshToken ||
		grant.TokenType != token.TokenType ||
		!grant.Expiry.Equal(token.Expiry)
}
