package service_test

import (
	"context"
	"errors"
	"slices"
	"sync"
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

// TestDietTemplatesReplaceSlotsLeavesExistingSlotsUntouchedOnFailure is
// finding #5's fix: the failure case must be one the plan's
// validate-before-write design raises *after* the DELETE and partial
// inserts have already run (ErrDuplicateSlot, from the
// template_slots_unique_slot_idx violation), not ErrDayIndexOutOfRange,
// which is raised before the DELETE and so proves nothing about rollback —
// the test would pass identically even without a transaction.
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
		{DayIndex: 0, Slot: "lunch", MealID: meal.ID, Portion: 1}, // duplicate non-snack slot: fails on insert, after the DELETE already ran
	})
	if !errors.Is(err, service.ErrDuplicateSlot) {
		t.Fatalf("err = %v, want ErrDuplicateSlot", err)
	}

	got, err := tpls.Get(context.Background(), owner, tpl.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if len(got.Slots) != 1 || got.Slots[0].Slot != "breakfast" || got.Slots[0].MealID != meal.ID {
		t.Errorf("slots after failed ReplaceSlots = %+v, want the original 1 breakfast slot untouched", got.Slots)
	}
}

// TestDietTemplatesReplaceSlotsBumpsUpdatedAt pins the second half of
// finding #1: ReplaceSlots now goes through TouchDietTemplateForUser (an
// UPDATE), not a plain SELECT, so it must advance diet_templates.updated_at
// like every other write, unlike before this fix when the slot list could
// change without updated_at moving.
func TestDietTemplatesReplaceSlotsBumpsUpdatedAt(t *testing.T) {
	tpls, meals, _, st := newDietTemplatesFixture(t)
	owner := newTestUser(t, st, "touch-template@example.com")
	meal := mustCreateMeal(t, meals, owner, "Touch")

	tpl, err := tpls.Create(context.Background(), owner, service.CreateDietTemplateInput{Name: "Touch", DayCount: 1})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	before := tpl.UpdatedAt

	after, err := tpls.ReplaceSlots(context.Background(), owner, tpl.ID, []service.TemplateSlotInput{
		{DayIndex: 0, Slot: "breakfast", MealID: meal.ID, Portion: 1},
	})
	if err != nil {
		t.Fatalf("ReplaceSlots: %v", err)
	}
	if !after.UpdatedAt.After(before) {
		t.Errorf("UpdatedAt after ReplaceSlots = %v, want after the pre-replace value %v", after.UpdatedAt, before)
	}
}

// TestDietTemplatesReplaceSlotsConcurrentCallsOnTheSameTemplateDoNotRace is
// finding #1's regression test: before the fix, ReplaceSlots read the
// template with a plain SELECT and took no row lock, so two concurrent slot
// replaces on the same template could interleave their DELETE+INSERT under
// READ COMMITTED — the second transaction's DELETE would block on the
// first's rows, then skip them once the first committed, and never see the
// first transaction's newly-inserted rows, so it would add its own rows on
// top (or hit an unexpected duplicate_slot error for a request that was
// valid on its own). Firing several concurrent calls and asserting none
// returns an unexpected error, and that the template ends with exactly the
// last-applied slot list, catches that regression; TouchDietTemplateForUser's
// row lock now serializes them instead.
func TestDietTemplatesReplaceSlotsConcurrentCallsOnTheSameTemplateDoNotRace(t *testing.T) {
	tpls, meals, _, st := newDietTemplatesFixture(t)
	owner := newTestUser(t, st, "race-template@example.com")
	meal := mustCreateMeal(t, meals, owner, "Race")

	tpl, err := tpls.Create(context.Background(), owner, service.CreateDietTemplateInput{Name: "Race", DayCount: 1})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	slots := []service.TemplateSlotInput{{DayIndex: 0, Slot: "breakfast", MealID: meal.ID, Portion: 1}}

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
			_, err := tpls.ReplaceSlots(context.Background(), owner, tpl.ID, slots)
			results[idx] = err
		}(i)
	}
	close(barrier)
	wg.Wait()

	for i, err := range results {
		if err != nil {
			t.Errorf("goroutine %d: ReplaceSlots returned an unexpected error: %v", i, err)
		}
	}

	// The template must still have exactly the one breakfast slot, not
	// duplicates or a corrupted list from an interleaved write.
	got, err := tpls.Get(context.Background(), owner, tpl.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if len(got.Slots) != 1 || got.Slots[0].MealID != meal.ID || got.Slots[0].Slot != "breakfast" {
		t.Errorf("Slots after concurrent replaces = %+v, want exactly one breakfast slot", got.Slots)
	}
}

