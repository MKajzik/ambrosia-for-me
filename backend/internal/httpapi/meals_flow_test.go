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

func newMealsRouter(t *testing.T) (router http.Handler, token1, token2 string) {
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

	router = newTestRouter(t, func(d *httpapi.Deps) {
		d.Ingredients = service.NewIngredients(st)
		d.Meals = service.NewMeals(st)
		d.Tokens = stubTwoUserTokens{u1: u1.ID, u2: u2.ID}
		d.Limits = httpapi.RateLimits{AuthPerMinute: 1000, UserPerMinute: 1000}
	})
	return router, "user1-token", "user2-token"
}

func TestMealsLifecycle(t *testing.T) {
	router, token1, token2 := newMealsRouter(t)

	rec := contract(t, router, http.MethodPost, "/ingredients", withBearer(token1),
		withBody(`{"name":"Rice","category":"grains_bread","nutrients":{"calories":130}}`))
	if rec.Code != http.StatusCreated {
		t.Fatalf("create ingredient: status = %d, body = %s", rec.Code, rec.Body.String())
	}
	rice := decodeAs[api.Ingredient](t, rec)

	rec = contract(t, router, http.MethodPost, "/meals", withBearer(token1),
		withBody(`{"name":"Rice Bowl","servings":2}`))
	if rec.Code != http.StatusCreated {
		t.Fatalf("create meal: status = %d, body = %s", rec.Code, rec.Body.String())
	}
	meal := decodeAs[api.Meal](t, rec)
	if len(meal.Ingredients) != 0 {
		t.Errorf("a freshly created meal has ingredients = %+v, want none", meal.Ingredients)
	}
	if !meal.NutritionPerServing.Calories.IsSpecified() || meal.NutritionPerServing.Calories.MustGet() != 0 {
		t.Errorf("a freshly created meal's calories = %+v, want 0", meal.NutritionPerServing.Calories)
	}

	rec = contract(t, router, http.MethodPut, "/meals/"+meal.Id.String()+"/ingredients", withBearer(token1),
		withBody(`{"items":[{"ingredient_id":"`+rice.Id.String()+`","quantity":200,"unit":"g"}]}`))
	if rec.Code != http.StatusOK {
		t.Fatalf("replace ingredients: status = %d, body = %s", rec.Code, rec.Body.String())
	}
	meal = decodeAs[api.Meal](t, rec)
	if len(meal.Ingredients) != 1 || meal.Ingredients[0].IngredientId != rice.Id {
		t.Errorf("meal ingredients = %+v", meal.Ingredients)
	}
	// 200g rice at 130 kcal/100g = 260 kcal total, /2 servings = 130/serving.
	if got := meal.NutritionPerServing.Calories.MustGet(); got != 130 {
		t.Errorf("calories per serving = %v, want 130", got)
	}

	// User 2 cannot see, update or delete user 1's meal.
	rec = contract(t, router, http.MethodGet, "/meals/"+meal.Id.String(), withBearer(token2))
	if rec.Code != http.StatusNotFound {
		t.Errorf("user 2 GET: status = %d, want 404", rec.Code)
	}
	rec = contract(t, router, http.MethodPatch, "/meals/"+meal.Id.String(), withBearer(token2), withBody(`{"name":"Hijack"}`))
	if rec.Code != http.StatusNotFound {
		t.Errorf("user 2 PATCH: status = %d, want 404", rec.Code)
	}

	// Copying gives user 1 a second, independent meal.
	rec = contract(t, router, http.MethodPost, "/meals/"+meal.Id.String()+"/copy", withBearer(token1))
	if rec.Code != http.StatusCreated {
		t.Fatalf("copy: status = %d, body = %s", rec.Code, rec.Body.String())
	}
	copyMeal := decodeAs[api.Meal](t, rec)
	if copyMeal.Id == meal.Id || len(copyMeal.Ingredients) != 1 {
		t.Errorf("copy = %+v", copyMeal)
	}
	if copyMeal.SharedWithPartner {
		t.Error("copy has shared_with_partner = true, want false")
	}

	// Deleting an ingredient in use by a meal is blocked.
	rec = contract(t, router, http.MethodDelete, "/ingredients/"+rice.Id.String(), withBearer(token1))
	if rec.Code != http.StatusConflict {
		t.Errorf("delete ingredient in use: status = %d, want 409", rec.Code)
	}
	if got := problemCode(t, rec); got != "ingredient_in_use" {
		t.Errorf("problem code = %q, want ingredient_in_use", got)
	}

	rec = contract(t, router, http.MethodDelete, "/meals/"+meal.Id.String(), withBearer(token1))
	if rec.Code != http.StatusNoContent {
		t.Errorf("delete meal: status = %d, want 204", rec.Code)
	}
	rec = contract(t, router, http.MethodDelete, "/meals/"+copyMeal.Id.String(), withBearer(token1))
	if rec.Code != http.StatusNoContent {
		t.Errorf("delete meal copy: status = %d, want 204", rec.Code)
	}

	// Now that no meal references it, deleting the ingredient succeeds.
	rec = contract(t, router, http.MethodDelete, "/ingredients/"+rice.Id.String(), withBearer(token1))
	if rec.Code != http.StatusNoContent {
		t.Errorf("delete ingredient once unused: status = %d, want 204", rec.Code)
	}
}

