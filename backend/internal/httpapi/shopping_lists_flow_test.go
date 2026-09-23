package httpapi_test

import (
	"context"
	"net/http"
	"testing"

	"github.com/google/uuid"

	"github.com/InzKazik/mealplanner/backend/internal/api"
	"github.com/InzKazik/mealplanner/backend/internal/db"
	"github.com/InzKazik/mealplanner/backend/internal/httpapi"
	"github.com/InzKazik/mealplanner/backend/internal/service"
	"github.com/InzKazik/mealplanner/backend/internal/store"
	"github.com/InzKazik/mealplanner/backend/internal/store/sqlc"
	"github.com/InzKazik/mealplanner/backend/internal/testutil"
)

// shoppingEnv is a router over the real services and Postgres, plus direct
// handles on the shopping lists service and its event hub for tests that
// need to act while a stream is open.
type shoppingEnv struct {
	router http.Handler
	lists  *service.ShoppingLists
	events *service.ListEventHub
	user1  uuid.UUID
}

func newShoppingListsRouter(t *testing.T) shoppingEnv {
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
	events := service.NewListEventHub()
	lists := service.NewShoppingLists(st, events)
	router := newTestRouter(t, func(d *httpapi.Deps) {
		d.Ingredients = service.NewIngredients(st)
		d.Meals = meals
		d.Plan = service.NewPlan(st, meals)
		d.ShoppingLists = lists
		d.Tokens = stubTwoUserTokens{u1: u1.ID, u2: u2.ID}
		d.Limits = httpapi.RateLimits{AuthPerMinute: 1000, UserPerMinute: 1000}
	})
	return shoppingEnv{router: router, lists: lists, events: events, user1: u1.ID}
}

