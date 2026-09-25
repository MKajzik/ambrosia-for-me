package service_test

import (
	"context"
	"errors"
	"math"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/InzKazik/mealplanner/backend/internal/service"
	"github.com/InzKazik/mealplanner/backend/internal/store"
	"github.com/InzKazik/mealplanner/backend/internal/store/sqlc"
)

func newMealsFixture(t *testing.T) (*service.Meals, *service.Ingredients, *store.Store) {
	t.Helper()
	_, st := newIngredientsFixture(t)
	return service.NewMeals(st), service.NewIngredients(st), st
}

func mustCreateIngredient(t *testing.T, ing *service.Ingredients, owner uuid.UUID, in service.CreateIngredientInput) service.Ingredient {
	t.Helper()
	i, err := ing.Create(context.Background(), owner, in)
	if err != nil {
		t.Fatalf("create ingredient %q: %v", in.Name, err)
	}
	return i
}

// almostEqual compares two nutrition values with a small tolerance. Values
// like 2.7, 0.92 and 0.027 are not exact in binary floating point (they are
// exact decimal fractions, not exact binary ones), so the golden tests below
// compare with a tolerance instead of claiming exact equality — the earlier
// comment here claimed float64 division was exact "since every input is a
// terminating decimal," which is false for exactly these values; the tests
// only passed because the rounding happened to land on the nose for the
// specific numbers and multiplication order used.
func almostEqual(got, want float64) bool {
	return math.Abs(got-want) < 1e-9
}

// TestMealsNutritionIsComputedToTheGram is the nutrition golden test the spec
// (§6) calls for: hand-verified totals for a small real recipe, checked to
// within a small tolerance (see almostEqual).
func TestMealsNutritionIsComputedToTheGram(t *testing.T) {
	meals, ing, st := newMealsFixture(t)
	owner := newTestUser(t, st, "chef@example.com")

	chicken := mustCreateIngredient(t, ing, owner, service.CreateIngredientInput{
		Name: "Chicken Breast", Category: "meat_seafood",
		Nutrients: map[string]float64{service.NutrientCalories: 165, service.NutrientProtein: 31},
	})
	rice := mustCreateIngredient(t, ing, owner, service.CreateIngredientInput{
		Name: "White Rice", Category: "grains_bread",
		Nutrients: map[string]float64{service.NutrientCalories: 130, service.NutrientProtein: 2.7},
	})

	meal, err := meals.Create(context.Background(), owner, service.CreateMealInput{Name: "Chicken and Rice", Servings: 2})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	meal, err = meals.ReplaceIngredients(context.Background(), owner, meal.ID, []service.MealIngredientInput{
		{IngredientID: chicken.ID, Quantity: 300, Unit: "g"},
		{IngredientID: rice.ID, Quantity: 200, Unit: "g"},
	})
	if err != nil {
		t.Fatalf("ReplaceIngredients: %v", err)
	}

	// Totals: calories = 3*165 + 2*130 = 755, protein = 3*31 + 2*2.7 = 98.4.
	// Per serving (servings=2): calories = 377.5, protein = 49.2.
	if got := meal.NutritionPerServing[service.NutrientCalories]; !almostEqual(got, 377.5) {
		t.Errorf("calories per serving = %v, want 377.5", got)
	}
	if got := meal.NutritionPerServing[service.NutrientProtein]; !almostEqual(got, 49.2) {
		t.Errorf("protein per serving = %v, want 49.2", got)
	}
}

func TestMealsNutritionConvertsPieceAndMlUnits(t *testing.T) {
	meals, ing, st := newMealsFixture(t)
	owner := newTestUser(t, st, "chef2@example.com")

	gramsPerEgg := 50.0
	egg := mustCreateIngredient(t, ing, owner, service.CreateIngredientInput{
		Name: "Egg", Category: "dairy_eggs", GramsPerPiece: &gramsPerEgg,
		Nutrients: map[string]float64{service.NutrientCalories: 155},
	})
	density := 0.92
	oil := mustCreateIngredient(t, ing, owner, service.CreateIngredientInput{
		Name: "Olive Oil", Category: "condiments_oils", DensityGPerMl: &density,
		Nutrients: map[string]float64{service.NutrientCalories: 884},
	})

	meal, err := meals.Create(context.Background(), owner, service.CreateMealInput{Name: "Fried Egg", Servings: 1})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	meal, err = meals.ReplaceIngredients(context.Background(), owner, meal.ID, []service.MealIngredientInput{
		{IngredientID: egg.ID, Quantity: 2, Unit: "piece"}, // 100g -> 155 kcal
		{IngredientID: oil.ID, Quantity: 10, Unit: "ml"},   // 9.2g -> 81.328 kcal
	})
	if err != nil {
		t.Fatalf("ReplaceIngredients: %v", err)
	}

	want := 155.0 + 9.2/100*884 // 236.328
	if got := meal.NutritionPerServing[service.NutrientCalories]; !almostEqual(got, want) {
		t.Errorf("calories per serving = %v, want %v", got, want)
	}
}

