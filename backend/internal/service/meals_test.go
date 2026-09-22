package service_test

import (
	"context"
	"errors"
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

// TestMealsNutritionIsComputedToTheGram is the nutrition golden test the spec
// (§6) calls for: hand-verified totals for a small real recipe, checked to
// four decimal places (float64 division is exact here since every input is a
// terminating decimal).
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
	if got := meal.NutritionPerServing[service.NutrientCalories]; got != 377.5 {
		t.Errorf("calories per serving = %v, want 377.5", got)
	}
	if got := meal.NutritionPerServing[service.NutrientProtein]; got != 49.2 {
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
	if got := meal.NutritionPerServing[service.NutrientCalories]; got != want {
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

func TestMealsGetBecomesUnitNotConvertibleIfAnIngredientIsEditedAfterTheFact(t *testing.T) {
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

	// ingredients has no idea meals exist, so clearing density_g_per_ml here
	// succeeds even though a meal now depends on it for an "ml" line.
	if _, err := ing.Update(context.Background(), owner, oil.ID, service.UpdateIngredientInput{
		DensityGPerMl: service.Set[float64](nil),
	}); err != nil {
		t.Fatalf("clear density: %v", err)
	}

	if _, err := meals.Get(context.Background(), owner, meal.ID); !errors.Is(err, service.ErrUnitNotConvertible) {
		t.Errorf("Get after the ingredient lost its density: err = %v, want ErrUnitNotConvertible", err)
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
	if got := meal.NutritionPerServing[service.NutrientProtein]; got != 90 {
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

func TestMealsAreOwnerOnlyForNow(t *testing.T) {
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
