package store

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

const seed = "0123456789abcdef0123456789abcdef" // 32 chars

func TestSeedPinsTokenOnFreshDir(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir, seed)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if got := s.CapabilityToken(); got != seed {
		t.Errorf("token: got %q, want seed %q", got, seed)
	}
	// Persisted to disk too.
	raw, _ := os.ReadFile(filepath.Join(dir, "state.json"))
	var data stateData
	_ = json.Unmarshal(raw, &data)
	if data.CapabilityToken != seed {
		t.Errorf("on-disk token: got %q, want %q", data.CapabilityToken, seed)
	}
}

func TestSeedOverridesExistingToken(t *testing.T) {
	dir := t.TempDir()
	// First boot with no seed → random token persisted.
	s1, err := Open(dir, "")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	random := s1.CapabilityToken()
	if random == seed {
		t.Fatal("random token unexpectedly equals seed")
	}
	// Re-open the same dir WITH a seed → seed wins.
	s2, err := Open(dir, seed)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	if got := s2.CapabilityToken(); got != seed {
		t.Errorf("token after seeded reopen: got %q, want %q", got, seed)
	}
}

func TestSeedSurvivesExternalReload(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir, seed)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	// Simulate another process rewriting state.json with a different token,
	// and bump mtime into the future so reloadIfChangedLocked fires.
	path := filepath.Join(dir, "state.json")
	external := stateData{CapabilityToken: "deadbeefdeadbeefdeadbeef", Calendars: map[string]calendarState{}}
	raw, _ := json.MarshalIndent(external, "", "  ")
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	future := time.Now().Add(time.Hour)
	if err := os.Chtimes(path, future, future); err != nil {
		t.Fatal(err)
	}
	// Read triggers reload; seed must still win.
	if got := s.CapabilityToken(); got != seed {
		t.Errorf("token after external reload: got %q, want pinned %q", got, seed)
	}
}

func TestRotateErrorsWhenPinned(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir, seed)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if _, err := s.RotateCapabilityToken(); err == nil {
		t.Error("expected RotateCapabilityToken to error when pinned, got nil")
	}
}

func TestNoSeedRandomStableAcrossReopen(t *testing.T) {
	dir := t.TempDir()
	s1, err := Open(dir, "")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	tok1 := s1.CapabilityToken()
	if len(tok1) < 16 {
		t.Errorf("random token too short: %q", tok1)
	}
	s2, err := Open(dir, "")
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	if tok2 := s2.CapabilityToken(); tok2 != tok1 {
		t.Errorf("token not stable across reopen: %q vs %q", tok1, tok2)
	}
}

func TestRotateWorksWhenNotPinned(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir, "")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	before := s.CapabilityToken()
	after, err := s.RotateCapabilityToken()
	if err != nil {
		t.Fatalf("rotate: %v", err)
	}
	if after == before {
		t.Error("rotate did not change the token")
	}
	if s.CapabilityToken() != after {
		t.Error("CapabilityToken did not reflect rotation")
	}
}
