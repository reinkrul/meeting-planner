package store

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"golang.org/x/oauth2"

	"github.com/reinkrul/meeting-planner/internal/config"
)

// State holds persisted runtime data (capability token, per-calendar OAuth
// tokens). It is the only persisted state beyond the YAML config.
//
// State is multi-process safe by best-effort mtime-based reload: every read
// checks state.json's mtime and reloads if it changed since the last read.
// This makes CLI subcommands (rotate-capability, reauth) visible to a
// running server without restart, at the cost of an os.Stat per accessor.
type State struct {
	path     string
	mu       sync.Mutex
	data     stateData
	loadedAt time.Time
}

type stateData struct {
	CapabilityToken string                   `json:"capability_token"`
	Calendars       map[string]calendarState `json:"calendars"`
}

type calendarState struct {
	OAuthToken *oauth2.Token `json:"oauth_token,omitempty"`
}

// Open loads (or creates) the state file at <dataDir>/state.json. On first
// boot it generates a fresh capability token.
func Open(dataDir string) (*State, error) {
	if dataDir == "" {
		return nil, errors.New("data_dir is required")
	}
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		return nil, fmt.Errorf("create data dir: %w", err)
	}
	path := filepath.Join(dataDir, "state.json")
	s := &State{path: path, data: stateData{Calendars: map[string]calendarState{}}}

	info, err := os.Stat(path)
	switch {
	case errors.Is(err, os.ErrNotExist):
		tok, err := newCapabilityToken()
		if err != nil {
			return nil, err
		}
		s.data.CapabilityToken = tok
		if err := s.writeLocked(); err != nil {
			return nil, err
		}
	case err != nil:
		return nil, fmt.Errorf("stat %s: %w", path, err)
	default:
		raw, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", path, err)
		}
		if err := json.Unmarshal(raw, &s.data); err != nil {
			return nil, fmt.Errorf("parse %s: %w", path, err)
		}
		if s.data.Calendars == nil {
			s.data.Calendars = map[string]calendarState{}
		}
		s.loadedAt = info.ModTime()
		if s.data.CapabilityToken == "" {
			tok, err := newCapabilityToken()
			if err != nil {
				return nil, err
			}
			s.data.CapabilityToken = tok
			if err := s.writeLocked(); err != nil {
				return nil, err
			}
		}
	}
	return s, nil
}

// reloadIfChangedLocked re-reads state.json when its mtime is newer than the
// last load. Caller must hold s.mu. Errors are ignored (best-effort).
func (s *State) reloadIfChangedLocked() {
	info, err := os.Stat(s.path)
	if err != nil {
		return
	}
	if !info.ModTime().After(s.loadedAt) {
		return
	}
	raw, err := os.ReadFile(s.path)
	if err != nil {
		return
	}
	var data stateData
	if err := json.Unmarshal(raw, &data); err != nil {
		return
	}
	if data.Calendars == nil {
		data.Calendars = map[string]calendarState{}
	}
	s.data = data
	s.loadedAt = info.ModTime()
}

// CapabilityToken returns the current capability token.
func (s *State) CapabilityToken() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.reloadIfChangedLocked()
	return s.data.CapabilityToken
}

// RotateCapabilityToken generates a new capability token and persists it.
// Returns the new token.
func (s *State) RotateCapabilityToken() (string, error) {
	tok, err := newCapabilityToken()
	if err != nil {
		return "", err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.reloadIfChangedLocked()
	s.data.CapabilityToken = tok
	if err := s.writeLocked(); err != nil {
		return "", err
	}
	return tok, nil
}

// OAuthToken returns the stored OAuth token for a calendar, or nil if absent.
func (s *State) OAuthToken(calendarID string) *oauth2.Token {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.reloadIfChangedLocked()
	cs, ok := s.data.Calendars[calendarID]
	if !ok {
		return nil
	}
	return cs.OAuthToken
}

// SetOAuthToken stores or replaces the OAuth token for a calendar.
func (s *State) SetOAuthToken(calendarID string, tok *oauth2.Token) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.reloadIfChangedLocked()
	cs := s.data.Calendars[calendarID]
	cs.OAuthToken = tok
	s.data.Calendars[calendarID] = cs
	return s.writeLocked()
}

// DropOAuthToken removes the stored OAuth token for a calendar so that the
// admin UI re-enables for it.
func (s *State) DropOAuthToken(calendarID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.reloadIfChangedLocked()
	cs := s.data.Calendars[calendarID]
	cs.OAuthToken = nil
	s.data.Calendars[calendarID] = cs
	return s.writeLocked()
}

// NeedsOAuth returns the IDs of OAuth-requiring calendars that don't yet have
// a stored token. While the slice is non-empty, /admin auto-enables.
func (s *State) NeedsOAuth(cals []config.CalendarConfig) []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.reloadIfChangedLocked()
	var out []string
	for _, c := range cals {
		if !c.RequiresOAuth() {
			continue
		}
		cs := s.data.Calendars[c.ID]
		if cs.OAuthToken == nil {
			out = append(out, c.ID)
		}
	}
	return out
}

// writeLocked persists the state atomically. Caller must hold s.mu.
func (s *State) writeLocked() error {
	raw, err := json.MarshalIndent(s.data, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal state: %w", err)
	}
	dir := filepath.Dir(s.path)
	tmp, err := os.CreateTemp(dir, ".state-*.json")
	if err != nil {
		return fmt.Errorf("create tmp state file: %w", err)
	}
	tmpPath := tmp.Name()
	cleanup := func() { _ = os.Remove(tmpPath) }
	if _, err := tmp.Write(raw); err != nil {
		_ = tmp.Close()
		cleanup()
		return fmt.Errorf("write tmp state: %w", err)
	}
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		cleanup()
		return fmt.Errorf("chmod tmp state: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		cleanup()
		return fmt.Errorf("fsync tmp state: %w", err)
	}
	if err := tmp.Close(); err != nil {
		cleanup()
		return fmt.Errorf("close tmp state: %w", err)
	}
	if err := os.Rename(tmpPath, s.path); err != nil {
		cleanup()
		return fmt.Errorf("rename state: %w", err)
	}
	// Capture the on-disk mtime so reloadIfChangedLocked doesn't think our
	// own write is an external change to re-load.
	if info, err := os.Stat(s.path); err == nil {
		s.loadedAt = info.ModTime()
	}
	return nil
}

func newCapabilityToken() (string, error) {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("generate capability token: %w", err)
	}
	return hex.EncodeToString(b[:]), nil
}
