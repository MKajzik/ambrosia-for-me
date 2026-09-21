package httpapi_test

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"github.com/InzKazik/mealplanner/backend/internal/httpapi"
)

func do(t *testing.T, h http.Handler, req *http.Request) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func decodeProblem(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	if got := rec.Header().Get("Content-Type"); got != "application/problem+json" {
		t.Fatalf("Content-Type = %q, want application/problem+json", got)
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode problem: %v", err)
	}
	return body
}

func TestUnknownPathReturnsProblem(t *testing.T) {
	rec := do(t, newRouter(t, alwaysReady), httptest.NewRequest(http.MethodGet, "/v1/nope", nil))

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
	if code := decodeProblem(t, rec)["code"]; code != httpapi.CodeNotFound {
		t.Errorf("code = %v, want %s", code, httpapi.CodeNotFound)
	}
}

func TestWrongMethodReturnsProblem(t *testing.T) {
	rec := do(t, newRouter(t, alwaysReady), httptest.NewRequest(http.MethodPost, "/v1/healthz", nil))

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405", rec.Code)
	}
	if code := decodeProblem(t, rec)["code"]; code != httpapi.CodeMethodNotAllowed {
		t.Errorf("code = %v, want %s", code, httpapi.CodeMethodNotAllowed)
	}
	if got := rec.Header().Get("Allow"); got != "GET" {
		t.Errorf("Allow = %q, want %q (RFC 9110 requires it on 405)", got, "GET")
	}
}

func TestRequestIDIsGeneratedAndReplacedWhenInvalid(t *testing.T) {
	h := newRouter(t, alwaysReady)
	hex32 := regexp.MustCompile(`^[0-9a-f]{32}$`)

	rec := do(t, h, httptest.NewRequest(http.MethodGet, "/v1/healthz", nil))
	if id := rec.Header().Get("X-Request-Id"); !hex32.MatchString(id) {
		t.Errorf("generated X-Request-Id = %q, want 32 hex chars", id)
	}

	bad := httptest.NewRequest(http.MethodGet, "/v1/healthz", nil)
	bad.Header.Set("X-Request-Id", "has spaces and \"quotes\"")
	if id := do(t, h, bad).Header().Get("X-Request-Id"); !hex32.MatchString(id) {
		t.Errorf("invalid inbound ID was kept: %q", id)
	}
}

func TestRequestIDIsEchoedWhenValid(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/v1/healthz", nil)
	req.Header.Set("X-Request-Id", "trace-abc.123")

	rec := do(t, newRouter(t, alwaysReady), req)

	if id := rec.Header().Get("X-Request-Id"); id != "trace-abc.123" {
		t.Errorf("X-Request-Id = %q, want the inbound value", id)
	}
}

func TestRequestIsLoggedWithRequestID(t *testing.T) {
	var buf bytes.Buffer
	h := httpapi.NewRouter(httpapi.Deps{
		Logger:    slog.New(slog.NewJSONHandler(&buf, nil)),
		Ready:     alwaysReady,
		WebOrigin: "http://localhost:3000",
	})
	req := httptest.NewRequest(http.MethodGet, "/v1/healthz", nil)
	req.Header.Set("X-Request-Id", "log-me")

	do(t, h, req)

	var line map[string]any
	if err := json.Unmarshal(buf.Bytes(), &line); err != nil {
		t.Fatalf("log line is not JSON: %q (%v)", buf.String(), err)
	}
	want := map[string]any{"msg": "request", "request_id": "log-me", "method": "GET", "path": "/v1/healthz", "status": float64(200)}
	for k, v := range want {
		if line[k] != v {
			t.Errorf("log[%q] = %v, want %v", k, line[k], v)
		}
	}
}

func TestCORS(t *testing.T) {
	h := newRouter(t, alwaysReady)
	preflight := func(origin string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodOptions, "/v1/healthz", nil)
		req.Header.Set("Origin", origin)
		req.Header.Set("Access-Control-Request-Method", "GET")
		req.Header.Set("Access-Control-Request-Headers", "authorization")
		return do(t, h, req)
	}

	allowed := preflight("http://localhost:3000")
	if got := allowed.Header().Get("Access-Control-Allow-Origin"); got != "http://localhost:3000" {
		t.Errorf("allowed origin: Allow-Origin = %q, want the origin echoed", got)
	}
	if got := allowed.Header().Get("Access-Control-Allow-Headers"); !strings.Contains(strings.ToLower(got), "authorization") {
		t.Errorf("Allow-Headers = %q, want it to include Authorization", got)
	}

	denied := preflight("https://evil.example")
	if got := denied.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Errorf("disallowed origin got Allow-Origin = %q, want none", got)
	}
}
