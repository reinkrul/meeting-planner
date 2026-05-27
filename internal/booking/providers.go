package booking

import (
	"fmt"

	"github.com/reinkrul/meeting-planner/internal/calendar"
	"github.com/reinkrul/meeting-planner/internal/calendar/caldav"
	googleprov "github.com/reinkrul/meeting-planner/internal/calendar/google"
	"github.com/reinkrul/meeting-planner/internal/calendar/icsfile"
	"github.com/reinkrul/meeting-planner/internal/calendar/icsurl"
	"github.com/reinkrul/meeting-planner/internal/calendar/ox"
	"github.com/reinkrul/meeting-planner/internal/config"
	"github.com/reinkrul/meeting-planner/internal/store"
)

// BuildProviders instantiates a calendar.Provider for each configured calendar.
// The result is keyed by calendar ID.
func BuildProviders(cfg config.Config, st *store.State) (map[string]calendar.Provider, error) {
	out := map[string]calendar.Provider{}
	for _, c := range cfg.Calendars {
		switch c.Provider {
		case "google":
			out[c.ID] = googleprov.New(c.ID, *c.Google, st, cfg.Server.PublicBaseURL)
		case "ics_file":
			out[c.ID] = icsfile.New(c.ID, *c.ICSFile)
		case "caldav":
			p, err := caldav.New(c.ID, *c.CalDAV)
			if err != nil {
				return nil, fmt.Errorf("calendar %q: %w", c.ID, err)
			}
			out[c.ID] = p
		case "ox":
			p, err := ox.New(c.ID, *c.OX)
			if err != nil {
				return nil, fmt.Errorf("calendar %q: %w", c.ID, err)
			}
			out[c.ID] = p
		case "ics_url":
			out[c.ID] = icsurl.New(c.ID, *c.ICSURL)
		default:
			return nil, fmt.Errorf("calendar %q: unknown provider %q", c.ID, c.Provider)
		}
	}
	return out, nil
}
