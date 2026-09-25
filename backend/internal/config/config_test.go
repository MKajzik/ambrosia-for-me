package config_test

import (
	"strings"
	"testing"

	"github.com/InzKazik/mealplanner/backend/internal/config"
)

const secret = "0123456789abcdef0123456789abcdef" // 32 bytes

// envWith returns a getenv over a valid environment (DATABASE_URL and JWT_SECRET set);
// each test case overrides keys, or removes them by setting "".
func envWith(overrides map[string]string) func(string) string {
	m := map[string]string{"DATABASE_URL": "postgres://x", "JWT_SECRET": secret}
	for k, v := range overrides {
		m[k] = v
	}
	return func(k string) string { return m[k] }
}

func TestLoad(t *testing.T) {
	tests := []struct {
		name      string
		overrides map[string]string
		want      config.Config
		wantErr   string
		also      string // second substring the error must contain
	}{
		{
			name: "defaults applied",
			want: config.Config{Addr: ":8080", DatabaseURL: "postgres://x", WebOrigin: "http://localhost:3000", JWTSecret: secret},
		},
		{
			name: "all set",
			overrides: map[string]string{
				"API_ADDR": "127.0.0.1:9000", "DATABASE_URL": "postgres://y", "WEB_ORIGIN": "https://app.example",
				"TRUSTED_PROXY_COUNT": "2",
			},
			want: config.Config{
				Addr: "127.0.0.1:9000", DatabaseURL: "postgres://y", WebOrigin: "https://app.example",
				JWTSecret: secret, TrustedProxies: 2,
			},
		},
		{name: "missing database url", overrides: map[string]string{"DATABASE_URL": ""}, wantErr: "DATABASE_URL is required"},
		{name: "missing jwt secret", overrides: map[string]string{"JWT_SECRET": ""}, wantErr: "JWT_SECRET is required"},
		{name: "jwt secret one byte short", overrides: map[string]string{"JWT_SECRET": strings.Repeat("s", 31)}, wantErr: "JWT_SECRET must be at least 32 bytes"},
		{name: "jwt secret exactly 32 bytes", overrides: map[string]string{"JWT_SECRET": strings.Repeat("s", 32)},
			want: config.Config{Addr: ":8080", DatabaseURL: "postgres://x", WebOrigin: "http://localhost:3000", JWTSecret: strings.Repeat("s", 32)}},
		{name: "public dev secret is refused by default", overrides: map[string]string{"JWT_SECRET": config.DevJWTSecret}, wantErr: "public development value"},
		{name: "public dev secret is allowed with ALLOW_DEV_JWT_SECRET=1",
			overrides: map[string]string{"JWT_SECRET": config.DevJWTSecret, "ALLOW_DEV_JWT_SECRET": "1"},
			want:      config.Config{Addr: ":8080", DatabaseURL: "postgres://x", WebOrigin: "http://localhost:3000", JWTSecret: config.DevJWTSecret}},
		{name: "ALLOW_DEV_JWT_SECRET must be exactly 1", overrides: map[string]string{"JWT_SECRET": config.DevJWTSecret, "ALLOW_DEV_JWT_SECRET": "true"}, wantErr: "public development value"},
		{name: "short jwt secret", overrides: map[string]string{"JWT_SECRET": "too-short"}, wantErr: "JWT_SECRET must be at least 32 bytes"},
		{
			name:      "origin with port",
			overrides: map[string]string{"WEB_ORIGIN": "https://app.example:8443"},
			want:      config.Config{Addr: ":8080", DatabaseURL: "postgres://x", WebOrigin: "https://app.example:8443", JWTSecret: secret},
		},
		{name: "origin wildcard", overrides: map[string]string{"WEB_ORIGIN": "*"}, wantErr: "WEB_ORIGIN"},
		{name: "origin trailing slash", overrides: map[string]string{"WEB_ORIGIN": "http://localhost:3000/"}, wantErr: "WEB_ORIGIN"},
		{name: "origin no scheme", overrides: map[string]string{"WEB_ORIGIN": "localhost:3000"}, wantErr: "WEB_ORIGIN"},
		{name: "origin bad scheme", overrides: map[string]string{"WEB_ORIGIN": "ftp://app.example"}, wantErr: "WEB_ORIGIN"},
		{name: "origin no host", overrides: map[string]string{"WEB_ORIGIN": "http://"}, wantErr: "WEB_ORIGIN"},
		{name: "origin with path", overrides: map[string]string{"WEB_ORIGIN": "https://app.example/path"}, wantErr: "WEB_ORIGIN"},
		{name: "origin with userinfo", overrides: map[string]string{"WEB_ORIGIN": "https://user@app.example"}, wantErr: "WEB_ORIGIN"},
		{name: "origin with query", overrides: map[string]string{"WEB_ORIGIN": "https://app.example?x=1"}, wantErr: "WEB_ORIGIN"},
		{name: "origin with fragment", overrides: map[string]string{"WEB_ORIGIN": "https://app.example#frag"}, wantErr: "WEB_ORIGIN"},
		{name: "proxy count zero", overrides: map[string]string{"TRUSTED_PROXY_COUNT": "0"},
			want: config.Config{Addr: ":8080", DatabaseURL: "postgres://x", WebOrigin: "http://localhost:3000", JWTSecret: secret}},
		{name: "proxy count at the maximum", overrides: map[string]string{"TRUSTED_PROXY_COUNT": "10"},
			want: config.Config{Addr: ":8080", DatabaseURL: "postgres://x", WebOrigin: "http://localhost:3000", JWTSecret: secret, TrustedProxies: 10}},
		{name: "proxy count just over the maximum", overrides: map[string]string{"TRUSTED_PROXY_COUNT": "11"}, wantErr: "TRUSTED_PROXY_COUNT"},
		{name: "proxy count not a number", overrides: map[string]string{"TRUSTED_PROXY_COUNT": "two"}, wantErr: "TRUSTED_PROXY_COUNT"},
		{name: "proxy count negative", overrides: map[string]string{"TRUSTED_PROXY_COUNT": "-1"}, wantErr: "TRUSTED_PROXY_COUNT"},
		{name: "proxy count absurd", overrides: map[string]string{"TRUSTED_PROXY_COUNT": "50"}, wantErr: "TRUSTED_PROXY_COUNT"},
		{name: "auth rate limit set", overrides: map[string]string{"AUTH_RATE_LIMIT_PER_MINUTE": "600"},
			want: config.Config{Addr: ":8080", DatabaseURL: "postgres://x", WebOrigin: "http://localhost:3000", JWTSecret: secret, AuthRateLimitPerMinute: 600}},
		{name: "auth rate limit at the maximum", overrides: map[string]string{"AUTH_RATE_LIMIT_PER_MINUTE": "10000"},
			want: config.Config{Addr: ":8080", DatabaseURL: "postgres://x", WebOrigin: "http://localhost:3000", JWTSecret: secret, AuthRateLimitPerMinute: 10000}},
		{name: "auth rate limit just over the maximum", overrides: map[string]string{"AUTH_RATE_LIMIT_PER_MINUTE": "10001"}, wantErr: "AUTH_RATE_LIMIT_PER_MINUTE"},
		{name: "auth rate limit zero", overrides: map[string]string{"AUTH_RATE_LIMIT_PER_MINUTE": "0"}, wantErr: "AUTH_RATE_LIMIT_PER_MINUTE"},
		{name: "auth rate limit negative", overrides: map[string]string{"AUTH_RATE_LIMIT_PER_MINUTE": "-5"}, wantErr: "AUTH_RATE_LIMIT_PER_MINUTE"},
		{name: "auth rate limit not a number", overrides: map[string]string{"AUTH_RATE_LIMIT_PER_MINUTE": "many"}, wantErr: "AUTH_RATE_LIMIT_PER_MINUTE"},
		{
			name:      "several problems are all reported",
			overrides: map[string]string{"DATABASE_URL": "", "JWT_SECRET": "", "WEB_ORIGIN": "*"},
			wantErr:   "DATABASE_URL is required",
			also:      "JWT_SECRET is required",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := config.Load(envWith(tt.overrides))
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

func TestLoadDoesNotPutTheSecretInErrors(t *testing.T) {
	_, err := config.Load(envWith(map[string]string{"JWT_SECRET": "short-secret-value", "DATABASE_URL": ""}))

	if err == nil || strings.Contains(err.Error(), "short-secret-value") {
		t.Errorf("err = %v; the error must exist and must not contain the secret", err)
	}
}
