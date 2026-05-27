// Package icsfile implements calendar.Provider on top of a local ICS file.
// It treats every VEVENT in the file as busy and appends VEVENTs on
// CreateEvent. Useful for local development and tests; also a stepping-stone
// toward a CalDAV provider.
package icsfile

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
	"time"

	ical "github.com/arran4/golang-ical"
	"github.com/google/uuid"

	"github.com/reinkrul/meeting-planner/internal/calendar"
	"github.com/reinkrul/meeting-planner/internal/config"
)

// Provider is a file-backed calendar.Provider.
type Provider struct {
	id  string
	cfg config.ICSFileCalendarConfig
	mu  sync.Mutex // serializes in-process access to the file
}

func New(id string, cfg config.ICSFileCalendarConfig) *Provider {
	return &Provider{id: id, cfg: cfg}
}

func (p *Provider) ID() string { return p.id }

// FreeBusy returns busy blocks overlapping [from, to). A missing file is
// treated as "no events" (empty result, no error) so first-run UX is smooth.
func (p *Provider) FreeBusy(_ context.Context, from, to time.Time) ([]calendar.BusyBlock, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	cal, err := loadCalendar(p.cfg.Path)
	if err != nil {
		return nil, err
	}
	if cal == nil {
		return nil, nil
	}
	var out []calendar.BusyBlock
	for _, ev := range cal.Events() {
		start, err := ev.GetStartAt()
		if err != nil {
			continue
		}
		end, err := ev.GetEndAt()
		if err != nil {
			continue
		}
		if end.After(from) && start.Before(to) {
			out = append(out, calendar.BusyBlock{Start: start, End: end})
		}
	}
	return out, nil
}

// CreateEvent appends a VEVENT to the calendar file and returns the new UID.
// No emails are sent — the event exists only in the file.
func (p *Provider) CreateEvent(_ context.Context, ev calendar.Event) (string, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	cal, err := loadCalendar(p.cfg.Path)
	if err != nil {
		return "", err
	}
	if cal == nil {
		cal = ical.NewCalendar()
		cal.SetProductId("-//meeting-planner//EN")
		cal.SetVersion("2.0")
	}

	uid := uuid.New().String()
	event := cal.AddEvent(uid)
	now := time.Now().UTC()
	event.SetCreatedTime(now)
	event.SetDtStampTime(now)
	event.SetStartAt(ev.Start.UTC())
	event.SetEndAt(ev.End.UTC())
	event.SetSummary(ev.Title)
	if ev.Description != "" {
		event.SetDescription(ev.Description)
	}
	if ev.Location != "" {
		event.SetLocation(ev.Location)
	}
	if p.cfg.OrganizerEmail != "" {
		if p.cfg.OrganizerName != "" {
			event.SetOrganizer("mailto:"+p.cfg.OrganizerEmail, ical.WithCN(p.cfg.OrganizerName))
		} else {
			event.SetOrganizer("mailto:" + p.cfg.OrganizerEmail)
		}
	}
	for _, a := range ev.Attendees {
		if a.Name != "" {
			event.AddAttendee("mailto:"+a.Email, ical.WithCN(a.Name))
		} else {
			event.AddAttendee("mailto:" + a.Email)
		}
	}

	if err := writeCalendar(p.cfg.Path, cal); err != nil {
		return "", err
	}
	return uid, nil
}

func loadCalendar(path string) (*ical.Calendar, error) {
	f, err := os.Open(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("open ICS %s: %w", path, err)
	}
	defer f.Close()
	cal, err := ical.ParseCalendar(f)
	if err != nil {
		return nil, fmt.Errorf("parse ICS %s: %w", path, err)
	}
	return cal, nil
}

// writeCalendar serializes atomically: write to a sibling tmp file, fsync, rename.
func writeCalendar(path string, cal *ical.Calendar) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create ICS dir: %w", err)
	}
	tmp, err := os.CreateTemp(dir, ".ics-*.tmp")
	if err != nil {
		return fmt.Errorf("create tmp ICS: %w", err)
	}
	tmpPath := tmp.Name()
	cleanup := func() { _ = os.Remove(tmpPath) }
	if err := cal.SerializeTo(tmp); err != nil {
		_ = tmp.Close()
		cleanup()
		return fmt.Errorf("serialize ICS: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		cleanup()
		return fmt.Errorf("fsync ICS: %w", err)
	}
	if err := tmp.Close(); err != nil {
		cleanup()
		return fmt.Errorf("close tmp ICS: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		cleanup()
		return fmt.Errorf("rename ICS: %w", err)
	}
	return nil
}
