package config

// Config is the full application configuration. See configs/config.example.yaml
// and the README for documentation of fields and env-var overrides.
type Config struct {
	Server             ServerConfig        `yaml:"server"`
	DataDir            string              `yaml:"data_dir"`
	Owner              OwnerConfig         `yaml:"owner"`
	Calendars          []CalendarConfig    `yaml:"calendars"`
	InviteFromCalendar string              `yaml:"invite_from_calendar"`
	Availability       AvailabilityConfig  `yaml:"availability"`
	Notifications      NotificationsConfig `yaml:"notifications"`
}

// NotificationsConfig configures fire-and-forget notifications sent to the
// owner after a successful booking. SMTP is the only transport for MVP.
type NotificationsConfig struct {
	SMTP SMTPConfig `yaml:"smtp"`
}

type SMTPConfig struct {
	Enabled     bool     `yaml:"enabled"`
	Host        string   `yaml:"host"`
	Port        int      `yaml:"port"`
	Username    string   `yaml:"username"`
	PasswordEnv string   `yaml:"password_env"`
	From        string   `yaml:"from"`
	To          []string `yaml:"to"`
	// StartTLS upgrades a plain connection to TLS (RFC 3207). Standard for
	// submission on port 587. Set false only for testing or local relays.
	StartTLS bool `yaml:"start_tls"`
}

type ServerConfig struct {
	Listen        string `yaml:"listen"`
	PublicBaseURL string `yaml:"public_base_url"`
	// CapabilityToken optionally pins the capability token (the booking link).
	// When set it overrides whatever is on disk and survives restarts —
	// required on hosts with an ephemeral filesystem (e.g. DigitalOcean App
	// Platform) where state.json doesn't persist. It is a secret; prefer
	// injecting it via the MP_SERVER_CAPABILITY_TOKEN env var rather than
	// committing it to YAML. Generate with `openssl rand -hex 32`.
	CapabilityToken string `yaml:"capability_token"`
}

type OwnerConfig struct {
	DisplayName string `yaml:"display_name"`
	Timezone    string `yaml:"timezone"`
}

type CalendarConfig struct {
	ID       string                 `yaml:"id"`
	Provider string                 `yaml:"provider"` // "google" | "ics_file" | "caldav" | "ox" | "ics_url"
	Google   *GoogleCalendarConfig  `yaml:"google,omitempty"`
	ICSFile  *ICSFileCalendarConfig `yaml:"ics_file,omitempty"`
	CalDAV   *CalDAVCalendarConfig  `yaml:"caldav,omitempty"`
	OX       *OXCalendarConfig      `yaml:"ox,omitempty"`
	ICSURL   *ICSURLCalendarConfig  `yaml:"ics_url,omitempty"`
}

// RequiresOAuth reports whether this calendar provider needs an interactive
// OAuth flow before it can be used. Determines whether /admin auto-enables.
func (c CalendarConfig) RequiresOAuth() bool {
	return c.Provider == "google"
}

// Writable reports whether this provider supports CreateEvent — i.e. whether
// it can serve as `invite_from_calendar`. Read-only providers like `ics_url`
// only contribute busy time.
func (c CalendarConfig) Writable() bool {
	return c.Provider != "ics_url"
}

// GoogleCalendarConfig references env-var names rather than holding secrets directly.
type GoogleCalendarConfig struct {
	ClientIDEnv     string `yaml:"client_id_env"`
	ClientSecretEnv string `yaml:"client_secret_env"`
	CalendarID      string `yaml:"calendar_id"`
}

type ICSFileCalendarConfig struct {
	Path           string `yaml:"path"`
	OrganizerName  string `yaml:"organizer_name"`
	OrganizerEmail string `yaml:"organizer_email"`
}

// CalDAVCalendarConfig configures a CalDAV-backed calendar (e.g. Apple,
// Fastmail, Nextcloud, Proton via Bridge). Google and Hostnet do NOT support
// CalDAV for personal accounts — use the google or ox provider instead.
type CalDAVCalendarConfig struct {
	ServerURL    string `yaml:"server_url"`    // full URL of the calendar collection
	Username     string `yaml:"username"`      // usually the account email
	PasswordEnv  string `yaml:"password_env"`  // env var holding the password / app password
	OrganizerCN  string `yaml:"organizer_cn"`  // optional display name for ORGANIZER (defaults to Username)
}

