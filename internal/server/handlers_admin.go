package server

import (
	"net/http"

	"github.com/go-chi/chi/v5"
)

type pendingCalendar struct {
	ID         string
	Provider   string
	ConnectURL string
}

func (s *Server) handleAdminConnect(w http.ResponseWriter, _ *http.Request) {
	pendingIDs := s.state.NeedsOAuth(s.cfg.Calendars)
	pendingSet := map[string]bool{}
	for _, id := range pendingIDs {
		pendingSet[id] = true
	}
	var pending []pendingCalendar
	for _, c := range s.cfg.Calendars {
		if !pendingSet[c.ID] {
			continue
		}
		pending = append(pending, pendingCalendar{
			ID:         c.ID,
			Provider:   c.Provider,
			ConnectURL: "/admin/calendars/" + c.ID + "/connect",
		})
	}
	_ = s.renderer.Render(w, "admin_connect", map[string]any{
		"Title":         "Admin — connect calendars",
		"Pending":       pending,
		"CapabilityURL": s.capabilityURL(),
	})
}

func (s *Server) handleAdminOAuthStart(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	prov, ok := s.googleByCal[id]
	if !ok {
		http.NotFound(w, r)
		return
	}
	state, err := s.newOAuthState(id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, prov.AuthCodeURL(state), http.StatusFound)
}

func (s *Server) handleAdminOAuthCallback(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	prov, ok := s.googleByCal[id]
	if !ok {
		http.NotFound(w, r)
		return
	}
	if errParam := r.URL.Query().Get("error"); errParam != "" {
		http.Error(w, "OAuth error: "+errParam, http.StatusBadRequest)
		return
	}
	state := r.URL.Query().Get("state")
	wantID, ok := s.consumeOAuthState(state)
	if !ok || wantID != id {
		http.Error(w, "invalid or expired state token", http.StatusBadRequest)
		return
	}
	code := r.URL.Query().Get("code")
	if code == "" {
		http.Error(w, "missing code", http.StatusBadRequest)
		return
	}
	if err := prov.Exchange(r.Context(), code); err != nil {
		http.Error(w, "exchange failed: "+err.Error(), http.StatusInternalServerError)
		return
	}
	// Send the operator back to the admin dashboard; if this was the last
	// calendar, /admin will auto-disable from now on.
	http.Redirect(w, r, "/admin/", http.StatusFound)
}
