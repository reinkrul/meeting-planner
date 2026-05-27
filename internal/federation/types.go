// Package federation defines the JSON shape exchanged between meeting-planner
// instances at /c/{token}/freebusy, plus a client to fetch it from a peer
// and a builder to construct the response from local state.
package federation

import (
	"fmt"
	"time"

	"github.com/reinkrul/meeting-planner/internal/availability"
	"github.com/reinkrul/meeting-planner/internal/config"
)

// FreeBusyResponse is the wire format. It carries the peer's free time
// windows (already with their own buffer applied) plus working hours and
// soft preferences. The proposing instance derives busy = working_hours -
// free for scoring purposes (back-to-back, long-stretch). No event metadata
// is ever included.
type FreeBusyResponse struct {
	Timezone      string             `json:"timezone"`
	WorkingHours  WorkingHoursWire   `json:"working_hours"`
	Free          []TimeWindowWire   `json:"free"`
	Preferences   *PreferencesWire   `json:"preferences,omitempty"`
	BufferMinutes int                `json:"buffer_minutes"`
}

type WorkingHoursWire struct {
	Start string   `json:"start"` // HH:MM
	End   string   `json:"end"`   // HH:MM
	Days  []string `json:"days"`  // [mon..sun]
}

// TimeWindowWire is a generic [start, end) interval — used for free blocks.
type TimeWindowWire struct {
	Start time.Time `json:"start"`
	End   time.Time `json:"end"`
}

type PreferencesWire struct {
	AvoidLunch             *AvoidLunchWire      `json:"avoid_lunch,omitempty"`
	AvoidBackToBack        *AvoidBackToBackWire `json:"avoid_back_to_back,omitempty"`
	AvoidLongBusyStretches *AvoidLongStretchWire `json:"avoid_long_busy_stretches,omitempty"`
	PreferMornings         *PreferMorningsWire  `json:"prefer_mornings,omitempty"`
}

type AvoidLunchWire struct {
	Enabled bool   `json:"enabled"`
	Start   string `json:"start"`
	End     string `json:"end"`
	Penalty int    `json:"penalty"`
}

type AvoidBackToBackWire struct {
	Enabled    bool `json:"enabled"`
	GapMinutes int  `json:"gap_minutes"`
	Penalty    int  `json:"penalty"`
}

type AvoidLongStretchWire struct {
	Enabled             bool `json:"enabled"`
	MaxStretchMinutes   int  `json:"max_stretch_minutes"`
	PenaltyPer30MinOver int  `json:"penalty_per_30min_over"`
}

type PreferMorningsWire struct {
	Enabled bool `json:"enabled"`
	Penalty int  `json:"penalty"`
}

// Build constructs a FreeBusyResponse from the local availability config
// and a set of pre-computed free windows (working_hours minus buffered busy,
// intersected with [from, to)).
func Build(a config.AvailabilityConfig, ownerTZ string, free []availability.FreeWindow) FreeBusyResponse {
	wire := make([]TimeWindowWire, len(free))
	for i, f := range free {
		// Normalise to UTC so wire timestamps are uniformly formatted (`…Z`).
		wire[i] = TimeWindowWire{Start: f.Start.UTC(), End: f.End.UTC()}
	}
	return FreeBusyResponse{
		Timezone: ownerTZ,
		WorkingHours: WorkingHoursWire{
			Start: a.WorkingHours.Start,
			End:   a.WorkingHours.End,
			Days:  append([]string(nil), a.WorkingDays...),
		},
		Free:          wire,
		Preferences:   prefsFromConfig(a.Rules),
		BufferMinutes: a.BufferMinutes,
	}
}

func prefsFromConfig(r config.RuleConfig) *PreferencesWire {
	return &PreferencesWire{
		AvoidLunch: &AvoidLunchWire{
			Enabled: r.AvoidLunch.Enabled, Start: r.AvoidLunch.Start,
			End: r.AvoidLunch.End, Penalty: r.AvoidLunch.Penalty,
		},
		AvoidBackToBack: &AvoidBackToBackWire{
			Enabled: r.AvoidBackToBack.Enabled,
			GapMinutes: r.AvoidBackToBack.GapMinutes,
			Penalty: r.AvoidBackToBack.Penalty,
		},
		AvoidLongBusyStretches: &AvoidLongStretchWire{
			Enabled: r.AvoidLongBusyStretches.Enabled,
			MaxStretchMinutes: r.AvoidLongBusyStretches.MaxStretchMinutes,
			PenaltyPer30MinOver: r.AvoidLongBusyStretches.PenaltyPer30MinOver,
		},
		PreferMornings: &PreferMorningsWire{
			Enabled: r.PreferMornings.Enabled,
			Penalty: r.PreferMornings.Penalty,
		},
	}
}

