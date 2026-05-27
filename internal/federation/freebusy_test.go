package federation

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/reinkrul/meeting-planner/internal/availability"
	"github.com/reinkrul/meeting-planner/internal/config"
)

// TestBuildAndRoundTrip verifies a built response marshals/unmarshals and
// converts back to a Party where busy is synthesised from the wire's free
// blocks + working hours.
func TestBuildAndRoundTrip(t *testing.T) {
	a := config.AvailabilityConfig{
		WorkingDays:   []string{"mon", "tue", "wed", "thu", "fri"},
		WorkingHours:  config.TimeWindow{Start: "09:00", End: "17:00"},
		BufferMinutes: 0,
		Rules: config.RuleConfig{
			AvoidLunch: config.AvoidLunchRule{
				Enabled: true, Start: "12:00", End: "13:00", Penalty: 50,
			},
		},
	}
	tz := "Europe/Amsterdam"
	// One busy block in the middle of the working day: 10:00–11:00 UTC
	// (= 12:00–13:00 CEST). With working hours 09:00–17:00 CEST
	// (= 07:00–15:00 UTC), the free windows are [07:00,10:00) and [11:00,15:00).
	free := []availability.FreeWindow{
		{Start: time.Date(2026, 5, 27, 7, 0, 0, 0, time.UTC), End: time.Date(2026, 5, 27, 10, 0, 0, 0, time.UTC)},
		{Start: time.Date(2026, 5, 27, 11, 0, 0, 0, time.UTC), End: time.Date(2026, 5, 27, 15, 0, 0, 0, time.UTC)},
	}
	resp := Build(a, tz, free)

	raw, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got FreeBusyResponse
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	from := time.Date(2026, 5, 27, 0, 0, 0, 0, time.UTC)
	to := time.Date(2026, 5, 28, 0, 0, 0, 0, time.UTC)
	party, err := got.ToParty("peer", from, to)
	if err != nil {
		t.Fatalf("ToParty: %v", err)
	}
	if party.Label != "peer" {
		t.Errorf("label: got %q", party.Label)
	}
	if party.Timezone.String() != tz {
		t.Errorf("timezone: got %q, want %q", party.Timezone.String(), tz)
	}
	if party.WorkingHours.StartMinutes != 9*60 || party.WorkingHours.EndMinutes != 17*60 {
		t.Errorf("working hours wrong: %+v", party.WorkingHours)
	}
	// Derived busy should be exactly the 10:00–11:00 UTC gap.
	if len(party.Busy) != 1 {
		t.Fatalf("want 1 derived busy block, got %d (%+v)", len(party.Busy), party.Busy)
	}
	wantStart := time.Date(2026, 5, 27, 10, 0, 0, 0, time.UTC)
	wantEnd := time.Date(2026, 5, 27, 11, 0, 0, 0, time.UTC)
	if !party.Busy[0].Start.Equal(wantStart) || !party.Busy[0].End.Equal(wantEnd) {
		t.Errorf("derived busy: got %v..%v, want %v..%v",
			party.Busy[0].Start, party.Busy[0].End, wantStart, wantEnd)
	}
	if !party.Rules.AvoidLunch.Enabled || party.Rules.AvoidLunch.Penalty != 50 {
		t.Errorf("avoid_lunch rule lost: %+v", party.Rules.AvoidLunch)
	}
	if party.Buffer != 0 {
		t.Errorf("buffer: got %v, want 0 (peer buffer already baked into free blocks)", party.Buffer)
	}
}
