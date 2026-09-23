package httpapi_test

import (
	"net/http"
	"testing"

	"github.com/InzKazik/mealplanner/backend/internal/api"
)

func TestPlanLifecycle(t *testing.T) {
	router, token1, token2 := newDietTemplatesRouter(t)

	rec := contract(t, router, http.MethodPost, "/ingredients", withBearer(token1),
		withBody(`{"name":"Oats","category":"grains_bread","nutrients":{"calories":389}}`))
	if rec.Code != http.StatusCreated {
		t.Fatalf("create ingredient: status = %d, body = %s", rec.Code, rec.Body.String())
	}
	oats := decodeAs[api.Ingredient](t, rec)

	rec = contract(t, router, http.MethodPost, "/meals", withBearer(token1), withBody(`{"name":"Oatmeal","servings":1}`))
	if rec.Code != http.StatusCreated {
		t.Fatalf("create meal: status = %d, body = %s", rec.Code, rec.Body.String())
	}
	meal := decodeAs[api.Meal](t, rec)
	rec = contract(t, router, http.MethodPut, "/meals/"+meal.Id.String()+"/ingredients", withBearer(token1),
		withBody(`{"items":[{"ingredient_id":"`+oats.Id.String()+`","quantity":50,"unit":"g"}]}`))
	if rec.Code != http.StatusOK {
		t.Fatalf("replace meal ingredients: status = %d, body = %s", rec.Code, rec.Body.String())
	}
	// 50g oats at 389 kcal/100g: 194.5 kcal per serving.

	rec = contract(t, router, http.MethodPut, "/plan/2026-05-04/breakfast", withBearer(token1),
		withBody(`{"meal_id":"`+meal.Id.String()+`"}`))
	if rec.Code != http.StatusOK {
		t.Fatalf("set plan entry: status = %d, body = %s", rec.Code, rec.Body.String())
	}
	entry := decodeAs[api.PlanEntry](t, rec)
	if !entry.FromTemplateId.IsNull() {
		t.Error("a manually set entry has from_template_id set, want null")
	}

	rec = contract(t, router, http.MethodGet, "/plan?from=2026-05-04&to=2026-05-04", withBearer(token1))
	if rec.Code != http.StatusOK {
		t.Fatalf("get plan: status = %d, body = %s", rec.Code, rec.Body.String())
	}
	rng := decodeAs[api.PlanRange](t, rec)
	if len(rng.Days) != 1 || len(rng.Days[0].Entries) != 1 {
		t.Fatalf("plan range = %+v, want one day with one entry", rng)
	}
	if !rng.Days[0].NutritionPerDay.Calories.IsSpecified() || rng.Days[0].NutritionPerDay.Calories.MustGet() != 194.5 {
		t.Errorf("day calories = %+v, want 194.5", rng.Days[0].NutritionPerDay.Calories)
	}

	rec = contract(t, router, http.MethodGet, "/plan?from=2026-05-04&to=2026-05-04", withBearer(token2))
	if rec.Code != http.StatusOK {
		t.Fatalf("get plan (other user): status = %d, body = %s", rec.Code, rec.Body.String())
	}
	rng2 := decodeAs[api.PlanRange](t, rec)
	if len(rng2.Days) != 1 || len(rng2.Days[0].Entries) != 0 {
		t.Errorf("other user's plan range = %+v, want the same date with no entries (isolated)", rng2)
	}

	rec = contract(t, router, http.MethodDelete, "/plan/2026-05-04/breakfast", withBearer(token1))
	if rec.Code != http.StatusNoContent {
		t.Errorf("delete plan entry: status = %d, want 204", rec.Code)
	}
	rec = contract(t, router, http.MethodDelete, "/plan/2026-05-04/breakfast", withBearer(token1))
	if rec.Code != http.StatusNotFound {
		t.Errorf("delete plan entry again: status = %d, want 404", rec.Code)
	}
}
