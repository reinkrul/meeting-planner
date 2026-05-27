package availability

import (
	"testing"
	"time"

	"github.com/reinkrul/meeting-planner/internal/calendar"
)

func mustTZ(t *testing.T, name string) *time.Location {
	t.Helper()
	tz, err := time.LoadLocation(name)
	if err != nil {
		t.Fatalf("load %s: %v", name, err)
	}
	return tz
}

// mkParty is a convenience for table-driven tests.
func mkParty(tz *time.Location, days []time.Weekday, startMin, endMin int, busy []calendar.BusyBlock) Party {
	wd := map[time.Weekday]bool{}
	for _, d := range days {
		wd[d] = true
	}
	return Party{
		Label:        "p",
		Timezone:     tz,
		WorkingDays:  wd,
		WorkingHours: TimeOfDayWindow{StartMinutes: startMin, EndMinutes: endMin},
		Busy:         busy,
	}
}

func TestIntersect_SinglePartyNoBusy(t *testing.T) {
	tz := mustTZ(t, "UTC")
	// Wednesday 2026-05-27 → working hours 09:00–17:00 UTC.
	p := mkParty(tz, []time.Weekday{time.Wednesday}, 9*60, 17*60, nil)
	from := time.Date(2026, 5, 27, 0, 0, 0, 0, tz)
	to := time.Date(2026, 5, 28, 0, 0, 0, 0, tz)
	got := Intersect([]Party{p}, from, to)
	if len(got) != 1 {
		t.Fatalf("want 1 window, got %d", len(got))
	}
	want := FreeWindow{
		Start: time.Date(2026, 5, 27, 9, 0, 0, 0, tz),
		End:   time.Date(2026, 5, 27, 17, 0, 0, 0, tz),
	}
	if !got[0].Start.Equal(want.Start) || !got[0].End.Equal(want.End) {
		t.Errorf("window mismatch: got %v..%v, want %v..%v", got[0].Start, got[0].End, want.Start, want.End)
	}
}

func TestIntersect_NonWorkingDayProducesNoWindow(t *testing.T) {
	tz := mustTZ(t, "UTC")
	// Saturday: not a working day.
	p := mkParty(tz, []time.Weekday{time.Monday, time.Tuesday, time.Wednesday, time.Thursday, time.Friday}, 9*60, 17*60, nil)
	from := time.Date(2026, 5, 30, 0, 0, 0, 0, tz) // Sat
	to := time.Date(2026, 5, 31, 0, 0, 0, 0, tz)   // Sun
	got := Intersect([]Party{p}, from, to)
	if len(got) != 0 {
		t.Errorf("want 0 windows on Saturday, got %d", len(got))
	}
}

func TestIntersect_BusyBlockSubtracted(t *testing.T) {
	tz := mustTZ(t, "UTC")
	// Wed 10:00–11:00 busy; expect 09:00–10:00 and 11:00–17:00 free.
	busy := []calendar.BusyBlock{{
		Start: time.Date(2026, 5, 27, 10, 0, 0, 0, tz),
		End:   time.Date(2026, 5, 27, 11, 0, 0, 0, tz),
	}}
	p := mkParty(tz, []time.Weekday{time.Wednesday}, 9*60, 17*60, busy)
	from := time.Date(2026, 5, 27, 0, 0, 0, 0, tz)
	to := time.Date(2026, 5, 28, 0, 0, 0, 0, tz)
	got := Intersect([]Party{p}, from, to)
	if len(got) != 2 {
		t.Fatalf("want 2 free windows, got %d", len(got))
	}
}

func TestIntersect_TwoPartiesDifferentTimezones(t *testing.T) {
	// Owner in Europe/Amsterdam (UTC+2 in May), Peer in America/New_York (UTC-4).
	// Both 09:00–17:00 LOCAL.
	// Amsterdam 09:00–17:00 == 07:00–15:00 UTC on Wed 2026-05-27.
	// New York 09:00–17:00 == 13:00–21:00 UTC on Wed 2026-05-27.
	// Intersection: 13:00–15:00 UTC.
	ams := mustTZ(t, "Europe/Amsterdam")
	nyc := mustTZ(t, "America/New_York")
	owner := mkParty(ams, []time.Weekday{time.Wednesday}, 9*60, 17*60, nil)
	peer := mkParty(nyc, []time.Weekday{time.Wednesday}, 9*60, 17*60, nil)

	from := time.Date(2026, 5, 27, 0, 0, 0, 0, time.UTC)
	to := time.Date(2026, 5, 28, 0, 0, 0, 0, time.UTC)
	got := Intersect([]Party{owner, peer}, from, to)
	if len(got) != 1 {
		t.Fatalf("want 1 overlap window, got %d (%+v)", len(got), got)
	}
	wantStart := time.Date(2026, 5, 27, 13, 0, 0, 0, time.UTC)
	wantEnd := time.Date(2026, 5, 27, 15, 0, 0, 0, time.UTC)
	if !got[0].Start.Equal(wantStart) || !got[0].End.Equal(wantEnd) {
		t.Errorf("window mismatch: got %v..%v, want %v..%v", got[0].Start, got[0].End, wantStart, wantEnd)
	}
}

func TestIntersect_BufferAppliedToBusy(t *testing.T) {
	tz := mustTZ(t, "UTC")
	busy := []calendar.BusyBlock{{
		Start: time.Date(2026, 5, 27, 10, 0, 0, 0, tz),
		End:   time.Date(2026, 5, 27, 11, 0, 0, 0, tz),
	}}
	p := mkParty(tz, []time.Weekday{time.Wednesday}, 9*60, 17*60, busy)
	p.Buffer = 30 * time.Minute
	from := time.Date(2026, 5, 27, 0, 0, 0, 0, tz)
	to := time.Date(2026, 5, 28, 0, 0, 0, 0, tz)
	got := Intersect([]Party{p}, from, to)
	if len(got) != 2 {
		t.Fatalf("want 2 windows after buffer, got %d", len(got))
	}
	// First window should now end at 09:30 (10:00 - buffer 30m).
	want1End := time.Date(2026, 5, 27, 9, 30, 0, 0, tz)
	if !got[0].End.Equal(want1End) {
		t.Errorf("first window end: got %v, want %v", got[0].End, want1End)
	}
	// Second window should start at 11:30.
	want2Start := time.Date(2026, 5, 27, 11, 30, 0, 0, tz)
	if !got[1].Start.Equal(want2Start) {
		t.Errorf("second window start: got %v, want %v", got[1].Start, want2Start)
	}
}