// OXCalendarConfig configures an Open-Xchange App Suite calendar via the
// chronos HTTP API. Used for hosters that ship OX (Hostnet, mailbox.org,
// many EU email/calendar providers). Session-cookie auth.
type OXCalendarConfig struct {
	ServerURL    string `yaml:"server_url"`    // base URL, e.g. "https://appsuite.hostnet.nl"
	Username     string `yaml:"username"`      // login name (usually email)
	PasswordEnv  string `yaml:"password_env"`  // env var holding the password
	FolderID     string `yaml:"folder_id"`     // calendar folder id (e.g. "31"); if empty, default calendar is discovered
}

// ICSURLCalendarConfig configures a read-only calendar source that fetches an
// ICS feed over HTTP (e.g. Google Calendar's "Secret address in iCal format",
// any published iCal feed). Contributes busy time only — cannot be used as
// invite_from_calendar.
type ICSURLCalendarConfig struct {
	URL          string `yaml:"url"`            // full ICS feed URL
	CacheMinutes int    `yaml:"cache_minutes"`  // how long to cache the fetched feed; 0 = default (10)
}

type AvailabilityConfig struct {
	WorkingDays            []string   `yaml:"working_days"`
	WorkingHours           TimeWindow `yaml:"working_hours"`
	SlotGranularityMinutes int        `yaml:"slot_granularity_minutes"`
	MinNoticeHours         int        `yaml:"min_notice_hours"`
	MaxHorizonDays         int        `yaml:"max_horizon_days"`
	BufferMinutes          int        `yaml:"buffer_minutes"`
	Rules                  RuleConfig `yaml:"rules"`
}

type TimeWindow struct {
	Start string `yaml:"start"` // "HH:MM"
	End   string `yaml:"end"`
}

type RuleConfig struct {
	AvoidLunch             AvoidLunchRule       `yaml:"avoid_lunch"`
	AvoidBackToBack        AvoidBackToBackRule  `yaml:"avoid_back_to_back"`
	AvoidLongBusyStretches AvoidLongStretchRule `yaml:"avoid_long_busy_stretches"`
	PreferMornings         PreferMorningsRule   `yaml:"prefer_mornings"`
}

type AvoidLunchRule struct {
	Enabled bool   `yaml:"enabled"`
	Start   string `yaml:"start"`
	End     string `yaml:"end"`
	Penalty int    `yaml:"penalty"`
}

type AvoidBackToBackRule struct {
	Enabled    bool `yaml:"enabled"`
	GapMinutes int  `yaml:"gap_minutes"`
	Penalty    int  `yaml:"penalty"`
}

type AvoidLongStretchRule struct {
	Enabled             bool `yaml:"enabled"`
	MaxStretchMinutes   int  `yaml:"max_stretch_minutes"`
	PenaltyPer30MinOver int  `yaml:"penalty_per_30min_over"`
}

type PreferMorningsRule struct {
	Enabled bool `yaml:"enabled"`
	Penalty int  `yaml:"penalty"`
}

// Defaults returns a Config populated with sensible defaults. List-shaped
// fields (calendars) are left empty; the caller must supply them via YAML
// or indexed env vars.
func Defaults() Config {
	return Config{
		Server: ServerConfig{
			Listen: ":8080",
		},
		Notifications: NotificationsConfig{
			SMTP: SMTPConfig{Port: 587, StartTLS: true},
		},
		Availability: AvailabilityConfig{
			WorkingDays:            []string{"mon", "tue", "wed", "thu", "fri"},
			WorkingHours:           TimeWindow{Start: "09:00", End: "17:00"},
			SlotGranularityMinutes: 15,
			MinNoticeHours:         24,
			MaxHorizonDays:         30,
			BufferMinutes:          0,
			Rules: RuleConfig{
				AvoidLunch:             AvoidLunchRule{Enabled: true, Start: "12:00", End: "13:00", Penalty: 50},
				AvoidBackToBack:        AvoidBackToBackRule{Enabled: true, GapMinutes: 15, Penalty: 30},
				AvoidLongBusyStretches: AvoidLongStretchRule{Enabled: true, MaxStretchMinutes: 240, PenaltyPer30MinOver: 20},
				PreferMornings:         PreferMorningsRule{Enabled: false, Penalty: 10},
			},
		},
	}
}
