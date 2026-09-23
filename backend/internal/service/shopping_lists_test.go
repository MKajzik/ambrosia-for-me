package service_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/InzKazik/mealplanner/backend/internal/service"
	"github.com/InzKazik/mealplanner/backend/internal/store"
)

type shoppingFixture struct {
	lists  *service.ShoppingLists
	events *service.ListEventHub
	meals  *service.Meals
	ing    *service.Ingredients
	plan   *service.Plan
	st     *store.Store
}

func newShoppingListsFixture(t *testing.T) shoppingFixture {
	t.Helper()
	ing, st := newIngredientsFixture(t)
	meals := service.NewMeals(st)
	events := service.NewListEventHub()
	return shoppingFixture{
		lists: service.NewShoppingLists(st, events), events: events,
		meals: meals, ing: ing, plan: service.NewPlan(st, meals), st: st,
	}
}

// nextEvent waits briefly for the subscription's next event.
func nextEvent(t *testing.T, sub *service.ListSubscription) service.ListEvent {
	t.Helper()
	select {
	case ev, open := <-sub.Events():
		if !open {
			t.Fatal("subscription closed, want an event")
		}
		return ev
	case <-time.After(2 * time.Second):
		t.Fatal("no event within 2s")
	}
	return service.ListEvent{}
}

