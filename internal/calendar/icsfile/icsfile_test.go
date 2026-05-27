package icsfile

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/reinkrul/meeting-planner/internal/calendar"
	"github.com/reinkrul/meeting-planner/internal/config"
)

// TestRoundTrip: create an event via CreateEvent, then re-read busy blocks
// and confirm the new event shows up in FreeBusy.
func TestRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "owner.ics")
	p := New("owner", config.ICSFileCalendarConfig{
		Path:           path,
		OrganizerName:  "Test",
		OrganizerEmail: "t@example.com",
	})

	start := time.Date(2026, 5, 27, 10, 0, 0, 0, time.UTC)
	end := start.Add(30 * time.Minute)
	id, err := p.CreateEvent(context.Background(), calendar.Event{
		Title:     "test",
		Start:     start,
		End:       end,
		Attendees: []calendar.Attendee{{Name: "Guest", Email: "g@example.com"}},
	})
	if err != nil {
		t.Fatalf("CreateEvent: %v", err)
	}
	if id == "" {
		t.Fatal("expected non-empty event id")
	}

	busy, err := p.FreeBusy(context.Background(), start.Add(-1*time.Hour), end.Add(1*time.Hour))
	if err != nil {
		t.Fatalf("FreeBusy: %v", err)
	}
	if len(busy) != 1 {
		t.Fatalf("want 1 busy block, got %d", len(busy))
	}
	if !busy[0].Start.Equal(start) || !busy[0].End.Equal(end) {
		t.Errorf("busy mismatch: got %v..%v, want %v..%v", busy[0].Start, busy[0].End, start, end)
	}
}

// TestMissingFileEmptyBusy: requesting free-busy on a non-existent file is
// not an error — it's just "no events".
func TestMissingFileEmptyBusy(t *testing.T) {
	p := New("owner", config.ICSFileCalendarConfig{Path: "/nonexistent/path/owner.ics"})
	got, err := p.FreeBusy(context.Background(), time.Now(), time.Now().Add(24*time.Hour))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != nil {
		t.Errorf("want nil, got %+v", got)
	}
}
