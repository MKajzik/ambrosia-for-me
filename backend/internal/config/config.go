// Package config loads process configuration from environment variables.
package config

import (
	"errors"
	"fmt"
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
// error naming every missing required variable.
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
