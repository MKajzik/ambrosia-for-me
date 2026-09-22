package httpapi_test

import (
	"context"
	"net/http"
	"testing"

	"github.com/InzKazik/mealplanner/backend/internal/api"
	"github.com/InzKazik/mealplanner/backend/internal/db"
	"github.com/InzKazik/mealplanner/backend/internal/httpapi"
	"github.com/InzKazik/mealplanner/backend/internal/service"
	"github.com/InzKazik/mealplanner/backend/internal/store"
	"github.com/InzKazik/mealplanner/backend/internal/store/sqlc"
	"github.com/InzKazik/mealplanner/backend/internal/testutil"
)

func newDietTemplatesRouter(t *testing.T) (router http.Handler, token1, token2 string) {
	t.Helper()
	pool, err := db.Connect(context.Background(), testutil.NewMigratedDatabase(t))
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(pool.Close)
	st := store.New(pool)

	hash := "hash"
	u1, err := st.CreateUser(context.Background(), sqlc.CreateUserParams{Email: "a@example.com", PasswordHash: &hash, DisplayName: "A"})
	if err != nil {
		t.Fatalf("create user 1: %v", err)
	}
	u2, err := st.CreateUser(context.Background(), sqlc.CreateUserParams{Email: "b@example.com", PasswordHash: &hash, DisplayName: "B"})
	if err != nil {
		t.Fatalf("create user 2: %v", err)
	}

	meals := service.NewMeals(st)
	router = newTestRouter(t, func(d *httpapi.Deps) {
		d.Ingredients = service.NewIngredients(st)
		d.Meals = meals
		d.DietTemplates = service.NewDietTemplates(st)
		d.Plan = service.NewPlan(st, meals)
		d.Tokens = stubTwoUserTokens{u1: u1.ID, u2: u2.ID}
		d.Limits = httpapi.RateLimits{AuthPerMinute: 1000, UserPerMinute: 1000}
	})
	return router, "user1-token", "user2-token"
}

func TestDietTemplatesLifecycle(t *testing.T) {
	router, token1, token2 := newDietTemplatesRouter(t)

	rec := contract(t, router, http.MethodPost, "/ingredients", withBearer(token1),
		withBody(`{"name":"Rice","category":"grains_bread","nutrients":{"calories":130}}`))
	if rec.Code != http.StatusCreated {
		t.Fatalf("create ingredient: status = %d, body = %s", rec.Code, rec.Body.String())
	}
	rice := decodeAs[api.Ingredient](t, rec)

	rec = contract(t, router, http.MethodPost, "/meals", withBearer(token1), withBody(`{"name":"Rice Bowl","servings":1}`))
	if rec.Code != http.StatusCreated {
		t.Fatalf("create meal: status = %d, body = %s", rec.Code, rec.Body.String())
	}
	meal := decodeAs[api.Meal](t, rec)
	rec = contract(t, router, http.MethodPut, "/meals/"+meal.Id.String()+"/ingredients", withBearer(token1),
		withBody(`{"items":[{"ingredient_id":"`+rice.Id.String()+`","quantity":200,"unit":"g"}]}`))
	if rec.Code != http.StatusOK {
		t.Fatalf("replace meal ingredients: status = %d, body = %s", rec.Code, rec.Body.String())
	}

	rec = contract(t, router, http.MethodPost, "/diet-templates", withBearer(token1), withBody(`{"name":"One Week","day_count":7}`))
	if rec.Code != http.StatusCreated {
		t.Fatalf("create diet template: status = %d, body = %s", rec.Code, rec.Body.String())
	}
	tpl := decodeAs[api.DietTemplate](t, rec)
	if len(tpl.Slots) != 0 {
		t.Errorf("a freshly created template has slots = %+v, want none", tpl.Slots)
	}

	rec = contract(t, router, http.MethodPut, "/diet-templates/"+tpl.Id.String()+"/slots", withBearer(token1),
		withBody(`{"items":[{"day_index":0,"slot":"breakfast","meal_id":"`+meal.Id.String()+`"}]}`))
	if rec.Code != http.StatusOK {
		t.Fatalf("replace template slots: status = %d, body = %s", rec.Code, rec.Body.String())
	}
	tpl = decodeAs[api.DietTemplate](t, rec)
	if len(tpl.Slots) != 1 || tpl.Slots[0].Portion != 1 {
		t.Fatalf("template slots after replace = %+v, want one slot at portion 1 (the omitted portion defaulted)", tpl.Slots)
	}

	rec = contract(t, router, http.MethodGet, "/diet-templates/"+tpl.Id.String(), withBearer(token2))
	if rec.Code != http.StatusNotFound {
		t.Errorf("get another user's template: status = %d, want 404", rec.Code)
	}

	rec = contract(t, router, http.MethodPost, "/diet-templates/"+tpl.Id.String()+"/apply", withBearer(token1),
		withBody(`{"start_date":"2026-05-04"}`))
	if rec.Code != http.StatusNoContent {
		t.Fatalf("apply template: status = %d, body = %s", rec.Code, rec.Body.String())
	}
	rec = contract(t, router, http.MethodPost, "/diet-templates/"+tpl.Id.String()+"/apply", withBearer(token1),
		withBody(`{"start_date":"2026-05-04"}`))
	if rec.Code != http.StatusConflict {
		t.Errorf("re-apply without overwrite: status = %d, want 409", rec.Code)
	}

	rec = contract(t, router, http.MethodPost, "/diet-templates/"+tpl.Id.String()+"/copy", withBearer(token1))
	if rec.Code != http.StatusCreated {
		t.Fatalf("copy template: status = %d, body = %s", rec.Code, rec.Body.String())
	}
	cp := decodeAs[api.DietTemplate](t, rec)
	if cp.SharedWithPartner {
		t.Error("copy has shared_with_partner = true, want false")
	}

	rec = contract(t, router, http.MethodDelete, "/meals/"+meal.Id.String(), withBearer(token1))
	if rec.Code != http.StatusConflict {
		t.Errorf("delete a meal still scheduled in a template/plan: status = %d, want 409", rec.Code)
	}

	rec = contract(t, router, http.MethodDelete, "/diet-templates/"+tpl.Id.String(), withBearer(token1))
	if rec.Code != http.StatusNoContent {
		t.Errorf("delete template: status = %d, want 204", rec.Code)
	}
}
