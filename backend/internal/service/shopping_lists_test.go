package service_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/InzKazik/mealplanner/backend/internal/service"
	"github.com/InzKazik/mealplanner/backend/internal/store"
	"github.com/InzKazik/mealplanner/backend/internal/store/sqlc"
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

func mustSetEntry(t *testing.T, plan *service.Plan, owner uuid.UUID, date time.Time, slot string, mealID uuid.UUID, portion float64) {
	t.Helper()
	if _, err := plan.SetEntry(context.Background(), owner, date, slot, service.SetPlanEntryInput{MealID: mealID, Portion: portion}); err != nil {
		t.Fatalf("SetEntry %s %s: %v", date.Format(time.DateOnly), slot, err)
	}
}

func deref[T any](p *T) any {
	if p == nil {
		return nil
	}
	return *p
}

// TestShoppingListsGenerateSumsMergesAndGroupsByCategory is the shopping-list
// merging test spec §6 calls for.
func TestShoppingListsGenerateSumsMergesAndGroupsByCategory(t *testing.T) {
	f := newShoppingListsFixture(t)
	ctx := context.Background()
	owner := newTestUser(t, f.st, "shopper1@example.com")

	rice := mustCreateIngredient(t, f.ing, owner, service.CreateIngredientInput{Name: "Rice", Category: "grains_bread"})
	egg := mustCreateIngredient(t, f.ing, owner, service.CreateIngredientInput{Name: "Egg", Category: "dairy_eggs", GramsPerPiece: ptr(50.0)})
	oil := mustCreateIngredient(t, f.ing, owner, service.CreateIngredientInput{Name: "Olive Oil", Category: "condiments_oils", DensityGPerMl: ptr(0.92)})
	flour := mustCreateIngredient(t, f.ing, owner, service.CreateIngredientInput{Name: "Flour", Category: "grains_bread"})

	bowl, err := f.meals.Create(ctx, owner, service.CreateMealInput{Name: "Rice Bowl", Servings: 2})
	if err != nil {
		t.Fatalf("create meal: %v", err)
	}
	if _, err := f.meals.ReplaceIngredients(ctx, owner, bowl.ID, []service.MealIngredientInput{
		{IngredientID: rice.ID, Quantity: 200, Unit: "g"},
		{IngredientID: oil.ID, Quantity: 10, Unit: "ml"},
		{IngredientID: egg.ID, Quantity: 2, Unit: "piece"},
	}); err != nil {
		t.Fatalf("ReplaceIngredients (bowl): %v", err)
	}
	pancakes, err := f.meals.Create(ctx, owner, service.CreateMealInput{Name: "Pancakes", Servings: 1})
	if err != nil {
		t.Fatalf("create meal: %v", err)
	}
	if _, err := f.meals.ReplaceIngredients(ctx, owner, pancakes.ID, []service.MealIngredientInput{
		{IngredientID: flour.ID, Quantity: 100, Unit: "g"},
		{IngredientID: oil.ID, Quantity: 5, Unit: "g"},
	}); err != nil {
		t.Fatalf("ReplaceIngredients (pancakes): %v", err)
	}
	// Flour has no grams_per_piece, so Meals.ReplaceIngredients would refuse
	// a "piece" line. Insert one directly: it stands for the narrow, accepted
	// race backend/CLAUDE.md documents (an ingredient edit interleaving a
	// meal write), and generation must still never merge it into grams.
	if _, err := f.st.InsertMealIngredient(ctx, sqlc.InsertMealIngredientParams{
		MealID: pancakes.ID, IngredientID: flour.ID, Quantity: 1, Unit: "piece", Position: 2,
	}); err != nil {
		t.Fatalf("insert unconvertible line: %v", err)
	}

	day1 := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	day2 := day1.AddDate(0, 0, 1)
	mustSetEntry(t, f.plan, owner, day1, "breakfast", bowl.ID, 1)
	mustSetEntry(t, f.plan, owner, day2, "breakfast", bowl.ID, 1)
	mustSetEntry(t, f.plan, owner, day2, "lunch", pancakes.ID, 2)
	// Outside the range: must not be counted.
	mustSetEntry(t, f.plan, owner, day2.AddDate(0, 0, 1), "dinner", pancakes.ID, 1)

	list, created, err := f.lists.Generate(ctx, owner, service.GenerateShoppingListInput{From: day1, To: day2})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if !created {
		t.Error("created = false, want true (no list_id)")
	}
	if list.SourceFrom == nil || !list.SourceFrom.Equal(day1) || list.SourceTo == nil || !list.SourceTo.Equal(day2) {
		t.Errorf("source range = %v..%v, want %v..%v", list.SourceFrom, list.SourceTo, day1, day2)
	}

	// Bowl (2 servings) twice at portion 1: half the recipe each time, so
	// the whole recipe once: rice 200 g, oil 10 ml, egg 2 piece.
	// Pancakes (1 serving) once at portion 2: flour 200 g + 2 piece, oil 10 g.
	// Egg: one unit only, kept as piece. Oil: ml and g mixed, both
	// convertible, merged into grams: 10 ml * 0.92 + 10 g = 19.2 g. Flour: g
	// and an unconvertible piece, two lines. Ordered by category, name, unit.
	want := []struct {
		name, category, unit string
		quantity             float64
	}{
		{"Olive Oil", "condiments_oils", "g", 19.2},
		{"Egg", "dairy_eggs", "piece", 2},
		{"Flour", "grains_bread", "g", 200},
		{"Flour", "grains_bread", "piece", 2},
		{"Rice", "grains_bread", "g", 200},
	}
	if len(list.Items) != len(want) {
		t.Fatalf("items = %+v, want %d lines", list.Items, len(want))
	}
	for i, w := range want {
		got := list.Items[i]
		if got.Name != w.name || got.Category != w.category || got.Unit == nil || *got.Unit != w.unit ||
			got.Quantity == nil || !almostEqual(*got.Quantity, w.quantity) {
			t.Errorf("item %d = %s %s %v %v, want %s %s %v %s", i, got.Name, got.Category, deref(got.Quantity), deref(got.Unit), w.name, w.category, w.quantity, w.unit)
		}
		if got.Origin != "generated" || got.IngredientID == nil || got.Position != i || got.Version != 1 || got.Checked {
			t.Errorf("item %d = %+v, want a fresh generated item at position %d with its ingredient id", i, got, i)
		}
	}
}

