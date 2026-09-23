package service_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/InzKazik/mealplanner/backend/internal/auth"
	"github.com/InzKazik/mealplanner/backend/internal/service"
	"github.com/InzKazik/mealplanner/backend/internal/store"
)

func newPlanFixture(t *testing.T) (*service.Plan, *service.Meals, *service.Ingredients, *store.Store) {
	t.Helper()
	_, st := newIngredientsFixture(t)
	meals := service.NewMeals(st)
	return service.NewPlan(st, meals), meals, service.NewIngredients(st), st
}

// newAuthForStore builds an Auth on an existing store, so a test can delete a
// user out from under another service. Same light argon2 parameters and
// development-only token settings as auth_test.go's fixture.
func newAuthForStore(t *testing.T, st *store.Store) *service.Auth {
	t.Helper()
	now := func() time.Time { return time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC) }
	return service.NewAuth(st,
		auth.NewHasher(auth.HashParams{MemoryKiB: 8, Iterations: 1, Parallelism: 1}),
		auth.NewTokenIssuer([]byte("0123456789abcdef0123456789abcdef"), 15*time.Minute, now),
		30*24*time.Hour, now)
}

func TestPlanSetEntryUpsertsNonSnackSlotsAndClearsProvenance(t *testing.T) {
	plan, meals, _, st := newPlanFixture(t)
	tpls := service.NewDietTemplates(st)
	owner := newTestUser(t, st, "planowner1@example.com")
	meal1 := mustCreateMeal(t, meals, owner, "First")
	meal2 := mustCreateMeal(t, meals, owner, "Second")
	date := time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC)

	// The entry must have real provenance before SetEntry can meaningfully
	// clear it, so it comes from applying a template rather than from a
	// first SetEntry (which writes from_template_id = NULL itself, making
	// the assertion below unfalsifiable).
	tpl, err := tpls.Create(context.Background(), owner, service.CreateDietTemplateInput{Name: "One Day", DayCount: 1})
	if err != nil {
		t.Fatalf("create diet template: %v", err)
	}
	if _, err := tpls.ReplaceSlots(context.Background(), owner, tpl.ID, []service.TemplateSlotInput{
		{DayIndex: 0, Slot: "breakfast", MealID: meal1.ID, Portion: 1},
	}); err != nil {
		t.Fatalf("ReplaceSlots: %v", err)
	}
	if _, err := tpls.Apply(context.Background(), owner, tpl.ID, service.ApplyTemplateInput{StartDate: date}); err != nil {
		t.Fatalf("Apply: %v", err)
	}

	applied, err := plan.GetRange(context.Background(), owner, date, date)
	if err != nil {
		t.Fatalf("GetRange (after Apply): %v", err)
	}
	if len(applied.Days) != 1 || len(applied.Days[0].Entries) != 1 {
		t.Fatalf("Days after Apply = %+v, want exactly one day with exactly one entry", applied.Days)
	}
	first := applied.Days[0].Entries[0]
	if first.FromTemplateID == nil || *first.FromTemplateID != tpl.ID {
		t.Fatalf("FromTemplateID after Apply = %v, want the applied template's id %v", first.FromTemplateID, tpl.ID)
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
		t.Errorf("FromTemplateID = %v, want nil after a manual set over a template-applied entry", second.FromTemplateID)
	}

	rng, err := plan.GetRange(context.Background(), owner, date, date)
	if err != nil {
		t.Fatalf("GetRange: %v", err)
	}
	if len(rng.Days) != 1 || len(rng.Days[0].Entries) != 1 {
		t.Fatalf("Days = %+v, want exactly one day with exactly one entry", rng.Days)
	}
	if got := rng.Days[0].Entries[0].FromTemplateID; got != nil {
		t.Errorf("FromTemplateID on re-read = %v, want nil", got)
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
	if _, err := plan.SetEntry(context.Background(), owner, day1, "snack", service.SetPlanEntryInput{MealID: riceMeal.ID, Portion: 0.5}); err != nil {
		t.Fatalf("SetEntry day1 snack: %v", err)
	}
	// day1 total calories: summed across both entries on the day, not just
	// the first one: 260*2 (breakfast) + 260*0.5 (snack) = 520 + 130 = 650.

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
	if len(rng.Days[0].Entries) != 2 {
		t.Fatalf("day1 entries = %+v, want 2 (breakfast and snack)", rng.Days[0].Entries)
	}
	if got := rng.Days[0].NutritionPerDay[service.NutrientCalories]; got != 650 {
		t.Errorf("day1 calories = %v, want 650 (summed across both of day1's entries)", got)
	}
	if _, ok := rng.Days[1].NutritionPerDay[service.NutrientCalories]; ok {
		t.Errorf("day2 calories = %v, want absent (unknownMeal has no calories data)", rng.Days[1].NutritionPerDay[service.NutrientCalories])
	}
	// day2's calories being absent must not mean every key was nulled out:
	// protein is known for unknownMeal's one ingredient, so it must still
	// come through with the correct summed value.
	if got := rng.Days[1].NutritionPerDay[service.NutrientProtein]; got != 80 {
		t.Errorf("day2 protein = %v, want 80 (unknownMeal's one known key, summed correctly)", got)
	}
}

