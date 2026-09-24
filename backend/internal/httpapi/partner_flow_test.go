package httpapi_test

import (
	"context"
	"net/http"
	"net/http/httptest"
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

// create posts body to path as token and returns the decoded 201 response.
func create[T any](t *testing.T, router http.Handler, path, token, body string) T {
	t.Helper()
	rec := contract(t, router, http.MethodPost, path, withBearer(token), withBody(body))
	if rec.Code != http.StatusCreated {
		t.Fatalf("POST %s: status = %d, body = %s", path, rec.Code, rec.Body.String())
	}
	return decodeAs[T](t, rec)
}

// TestPartnerSharedMealsAndTemplatesThroughTheRoutes drives the read-only-plus-
// copy rules over HTTP: what a partner sees, what is 404, and what a copy
// carries over.
func TestPartnerSharedMealsAndTemplatesThroughTheRoutes(t *testing.T) {
	e := newPartnerEnv(t)
	router := e.router
	e.link(t)

	// Alice: a custom ingredient, a shared meal using it, a private meal, and a
	// shared template over the private meal.
	flour := create[api.Ingredient](t, router, "/ingredients", e.token1,
		`{"name":"Alice's Flour","category":"grains_bread","nutrients":{"calories":360}}`)
	shared := create[api.Meal](t, router, "/meals", e.token1, `{"name":"Flatbread","servings":2,"shared_with_partner":true}`)
	private := create[api.Meal](t, router, "/meals", e.token1, `{"name":"Secret Snack","servings":1}`)
	rec := contract(t, router, http.MethodPut, "/meals/"+shared.Id.String()+"/ingredients", withBearer(e.token1),
		withBody(`{"items":[{"ingredient_id":"`+flour.Id.String()+`","quantity":200,"unit":"g"}]}`))
	if rec.Code != http.StatusOK {
		t.Fatalf("replace ingredients: status = %d, body = %s", rec.Code, rec.Body.String())
	}
	template := create[api.DietTemplate](t, router, "/diet-templates", e.token1, `{"name":"Baking Week","day_count":2,"shared_with_partner":true}`)
	rec = contract(t, router, http.MethodPut, "/diet-templates/"+template.Id.String()+"/slots", withBearer(e.token1),
		withBody(`{"items":[{"day_index":0,"slot":"breakfast","meal_id":"`+private.Id.String()+`"}]}`))
	if rec.Code != http.StatusOK {
		t.Fatalf("replace slots: status = %d, body = %s", rec.Code, rec.Body.String())
	}

	// Meals: Bob reads the shared one (is_owner false), not the private one.
	mealPath := "/meals/" + shared.Id.String()
	rec = contract(t, router, http.MethodGet, mealPath, withBearer(e.token2))
	got := decodeAs[api.Meal](t, rec)
	if rec.Code != http.StatusOK || got.IsOwner || got.Ingredients[0].IngredientName != "Alice's Flour" || got.NutritionPerServing.Calories.MustGet() != 360 {
		t.Errorf("partner GET of the shared meal: status = %d, %+v, want it readable with is_owner false and 360 kcal per serving", rec.Code, got)
	}
	if rec = contract(t, router, http.MethodGet, mealPath, withBearer(e.token1)); !decodeAs[api.Meal](t, rec).IsOwner {
		t.Error("the owner's GET has is_owner false, want true")
	}
	if rec = contract(t, router, http.MethodGet, "/meals/"+private.Id.String(), withBearer(e.token2)); rec.Code != http.StatusNotFound {
		t.Errorf("partner GET of a private meal: status = %d, want 404", rec.Code)
	}
	if rec = contract(t, router, http.MethodGet, mealPath, withBearer(e.token3)); rec.Code != http.StatusNotFound {
		t.Errorf("stranger GET of a shared meal: status = %d, want 404", rec.Code)
	}
	for _, tc := range []struct{ method, path, body string }{
		{http.MethodPatch, mealPath, `{"name":"Mine"}`},
		{http.MethodDelete, mealPath, ""},
		{http.MethodPut, mealPath + "/ingredients", `{"items":[]}`},
	} {
		opts := []requestOption{withBearer(e.token2)}
		if tc.body != "" {
			opts = append(opts, withBody(tc.body))
		}
		if rec := contract(t, router, tc.method, tc.path, opts...); rec.Code != http.StatusNotFound {
			t.Errorf("partner %s %s: status = %d, want 404 (read-only + copy)", tc.method, tc.path, rec.Code)
		}
	}

	// Listings: own lists stay own; partner lists are separate.
	rec = contract(t, router, http.MethodGet, "/meals", withBearer(e.token2))
	if items := decodeAs[api.MealList](t, rec).Items; len(items) != 0 {
		t.Errorf("Bob's own /meals = %+v, want empty", items)
	}
	rec = contract(t, router, http.MethodGet, "/partner/meals", withBearer(e.token2))
	if items := decodeAs[api.MealList](t, rec).Items; rec.Code != http.StatusOK || len(items) != 1 || items[0].Id != shared.Id {
		t.Errorf("Bob's /partner/meals = %+v (status %d), want only the shared meal", items, rec.Code)
	}
	rec = contract(t, router, http.MethodGet, "/partner/meals", withBearer(e.token3))
	if rec.Code != http.StatusNotFound || problemCode(t, rec) != "partner_not_linked" {
		t.Errorf("stranger /partner/meals: status = %d, body = %s, want 404 partner_not_linked", rec.Code, rec.Body.String())
	}
	if rec = contract(t, router, http.MethodGet, "/partner/meals?cursor=not-a-cursor", withBearer(e.token2)); rec.Code != http.StatusBadRequest {
		t.Errorf("partner meals with a bad cursor: status = %d, want 400", rec.Code)
	}

	// Copy a partner meal: Bob's own private meal, on a duplicated ingredient.
	cp := create[api.Meal](t, router, mealPath+"/copy", e.token2, "")
	if !cp.IsOwner || cp.SharedWithPartner || cp.Ingredients[0].IngredientId == flour.Id || cp.NutritionPerServing.Calories.MustGet() != 360 {
		t.Errorf("copy = %+v, want Bob's private meal on a duplicated flour with the same 360 kcal per serving", cp)
	}
	if rec = contract(t, router, http.MethodPost, "/meals/"+private.Id.String()+"/copy", withBearer(e.token2)); rec.Code != http.StatusNotFound {
		t.Errorf("copy of a private meal: status = %d, want 404", rec.Code)
	}

	// Templates: readable with the slot's meal name, its meal still 404, copy carries the meal.
	tplPath := "/diet-templates/" + template.Id.String()
	rec = contract(t, router, http.MethodGet, tplPath, withBearer(e.token2))
	gotTpl := decodeAs[api.DietTemplate](t, rec)
	if rec.Code != http.StatusOK || gotTpl.IsOwner || len(gotTpl.Slots) != 1 || gotTpl.Slots[0].MealName != "Secret Snack" {
		t.Errorf("partner GET of the shared template: status = %d, %+v, want it readable, is_owner false, with the slot's meal name", rec.Code, gotTpl)
	}
	if rec = contract(t, router, http.MethodPost, tplPath+"/apply", withBearer(e.token2), withBody(`{"start_date":"2026-06-01"}`)); rec.Code != http.StatusNotFound {
		t.Errorf("partner apply of Alice's template: status = %d, want 404 (copy it first)", rec.Code)
	}
	rec = contract(t, router, http.MethodGet, "/partner/diet-templates", withBearer(e.token2))
	if items := decodeAs[api.DietTemplateList](t, rec).Items; rec.Code != http.StatusOK || len(items) != 1 || items[0].Id != template.Id {
		t.Errorf("Bob's /partner/diet-templates = %+v (status %d), want the shared template", items, rec.Code)
	}
	tplCopy := create[api.DietTemplate](t, router, tplPath+"/copy", e.token2, "")
	if !tplCopy.IsOwner || tplCopy.SharedWithPartner || len(tplCopy.Slots) != 1 || tplCopy.Slots[0].MealId == private.Id {
		t.Errorf("template copy = %+v, want Bob's private copy whose slot points at a meal copy", tplCopy)
	}
	if rec = contract(t, router, http.MethodPost, "/diet-templates/"+tplCopy.Id.String()+"/apply", withBearer(e.token2), withBody(`{"start_date":"2026-06-01"}`)); rec.Code != http.StatusNoContent {
		t.Errorf("apply of the copy: status = %d, body = %s, want 204", rec.Code, rec.Body.String())
	}

	// Unlinking ends all of it at once.
	if rec = contract(t, router, http.MethodDelete, "/partner", withBearer(e.token1)); rec.Code != http.StatusNoContent {
		t.Fatalf("unlink: status = %d", rec.Code)
	}
	if rec = contract(t, router, http.MethodGet, mealPath, withBearer(e.token2)); rec.Code != http.StatusNotFound {
		t.Errorf("partner GET of a shared meal after the unlink: status = %d, want 404", rec.Code)
	}
	if rec = contract(t, router, http.MethodGet, "/partner/diet-templates", withBearer(e.token2)); rec.Code != http.StatusNotFound {
		t.Errorf("/partner/diet-templates after the unlink: status = %d, want 404", rec.Code)
	}
	if rec = contract(t, router, http.MethodGet, "/meals/"+cp.Id.String(), withBearer(e.token2)); rec.Code != http.StatusOK {
		t.Errorf("Bob's copy after the unlink: status = %d, want 200 (copies are independent)", rec.Code)
	}
}

// TestPartnerSharedShoppingListsThroughTheRoutes covers the shopping-list
// half: both partners edit items, list-level actions are owner-only, and the
// list keeps its owner after an unlink.
func TestPartnerSharedShoppingListsThroughTheRoutes(t *testing.T) {
	e := newPartnerEnv(t)
	router := e.router
	e.link(t)

	shared := create[api.ShoppingList](t, router, "/shopping-lists", e.token1, `{"name":"Groceries","shared_with_partner":true}`)
	create[api.ShoppingList](t, router, "/shopping-lists", e.token1, `{"name":"Private"}`)
	listPath := "/shopping-lists/" + shared.Id.String()
	milk := create[api.ShoppingItem](t, router, listPath+"/items", e.token1, `{"name":"Milk"}`)

	rec := contract(t, router, http.MethodGet, listPath, withBearer(e.token2))
	if got := decodeAs[api.ShoppingList](t, rec); rec.Code != http.StatusOK || got.IsOwner || len(got.Items) != 1 {
		t.Errorf("partner GET of the shared list = %+v (status %d), want it readable with is_owner false", got, rec.Code)
	}
	rec = contract(t, router, http.MethodGet, "/partner/shopping-lists", withBearer(e.token2))
	if items := decodeAs[api.ShoppingListPage](t, rec).Items; rec.Code != http.StatusOK || len(items) != 1 || items[0].Id != shared.Id {
		t.Errorf("Bob's /partner/shopping-lists = %+v (status %d), want only the shared list", items, rec.Code)
	}
	rec = contract(t, router, http.MethodGet, "/shopping-lists", withBearer(e.token2))
	if items := decodeAs[api.ShoppingListPage](t, rec).Items; len(items) != 0 {
		t.Errorf("Bob's own /shopping-lists = %+v, want empty", items)
	}

	// The partner adds, checks, edits and deletes items.
	eggs := create[api.ShoppingItem](t, router, listPath+"/items", e.token2, `{"name":"Eggs"}`)
	if eggs.Origin != api.ShoppingItemOriginManual {
		t.Errorf("partner-added item origin = %q, want manual", eggs.Origin)
	}
	rec = contract(t, router, http.MethodPatch, listPath+"/items/"+milk.Id.String(), withBearer(e.token2), withBody(`{"checked":true}`))
	checked := decodeAs[api.ShoppingItem](t, rec)
	if rec.Code != http.StatusOK || !checked.Checked || checked.CheckedBy.MustGet() != e.bob {
		t.Errorf("partner check = %+v (status %d), want checked_by Bob", checked, rec.Code)
	}
	rec = contract(t, router, http.MethodPatch, listPath+"/items/"+milk.Id.String(), withBearer(e.token2), withBody(`{"version":1,"name":"Oat milk"}`))
	if rec.Code != http.StatusConflict {
		t.Errorf("partner edit at a stale version: status = %d, want 409", rec.Code)
	}
	if rec = contract(t, router, http.MethodDelete, listPath+"/items/"+eggs.Id.String(), withBearer(e.token2)); rec.Code != http.StatusNoContent {
		t.Errorf("partner delete of an item: status = %d, want 204", rec.Code)
	}

	// List-level actions are owner-only.
	for _, tc := range []struct{ method, path, body string }{
		{http.MethodPatch, listPath, `{"name":"Mine"}`},
		{http.MethodPatch, listPath, `{"shared_with_partner":false}`},
		{http.MethodDelete, listPath, ""},
		{http.MethodPost, "/shopping-lists/generate", `{"from":"2026-06-01","to":"2026-06-02","list_id":"` + shared.Id.String() + `"}`},
	} {
		opts := []requestOption{withBearer(e.token2)}
		if tc.body != "" {
			opts = append(opts, withBody(tc.body))
		}
		if rec := contract(t, router, tc.method, tc.path, opts...); rec.Code != http.StatusNotFound {
			t.Errorf("partner %s %s %s: status = %d, want 404 (owner-only)", tc.method, tc.path, tc.body, rec.Code)
		}
	}
	if rec = contract(t, router, http.MethodGet, listPath, withBearer(e.token3)); rec.Code != http.StatusNotFound {
		t.Errorf("stranger GET of the shared list: status = %d, want 404", rec.Code)
	}

	// After the unlink the list stays with its owner, partner edits included.
	if rec = contract(t, router, http.MethodDelete, "/partner", withBearer(e.token2)); rec.Code != http.StatusNoContent {
		t.Fatalf("unlink: status = %d", rec.Code)
	}
	if rec = contract(t, router, http.MethodGet, listPath, withBearer(e.token2)); rec.Code != http.StatusNotFound {
		t.Errorf("partner GET after the unlink: status = %d, want 404", rec.Code)
	}
	rec = contract(t, router, http.MethodGet, listPath, withBearer(e.token1))
	if got := decodeAs[api.ShoppingList](t, rec); rec.Code != http.StatusOK || len(got.Items) != 1 || !got.Items[0].Checked {
		t.Errorf("owner GET after the unlink = %+v (status %d), want the list intact with the partner's check", got, rec.Code)
	}
}

// TestPartnerEventStreamEndsWhenThePartnerIsUnlinked opens the partner's
// stream on a shared list and unlinks from the other side while it is open.
func TestPartnerEventStreamEndsWhenThePartnerIsUnlinked(t *testing.T) {
	e := newPartnerEnv(t)
	router := e.router
	e.link(t)
	shared := create[api.ShoppingList](t, router, "/shopping-lists", e.token1, `{"name":"Groceries","shared_with_partner":true}`)
	eventsPath := "/shopping-lists/" + shared.Id.String() + "/events"

	// contract() serves the request synchronously, so unlink from the side once
	// the partner's stream has subscribed.
	go func() {
		deadline := time.Now().Add(5 * time.Second)
		for e.events.Subscribers(shared.Id) == 0 && time.Now().Before(deadline) {
			time.Sleep(5 * time.Millisecond)
		}
		req := httptest.NewRequest(http.MethodDelete, "/v1/partner", nil)
		req.Header.Set("Authorization", "Bearer "+e.token1)
		if rec := do(t, router, req); rec.Code != http.StatusNoContent {
			t.Errorf("unlink from the side: status = %d, want 204", rec.Code)
		}
	}()
	rec := contract(t, router, http.MethodGet, eventsPath, withBearer(e.token2))
	if rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != "text/event-stream" {
		t.Fatalf("partner stream: status = %d, content type %q, want 200 text/event-stream", rec.Code, rec.Header().Get("Content-Type"))
	}
	if rec.Body.String() != ": connected\n\n" {
		t.Errorf("stream body = %q, want only the connected comment before the unlink closed it", rec.Body.String())
	}
	if rec = contract(t, router, http.MethodGet, eventsPath, withBearer(e.token2)); rec.Code != http.StatusNotFound {
		t.Errorf("partner reconnect after the unlink: status = %d, want 404", rec.Code)
	}
}
