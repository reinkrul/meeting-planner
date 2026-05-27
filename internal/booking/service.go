// Package booking orchestrates the multi-party slot search and event creation.
package booking

import (
	"context"
	"errors"
	"fmt"
	"log"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/reinkrul/meeting-planner/internal/availability"
	"github.com/reinkrul/meeting-planner/internal/calendar"
	"github.com/reinkrul/meeting-planner/internal/config"
	"github.com/reinkrul/meeting-planner/internal/federation"
	"github.com/reinkrul/meeting-planner/internal/notify"
	"github.com/reinkrul/meeting-planner/internal/store"
)

// Service is the booking orchestrator. Constructed once at startup.
type Service struct {
	cfg       config.Config
	state     *store.State
	providers map[string]calendar.Provider
	fed       *federation.Client
	notifier  notify.Notifier
}

func NewService(cfg config.Config, st *store.State, providers map[string]calendar.Provider, notifier notify.Notifier) *Service {
	if notifier == nil {
		notifier = notify.Disabled{}
	}
	return &Service{
		cfg:       cfg,
		state:     st,
		providers: providers,
		fed:       federation.NewClient(),
		notifier:  notifier,
	}
}

// Participant is one person attending the meeting. PeerURL is optional; when
// present, that participant's availability and preferences are pulled from
// their meeting-planner instance.
type Participant struct {
	Name    string
	Email   string
	PeerURL string
}

// Request is the input from the booking guest.
type Request struct {
	Initiator   Participant   // the person opening the booking page
	Others      []Participant // additional participants
	Duration    time.Duration
	Title       string
	Description string
}

// ProposeResult is the ranked slot list plus any per-party warnings (e.g. a
// peer instance was unreachable). When Warnings is non-empty the operator
// should be shown them so they can retry or remove the affected participant.
type ProposeResult struct {
	Slots    []availability.ScoredCandidate
	Warnings []string
}

// Propose computes the best candidate slots for the meeting.
func (s *Service) Propose(ctx context.Context, req Request) (ProposeResult, error) {
	if req.Duration <= 0 {
		return ProposeResult{}, errors.New("duration must be positive")
	}

	tz, err := time.LoadLocation(s.cfg.Owner.Timezone)
	if err != nil {
		return ProposeResult{}, fmt.Errorf("owner timezone: %w", err)
	}
	now := time.Now()
	from := now.Add(time.Duration(s.cfg.Availability.MinNoticeHours) * time.Hour)
	to := now.AddDate(0, 0, s.cfg.Availability.MaxHorizonDays)

	ownerBusy, err := s.OwnerFreeBusy(ctx, from, to)
	if err != nil {
		return ProposeResult{}, fmt.Errorf("owner busy: %w", err)
	}

	parties := []availability.Party{ownerParty(s.cfg, tz, ownerBusy)}
	var warnings []string

	// Fetch each peer in parallel.
	type peerResult struct {
		party availability.Party
		err   error
		who   string
	}
	var wg sync.WaitGroup
	resCh := make(chan peerResult, len(req.Others))
	for _, p := range req.Others {
		if p.PeerURL == "" {
			continue
		}
		wg.Add(1)
		go func(p Participant) {
			defer wg.Done()
			resp, err := s.fed.Fetch(ctx, p.PeerURL, from, to)
			if err != nil {
				resCh <- peerResult{err: err, who: peerLabel(p)}
				return
			}
			party, err := resp.ToParty(peerLabel(p), from, to)
			if err != nil {
				resCh <- peerResult{err: err, who: peerLabel(p)}
				return
			}
			resCh <- peerResult{party: party, who: peerLabel(p)}
		}(p)
	}
	wg.Wait()
	close(resCh)
	for r := range resCh {
		if r.err != nil {
			warnings = append(warnings, fmt.Sprintf("%s: %v", r.who, r.err))
			continue
		}
		parties = append(parties, r.party)
	}

	free := availability.Intersect(parties, from, to)
	granularity := time.Duration(s.cfg.Availability.SlotGranularityMinutes) * time.Minute
	candidates := availability.Slice(free, req.Duration, granularity, tz)

	scored := make([]availability.ScoredCandidate, 0, len(candidates))
	for _, c := range candidates {
		scored = append(scored, availability.ScoreAll(c, parties))
	}
	// Chronological order; the handler groups by day and the template lets
	// the user pick a day. Penalty is shown alongside each slot.
	sort.SliceStable(scored, func(i, j int) bool {
		return scored[i].Candidate.Start.Before(scored[j].Candidate.Start)
	})
	return ProposeResult{Slots: scored, Warnings: warnings}, nil
}

// Confirm creates the calendar event on the configured invite-from calendar
// with all participants as attendees. Returns the provider's event ID.
func (s *Service) Confirm(ctx context.Context, req Request, slot availability.Candidate) (string, error) {
	prov, ok := s.providers[s.cfg.InviteFromCalendar]
	if !ok {
		return "", fmt.Errorf("invite_from_calendar %q has no provider", s.cfg.InviteFromCalendar)
	}
	attendees := make([]calendar.Attendee, 0, 1+len(req.Others))
	if req.Initiator.Email != "" {
		attendees = append(attendees, calendar.Attendee{Name: req.Initiator.Name, Email: req.Initiator.Email})
	}
	for _, p := range req.Others {
		if p.Email == "" {
			continue
		}
		attendees = append(attendees, calendar.Attendee{Name: p.Name, Email: p.Email})
	}
	title := req.Title
	if title == "" {
		title = "Meeting"
	}
	ev := calendar.Event{
		Title:       title,
		Description: req.Description,
		Start:       slot.Start,
		End:         slot.End,
		Attendees:   attendees,
	}
	id, err := prov.CreateEvent(ctx, ev)
	if err != nil {
		return "", err
	}
	// Fire-and-forget notification. Don't block the HTTP response on SMTP latency,
	// and don't fail the booking if mail send fails.
	go s.notifyBooking(req, slot, id)
	return id, nil
}

