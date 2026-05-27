// Package availability computes free slots across multiple parties (owner +
// peers) and scores them against per-party soft rules.
package availability

import (
	"time"

	"github.com/reinkrul/meeting-planner/internal/calendar"
	"github.com/reinkrul/meeting-planner/internal/config"
)

// Party represents one calendar-holding participant in a multi-party meeting.
// Built once from local config (owner) and once per peer free-busy response.
type Party struct {
	Label        string         // for display (e.g. "owner", peer email/URL host)
	Timezone     *time.Location // for interpreting working-hours and rules
	WorkingDays  map[time.Weekday]bool
	WorkingHours TimeOfDayWindow
	Busy         []calendar.BusyBlock
	Buffer       time.Duration     // padded around each busy block
	Rules        config.RuleConfig // soft scoring rules
}

// TimeOfDayWindow is a half-open [Start, End) window in minutes since midnight.
type TimeOfDayWindow struct {
	StartMinutes int
	EndMinutes   int
}

// FreeWindow is a contiguous range during which all relevant parties are
// within their working hours and none is busy.
type FreeWindow struct {
	Start time.Time
	End   time.Time
}

// Candidate is a proposed meeting slot (always [Start, End)).
type Candidate struct {
	Start time.Time
	End   time.Time
}

// ScoredCandidate carries a candidate plus its total penalty and per-party
// breakdown (used by the UI to explain rankings).
type ScoredCandidate struct {
	Candidate Candidate
	Total     int
	PerParty  map[string][]RuleResult // partyLabel -> rule penalties
}

// RuleResult is one rule's contribution to a party's score.
type RuleResult struct {
	Rule    string
	Penalty int
}

// dayCodeToWeekday maps short config day codes to time.Weekday.
var dayCodeToWeekday = map[string]time.Weekday{
	"sun": time.Sunday,
	"mon": time.Monday,
	"tue": time.Tuesday,
	"wed": time.Wednesday,
	"thu": time.Thursday,
	"fri": time.Friday,
	"sat": time.Saturday,
}

// WorkingDaysFromConfig converts ["mon","tue",...] into a set keyed by weekday.
func WorkingDaysFromConfig(days []string) map[time.Weekday]bool {
	out := map[time.Weekday]bool{}
	for _, d := range days {
		if wd, ok := dayCodeToWeekday[d]; ok {
			out[wd] = true
		}
	}
	return out
}