// allNutrientKeysForTest is the fixed 18-key set NutritionPerDay must report
// for every day, empty or not (see backend/CLAUDE.md's note on null
// propagation one level up). Listed locally because allNutrientKeys itself
// is unexported and this is an external (service_test) test package.
var allNutrientKeysForTest = []string{
	service.NutrientCalories, service.NutrientProtein, service.NutrientCarbohydrates, service.NutrientSugar,
	service.NutrientFibre, service.NutrientFat, service.NutrientSaturatedFat, service.NutrientSodium,
	service.NutrientPotassium, service.NutrientCalcium, service.NutrientIron, service.NutrientMagnesium,
	service.NutrientZinc, service.NutrientVitaminA, service.NutrientVitaminC, service.NutrientVitaminD,
	service.NutrientVitaminB12, service.NutrientFolate,
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
		// Every one of the 18 keys must be present and 0, not merely absent
		// (a Go map read of a missing key also returns 0, so checking a
		// single key by plain index cannot tell "reports 0" from "reports
		// nothing at all" apart).
		if len(d.NutritionPerDay) != len(allNutrientKeysForTest) {
			t.Errorf("day %v NutritionPerDay has %d keys, want %d", d.Date, len(d.NutritionPerDay), len(allNutrientKeysForTest))
		}
		for _, k := range allNutrientKeysForTest {
			v, ok := d.NutritionPerDay[k]
			if !ok || v != 0 {
				t.Errorf("day %v NutritionPerDay[%q] = (%v, present=%v), want (0, present=true)", d.Date, k, v, ok)
			}
		}
	}

	_, err = plan.GetRange(context.Background(), owner, from, from.AddDate(0, 0, 93))
	if !errors.Is(err, service.ErrPlanRangeTooLong) {
		t.Errorf("a 93-day range: err = %v, want ErrPlanRangeTooLong", err)
	}
}

// TestPlanGetRangeCapsAt92DaysTotalInclusive is finding #7's boundary test:
// the cap is 92 calendar days in [from, to] inclusive (matching the
// "Capped at 92 days" description on GET /plan in openapi.yaml), so a range
// of from..from+91 (92 days total) must succeed and from..from+92 (93 days
// total) must fail — the previous `> 92` check on the day *difference*
// allowed one extra day (93 total) before rejecting.
func TestPlanGetRangeCapsAt92DaysTotalInclusive(t *testing.T) {
	plan, _, _, st := newPlanFixture(t)
	owner := newTestUser(t, st, "planowner8@example.com")
	from := time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC)

	rng, err := plan.GetRange(context.Background(), owner, from, from.AddDate(0, 0, 91))
	if err != nil {
		t.Fatalf("a 92-day range (from..from+91): unexpected err = %v", err)
	}
	if len(rng.Days) != 92 {
		t.Errorf("Days = %d, want 92", len(rng.Days))
	}

	_, err = plan.GetRange(context.Background(), owner, from, from.AddDate(0, 0, 92))
	if !errors.Is(err, service.ErrPlanRangeTooLong) {
		t.Errorf("a 93-day range (from..from+92): err = %v, want ErrPlanRangeTooLong", err)
	}
}

// TestPlanGetRangeRejectsAReversedRangeWithADistinctError is finding #7's
// second fix: to < from is a malformed range, not one that is too long, so
// it must not return the misleading ErrPlanRangeTooLong.
func TestPlanGetRangeRejectsAReversedRangeWithADistinctError(t *testing.T) {
	plan, _, _, st := newPlanFixture(t)
	owner := newTestUser(t, st, "planowner9@example.com")
	from := time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC)

	_, err := plan.GetRange(context.Background(), owner, from, from.AddDate(0, 0, -1))
	if !errors.Is(err, service.ErrPlanRangeInvalid) {
		t.Errorf("to before from: err = %v, want ErrPlanRangeInvalid (not the too-long error)", err)
	}
}

// TestPlanGetRangeForADeletedUserIsNotFound guards the deleted-account
// window: access tokens are stateless for up to 15 minutes after DELETE /me,
// so a still-valid token can reach GetRange after the user row is gone.
// GetRange reads the user row directly for the caller's targets, and must
// translate the missing row into ErrNotFound (401) like Auth.GetUser does,
// not a wrapped store error (500).
func TestPlanGetRangeForADeletedUserIsNotFound(t *testing.T) {
	plan, _, _, st := newPlanFixture(t)
	auths := newAuthForStore(t, st)
	owner := newTestUser(t, st, "planowner7@example.com")

	date := time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC)
	if _, err := plan.GetRange(context.Background(), owner, date, date); err != nil {
		t.Fatalf("GetRange before deletion: %v", err)
	}
	if err := auths.DeleteUser(context.Background(), owner); err != nil {
		t.Fatalf("DeleteUser: %v", err)
	}

	_, err := plan.GetRange(context.Background(), owner, date, date)
	if !errors.Is(err, service.ErrNotFound) {
		t.Errorf("GetRange after the user was deleted: err = %v, want service.ErrNotFound", err)
	}
}
