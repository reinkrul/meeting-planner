// Package server wires HTTP handlers, middleware, and templates.
package server

import (
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/reinkrul/meeting-planner/internal/booking"
	googleprov "github.com/reinkrul/meeting-planner/internal/calendar/google"
	"github.com/reinkrul/meeting-planner/internal/config"
	"github.com/reinkrul/meeting-planner/internal/store"
	"github.com/reinkrul/meeting-planner/internal/web"
)

// Server is the HTTP server.
type Server struct {
	cfg            config.Config
	state          *store.State
	booking        *booking.Service
	renderer       *web.Renderer
	googleByCal    map[string]*googleprov.Provider // only for OAuth flow access
	oauthStates    sync.Map                        // state token -> oauthStateRec
	basePath       string                          // derived from public_base_url's path; "" for root mount
}

type oauthStateRec struct {
	calendarID string
	expiry     time.Time
}

// New constructs a Server. googleProviders is the subset of providers that
// require OAuth (used by the admin connect flow).
func New(cfg config.Config, st *store.State, bs *booking.Service, googleProviders map[string]*googleprov.Provider) (*Server, error) {
	r, err := web.NewRenderer()
	if err != nil {
		return nil, err
	}
	return &Server{
		cfg:         cfg,
		state:       st,
		booking:     bs,
		renderer:    r,
		googleByCal: googleProviders,
		basePath:    basePathFromPublicURL(cfg.Server.PublicBaseURL),
	}, nil
}

// basePathFromPublicURL extracts the prefix the app should mount under.
// e.g. https://meet.example.com/meeting → "/meeting". Empty string means
// mount at root.
func basePathFromPublicURL(s string) string {
	u, err := url.Parse(s)
	if err != nil {
		return ""
	}
	p := strings.TrimRight(u.Path, "/")
	if p == "" {
		return ""
	}
	if !strings.HasPrefix(p, "/") {
		p = "/" + p
	}
	return p
}

// BasePath exposes the configured subpath prefix (read-only).
func (s *Server) BasePath() string { return s.basePath }

// Handler returns the configured HTTP handler. If a subpath base is
// configured (via public_base_url's path), routes are mounted under it.
func (s *Server) Handler() http.Handler {
	inner := chi.NewRouter()

	inner.Get("/", s.handleLanding)
	inner.Get("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("ok"))
	})

	inner.Route("/c/{token}", func(r chi.Router) {
		r.Use(s.requireCapabilityToken)
		r.Get("/", s.handleBookingForm)
		r.Get("/participants/row", s.handleParticipantRow)
		r.Post("/slots", s.handleSlots)
		r.Post("/confirm", s.handleConfirm)
		r.Get("/freebusy", s.handleFreeBusy)
	})

	inner.Route("/admin", func(r chi.Router) {
		r.Use(s.requireAdminEnabled)
		r.Get("/", s.handleAdminConnect)
		r.Get("/calendars/{id}/connect", s.handleAdminOAuthStart)
		r.Get("/calendars/{id}/callback", s.handleAdminOAuthCallback)
	})

	if s.basePath == "" {
		return inner
	}
	outer := chi.NewRouter()
	outer.Mount(s.basePath, inner)
	return outer
}

// requireCapabilityToken validates {token} against the current capability
// token (constant-time). A mismatch returns 404 to keep the URL opaque.
func (s *Server) requireCapabilityToken(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got := chi.URLParam(r, "token")
		want := s.state.CapabilityToken()
		if !constantTimeEqualString(got, want) {
			http.NotFound(w, r)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// requireAdminEnabled returns 404 when no OAuth-requiring calendar is pending.
// "Indistinguishable from no endpoint" is intentional.
func (s *Server) requireAdminEnabled(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if len(s.state.NeedsOAuth(s.cfg.Calendars)) == 0 {
			http.NotFound(w, r)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) handleLanding(w http.ResponseWriter, _ *http.Request) {
	_ = s.renderer.Render(w, "landing", map[string]any{
		"Title": "meeting-planner",
	})
}

// newOAuthState mints a state token bound to a calendar id, with a 10-minute TTL.
func (s *Server) newOAuthState(calendarID string) (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	tok := hex.EncodeToString(b[:])
	s.oauthStates.Store(tok, oauthStateRec{
		calendarID: calendarID,
		expiry:     time.Now().Add(10 * time.Minute),
	})
	return tok, nil
}

// consumeOAuthState verifies & removes a state token, returning the bound calendar id.
func (s *Server) consumeOAuthState(tok string) (string, bool) {
	v, ok := s.oauthStates.LoadAndDelete(tok)
	if !ok {
		return "", false
	}
	rec := v.(oauthStateRec)
	if time.Now().After(rec.expiry) {
		return "", false
	}
	return rec.calendarID, true
}

// constantTimeEqualString compares two strings in constant time relative to len(want).
func constantTimeEqualString(got, want string) bool {
	if len(got) != len(want) {
		return false
	}
	var diff byte
	for i := 0; i < len(got); i++ {
		diff |= got[i] ^ want[i]
	}
	return diff == 0
}

// capabilityURL returns the full public capability URL.
func (s *Server) capabilityURL() string {
	return strings.TrimRight(s.cfg.Server.PublicBaseURL, "/") + "/c/" + s.state.CapabilityToken()
}
