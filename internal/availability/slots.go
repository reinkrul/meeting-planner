package availability

import (
	"time"
)

// Slice converts free windows into candidate slots of `duration`, stepped by
// `granularity`. Candidate start times are aligned to `granularity`
// boundaries within `displayTZ` so the resulting times look natural
// (e.g. 09:00, 09:15 rather than awkward UTC offsets).
func Slice(windows []FreeWindow, duration, granularity time.Duration, displayTZ *time.Location) []Candidate {
	if displayTZ == nil {
		displayTZ = time.UTC
	}
	var out []Candidate
	for _, w := range windows {
		start := alignUp(w.Start.In(displayTZ), granularity)
		for !start.Add(duration).After(w.End) {
			out = append(out, Candidate{
				Start: start.UTC(),
				End:   start.Add(duration).UTC(),
			})
			start = start.Add(granularity)
		}
	}
	return out
}

// alignUp rounds t up to the next multiple of step, anchored to midnight in
// t's location. Returns t unchanged if already aligned.
func alignUp(t time.Time, step time.Duration) time.Time {
	midnight := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location())
	delta := t.Sub(midnight)
	rem := delta % step
	if rem == 0 {
		return t
	}
	return t.Add(step - rem)
}