// TestDietTemplatesReplaceSlotsReturnsSlotsInReadOrder pins ReplaceSlots's
// response order to GetTemplateSlots' ORDER BY day_index, then meal-time
// position — the order Get, Update and Apply already use — so a client
// that PUTs its slots out of order does not get one order back from the PUT
// and a different one on the next GET. Covers all four slot kinds
// (including a day with two snacks) because meal-time position sorts
// differently from plain alphabetical "slot" text (which would put dinner
// before lunch) — a test using only breakfast/lunch would not catch that.
func TestDietTemplatesReplaceSlotsReturnsSlotsInReadOrder(t *testing.T) {
	tpls, meals, _, st := newDietTemplatesFixture(t)
	owner := newTestUser(t, st, "planner10@example.com")
	meal := mustCreateMeal(t, meals, owner, "Meal")

	tpl, err := tpls.Create(context.Background(), owner, service.CreateDietTemplateInput{Name: "Three Days", DayCount: 3})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	put, err := tpls.ReplaceSlots(context.Background(), owner, tpl.ID, []service.TemplateSlotInput{
		{DayIndex: 2, Slot: "lunch", MealID: meal.ID, Portion: 1},
		{DayIndex: 0, Slot: "dinner", MealID: meal.ID, Portion: 1},
		{DayIndex: 0, Slot: "snack", MealID: meal.ID, Portion: 1},
		{DayIndex: 0, Slot: "breakfast", MealID: meal.ID, Portion: 1},
		{DayIndex: 0, Slot: "snack", MealID: meal.ID, Portion: 1},
		{DayIndex: 0, Slot: "lunch", MealID: meal.ID, Portion: 1},
	})
	if err != nil {
		t.Fatalf("ReplaceSlots: %v", err)
	}

	type pos struct {
		day  int
		slot string
	}
	// Meal-time order within day 0 (breakfast, lunch, dinner, then both
	// snacks), not plain alphabetical order (which would put dinner before
	// lunch), followed by day 2's lunch.
	want := []pos{
		{0, "breakfast"}, {0, "lunch"}, {0, "dinner"}, {0, "snack"}, {0, "snack"},
		{2, "lunch"},
	}
	order := func(slots []service.TemplateSlot) []pos {
		out := make([]pos, len(slots))
		for i, sl := range slots {
			out[i] = pos{sl.DayIndex, sl.Slot}
		}
		return out
	}
	if len(put.Slots) != len(want) {
		t.Fatalf("ReplaceSlots returned %d slots, want %d", len(put.Slots), len(want))
	}
	if got := order(put.Slots); !slices.Equal(got, want) {
		t.Errorf("ReplaceSlots order = %+v, want %+v (day_index, then meal-time position)", got, want)
	}

	read, err := tpls.Get(context.Background(), owner, tpl.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got := order(read.Slots); !slices.Equal(got, order(put.Slots)) {
		t.Errorf("Get order = %+v, want the same order ReplaceSlots returned (%+v)", got, order(put.Slots))
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

// TestDietTemplatesApplyConcurrentCallsOnOverlappingDatesConflictInsteadOfErroring
// is finding #2's regression test: before the fix, two concurrent
// POST /diet-templates/{id}/apply calls (e.g. a double-tapped Apply button)
// targeting the same dates both passed the pre-write conflict check, and
// the loser's InsertPlanEntry then blocked on plan_entries_unique_slot_idx
// and failed with a raw, untranslated 23505 (a 500). Firing several
// concurrent Apply calls at the same template/start date and asserting
// every result is either a successful write or the ErrPlanConflict
// sentinel — never any other error — catches that regression.
func TestDietTemplatesApplyConcurrentCallsOnOverlappingDatesConflictInsteadOfErroring(t *testing.T) {
	tpls, meals, _, st := newDietTemplatesFixture(t)
	owner := newTestUser(t, st, "planner8@example.com")
	meal := mustCreateMeal(t, meals, owner, "Meal")

	tpl, err := tpls.Create(context.Background(), owner, service.CreateDietTemplateInput{Name: "One Day", DayCount: 1})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, err := tpls.ReplaceSlots(context.Background(), owner, tpl.ID, []service.TemplateSlotInput{
		{DayIndex: 0, Slot: "breakfast", MealID: meal.ID, Portion: 1},
	}); err != nil {
		t.Fatalf("ReplaceSlots: %v", err)
	}
	start := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)

	const numGoroutines = 8
	var (
		wg        sync.WaitGroup
		barrier   = make(chan struct{})
		writeErrs = make([]error, numGoroutines)
		written   = make([]int, numGoroutines)
	)
	wg.Add(numGoroutines)
	for i := 0; i < numGoroutines; i++ {
		go func(idx int) {
			defer wg.Done()
			<-barrier // release all goroutines at once
			n, err := tpls.Apply(context.Background(), owner, tpl.ID, service.ApplyTemplateInput{StartDate: start})
			written[idx] = n
			writeErrs[idx] = err
		}(i)
	}
	close(barrier)
	wg.Wait()

	succeeded := 0
	for i, err := range writeErrs {
		switch {
		case err == nil:
			succeeded++
			if written[i] != 1 {
				t.Errorf("goroutine %d: Apply wrote %d entries, want 1", i, written[i])
			}
		case errors.Is(err, service.ErrPlanConflict):
			// Expected for every loser of the race.
		default:
			t.Errorf("goroutine %d: Apply returned an unexpected error: %v", i, err)
		}
	}
	if succeeded != 1 {
		t.Errorf("successful Apply calls = %d, want exactly 1 (one winner, the rest ErrPlanConflict)", succeeded)
	}

	rows, err := st.GetPlanEntriesForUserInRange(context.Background(), sqlc.GetPlanEntriesForUserInRangeParams{
		UserID: owner, FromDate: pgtype.Date{Time: start, Valid: true}, ToDate: pgtype.Date{Time: start, Valid: true},
	})
	if err != nil {
		t.Fatalf("GetPlanEntriesForUserInRange: %v", err)
	}
	if len(rows) != 1 {
		t.Errorf("plan entries after concurrent applies = %d, want exactly 1 (no duplicate from a half-applied loser)", len(rows))
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

// sharedTemplate creates a template owned by owner with the given slots and
// shared_with_partner set to shared.
func sharedTemplate(t *testing.T, tpls *service.DietTemplates, owner uuid.UUID, name string, shared bool, slots []service.TemplateSlotInput) service.DietTemplate {
	t.Helper()
	ctx := context.Background()
	tpl, err := tpls.Create(ctx, owner, service.CreateDietTemplateInput{Name: name, DayCount: 2, SharedWithPartner: shared})
	if err != nil {
		t.Fatalf("Create template %q: %v", name, err)
	}
	tpl, err = tpls.ReplaceSlots(ctx, owner, tpl.ID, slots)
	if err != nil {
		t.Fatalf("ReplaceSlots %q: %v", name, err)
	}
	return tpl
}

func TestDietTemplatesPartnerReadsOnlySharedTemplatesAndNeverWritesThem(t *testing.T) {
	tpls, meals, _, st := newDietTemplatesFixture(t)
	ctx := context.Background()
	alice := newTestUser(t, st, "alice30@example.com")
	bob := newTestUser(t, st, "bob30@example.com")
	carol := newTestUser(t, st, "carol30@example.com")
	partners := linkPartners(t, st, alice, bob)

	toast := mustCreateMeal(t, meals, alice, "Toast") // not shared on its own
	shared := sharedTemplate(t, tpls, alice, "Shared Week", true, []service.TemplateSlotInput{
		{DayIndex: 0, Slot: "breakfast", MealID: toast.ID, Portion: 1},
	})
	private := sharedTemplate(t, tpls, alice, "Private Week", false, []service.TemplateSlotInput{
		{DayIndex: 0, Slot: "breakfast", MealID: toast.ID, Portion: 1},
	})

	got, err := tpls.Get(ctx, bob, shared.ID)
	if err != nil {
		t.Fatalf("partner Get of a shared template: %v", err)
	}
	if got.OwnerID != alice || len(got.Slots) != 1 || got.Slots[0].MealID != toast.ID || got.Slots[0].MealName != "Toast" {
		t.Errorf("partner sees %+v, want Alice's template whose slot shows the meal id and name", got)
	}
	// Sharing the template shares what its slots show, not the meal itself.
	if _, err := meals.Get(ctx, bob, toast.ID); !errors.Is(err, service.ErrMealNotFound) {
		t.Errorf("partner Get of the unshared meal in a shared template: err = %v, want ErrMealNotFound", err)
	}

	newName := "Hijacked"
	for label, err := range map[string]error{
		"Get of an unshared template": errOf(tpls.Get(ctx, bob, private.ID)),
		"Get by a stranger":           errOf(tpls.Get(ctx, carol, shared.ID)),
		"Update":                      errOf(tpls.Update(ctx, bob, shared.ID, service.UpdateDietTemplateInput{Name: &newName})),
		"Delete":                      tpls.Delete(ctx, bob, shared.ID),
		"ReplaceSlots":                errOf(tpls.ReplaceSlots(ctx, bob, shared.ID, nil)),
		"Apply":                       errOf(tpls.Apply(ctx, bob, shared.ID, service.ApplyTemplateInput{StartDate: time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)})),
	} {
		if !errors.Is(err, service.ErrDietTemplateNotFound) {
			t.Errorf("%s: err = %v, want ErrDietTemplateNotFound", label, err)
		}
	}

	if own, err := tpls.List(ctx, bob, service.ListDietTemplatesInput{Limit: 10}); err != nil || len(own.Items) != 0 {
		t.Errorf("the partner's own List = %+v (err %v), want empty: shared templates are not mixed in", own.Items, err)
	}
	page, err := tpls.ListPartner(ctx, bob, service.ListDietTemplatesInput{Limit: 10})
	if err != nil || len(page.Items) != 1 || page.Items[0].ID != shared.ID {
		t.Errorf("ListPartner = %+v (err %v), want only the shared template", page.Items, err)
	}
	if _, err := tpls.ListPartner(ctx, carol, service.ListDietTemplatesInput{Limit: 10}); !errors.Is(err, service.ErrPartnerNotLinked) {
		t.Errorf("ListPartner without a partner: err = %v, want ErrPartnerNotLinked", err)
	}

	if err := partners.Unlink(ctx, bob); err != nil {
		t.Fatalf("Unlink: %v", err)
	}
	if _, err := tpls.Get(ctx, bob, shared.ID); !errors.Is(err, service.ErrDietTemplateNotFound) {
		t.Errorf("partner Get after unlinking: err = %v, want ErrDietTemplateNotFound", err)
	}
}

func TestDietTemplatesCopyOfAPartnersTemplateCopiesItsMealsOnce(t *testing.T) {
	tpls, meals, ing, st := newDietTemplatesFixture(t)
	ctx := context.Background()
	alice := newTestUser(t, st, "alice31@example.com")
	bob := newTestUser(t, st, "bob31@example.com")
	partners := linkPartners(t, st, alice, bob)

	flour := mustCreateIngredient(t, ing, alice, service.CreateIngredientInput{
		Name: "Alice's Flour", Category: "grains_bread", Nutrients: map[string]float64{service.NutrientCalories: 360},
	})
	makeMeal := func(name string) service.Meal {
		t.Helper()
		m := mustCreateMeal(t, meals, alice, name)
		m, err := meals.ReplaceIngredients(ctx, alice, m.ID, []service.MealIngredientInput{{IngredientID: flour.ID, Quantity: 100, Unit: "g"}})
		if err != nil {
			t.Fatalf("ReplaceIngredients %q: %v", name, err)
		}
		return m
	}
	pancakes, bread := makeMeal("Pancakes"), makeMeal("Bread")
	original := sharedTemplate(t, tpls, alice, "Baking Week", true, []service.TemplateSlotInput{
		{DayIndex: 0, Slot: "breakfast", MealID: pancakes.ID, Portion: 1},
		{DayIndex: 0, Slot: "dinner", MealID: bread.ID, Portion: 1.5},
		{DayIndex: 1, Slot: "breakfast", MealID: pancakes.ID, Portion: 2},
	})

	cp, err := tpls.Copy(ctx, bob, original.ID)
	if err != nil {
		t.Fatalf("partner Copy: %v", err)
	}
	if cp.OwnerID != bob || cp.SharedWithPartner || cp.Name != "Baking Week" || cp.DayCount != 2 || len(cp.Slots) != 3 {
		t.Fatalf("copy = %+v, want Bob's private 2-day copy with 3 slots", cp)
	}
	// Slots come back ordered by day, then meal time: breakfast d0, dinner d0, breakfast d1.
	first, second, third := cp.Slots[0], cp.Slots[1], cp.Slots[2]
	if first.MealID == pancakes.ID || second.MealID == bread.ID {
		t.Errorf("copy slots still point at Alice's meals: %+v", cp.Slots)
	}
	if first.MealID != third.MealID {
		t.Errorf("the pancakes fill two slots but were copied to %v and %v, want one copy", first.MealID, third.MealID)
	}
	if first.MealName != "Pancakes" || second.MealName != "Bread" || second.Portion != 1.5 || third.Portion != 2 {
		t.Errorf("copy slots = %+v, want names and portions carried over", cp.Slots)
	}

	// Bob owns exactly two new private meals, sharing one duplicated ingredient.
	page, err := meals.List(ctx, bob, service.ListMealsInput{Limit: 10})
	if err != nil || len(page.Items) != 2 {
		t.Fatalf("Bob's meals = %+v (err %v), want exactly the 2 copies", page.Items, err)
	}
	var flourIDs []uuid.UUID
	for _, item := range page.Items {
		if item.SharedWithPartner {
			t.Errorf("copied meal %q is shared, want private", item.Name)
		}
		m, err := meals.Get(ctx, bob, item.ID)
		if err != nil || len(m.Ingredients) != 1 || m.Ingredients[0].IngredientID == flour.ID {
			t.Fatalf("copied meal %q = %+v (err %v), want one line on a duplicated flour", item.Name, m.Ingredients, err)
		}
		flourIDs = append(flourIDs, m.Ingredients[0].IngredientID)
	}
	if flourIDs[0] != flourIDs[1] {
		t.Errorf("the two copied meals use flour %v and %v, want one duplicate shared across the template copy", flourIDs[0], flourIDs[1])
	}

	// Bob can plan with it: Apply needs meals Bob owns.
	written, err := tpls.Apply(ctx, bob, cp.ID, service.ApplyTemplateInput{StartDate: time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)})
	if err != nil || written != 3 {
		t.Errorf("Apply of the copy = %d entries (err %v), want 3", written, err)
	}

	// Independent of the original: Alice can delete her template and meals.
	if err := partners.Unlink(ctx, alice); err != nil {
		t.Fatalf("Unlink: %v", err)
	}
	if err := tpls.Delete(ctx, alice, original.ID); err != nil {
		t.Fatalf("delete original template: %v", err)
	}
	if err := meals.Delete(ctx, alice, pancakes.ID); err != nil {
		t.Fatalf("delete original meal: %v", err)
	}
	if again, err := tpls.Get(ctx, bob, cp.ID); err != nil || len(again.Slots) != 3 {
		t.Errorf("copy after the original was deleted = %+v (err %v), want it untouched", again.Slots, err)
	}
}

func TestDietTemplatesCopyOfAnOwnTemplateKeepsItsMeals(t *testing.T) {
	tpls, meals, _, st := newDietTemplatesFixture(t)
	ctx := context.Background()
	owner := newTestUser(t, st, "owner32@example.com")
	meal := mustCreateMeal(t, meals, owner, "Toast")
	original := sharedTemplate(t, tpls, owner, "Mine", true, []service.TemplateSlotInput{
		{DayIndex: 0, Slot: "breakfast", MealID: meal.ID, Portion: 1},
	})

	cp, err := tpls.Copy(ctx, owner, original.ID)
	if err != nil {
		t.Fatalf("Copy: %v", err)
	}
	if len(cp.Slots) != 1 || cp.Slots[0].MealID != meal.ID {
		t.Errorf("own copy slots = %+v, want the same meal %v", cp.Slots, meal.ID)
	}
	if page, err := meals.List(ctx, owner, service.ListMealsInput{Limit: 10}); err != nil || len(page.Items) != 1 {
		t.Errorf("meals after an own copy = %+v (err %v), want still 1", page.Items, err)
	}
}
