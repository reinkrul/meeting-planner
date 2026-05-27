package availability

import (
	"testing"
	"time"

	"github.com/reinkrul/meeting-planner/internal/calendar"
	"github.com/reinkrul/meeting-planner/internal/config"
)

func TestAvoidLunch_FullOverlapHitsFullPenalty(t *testing.T) {
	tz := mustTZ(t, "UTC")
	p := Party{
		Timezone: tz,
		Rules: config.RuleConfig{AvoidLunch: config.AvoidLunchRule{
			Enabled: true, Start: "12:00", End: "13:00", Penalty: 50,
		}},
	}
	c := Candidate{
		Start: time.Date(2026, 5, 27, 12, 0, 0, 0, tz),
		End:   time.Date(2026, 5, 27, 13, 0, 0, 0, tz),
	}
	if got := scoreAvoidLunch(c, p); got != 50 {
		t.Errorf("want 50, got %d", got)
	}
}

func TestAvoidLunch_NoOverlapZero(t *testing.T) {
	tz := mustTZ(t, "UTC")
	p := Party{
		Timezone: tz,
		Rules: config.RuleConfig{AvoidLunch: config.AvoidLunchRule{
			Enabled: true, Start: "12:00", End: "13:00", Penalty: 50,
		}},
	}
	c := Candidate{
		Start: time.Date(2026, 5, 27, 14, 0, 0, 0, tz),
		End:   time.Date(2026, 5, 27, 14, 30, 0, 0, tz),
	}
	if got := scoreAvoidLunch(c, p); got != 0 {
		t.Errorf("want 0, got %d", got)
	}
}

func TestAvoidLunch_HalfOverlapProportional(t *testing.T) {
	tz := mustTZ(t, "UTC")
	p := Party{
		Timezone: tz,
		Rules: config.RuleConfig{AvoidLunch: config.AvoidLunchRule{
			Enabled: true, Start: "12:00", End: "13:00", Penalty: 50,
		}},
	}
	// 12:30–13:30 overlaps lunch for 30 of 60 min → penalty 25.
	c := Candidate{
		Start: time.Date(2026, 5, 27, 12, 30, 0, 0, tz),
		End:   time.Date(2026, 5, 27, 13, 30, 0, 0, tz),
	}
	if got := scoreAvoidLunch(c, p); got != 25 {
		t.Errorf("want 25, got %d", got)
	}
}

func TestAvoidBackToBack_AdjacentBusyHits(t *testing.T) {
	tz := mustTZ(t, "UTC")
	p := Party{
		Timezone: tz,
		Busy: []calendar.BusyBlock{{
			Start: time.Date(2026, 5, 27, 9, 30, 0, 0, tz),
			End:   time.Date(2026, 5, 27, 10, 0, 0, 0, tz),
		}},
		Rules: config.RuleConfig{AvoidBackToBack: config.AvoidBackToBackRule{
			Enabled: true, GapMinutes: 15, Penalty: 30,
		}},
	}
	// Candidate 10:00–10:30 is directly after the 09:30–10:00 block (gap 0 < 15).
	c := Candidate{
		Start: time.Date(2026, 5, 27, 10, 0, 0, 0, tz),
		End:   time.Date(2026, 5, 27, 10, 30, 0, 0, tz),
	}
	if got := scoreAvoidBackToBack(c, p); got != 30 {
		t.Errorf("want 30, got %d", got)
	}
}

func TestAvoidBackToBack_SandwichScoresTwice(t *testing.T) {
	tz := mustTZ(t, "UTC")
	p := Party{
		Timezone: tz,
		Busy: []calendar.BusyBlock{
			{Start: time.Date(2026, 5, 27, 9, 30, 0, 0, tz), End: time.Date(2026, 5, 27, 10, 0, 0, 0, tz)},
			{Start: time.Date(2026, 5, 27, 10, 30, 0, 0, tz), End: time.Date(2026, 5, 27, 11, 0, 0, 0, tz)},
		},
		Rules: config.RuleConfig{AvoidBackToBack: config.AvoidBackToBackRule{
			Enabled: true, GapMinutes: 15, Penalty: 30,
		}},
	}
	c := Candidate{
		Start: time.Date(2026, 5, 27, 10, 0, 0, 0, tz),
		End:   time.Date(2026, 5, 27, 10, 30, 0, 0, tz),
	}
	if got := scoreAvoidBackToBack(c, p); got != 60 {
		t.Errorf("want 60 (sandwich), got %d", got)
	}
}

func TestAvoidLongStretch_PushesOverCap(t *testing.T) {
	tz := mustTZ(t, "UTC")
	// 3h of existing back-to-back: 09:00–12:00. Cap is 240 (4h). Adding a 90-min
	// slot directly after pushes the stretch to 4h30 → over by 30 → 1 chunk × 20 = 20.
	p := Party{
		Timezone: tz,
		Busy: []calendar.BusyBlock{
			{Start: time.Date(2026, 5, 27, 9, 0, 0, 0, tz), End: time.Date(2026, 5, 27, 12, 0, 0, 0, tz)},
		},
		Rules: config.RuleConfig{
			AvoidBackToBack: config.AvoidBackToBackRule{Enabled: false, GapMinutes: 15},
			AvoidLongBusyStretches: config.AvoidLongStretchRule{
				Enabled: true, MaxStretchMinutes: 240, PenaltyPer30MinOver: 20,
			},
		},
	}
	c := Candidate{
		Start: time.Date(2026, 5, 27, 12, 0, 0, 0, tz),
		End:   time.Date(2026, 5, 27, 13, 30, 0, 0, tz),
	}
	if got := scoreAvoidLongStretch(c, p); got != 20 {
		t.Errorf("want 20, got %d", got)
	}
}

func TestScoreAll_SumsAcrossParties(t *testing.T) {
	tz := mustTZ(t, "UTC")
	owner := Party{
		Label:    "owner",
		Timezone: tz,
		Rules: config.RuleConfig{AvoidLunch: config.AvoidLunchRule{
			Enabled: true, Start: "12:00", End: "13:00", Penalty: 50,
		}},
	}
	peer := Party{
		Label:    "peer",
		Timezone: tz,
		Rules: config.RuleConfig{AvoidLunch: config.AvoidLunchRule{
			Enabled: true, Start: "12:00", End: "13:00", Penalty: 80,
		}},
	}
	c := Candidate{
		Start: time.Date(2026, 5, 27, 12, 0, 0, 0, tz),
		End:   time.Date(2026, 5, 27, 13, 0, 0, 0, tz),
	}
	got := ScoreAll(c, []Party{owner, peer})
	if got.Total != 130 {
		t.Errorf("want total 130 (50+80), got %d", got.Total)
	}
	if len(got.PerParty) != 2 {
		t.Errorf("want 2 party entries, got %d", len(got.PerParty))
	}
}