func TestShoppingListsUpdateItemEnforcesVersionsForEditsButNotForChecks(t *testing.T) {
	f := newShoppingListsFixture(t)
	ctx := context.Background()
	owner := newTestUser(t, f.st, "shopper3@example.com")
	other := newTestUser(t, f.st, "other3@example.com")

	list, err := f.lists.Create(ctx, owner, service.CreateShoppingListInput{Name: "Groceries"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	item, err := f.lists.AddItem(ctx, owner, list.ID, service.CreateShoppingItemInput{Name: "Milk"})
	if err != nil {
		t.Fatalf("AddItem: %v", err)
	}
	if item.Version != 1 || item.Category != "other" || item.Origin != "manual" {
		t.Fatalf("new item = %+v, want version 1, category other, origin manual", item)
	}

	item, err = f.lists.UpdateItem(ctx, owner, list.ID, item.ID, service.UpdateShoppingItemInput{Version: ptr(1), Name: ptr("Oat milk")})
	if err != nil {
		t.Fatalf("rename at the current version: %v", err)
	}
	if item.Version != 2 || item.Name != "Oat milk" {
		t.Fatalf("after rename = %+v, want version 2 named Oat milk", item)
	}

	_, err = f.lists.UpdateItem(ctx, owner, list.ID, item.ID, service.UpdateShoppingItemInput{
		Version: ptr(1), Quantity: service.Set(ptr(2.0)),
	})
	var conflict *service.ShoppingItemVersionConflictError
	if !errors.As(err, &conflict) {
		t.Fatalf("edit at a stale version: err = %v, want a version conflict", err)
	}
	if conflict.Current.Version != 2 || conflict.Current.Name != "Oat milk" {
		t.Errorf("conflict.Current = %+v, want the item at version 2", conflict.Current)
	}

	if _, err := f.lists.UpdateItem(ctx, owner, list.ID, item.ID, service.UpdateShoppingItemInput{Name: ptr("Soy milk")}); !errors.Is(err, service.ErrShoppingItemVersionRequired) {
		t.Errorf("edit without a version: err = %v, want ErrShoppingItemVersionRequired", err)
	}

	item, err = f.lists.UpdateItem(ctx, owner, list.ID, item.ID, service.UpdateShoppingItemInput{Checked: ptr(true)})
	if err != nil {
		t.Fatalf("check without a version: %v", err)
	}
	if !item.Checked || item.Version != 3 || item.CheckedBy == nil || *item.CheckedBy != owner {
		t.Errorf("after check = %+v, want checked by the owner at version 3", item)
	}

	// A replay of the same check, carrying a version that is stale by now,
	// is neither a conflict nor a change.
	replayed, err := f.lists.UpdateItem(ctx, owner, list.ID, item.ID, service.UpdateShoppingItemInput{Version: ptr(2), Checked: ptr(true)})
	if err != nil {
		t.Fatalf("replayed check: %v", err)
	}
	if replayed.Version != 3 {
		t.Errorf("version after a replayed check = %d, want 3 (no-op)", replayed.Version)
	}

	item, err = f.lists.UpdateItem(ctx, owner, list.ID, item.ID, service.UpdateShoppingItemInput{Checked: ptr(false)})
	if err != nil {
		t.Fatalf("uncheck: %v", err)
	}
	if item.Checked || item.CheckedBy != nil || item.Version != 4 {
		t.Errorf("after uncheck = %+v, want unchecked, checked_by nil, version 4", item)
	}

	if _, err := f.lists.UpdateItem(ctx, other, list.ID, item.ID, service.UpdateShoppingItemInput{Checked: ptr(true)}); !errors.Is(err, service.ErrShoppingItemNotFound) {
		t.Errorf("another user's check: err = %v, want ErrShoppingItemNotFound", err)
	}
}

func TestShoppingListsItemChangesReachTheListsEventStream(t *testing.T) {
	f := newShoppingListsFixture(t)
	ctx := context.Background()
	owner := newTestUser(t, f.st, "shopper4@example.com")
	other := newTestUser(t, f.st, "other4@example.com")

	list, err := f.lists.Create(ctx, owner, service.CreateShoppingListInput{Name: "Groceries"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, err := f.lists.Subscribe(ctx, other, list.ID); !errors.Is(err, service.ErrShoppingListNotFound) {
		t.Errorf("another user's Subscribe: err = %v, want ErrShoppingListNotFound", err)
	}
	if n := f.events.Subscribers(list.ID); n != 0 {
		t.Errorf("Subscribers after a refused Subscribe = %d, want 0", n)
	}
	sub, err := f.lists.Subscribe(ctx, owner, list.ID)
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	defer sub.Close()

	item, err := f.lists.AddItem(ctx, owner, list.ID, service.CreateShoppingItemInput{Name: "Bread"})
	if err != nil {
		t.Fatalf("AddItem: %v", err)
	}
	if ev := nextEvent(t, sub); ev.Type != service.ListEventItemChanged || *ev.ItemID != item.ID || *ev.Version != 1 {
		t.Errorf("after add: event = %+v, want item_changed at version 1", ev)
	}
	if _, err := f.lists.UpdateItem(ctx, owner, list.ID, item.ID, service.UpdateShoppingItemInput{Checked: ptr(true)}); err != nil {
		t.Fatalf("check: %v", err)
	}
	if ev := nextEvent(t, sub); ev.Type != service.ListEventItemChanged || *ev.Version != 2 {
		t.Errorf("after check: event = %+v, want item_changed at version 2", ev)
	}
	// A no-op check publishes nothing: the next event must be the delete.
	if _, err := f.lists.UpdateItem(ctx, owner, list.ID, item.ID, service.UpdateShoppingItemInput{Checked: ptr(true)}); err != nil {
		t.Fatalf("no-op check: %v", err)
	}
	if err := f.lists.DeleteItem(ctx, owner, list.ID, item.ID); err != nil {
		t.Fatalf("DeleteItem: %v", err)
	}
	if ev := nextEvent(t, sub); ev.Type != service.ListEventItemDeleted || *ev.ItemID != item.ID || *ev.Version != 2 {
		t.Errorf("after delete: event = %+v, want item_deleted at version 2", ev)
	}
	if err := f.lists.Delete(ctx, owner, list.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if ev := nextEvent(t, sub); ev.Type != service.ListEventListDeleted {
		t.Errorf("after list delete: event = %+v, want list_deleted", ev)
	}
	if _, open := <-sub.Events(); open {
		t.Error("stream still open after list_deleted, want closed")
	}
}

func TestShoppingListsDeletingAnIngredientKeepsItsItems(t *testing.T) {
	f := newShoppingListsFixture(t)
	ctx := context.Background()
	owner := newTestUser(t, f.st, "shopper5@example.com")
	other := newTestUser(t, f.st, "other5@example.com")

	tofu := mustCreateIngredient(t, f.ing, owner, service.CreateIngredientInput{Name: "Tofu", Category: "legumes_nuts_seeds"})
	list, err := f.lists.Create(ctx, owner, service.CreateShoppingListInput{Name: "Groceries"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	item, err := f.lists.AddItem(ctx, owner, list.ID, service.CreateShoppingItemInput{
		IngredientID: &tofu.ID, Name: "Tofu", Quantity: ptr(400.0), Unit: ptr("g"),
	})
	if err != nil {
		t.Fatalf("AddItem: %v", err)
	}
	if item.Category != "legumes_nuts_seeds" {
		t.Errorf("category = %q, want the ingredient's category", item.Category)
	}

	othersIngredient := mustCreateIngredient(t, f.ing, other, service.CreateIngredientInput{Name: "Secret", Category: "other"})
	if _, err := f.lists.AddItem(ctx, owner, list.ID, service.CreateShoppingItemInput{IngredientID: &othersIngredient.ID, Name: "Secret"}); !errors.Is(err, service.ErrShoppingItemIngredientNotFound) {
		t.Errorf("AddItem with another user's ingredient: err = %v, want ErrShoppingItemIngredientNotFound", err)
	}

	// Unlike a meal line, a shopping item never blocks deleting its ingredient.
	if err := f.ing.Delete(ctx, owner, tofu.ID); err != nil {
		t.Fatalf("delete an ingredient a shopping item references: %v", err)
	}
	got, err := f.lists.Get(ctx, owner, list.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if len(got.Items) != 1 || got.Items[0].IngredientID != nil || got.Items[0].Name != "Tofu" || *got.Items[0].Quantity != 400 {
		t.Errorf("items after the ingredient was deleted = %+v, want the same item with ingredient_id nil", got.Items)
	}
}

func TestShoppingListsAreVisibleToTheirOwnerOnly(t *testing.T) {
	f := newShoppingListsFixture(t)
	ctx := context.Background()
	owner := newTestUser(t, f.st, "shopper6@example.com")
	other := newTestUser(t, f.st, "other6@example.com")

	list, err := f.lists.Create(ctx, owner, service.CreateShoppingListInput{Name: "Mine", SharedWithPartner: true})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, err := f.lists.Get(ctx, other, list.ID); !errors.Is(err, service.ErrShoppingListNotFound) {
		t.Errorf("Get by another user (even with shared_with_partner): err = %v, want ErrShoppingListNotFound", err)
	}
	if _, err := f.lists.AddItem(ctx, other, list.ID, service.CreateShoppingItemInput{Name: "Sneaky"}); !errors.Is(err, service.ErrShoppingListNotFound) {
		t.Errorf("AddItem by another user: err = %v, want ErrShoppingListNotFound", err)
	}
	if _, err := f.lists.Update(ctx, other, list.ID, service.UpdateShoppingListInput{Name: ptr("Mine now")}); !errors.Is(err, service.ErrShoppingListNotFound) {
		t.Errorf("Update by another user: err = %v, want ErrShoppingListNotFound", err)
	}
	if err := f.lists.Delete(ctx, other, list.ID); !errors.Is(err, service.ErrShoppingListNotFound) {
		t.Errorf("Delete by another user: err = %v, want ErrShoppingListNotFound", err)
	}
	page, err := f.lists.List(ctx, other, service.ListShoppingListsInput{Limit: 10})
	if err != nil || len(page.Items) != 0 {
		t.Errorf("List by another user = %+v (err %v), want empty", page.Items, err)
	}
}
