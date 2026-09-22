package service_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/InzKazik/mealplanner/backend/internal/service"
	"github.com/InzKazik/mealplanner/backend/internal/store"
)

func newPlanFixture(t *testing.T) (*service.Plan, *service.Meals, *service.Ingredients, *store.Store) {
	t.Helper()
	_, st := newIngredientsFixture(t)
	meals := service.NewMeals(st)
	return service.NewPlan(st, meals), meals, service.NewIngredients(st), st
}

func TestPlanSetEntryUpsertsNonSnackSlotsAndClearsProvenance(t *testing.T) {
	plan, meals, _, st := newPlanFixture(t)
	owner := newTestUser(t, st, "planowner1@example.com")
	meal1 := mustCreateMeal(t, meals, owner, "First")
	meal2 := mustCreateMeal(t, meals, owner, "Second")
	date := time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC)

	first, err := plan.SetEntry(context.Background(), owner, date, "breakfast", service.SetPlanEntryInput{MealID: meal1.ID, Portion: 1})
	if err != nil {
		t.Fatalf("SetEntry (first): %v", err)
	}

	second, err := plan.SetEntry(context.Background(), owner, date, "breakfast", service.SetPlanEntryInput{MealID: meal2.ID, Portion: 2})
	if err != nil {
		t.Fatalf("SetEntry (swap): %v", err)
	}
	if second.ID != first.ID {
		t.Errorf("swap created a new row (id %v), want the same row (id %v) updated in place", second.ID, first.ID)
	}
	if second.MealID != meal2.ID || second.Portion != 2 {
		t.Errorf("second = %+v, want meal2 at portion 2", second)
	}
	if second.FromTemplateID != nil {
		t.Errorf("FromTemplateID = %v, want nil after a manual set", second.FromTemplateID)
	}

	rng, err := plan.GetRange(context.Background(), owner, date, date)
	if err != nil {
		t.Fatalf("GetRange: %v", err)
	}
	if len(rng.Days) != 1 || len(rng.Days[0].Entries) != 1 {
		t.Fatalf("Days = %+v, want exactly one day with exactly one entry", rng.Days)
	}
}

func TestPlanSetEntryOnSnackAlwaysAddsANewRow(t *testing.T) {
	plan, meals, _, st := newPlanFixture(t)
	owner := newTestUser(t, st, "planowner2@example.com")
	meal := mustCreateMeal(t, meals, owner, "Snack Food")
	date := time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC)

	if _, err := plan.SetEntry(context.Background(), owner, date, "snack", service.SetPlanEntryInput{MealID: meal.ID, Portion: 1}); err != nil {
		t.Fatalf("SetEntry (first snack): %v", err)
	}
	if _, err := plan.SetEntry(context.Background(), owner, date, "snack", service.SetPlanEntryInput{MealID: meal.ID, Portion: 1}); err != nil {
		t.Fatalf("SetEntry (second snack): %v", err)
	}

	rng, err := plan.GetRange(context.Background(), owner, date, date)
	if err != nil {
		t.Fatalf("GetRange: %v", err)
	}
	if len(rng.Days) != 1 || len(rng.Days[0].Entries) != 2 {
		t.Fatalf("Days = %+v, want one day with two snack entries", rng.Days)
	}
}

func TestPlanSetEntryRejectsAMealNotOwnedByTheCaller(t *testing.T) {
	plan, meals, _, st := newPlanFixture(t)
	owner := newTestUser(t, st, "planowner3@example.com")
	other := newTestUser(t, st, "planother3@example.com")
	othersMeal := mustCreateMeal(t, meals, other, "Not Mine")

	_, err := plan.SetEntry(context.Background(), owner, time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC), "lunch", service.SetPlanEntryInput{MealID: othersMeal.ID, Portion: 1})
	if !errors.Is(err, service.ErrPlanMealNotFound) {
		t.Errorf("err = %v, want ErrPlanMealNotFound", err)
	}
}

func TestPlanDeleteEntryRemovesTheOneNonSnackEntryOrAllSnacks(t *testing.T) {
	plan, meals, _, st := newPlanFixture(t)
	owner := newTestUser(t, st, "planowner4@example.com")
	meal := mustCreateMeal(t, meals, owner, "Meal")
	date := time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC)

	if _, err := plan.SetEntry(context.Background(), owner, date, "dinner", service.SetPlanEntryInput{MealID: meal.ID, Portion: 1}); err != nil {
		t.Fatalf("SetEntry: %v", err)
	}
	if err := plan.DeleteEntry(context.Background(), owner, date, "dinner"); err != nil {
		t.Errorf("DeleteEntry: %v", err)
	}
	if err := plan.DeleteEntry(context.Background(), owner, date, "dinner"); !errors.Is(err, service.ErrPlanEntryNotFound) {
		t.Errorf("DeleteEntry again: err = %v, want ErrPlanEntryNotFound", err)
	}

	if _, err := plan.SetEntry(context.Background(), owner, date, "snack", service.SetPlanEntryInput{MealID: meal.ID, Portion: 1}); err != nil {
		t.Fatalf("SetEntry snack 1: %v", err)
	}
	if _, err := plan.SetEntry(context.Background(), owner, date, "snack", service.SetPlanEntryInput{MealID: meal.ID, Portion: 1}); err != nil {
		t.Fatalf("SetEntry snack 2: %v", err)
	}
	if err := plan.DeleteEntry(context.Background(), owner, date, "snack"); err != nil {
		t.Errorf("DeleteEntry (snack, both): %v", err)
	}
	rng, err := plan.GetRange(context.Background(), owner, date, date)
	if err != nil {
		t.Fatalf("GetRange: %v", err)
	}
	if len(rng.Days[0].Entries) != 0 {
		t.Errorf("entries after deleting all snacks = %+v, want none", rng.Days[0].Entries)
	}
}

