package federation

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Client fetches free-busy from a peer instance over HTTP.
type Client struct {
	HTTP *http.Client
}

func NewClient() *Client {
	return &Client{HTTP: &http.Client{Timeout: 10 * time.Second}}
}

// Fetch GETs <peerCapabilityURL>/freebusy?from=...&to=... and decodes the response.
// peerCapabilityURL is the booking URL (e.g. https://meet.example.com/c/<token>);
// the freebusy endpoint is one path segment beyond it.
func (c *Client) Fetch(ctx context.Context, peerCapabilityURL string, from, to time.Time) (FreeBusyResponse, error) {
	base, err := url.Parse(strings.TrimRight(peerCapabilityURL, "/"))
	if err != nil {
		return FreeBusyResponse{}, fmt.Errorf("peer URL: %w", err)
	}
	base.Path = strings.TrimRight(base.Path, "/") + "/freebusy"
	q := base.Query()
	q.Set("from", from.UTC().Format(time.RFC3339))
	q.Set("to", to.UTC().Format(time.RFC3339))
	base.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base.String(), nil)
	if err != nil {
		return FreeBusyResponse{}, err
	}
	req.Header.Set("Accept", "application/json")

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return FreeBusyResponse{}, fmt.Errorf("peer fetch: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return FreeBusyResponse{}, fmt.Errorf("peer returned %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var out FreeBusyResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return FreeBusyResponse{}, fmt.Errorf("decode peer response: %w", err)
	}
	return out, nil
}