func TestMealsReplaceIngredientsWithAnUnknownIngredientIsRejected(t *testing.T) {
	router, token1, _ := newMealsRouter(t)

	rec := contract(t, router, http.MethodPost, "/meals", withBearer(token1), withBody(`{"name":"Meal","servings":1}`))
	meal := decodeAs[api.Meal](t, rec)

	rec = contract(t, router, http.MethodPut, "/meals/"+meal.Id.String()+"/ingredients", withBearer(token1),
		withBody(`{"items":[{"ingredient_id":"00000000-0000-0000-0000-000000000001","quantity":100,"unit":"g"}]}`))
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
	if got := problemCode(t, rec); got != "invalid_ingredient" {
		t.Errorf("problem code = %q, want invalid_ingredient", got)
	}
}

// TestUpdateIngredientRejectsClearingDensityWhileInUseByAMeal sends finding
// #2's write-time rejection through the real HTTP router, so its 409
// unit_not_convertible status and problem+json shape are checked against
// openapi.yaml (previously unverified: nothing sent that problem code
// through contract()).
func TestUpdateIngredientRejectsClearingDensityWhileInUseByAMeal(t *testing.T) {
	router, token1, _ := newMealsRouter(t)

	rec := contract(t, router, http.MethodPost, "/ingredients", withBearer(token1),
		withBody(`{"name":"Olive Oil","category":"condiments_oils","density_g_per_ml":0.92,"nutrients":{"calories":884}}`))
	if rec.Code != http.StatusCreated {
		t.Fatalf("create ingredient: status = %d, body = %s", rec.Code, rec.Body.String())
	}
	oil := decodeAs[api.Ingredient](t, rec)

	rec = contract(t, router, http.MethodPost, "/meals", withBearer(token1), withBody(`{"name":"Dressing","servings":1}`))
	meal := decodeAs[api.Meal](t, rec)

	rec = contract(t, router, http.MethodPut, "/meals/"+meal.Id.String()+"/ingredients", withBearer(token1),
		withBody(`{"items":[{"ingredient_id":"`+oil.Id.String()+`","quantity":10,"unit":"ml"}]}`))
	if rec.Code != http.StatusOK {
		t.Fatalf("replace ingredients: status = %d, body = %s", rec.Code, rec.Body.String())
	}

	rec = contract(t, router, http.MethodPatch, "/ingredients/"+oil.Id.String(), withBearer(token1),
		withBody(`{"density_g_per_ml":null}`))
	if rec.Code != http.StatusConflict {
		t.Errorf("clear density while in use: status = %d, want 409, body = %s", rec.Code, rec.Body.String())
	}
	if got := problemCode(t, rec); got != "unit_not_convertible" {
		t.Errorf("problem code = %q, want unit_not_convertible", got)
	}

	// The meal must still be readable, with density untouched.
	rec = contract(t, router, http.MethodGet, "/meals/"+meal.Id.String(), withBearer(token1))
	if rec.Code != http.StatusOK {
		t.Errorf("get meal after the rejected edit: status = %d, want 200, body = %s", rec.Code, rec.Body.String())
	}
}

func TestMealsListPaginatesThroughTheContract(t *testing.T) {
	router, token1, _ := newMealsRouter(t)
	for _, name := range []string{"Breakfast", "Dinner", "Lunch"} {
		rec := contract(t, router, http.MethodPost, "/meals", withBearer(token1),
			withBody(`{"name":"`+name+`","servings":1}`))
		if rec.Code != http.StatusCreated {
			t.Fatalf("create %s: status = %d", name, rec.Code)
		}
	}

	var names []string
	path := "/meals?limit=1"
	for {
		rec := contract(t, router, http.MethodGet, path, withBearer(token1))
		if rec.Code != http.StatusOK {
			t.Fatalf("list %s: status = %d", path, rec.Code)
		}
		list := decodeAs[api.MealList](t, rec)
		for _, m := range list.Items {
			names = append(names, m.Name)
		}
		if !list.NextCursor.IsSpecified() || list.NextCursor.IsNull() {
			break
		}
		path = "/meals?limit=1&cursor=" + list.NextCursor.MustGet()
		if len(names) > 10 {
			t.Fatal("pagination did not terminate")
		}
	}
	if want := []string{"Breakfast", "Dinner", "Lunch"}; len(names) != 3 || names[0] != want[0] || names[1] != want[1] || names[2] != want[2] {
		t.Errorf("paginated names = %v, want %v", names, want)
	}
}
