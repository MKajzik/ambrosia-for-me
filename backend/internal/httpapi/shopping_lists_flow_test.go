package httpapi_test

import (
	"bufio"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

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

// TestShoppingListEventsThroughTheContract checks the events route's
// declared responses: 401 and 404 like any other secured route, and a 200
// text/event-stream body that ends with list_deleted when the list goes.
func TestShoppingListEventsThroughTheContract(t *testing.T) {
	env := newShoppingListsRouter(t)
	list, err := env.lists.Create(context.Background(), env.user1, service.CreateShoppingListInput{Name: "Groceries"})
	if err != nil {
		t.Fatalf("create list: %v", err)
	}
	eventsPath := "/shopping-lists/" + list.ID.String() + "/events"

	rec := contract(t, env.router, http.MethodGet, eventsPath)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("no token: status = %d, want 401", rec.Code)
	}
	rec = contract(t, env.router, http.MethodGet, eventsPath, withBearer("user2-token"))
	if rec.Code != http.StatusNotFound {
		t.Errorf("another user's list: status = %d, want 404", rec.Code)
	}

	// contract() serves the request synchronously, so end the stream from
	// the side: once the handler has subscribed, delete the list.
	go func() {
		deadline := time.Now().Add(5 * time.Second)
		for env.events.Subscribers(list.ID) == 0 && time.Now().Before(deadline) {
			time.Sleep(5 * time.Millisecond)
		}
		if err := env.lists.Delete(context.Background(), env.user1, list.ID); err != nil {
			t.Errorf("delete list: %v", err)
		}
	}()
	rec = contract(t, env.router, http.MethodGet, eventsPath, withBearer("user1-token"))
	if rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != "text/event-stream" {
		t.Fatalf("stream: status = %d, content type %q, want 200 text/event-stream", rec.Code, rec.Header().Get("Content-Type"))
	}
	want := ": connected\n\nevent: list_deleted\ndata: {\"type\":\"list_deleted\",\"list_id\":\"" + list.ID.String() + "\"}\n\n"
	if rec.Body.String() != want {
		t.Errorf("stream body = %q, want %q", rec.Body.String(), want)
	}
}

// TestShoppingListEventsOutliveTheServerWriteTimeout runs the router on a
// real server whose WriteTimeout is far shorter than the wait before the
// event, the way cmd/api's 30s WriteTimeout would otherwise cut every stream.
func TestShoppingListEventsOutliveTheServerWriteTimeout(t *testing.T) {
	env := newShoppingListsRouter(t)
	ctx := context.Background()
	list, err := env.lists.Create(ctx, env.user1, service.CreateShoppingListInput{Name: "Groceries"})
	if err != nil {
		t.Fatalf("create list: %v", err)
	}

	srv := httptest.NewUnstartedServer(env.router)
	srv.Config.WriteTimeout = 200 * time.Millisecond
	srv.Start()
	defer srv.Close()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL+"/v1/shopping-lists/"+list.ID.String()+"/events", nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer user1-token")
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatalf("open stream: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	lines := make(chan string)
	go func() {
		defer close(lines)
		sc := bufio.NewScanner(resp.Body)
		for sc.Scan() {
			lines <- sc.Text()
		}
	}()
	next := func() string {
		t.Helper()
		for {
			select {
			case l, ok := <-lines:
				if !ok {
					t.Fatal("stream ended early")
				}
				if l != "" {
					return l
				}
			case <-time.After(3 * time.Second):
				t.Fatal("no line within 3s")
			}
		}
	}

	if l := next(); l != ": connected" {
		t.Fatalf("first line = %q, want \": connected\"", l)
	}
	time.Sleep(600 * time.Millisecond) // three times the server's WriteTimeout

	item, err := env.lists.AddItem(ctx, env.user1, list.ID, service.CreateShoppingItemInput{Name: "Bread"})
	if err != nil {
		t.Fatalf("AddItem: %v", err)
	}
	if l := next(); l != "event: item_changed" {
		t.Fatalf("event line = %q, want \"event: item_changed\"", l)
	}
	var data struct {
		Type    string    `json:"type"`
		ListID  uuid.UUID `json:"list_id"`
		ItemID  uuid.UUID `json:"item_id"`
		Version int       `json:"version"`
	}
	if err := json.Unmarshal([]byte(strings.TrimPrefix(next(), "data: ")), &data); err != nil {
		t.Fatalf("decode data: %v", err)
	}
	if data.Type != "item_changed" || data.ListID != list.ID || data.ItemID != item.ID || data.Version != 1 {
		t.Errorf("data = %+v, want item_changed for the new item at version 1", data)
	}

	if err := env.lists.Delete(ctx, env.user1, list.ID); err != nil {
		t.Fatalf("delete list: %v", err)
	}
	if l := next(); l != "event: list_deleted" {
		t.Errorf("event line = %q, want \"event: list_deleted\"", l)
	}
}
