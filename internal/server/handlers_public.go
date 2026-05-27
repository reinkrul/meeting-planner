package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/reinkrul/meeting-planner/internal/availability"
	"github.com/reinkrul/meeting-planner/internal/booking"
	"github.com/reinkrul/meeting-planner/internal/federation"
)

func (s *Server) handleBookingForm(w http.ResponseWriter, r *http.Request) {
	token := chi.URLParam(r, "token")
	base := "/c/" + token
	name, email := readUserCookies(r)
	_ = s.renderer.Render(w, "booking_form", map[string]any{
		"Title":             "Book a meeting",
		"OwnerName":         s.cfg.Owner.DisplayName,
		"SlotsURL":          base + "/slots",
		"ParticipantRowURL": base + "/participants/row",
		"InitiatorName":     name,
		"InitiatorEmail":    email,
	})
}

func (s *Server) handleParticipantRow(w http.ResponseWriter, _ *http.Request) {
	_ = s.renderer.RenderFragment(w, "participant_row", nil)
}

// handleSlots reads the booking form, queries Service.Propose, and returns
// an htmx fragment with the scored slot list (plus warnings).
func (s *Server) handleSlots(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	req, err := parseBookingForm(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	res, err := s.booking.Propose(r.Context(), req)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Remember the booker for next time so the form pre-fills on return visits.
	setUserCookies(w, req.Initiator.Name, req.Initiator.Email)

	tz, _ := time.LoadLocation(s.cfg.Owner.Timezone)
	token := chi.URLParam(r, "token")
	_ = s.renderer.RenderFragment(w, "slots", map[string]any{
		"ConfirmURL":      "/c/" + token + "/confirm",
		"InitiatorName":   req.Initiator.Name,
		"InitiatorEmail":  req.Initiator.Email,
		"Title":           req.Title, // forwarded back via hidden field
		"Description":     req.Description,
		"DurationMinutes": int(req.Duration.Minutes()),
		"Others":          req.Others,
		"Warnings":        res.Warnings,
		"Days":            groupSlotsByDay(res.Slots, req.Duration, tz),
	})
}

// handleConfirm creates the calendar event for the chosen slot.
func (s *Server) handleConfirm(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	req, err := parseBookingForm(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	slotStartStr := r.FormValue("slot_start")
	slotStart, err := time.Parse(time.RFC3339, slotStartStr)
	if err != nil {
		http.Error(w, "invalid slot_start: "+err.Error(), http.StatusBadRequest)
		return
	}
	slot := availability.Candidate{
		Start: slotStart,
		End:   slotStart.Add(req.Duration),
	}

	id, err := s.booking.Confirm(r.Context(), req, slot)
	if err != nil {
		http.Error(w, "booking failed: "+err.Error(), http.StatusInternalServerError)
		return
	}

	tz, _ := time.LoadLocation(s.cfg.Owner.Timezone)
	attendees := []string{}
	if req.Initiator.Email != "" {
		attendees = append(attendees, formatAttendee(req.Initiator))
	}
	for _, p := range req.Others {
		if p.Email != "" {
			attendees = append(attendees, formatAttendee(p))
		}
	}

	meetingTitle := req.Title
	if meetingTitle == "" {
		meetingTitle = "Meeting"
	}
	_ = s.renderer.Render(w, "confirm", map[string]any{
		"Title":             meetingTitle, // page title and meeting title coincide
		"LocalStartDisplay": slot.Start.In(tz).Format("Mon 2 Jan 2006, 15:04 MST"),
		"LocalEndDisplay":   slot.End.In(tz).Format("15:04 MST"),
		"Attendees":         attendees,
		"EventID":           id,
	})
}

// handleFreeBusy serves the federation JSON.
func (s *Server) handleFreeBusy(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	from, err := time.Parse(time.RFC3339, q.Get("from"))
	if err != nil {
		http.Error(w, "from must be RFC3339", http.StatusBadRequest)
		return
	}
	to, err := time.Parse(time.RFC3339, q.Get("to"))
	if err != nil {
		http.Error(w, "to must be RFC3339", http.StatusBadRequest)
		return
	}
	party, err := s.booking.OwnerParty(r.Context(), from, to)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	free := availability.Intersect([]availability.Party{party}, from, to)
	resp := federation.Build(s.cfg.Availability, s.cfg.Owner.Timezone, free)
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

func parseBookingForm(r *http.Request) (booking.Request, error) {
	dur, err := strconv.Atoi(r.FormValue("duration_minutes"))
	if err != nil || dur <= 0 {
		return booking.Request{}, fmt.Errorf("duration_minutes must be a positive integer")
	}
	initiator := booking.Participant{
		Name:  strings.TrimSpace(r.FormValue("initiator_name")),
		Email: strings.TrimSpace(r.FormValue("initiator_email")),
	}
	if initiator.Email == "" {
		return booking.Request{}, fmt.Errorf("initiator_email is required")
	}

	names := r.Form["participant_name"]
	emails := r.Form["participant_email"]
	urls := r.Form["participant_peer_url"]
	n := len(names)
	if len(emails) > n {
		n = len(emails)
	}
	if len(urls) > n {
		n = len(urls)
	}
	var others []booking.Participant
	for i := 0; i < n; i++ {
		p := booking.Participant{}
		if i < len(names) {
			p.Name = strings.TrimSpace(names[i])
		}
		if i < len(emails) {
			p.Email = strings.TrimSpace(emails[i])
		}
		if i < len(urls) {
			p.PeerURL = strings.TrimSpace(urls[i])
		}
		if p.Email == "" && p.PeerURL == "" && p.Name == "" {
			continue // empty row
		}
		if p.Email == "" {
			return booking.Request{}, fmt.Errorf("participant %d: email is required", i+1)
		}
		others = append(others, p)
	}

	return booking.Request{
		Initiator:   initiator,
		Others:      others,
		Duration:    time.Duration(dur) * time.Minute,
		Title:       strings.TrimSpace(r.FormValue("title")),
		Description: strings.TrimSpace(r.FormValue("description")),
	}, nil
}

func formatAttendee(p booking.Participant) string {
	if p.Name != "" {
		return p.Name + " <" + p.Email + ">"
	}
	return p.Email
}

// slotView wraps a ScoredCandidate with display-friendly strings for the template.
type slotView struct {
	Candidate         availability.Candidate
	Total             int
	LocalStartDisplay string // just the time part within a day (e.g. "09:15")
	LocalEndDisplay   string
	DurationDisplay   string
	PerPartyBreakdown string
}

// dayGroup is one day's worth of candidate slots for the booking template.
type dayGroup struct {
	Label string     // e.g. "Wed 28 May 2026"
	Date  string     // e.g. "2026-05-28"  (for stable ids)
	Slots []slotView // chronological within the day
}

// groupSlotsByDay buckets chronologically-sorted slots by the local-date in
// the owner's display timezone. Days are emitted in chronological order;
// slots within a day stay chronological.
func groupSlotsByDay(slots []availability.ScoredCandidate, dur time.Duration, tz *time.Location) []dayGroup {
	var days []dayGroup
	byDate := map[string]int{} // date -> index into days
	for _, sc := range slots {
		localStart := sc.Candidate.Start.In(tz)
		localEnd := sc.Candidate.End.In(tz)
		date := localStart.Format("2006-01-02")
		idx, ok := byDate[date]
		if !ok {
			days = append(days, dayGroup{
				Label: localStart.Format("Mon 2 Jan 2006"),
				Date:  date,
			})
			idx = len(days) - 1
			byDate[date] = idx
		}
		days[idx].Slots = append(days[idx].Slots, slotView{
			Candidate:         sc.Candidate,
			Total:             sc.Total,
			LocalStartDisplay: localStart.Format("15:04"),
			LocalEndDisplay:   localEnd.Format("15:04"),
			DurationDisplay:   dur.String(),
			PerPartyBreakdown: formatBreakdown(sc.PerParty),
		})
	}
	return days
}

func formatBreakdown(per map[string][]availability.RuleResult) string {
	if len(per) == 0 {
		return ""
	}
	var parts []string
	for label, results := range per {
		var rules []string
		for _, r := range results {
			rules = append(rules, fmt.Sprintf("%s+%d", r.Rule, r.Penalty))
		}
		parts = append(parts, label+": "+strings.Join(rules, ","))
	}
	return strings.Join(parts, " | ")
}
