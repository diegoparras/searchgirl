package llm

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
)

// Store is a runtime-switchable Provider. It implements the Provider interface
// by delegating to the currently-active provider under an RWMutex, so the
// answer engine and the API keep seeing a plain Provider — but the active one
// can be reconfigured from the UI without a restart.
//
// Precedence (loadProvider): a saved GUI setting wins; otherwise the env-based
// fallback (llm.FromEnv at startup); otherwise Noop (off). The saved setting
// lives in <configDir>/llm.json (0600, holds an API key).
type Store struct {
	configDir string
	fallback  Provider // env-based provider captured at startup

	mu     sync.RWMutex
	active Provider
	mem    *Settings // in-memory config when configDir == "" (no persistence)
}

// Settings is the persisted, user-editable LLM config.
type Settings struct {
	BaseURL string `json:"base_url"`
	Model   string `json:"model"`
	APIKey  string `json:"api_key"`
}

// NewStore builds the store. configDir empty → persistence is disabled (the
// selector still works in-memory until restart). fallback is what to use when
// nothing is saved (typically llm.FromEnv()).
func NewStore(configDir string, fallback Provider) *Store {
	if fallback == nil {
		fallback = Noop{}
	}
	s := &Store{configDir: configDir, fallback: fallback}
	s.active = s.loadProvider()
	return s
}

// Persisted reports whether the store can save settings to disk.
func (s *Store) Persisted() bool { return s.configDir != "" }

func (s *Store) path() string { return filepath.Join(s.configDir, "llm.json") }

func (s *Store) read() (Settings, error) {
	if s.configDir == "" {
		s.mu.RLock()
		defer s.mu.RUnlock()
		if s.mem != nil {
			return *s.mem, nil
		}
		return Settings{}, os.ErrNotExist
	}
	var set Settings
	b, err := os.ReadFile(s.path())
	if err != nil {
		return set, err
	}
	return set, json.Unmarshal(b, &set)
}

func (s *Store) write(set Settings) error {
	if s.configDir == "" {
		return os.ErrPermission
	}
	if err := os.MkdirAll(s.configDir, 0o700); err != nil {
		return err
	}
	b, _ := json.MarshalIndent(set, "", "  ")
	return os.WriteFile(s.path(), b, 0o600)
}

// loadProvider: saved GUI setting (BaseURL+Model) wins; else the env fallback.
func (s *Store) loadProvider() Provider {
	if set, err := s.read(); err == nil && set.BaseURL != "" && set.Model != "" {
		return &OpenAICompatible{BaseURL: set.BaseURL, Model: set.Model, APIKey: set.APIKey, Referer: os.Getenv("LLM_REFERER")}
	}
	return s.fallback
}

// --- Provider interface: delegate to the active provider ---

func (s *Store) current() Provider {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.active
}

func (s *Store) Available() bool { return s.current().Available() }
func (s *Store) Name() string    { return s.current().Name() }
func (s *Store) Complete(ctx context.Context, prompt string) (string, error) {
	return s.current().Complete(ctx, prompt)
}

// Save persists the settings and reloads the active provider. Empty
// BaseURL/Model clears the saved config (turns the LLM off, falling back to
// env). A blank APIKey keeps the previously-saved one (so the user can change
// the URL/model without re-typing the key).
func (s *Store) Save(set Settings) error {
	clearing := set.BaseURL == "" || set.Model == ""
	if !clearing && set.APIKey == "" {
		if old, err := s.read(); err == nil { // fuera del Lock (read toma su propio RLock)
			set.APIKey = old.APIKey // blank key = keep the old one
		}
	}
	// Persistir (si hay volumen).
	if s.configDir != "" {
		if clearing {
			_ = os.Remove(s.path())
		} else if err := s.write(set); err != nil {
			return err
		}
	}
	// Construir el nuevo provider inline (sin re-leer bajo el Lock).
	var np Provider
	if clearing {
		np = s.fallback
	} else {
		np = &OpenAICompatible{BaseURL: set.BaseURL, Model: set.Model, APIKey: set.APIKey, Referer: os.Getenv("LLM_REFERER")}
	}
	s.mu.Lock()
	if s.configDir == "" { // sin volumen: la config vive en memoria para Snapshot
		if clearing {
			s.mem = nil
		} else {
			cp := set
			s.mem = &cp
		}
	}
	s.active = np
	s.mu.Unlock()
	return nil
}

// Snapshot returns the current config for the UI — never the API key, only
// whether one is set.
func (s *Store) Snapshot() map[string]any {
	set, _ := s.read()
	p := s.current()
	return map[string]any{
		"base_url":   set.BaseURL,
		"model":      set.Model,
		"has_key":    set.APIKey != "",
		"configured": p.Available(),
		"name":       p.Name(),
		"persisted":  s.Persisted(),
	}
}

// ModelsFor lists the models an endpoint exposes. A blank key on the same
// saved server reuses the stored key (so the user can list models without
// re-typing it).
func (s *Store) ModelsFor(ctx context.Context, baseURL, key string) ([]string, error) {
	if saved, err := s.read(); err == nil {
		if baseURL == "" {
			baseURL = saved.BaseURL
		}
		if key == "" && baseURL == saved.BaseURL {
			key = saved.APIKey
		}
	}
	p := &OpenAICompatible{BaseURL: baseURL, Model: "-", APIKey: key, Referer: os.Getenv("LLM_REFERER")}
	return p.Models(ctx)
}
