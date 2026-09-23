// Package service holds business rules. It calls the store and never speaks HTTP.
package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/InzKazik/mealplanner/backend/internal/store"
	"github.com/InzKazik/mealplanner/backend/internal/store/sqlc"
)

const maxPlanRangeDays = 92

// Errors returned by Plan. Handlers map them to problem responses.
var (
	ErrPlanEntryNotFound = errors.New("plan entry not found")
	ErrPlanMealNotFound  = errors.New("meal does not exist or is not visible to you")
	// ErrPlanRangeTooLong means [from, to] spans more than maxPlanRangeDays
	// calendar days in total (from and to both inclusive). There is no
	// growing list here to cursor-paginate (§4.2 reserves that for
	// meals/ingredients/diet-templates), so this bounds the request size
	// instead; OpenAPI's JSON Schema can't express a cross-field constraint
	// between from and to, so it is enforced here.
	ErrPlanRangeTooLong = errors.New("date range is too long")
	// ErrPlanRangeInvalid means to is before from: a malformed range, not
	// one that is simply too long, so it gets its own error rather than
	// reusing ErrPlanRangeTooLong's misleading message for this case.
	ErrPlanRangeInvalid = errors.New("to must not be before from")
)

// PlanEntry is one scheduled meal on the calendar, with the referenced
// meal's name inlined.
type PlanEntry struct {
	ID             uuid.UUID
	Date           time.Time
	Slot           string
	MealID         uuid.UUID
	MealName       string
	Portion        float64
	FromTemplateID *uuid.UUID
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

// DailyTotal is one calendar date's entries plus computed nutrition, using
// the same null-propagation rule as a single meal one level up: a key is
// absent if any contributing entry's meal has it absent. A date with no
// entries reports 0 for every key.
type DailyTotal struct {
	Date            time.Time
	Entries         []PlanEntry
	NutritionPerDay map[string]float64
}

// Targets are the caller's daily nutrition goals, straight from their
// profile (nil where unset). Comparing totals against targets is a client
// concern; this just puts both in the same response.
type Targets struct {
	Kcal     *float64
	ProteinG *float64
	CarbsG   *float64
	FatG     *float64
}

// PlanRange is [From, To] inclusive, one DailyTotal per calendar date, plus
// the caller's targets.
type PlanRange struct {
	From    time.Time
	To      time.Time
	Days    []DailyTotal
	Targets Targets
}

// SetPlanEntryInput is the meal and portion for one date+slot.
type SetPlanEntryInput struct {
	MealID  uuid.UUID
	Portion float64
}

// Plan implements the calendar: which meal is scheduled for which date and
// slot, and the nutrition totals that follow from it. Unlike every other
// service in this package, it depends on Meals rather than reading meal rows
// via store directly — specifically to reuse Meals.Get's nutrition
// computation instead of reimplementing unit conversion and
// null-propagation a third time. See the diets-and-plan plan's Global
// Constraints for the reasoning.
type Plan struct {
	st    *store.Store
	meals *Meals
}

// NewPlan returns a Plan service.
func NewPlan(st *store.Store, meals *Meals) *Plan { return &Plan{st: st, meals: meals} }

// GetRange returns one DailyTotal per calendar date in [from, to]
// inclusive, capped at maxPlanRangeDays calendar days in total (from and to
// both inclusive) — so to may be at most from+(maxPlanRangeDays-1).
//
// Deviation from the brief: plan_entries.date is sqlc.PlanEntry.Date of
// type pgtype.Date, not time.Time as the brief's draft assumed (see
// util.go's toPgDate/fromPgDate doc comment, and diet_templates.go's Apply
// for the established pattern) — every date value crossing into a sqlc call
// or read back from one is converted at that boundary below; the
// service-level aggregation logic is unchanged from the brief.
func (s *Plan) GetRange(ctx context.Context, ownerID uuid.UUID, from, to time.Time) (PlanRange, error) {
	if to.Before(from) {
		return PlanRange{}, ErrPlanRangeInvalid
	}
	// diffDays+1 is the total number of calendar days in [from, to]
	// inclusive; reject once that total would exceed maxPlanRangeDays (so
	// to may be at most from+(maxPlanRangeDays-1), i.e. diffDays must stay
	// below maxPlanRangeDays).
	if diffDays := int(to.Sub(from).Hours() / 24); diffDays >= maxPlanRangeDays {
		return PlanRange{}, ErrPlanRangeTooLong
	}

	rows, err := s.st.GetPlanEntriesForUserInRange(ctx, sqlc.GetPlanEntriesForUserInRangeParams{
		UserID: ownerID, FromDate: toPgDate(from), ToDate: toPgDate(to),
	})
	if err != nil {
		return PlanRange{}, fmt.Errorf("get plan entries: %w", err)
	}

	mealIDs := uniqueUUIDs(rows, func(r sqlc.PlanEntry) uuid.UUID { return r.MealID })
	mealsByID := make(map[uuid.UUID]Meal, len(mealIDs))
	for _, id := range mealIDs {
		// s.meals.Get can return ErrMealNotFound here only from a race: a
		// meal referenced by a plan_entries row in this range was deleted
		// between the read above and this lookup (deleting an in-use meal
		// is normally blocked by plan_entries_meal_id_fkey, but this read
		// runs in a separate transaction with no lock spanning both).
		// Unlike Plan.SetEntry, where an unknown meal is the caller's own
		// mistake (400), a GET returning 400 because of someone else's
		// concurrent write would be confusing, so this is left as a plain
		// wrapped error (500) rather than translated to ErrPlanMealNotFound.
		m, err := s.meals.Get(ctx, ownerID, id)
		if err != nil {
			return PlanRange{}, fmt.Errorf("get meal %s: %w", id, err)
		}
		mealsByID[id] = m
	}

	byDate := make(map[string][]sqlc.PlanEntry, len(rows))
	for _, r := range rows {
		key := fromPgDate(r.Date).Format("2006-01-02")
		byDate[key] = append(byDate[key], r)
	}

	// Access tokens stay valid for up to 15 minutes after DELETE /me, so the
	// caller's row can be gone while their token still authenticates. Answer
	// that the same way Auth.GetUser does — ErrNotFound, mapped to 401 — not
	// as an unexpected store failure (500).
	user, err := s.st.GetUserByID(ctx, ownerID)
	if store.IsNotFound(err) {
		return PlanRange{}, ErrNotFound
	}
	if err != nil {
		return PlanRange{}, fmt.Errorf("get user: %w", err)
	}

	var days []DailyTotal
	for d := from; !d.After(to); d = d.AddDate(0, 0, 1) {
		dayRows := byDate[d.Format("2006-01-02")]
		entries := make([]PlanEntry, len(dayRows))
		totals := make(map[string]float64, len(allNutrientKeys))
		for _, k := range allNutrientKeys {
			totals[k] = 0
		}
		unknown := make(map[string]bool, len(allNutrientKeys))
		for i, r := range dayRows {
			m := mealsByID[r.MealID]
			entries[i] = PlanEntry{
				ID: r.ID, Date: fromPgDate(r.Date), Slot: r.Slot, MealID: r.MealID, MealName: m.Name,
				Portion: r.Portion, FromTemplateID: r.FromTemplateID, CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt,
			}
			for _, k := range allNutrientKeys {
				amount, ok := m.NutritionPerServing[k]
				if !ok {
					unknown[k] = true
					continue
				}
				totals[k] += amount * r.Portion
			}
		}
		perDay := make(map[string]float64, len(allNutrientKeys))
		for _, k := range allNutrientKeys {
			if unknown[k] {
				continue
			}
			perDay[k] = totals[k]
		}
		days = append(days, DailyTotal{Date: d, Entries: entries, NutritionPerDay: perDay})
	}

	return PlanRange{
		From: from, To: to, Days: days,
		Targets: Targets{Kcal: user.TargetKcal, ProteinG: user.TargetProteinG, CarbsG: user.TargetCarbsG, FatG: user.TargetFatG},
	}, nil
}

// SetEntry sets or swaps the meal (and/or portion) for a date and slot. For
// breakfast/lunch/dinner this is a true upsert (one entry per date+slot).
// For snack, since a URL can't select among several, this always adds a new
// entry. Either way the written entry's from_template_id is NULL: a manual
// set is no longer "from" a template, even if it replaces one that was.
func (s *Plan) SetEntry(ctx context.Context, ownerID uuid.UUID, date time.Time, slot string, in SetPlanEntryInput) (PlanEntry, error) {
	meal, err := s.st.GetMealForUser(ctx, sqlc.GetMealForUserParams{ID: in.MealID, UserID: ownerID})
	if store.IsNotFound(err) {
		return PlanEntry{}, ErrPlanMealNotFound
	}
	if err != nil {
		return PlanEntry{}, fmt.Errorf("get meal: %w", err)
	}

	pgDate := toPgDate(date)
	var row sqlc.PlanEntry
	if slot == "snack" {
		row, err = s.st.InsertPlanEntry(ctx, sqlc.InsertPlanEntryParams{
			OwnerID: ownerID, Date: pgDate, Slot: slot, MealID: in.MealID, Portion: in.Portion, FromTemplateID: nil,
		})
	} else {
		row, err = s.st.UpsertPlanEntry(ctx, sqlc.UpsertPlanEntryParams{
			OwnerID: ownerID, Date: pgDate, Slot: slot, MealID: in.MealID, Portion: in.Portion,
		})
	}
	// The meal existence check above (GetMealForUser) is not in the same
	// transaction as this insert (SetEntry runs unwrapped), so a concurrent
	// delete of the meal in between can still reach here (deleting an
	// in-use meal is normally blocked by plan_entries_meal_id_fkey, but
	// this row doesn't exist yet at check time). Translate that race into
	// the same 404 a meal that never existed would get, instead of letting
	// a raw foreign-key violation surface as a 500.
	if store.IsForeignKeyViolation(err, "plan_entries_meal_id_fkey") {
		return PlanEntry{}, ErrPlanMealNotFound
	}
	if err != nil {
		return PlanEntry{}, fmt.Errorf("set plan entry: %w", err)
	}
	return PlanEntry{
		ID: row.ID, Date: fromPgDate(row.Date), Slot: row.Slot, MealID: row.MealID, MealName: meal.Name,
		Portion: row.Portion, FromTemplateID: row.FromTemplateID, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt,
	}, nil
}

// DeleteEntry removes the plan entry (or entries) for a date and slot. For
// breakfast/lunch/dinner this removes the one entry. For snack, since a URL
// can't select among several, this removes every snack entry for that date.
func (s *Plan) DeleteEntry(ctx context.Context, ownerID uuid.UUID, date time.Time, slot string) error {
	pgDate := toPgDate(date)
	var n int64
	var err error
	if slot == "snack" {
		n, err = s.st.DeleteAllSnackEntriesForUserOnDate(ctx, sqlc.DeleteAllSnackEntriesForUserOnDateParams{UserID: ownerID, Date: pgDate})
	} else {
		n, err = s.st.DeletePlanEntryForUserOnDateSlot(ctx, sqlc.DeletePlanEntryForUserOnDateSlotParams{UserID: ownerID, Date: pgDate, Slot: slot})
	}
	if err != nil {
		return fmt.Errorf("delete plan entry: %w", err)
	}
	if n == 0 {
		return ErrPlanEntryNotFound
	}
	return nil
}
