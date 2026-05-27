// Package google implements calendar.Provider on top of Google Calendar.
// FreeBusy uses freebusy.Query (no event metadata leaks). CreateEvent uses
// events.Insert with sendUpdates=all so Google emails invites to attendees.
package google

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"
	"time"

	"golang.org/x/oauth2"
	googleoauth "golang.org/x/oauth2/google"
	googlecal "google.golang.org/api/calendar/v3"
	"google.golang.org/api/option"

	"github.com/reinkrul/meeting-planner/internal/calendar"
	"github.com/reinkrul/meeting-planner/internal/config"
	"github.com/reinkrul/meeting-planner/internal/store"
)

// ErrNotAuthenticated is returned by calendar operations when no OAuth token
// is stored for the calendar. Callers route the operator to /admin to connect.
var ErrNotAuthenticated = errors.New("calendar is not authenticated; visit /admin to connect")

// Provider is a Google Calendar-backed calendar.Provider.
type Provider struct {
	id    string
	cfg   config.GoogleCalendarConfig
	state *store.State
	oauth *oauth2.Config
}

// New constructs a Google provider. publicBaseURL is used to derive the
// OAuth redirect URI: <publicBaseURL>/admin/calendars/<id>/callback.
func New(id string, cfg config.GoogleCalendarConfig, st *store.State, publicBaseURL string) *Provider {
	return &Provider{
		id:    id,
		cfg:   cfg,
		state: st,
		oauth: &oauth2.Config{
			ClientID:     os.Getenv(cfg.ClientIDEnv),
			ClientSecret: os.Getenv(cfg.ClientSecretEnv),
			Endpoint:     googleoauth.Endpoint,
			RedirectURL:  publicBaseURL + "/admin/calendars/" + id + "/callback",
			Scopes: []string{
				googlecal.CalendarEventsScope,
				googlecal.CalendarFreebusyScope,
			},
		},
	}
}

func (p *Provider) ID() string { return p.id }

// AuthCodeURL returns the URL to send the operator to for consent.
// AccessTypeOffline + prompt=consent ensures Google issues a refresh token
// even on repeat connects (otherwise refresh_token is only sent once).
func (p *Provider) AuthCodeURL(state string) string {
	return p.oauth.AuthCodeURL(state,
		oauth2.AccessTypeOffline,
		oauth2.SetAuthURLParam("prompt", "consent"),
	)
}

// Exchange swaps an authorization code for tokens and persists them.
func (p *Provider) Exchange(ctx context.Context, code string) error {
	tok, err := p.oauth.Exchange(ctx, code)
	if err != nil {
		return fmt.Errorf("oauth exchange: %w", err)
	}
	return p.state.SetOAuthToken(p.id, tok)
}

func (p *Provider) FreeBusy(ctx context.Context, from, to time.Time) ([]calendar.BusyBlock, error) {
	svc, err := p.service(ctx)
	if err != nil {
		return nil, err
	}
	resp, err := svc.Freebusy.Query(&googlecal.FreeBusyRequest{
		TimeMin: from.UTC().Format(time.RFC3339),
		TimeMax: to.UTC().Format(time.RFC3339),
		Items:   []*googlecal.FreeBusyRequestItem{{Id: p.cfg.CalendarID}},
	}).Context(ctx).Do()
	if err != nil {
		return nil, fmt.Errorf("freebusy.Query: %w", err)
	}
	cal, ok := resp.Calendars[p.cfg.CalendarID]
	if !ok {
		return nil, nil
	}
	out := make([]calendar.BusyBlock, 0, len(cal.Busy))
	for _, b := range cal.Busy {
		start, err := time.Parse(time.RFC3339, b.Start)
		if err != nil {
			continue
		}
		end, err := time.Parse(time.RFC3339, b.End)
		if err != nil {
			continue
		}
		out = append(out, calendar.BusyBlock{Start: start, End: end})
	}
	return out, nil
}

func (p *Provider) CreateEvent(ctx context.Context, ev calendar.Event) (string, error) {
	svc, err := p.service(ctx)
	if err != nil {
		return "", err
	}
	attendees := make([]*googlecal.EventAttendee, 0, len(ev.Attendees))
	for _, a := range ev.Attendees {
		attendees = append(attendees, &googlecal.EventAttendee{
			Email:       a.Email,
			DisplayName: a.Name,
		})
	}
	e := &googlecal.Event{
		Summary:     ev.Title,
		Description: ev.Description,
		Location:    ev.Location,
		Start:       &googlecal.EventDateTime{DateTime: ev.Start.UTC().Format(time.RFC3339)},
		End:         &googlecal.EventDateTime{DateTime: ev.End.UTC().Format(time.RFC3339)},
		Attendees:   attendees,
	}
	created, err := svc.Events.Insert(p.cfg.CalendarID, e).SendUpdates("all").Context(ctx).Do()
	if err != nil {
		return "", fmt.Errorf("events.Insert: %w", err)
	}
	return created.Id, nil
}

func (p *Provider) service(ctx context.Context) (*googlecal.Service, error) {
	tok := p.state.OAuthToken(p.id)
	if tok == nil {
		return nil, ErrNotAuthenticated
	}
	src := p.oauth.TokenSource(ctx, tok)
	src = &persistingTokenSource{src: src, calendarID: p.id, state: p.state, last: tok}
	return googlecal.NewService(ctx, option.WithTokenSource(src))
}

// persistingTokenSource writes refreshed tokens back to the state store so
// the refresh token (and updated expiry) survive process restarts.
type persistingTokenSource struct {
	src        oauth2.TokenSource
	calendarID string
	state      *store.State
	mu         sync.Mutex
	last       *oauth2.Token
}

func (p *persistingTokenSource) Token() (*oauth2.Token, error) {
	tok, err := p.src.Token()
	if err != nil {
		return nil, err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.last == nil || tok.AccessToken != p.last.AccessToken {
		_ = p.state.SetOAuthToken(p.calendarID, tok) // best-effort
		p.last = tok
	}
	return tok, nil
}
