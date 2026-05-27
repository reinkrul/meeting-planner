package availability

import (
	"testing"
	"time"
)

func TestSlice_BasicGranularityAndDuration(t *testing.T) {
	tz := mustTZ(t, "UTC")
	w := FreeWindow{
		Start: time.Date(2026, 5, 27, 9, 0, 0, 0, tz),
		End:   time.Date(2026, 5, 27, 10, 0, 0, 0, tz),
	}
	got := Slice([]FreeWindow{w}, 30*time.Minute, 15*time.Minute, tz)
	// In a 60-min window, 30-min slots at 15-min granularity should yield:
	// 09:00, 09:15, 09:30 (last that fits with +30m ≤ 10:00).
	wantStarts := []time.Time{
		time.Date(2026, 5, 27, 9, 0, 0, 0, tz),
		time.Date(2026, 5, 27, 9, 15, 0, 0, tz),
		time.Date(2026, 5, 27, 9, 30, 0, 0, tz),
	}
	if len(got) != len(wantStarts) {
		t.Fatalf("want %d slots, got %d: %+v", len(wantStarts), len(got), got)
	}
	for i, c := range got {
		if !c.Start.Equal(wantStarts[i]) {
			t.Errorf("slot %d: got start %v, want %v", i, c.Start, wantStarts[i])
		}
	}
}

func TestSlice_AlignsToGranularityInDisplayTZ(t *testing.T) {
	tz := mustTZ(t, "Europe/Amsterdam")
	// Window starts at 09:07 local (off-grid for 15-min granularity).
	w := FreeWindow{
		Start: time.Date(2026, 5, 27, 9, 7, 0, 0, tz),
		End:   time.Date(2026, 5, 27, 10, 0, 0, 0, tz),
	}
	got := Slice([]FreeWindow{w}, 30*time.Minute, 15*time.Minute, tz)
	if len(got) == 0 {
		t.Fatalf("want >=1 slot, got 0")
	}
	first := got[0].Start.In(tz)
	if first.Minute()%15 != 0 {
		t.Errorf("first slot start not aligned to 15min: %v", first)
	}
	want := time.Date(2026, 5, 27, 9, 15, 0, 0, tz)
	if !first.Equal(want) {
		t.Errorf("first slot: got %v, want %v", first, want)
	}
}

func TestSlice_DurationLongerThanWindowYieldsNothing(t *testing.T) {
	tz := mustTZ(t, "UTC")
	w := FreeWindow{
		Start: time.Date(2026, 5, 27, 9, 0, 0, 0, tz),
		End:   time.Date(2026, 5, 27, 9, 30, 0, 0, tz),
	}
	got := Slice([]FreeWindow{w}, 60*time.Minute, 15*time.Minute, tz)
	if len(got) != 0 {
		t.Errorf("want 0 slots, got %d", len(got))
	}
}