func TestShoppingListsLifecycle(t *testing.T) {
	env := newShoppingListsRouter(t)
	router, token1, token2 := env.router, "user1-token", "user2-token"

	rec := contract(t, router, http.MethodPost, "/ingredients", withBearer(token1),
		withBody(`{"name":"Oats","category":"grains_bread","nutrients":{"calories":389}}`))
	if rec.Code != http.StatusCreated {
		t.Fatalf("create ingredient: status = %d, body = %s", rec.Code, rec.Body.String())
	}
	oats := decodeAs[api.Ingredient](t, rec)
	rec = contract(t, router, http.MethodPost, "/meals", withBearer(token1), withBody(`{"name":"Porridge","servings":1}`))
	if rec.Code != http.StatusCreated {
		t.Fatalf("create meal: status = %d, body = %s", rec.Code, rec.Body.String())
	}
	meal := decodeAs[api.Meal](t, rec)
	rec = contract(t, router, http.MethodPut, "/meals/"+meal.Id.String()+"/ingredients", withBearer(token1),
		withBody(`{"items":[{"ingredient_id":"`+oats.Id.String()+`","quantity":80,"unit":"g"}]}`))
	if rec.Code != http.StatusOK {
		t.Fatalf("replace meal ingredients: status = %d, body = %s", rec.Code, rec.Body.String())
	}
	for _, date := range []string{"2026-06-01", "2026-06-02"} {
		rec = contract(t, router, http.MethodPut, "/plan/"+date+"/breakfast", withBearer(token1), withBody(`{"meal_id":"`+meal.Id.String()+`"}`))
		if rec.Code != http.StatusOK {
			t.Fatalf("set plan entry %s: status = %d, body = %s", date, rec.Code, rec.Body.String())
		}
	}

	rec = contract(t, router, http.MethodPost, "/shopping-lists/generate", withBearer(token1),
		withBody(`{"from":"2026-06-01","to":"2026-06-02","name":"Week 23"}`))
	if rec.Code != http.StatusCreated {
		t.Fatalf("generate: status = %d, body = %s", rec.Code, rec.Body.String())
	}
	list := decodeAs[api.ShoppingList](t, rec)
	if len(list.Items) != 1 || list.Items[0].Quantity.MustGet() != 160 || list.Items[0].Origin != api.ShoppingItemOriginGenerated {
		t.Fatalf("generated items = %+v, want one generated oats line of 160 g", list.Items)
	}
	listPath := "/shopping-lists/" + list.Id.String()

	rec = contract(t, router, http.MethodGet, listPath, withBearer(token2))
	if rec.Code != http.StatusNotFound {
		t.Errorf("get another user's list: status = %d, want 404", rec.Code)
	}

	rec = contract(t, router, http.MethodPost, listPath+"/items", withBearer(token1), withBody(`{"name":"Coffee","quantity":1,"unit":"piece","category":"beverages"}`))
	if rec.Code != http.StatusCreated {
		t.Fatalf("add item: status = %d, body = %s", rec.Code, rec.Body.String())
	}
	coffee := decodeAs[api.ShoppingItem](t, rec)
	itemPath := listPath + "/items/" + coffee.Id.String()

	rec = contract(t, router, http.MethodPatch, itemPath, withBearer(token1), withBody(`{"checked":true}`))
	if rec.Code != http.StatusOK {
		t.Fatalf("check: status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if checked := decodeAs[api.ShoppingItem](t, rec); !checked.Checked || checked.Version != 2 {
		t.Errorf("after check = %+v, want checked at version 2", checked)
	}

	rec = contract(t, router, http.MethodPatch, itemPath, withBearer(token1), withBody(`{"version":1,"name":"Decaf"}`))
	if rec.Code != http.StatusConflict {
		t.Fatalf("stale edit: status = %d, want 409, body = %s", rec.Code, rec.Body.String())
	}
	conflict := decodeAs[api.ShoppingItemConflict](t, rec)
	if conflict.Code != "version_conflict" || conflict.Current.Version != 2 || conflict.Current.Name != "Coffee" {
		t.Errorf("conflict = %+v, want version_conflict carrying the item at version 2", conflict)
	}

	rec = contract(t, router, http.MethodPatch, itemPath, withBearer(token1), withBody(`{"name":"Decaf"}`))
	if rec.Code != http.StatusBadRequest || problemCode(t, rec) != "version_required" {
		t.Errorf("edit without version: status = %d, body = %s, want 400 version_required", rec.Code, rec.Body.String())
	}

	rec = contract(t, router, http.MethodPatch, itemPath, withBearer(token1), withBody(`{"version":2,"name":"Decaf","quantity":null,"unit":null}`))
	if rec.Code != http.StatusOK {
		t.Fatalf("edit at the current version: status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if edited := decodeAs[api.ShoppingItem](t, rec); edited.Name != "Decaf" || !edited.Quantity.IsNull() || !edited.Unit.IsNull() || edited.Version != 3 {
		t.Errorf("after edit = %+v, want Decaf with quantity and unit cleared at version 3", edited)
	}

	rec = contract(t, router, http.MethodPost, "/shopping-lists/generate", withBearer(token1),
		withBody(`{"from":"2026-06-01","to":"2026-06-01","list_id":"`+list.Id.String()+`"}`))
	if rec.Code != http.StatusOK {
		t.Fatalf("regenerate: status = %d, body = %s", rec.Code, rec.Body.String())
	}
	regenerated := decodeAs[api.ShoppingList](t, rec)
	if len(regenerated.Items) != 2 || regenerated.Items[0].Id != coffee.Id || regenerated.Items[1].Quantity.MustGet() != 80 {
		t.Errorf("regenerated items = %+v, want the manual item kept and one 80 g oats line", regenerated.Items)
	}

	rec = contract(t, router, http.MethodDelete, itemPath, withBearer(token1))
	if rec.Code != http.StatusNoContent {
		t.Errorf("delete item: status = %d, want 204", rec.Code)
	}
	rec = contract(t, router, http.MethodDelete, itemPath, withBearer(token1))
	if rec.Code != http.StatusNotFound {
		t.Errorf("delete item again: status = %d, want 404", rec.Code)
	}

	rec = contract(t, router, http.MethodPatch, listPath, withBearer(token1), withBody(`{"name":"Week 23 (final)"}`))
	if rec.Code != http.StatusOK || decodeAs[api.ShoppingList](t, rec).Name != "Week 23 (final)" {
		t.Errorf("rename: status = %d, body = %s", rec.Code, rec.Body.String())
	}

	rec = contract(t, router, http.MethodPost, "/shopping-lists", withBearer(token1), withBody(`{"name":"Party"}`))
	if rec.Code != http.StatusCreated {
		t.Fatalf("create list: status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var names []string
	path := "/shopping-lists?limit=1"
	for range 5 {
		rec = contract(t, router, http.MethodGet, path, withBearer(token1))
		if rec.Code != http.StatusOK {
			t.Fatalf("list %s: status = %d", path, rec.Code)
		}
		page := decodeAs[api.ShoppingListPage](t, rec)
		for _, l := range page.Items {
			names = append(names, l.Name)
		}
		if page.NextCursor.IsNull() {
			break
		}
		path = "/shopping-lists?limit=1&cursor=" + page.NextCursor.MustGet()
	}
	if len(names) != 2 || names[0] != "Party" || names[1] != "Week 23 (final)" {
		t.Errorf("paginated names = %v, want [Party, Week 23 (final)] (newest first)", names)
	}

	rec = contract(t, router, http.MethodDelete, listPath, withBearer(token1))
	if rec.Code != http.StatusNoContent {
		t.Errorf("delete list: status = %d, want 204", rec.Code)
	}
	rec = contract(t, router, http.MethodGet, listPath, withBearer(token1))
	if rec.Code != http.StatusNotFound {
		t.Errorf("get deleted list: status = %d, want 404", rec.Code)
	}
}