// ToParty converts the wire response into an availability.Party suitable
// for intersection and scoring. `label` is used for the per-party penalty
// breakdown. The [from, to) window is the same one the receiver passed to
// the peer's freebusy endpoint — needed to enumerate working windows so
// we can derive busy = working_hours - free for scoring.
//
// Peer's own buffer is already baked into the free blocks, so we set the
// resulting Party.Buffer = 0 to avoid double-applying.
func (r FreeBusyResponse) ToParty(label string, from, to time.Time) (availability.Party, error) {
	tz, err := time.LoadLocation(r.Timezone)
	if err != nil {
		return availability.Party{}, fmt.Errorf("party %s: timezone %q: %w", label, r.Timezone, err)
	}
	startMin, ok := minutesFromHHMM(r.WorkingHours.Start)
	if !ok {
		return availability.Party{}, fmt.Errorf("party %s: invalid working_hours.start %q", label, r.WorkingHours.Start)
	}
	endMin, ok := minutesFromHHMM(r.WorkingHours.End)
	if !ok {
		return availability.Party{}, fmt.Errorf("party %s: invalid working_hours.end %q", label, r.WorkingHours.End)
	}
	free := make([]availability.FreeWindow, len(r.Free))
	for i, f := range r.Free {
		free[i] = availability.FreeWindow{Start: f.Start, End: f.End}
	}

	wd := availability.WorkingDaysFromConfig(r.WorkingHours.Days)
	wh := availability.TimeOfDayWindow{StartMinutes: startMin, EndMinutes: endMin}
	busy := availability.DeriveBusyFromFree(tz, wd, wh, free, from, to)

	p := availability.Party{
		Label:        label,
		Timezone:     tz,
		WorkingDays:  wd,
		WorkingHours: wh,
		Busy:         busy,
		Buffer:       0, // already in the wire data
		Rules:        rulesFromPrefs(r.Preferences),
	}
	return p, nil
}

func rulesFromPrefs(p *PreferencesWire) config.RuleConfig {
	if p == nil {
		return config.RuleConfig{}
	}
	var rc config.RuleConfig
	if p.AvoidLunch != nil {
		rc.AvoidLunch = config.AvoidLunchRule{
			Enabled: p.AvoidLunch.Enabled, Start: p.AvoidLunch.Start,
			End: p.AvoidLunch.End, Penalty: p.AvoidLunch.Penalty,
		}
	}
	if p.AvoidBackToBack != nil {
		rc.AvoidBackToBack = config.AvoidBackToBackRule{
			Enabled: p.AvoidBackToBack.Enabled,
			GapMinutes: p.AvoidBackToBack.GapMinutes,
			Penalty: p.AvoidBackToBack.Penalty,
		}
	}
	if p.AvoidLongBusyStretches != nil {
		rc.AvoidLongBusyStretches = config.AvoidLongStretchRule{
			Enabled: p.AvoidLongBusyStretches.Enabled,
			MaxStretchMinutes: p.AvoidLongBusyStretches.MaxStretchMinutes,
			PenaltyPer30MinOver: p.AvoidLongBusyStretches.PenaltyPer30MinOver,
		}
	}
	if p.PreferMornings != nil {
		rc.PreferMornings = config.PreferMorningsRule{
			Enabled: p.PreferMornings.Enabled,
			Penalty: p.PreferMornings.Penalty,
		}
	}
	return rc
}

func minutesFromHHMM(s string) (int, bool) {
	var h, m int
	_, err := fmt.Sscanf(s, "%d:%d", &h, &m)
	if err != nil || h < 0 || h > 23 || m < 0 || m > 59 {
		return 0, false
	}
	return h*60 + m, true
}
