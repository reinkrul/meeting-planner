// Package ox implements calendar.Provider on top of Open-Xchange App Suite's
// HTTP API (the "chronos" endpoint). Use this when CalDAV is disabled on the
// OX deployment but the standard web API is reachable — e.g. Hostnet.
package ox

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/reinkrul/meeting-planner/internal/calendar"
	"github.com/reinkrul/meeting-planner/internal/config"
)

type Provider struct {
	id   string
	cfg  config.OXCalendarConfig
	http *http.Client

	mu        sync.Mutex
	sessionID string
	folderID  string
}

func New(id string, cfg config.OXCalendarConfig) (*Provider, error) {
	if os.Getenv(cfg.PasswordEnv) == "" {
		return nil, fmt.Errorf("env var %s is not set", cfg.PasswordEnv)
	}
	jar, err := cookiejar.New(nil)
	if err != nil {
		return nil, err
	}
	return &Provider{
		id:       id,
		cfg:      cfg,
		http:     &http.Client{Jar: jar, Timeout: 30 * time.Second},
		folderID: cfg.FolderID,
	}, nil
}

func (p *Provider) ID() string { return p.id }

// FreeBusy queries OX chronos action=freeBusy for the configured user.
// Response includes full event details but we drop everything except
// {start, end} to preserve the privacy boundary defined by calendar.BusyBlock.
func (p *Provider) FreeBusy(ctx context.Context, from, to time.Time) ([]calendar.BusyBlock, error) {
	type fbAttendee struct {
		CuType string `json:"cuType"`
		URI    string `json:"uri"`
	}
	type fbReq struct {
		Attendees []fbAttendee `json:"attendees"`
	}
	type fbTime struct {
		StartTime int64  `json:"startTime"`
		EndTime   int64  `json:"endTime"`
		FBType    string `json:"fbType"`
	}
	type fbEntry struct {
		FreeBusyTime []fbTime `json:"freeBusyTime"`
	}
	type fbResp struct {
		Data  []fbEntry `json:"data"`
		Error string    `json:"error"`
	}

	body := fbReq{Attendees: []fbAttendee{{CuType: "INDIVIDUAL", URI: "mailto:" + p.cfg.Username}}}
	query := map[string]string{
		"from":  oxUTC(from),
		"until": oxUTC(to),
	}
	var resp fbResp
	if err := p.doJSON(ctx, http.MethodPut, "/appsuite/api/chronos?action=freeBusy", query, body, &resp); err != nil {
		return nil, err
	}
	if resp.Error != "" {
		return nil, fmt.Errorf("ox: %s", resp.Error)
	}

	var out []calendar.BusyBlock
	for _, e := range resp.Data {
		for _, t := range e.FreeBusyTime {
			if !strings.EqualFold(t.FBType, "BUSY") && !strings.EqualFold(t.FBType, "BUSY-UNAVAILABLE") {
				continue
			}
			out = append(out, calendar.BusyBlock{
				Start: time.UnixMilli(t.StartTime).UTC(),
				End:   time.UnixMilli(t.EndTime).UTC(),
			})
		}
	}
	return out, nil
}

// CreateEvent posts to chronos action=new. OX dispatches invite emails for
// events with attendees on the user's own calendar by default.
func (p *Provider) CreateEvent(ctx context.Context, ev calendar.Event) (string, error) {
	if err := p.ensureFolder(ctx); err != nil {
		return "", err
	}
	type oxAttendee struct {
		CuType   string `json:"cuType"`
		URI      string `json:"uri"`
		CN       string `json:"cn,omitempty"`
		PartStat string `json:"partStat,omitempty"`
	}
	type oxDateTime struct {
		Value string `json:"value"` // YYYYMMDDTHHMMSSZ for UTC
	}
	type oxEvent struct {
		Folder      string       `json:"folder"`
		Summary     string       `json:"summary"`
		Description string       `json:"description,omitempty"`
		Location    string       `json:"location,omitempty"`
		StartDate   oxDateTime   `json:"startDate"`
		EndDate     oxDateTime   `json:"endDate"`
		Attendees   []oxAttendee `json:"attendees,omitempty"`
	}
	// chronos action=new returns {"data":{"created":[{...,"id":"208",...}], ...}}
	type oxCreated struct {
		ID  string `json:"id"`
		UID string `json:"uid"`
	}
	type oxResp struct {
		Data struct {
			Created []oxCreated `json:"created"`
		} `json:"data"`
		Error string `json:"error"`
	}

	folderRef := "cal://0/" + p.folderID
	atts := make([]oxAttendee, 0, len(ev.Attendees))
	for _, a := range ev.Attendees {
		atts = append(atts, oxAttendee{
			CuType:   "INDIVIDUAL",
			URI:      "mailto:" + a.Email,
			CN:       a.Name,
			PartStat: "NEEDS-ACTION",
		})
	}
	body := oxEvent{
		Folder:      folderRef,
		Summary:     ev.Title,
		Description: ev.Description,
		Location:    ev.Location,
		StartDate:   oxDateTime{Value: oxUTC(ev.Start)},
		EndDate:     oxDateTime{Value: oxUTC(ev.End)},
		Attendees:   atts,
	}
	var resp oxResp
	query := map[string]string{"folder": folderRef}
	if err := p.doJSON(ctx, http.MethodPut, "/appsuite/api/chronos?action=new", query, body, &resp); err != nil {
		return "", err
	}
	if resp.Error != "" {
		return "", fmt.Errorf("ox: %s", resp.Error)
	}
	if len(resp.Data.Created) == 0 {
		return "", fmt.Errorf("ox: create returned no created entries")
	}
	return resp.Data.Created[0].ID, nil
}

