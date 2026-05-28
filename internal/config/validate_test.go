package config

import "testing"

// validConfig returns a minimal config that passes Validate, so individual
// tests can mutate one field and assert on the result.
func validConfig() Config {
	c := Defaults()
	c.Server.PublicBaseURL = "https://meet.example.com"
	c.DataDir = "/tmp/mp-test"
	c.Owner.DisplayName = "Test"
	c.Owner.Timezone = "Europe/Amsterdam"
	c.Calendars = []CalendarConfig{{
		ID:       "owner",
		Provider: "ics_file",
		ICSFile:  &ICSFileCalendarConfig{Path: "/tmp/owner.ics"},
	}}
	c.InviteFromCalendar = "owner"
	return c
}

func TestValidate_CapabilityTokenOptional(t *testing.T) {
	c := validConfig()
	c.Server.CapabilityToken = "" // unset is fine
	if err := Validate(&c); err != nil {
		t.Errorf("empty capability_token should be valid, got %v", err)
	}
}

func TestValidate_CapabilityTokenTooShort(t *testing.T) {
	c := validConfig()
	c.Server.CapabilityToken = "tooshort" // 8 chars
	if err := Validate(&c); err == nil {
		t.Error("expected error for short capability_token, got nil")
	}
}

func TestValidate_CapabilityTokenLongEnough(t *testing.T) {
	c := validConfig()
	c.Server.CapabilityToken = "0123456789abcdef0123456789abcdef" // 32
	if err := Validate(&c); err != nil {
		t.Errorf("32-char capability_token should be valid, got %v", err)
	}
}

func TestValidate_ReadOnlyInviteFromRejected(t *testing.T) {
	c := validConfig()
	c.Calendars = []CalendarConfig{{
		ID:       "feed",
		Provider: "ics_url",
		ICSURL:   &ICSURLCalendarConfig{URL: "https://example.com/cal.ics"},
	}}
	c.InviteFromCalendar = "feed"
	if err := Validate(&c); err == nil {
		t.Error("expected error: read-only ics_url cannot be invite_from_calendar")
	}
}
