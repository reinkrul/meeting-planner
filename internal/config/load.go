package config

import (
	"errors"
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// Options controls where Load reads the config source from.
type Options struct {
	// ConfigFile, if non-empty, takes highest precedence as the YAML source.
	// If empty, MP_CONFIG_FILE then MP_CONFIG env vars are consulted.
	ConfigFile string
}

// Load resolves config in this order:
//  1. Built-in defaults.
//  2. YAML source (flag > MP_CONFIG_FILE > MP_CONFIG).
//  3. Calendars from MP_CALENDARS_<N>_* env vars (only if YAML had no calendars).
//  4. Per-field env-var overrides for scalar fields (MP_<UPPER_YAML_PATH>).
//  5. Validation.
func Load(opts Options) (Config, error) {
	cfg := Defaults()

	yamlSrc, srcDesc, err := pickYAMLSource(opts.ConfigFile)
	if err != nil {
		return Config{}, err
	}
	if yamlSrc != nil {
		if err := yaml.Unmarshal(yamlSrc, &cfg); err != nil {
			return Config{}, fmt.Errorf("parse %s: %w", srcDesc, err)
		}
	}

	// Calendars from indexed env vars — only if YAML did not provide any.
	envCals, err := loadCalendarsFromEnv()
	if err != nil {
		return Config{}, fmt.Errorf("calendars from env: %w", err)
	}
	if len(envCals) > 0 {
		if len(cfg.Calendars) > 0 {
			return Config{}, errors.New("calendars configured in both YAML and MP_CALENDARS_* env vars; pick one source")
		}
		cfg.Calendars = envCals
	}

	// Per-field scalar overrides.
	if err := applyEnvOverrides(&cfg); err != nil {
		return Config{}, err
	}

	if err := Validate(&cfg); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func pickYAMLSource(flagFile string) ([]byte, string, error) {
	if flagFile != "" {
		data, err := os.ReadFile(flagFile)
		if err != nil {
			return nil, "", fmt.Errorf("read config file %s: %w", flagFile, err)
		}
		return data, "config file " + flagFile, nil
	}
	if path := os.Getenv("MP_CONFIG_FILE"); path != "" {
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, "", fmt.Errorf("read MP_CONFIG_FILE %s: %w", path, err)
		}
		return data, "MP_CONFIG_FILE " + path, nil
	}
	if inline := os.Getenv("MP_CONFIG"); inline != "" {
		return []byte(inline), "MP_CONFIG env var", nil
	}
	return nil, "", nil
}
