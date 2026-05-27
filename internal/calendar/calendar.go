// Package calendar defines the Provider abstraction over different calendar
// backends (Google, ICS file, future CalDAV/Proton).
package calendar

import (
	"context"
	"time"
)

// BusyBlock is an opaque busy time range. Providers MUST NOT leak any other
// metadata (titles, attendees, descriptions) through this type — privacy is
// enforced at this seam.
type BusyBlock struct {
	Start time.Time
	End   time.Time
}

type Attendee struct {
	Name  string
	Email string
}

// Event is the input to CreateEvent. Times must be timezone-aware (UTC is fine).
type Event struct {
	Title       string
	Description string
	Start, End  time.Time
	Attendees   []Attendee
	Location    string
}

// Provider abstracts a calendar backend.
type Provider interface {
	// ID returns the configured calendar id (e.g. "work").
	ID() string
	// FreeBusy returns busy blocks overlapping [from, to). Order and merging
	// are the caller's responsibility.
	FreeBusy(ctx context.Context, from, to time.Time) ([]BusyBlock, error)
	// CreateEvent creates the event on the underlying calendar and returns
	// a provider-side identifier. Implementations that support it (e.g. Google)
	// should arrange for invite emails to be sent to attendees.
	CreateEvent(ctx context.Context, ev Event) (string, error)
}
