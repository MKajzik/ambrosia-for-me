package config_test

import (
	"strings"
	"testing"

	"github.com/InzKazik/mealplanner/backend/internal/config"
)

func env(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func TestLoad(t *testing.T) {
	tests := []struct {
		name    string
		env     map[string]string
		want    config.Config
		wantErr string
		also    string // second substring the error must contain
	}{
		{
			name: "defaults applied",
			env:  map[string]string{"DATABASE_URL": "postgres://x"},
			want: config.Config{Addr: ":8080", DatabaseURL: "postgres://x", WebOrigin: "http://localhost:3000"},
		},
		{
			name: "all set",
			env: map[string]string{
				"API_ADDR": "127.0.0.1:9000", "DATABASE_URL": "postgres://y", "WEB_ORIGIN": "https://app.example",
			},
			want: config.Config{Addr: "127.0.0.1:9000", DatabaseURL: "postgres://y", WebOrigin: "https://app.example"},
		},
		{
			name:    "missing database url",
			env:     map[string]string{},
			wantErr: "DATABASE_URL is required",
		},
		{
			name: "origin with port",
			env:  map[string]string{"DATABASE_URL": "postgres://x", "WEB_ORIGIN": "https://app.example:8443"},
			want: config.Config{Addr: ":8080", DatabaseURL: "postgres://x", WebOrigin: "https://app.example:8443"},
		},
		{name: "origin wildcard", env: map[string]string{"DATABASE_URL": "postgres://x", "WEB_ORIGIN": "*"}, wantErr: "WEB_ORIGIN"},
		{name: "origin trailing slash", env: map[string]string{"DATABASE_URL": "postgres://x", "WEB_ORIGIN": "http://localhost:3000/"}, wantErr: "WEB_ORIGIN"},
		{name: "origin no scheme", env: map[string]string{"DATABASE_URL": "postgres://x", "WEB_ORIGIN": "localhost:3000"}, wantErr: "WEB_ORIGIN"},
		{name: "origin bad scheme", env: map[string]string{"DATABASE_URL": "postgres://x", "WEB_ORIGIN": "ftp://app.example"}, wantErr: "WEB_ORIGIN"},
		{name: "origin no host", env: map[string]string{"DATABASE_URL": "postgres://x", "WEB_ORIGIN": "http://"}, wantErr: "WEB_ORIGIN"},
		{name: "origin with path", env: map[string]string{"DATABASE_URL": "postgres://x", "WEB_ORIGIN": "https://app.example/path"}, wantErr: "WEB_ORIGIN"},
		{name: "origin with userinfo", env: map[string]string{"DATABASE_URL": "postgres://x", "WEB_ORIGIN": "https://user@app.example"}, wantErr: "WEB_ORIGIN"},
		{name: "origin with query", env: map[string]string{"DATABASE_URL": "postgres://x", "WEB_ORIGIN": "https://app.example?x=1"}, wantErr: "WEB_ORIGIN"},
		{name: "origin with fragment", env: map[string]string{"DATABASE_URL": "postgres://x", "WEB_ORIGIN": "https://app.example#frag"}, wantErr: "WEB_ORIGIN"},
		{
			name:    "both database url and origin invalid",
			env:     map[string]string{"WEB_ORIGIN": "*"},
			wantErr: "DATABASE_URL is required",
			also:    "WEB_ORIGIN must be an origin",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := config.Load(env(tt.env))
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("err = %v, want containing %q", err, tt.wantErr)
				}
				if tt.also != "" && !strings.Contains(err.Error(), tt.also) {
					t.Fatalf("err = %v, want also containing %q", err, tt.also)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Errorf("got %+v, want %+v", got, tt.want)
			}
		})
	}
}
