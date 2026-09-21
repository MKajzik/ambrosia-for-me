// Package config loads process configuration from environment variables.
package config

import (
	"errors"
	"fmt"
	"net/url"
)

// Config is the API's runtime configuration.
type Config struct {
	// Addr is the listen address, for example ":8080".
	Addr string
	// DatabaseURL is the Postgres connection string.
	DatabaseURL string
	// WebOrigin is the single browser origin allowed by CORS.
	WebOrigin string
}

// Load reads configuration through getenv (normally os.Getenv). It returns an
// error naming every missing required variable and every invalid value
// (currently WEB_ORIGIN, which must be a bare origin).
func Load(getenv func(string) string) (Config, error) {
	cfg := Config{
		Addr:        withDefault(getenv("API_ADDR"), ":8080"),
		DatabaseURL: getenv("DATABASE_URL"),
		WebOrigin:   withDefault(getenv("WEB_ORIGIN"), "http://localhost:3000"),
	}
	var errs []error
	if cfg.DatabaseURL == "" {
		errs = append(errs, errors.New("DATABASE_URL is required"))
	}
	if err := validateOrigin(cfg.WebOrigin); err != nil {
		errs = append(errs, err)
	}
	if err := errors.Join(errs...); err != nil {
		return Config{}, fmt.Errorf("invalid configuration: %w", err)
	}
	return cfg, nil
}

func withDefault(v, def string) string {
	if v == "" {
		return def
	}
	return v
}

// validateOrigin reports whether v is a bare origin (scheme and host, with an
// optional port) as browsers send in the Origin header, so it can be matched
// exactly by CORS. "*", trailing slashes, paths, queries, fragments and
// credentials are rejected.
func validateOrigin(v string) error {
	bad := fmt.Errorf("WEB_ORIGIN must be an origin such as https://app.example (scheme and host only, no path or trailing slash), got %q", v)
	u, err := url.Parse(v)
	if err != nil || v == "*" {
		return bad
	}
	if (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.Path != "" ||
		u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || u.User != nil {
		return bad
	}
	return nil
}
