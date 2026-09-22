package httpapi_test

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"

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

// allNutrientKeys mirrors NutrientAmounts in openapi.yaml, whose schema (also
// used, unlike the service and handlers, for CreateIngredientRequest and
// UpdateIngredientRequest) marks all 18 keys "required": present, though each
// may be null. nutrientsJSON fills in the ones a test does not care about as
// null so a partial nutrient set can still be sent as a contract-valid body.
var allNutrientKeys = []string{
	"calories", "protein", "carbohydrates", "sugar", "fibre", "fat", "saturated_fat",
	"sodium", "potassium", "calcium", "iron", "magnesium", "zinc",
	"vitamin_a", "vitamin_c", "vitamin_d", "vitamin_b12", "folate",
}

// nutrientsJSON renders a complete NutrientAmounts JSON object: the given
// values, nulls for every other key.
func nutrientsJSON(values map[string]float64) string {
	parts := make([]string, len(allNutrientKeys))
	for i, key := range allNutrientKeys {
		if v, ok := values[key]; ok {
			parts[i] = fmt.Sprintf("%q:%v", key, v)
		} else {
			parts[i] = fmt.Sprintf("%q:null", key)
		}
	}
	return "{" + strings.Join(parts, ",") + "}"
}

// stubTwoUserTokens accepts two fixed tokens, each mapped to a user actually
// created in Postgres by newIngredientsRouter (unlike the package-level
// stubTokens, which uses fixed UUIDs that do not exist in the database).
type stubTwoUserTokens struct{ u1, u2 uuid.UUID }

func (s stubTwoUserTokens) ParseAccess(token string) (uuid.UUID, error) {
	switch token {
	case "user1-token":
		return s.u1, nil
	case "user2-token":
		return s.u2, nil
	}
	return uuid.Nil, auth.ErrInvalidAccessToken
}

func newIngredientsRouter(t *testing.T) (router http.Handler, token1, token2 string) {
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
		d.Tokens = stubTwoUserTokens{u1: u1.ID, u2: u2.ID}
		d.Limits = httpapi.RateLimits{AuthPerMinute: 1000, UserPerMinute: 1000}
	})
	return router, "user1-token", "user2-token"
}

func TestIngredientsLifecycle(t *testing.T) {
	router, token1, token2 := newIngredientsRouter(t)

	// Create a custom ingredient as user 1.
	rec := contract(t, router, http.MethodPost, "/ingredients", withBearer(token1), withBody(`{
		"name": "Custom Oats", "category": "grains_bread",
		"nutrients": `+nutrientsJSON(map[string]float64{"calories": 389, "protein": 17})+`
	}`))
	if rec.Code != http.StatusCreated {
		t.Fatalf("create: status = %d, body = %s", rec.Code, rec.Body.String())
	}
	created := decodeAs[api.Ingredient](t, rec)
	if !created.IsCustom || created.Name != "Custom Oats" {
		t.Errorf("created = %+v", created)
	}
	if !created.Nutrients.Calories.IsSpecified() || created.Nutrients.Calories.IsNull() {
		t.Errorf("Nutrients.Calories = %+v, want 389", created.Nutrients.Calories)
	}

	// User 1 sees it in a search.
	rec = contract(t, router, http.MethodGet, "/ingredients?q=oats", withBearer(token1))
	if rec.Code != http.StatusOK {
		t.Fatalf("search: status = %d", rec.Code)
	}
	if list := decodeAs[api.IngredientList](t, rec); len(list.Items) != 1 || list.Items[0].Id != created.Id {
		t.Errorf("search results = %+v", list)
	}

	// User 2 does not see it (owned by someone else) and gets 404 touching it.
	rec = contract(t, router, http.MethodGet, "/ingredients?q=oats", withBearer(token2))
	if list := decodeAs[api.IngredientList](t, rec); len(list.Items) != 0 {
		t.Errorf("user 2 search results = %+v, want none", list)
	}
	rec = contract(t, router, http.MethodPatch, "/ingredients/"+created.Id.String(), withBearer(token2), withBody(`{"name":"Hijack"}`))
	if rec.Code != http.StatusNotFound {
		t.Errorf("user 2 PATCH: status = %d, want 404", rec.Code)
	}

	// User 1 updates it: the request replaces the full nutrient set.
	rec = contract(t, router, http.MethodPatch, "/ingredients/"+created.Id.String(), withBearer(token1), withBody(`{
		"nutrients": `+nutrientsJSON(map[string]float64{"fibre": 10})+`
	}`))
	if rec.Code != http.StatusOK {
		t.Fatalf("update: status = %d, body = %s", rec.Code, rec.Body.String())
	}
	updated := decodeAs[api.Ingredient](t, rec)
	if updated.Nutrients.Calories.IsSpecified() && !updated.Nutrients.Calories.IsNull() {
		t.Errorf("Nutrients.Calories = %+v, want cleared by the full replace", updated.Nutrients.Calories)
	}
	if !updated.Nutrients.Fibre.IsSpecified() || updated.Nutrients.Fibre.IsNull() {
		t.Errorf("Nutrients.Fibre = %+v, want 10", updated.Nutrients.Fibre)
	}

	// An invalid category is a validation error, not a 500.
	rec = contract(t, router, http.MethodPost, "/ingredients", withBearer(token1), withInvalidRequest(),
		withBody(`{"name":"Bad","category":"not_a_category"}`))
	if rec.Code != http.StatusBadRequest {
		t.Errorf("invalid category: status = %d, want 400", rec.Code)
	}

	// User 2 cannot delete it; user 1 can.
	rec = contract(t, router, http.MethodDelete, "/ingredients/"+created.Id.String(), withBearer(token2))
	if rec.Code != http.StatusNotFound {
		t.Errorf("user 2 DELETE: status = %d, want 404", rec.Code)
	}
	rec = contract(t, router, http.MethodDelete, "/ingredients/"+created.Id.String(), withBearer(token1))
	if rec.Code != http.StatusNoContent {
		t.Errorf("owner DELETE: status = %d, want 204", rec.Code)
	}
}

func TestIngredientsListPaginatesThroughTheContract(t *testing.T) {
	router, token1, _ := newIngredientsRouter(t)
	for _, name := range []string{"Apple", "Banana", "Carrot"} {
		rec := contract(t, router, http.MethodPost, "/ingredients", withBearer(token1),
			withBody(`{"name":"`+name+`","category":"produce"}`))
		if rec.Code != http.StatusCreated {
			t.Fatalf("create %s: status = %d", name, rec.Code)
		}
	}

	var names []string
	path := "/ingredients?limit=1"
	for {
		rec := contract(t, router, http.MethodGet, path, withBearer(token1))
		if rec.Code != http.StatusOK {
			t.Fatalf("list %s: status = %d", path, rec.Code)
		}
		list := decodeAs[api.IngredientList](t, rec)
		for _, ing := range list.Items {
			names = append(names, ing.Name)
		}
		if !list.NextCursor.IsSpecified() || list.NextCursor.IsNull() {
			break
		}
		path = "/ingredients?limit=1&cursor=" + list.NextCursor.MustGet()
		if len(names) > 10 {
			t.Fatal("pagination did not terminate")
		}
	}
	if want := []string{"Apple", "Banana", "Carrot"}; len(names) != 3 || names[0] != want[0] || names[1] != want[1] || names[2] != want[2] {
		t.Errorf("paginated names = %v, want %v", names, want)
	}
}