func TestShoppingListsRegenerateReplacesGeneratedItemsAndKeepsManualOnes(t *testing.T) {
	f := newShoppingListsFixture(t)
	ctx := context.Background()
	owner := newTestUser(t, f.st, "shopper2@example.com")

	rice := mustCreateIngredient(t, f.ing, owner, service.CreateIngredientInput{Name: "Rice", Category: "grains_bread"})
	meal, err := f.meals.Create(ctx, owner, service.CreateMealInput{Name: "Rice", Servings: 1})
	if err != nil {
		t.Fatalf("create meal: %v", err)
	}
	if _, err := f.meals.ReplaceIngredients(ctx, owner, meal.ID, []service.MealIngredientInput{{IngredientID: rice.ID, Quantity: 100, Unit: "g"}}); err != nil {
		t.Fatalf("ReplaceIngredients: %v", err)
	}
	day := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	mustSetEntry(t, f.plan, owner, day, "lunch", meal.ID, 1)

	list, _, err := f.lists.Generate(ctx, owner, service.GenerateShoppingListInput{From: day, To: day, Name: ptr("Week 23")})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if list.Name != "Week 23" || len(list.Items) != 1 {
		t.Fatalf("list = %+v, want Week 23 with one generated item", list)
	}
	oldGenerated := list.Items[0]
	manual, err := f.lists.AddItem(ctx, owner, list.ID, service.CreateShoppingItemInput{Name: "Paper towels"})
	if err != nil {
		t.Fatalf("AddItem: %v", err)
	}
	if _, err := f.lists.UpdateItem(ctx, owner, list.ID, manual.ID, service.UpdateShoppingItemInput{Checked: ptr(true)}); err != nil {
		t.Fatalf("check manual item: %v", err)
	}

	mustSetEntry(t, f.plan, owner, day, "lunch", meal.ID, 3)
	sub, err := f.lists.Subscribe(ctx, owner, list.ID)
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	defer sub.Close()

	regenerated, created, err := f.lists.Generate(ctx, owner, service.GenerateShoppingListInput{From: day, To: day, ListID: &list.ID})
	if err != nil {
		t.Fatalf("regenerate: %v", err)
	}
	if created {
		t.Error("created = true, want false (list_id given)")
	}
	if len(regenerated.Items) != 2 {
		t.Fatalf("items after regenerate = %+v, want the manual item plus one generated item", regenerated.Items)
	}
	kept, fresh := regenerated.Items[0], regenerated.Items[1]
	if kept.ID != manual.ID || !kept.Checked || kept.Origin != "manual" {
		t.Errorf("first item = %+v, want the manual item, untouched and still checked", kept)
	}
	if fresh.ID == oldGenerated.ID || fresh.Origin != "generated" || *fresh.Quantity != 300 || fresh.Position <= kept.Position {
		t.Errorf("second item = %+v, want a new generated rice line of 300 g after the manual item", fresh)
	}
	if ev := nextEvent(t, sub); ev.Type != service.ListEventListChanged || ev.ListID != list.ID {
		t.Errorf("event = %+v, want list_changed for the list", ev)
	}
}

func TestShoppingListsGenerateValidatesTheRangeAndTheListsOwner(t *testing.T) {
	f := newShoppingListsFixture(t)
	ctx := context.Background()
	owner := newTestUser(t, f.st, "shopper7@example.com")
	other := newTestUser(t, f.st, "other7@example.com")

	list, err := f.lists.Create(ctx, owner, service.CreateShoppingListInput{Name: "Mine"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	day := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	if _, _, err := f.lists.Generate(ctx, other, service.GenerateShoppingListInput{From: day, To: day, ListID: &list.ID}); !errors.Is(err, service.ErrShoppingListNotFound) {
		t.Errorf("regenerate another user's list: err = %v, want ErrShoppingListNotFound", err)
	}
	if _, _, err := f.lists.Generate(ctx, owner, service.GenerateShoppingListInput{From: day, To: day.AddDate(0, 0, -1)}); !errors.Is(err, service.ErrPlanRangeInvalid) {
		t.Errorf("to before from: err = %v, want ErrPlanRangeInvalid", err)
	}
	if _, _, err := f.lists.Generate(ctx, owner, service.GenerateShoppingListInput{From: day, To: day.AddDate(0, 0, 92)}); !errors.Is(err, service.ErrPlanRangeTooLong) {
		t.Errorf("93-day range: err = %v, want ErrPlanRangeTooLong", err)
	}
	empty, created, err := f.lists.Generate(ctx, owner, service.GenerateShoppingListInput{From: day, To: day.AddDate(0, 0, 91)})
	if err != nil || !created || len(empty.Items) != 0 || empty.Name != "Shopping 2026-06-01 to 2026-08-31" {
		t.Errorf("92-day range with no plan entries = %+v, %v, %v; want a new empty list with the default name", empty, created, err)
	}
}