func (s *Service) notifyBooking(req Request, slot availability.Candidate, eventID string) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	tz, _ := time.LoadLocation(s.cfg.Owner.Timezone)
	if tz == nil {
		tz = time.UTC
	}
	subject := "New meeting booked"
	if t := strings.TrimSpace(req.Title); t != "" {
		subject = "New meeting booked: " + t
	}
	var attendees []string
	if req.Initiator.Email != "" {
		attendees = append(attendees, formatNotifyAttendee(req.Initiator))
	}
	for _, p := range req.Others {
		if p.Email == "" {
			continue
		}
		attendees = append(attendees, formatNotifyAttendee(p))
	}
	body := fmt.Sprintf(
		"A new meeting was booked through meeting-planner.\n\n"+
			"  When:        %s – %s\n"+
			"  Title:       %s\n"+
			"  Description: %s\n"+
			"  Attendees:   %s\n"+
			"  Event ID:    %s\n\n"+
			"It's already on your calendar; invites have been sent to attendees.\n",
		slot.Start.In(tz).Format("Mon 2 Jan 2006, 15:04 MST"),
		slot.End.In(tz).Format("15:04 MST"),
		stringOr(req.Title, "(no title)"),
		stringOr(req.Description, "(none)"),
		strings.Join(attendees, ", "),
		eventID,
	)
	if err := s.notifier.Notify(ctx, subject, body); err != nil {
		log.Printf("notify: %v", err)
	}
}

func formatNotifyAttendee(p Participant) string {
	if p.Name != "" {
		return p.Name + " <" + p.Email + ">"
	}
	return p.Email
}

func stringOr(s, fallback string) string {
	if strings.TrimSpace(s) == "" {
		return fallback
	}
	return s
}

// OwnerParty builds the owner availability.Party for the configured timezone,
// with busy blocks gathered from all configured calendars. Used by the
// federation /freebusy handler to compute free windows.
func (s *Service) OwnerParty(ctx context.Context, from, to time.Time) (availability.Party, error) {
	tz, err := time.LoadLocation(s.cfg.Owner.Timezone)
	if err != nil {
		return availability.Party{}, fmt.Errorf("owner timezone: %w", err)
	}
	busy, err := s.OwnerFreeBusy(ctx, from, to)
	if err != nil {
		return availability.Party{}, err
	}
	return ownerParty(s.cfg, tz, busy), nil
}

// OwnerFreeBusy fetches busy blocks from all configured calendars in
// parallel and returns their union (not merged). Used by both Propose
// (own party) and the federation /freebusy handler.
func (s *Service) OwnerFreeBusy(ctx context.Context, from, to time.Time) ([]calendar.BusyBlock, error) {
	type calRes struct {
		blocks []calendar.BusyBlock
		err    error
		id     string
	}
	var wg sync.WaitGroup
	resCh := make(chan calRes, len(s.providers))
	for id, p := range s.providers {
		wg.Add(1)
		go func(id string, p calendar.Provider) {
			defer wg.Done()
			b, err := p.FreeBusy(ctx, from, to)
			resCh <- calRes{blocks: b, err: err, id: id}
		}(id, p)
	}
	wg.Wait()
	close(resCh)
	var all []calendar.BusyBlock
	for r := range resCh {
		if r.err != nil {
			return nil, fmt.Errorf("calendar %s: %w", r.id, r.err)
		}
		all = append(all, r.blocks...)
	}
	return all, nil
}

// Config exposes the loaded config (read-only) so handlers can read it.
func (s *Service) Config() config.Config { return s.cfg }

func ownerParty(cfg config.Config, tz *time.Location, busy []calendar.BusyBlock) availability.Party {
	startMin, _ := hhmmToMinutes(cfg.Availability.WorkingHours.Start)
	endMin, _ := hhmmToMinutes(cfg.Availability.WorkingHours.End)
	return availability.Party{
		Label:        cfg.Owner.DisplayName,
		Timezone:     tz,
		WorkingDays:  availability.WorkingDaysFromConfig(cfg.Availability.WorkingDays),
		WorkingHours: availability.TimeOfDayWindow{StartMinutes: startMin, EndMinutes: endMin},
		Busy:         busy,
		Buffer:       time.Duration(cfg.Availability.BufferMinutes) * time.Minute,
		Rules:        cfg.Availability.Rules,
	}
}

func hhmmToMinutes(s string) (int, bool) {
	var h, m int
	_, err := fmt.Sscanf(s, "%d:%d", &h, &m)
	if err != nil {
		return 0, false
	}
	return h*60 + m, true
}

func peerLabel(p Participant) string {
	if p.Email != "" {
		return p.Email
	}
	if p.Name != "" {
		return p.Name
	}
	return p.PeerURL
}
