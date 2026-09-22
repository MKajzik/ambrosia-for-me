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

func newDietTemplatesFixture(t *testing.T) (*service.DietTemplates, *service.Meals, *service.Ingredients, *store.Store) {
	t.Helper()
	_, st := newIngredientsFixture(t)
	return service.NewDietTemplates(st), service.NewMeals(st), service.NewIngredients(st), st
}

func mustCreateMeal(t *testing.T, meals *service.Meals, owner uuid.UUID, name string) service.Meal {
	t.Helper()
	m, err := meals.Create(context.Background(), owner, service.CreateMealInput{Name: name, Servings: 1})
	if err != nil {
		t.Fatalf("create meal %q: %v", name, err)
	}
	return m
}

func TestDietTemplatesReplaceSlotsRejectsDayIndexPastDayCount(t *testing.T) {
	tpls, meals, _, st := newDietTemplatesFixture(t)
	owner := newTestUser(t, st, "planner1@example.com")
	meal := mustCreateMeal(t, meals, owner, "Toast")

	tpl, err := tpls.Create(context.Background(), owner, service.CreateDietTemplateInput{Name: "Two Days", DayCount: 2})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	_, err = tpls.ReplaceSlots(context.Background(), owner, tpl.ID, []service.TemplateSlotInput{
		{DayIndex: 2, Slot: "breakfast", MealID: meal.ID, Portion: 1},
	})
	if !errors.Is(err, service.ErrDayIndexOutOfRange) {
		t.Errorf("err = %v, want ErrDayIndexOutOfRange (day_count is 2, valid indexes are 0 and 1)", err)
	}
}

func TestDietTemplatesReplaceSlotsRejectsAMealNotOwnedByTheCaller(t *testing.T) {
	tpls, meals, _, st := newDietTemplatesFixture(t)
	owner := newTestUser(t, st, "planner2@example.com")
	other := newTestUser(t, st, "other2@example.com")
	othersMeal := mustCreateMeal(t, meals, other, "Not Mine")

	tpl, err := tpls.Create(context.Background(), owner, service.CreateDietTemplateInput{Name: "Week", DayCount: 7})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	_, err = tpls.ReplaceSlots(context.Background(), owner, tpl.ID, []service.TemplateSlotInput{
		{DayIndex: 0, Slot: "breakfast", MealID: othersMeal.ID, Portion: 1},
	})
	if !errors.Is(err, service.ErrTemplateMealNotFound) {
		t.Errorf("err = %v, want ErrTemplateMealNotFound", err)
	}
}

func TestDietTemplatesReplaceSlotsRejectsADuplicateNonSnackSlotButAllowsTwoSnacks(t *testing.T) {
	tpls, meals, _, st := newDietTemplatesFixture(t)
	owner := newTestUser(t, st, "planner3@example.com")
	meal := mustCreateMeal(t, meals, owner, "Meal")

	tpl, err := tpls.Create(context.Background(), owner, service.CreateDietTemplateInput{Name: "Week", DayCount: 7})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	_, err = tpls.ReplaceSlots(context.Background(), owner, tpl.ID, []service.TemplateSlotInput{
		{DayIndex: 0, Slot: "breakfast", MealID: meal.ID, Portion: 1},
		{DayIndex: 0, Slot: "breakfast", MealID: meal.ID, Portion: 1},
	})
	if !errors.Is(err, service.ErrDuplicateSlot) {
		t.Errorf("err = %v, want ErrDuplicateSlot", err)
	}

	got, err := tpls.ReplaceSlots(context.Background(), owner, tpl.ID, []service.TemplateSlotInput{
		{DayIndex: 0, Slot: "snack", MealID: meal.ID, Portion: 1},
		{DayIndex: 0, Slot: "snack", MealID: meal.ID, Portion: 1},
	})
	if err != nil {
		t.Fatalf("two snacks on the same day: %v", err)
	}
	if len(got.Slots) != 2 {
		t.Errorf("Slots = %+v, want 2 snack rows", got.Slots)
	}
}

