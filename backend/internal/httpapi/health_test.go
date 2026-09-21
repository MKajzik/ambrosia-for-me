package httpapi_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/InzKazik/mealplanner/backend/internal/httpapi"
)

func TestHealthz(t *testing.T) {
	srv := httptest.NewServer(httpapi.NewRouter())
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/v1/healthz")
	if err != nil {
		t.Fatalf("GET /v1/healthz: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
	if got := resp.Header.Get("Content-Type"); got != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", got)
	}
	var body map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body["status"] != "ok" {
		t.Errorf("status field = %q, want ok", body["status"])
	}
}
