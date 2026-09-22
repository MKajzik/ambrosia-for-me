package httpapi_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/InzKazik/mealplanner/backend/internal/api"
	"github.com/InzKazik/mealplanner/backend/internal/auth"
	"github.com/InzKazik/mealplanner/backend/internal/db"
	"github.com/InzKazik/mealplanner/backend/internal/httpapi"
	"github.com/InzKazik/mealplanner/backend/internal/service"
	"github.com/InzKazik/mealplanner/backend/internal/store"
	"github.com/InzKazik/mealplanner/backend/internal/testutil"
)

func newAccountRouter(t *testing.T) http.Handler {
	t.Helper()
	pool, err := db.Connect(context.Background(), testutil.NewMigratedDatabase(t))
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(pool.Close)

	issuer := auth.NewTokenIssuer([]byte("0123456789abcdef0123456789abcdef"), 15*time.Minute, time.Now)
	svc := service.NewAuth(store.New(pool),
		auth.NewHasher(auth.HashParams{MemoryKiB: 8, Iterations: 1, Parallelism: 1}),
		issuer, 30*24*time.Hour, time.Now)
	return newTestRouter(t, func(d *httpapi.Deps) {
		d.Auth = svc
		d.Tokens = issuer
		d.Limits = httpapi.RateLimits{AuthPerMinute: 1000, UserPerMinute: 1000}
	})
}

func decodeAs[T any](t *testing.T, rec *httptest.ResponseRecorder) T {
	t.Helper()
	var v T
	if err := json.Unmarshal(rec.Body.Bytes(), &v); err != nil {
		t.Fatalf("decode %s: %v", rec.Body.String(), err)
	}
	return v
}

func problemCode(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	return decodeProblemBody(t, rec).Code
}