func TestDietTemplatesReplaceSlotsLeavesExistingSlotsUntouchedOnFailure(t *testing.T) {
	tpls, meals, _, st := newDietTemplatesFixture(t)
	owner := newTestUser(t, st, "planner9@example.com")
	meal := mustCreateMeal(t, meals, owner, "Meal")

	tpl, err := tpls.Create(context.Background(), owner, service.CreateDietTemplateInput{Name: "Week", DayCount: 7})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	tpl, err = tpls.ReplaceSlots(context.Background(), owner, tpl.ID, []service.TemplateSlotInput{
		{DayIndex: 0, Slot: "breakfast", MealID: meal.ID, Portion: 1},
	})
	if err != nil {
		t.Fatalf("initial ReplaceSlots: %v", err)
	}
	if len(tpl.Slots) != 1 {
		t.Fatalf("initial slots = %+v, want 1", tpl.Slots)
	}

	_, err = tpls.ReplaceSlots(context.Background(), owner, tpl.ID, []service.TemplateSlotInput{
		{DayIndex: 0, Slot: "lunch", MealID: meal.ID, Portion: 1},
		{DayIndex: 7, Slot: "dinner", MealID: meal.ID, Portion: 1}, // day_count is 7, valid indexes are 0..6
	})
	if !errors.Is(err, service.ErrDayIndexOutOfRange) {
		t.Fatalf("err = %v, want ErrDayIndexOutOfRange", err)
	}

	got, err := tpls.Get(context.Background(), owner, tpl.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if len(got.Slots) != 1 || got.Slots[0].Slot != "breakfast" || got.Slots[0].MealID != meal.ID {
		t.Errorf("slots after failed ReplaceSlots = %+v, want the original 1 breakfast slot untouched", got.Slots)
	}
}

func TestDietTemplatesCopyDuplicatesSlotsAndStartsPrivate(t *testing.T) {
	tpls, meals, _, st := newDietTemplatesFixture(t)
	owner := newTestUser(t, st, "planner4@example.com")
	meal := mustCreateMeal(t, meals, owner, "Meal")

	original, err := tpls.Create(context.Background(), owner, service.CreateDietTemplateInput{Name: "Week", DayCount: 7, SharedWithPartner: true})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	original, err = tpls.ReplaceSlots(context.Background(), owner, original.ID, []service.TemplateSlotInput{
		{DayIndex: 0, Slot: "breakfast", MealID: meal.ID, Portion: 1},
	})
	if err != nil {
		t.Fatalf("ReplaceSlots: %v", err)
	}

	copy_, err := tpls.Copy(context.Background(), owner, original.ID)
	if err != nil {
		t.Fatalf("Copy: %v", err)
	}
	if copy_.ID == original.ID {
		t.Fatal("copy has the same ID as the original")
	}
	if copy_.SharedWithPartner {
		t.Error("copy has shared_with_partner = true, want false regardless of the original")
	}
	if len(copy_.Slots) != 1 || copy_.Slots[0].MealID != meal.ID {
		t.Errorf("copy slots = %+v, want one breakfast slot", copy_.Slots)
	}
}

func TestDietTemplatesApplyWritesPlanEntriesAtTheRightDates(t *testing.T) {
	tpls, meals, _, st := newDietTemplatesFixture(t)
	owner := newTestUser(t, st, "planner5@example.com")
	meal := mustCreateMeal(t, meals, owner, "Meal")

	tpl, err := tpls.Create(context.Background(), owner, service.CreateDietTemplateInput{Name: "Two Days", DayCount: 2})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, err := tpls.ReplaceSlots(context.Background(), owner, tpl.ID, []service.TemplateSlotInput{
		{DayIndex: 0, Slot: "breakfast", MealID: meal.ID, Portion: 1},
		{DayIndex: 1, Slot: "breakfast", MealID: meal.ID, Portion: 1},
	}); err != nil {
		t.Fatalf("ReplaceSlots: %v", err)
	}

	start := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	n, err := tpls.Apply(context.Background(), owner, tpl.ID, service.ApplyTemplateInput{StartDate: start})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if n != 2 {
		t.Errorf("Apply wrote %d entries, want 2", n)
	}

	rows, err := st.GetPlanEntriesForUserInRange(context.Background(), sqlc.GetPlanEntriesForUserInRangeParams{
		UserID:   owner,
		FromDate: pgtype.Date{Time: start, Valid: true},
		ToDate:   pgtype.Date{Time: start.AddDate(0, 0, 1), Valid: true},
	})
	if err != nil {
		t.Fatalf("GetPlanEntriesForUserInRange: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("plan entries after apply = %d, want 2", len(rows))
	}
	if !rows[0].Date.Time.Equal(start) || !rows[1].Date.Time.Equal(start.AddDate(0, 0, 1)) {
		t.Errorf("plan entry dates = %v, %v, want %v, %v", rows[0].Date.Time, rows[1].Date.Time, start, start.AddDate(0, 0, 1))
	}
	for _, r := range rows {
		if r.FromTemplateID == nil || *r.FromTemplateID != tpl.ID {
			t.Errorf("plan entry from_template_id = %v, want %v", r.FromTemplateID, tpl.ID)
		}
	}
}

func TestDietTemplatesApplyWithoutOverwriteConflictsOnAnExistingNonSnackEntry(t *testing.T) {
	tpls, meals, _, st := newDietTemplatesFixture(t)
	owner := newTestUser(t, st, "planner6@example.com")
	meal := mustCreateMeal(t, meals, owner, "Meal")

	start := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	if _, err := st.InsertPlanEntry(context.Background(), sqlc.InsertPlanEntryParams{
		OwnerID: owner, Date: pgtype.Date{Time: start, Valid: true}, Slot: "breakfast", MealID: meal.ID, Portion: 1,
	}); err != nil {
		t.Fatalf("seed existing entry: %v", err)
	}

	tpl, err := tpls.Create(context.Background(), owner, service.CreateDietTemplateInput{Name: "One Day", DayCount: 1})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, err := tpls.ReplaceSlots(context.Background(), owner, tpl.ID, []service.TemplateSlotInput{
		{DayIndex: 0, Slot: "breakfast", MealID: meal.ID, Portion: 1},
	}); err != nil {
		t.Fatalf("ReplaceSlots: %v", err)
	}

	if _, err := tpls.Apply(context.Background(), owner, tpl.ID, service.ApplyTemplateInput{StartDate: start}); !errors.Is(err, service.ErrPlanConflict) {
		t.Errorf("Apply without overwrite over an existing entry: err = %v, want ErrPlanConflict", err)
	}

	n, err := tpls.Apply(context.Background(), owner, tpl.ID, service.ApplyTemplateInput{StartDate: start, Overwrite: true})
	if err != nil {
		t.Fatalf("Apply with overwrite: %v", err)
	}
	if n != 1 {
		t.Errorf("Apply with overwrite wrote %d entries, want 1", n)
	}
	rows, err := st.GetPlanEntriesForUserInRange(context.Background(), sqlc.GetPlanEntriesForUserInRangeParams{
		UserID:   owner,
		FromDate: pgtype.Date{Time: start, Valid: true},
		ToDate:   pgtype.Date{Time: start, Valid: true},
	})
	if err != nil {
		t.Fatalf("GetPlanEntriesForUserInRange: %v", err)
	}
	if len(rows) != 1 {
		t.Errorf("plan entries after overwrite apply = %d, want 1 (the old entry replaced, not duplicated)", len(rows))
	}
}

func TestDietTemplatesApplySnackSlotsNeverConflictAndAlwaysAccumulate(t *testing.T) {
	tpls, meals, _, st := newDietTemplatesFixture(t)
	owner := newTestUser(t, st, "planner7@example.com")
	meal := mustCreateMeal(t, meals, owner, "Meal")

	start := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	if _, err := st.InsertPlanEntry(context.Background(), sqlc.InsertPlanEntryParams{
		OwnerID: owner, Date: pgtype.Date{Time: start, Valid: true}, Slot: "snack", MealID: meal.ID, Portion: 1,
	}); err != nil {
		t.Fatalf("seed existing snack entry: %v", err)
	}

	tpl, err := tpls.Create(context.Background(), owner, service.CreateDietTemplateInput{Name: "One Day", DayCount: 1})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, err := tpls.ReplaceSlots(context.Background(), owner, tpl.ID, []service.TemplateSlotInput{
		{DayIndex: 0, Slot: "snack", MealID: meal.ID, Portion: 1},
	}); err != nil {
		t.Fatalf("ReplaceSlots: %v", err)
	}

	countSnacksOn := func(date time.Time) int {
		t.Helper()
		rows, err := st.GetPlanEntriesForUserInRange(context.Background(), sqlc.GetPlanEntriesForUserInRangeParams{
			UserID: owner, FromDate: pgtype.Date{Time: date, Valid: true}, ToDate: pgtype.Date{Time: date, Valid: true},
		})
		if err != nil {
			t.Fatalf("GetPlanEntriesForUserInRange: %v", err)
		}
		n := 0
		for _, r := range rows {
			if r.Slot == "snack" {
				n++
			}
		}
		return n
	}

	// Without overwrite, applying a template snack slot over an existing
	// snack must succeed (snacks never conflict) and both entries must
	// exist afterwards.
	n, err := tpls.Apply(context.Background(), owner, tpl.ID, service.ApplyTemplateInput{StartDate: start})
	if err != nil {
		t.Fatalf("Apply without overwrite over an existing snack: err = %v, want nil (snacks never conflict)", err)
	}
	if n != 1 {
		t.Errorf("Apply wrote %d entries, want 1", n)
	}
	if got := countSnacksOn(start); got != 2 {
		t.Errorf("snack entries after non-overwrite apply = %d, want 2 (the seeded one plus the applied one)", got)
	}

	// With overwrite, the snack slot is still additive: overwrite only
	// replaces non-snack targets, so applying again adds a third snack
	// rather than replacing either existing one.
	n, err = tpls.Apply(context.Background(), owner, tpl.ID, service.ApplyTemplateInput{StartDate: start, Overwrite: true})
	if err != nil {
		t.Fatalf("Apply with overwrite: %v", err)
	}
	if n != 1 {
		t.Errorf("Apply with overwrite wrote %d entries, want 1", n)
	}
	if got := countSnacksOn(start); got != 3 {
		t.Errorf("snack entries after overwrite apply = %d, want 3 (overwrite must not remove existing snacks)", got)
	}
}
