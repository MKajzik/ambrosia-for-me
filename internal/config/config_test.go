package config

import (
	"testing"
)

func TestLoadDefaults(t *testing.T) {
	cfg := Load()
	if cfg.Port != "8080" {
		t.Errorf("Port = %q, want %q", cfg.Port, "8080")
	}
	if cfg.DBPath != "mealplanner.db" {
		t.Errorf("DBPath = %q, want %q", cfg.DBPath, "mealplanner.db")
	}
	if cfg.JWTSecret != "dev-secret-change-me" {
		t.Errorf("JWTSecret = %q, want %q", cfg.JWTSecret, "dev-secret-change-me")
	}
}

func TestLoadFromEnv(t *testing.T) {
	t.Setenv("PORT", "3000")
	t.Setenv("DB_PATH", "/tmp/test.db")
	t.Setenv("JWT_SECRET", "super-secret")

	cfg := Load()
	if cfg.Port != "3000" {
		t.Errorf("Port = %q, want %q", cfg.Port, "3000")
	}
	if cfg.DBPath != "/tmp/test.db" {
		t.Errorf("DBPath = %q, want %q", cfg.DBPath, "/tmp/test.db")
	}
	if cfg.JWTSecret != "super-secret" {
		t.Errorf("JWTSecret = %q, want %q", cfg.JWTSecret, "super-secret")
	}
}