// TestAccountLifecycle drives every account endpoint through the real router,
// service and Postgres. contract() validates each request and response
// against openapi.yaml.
func TestAccountLifecycle(t *testing.T) {
	router := newAccountRouter(t)
	creds := func(email, password string) requestOption {
		return withBody(`{"email":"` + email + `","password":"` + password + `"}`)
	}
	const password = "a-long-enough-password"

	// Register.
	rec := contract(t, router, http.MethodPost, "/auth/register",
		withBody(`{"email":"Alice@Example.com","password":"`+password+`","display_name":"Alice"}`))
	if rec.Code != http.StatusCreated {
		t.Fatalf("register: status = %d, body %s", rec.Code, rec.Body.String())
	}
	registered := decodeAs[api.AuthResponse](t, rec)
	if registered.TokenType != api.AuthResponseTokenTypeBearer || registered.ExpiresIn != 900 {
		t.Errorf("register: token_type=%q expires_in=%d, want Bearer and 900", registered.TokenType, registered.ExpiresIn)
	}
	if registered.User.Email != "Alice@Example.com" || registered.User.DisplayName != "Alice" {
		t.Errorf("register: user = %+v", registered.User)
	}
	if !strings.Contains(rec.Body.String(), `"target_kcal":null`) {
		t.Errorf("a new user's unset targets must be explicit nulls: %s", rec.Body.String())
	}

	// A second account with the same email (different case) is refused.
	rec = contract(t, router, http.MethodPost, "/auth/register",
		withBody(`{"email":"ALICE@example.com","password":"`+password+`","display_name":"Other"}`))
	if rec.Code != http.StatusConflict || problemCode(t, rec) != httpapi.CodeEmailTaken {
		t.Errorf("duplicate register: status = %d, body %s", rec.Code, rec.Body.String())
	}

	// Login: wrong password and unknown email are indistinguishable.
	for _, c := range []struct{ email, pw string }{{"alice@example.com", "wrong-password!"}, {"nobody@example.com", password}} {
		rec = contract(t, router, http.MethodPost, "/auth/login", creds(c.email, c.pw))
		if rec.Code != http.StatusUnauthorized || problemCode(t, rec) != httpapi.CodeInvalidCredentials {
			t.Errorf("login(%s): status = %d, body %s", c.email, rec.Code, rec.Body.String())
		}
	}
	rec = contract(t, router, http.MethodPost, "/auth/login", creds("alice@example.com", password))
	if rec.Code != http.StatusOK {
		t.Fatalf("login: status = %d, body %s", rec.Code, rec.Body.String())
	}
	session := decodeAs[api.AuthResponse](t, rec)

	// Read and update the profile.
	rec = contract(t, router, http.MethodGet, "/me", withBearer(session.AccessToken))
	if rec.Code != http.StatusOK || decodeAs[api.User](t, rec).Email != "Alice@Example.com" {
		t.Fatalf("GET /me: status = %d, body %s", rec.Code, rec.Body.String())
	}
	rec = contract(t, router, http.MethodPatch, "/me", withBearer(session.AccessToken),
		withBody(`{"display_name":"Ally","target_kcal":2200,"target_protein_g":150}`))
	if rec.Code != http.StatusOK {
		t.Fatalf("PATCH /me: status = %d, body %s", rec.Code, rec.Body.String())
	}
	updated := decodeAs[api.User](t, rec)
	if updated.DisplayName != "Ally" || updated.TargetKcal.MustGet() != 2200 || updated.TargetProteinG.MustGet() != 150 {
		t.Errorf("after PATCH: %+v", updated)
	}
	// Clearing one target with null leaves the others (and the name) alone.
	rec = contract(t, router, http.MethodPatch, "/me", withBearer(session.AccessToken), withBody(`{"target_kcal":null}`))
	cleared := decodeAs[api.User](t, rec)
	if !cleared.TargetKcal.IsNull() || cleared.TargetProteinG.MustGet() != 150 || cleared.DisplayName != "Ally" {
		t.Errorf("after clearing kcal: %s", rec.Body.String())
	}
	rec = contract(t, router, http.MethodPatch, "/me", withBearer(session.AccessToken),
		withBody(`{"target_fat_g":-1}`), withInvalidRequest())
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("PATCH /me with an out-of-range value: status = %d", rec.Code)
	}
	if p := decodeProblemBody(t, rec); len(p.Errors) != 1 || p.Errors[0].Field != "target_fat_g" || p.Errors[0].Code != httpapi.FieldOutOfRange {
		t.Errorf("problem = %+v, want one out_of_range error on target_fat_g", p)
	}

	// Refresh rotates the token; replaying the old one revokes the family.
	rec = contract(t, router, http.MethodPost, "/auth/refresh", withBody(`{"refresh_token":"`+session.RefreshToken+`"}`))
	if rec.Code != http.StatusOK {
		t.Fatalf("refresh: status = %d, body %s", rec.Code, rec.Body.String())
	}
	rotated := decodeAs[api.AuthResponse](t, rec)
	if rotated.RefreshToken == session.RefreshToken {
		t.Error("refresh token was not rotated")
	}
	rec = contract(t, router, http.MethodPost, "/auth/refresh", withBody(`{"refresh_token":"`+session.RefreshToken+`"}`))
	if rec.Code != http.StatusUnauthorized || problemCode(t, rec) != httpapi.CodeInvalidRefreshToken {
		t.Errorf("replayed refresh token: status = %d, body %s", rec.Code, rec.Body.String())
	}
	rec = contract(t, router, http.MethodPost, "/auth/refresh", withBody(`{"refresh_token":"`+rotated.RefreshToken+`"}`))
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("newest token after reuse: status = %d, want 401 (family revoked)", rec.Code)
	}

	// Logout revokes the session and is idempotent.
	rec = contract(t, router, http.MethodPost, "/auth/login", creds("alice@example.com", password))
	session = decodeAs[api.AuthResponse](t, rec)
	for i := 0; i < 2; i++ {
		rec = contract(t, router, http.MethodPost, "/auth/logout", withBody(`{"refresh_token":"`+session.RefreshToken+`"}`))
		if rec.Code != http.StatusNoContent {
			t.Errorf("logout #%d: status = %d, want 204", i+1, rec.Code)
		}
	}
	rec = contract(t, router, http.MethodPost, "/auth/refresh", withBody(`{"refresh_token":"`+session.RefreshToken+`"}`))
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("refresh after logout: status = %d, want 401", rec.Code)
	}

	// Delete the account: its access token stops working immediately.
	rec = contract(t, router, http.MethodPost, "/auth/login", creds("alice@example.com", password))
	session = decodeAs[api.AuthResponse](t, rec)
	rec = contract(t, router, http.MethodDelete, "/me", withBearer(session.AccessToken))
	if rec.Code != http.StatusNoContent {
		t.Fatalf("DELETE /me: status = %d, body %s", rec.Code, rec.Body.String())
	}
	rec = contract(t, router, http.MethodGet, "/me", withBearer(session.AccessToken))
	if rec.Code != http.StatusUnauthorized || problemCode(t, rec) != httpapi.CodeUnauthorized {
		t.Errorf("GET /me after deletion: status = %d, body %s", rec.Code, rec.Body.String())
	}
	rec = contract(t, router, http.MethodPost, "/auth/login", creds("alice@example.com", password))
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("login after deletion: status = %d, want 401", rec.Code)
	}
}
