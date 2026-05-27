package config

import (
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

var validDays = map[string]bool{
	"mon": true, "tue": true, "wed": true, "thu": true,
	"fri": true, "sat": true, "sun": true,
}

// Validate checks invariants after all config sources are merged.
// Errors are reported one at a time (fail fast on first problem) so the
// startup log makes the cause obvious.
func Validate(cfg *Config) error {
	if cfg.Server.Listen == "" {
		return fmt.Errorf("server.listen is required")
	}
	if cfg.Server.PublicBaseURL == "" {
		return fmt.Errorf("server.public_base_url is required")
	}
	if u, err := url.Parse(cfg.Server.PublicBaseURL); err != nil || u.Scheme == "" || u.Host == "" {
		return fmt.Errorf("server.public_base_url is not a valid URL: %q", cfg.Server.PublicBaseURL)
	}
	if cfg.DataDir == "" {
		return fmt.Errorf("data_dir is required")
	}
	if cfg.Owner.DisplayName == "" {
		return fmt.Errorf("owner.display_name is required")
	}
	if cfg.Owner.Timezone == "" {
		return fmt.Errorf("owner.timezone is required")
	}
	if _, err := time.LoadLocation(cfg.Owner.Timezone); err != nil {
		return fmt.Errorf("owner.timezone %q: %w", cfg.Owner.Timezone, err)
	}

	if len(cfg.Calendars) == 0 {
		return fmt.Errorf("at least one calendar must be configured")
	}
	seen := map[string]bool{}
	for i, c := range cfg.Calendars {
		if c.ID == "" {
			return fmt.Errorf("calendars[%d].id is required", i)
		}
		if seen[c.ID] {
			return fmt.Errorf("calendars[%d].id %q is duplicated", i, c.ID)
		}
		seen[c.ID] = true
		if err := validateCalendar(c); err != nil {
			return fmt.Errorf("calendars[%d] (%s): %w", i, c.ID, err)
		}
	}

	if cfg.InviteFromCalendar == "" {
		return fmt.Errorf("invite_from_calendar is required")
	}
	if !seen[cfg.InviteFromCalendar] {
		return fmt.Errorf("invite_from_calendar %q does not match any configured calendar", cfg.InviteFromCalendar)
	}

	if err := validateAvailability(cfg.Availability); err != nil {
		return err
	}
	return validateNotifications(cfg.Notifications)
}

func validateNotifications(n NotificationsConfig) error {
	s := n.SMTP
	if !s.Enabled {
		return nil
	}
	if s.Host == "" {
		return fmt.Errorf("notifications.smtp.host is required when enabled")
	}
	if s.Port <= 0 || s.Port > 65535 {
		return fmt.Errorf("notifications.smtp.port must be 1..65535")
	}
	if s.Username == "" {
		return fmt.Errorf("notifications.smtp.username is required when enabled")
	}
	if s.PasswordEnv == "" {
		return fmt.Errorf("notifications.smtp.password_env is required when enabled")
	}
	if os.Getenv(s.PasswordEnv) == "" {
		return fmt.Errorf("env var %s (referenced by notifications.smtp.password_env) is not set", s.PasswordEnv)
	}
	if s.From == "" {
		return fmt.Errorf("notifications.smtp.from is required when enabled")
	}
	if len(s.To) == 0 {
		return fmt.Errorf("notifications.smtp.to must have at least one recipient when enabled")
	}
	return nil
}

func validateCalendar(c CalendarConfig) error {
	switch c.Provider {
	case "google":
		if c.Google == nil {
			return fmt.Errorf("provider=google requires google: block")
		}
		if c.Google.ClientIDEnv == "" {
			return fmt.Errorf("google.client_id_env is required")
		}
		if os.Getenv(c.Google.ClientIDEnv) == "" {
			return fmt.Errorf("env var %s (referenced by google.client_id_env) is not set", c.Google.ClientIDEnv)
		}
		if c.Google.ClientSecretEnv == "" {
			return fmt.Errorf("google.client_secret_env is required")
		}
		if os.Getenv(c.Google.ClientSecretEnv) == "" {
			return fmt.Errorf("env var %s (referenced by google.client_secret_env) is not set", c.Google.ClientSecretEnv)
		}
		if c.Google.CalendarID == "" {
			return fmt.Errorf("google.calendar_id is required (use \"primary\" for the main calendar)")
		}
	case "ics_file":
		if c.ICSFile == nil {
			return fmt.Errorf("provider=ics_file requires ics_file: block")
		}
		if c.ICSFile.Path == "" {
			return fmt.Errorf("ics_file.path is required")
		}
	case "caldav":
		if c.CalDAV == nil {
			return fmt.Errorf("provider=caldav requires caldav: block")
		}
		if c.CalDAV.ServerURL == "" {
			return fmt.Errorf("caldav.server_url is required")
		}
		if c.CalDAV.Username == "" {
			return fmt.Errorf("caldav.username is required")
		}
		if c.CalDAV.PasswordEnv == "" {
			return fmt.Errorf("caldav.password_env is required")
		}
		if os.Getenv(c.CalDAV.PasswordEnv) == "" {
			return fmt.Errorf("env var %s (referenced by caldav.password_env) is not set", c.CalDAV.PasswordEnv)
		}
	case "ox":
		if c.OX == nil {
			return fmt.Errorf("provider=ox requires ox: block")
		}
		if c.OX.ServerURL == "" {
			return fmt.Errorf("ox.server_url is required")
		}
		if c.OX.Username == "" {
			return fmt.Errorf("ox.username is required")
		}
		if c.OX.PasswordEnv == "" {
			return fmt.Errorf("ox.password_env is required")
		}
		if os.Getenv(c.OX.PasswordEnv) == "" {
			return fmt.Errorf("env var %s (referenced by ox.password_env) is not set", c.OX.PasswordEnv)
		}
	case "":
		return fmt.Errorf("provider is required (google, ics_file, caldav, or ox)")
	default:
		return fmt.Errorf("unknown provider %q (supported: google, ics_file, caldav, ox)", c.Provider)
	}
	return nil
}

func validateAvailability(a AvailabilityConfig) error {
	if len(a.WorkingDays) == 0 {
		return fmt.Errorf("availability.working_days must not be empty")
	}
	for _, d := range a.WorkingDays {
		if !validDays[strings.ToLower(d)] {
			return fmt.Errorf("availability.working_days: %q is not a valid day (mon..sun)", d)
		}
	}
	if err := validateHHMMRange("availability.working_hours", a.WorkingHours); err != nil {
		return err
	}
	if a.SlotGranularityMinutes < 1 {
		return fmt.Errorf("availability.slot_granularity_minutes must be >= 1")
	}
	if a.MinNoticeHours < 0 {
		return fmt.Errorf("availability.min_notice_hours must be >= 0")
	}
	if a.MaxHorizonDays < 1 {
		return fmt.Errorf("availability.max_horizon_days must be >= 1")
	}
	if a.BufferMinutes < 0 {
		return fmt.Errorf("availability.buffer_minutes must be >= 0")
	}
	if a.Rules.AvoidLunch.Enabled {
		if err := validateHHMMRange("availability.rules.avoid_lunch", TimeWindow{Start: a.Rules.AvoidLunch.Start, End: a.Rules.AvoidLunch.End}); err != nil {
			return err
		}
	}
	if a.Rules.AvoidBackToBack.Enabled && a.Rules.AvoidBackToBack.GapMinutes < 0 {
		return fmt.Errorf("availability.rules.avoid_back_to_back.gap_minutes must be >= 0")
	}
	if a.Rules.AvoidLongBusyStretches.Enabled && a.Rules.AvoidLongBusyStretches.MaxStretchMinutes < 1 {
		return fmt.Errorf("availability.rules.avoid_long_busy_stretches.max_stretch_minutes must be >= 1")
	}
	return nil
}

func validateHHMMRange(label string, w TimeWindow) error {
	start, err := parseHHMM(w.Start)
	if err != nil {
		return fmt.Errorf("%s.start %q: %w", label, w.Start, err)
	}
	end, err := parseHHMM(w.End)
	if err != nil {
		return fmt.Errorf("%s.end %q: %w", label, w.End, err)
	}
	if !(start < end) {
		return fmt.Errorf("%s: start must be before end (got %s..%s)", label, w.Start, w.End)
	}
	return nil
}

// parseHHMM parses "HH:MM" and returns minutes since midnight.
func parseHHMM(s string) (int, error) {
	parts := strings.SplitN(s, ":", 2)
	if len(parts) != 2 {
		return 0, fmt.Errorf("must be HH:MM")
	}
	h, err := strconv.Atoi(parts[0])
	if err != nil || h < 0 || h > 23 {
		return 0, fmt.Errorf("hours must be 0..23")
	}
	m, err := strconv.Atoi(parts[1])
	if err != nil || m < 0 || m > 59 {
		return 0, fmt.Errorf("minutes must be 0..59")
	}
	return h*60 + m, nil
}