func TestMealsReplaceIngredientsRejectsAnUnconvertibleUnitAndLeavesTheMealUnchanged(t *testing.T) {
	meals, ing, st := newMealsFixture(t)
	owner := newTestUser(t, st, "chef3@example.com")

	// No grams_per_piece: this ingredient cannot be used with unit "piece".
	flour := mustCreateIngredient(t, ing, owner, service.CreateIngredientInput{Name: "Flour", Category: "grains_bread"})
	rice := mustCreateIngredient(t, ing, owner, service.CreateIngredientInput{
		Name: "Rice", Category: "grains_bread",
		Nutrients: map[string]float64{service.NutrientCalories: 130},
	})

	meal, err := meals.Create(context.Background(), owner, service.CreateMealInput{Name: "Bread", Servings: 1})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	meal, err = meals.ReplaceIngredients(context.Background(), owner, meal.ID, []service.MealIngredientInput{
		{IngredientID: rice.ID, Quantity: 100, Unit: "g"},
	})
	if err != nil {
		t.Fatalf("seed ReplaceIngredients: %v", err)
	}

	_, err = meals.ReplaceIngredients(context.Background(), owner, meal.ID, []service.MealIngredientInput{
		{IngredientID: rice.ID, Quantity: 100, Unit: "g"},
		{IngredientID: flour.ID, Quantity: 3, Unit: "piece"},
	})
	if !errors.Is(err, service.ErrUnitNotConvertible) {
		t.Errorf("err = %v, want ErrUnitNotConvertible", err)
	}

	// The rejected replace must not have touched the meal: it should still
	// have exactly the one rice line from the seed call.
	got, err := meals.Get(context.Background(), owner, meal.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if len(got.Ingredients) != 1 || got.Ingredients[0].IngredientID != rice.ID {
		t.Errorf("Ingredients after the rejected replace = %+v, want unchanged (just rice)", got.Ingredients)
	}
}

// TestMealsStayReadableWhenAnIngredientEditWouldBreakUnitConversion pins
// finding #2's fix: clearing an ingredient's density_g_per_ml (or
// grams_per_piece) while a meal still depends on it for an "ml" (or
// "piece") line is now rejected at write time, on the ingredient edit
// itself — not discovered later when the meal becomes unreadable. Before
// this fix, the edit above succeeded and every subsequent GET/PATCH/copy of
// the meal returned 409 unit_not_convertible with no way to fix it (the
// client could no longer GET the meal to see what to edit).
func TestMealsStayReadableWhenAnIngredientEditWouldBreakUnitConversion(t *testing.T) {
	meals, ing, st := newMealsFixture(t)
	owner := newTestUser(t, st, "chef4@example.com")

	density := 0.92
	oil := mustCreateIngredient(t, ing, owner, service.CreateIngredientInput{
		Name: "Olive Oil", Category: "condiments_oils", DensityGPerMl: &density,
		Nutrients: map[string]float64{service.NutrientCalories: 884},
	})
	meal, err := meals.Create(context.Background(), owner, service.CreateMealInput{Name: "Dressing", Servings: 1})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, err := meals.ReplaceIngredients(context.Background(), owner, meal.ID, []service.MealIngredientInput{
		{IngredientID: oil.ID, Quantity: 10, Unit: "ml"},
	}); err != nil {
		t.Fatalf("ReplaceIngredients: %v", err)
	}

	// The edit itself is rejected now, before anything is written.
	if _, err := ing.Update(context.Background(), owner, oil.ID, service.UpdateIngredientInput{
		DensityGPerMl: service.Set[float64](nil),
	}); !errors.Is(err, service.ErrIngredientInUseByUnconvertibleUnit) {
		t.Errorf("clear density while in use by a meal: err = %v, want ErrIngredientInUseByUnconvertibleUnit", err)
	}

	// The ingredient's density_g_per_ml must be untouched by the rejected
	// edit, and the meal must remain fully readable with its original
	// nutrition.
	unchanged, err := ing.List(context.Background(), owner, service.ListIngredientsInput{Limit: 10})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	var found bool
	for _, i := range unchanged.Items {
		if i.ID == oil.ID {
			found = true
			if i.DensityGPerMl == nil || *i.DensityGPerMl != density {
				t.Errorf("oil.DensityGPerMl = %v, want unchanged at %v", i.DensityGPerMl, density)
			}
		}
	}
	if !found {
		t.Fatal("oil not found in List after the rejected update")
	}

	got, err := meals.Get(context.Background(), owner, meal.ID)
	if err != nil {
		t.Fatalf("Get after the rejected ingredient edit: %v", err)
	}
	if len(got.Ingredients) != 1 || got.Ingredients[0].IngredientID != oil.ID {
		t.Errorf("Ingredients = %+v, want unchanged (just the oil line)", got.Ingredients)
	}
	// 10ml * 0.92 g/ml -> 9.2g, at 884 kcal/100g, /1 serving.
	want := 9.2 / 100 * 884
	if got := got.NutritionPerServing[service.NutrientCalories]; !almostEqual(got, want) {
		t.Errorf("calories per serving = %v, want %v", got, want)
	}
}

func TestMealsNutritionIsNullForAKeyMissingFromAnyIngredient(t *testing.T) {
	meals, ing, st := newMealsFixture(t)
	owner := newTestUser(t, st, "chef5@example.com")

	// Only protein is known for this one; calories is absent from its map.
	proteinOnly := mustCreateIngredient(t, ing, owner, service.CreateIngredientInput{
		Name: "Mystery Powder", Category: "other",
		Nutrients: map[string]float64{service.NutrientProtein: 80},
	})
	known := mustCreateIngredient(t, ing, owner, service.CreateIngredientInput{
		Name: "Known Thing", Category: "other",
		Nutrients: map[string]float64{service.NutrientCalories: 100, service.NutrientProtein: 10},
	})

	meal, err := meals.Create(context.Background(), owner, service.CreateMealInput{Name: "Shake", Servings: 1})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	meal, err = meals.ReplaceIngredients(context.Background(), owner, meal.ID, []service.MealIngredientInput{
		{IngredientID: proteinOnly.ID, Quantity: 100, Unit: "g"},
		{IngredientID: known.ID, Quantity: 100, Unit: "g"},
	})
	if err != nil {
		t.Fatalf("ReplaceIngredients: %v", err)
	}

	if _, ok := meal.NutritionPerServing[service.NutrientCalories]; ok {
		t.Errorf("calories = %v, want absent (one ingredient's calories is unknown)", meal.NutritionPerServing[service.NutrientCalories])
	}
	if got := meal.NutritionPerServing[service.NutrientProtein]; !almostEqual(got, 90) {
		t.Errorf("protein = %v, want 90 (both known)", got)
	}
}

func TestMealsEmptyMealHasZeroNutritionForEveryKey(t *testing.T) {
	meals, _, st := newMealsFixture(t)
	owner := newTestUser(t, st, "chef6@example.com")

	meal, err := meals.Create(context.Background(), owner, service.CreateMealInput{Name: "Empty", Servings: 3})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if got := meal.NutritionPerServing[service.NutrientCalories]; got != 0 {
		t.Errorf("calories = %v, want 0", got)
	}
	if got := meal.NutritionPerServing[service.NutrientFolate]; got != 0 {
		t.Errorf("folate = %v, want 0", got)
	}
	if len(meal.NutritionPerServing) != 18 {
		t.Errorf("len(NutritionPerServing) = %d, want 18", len(meal.NutritionPerServing))
	}
	if len(meal.Ingredients) != 0 {
		t.Errorf("Ingredients = %+v, want empty", meal.Ingredients)
	}
}

func TestMealsOfOneUserAreInvisibleAndUntouchableToAStranger(t *testing.T) {
	meals, _, st := newMealsFixture(t)
	owner := newTestUser(t, st, "owner7@example.com")
	other := newTestUser(t, st, "other7@example.com")

	meal, err := meals.Create(context.Background(), owner, service.CreateMealInput{Name: "Private Meal", Servings: 1})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	if _, err := meals.Get(context.Background(), other, meal.ID); !errors.Is(err, service.ErrMealNotFound) {
		t.Errorf("Get by a non-owner: err = %v, want ErrMealNotFound", err)
	}
	newName := "Hijacked"
	if _, err := meals.Update(context.Background(), other, meal.ID, service.UpdateMealInput{Name: &newName}); !errors.Is(err, service.ErrMealNotFound) {
		t.Errorf("Update by a non-owner: err = %v, want ErrMealNotFound", err)
	}
	if err := meals.Delete(context.Background(), other, meal.ID); !errors.Is(err, service.ErrMealNotFound) {
		t.Errorf("Delete by a non-owner: err = %v, want ErrMealNotFound", err)
	}
	if _, err := meals.Copy(context.Background(), other, meal.ID); !errors.Is(err, service.ErrMealNotFound) {
		t.Errorf("Copy by a non-owner: err = %v, want ErrMealNotFound", err)
	}
	if _, err := meals.ReplaceIngredients(context.Background(), other, meal.ID, nil); !errors.Is(err, service.ErrMealNotFound) {
		t.Errorf("ReplaceIngredients by a non-owner: err = %v, want ErrMealNotFound", err)
	}
}

// TestMealsUpdateRecomputesNutritionWhenServingsChange is finding #3's first
// PATCH success-path case: Update had no test coverage beyond the two
// failure-path cases above. A servings change must recompute
// NutritionPerServing (the ingredient totals are unchanged; only the
// division changes).
func TestMealsUpdateRecomputesNutritionWhenServingsChange(t *testing.T) {
	meals, ing, st := newMealsFixture(t)
	owner := newTestUser(t, st, "patch1@example.com")

	rice := mustCreateIngredient(t, ing, owner, service.CreateIngredientInput{
		Name: "Rice", Category: "grains_bread",
		Nutrients: map[string]float64{service.NutrientCalories: 130},
	})
	meal, err := meals.Create(context.Background(), owner, service.CreateMealInput{Name: "Bowl", Servings: 2})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	meal, err = meals.ReplaceIngredients(context.Background(), owner, meal.ID, []service.MealIngredientInput{
		{IngredientID: rice.ID, Quantity: 200, Unit: "g"},
	})
	if err != nil {
		t.Fatalf("ReplaceIngredients: %v", err)
	}
	// 200g rice at 130 kcal/100g = 260 kcal total, /2 servings = 130/serving.
	if got := meal.NutritionPerServing[service.NutrientCalories]; got != 130 {
		t.Fatalf("calories per serving before update = %v, want 130", got)
	}

	newServings := 1.0
	updated, err := meals.Update(context.Background(), owner, meal.ID, service.UpdateMealInput{Servings: &newServings})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if updated.Servings != 1 {
		t.Errorf("Servings = %v, want 1", updated.Servings)
	}
	// Same 260 kcal total, now /1 serving = 260/serving.
	if got := updated.NutritionPerServing[service.NutrientCalories]; got != 260 {
		t.Errorf("calories per serving after update = %v, want 260", got)
	}
}

// TestMealsUpdateNotesHandlesAllThreeTriStates is finding #3's remaining two
// PATCH success-path cases, both exercising the Optional[string] tri-state
// toOptionalString maps a request onto: an explicit null clears existing
// notes, and an omitted notes field (the zero-value Optional[string]{},
// Specified: false) leaves the existing value alone — the third state the
// tri-state exists to distinguish from null.
func TestMealsUpdateNotesHandlesAllThreeTriStates(t *testing.T) {
	meals, _, st := newMealsFixture(t)
	owner := newTestUser(t, st, "patch2@example.com")

	meal, err := meals.Create(context.Background(), owner, service.CreateMealInput{
		Name: "Notes Meal", Notes: strPtr("original notes"), Servings: 1,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	// Omitted (Optional[string]{}, the zero value): notes unchanged, even
	// though another field is updated in the same call.
	newName := "Renamed"
	updated, err := meals.Update(context.Background(), owner, meal.ID, service.UpdateMealInput{Name: &newName})
	if err != nil {
		t.Fatalf("Update(name only): %v", err)
	}
	if updated.Notes == nil || *updated.Notes != "original notes" {
		t.Errorf("Notes after an update that omits notes = %v, want unchanged (original notes)", updated.Notes)
	}

	// Set[string](nil): explicit null clears notes.
	cleared, err := meals.Update(context.Background(), owner, meal.ID, service.UpdateMealInput{Notes: service.Set[string](nil)})
	if err != nil {
		t.Fatalf("Update(clear notes): %v", err)
	}
	if cleared.Notes != nil {
		t.Errorf("Notes after Set(nil) = %v, want nil", cleared.Notes)
	}

	// Set(&v): a genuine new value.
	newNotes := "updated notes"
	set, err := meals.Update(context.Background(), owner, meal.ID, service.UpdateMealInput{Notes: service.Set(&newNotes)})
	if err != nil {
		t.Fatalf("Update(set notes): %v", err)
	}
	if set.Notes == nil || *set.Notes != newNotes {
		t.Errorf("Notes after Set(&newNotes) = %v, want %q", set.Notes, newNotes)
	}
}

// TestMealsReplaceIngredientsRejectsAnotherUsersCustomIngredient pins the
// visibility half of the write-path guard: the ingredient exists, so only the
// (owner_id IS NULL OR owner_id = user_id) filter in GetIngredientsForUser
// keeps it out. Without it, another user's private ingredient — its name,
// category and nutrition — would leak into this meal's response.
func TestMealsReplaceIngredientsRejectsAnotherUsersCustomIngredient(t *testing.T) {
	meals, ing, st := newMealsFixture(t)
	userA := newTestUser(t, st, "a10@example.com")
	userB := newTestUser(t, st, "b10@example.com")

	secret := mustCreateIngredient(t, ing, userA, service.CreateIngredientInput{
		Name: "Secret Blend", Category: "condiments_oils",
		Nutrients: map[string]float64{service.NutrientCalories: 500},
	})

	meal, err := meals.Create(context.Background(), userB, service.CreateMealInput{Name: "Curious Meal", Servings: 1})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, err := meals.ReplaceIngredients(context.Background(), userB, meal.ID, []service.MealIngredientInput{
		{IngredientID: secret.ID, Quantity: 100, Unit: "g"},
	}); !errors.Is(err, service.ErrMealIngredientNotFound) {
		t.Fatalf("ReplaceIngredients with another user's ingredient: err = %v, want ErrMealIngredientNotFound", err)
	}

	// Nothing was written: the meal still has no ingredients.
	got, err := meals.Get(context.Background(), userB, meal.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if len(got.Ingredients) != 0 {
		t.Errorf("Ingredients = %+v, want none", got.Ingredients)
	}
}

// TestMealsReplaceIngredientsBumpsUpdatedAt pins the second half of finding
// #1: ReplaceIngredients now goes through TouchMealForUser (an UPDATE), not
// a plain SELECT, so it must advance meals.updated_at like every other
// write, unlike before this fix when the ingredient list could change
// without updated_at moving.
func TestMealsReplaceIngredientsBumpsUpdatedAt(t *testing.T) {
	meals, ing, st := newMealsFixture(t)
	owner := newTestUser(t, st, "touch@example.com")

	rice := mustCreateIngredient(t, ing, owner, service.CreateIngredientInput{
		Name: "Rice", Category: "grains_bread",
		Nutrients: map[string]float64{service.NutrientCalories: 130},
	})
	meal, err := meals.Create(context.Background(), owner, service.CreateMealInput{Name: "Touch", Servings: 1})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	before := meal.UpdatedAt

	after, err := meals.ReplaceIngredients(context.Background(), owner, meal.ID, []service.MealIngredientInput{
		{IngredientID: rice.ID, Quantity: 100, Unit: "g"},
	})
	if err != nil {
		t.Fatalf("ReplaceIngredients: %v", err)
	}
	if !after.UpdatedAt.After(before) {
		t.Errorf("UpdatedAt after ReplaceIngredients = %v, want after the pre-replace value %v", after.UpdatedAt, before)
	}
}

// TestMealsReplaceIngredientsConcurrentCallsOnTheSameMealDoNotRace is finding
// #1's regression test: before the fix, ReplaceIngredients read the meal
// with a plain SELECT and took no row lock, so two concurrent replaces on
// the same meal could interleave their DELETE+INSERT under READ COMMITTED
// and the loser's INSERT would hit the meal_ingredients (meal_id, position)
// unique constraint — a raw 23505 nothing translates, surfacing as an
// unexpected error instead of a clean result. Firing several concurrent
// calls and asserting none returns an unexpected error catches that
// regression; TouchMealForUser's row lock now serializes them instead.
func TestMealsReplaceIngredientsConcurrentCallsOnTheSameMealDoNotRace(t *testing.T) {
	meals, ing, st := newMealsFixture(t)
	owner := newTestUser(t, st, "race@example.com")

	rice := mustCreateIngredient(t, ing, owner, service.CreateIngredientInput{
		Name: "Rice", Category: "grains_bread",
		Nutrients: map[string]float64{service.NutrientCalories: 130},
	})
	meal, err := meals.Create(context.Background(), owner, service.CreateMealInput{Name: "Race", Servings: 1})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	items := []service.MealIngredientInput{{IngredientID: rice.ID, Quantity: 100, Unit: "g"}}

	const numGoroutines = 8
	var (
		wg      sync.WaitGroup
		barrier = make(chan struct{})
		results = make([]error, numGoroutines)
	)
	wg.Add(numGoroutines)
	for i := 0; i < numGoroutines; i++ {
		go func(idx int) {
			defer wg.Done()
			<-barrier // release all goroutines at once
			_, err := meals.ReplaceIngredients(context.Background(), owner, meal.ID, items)
			results[idx] = err
		}(i)
	}
	close(barrier)
	wg.Wait()

	for i, err := range results {
		if err != nil {
			t.Errorf("goroutine %d: ReplaceIngredients returned an unexpected error: %v", i, err)
		}
	}

	// The meal must still have exactly the one rice line, not duplicates or
	// a corrupted position sequence from an interleaved write.
	got, err := meals.Get(context.Background(), owner, meal.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if len(got.Ingredients) != 1 || got.Ingredients[0].IngredientID != rice.ID {
		t.Errorf("Ingredients after concurrent replaces = %+v, want exactly one rice line", got.Ingredients)
	}
}

func TestMealsCopyDuplicatesIngredientsAndStartsPrivate(t *testing.T) {
	meals, ing, st := newMealsFixture(t)
	owner := newTestUser(t, st, "chef8@example.com")

	rice := mustCreateIngredient(t, ing, owner, service.CreateIngredientInput{
		Name: "Rice", Category: "grains_bread",
		Nutrients: map[string]float64{service.NutrientCalories: 130},
	})
	original, err := meals.Create(context.Background(), owner, service.CreateMealInput{
		Name: "Rice Bowl", Notes: strPtr("family recipe"), Servings: 2, SharedWithPartner: true,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	original, err = meals.ReplaceIngredients(context.Background(), owner, original.ID, []service.MealIngredientInput{
		{IngredientID: rice.ID, Quantity: 200, Unit: "g"},
	})
	if err != nil {
		t.Fatalf("ReplaceIngredients: %v", err)
	}

	copy_, err := meals.Copy(context.Background(), owner, original.ID)
	if err != nil {
		t.Fatalf("Copy: %v", err)
	}
	if copy_.ID == original.ID {
		t.Fatal("copy has the same ID as the original")
	}
	if copy_.Name != original.Name || copy_.Servings != original.Servings {
		t.Errorf("copy = %+v, want same name/servings as original", copy_)
	}
	if copy_.Notes == nil || *copy_.Notes != "family recipe" {
		t.Errorf("copy.Notes = %v, want same notes as original (family recipe)", copy_.Notes)
	}
	if copy_.SharedWithPartner {
		t.Error("copy has shared_with_partner = true, want false regardless of the original")
	}
	if len(copy_.Ingredients) != 1 || copy_.Ingredients[0].IngredientID != rice.ID || copy_.Ingredients[0].Quantity != 200 {
		t.Errorf("copy ingredients = %+v, want one rice line at 200g", copy_.Ingredients)
	}
	if copy_.NutritionPerServing[service.NutrientCalories] != original.NutritionPerServing[service.NutrientCalories] {
		t.Error("copy nutrition differs from the original's")
	}
}

func TestMealsListPaginatesOwnMealsOnly(t *testing.T) {
	meals, _, st := newMealsFixture(t)
	owner := newTestUser(t, st, "owner9@example.com")
	other := newTestUser(t, st, "other9@example.com")

	for _, name := range []string{"Breakfast", "Dinner", "Lunch"} {
		if _, err := meals.Create(context.Background(), owner, service.CreateMealInput{Name: name, Servings: 1}); err != nil {
			t.Fatalf("Create(%s): %v", name, err)
		}
	}
	if _, err := meals.Create(context.Background(), other, service.CreateMealInput{Name: "Not Mine", Servings: 1}); err != nil {
		t.Fatalf("Create(other's meal): %v", err)
	}

	var names []string
	var cursor *service.MealCursor
	for {
		page, err := meals.List(context.Background(), owner, service.ListMealsInput{Cursor: cursor, Limit: 1})
		if err != nil {
			t.Fatalf("List: %v", err)
		}
		for _, m := range page.Items {
			names = append(names, m.Name)
		}
		if page.NextCursor == nil {
			break
		}
		cursor = page.NextCursor
	}
	if want := []string{"Breakfast", "Dinner", "Lunch"}; len(names) != len(want) || names[0] != want[0] || names[1] != want[1] || names[2] != want[2] {
		t.Errorf("paginated names = %v, want %v (alphabetical, owner's only)", names, want)
	}
}

func strPtr(s string) *string { return &s }

// TestMealsDeleteIsBlockedWhileInUseByATemplateSlot pins the
// template_slots_meal_id_fkey half of the in-use guard: a diet template's
// slot referencing the meal (NO ACTION, like meal_ingredients_ingredient_id)
// must block the delete rather than fail with an unmapped foreign-key error.
func TestMealsDeleteIsBlockedWhileInUseByATemplateSlot(t *testing.T) {
	meals, _, st := newMealsFixture(t)
	owner := newTestUser(t, st, "chef10@example.com")

	meal, err := meals.Create(context.Background(), owner, service.CreateMealInput{Name: "Oatmeal", Servings: 1})
	if err != nil {
		t.Fatalf("Create meal: %v", err)
	}
	tpl, err := st.CreateDietTemplate(context.Background(), sqlc.CreateDietTemplateParams{OwnerID: owner, Name: "Week", DayCount: 7})
	if err != nil {
		t.Fatalf("create template: %v", err)
	}
	if _, err := st.InsertTemplateSlot(context.Background(), sqlc.InsertTemplateSlotParams{
		TemplateID: tpl.ID, DayIndex: 0, Slot: "breakfast", MealID: meal.ID, Portion: 1,
	}); err != nil {
		t.Fatalf("insert template slot: %v", err)
	}

	if err := meals.Delete(context.Background(), owner, meal.ID); !errors.Is(err, service.ErrMealInUse) {
		t.Errorf("Delete while referenced by a template slot: err = %v, want ErrMealInUse", err)
	}

	if err := st.DeleteTemplateSlots(context.Background(), tpl.ID); err != nil {
		t.Fatalf("clear template slots: %v", err)
	}
	if err := meals.Delete(context.Background(), owner, meal.ID); err != nil {
		t.Errorf("Delete once no longer referenced: %v", err)
	}
}

// TestMealsDeleteIsBlockedWhileInUseByAPlanEntry pins the
// plan_entries_meal_id_fkey half of the same guard.
func TestMealsDeleteIsBlockedWhileInUseByAPlanEntry(t *testing.T) {
	meals, _, st := newMealsFixture(t)
	owner := newTestUser(t, st, "chef11@example.com")

	meal, err := meals.Create(context.Background(), owner, service.CreateMealInput{Name: "Salad", Servings: 1})
	if err != nil {
		t.Fatalf("Create meal: %v", err)
	}
	entry, err := st.InsertPlanEntry(context.Background(), sqlc.InsertPlanEntryParams{
		OwnerID: owner, Date: pgtype.Date{Time: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), Valid: true},
		Slot: "lunch", MealID: meal.ID, Portion: 1,
	})
	if err != nil {
		t.Fatalf("insert plan entry: %v", err)
	}

	if err := meals.Delete(context.Background(), owner, meal.ID); !errors.Is(err, service.ErrMealInUse) {
		t.Errorf("Delete while referenced by a plan entry: err = %v, want ErrMealInUse", err)
	}

	if _, err := st.DeletePlanEntryForUserOnDateSlot(context.Background(), sqlc.DeletePlanEntryForUserOnDateSlotParams{
		UserID: owner, Date: entry.Date, Slot: entry.Slot,
	}); err != nil {
		t.Fatalf("clear plan entry: %v", err)
	}
	if err := meals.Delete(context.Background(), owner, meal.ID); err != nil {
		t.Errorf("Delete once no longer referenced: %v", err)
	}
}

func TestMealsPartnerReadsOnlySharedMealsAndNeverWritesThem(t *testing.T) {
	meals, ing, st := newMealsFixture(t)
	ctx := context.Background()
	alice := newTestUser(t, st, "alice20@example.com")
	bob := newTestUser(t, st, "bob20@example.com")
	carol := newTestUser(t, st, "carol20@example.com")
	partners := linkPartners(t, st, alice, bob)

	oats := mustCreateIngredient(t, ing, alice, service.CreateIngredientInput{
		Name: "Alice's Oats", Category: "grains_bread",
		Nutrients: map[string]float64{service.NutrientCalories: 400},
	})
	shared, err := meals.Create(ctx, alice, service.CreateMealInput{Name: "Shared Porridge", Servings: 2, SharedWithPartner: true})
	if err != nil {
		t.Fatalf("Create shared: %v", err)
	}
	shared, err = meals.ReplaceIngredients(ctx, alice, shared.ID, []service.MealIngredientInput{{IngredientID: oats.ID, Quantity: 100, Unit: "g"}})
	if err != nil {
		t.Fatalf("ReplaceIngredients: %v", err)
	}
	private, err := meals.Create(ctx, alice, service.CreateMealInput{Name: "Private Snack", Servings: 1})
	if err != nil {
		t.Fatalf("Create private: %v", err)
	}

	got, err := meals.Get(ctx, bob, shared.ID)
	if err != nil {
		t.Fatalf("partner Get of a shared meal: %v", err)
	}
	if got.OwnerID != alice || len(got.Ingredients) != 1 || got.Ingredients[0].IngredientName != "Alice's Oats" ||
		got.NutritionPerServing[service.NutrientCalories] != 200 {
		t.Errorf("partner sees %+v, want Alice's meal with her custom ingredient resolved and 200 kcal per serving", got)
	}
	if own, err := meals.Get(ctx, alice, shared.ID); err != nil || own.OwnerID != alice {
		t.Errorf("owner Get: %+v, %v", own, err)
	}

	name := "Hijacked"
	for label, err := range map[string]error{
		"Get of an unshared meal":        errOf(meals.Get(ctx, bob, private.ID)),
		"Get by a stranger":              errOf(meals.Get(ctx, carol, shared.ID)),
		"Update of a shared meal":        errOf(meals.Update(ctx, bob, shared.ID, service.UpdateMealInput{Name: &name})),
		"Delete of a shared meal":        meals.Delete(ctx, bob, shared.ID),
		"ReplaceIngredients of a shared": errOf(meals.ReplaceIngredients(ctx, bob, shared.ID, nil)),
	} {
		if !errors.Is(err, service.ErrMealNotFound) {
			t.Errorf("%s: err = %v, want ErrMealNotFound", label, err)
		}
	}

	ownList, err := meals.List(ctx, bob, service.ListMealsInput{Limit: 10})
	if err != nil || len(ownList.Items) != 0 {
		t.Errorf("the partner's own List = %+v (err %v), want empty: shared meals are not mixed in", ownList.Items, err)
	}
	partnerList, err := meals.ListPartner(ctx, bob, service.ListMealsInput{Limit: 10})
	if err != nil || len(partnerList.Items) != 1 || partnerList.Items[0].ID != shared.ID {
		t.Errorf("ListPartner = %+v (err %v), want only the shared meal", partnerList.Items, err)
	}
	if _, err := meals.ListPartner(ctx, carol, service.ListMealsInput{Limit: 10}); !errors.Is(err, service.ErrPartnerNotLinked) {
		t.Errorf("ListPartner without a partner: err = %v, want ErrPartnerNotLinked", err)
	}

	// Unsharing hides the meal at once; so does unlinking.
	if _, err := meals.Update(ctx, alice, shared.ID, service.UpdateMealInput{SharedWithPartner: ptr(false)}); err != nil {
		t.Fatalf("unshare: %v", err)
	}
	if _, err := meals.Get(ctx, bob, shared.ID); !errors.Is(err, service.ErrMealNotFound) {
		t.Errorf("partner Get after unsharing: err = %v, want ErrMealNotFound", err)
	}
	if _, err := meals.Update(ctx, alice, shared.ID, service.UpdateMealInput{SharedWithPartner: ptr(true)}); err != nil {
		t.Fatalf("share again: %v", err)
	}
	if err := partners.Unlink(ctx, alice); err != nil {
		t.Fatalf("Unlink: %v", err)
	}
	if _, err := meals.Get(ctx, bob, shared.ID); !errors.Is(err, service.ErrMealNotFound) {
		t.Errorf("partner Get after unlinking: err = %v, want ErrMealNotFound", err)
	}
	if _, err := meals.ListPartner(ctx, bob, service.ListMealsInput{Limit: 10}); !errors.Is(err, service.ErrPartnerNotLinked) {
		t.Errorf("ListPartner after unlinking: err = %v, want ErrPartnerNotLinked", err)
	}
}

// errOf drops a call's value, keeping its error, so a table of calls with
// different return types can be checked the same way.
func errOf[T any](_ T, err error) error { return err }

func TestMealsCopyOfAPartnersMealDuplicatesCustomIngredientsOnceAndSharesGlobalOnes(t *testing.T) {
	meals, ing, st := newMealsFixture(t)
	ctx := context.Background()
	alice := newTestUser(t, st, "alice21@example.com")
	bob := newTestUser(t, st, "bob21@example.com")
	carol := newTestUser(t, st, "carol21@example.com")
	partners := linkPartners(t, st, alice, bob)

	fdc := int32(170000)
	salt, err := st.UpsertUSDAIngredient(ctx, sqlc.UpsertUSDAIngredientParams{Name: "Salt", Category: "spices_herbs", UsdaFdcID: &fdc})
	if err != nil {
		t.Fatalf("UpsertUSDAIngredient: %v", err)
	}
	flour := mustCreateIngredient(t, ing, alice, service.CreateIngredientInput{
		Name: "Alice's Flour", Category: "grains_bread", GramsPerPiece: ptr(30.0),
		Nutrients: map[string]float64{service.NutrientCalories: 360, service.NutrientProtein: 10},
	})
	original, err := meals.Create(ctx, alice, service.CreateMealInput{Name: "Flatbread", Notes: strPtr("thin"), Servings: 2, SharedWithPartner: true})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	// The custom ingredient is on two lines, in two units; the global one on a third.
	original, err = meals.ReplaceIngredients(ctx, alice, original.ID, []service.MealIngredientInput{
		{IngredientID: flour.ID, Quantity: 200, Unit: "g"},
		{IngredientID: salt.ID, Quantity: 5, Unit: "g"},
		{IngredientID: flour.ID, Quantity: 2, Unit: "piece"},
	})
	if err != nil {
		t.Fatalf("ReplaceIngredients: %v", err)
	}

	cp, err := meals.Copy(ctx, bob, original.ID)
	if err != nil {
		t.Fatalf("partner Copy: %v", err)
	}
	if cp.OwnerID != bob || cp.SharedWithPartner || cp.Name != "Flatbread" || cp.Notes == nil || *cp.Notes != "thin" || cp.Servings != 2 {
		t.Errorf("copy = %+v, want Bob's private copy of the meal", cp)
	}
	if len(cp.Ingredients) != 3 {
		t.Fatalf("copy has %d lines, want 3", len(cp.Ingredients))
	}
	copiedFlour := cp.Ingredients[0].IngredientID
	if copiedFlour == flour.ID || cp.Ingredients[2].IngredientID != copiedFlour {
		t.Errorf("flour ids on the copy = %v and %v, want one new id (not %v) on both lines", copiedFlour, cp.Ingredients[2].IngredientID, flour.ID)
	}
	if cp.Ingredients[1].IngredientID != salt.ID {
		t.Errorf("the global ingredient on the copy = %v, want the shared %v", cp.Ingredients[1].IngredientID, salt.ID)
	}
	for k, want := range original.NutritionPerServing {
		if got := cp.NutritionPerServing[k]; !almostEqual(got, want) {
			t.Errorf("copy nutrition[%s] = %v, want the original's %v", k, got, want)
		}
	}

	// The duplicate is a real custom ingredient in Bob's library, invisible to Alice.
	dup, err := st.GetIngredientForUser(ctx, sqlc.GetIngredientForUserParams{ID: copiedFlour, UserID: &bob})
	if err != nil || dup.OwnerID == nil || *dup.OwnerID != bob || dup.GramsPerPiece == nil || *dup.GramsPerPiece != 30 {
		t.Errorf("duplicate ingredient = %+v (err %v), want Bob's custom flour with grams_per_piece 30", dup, err)
	}
	if _, err := st.GetIngredientForUser(ctx, sqlc.GetIngredientForUserParams{ID: copiedFlour, UserID: &alice}); !store.IsNotFound(err) {
		t.Errorf("Alice sees Bob's duplicate ingredient: err = %v, want not found", err)
	}

	// The copy is independent of the original: rename the ingredient, delete
	// the meal, delete the ingredient's owner's ingredient.
	if _, err := ing.Update(ctx, alice, flour.ID, service.UpdateIngredientInput{Name: strPtr("Renamed")}); err != nil {
		t.Fatalf("rename original ingredient: %v", err)
	}
	if err := meals.Delete(ctx, alice, original.ID); err != nil {
		t.Fatalf("delete original meal: %v", err)
	}
	if err := ing.Delete(ctx, alice, flour.ID); err != nil {
		t.Fatalf("delete original ingredient: %v", err)
	}
	after, err := meals.Get(ctx, bob, cp.ID)
	if err != nil || after.Ingredients[0].IngredientName != "Alice's Flour" {
		t.Errorf("copy after the original changed = %+v (err %v), want it untouched", after.Ingredients, err)
	}

	// Bob now owns the ingredient, so copying his own copy keeps referencing it.
	again, err := meals.Copy(ctx, bob, cp.ID)
	if err != nil || again.Ingredients[0].IngredientID != copiedFlour {
		t.Errorf("copy of Bob's own copy: %+v (err %v), want it to keep Bob's own ingredient", again.Ingredients, err)
	}

	// Not shared, or not a partner: not copyable.
	hidden, err := meals.Create(ctx, alice, service.CreateMealInput{Name: "Hidden", Servings: 1})
	if err != nil {
		t.Fatalf("Create hidden: %v", err)
	}
	if _, err := meals.Copy(ctx, bob, hidden.ID); !errors.Is(err, service.ErrMealNotFound) {
		t.Errorf("Copy of an unshared partner meal: err = %v, want ErrMealNotFound", err)
	}
	visible, err := meals.Create(ctx, alice, service.CreateMealInput{Name: "Visible", Servings: 1, SharedWithPartner: true})
	if err != nil {
		t.Fatalf("Create visible: %v", err)
	}
	if _, err := meals.Copy(ctx, carol, visible.ID); !errors.Is(err, service.ErrMealNotFound) {
		t.Errorf("Copy by a stranger: err = %v, want ErrMealNotFound", err)
	}
	if err := partners.Unlink(ctx, bob); err != nil {
		t.Fatalf("Unlink: %v", err)
	}
	if _, err := meals.Copy(ctx, bob, visible.ID); !errors.Is(err, service.ErrMealNotFound) {
		t.Errorf("Copy after unlinking: err = %v, want ErrMealNotFound", err)
	}
}

func TestMealsEachCopyOfAPartnersMealDuplicatesItsCustomIngredientsAgain(t *testing.T) {
	meals, ing, st := newMealsFixture(t)
	ctx := context.Background()
	alice := newTestUser(t, st, "alice22@example.com")
	bob := newTestUser(t, st, "bob22@example.com")
	linkPartners(t, st, alice, bob)

	milk := mustCreateIngredient(t, ing, alice, service.CreateIngredientInput{Name: "Alice's Milk", Category: "dairy_eggs"})
	m, err := meals.Create(ctx, alice, service.CreateMealInput{Name: "Latte", Servings: 1, SharedWithPartner: true})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, err := meals.ReplaceIngredients(ctx, alice, m.ID, []service.MealIngredientInput{{IngredientID: milk.ID, Quantity: 100, Unit: "g"}}); err != nil {
		t.Fatalf("ReplaceIngredients: %v", err)
	}
	first, err := meals.Copy(ctx, bob, m.ID)
	if err != nil {
		t.Fatalf("first Copy: %v", err)
	}
	again, err := meals.Copy(ctx, bob, m.ID)
	if err != nil {
		t.Fatalf("second Copy: %v", err)
	}
	if first.Ingredients[0].IngredientID == again.Ingredients[0].IngredientID {
		t.Error("two copies share one duplicated ingredient, want one duplicate per copy operation")
	}
}
