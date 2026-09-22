// Package config loads process configuration from environment variables.
package config

import (
	"errors"
	"fmt"
	"net/url"
	"strconv"

	"github.com/InzKazik/mealplanner/backend/internal/auth"
)

// maxTrustedProxies bounds TRUSTED_PROXY_COUNT: more hops than this is a typo.
const maxTrustedProxies = 10

// DevJWTSecret is the public secret `make run-api` uses; Load refuses it unless
// ALLOW_DEV_JWT_SECRET=1.
const DevJWTSecret = "dev-only-secret-change-me-0123456789" //nolint:gosec // public development value, refused unless ALLOW_DEV_JWT_SECRET=1

// Config is the API's runtime configuration.
type Config struct {
	// Addr is the listen address, for example ":8080".
	Addr string
	// DatabaseURL is the Postgres connection string.
	DatabaseURL string
	// WebOrigin is the single browser origin allowed by CORS.
	WebOrigin string
	// JWTSecret signs access tokens (HS256). At least 32 bytes.
	JWTSecret string
	// TrustedProxies is how many reverse proxies in front of the API append to
	// X-Forwarded-For. 0 means clients connect directly.
	TrustedProxies int
}

// Load reads configuration through getenv (normally os.Getenv). It returns an
// error naming every missing required variable and every invalid value: it
// never includes a secret's value.
func Load(getenv func(string) string) (Config, error) {
	cfg := Config{
		Addr:        withDefault(getenv("API_ADDR"), ":8080"),
		DatabaseURL: getenv("DATABASE_URL"),
		WebOrigin:   withDefault(getenv("WEB_ORIGIN"), "http://localhost:3000"),
		JWTSecret:   getenv("JWT_SECRET"),
	}
	var errs []error
	if cfg.DatabaseURL == "" {
		errs = append(errs, errors.New("DATABASE_URL is required"))
	}
	switch {
	case cfg.JWTSecret == "":
		errs = append(errs, errors.New("JWT_SECRET is required"))
	case len(cfg.JWTSecret) < auth.MinSecretLength:
		errs = append(errs, fmt.Errorf("JWT_SECRET must be at least %d bytes", auth.MinSecretLength))
	case cfg.JWTSecret == DevJWTSecret && getenv("ALLOW_DEV_JWT_SECRET") != "1":
		errs = append(errs, errors.New("JWT_SECRET is the public development value: set a real secret (ALLOW_DEV_JWT_SECRET=1 allows it for local development only)"))
	}
	if err := validateOrigin(cfg.WebOrigin); err != nil {
		errs = append(errs, err)
	}
	if raw := getenv("TRUSTED_PROXY_COUNT"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 0 || n > maxTrustedProxies {
			errs = append(errs, fmt.Errorf("TRUSTED_PROXY_COUNT must be an integer from 0 to %d, got %q", maxTrustedProxies, raw))
		} else {
			cfg.TrustedProxies = n
		}
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
