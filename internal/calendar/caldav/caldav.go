// Package caldav implements calendar.Provider over the CalDAV protocol.
// Authenticates with HTTP Basic (username + password / app-password). No
// OAuth client / Google Cloud project required — works with Google Calendar,
// Apple iCloud, Fastmail, Proton (via Bridge), Nextcloud, etc.
package caldav

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/emersion/go-ical"
	"github.com/emersion/go-webdav"
	caldavclient "github.com/emersion/go-webdav/caldav"

	"github.com/reinkrul/meeting-planner/internal/calendar"
	"github.com/reinkrul/meeting-planner/internal/config"
)

type Provider struct {
	id       string
	cfg      config.CalDAVCalendarConfig
	client   *caldavclient.Client
	calendar string // path passed to QueryCalendar / PutCalendarObject
}

// New constructs a CalDAV provider. The password is read from cfg.PasswordEnv
// at construction time; missing → error.
func New(id string, cfg config.CalDAVCalendarConfig) (*Provider, error) {
	pw := os.Getenv(cfg.PasswordEnv)
	if pw == "" {
		return nil, fmt.Errorf("env var %s is not set", cfg.PasswordEnv)
	}
	httpc := webdav.HTTPClientWithBasicAuth(http.DefaultClient, cfg.Username, pw)
	cli, err := caldavclient.NewClient(httpc, cfg.ServerURL)
	if err != nil {
		return nil, fmt.Errorf("caldav client: %w", err)
	}
	return &Provider{id: id, cfg: cfg, client: cli, calendar: ""}, nil
}

func (p *Provider) ID() string { return p.id }

// FreeBusy issues a CalDAV REPORT (calendar-query) for VEVENTs overlapping
// [from, to). Cancelled and transparent (=free) events are skipped.
func (p *Provider) FreeBusy(ctx context.Context, from, to time.Time) ([]calendar.BusyBlock, error) {
	q := &caldavclient.CalendarQuery{
		CompRequest: caldavclient.CalendarCompRequest{
			Name: "VCALENDAR",
			Comps: []caldavclient.CalendarCompRequest{{
				Name: "VEVENT",
				Props: []string{
					ical.PropUID,
					ical.PropDateTimeStart,
					ical.PropDateTimeEnd,
					ical.PropStatus,
					ical.PropTransparency,
				},
			}},
		},
		CompFilter: caldavclient.CompFilter{
			Name: "VCALENDAR",
			Comps: []caldavclient.CompFilter{{
				Name:  "VEVENT",
				Start: from,
				End:   to,
			}},
		},
	}
	objs, err := p.client.QueryCalendar(ctx, p.calendar, q)
	if err != nil {
		return nil, fmt.Errorf("caldav query: %w", err)
	}
	var out []calendar.BusyBlock
	for _, obj := range objs {
		if obj.Data == nil {
			continue
		}
		for _, comp := range obj.Data.Children {
			if comp.Name != ical.CompEvent {
				continue
			}
			if t := comp.Props.Get(ical.PropTransparency); t != nil && strings.EqualFold(t.Value, "TRANSPARENT") {
				continue
			}
			if s := comp.Props.Get(ical.PropStatus); s != nil && strings.EqualFold(s.Value, "CANCELLED") {
				continue
			}
			start, err := comp.Props.DateTime(ical.PropDateTimeStart, time.UTC)
			if err != nil {
				continue
			}
			end, err := comp.Props.DateTime(ical.PropDateTimeEnd, time.UTC)
			if err != nil {
				continue
			}
			if !end.After(from) || !start.Before(to) {
				continue
			}
			out = append(out, calendar.BusyBlock{Start: start, End: end})
		}
	}
	return out, nil
}

// CreateEvent PUTs a new VEVENT to the calendar collection. Per RFC 6638
// (CalDAV scheduling), servers like Google process ATTENDEEs and dispatch
// invite emails automatically.
func (p *Provider) CreateEvent(ctx context.Context, ev calendar.Event) (string, error) {
	uid := newUID()
	now := time.Now().UTC()

	cal := ical.NewCalendar()
	cal.Props.SetText(ical.PropVersion, "2.0")
	cal.Props.SetText(ical.PropProductID, "-//meeting-planner//EN")

	event := ical.NewEvent()
	event.Props.SetText(ical.PropUID, uid)
	event.Props.SetDateTime(ical.PropDateTimeStamp, now)
	event.Props.SetDateTime(ical.PropDateTimeStart, ev.Start.UTC())
	event.Props.SetDateTime(ical.PropDateTimeEnd, ev.End.UTC())
	event.Props.SetText(ical.PropSummary, ev.Title)
	if ev.Description != "" {
		event.Props.SetText(ical.PropDescription, ev.Description)
	}
	if ev.Location != "" {
		event.Props.SetText(ical.PropLocation, ev.Location)
	}

	organizer := ical.NewProp(ical.PropOrganizer)
	organizer.Value = "mailto:" + p.cfg.Username
	if cn := p.cfg.OrganizerCN; cn != "" {
		organizer.Params.Set("CN", cn)
	}
	event.Props.Set(organizer)

	for _, a := range ev.Attendees {
		att := ical.NewProp(ical.PropAttendee)
		att.Value = "mailto:" + a.Email
		if a.Name != "" {
			att.Params.Set("CN", a.Name)
		}
		att.Params.Set("RSVP", "TRUE")
		att.Params.Set("PARTSTAT", "NEEDS-ACTION")
		event.Props.Add(att)
	}

	cal.Children = append(cal.Children, event.Component)

	objPath := strings.TrimRight(p.cfg.ServerURL, "/") + "/" + uid + ".ics"
	if _, err := p.client.PutCalendarObject(ctx, objPath, cal); err != nil {
		return "", fmt.Errorf("caldav put: %w", err)
	}
	return uid, nil
}

func newUID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:]) + "@meeting-planner"
}