func TestPlanGetRangeComputesDailyTotalsAndPropagatesUnknownNutrients(t *testing.T) {
	plan, meals, ing, st := newPlanFixture(t)
	owner := newTestUser(t, st, "planowner5@example.com")

	rice, err := ing.Create(context.Background(), owner, service.CreateIngredientInput{
		Name: "Rice", Category: "grains_bread", Nutrients: map[string]float64{service.NutrientCalories: 130},
	})
	if err != nil {
		t.Fatalf("create ingredient: %v", err)
	}
	riceMeal, err := meals.Create(context.Background(), owner, service.CreateMealInput{Name: "Rice Bowl", Servings: 1})
	if err != nil {
		t.Fatalf("create meal: %v", err)
	}
	if _, err := meals.ReplaceIngredients(context.Background(), owner, riceMeal.ID, []service.MealIngredientInput{
		{IngredientID: rice.ID, Quantity: 200, Unit: "g"},
	}); err != nil {
		t.Fatalf("ReplaceIngredients: %v", err)
	}
	// riceMeal's calories per serving: 200/100*130 = 260.

	day1 := time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC)
	day2 := day1.AddDate(0, 0, 1)
	if _, err := plan.SetEntry(context.Background(), owner, day1, "breakfast", service.SetPlanEntryInput{MealID: riceMeal.ID, Portion: 2}); err != nil {
		t.Fatalf("SetEntry day1: %v", err)
	}
	// day1 total calories: 260 * 2 = 520.

	protein, err := ing.Create(context.Background(), owner, service.CreateIngredientInput{
		Name: "Mystery Protein", Category: "other", Nutrients: map[string]float64{service.NutrientProtein: 80},
	})
	if err != nil {
		t.Fatalf("create ingredient: %v", err)
	}
	unknownMeal, err := meals.Create(context.Background(), owner, service.CreateMealInput{Name: "Shake", Servings: 1})
	if err != nil {
		t.Fatalf("create meal: %v", err)
	}
	if _, err := meals.ReplaceIngredients(context.Background(), owner, unknownMeal.ID, []service.MealIngredientInput{
		{IngredientID: protein.ID, Quantity: 100, Unit: "g"},
	}); err != nil {
		t.Fatalf("ReplaceIngredients: %v", err)
	}
	if _, err := plan.SetEntry(context.Background(), owner, day2, "lunch", service.SetPlanEntryInput{MealID: unknownMeal.ID, Portion: 1}); err != nil {
		t.Fatalf("SetEntry day2: %v", err)
	}

	rng, err := plan.GetRange(context.Background(), owner, day1, day2)
	if err != nil {
		t.Fatalf("GetRange: %v", err)
	}
	if len(rng.Days) != 2 {
		t.Fatalf("Days = %+v, want 2 (day1 and day2)", rng.Days)
	}
	if got := rng.Days[0].NutritionPerDay[service.NutrientCalories]; got != 520 {
		t.Errorf("day1 calories = %v, want 520", got)
	}
	if _, ok := rng.Days[1].NutritionPerDay[service.NutrientCalories]; ok {
		t.Errorf("day2 calories = %v, want absent (unknownMeal has no calories data)", rng.Days[1].NutritionPerDay[service.NutrientCalories])
	}
}

func TestPlanGetRangeIncludesEmptyDaysAndRejectsATooLongRange(t *testing.T) {
	plan, _, _, st := newPlanFixture(t)
	owner := newTestUser(t, st, "planowner6@example.com")

	from := time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC)
	to := from.AddDate(0, 0, 3)
	rng, err := plan.GetRange(context.Background(), owner, from, to)
	if err != nil {
		t.Fatalf("GetRange: %v", err)
	}
	if len(rng.Days) != 4 {
		t.Fatalf("Days = %d, want 4 (from..to inclusive, all empty)", len(rng.Days))
	}
	for _, d := range rng.Days {
		if len(d.Entries) != 0 {
			t.Errorf("day %v entries = %+v, want none", d.Date, d.Entries)
		}
		if got := d.NutritionPerDay[service.NutrientCalories]; got != 0 {
			t.Errorf("day %v calories = %v, want 0", d.Date, got)
		}
	}

	_, err = plan.GetRange(context.Background(), owner, from, from.AddDate(0, 0, 93))
	if !errors.Is(err, service.ErrPlanRangeTooLong) {
		t.Errorf("a 93-day range: err = %v, want ErrPlanRangeTooLong", err)
	}
}
