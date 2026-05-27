package availability

import (
	"fmt"
	"sort"
	"time"

	"github.com/reinkrul/meeting-planner/internal/calendar"
)

// Score returns all rule contributions for one party. Empty result = no penalty.
func Score(c Candidate, p Party) []RuleResult {
	var out []RuleResult
	if p.Rules.AvoidLunch.Enabled {
		if pen := scoreAvoidLunch(c, p); pen > 0 {
			out = append(out, RuleResult{Rule: "avoid_lunch", Penalty: pen})
		}
	}
	if p.Rules.AvoidBackToBack.Enabled {
		if pen := scoreAvoidBackToBack(c, p); pen > 0 {
			out = append(out, RuleResult{Rule: "avoid_back_to_back", Penalty: pen})
		}
	}
	if p.Rules.AvoidLongBusyStretches.Enabled {
		if pen := scoreAvoidLongStretch(c, p); pen > 0 {
			out = append(out, RuleResult{Rule: "avoid_long_busy_stretches", Penalty: pen})
		}
	}
	if p.Rules.PreferMornings.Enabled {
		if pen := scorePreferMornings(c, p); pen > 0 {
			out = append(out, RuleResult{Rule: "prefer_mornings", Penalty: pen})
		}
	}
	return out
}

// ScoreAll evaluates a candidate against every party. Returns the total
// penalty and the per-party breakdown for UI display.
func ScoreAll(c Candidate, parties []Party) ScoredCandidate {
	per := make(map[string][]RuleResult, len(parties))
	total := 0
	for _, p := range parties {
		res := Score(c, p)
		if len(res) > 0 {
			per[p.Label] = res
			for _, r := range res {
				total += r.Penalty
			}
		}
	}
	return ScoredCandidate{Candidate: c, Total: total, PerParty: per}
}

// scoreAvoidLunch returns a penalty proportional to the overlap between the
// candidate and the party's lunch window (in the party's local timezone).
// Penalty caps at the configured value when overlap == lunch window length.
func scoreAvoidLunch(c Candidate, p Party) int {
	tz := tzOf(p)
	r := p.Rules.AvoidLunch
	lunchStart, lunchEnd, ok := windowForDay(c.Start.In(tz), r.Start, r.End, tz)
	if !ok {
		return 0
	}
	overlap := overlapMinutes(c.Start, c.End, lunchStart, lunchEnd)
	if overlap <= 0 {
		return 0
	}
	lunchLen := int(lunchEnd.Sub(lunchStart).Minutes())
	if lunchLen <= 0 {
		return r.Penalty
	}
	pen := r.Penalty * overlap / lunchLen
	if pen > r.Penalty {
		pen = r.Penalty
	}
	if pen < 1 {
		pen = 1
	}
	return pen
}

// scoreAvoidBackToBack penalizes once per side (so a sandwiched slot gets 2x).
func scoreAvoidBackToBack(c Candidate, p Party) int {
	r := p.Rules.AvoidBackToBack
	gap := time.Duration(r.GapMinutes) * time.Minute
	pen := 0
	for _, b := range p.Busy {
		// Block ends just before candidate starts.
		if !b.End.After(c.Start) {
			if c.Start.Sub(b.End) < gap {
				pen += r.Penalty
			}
		}
		// Block starts just after candidate ends.
		if !b.Start.Before(c.End) {
			if b.Start.Sub(c.End) < gap {
				pen += r.Penalty
			}
		}
	}
	return pen
}

// scoreAvoidLongStretch simulates adding the candidate to the party's busy
// list, finds the continuous stretch containing the candidate (busy blocks
// separated by < AvoidBackToBack.GapMinutes are merged into one stretch),
// and penalizes proportional to how far over max_stretch_minutes it pushes.
func scoreAvoidLongStretch(c Candidate, p Party) int {
	r := p.Rules.AvoidLongBusyStretches
	mergeGap := time.Duration(p.Rules.AvoidBackToBack.GapMinutes) * time.Minute

	blocks := make([]calendar.BusyBlock, 0, len(p.Busy)+1)
	blocks = append(blocks, p.Busy...)
	blocks = append(blocks, calendar.BusyBlock{Start: c.Start, End: c.End})
	sort.Slice(blocks, func(i, j int) bool { return blocks[i].Start.Before(blocks[j].Start) })

	// Walk merged stretches and find the one containing the candidate.
	var stretchStart, stretchEnd time.Time
	cur := blocks[0]
	for _, b := range blocks[1:] {
		if b.Start.Sub(cur.End) <= mergeGap {
			if b.End.After(cur.End) {
				cur.End = b.End
			}
			continue
		}
		if !cur.End.Before(c.Start) && !cur.Start.After(c.End) {
			stretchStart, stretchEnd = cur.Start, cur.End
			break
		}
		cur = b
	}
	if stretchStart.IsZero() {
		stretchStart, stretchEnd = cur.Start, cur.End
	}

	stretchMin := int(stretchEnd.Sub(stretchStart).Minutes())
	over := stretchMin - r.MaxStretchMinutes
	if over <= 0 {
		return 0
	}
	chunks := over / 30
	if over%30 != 0 {
		chunks++
	}
	return chunks * r.PenaltyPer30MinOver
}

// scorePreferMornings penalizes candidates starting at or after noon (local).
func scorePreferMornings(c Candidate, p Party) int {
	tz := tzOf(p)
	local := c.Start.In(tz)
	if local.Hour() >= 12 {
		return p.Rules.PreferMornings.Penalty
	}
	return 0
}

// windowForDay builds [start, end) on the same date as `day` using HH:MM strings.
func windowForDay(day time.Time, startHHMM, endHHMM string, tz *time.Location) (time.Time, time.Time, bool) {
	sh, sm, ok := parseHHMM(startHHMM)
	if !ok {
		return time.Time{}, time.Time{}, false
	}
	eh, em, ok := parseHHMM(endHHMM)
	if !ok {
		return time.Time{}, time.Time{}, false
	}
	y, m, d := day.Date()
	return time.Date(y, m, d, sh, sm, 0, 0, tz),
		time.Date(y, m, d, eh, em, 0, 0, tz), true
}

func parseHHMM(s string) (int, int, bool) {
	if len(s) < 4 || len(s) > 5 {
		return 0, 0, false
	}
	var h, m int
	if _, err := fmt.Sscanf(s, "%d:%d", &h, &m); err != nil {
		return 0, 0, false
	}
	if h < 0 || h > 23 || m < 0 || m > 59 {
		return 0, 0, false
	}
	return h, m, true
}

func overlapMinutes(aStart, aEnd, bStart, bEnd time.Time) int {
	start := aStart
	if bStart.After(start) {
		start = bStart
	}
	end := aEnd
	if bEnd.Before(end) {
		end = bEnd
	}
	if !start.Before(end) {
		return 0
	}
	return int(end.Sub(start).Minutes())
}

func tzOf(p Party) *time.Location {
	if p.Timezone != nil {
		return p.Timezone
	}
	return time.UTC
}
