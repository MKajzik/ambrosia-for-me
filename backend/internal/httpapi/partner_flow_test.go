package httpapi_test

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/InzKazik/mealplanner/backend/internal/api"
	"github.com/InzKazik/mealplanner/backend/internal/auth"
	"github.com/InzKazik/mealplanner/backend/internal/db"
	"github.com/InzKazik/mealplanner/backend/internal/httpapi"
	"github.com/InzKazik/mealplanner/backend/internal/service"
	"github.com/InzKazik/mealplanner/backend/internal/store"
	"github.com/InzKazik/mealplanner/backend/internal/store/sqlc"
	"github.com/InzKazik/mealplanner/backend/internal/testutil"
)

// partnerEnv is a router over the real services and Postgres with three
// users: Alice (token1), Bob (token2) and Carol (token3), not yet linked.
type partnerEnv struct {
	router                 http.Handler
	events                 *service.ListEventHub
	alice, bob, carol      uuid.UUID
	token1, token2, token3 string
}

type stubThreeUserTokens struct{ u1, u2, u3 uuid.UUID }

func (s stubThreeUserTokens) ParseAccess(token string) (uuid.UUID, error) {
	switch token {
	case "user1-token":
		return s.u1, nil
	case "user2-token":
		return s.u2, nil
	case "user3-token":
		return s.u3, nil
	}
	return uuid.Nil, auth.ErrInvalidAccessToken
}

func newPartnerEnv(t *testing.T) partnerEnv {
	t.Helper()
	pool, err := db.Connect(context.Background(), testutil.NewMigratedDatabase(t))
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(pool.Close)
	st := store.New(pool)

	hash := "hash"
	newUser := func(email, name string) uuid.UUID {
		u, err := st.CreateUser(context.Background(), sqlc.CreateUserParams{Email: email, PasswordHash: &hash, DisplayName: name})
		if err != nil {
			t.Fatalf("create user %s: %v", email, err)
		}
		return u.ID
	}
	alice, bob, carol := newUser("alice@example.com", "Alice"), newUser("bob@example.com", "Bob"), newUser("carol@example.com", "Carol")

	meals := service.NewMeals(st)
	events := service.NewListEventHub()
	router := newTestRouter(t, func(d *httpapi.Deps) {
		d.Ingredients = service.NewIngredients(st)
		d.Meals = meals
		d.DietTemplates = service.NewDietTemplates(st)
		d.Plan = service.NewPlan(st, meals)
		d.ShoppingLists = service.NewShoppingLists(st, events)
		d.Partners = service.NewPartners(st, events, time.Now)
		d.Tokens = stubThreeUserTokens{u1: alice, u2: bob, u3: carol}
		d.Limits = httpapi.RateLimits{AuthPerMinute: 1000, UserPerMinute: 1000, AcceptPerHour: 1000}
	})
	return partnerEnv{
		router: router, events: events, alice: alice, bob: bob, carol: carol,
		token1: "user1-token", token2: "user2-token", token3: "user3-token",
	}
}

// link makes Alice and Bob partners through the real routes.
func (e partnerEnv) link(t *testing.T) {
	t.Helper()
	rec := contract(t, e.router, http.MethodPost, "/partner/invite", withBearer(e.token1))
	if rec.Code != http.StatusCreated {
		t.Fatalf("invite: status = %d, body = %s", rec.Code, rec.Body.String())
	}
	invite := decodeAs[api.PartnerInvite](t, rec)
	rec = contract(t, e.router, http.MethodPost, "/partner/accept", withBearer(e.token2), withBody(`{"code":"`+invite.Code+`"}`))
	if rec.Code != http.StatusOK {
		t.Fatalf("accept: status = %d, body = %s", rec.Code, rec.Body.String())
	}
}

