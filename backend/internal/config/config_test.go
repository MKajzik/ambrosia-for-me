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
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := config.Load(env(tt.env))
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("err = %v, want containing %q", err, tt.wantErr)
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