func (p *Provider) ensureFolder(ctx context.Context) error {
	p.mu.Lock()
	if p.folderID != "" {
		p.mu.Unlock()
		return nil
	}
	p.mu.Unlock()

	type folderResp struct {
		Data  [][]any `json:"data"` // [["31","Agenda"], ...]
		Error string  `json:"error"`
	}
	var resp folderResp
	query := map[string]string{
		"content_type": "calendar",
		"columns":      "1,300",
	}
	if err := p.doJSON(ctx, http.MethodGet, "/appsuite/api/folders?action=allVisible", query, nil, &resp); err != nil {
		return fmt.Errorf("discover folders: %w", err)
	}
	if resp.Error != "" {
		return fmt.Errorf("ox: %s", resp.Error)
	}
	if len(resp.Data) == 0 {
		return fmt.Errorf("ox: no calendar folders visible")
	}
	first, ok := resp.Data[0][0].(string)
	if !ok {
		return fmt.Errorf("ox: unexpected folder id type %T", resp.Data[0][0])
	}
	p.mu.Lock()
	p.folderID = first
	p.mu.Unlock()
	return nil
}

// doJSON issues an authenticated request. OX signals an expired session in
// two ways: an HTTP 401, or (more commonly) HTTP 200 with an error body whose
// code is in the SES- category. Either way we drop the session, re-login, and
// retry once.
func (p *Provider) doJSON(ctx context.Context, method, path string, query map[string]string, body, out any) error {
	if err := p.ensureSession(ctx); err != nil {
		return err
	}

	status, raw, err := p.rawDoBytes(ctx, method, path, query, body)
	if err != nil {
		return err
	}
	if status == http.StatusUnauthorized || isSessionExpired(raw) {
		p.clearSession()
		if err := p.ensureSession(ctx); err != nil {
			return err
		}
		status, raw, err = p.rawDoBytes(ctx, method, path, query, body)
		if err != nil {
			return err
		}
	}
	if status/100 != 2 {
		snippet := strings.TrimSpace(string(raw))
		if len(snippet) > 300 {
			snippet = snippet[:300] + "…"
		}
		return fmt.Errorf("ox: HTTP %d: %s", status, snippet)
	}
	if out != nil && len(raw) > 0 {
		if err := json.Unmarshal(raw, out); err != nil {
			return fmt.Errorf("ox: decode response: %w (body: %.200s)", err, raw)
		}
	}
	return nil
}

// rawDoBytes performs the request and returns status + body.
func (p *Provider) rawDoBytes(ctx context.Context, method, path string, query map[string]string, body any) (int, []byte, error) {
	resp, err := p.rawDo(ctx, method, path, query, body)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return 0, nil, err
	}
	return resp.StatusCode, raw, nil
}

func (p *Provider) clearSession() {
	p.mu.Lock()
	p.sessionID = ""
	p.mu.Unlock()
}

// isSessionExpired detects OX's session-category error in a 200 body.
func isSessionExpired(raw []byte) bool {
	var e struct {
		Error string `json:"error"`
		Code  string `json:"code"`
	}
	if json.Unmarshal(raw, &e) != nil {
		return false
	}
	// OX session errors carry codes like "SES-0203". The "Please login again"
	// text is the user-facing message for that category.
	return strings.HasPrefix(e.Code, "SES-")
}

func (p *Provider) rawDo(ctx context.Context, method, path string, query map[string]string, body any) (*http.Response, error) {
	u, err := url.Parse(strings.TrimRight(p.cfg.ServerURL, "/") + path)
	if err != nil {
		return nil, err
	}
	q := u.Query()
	for k, v := range query {
		q.Set(k, v)
	}
	p.mu.Lock()
	if p.sessionID != "" {
		q.Set("session", p.sessionID)
	}
	p.mu.Unlock()
	u.RawQuery = q.Encode()

	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		reader = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, u.String(), reader)
	if err != nil {
		return nil, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Accept", "application/json")
	return p.http.Do(req)
}

func (p *Provider) ensureSession(ctx context.Context) error {
	p.mu.Lock()
	if p.sessionID != "" {
		p.mu.Unlock()
		return nil
	}
	p.mu.Unlock()
	return p.login(ctx)
}

func (p *Provider) login(ctx context.Context) error {
	form := url.Values{
		"name":     {p.cfg.Username},
		"password": {os.Getenv(p.cfg.PasswordEnv)},
	}
	loginURL := strings.TrimRight(p.cfg.ServerURL, "/") + "/appsuite/api/login?action=login"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, loginURL, strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := p.http.Do(req)
	if err != nil {
		return fmt.Errorf("ox login: %w", err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("ox login: HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	var body struct {
		Session  string `json:"session"`
		Error    string `json:"error"`
		ErrorID  string `json:"error_id"`
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		return fmt.Errorf("ox login: decode: %w", err)
	}
	if body.Error != "" {
		return fmt.Errorf("ox login: %s (%s)", body.Error, body.ErrorID)
	}
	if body.Session == "" {
		return fmt.Errorf("ox login: no session in response")
	}
	p.mu.Lock()
	p.sessionID = body.Session
	p.mu.Unlock()
	return nil
}

// oxUTC formats t as YYYYMMDDTHHMMSSZ in UTC — the chronos API's preferred form.
func oxUTC(t time.Time) string {
	return t.UTC().Format("20060102T150405Z")
}
