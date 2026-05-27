package config

import (
	"fmt"
	"os"
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// applyEnvOverrides walks Config and sets scalar fields from MP_<UPPER_YAML_PATH>
// env vars. Slices of structs (e.g. Calendars) are intentionally skipped here —
// they are loaded by loadCalendarsFromEnv.
func applyEnvOverrides(cfg *Config) error {
	return walkAndOverride(reflect.ValueOf(cfg).Elem(), "MP")
}

func walkAndOverride(v reflect.Value, prefix string) error {
	t := v.Type()
	for i := 0; i < v.NumField(); i++ {
		field := t.Field(i)
		if !field.IsExported() {
			continue
		}
		tag := field.Tag.Get("yaml")
		if tag == "" || tag == "-" {
			continue
		}
		name := strings.SplitN(tag, ",", 2)[0]
		envKey := prefix + "_" + strings.ToUpper(name)
		fv := v.Field(i)

		switch fv.Kind() {
		case reflect.Struct:
			if err := walkAndOverride(fv, envKey); err != nil {
				return err
			}
		case reflect.Ptr:
			if !fv.IsNil() {
				if err := walkAndOverride(fv.Elem(), envKey); err != nil {
					return err
				}
			}
		case reflect.Slice:
			// Only []string is overridable via env (CSV). Slices of structs are
			// loaded by indexed env vars in a separate pass.
			if fv.Type().Elem().Kind() == reflect.String {
				if s, ok := os.LookupEnv(envKey); ok {
					fv.Set(reflect.ValueOf(splitCSV(s)))
				}
			}
		case reflect.String:
			if s, ok := os.LookupEnv(envKey); ok {
				fv.SetString(s)
			}
		case reflect.Bool:
			if s, ok := os.LookupEnv(envKey); ok {
				b, err := strconv.ParseBool(s)
				if err != nil {
					return fmt.Errorf("%s: parse bool: %w", envKey, err)
				}
				fv.SetBool(b)
			}
		case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
			if s, ok := os.LookupEnv(envKey); ok {
				n, err := strconv.ParseInt(s, 10, 64)
				if err != nil {
					return fmt.Errorf("%s: parse int: %w", envKey, err)
				}
				fv.SetInt(n)
			}
		}
	}
	return nil
}

func splitCSV(s string) []string {
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

var calendarEnvRe = regexp.MustCompile(`^MP_CALENDARS_(\d+)_(.+)$`)

// loadCalendarsFromEnv reads MP_CALENDARS_<N>_* env vars and returns the
// resulting calendar slice ordered by index. Returns nil if none are set.
func loadCalendarsFromEnv() ([]CalendarConfig, error) {
	grouped := map[int]map[string]string{}
	for _, e := range os.Environ() {
		eq := strings.IndexByte(e, '=')
		if eq < 0 {
			continue
		}
		k, v := e[:eq], e[eq+1:]
		m := calendarEnvRe.FindStringSubmatch(k)
		if m == nil {
			continue
		}
		idx, _ := strconv.Atoi(m[1])
		if grouped[idx] == nil {
			grouped[idx] = map[string]string{}
		}
		grouped[idx][m[2]] = v
	}
	if len(grouped) == 0 {
		return nil, nil
	}
	indices := make([]int, 0, len(grouped))
	for i := range grouped {
		indices = append(indices, i)
	}
	sort.Ints(indices)
	out := make([]CalendarConfig, 0, len(indices))
	for _, i := range indices {
		cal, err := buildCalendarFromEnvMap(grouped[i])
		if err != nil {
			return nil, fmt.Errorf("MP_CALENDARS_%d_*: %w", i, err)
		}
		out = append(out, cal)
	}
	return out, nil
}

func buildCalendarFromEnvMap(m map[string]string) (CalendarConfig, error) {
	c := CalendarConfig{
		ID:       m["ID"],
		Provider: m["PROVIDER"],
	}
	switch c.Provider {
	case "google":
		c.Google = &GoogleCalendarConfig{
			ClientIDEnv:     m["GOOGLE_CLIENT_ID_ENV"],
			ClientSecretEnv: m["GOOGLE_CLIENT_SECRET_ENV"],
			CalendarID:      m["GOOGLE_CALENDAR_ID"],
		}
	case "ics_file":
		c.ICSFile = &ICSFileCalendarConfig{
			Path:           m["ICS_FILE_PATH"],
			OrganizerName:  m["ICS_FILE_ORGANIZER_NAME"],
			OrganizerEmail: m["ICS_FILE_ORGANIZER_EMAIL"],
		}
	case "caldav":
		c.CalDAV = &CalDAVCalendarConfig{
			ServerURL:   m["CALDAV_SERVER_URL"],
			Username:    m["CALDAV_USERNAME"],
			PasswordEnv: m["CALDAV_PASSWORD_ENV"],
			OrganizerCN: m["CALDAV_ORGANIZER_CN"],
		}
	case "ox":
		c.OX = &OXCalendarConfig{
			ServerURL:   m["OX_SERVER_URL"],
			Username:    m["OX_USERNAME"],
			PasswordEnv: m["OX_PASSWORD_ENV"],
			FolderID:    m["OX_FOLDER_ID"],
		}
	case "ics_url":
		cm := 0
		if s := m["ICS_URL_CACHE_MINUTES"]; s != "" {
			if n, err := strconv.Atoi(s); err == nil {
				cm = n
			}
		}
		c.ICSURL = &ICSURLCalendarConfig{
			URL:          m["ICS_URL_URL"],
			CacheMinutes: cm,
		}
	case "":
		return c, fmt.Errorf("PROVIDER not set")
	default:
		return c, fmt.Errorf("unknown PROVIDER %q", c.Provider)
	}
	return c, nil
}
