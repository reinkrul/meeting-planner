// Package icsurl implements a read-only calendar.Provider that fetches an
// ICS feed over HTTP. Used for sources like Google Calendar's "Secret
// address in iCal format" — anything you can subscribe to but not write
// back to. Recurring events (RRULE + EXDATE) are expanded within the
// requested time window.
package icsurl

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	ical "github.com/arran4/golang-ical"
	"github.com/teambition/rrule-go"

	"github.com/reinkrul/meeting-planner/internal/calendar"
	"github.com/reinkrul/meeting-planner/internal/config"
)

// ErrReadOnly is returned when CreateEvent is called on an ics_url provider.
var ErrReadOnly = errors.New("ics_url is a read-only calendar source; cannot create events")

const (
	defaultCacheTTL = 10 * time.Minute
	maxBodyBytes    = 50 << 20 // 50 MiB hard cap on a single feed fetch
)

type Provider struct {
	id  string
	cfg config.ICSURLCalendarConfig
	cli *http.Client

	mu       sync.Mutex
	cached   *ical.Calendar
	cachedAt time.Time
}

func New(id string, cfg config.ICSURLCalendarConfig) *Provider {
	return &Provider{
		id:  id,
		cfg: cfg,
		cli: &http.Client{Timeout: 30 * time.Second},
	}
}

func (p *Provider) ID() string { return p.id }

// FreeBusy returns busy blocks overlapping [from, to). Recurring events are
// expanded. Cancelled and TRANSPARENT events are skipped.
func (p *Provider) FreeBusy(ctx context.Context, from, to time.Time) ([]calendar.BusyBlock, error) {
	cal, err := p.getCalendar(ctx)
	if err != nil {
		return nil, err
	}
	var out []calendar.BusyBlock
	for _, ev := range cal.Events() {
		blocks, err := eventBusyBlocks(ev, from, to)
		if err != nil {
			// Skip malformed events rather than failing the whole feed.
			continue
		}
		out = append(out, blocks...)
	}
	return out, nil
}

// CreateEvent always fails — this provider is read-only.
func (p *Provider) CreateEvent(_ context.Context, _ calendar.Event) (string, error) {
	return "", ErrReadOnly
}

func (p *Provider) ttl() time.Duration {
	if p.cfg.CacheMinutes <= 0 {
		return defaultCacheTTL
	}
	return time.Duration(p.cfg.CacheMinutes) * time.Minute
}

func (p *Provider) getCalendar(ctx context.Context) (*ical.Calendar, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.cached != nil && time.Since(p.cachedAt) < p.ttl() {
		return p.cached, nil
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.cfg.URL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "text/calendar")
	resp, err := p.cli.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch ICS: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetch ICS: HTTP %d", resp.StatusCode)
	}
	cal, err := ical.ParseCalendar(io.LimitReader(resp.Body, maxBodyBytes))
	if err != nil {
		return nil, fmt.Errorf("parse ICS: %w", err)
	}
	p.cached = cal
	p.cachedAt = time.Now()
	return cal, nil
}

// eventBusyBlocks converts one VEVENT into the busy blocks it represents
// inside [from, to). Handles RRULE expansion + EXDATE exclusions.
func eventBusyBlocks(ev *ical.VEvent, from, to time.Time) ([]calendar.BusyBlock, error) {
	if isSkippable(ev) {
		return nil, nil
	}
	start, err := ev.GetStartAt()
	if err != nil {
		return nil, err
	}
	end, err := ev.GetEndAt()
	if err != nil {
		return nil, err
	}
	dur := end.Sub(start)
	if dur <= 0 {
		return nil, nil
	}

	rruleProp := ev.GetProperty(ical.ComponentPropertyRrule)
	if rruleProp == nil || rruleProp.Value == "" {
		// Non-recurring event: emit once if it overlaps the window.
		if end.After(from) && start.Before(to) {
			return []calendar.BusyBlock{{Start: start, End: end}}, nil
		}
		return nil, nil
	}

	// Recurring: expand within [from, to). Anchor DTSTART to the parsed
	// start time so the rule fires from the right moment.
	opt, err := rrule.StrToROptionInLocation(rruleProp.Value, start.Location())
	if err != nil {
		return nil, err
	}
	opt.Dtstart = start
	rr, err := rrule.NewRRule(*opt)
	if err != nil {
		return nil, err
	}
	set := &rrule.Set{}
	set.RRule(rr)
	for _, ex := range exDates(ev) {
		set.ExDate(ex)
	}
	// Between is half-open in practice; widen the window by the event
	// duration so a recurrence whose start is just before `from` but
	// whose end falls within still counts.
	occurrences := set.Between(from.Add(-dur), to, true)
	out := make([]calendar.BusyBlock, 0, len(occurrences))
	for _, occ := range occurrences {
		end := occ.Add(dur)
		if !end.After(from) {
			continue
		}
		out = append(out, calendar.BusyBlock{Start: occ, End: end})
	}
	return out, nil
}

func isSkippable(ev *ical.VEvent) bool {
	if st := ev.GetProperty(ical.ComponentPropertyStatus); st != nil {
		if st.Value == "CANCELLED" {
			return true
		}
	}
	if tr := ev.GetProperty(ical.ComponentPropertyTransp); tr != nil {
		if tr.Value == "TRANSPARENT" {
			return true
		}
	}
	return false
}

// exDates returns EXDATE values, tolerating mis-parses by falling back to
// the empty list rather than failing the event.
func exDates(ev *ical.VEvent) []time.Time {
	dates, err := ev.GetExDates()
	if err != nil {
		return nil
	}
	return dates
}
