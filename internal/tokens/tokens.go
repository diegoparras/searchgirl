// Package tokens is the persistent store for Bearer tokens issued from the UI
// (the "Conexión MCP" panel): each token lets an app or agent reach the API
// and the MCP endpoint. It complements the env tokens (SEARCHGIRL_MCP_TOKEN):
// both are valid. Secrets are never stored — only their sha256 hash.
package tokens

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Token is one issued credential. The secret itself is shown once at creation
// and never persisted; only Hash (sha256 hex) lives on disk.
type Token struct {
	ID       string `json:"id"`
	Label    string `json:"label"`
	Hash     string `json:"hash"` // hex(sha256(secret)) — never the secret
	Created  string `json:"created"`
	LastUsed string `json:"last_used,omitempty"`
	Expires  string `json:"expires,omitempty"` // YYYY-MM-DD; empty = never
}

// Public is the metadata safe to show in the UI (no hash).
type Public struct {
	ID       string `json:"id"`
	Label    string `json:"label"`
	Created  string `json:"created"`
	LastUsed string `json:"last_used,omitempty"`
	Expires  string `json:"expires,omitempty"`
}

type Store struct {
	configDir string
	mu        sync.Mutex
	tokens    []Token
}

// New loads the store from <configDir>/tokens.json. configDir empty →
// in-memory only (tokens vanish on restart; the UI warns).
func New(configDir string) *Store {
	s := &Store{configDir: configDir}
	if configDir != "" {
		if b, err := os.ReadFile(s.path()); err == nil {
			_ = json.Unmarshal(b, &s.tokens)
		}
	}
	return s
}

// Persisted reports whether tokens survive a restart.
func (s *Store) Persisted() bool { return s.configDir != "" }

func (s *Store) path() string { return filepath.Join(s.configDir, "tokens.json") }

// saveLocked writes the store; call with the lock held. Best-effort in-memory.
func (s *Store) saveLocked() error {
	if s.configDir == "" {
		return nil
	}
	if err := os.MkdirAll(s.configDir, 0o700); err != nil {
		return err
	}
	b, _ := json.MarshalIndent(s.tokens, "", "  ")
	return os.WriteFile(s.path(), b, 0o600)
}

// List returns the issued tokens' metadata (no secrets, no hashes).
func (s *Store) List() []Public {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Public, 0, len(s.tokens))
	for _, t := range s.tokens {
		out = append(out, Public{ID: t.ID, Label: t.Label, Created: t.Created, LastUsed: t.LastUsed, Expires: t.Expires})
	}
	return out
}

// Create issues a token: generates a random secret, stores only its hash, and
// returns the secret (shown once) plus the public metadata. expiresDays 0 =
// never expires. today is YYYY-MM-DD (injected so it stays testable).
func (s *Store) Create(label string, expiresDays int, today string) (secret string, item Public, err error) {
	label = strings.TrimSpace(label)
	if label == "" {
		label = "token"
	}
	raw := make([]byte, 24)
	if _, err = rand.Read(raw); err != nil {
		return "", Public{}, err
	}
	secret = "sg_" + hex.EncodeToString(raw)
	sum := sha256.Sum256([]byte(secret))

	idb := make([]byte, 6)
	_, _ = rand.Read(idb)
	id := "tok_" + hex.EncodeToString(idb)

	expires := ""
	if expiresDays > 0 {
		if t, e := time.Parse("2006-01-02", today); e == nil {
			expires = t.AddDate(0, 0, expiresDays).Format("2006-01-02")
		}
	}
	tok := Token{ID: id, Label: label, Hash: hex.EncodeToString(sum[:]), Created: today, Expires: expires}

	s.mu.Lock()
	s.tokens = append(s.tokens, tok)
	err = s.saveLocked()
	s.mu.Unlock()
	if err != nil {
		return "", Public{}, err
	}
	return secret, Public{ID: id, Label: label, Created: today, Expires: expires}, nil
}

// Revoke deletes a token by id.
func (s *Store) Revoke(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, t := range s.tokens {
		if t.ID == id {
			s.tokens = append(s.tokens[:i], s.tokens[i+1:]...)
			_ = s.saveLocked()
			return true
		}
	}
	return false
}

// Verify checks a presented secret against every stored token in constant
// time, honoring expiry, and returns the matched token's label. It updates
// LastUsed best-effort. Suitable as auth's Bearer verifier.
func (s *Store) Verify(secret string) (label string, ok bool) {
	if !strings.HasPrefix(secret, "sg_") {
		return "", false
	}
	sum := sha256.Sum256([]byte(secret))
	want := []byte(hex.EncodeToString(sum[:]))
	today := time.Now().UTC().Format("2006-01-02")

	s.mu.Lock()
	defer s.mu.Unlock()
	matchedIdx := -1
	for i := range s.tokens { // sin corte temprano
		if subtle.ConstantTimeCompare([]byte(s.tokens[i].Hash), want) == 1 {
			if s.tokens[i].Expires != "" && today > s.tokens[i].Expires {
				continue // vencido
			}
			matchedIdx, label = i, s.tokens[i].Label
		}
	}
	if matchedIdx < 0 {
		return "", false
	}
	if s.tokens[matchedIdx].LastUsed != today {
		s.tokens[matchedIdx].LastUsed = today
		_ = s.saveLocked()
	}
	return label, true
}

// count is a tiny helper for tests/diagnostics.
func (s *Store) count() int { s.mu.Lock(); defer s.mu.Unlock(); return len(s.tokens) }