func TestPartnerLifecycle(t *testing.T) {
	e := newPartnerEnv(t)
	router := e.router

	rec := contract(t, router, http.MethodGet, "/partner", withBearer(e.token1))
	if rec.Code != http.StatusNotFound || problemCode(t, rec) != "partner_not_linked" {
		t.Fatalf("GET /partner without a partner: status = %d, body = %s, want 404 partner_not_linked", rec.Code, rec.Body.String())
	}

	rec = contract(t, router, http.MethodPost, "/partner/invite", withBearer(e.token1))
	if rec.Code != http.StatusCreated {
		t.Fatalf("invite: status = %d, body = %s", rec.Code, rec.Body.String())
	}
	invite := decodeAs[api.PartnerInvite](t, rec)
	if len(invite.Code) != 8 {
		t.Errorf("invite code = %q, want 8 characters", invite.Code)
	}

	rec = contract(t, router, http.MethodGet, "/partner", withBearer(e.token1))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /partner with a pending invite: status = %d, body = %s", rec.Code, rec.Body.String())
	}
	pending := decodeAs[api.Partnership](t, rec)
	if pending.Status != api.PartnershipStatusPending || pending.ExpiresAt.IsNull() || !pending.DisplayName.IsNull() || !pending.LinkedAt.IsNull() {
		t.Errorf("pending partnership = %+v, want status pending with only expires_at set", pending)
	}
	if strings.Contains(rec.Body.String(), invite.Code) {
		t.Error("GET /partner returned the invite code, want it shown only when the invite is created")
	}

	rec = contract(t, router, http.MethodPost, "/partner/accept", withBearer(e.token2), withBody(`{"code":"ZZZZZZZZ"}`))
	if rec.Code != http.StatusNotFound || problemCode(t, rec) != "invite_invalid" {
		t.Errorf("accept of a wrong code: status = %d, body = %s, want 404 invite_invalid", rec.Code, rec.Body.String())
	}
	rec = contract(t, router, http.MethodPost, "/partner/accept", withBearer(e.token1), withBody(`{"code":"`+invite.Code+`"}`))
	if rec.Code != http.StatusNotFound || problemCode(t, rec) != "invite_invalid" {
		t.Errorf("accept of the caller's own code: status = %d, body = %s, want 404 invite_invalid", rec.Code, rec.Body.String())
	}
	rec = contract(t, router, http.MethodPost, "/partner/accept", withBearer(e.token2), withBody(`{"code":""}`), withInvalidRequest())
	if rec.Code != http.StatusBadRequest {
		t.Errorf("accept of an empty code: status = %d, want 400", rec.Code)
	}

	rec = contract(t, router, http.MethodPost, "/partner/accept", withBearer(e.token2), withBody(`{"code":"`+strings.ToLower(invite.Code)+`"}`))
	if rec.Code != http.StatusOK {
		t.Fatalf("accept: status = %d, body = %s", rec.Code, rec.Body.String())
	}
	accepted := decodeAs[api.Partnership](t, rec)
	if accepted.Status != api.PartnershipStatusActive || accepted.DisplayName.MustGet() != "Alice" || accepted.LinkedAt.IsNull() || !accepted.ExpiresAt.IsNull() {
		t.Errorf("accepted = %+v, want an active partnership with Alice", accepted)
	}
	rec = contract(t, router, http.MethodGet, "/partner", withBearer(e.token1))
	if got := decodeAs[api.Partnership](t, rec); rec.Code != http.StatusOK || got.DisplayName.MustGet() != "Bob" {
		t.Errorf("the inviter's view = %+v (status %d), want an active partnership with Bob", got, rec.Code)
	}

	// Linked users cannot invite again, or accept anyone's code.
	rec = contract(t, router, http.MethodPost, "/partner/invite", withBearer(e.token1))
	if rec.Code != http.StatusConflict || problemCode(t, rec) != "partner_already_linked" {
		t.Errorf("invite while linked: status = %d, body = %s, want 409 partner_already_linked", rec.Code, rec.Body.String())
	}
	rec = contract(t, router, http.MethodPost, "/partner/invite", withBearer(e.token3))
	carolInvite := decodeAs[api.PartnerInvite](t, rec)
	rec = contract(t, router, http.MethodPost, "/partner/accept", withBearer(e.token1), withBody(`{"code":"`+carolInvite.Code+`"}`))
	if rec.Code != http.StatusConflict || problemCode(t, rec) != "partner_already_linked" {
		t.Errorf("accept while linked: status = %d, body = %s, want 409 partner_already_linked", rec.Code, rec.Body.String())
	}
	rec = contract(t, router, http.MethodPost, "/partner/accept", withBearer(e.token3), withBody(`{"code":"`+carolInvite.Code+`"}`))
	if rec.Code != http.StatusNotFound || problemCode(t, rec) != "invite_invalid" {
		t.Errorf("accept of Carol's own code: status = %d, want 404 invite_invalid", rec.Code)
	}

	// Either side may unlink; both then have no partner.
	rec = contract(t, router, http.MethodDelete, "/partner", withBearer(e.token2))
	if rec.Code != http.StatusNoContent {
		t.Fatalf("unlink: status = %d, body = %s", rec.Code, rec.Body.String())
	}
	rec = contract(t, router, http.MethodDelete, "/partner", withBearer(e.token1))
	if rec.Code != http.StatusNotFound || problemCode(t, rec) != "partner_not_linked" {
		t.Errorf("second unlink: status = %d, body = %s, want 404 partner_not_linked", rec.Code, rec.Body.String())
	}
	for _, token := range []string{e.token1, e.token2} {
		if rec := contract(t, router, http.MethodGet, "/partner", withBearer(token)); rec.Code != http.StatusNotFound {
			t.Errorf("GET /partner after the unlink: status = %d, want 404", rec.Code)
		}
	}

	// A pending invite is cancelled the same way.
	rec = contract(t, router, http.MethodDelete, "/partner", withBearer(e.token3))
	if rec.Code != http.StatusNoContent {
		t.Errorf("cancel a pending invite: status = %d, want 204", rec.Code)
	}
	if rec := contract(t, router, http.MethodPost, "/partner/accept", withBearer(e.token1), withBody(`{"code":"`+carolInvite.Code+`"}`)); rec.Code != http.StatusNotFound {
		t.Errorf("accept of a cancelled invite: status = %d, want 404", rec.Code)
	}
}

