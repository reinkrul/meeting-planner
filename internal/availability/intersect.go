package availability

import (
	"sort"
	"time"

	"github.com/reinkrul/meeting-planner/internal/calendar"
)

// Intersect returns the time windows in [from, to) where every party is
// (a) within their own working hours/days and (b) not busy. Each party's
// Buffer is applied to their busy blocks before subtraction.
//
// Algorithm: compute per-party free windows independently, then fold-intersect.
func Intersect(parties []Party, from, to time.Time) []FreeWindow {
	if len(parties) == 0 {
		return nil
	}
	cur := freeWindowsForParty(parties[0], from, to)
	for i := 1; i < len(parties); i++ {
		other := freeWindowsForParty(parties[i], from, to)
		cur = intersectPair(cur, other)
		if len(cur) == 0 {
			return nil
		}
	}
	return cur
}

// freeWindowsForParty builds the working-hour windows for the party in [from, to),
// then subtracts that party's (buffered, merged) busy blocks.
func freeWindowsForParty(p Party, from, to time.Time) []FreeWindow {
	work := workingWindows(p, from, to)
	if len(work) == 0 {
		return nil
	}
	busy := mergedBuffered(p.Busy, p.Buffer)
	return subtractBusy(work, busy)
}

// workingWindows enumerates each day in [from, to) and emits the
// working-hours window for that day in the party's timezone (converted to UTC).
func workingWindows(p Party, from, to time.Time) []FreeWindow {
	tz := p.Timezone
	if tz == nil {
		tz = time.UTC
	}
	// Iterate days from the day containing `from` (in party's TZ) through `to`.
	local := from.In(tz)
	start := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, tz)
	end := to.In(tz)

	var out []FreeWindow
	for day := start; !day.After(end); day = day.AddDate(0, 0, 1) {
		if !p.WorkingDays[day.Weekday()] {
			continue
		}
		ws := time.Date(day.Year(), day.Month(), day.Day(),
			0, p.WorkingHours.StartMinutes, 0, 0, tz)
		we := time.Date(day.Year(), day.Month(), day.Day(),
			0, p.WorkingHours.EndMinutes, 0, 0, tz)
		// Clamp to [from, to).
		if ws.Before(from) {
			ws = from
		}
		if we.After(to) {
			we = to
		}
		if !ws.Before(we) {
			continue
		}
		out = append(out, FreeWindow{Start: ws.UTC(), End: we.UTC()})
	}
	return out
}

// mergedBuffered pads each busy block by `buffer` on either side, sorts by
// start, and merges overlaps. Returned blocks are non-overlapping and sorted.
func mergedBuffered(busy []calendar.BusyBlock, buffer time.Duration) []calendar.BusyBlock {
	if len(busy) == 0 {
		return nil
	}
	padded := make([]calendar.BusyBlock, len(busy))
	for i, b := range busy {
		padded[i] = calendar.BusyBlock{
			Start: b.Start.Add(-buffer),
			End:   b.End.Add(buffer),
		}
	}
	sort.Slice(padded, func(i, j int) bool {
		return padded[i].Start.Before(padded[j].Start)
	})
	out := []calendar.BusyBlock{padded[0]}
	for _, b := range padded[1:] {
		last := &out[len(out)-1]
		if !b.Start.After(last.End) {
			if b.End.After(last.End) {
				last.End = b.End
			}
		} else {
			out = append(out, b)
		}
	}
	return out
}

// subtractBusy returns the parts of `windows` that aren't covered by `busy`.
// Both inputs must be sorted; `busy` must be merged.
func subtractBusy(windows []FreeWindow, busy []calendar.BusyBlock) []FreeWindow {
	if len(busy) == 0 {
		return windows
	}
	var out []FreeWindow
	for _, w := range windows {
		curStart := w.Start
		for _, b := range busy {
			if !b.End.After(curStart) {
				continue // busy ends before/at curStart
			}
			if !b.Start.Before(w.End) {
				break // busy starts at/after window end
			}
			if b.Start.After(curStart) {
				out = append(out, FreeWindow{Start: curStart, End: b.Start})
			}
			if b.End.After(curStart) {
				curStart = b.End
			}
			if !curStart.Before(w.End) {
				break
			}
		}
		if curStart.Before(w.End) {
			out = append(out, FreeWindow{Start: curStart, End: w.End})
		}
	}
	return out
}

// intersectPair returns the intersection of two sorted, non-overlapping free
// window lists.
func intersectPair(a, b []FreeWindow) []FreeWindow {
	var out []FreeWindow
	i, j := 0, 0
	for i < len(a) && j < len(b) {
		start := later(a[i].Start, b[j].Start)
		end := earlier(a[i].End, b[j].End)
		if start.Before(end) {
			out = append(out, FreeWindow{Start: start, End: end})
		}
		if a[i].End.Before(b[j].End) {
			i++
		} else {
			j++
		}
	}
	return out
}

// DeriveBusyFromFree synthesises busy blocks from a peer's free windows so
// per-party scoring rules (back-to-back, long-stretch) work on federated
// data. busy = working_hours_in_window - free_blocks. The peer's own
// buffer is already baked into `free`, so the result includes it.
func DeriveBusyFromFree(
	tz *time.Location,
	workingDays map[time.Weekday]bool,
	wh TimeOfDayWindow,
	free []FreeWindow,
	from, to time.Time,
) []calendar.BusyBlock {
	p := Party{
		Timezone:     tz,
		WorkingDays:  workingDays,
		WorkingHours: wh,
	}
	work := workingWindows(p, from, to)
	// Treat free blocks as "to-subtract"; the resulting gaps are busy.
	asBusy := make([]calendar.BusyBlock, len(free))
	for i, f := range free {
		asBusy[i] = calendar.BusyBlock{Start: f.Start, End: f.End}
	}
	merged := mergedBuffered(asBusy, 0)
	gaps := subtractBusy(work, merged)
	out := make([]calendar.BusyBlock, 0, len(gaps))
	for _, g := range gaps {
		out = append(out, calendar.BusyBlock{Start: g.Start, End: g.End})
	}
	return out
}

func later(a, b time.Time) time.Time {
	if a.After(b) {
		return a
	}
	return b
}

func earlier(a, b time.Time) time.Time {
	if a.Before(b) {
		return a
	}
	return b
}