func TestPartnerRoutesRequireAuthentication(t *testing.T) {
	router := newTestRouter(t)
	for _, tc := range []struct{ method, path, body string }{
		{http.MethodGet, "/partner", ""},
		{http.MethodDelete, "/partner", ""},
		{http.MethodPost, "/partner/invite", ""},
		{http.MethodPost, "/partner/accept", `{"code":"ABCDEFGH"}`},
	} {
		opts := []requestOption{withInvalidRequest()}
		if tc.body != "" {
			opts = append(opts, withBody(tc.body))
		}
		rec := contract(t, router, tc.method, tc.path, opts...)
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("%s %s without a token: status = %d, want 401", tc.method, tc.path, rec.Code)
		}
	}
}

// rejectingPartners answers every Accept as a wrong code and every Get as no
// partner, so the limiter tests need no database.
type rejectingPartners struct{ httpapi.PartnerService }

func (rejectingPartners) Accept(context.Context, uuid.UUID, string) (service.Partnership, error) {
	return service.Partnership{}, service.ErrInviteInvalid
}

func (rejectingPartners) Get(context.Context, uuid.UUID) (service.Partnership, error) {
	return service.Partnership{}, service.ErrPartnerNotLinked
}

func TestPartnerAcceptIsRateLimitedPerUserAndPerIP(t *testing.T) {
	newRouterWithLimit := func(t *testing.T) http.Handler {
		return newTestRouter(t, func(d *httpapi.Deps) {
			d.Partners = rejectingPartners{}
			d.Limits = httpapi.RateLimits{AcceptPerHour: 2}
		})
	}
	accept := func(t *testing.T, h http.Handler, token, addr string) *struct{ code int } {
		t.Helper()
		rec := contract(t, h, http.MethodPost, "/partner/accept", withBearer(token), withRemoteAddr(addr), withBody(`{"code":"ABCDEFGH"}`))
		return &struct{ code int }{rec.Code}
	}

	t.Run("per user, whatever the IP", func(t *testing.T) {
		h := newRouterWithLimit(t)
		for i, ip := range []string{"198.51.100.1:1000", "198.51.100.2:1000"} {
			if got := accept(t, h, validToken, ip).code; got != http.StatusNotFound {
				t.Fatalf("attempt %d: status = %d, want 404", i+1, got)
			}
		}
		if got := accept(t, h, validToken, "198.51.100.3:1000").code; got != http.StatusTooManyRequests {
			t.Errorf("third attempt by one user from a fresh IP: status = %d, want 429", got)
		}
		if got := accept(t, h, validToken2, "198.51.100.4:1000").code; got != http.StatusNotFound {
			t.Errorf("another user's first attempt: status = %d, want 404 (the limit is per user)", got)
		}
	})

	t.Run("per IP, whatever the user", func(t *testing.T) {
		h := newRouterWithLimit(t)
		const ip = "203.0.113.7:1000"
		for i, token := range []string{validToken, validToken2} {
			if got := accept(t, h, token, ip).code; got != http.StatusNotFound {
				t.Fatalf("attempt %d: status = %d, want 404", i+1, got)
			}
		}
		if got := accept(t, h, validToken, ip).code; got != http.StatusTooManyRequests {
			t.Errorf("third attempt from one IP: status = %d, want 429", got)
		}
	})

	t.Run("only the accept route", func(t *testing.T) {
		h := newRouterWithLimit(t)
		for range 5 {
			rec := contract(t, h, http.MethodGet, "/partner", withBearer(validToken), withRemoteAddr("203.0.113.9:1000"))
			if rec.Code != http.StatusNotFound {
				t.Fatalf("GET /partner: status = %d, want 404 (never rate limited by the accept limits)", rec.Code)
			}
		}
	})
}
