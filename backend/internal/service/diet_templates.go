// Package service holds business rules. It calls the store and never speaks HTTP.
package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/InzKazik/mealplanner/backend/internal/store"
	"github.com/InzKazik/mealplanner/backend/internal/store/sqlc"
)

// Errors returned by DietTemplates. Handlers map them to problem responses.
var (
	// ErrDietTemplateNotFound means the template does not exist, or the
	// caller may not see it: it belongs to someone other than the caller and
	// the caller's active partner, or to the partner but is not shared.
	// Writes to a partner's template, shared or not, are also
	// ErrDietTemplateNotFound.
	ErrDietTemplateNotFound = errors.New("diet template not found")
	// ErrDayIndexOutOfRange means a slot's day_index is >= the template's
	// day_count. There is no database constraint for this (a CHECK cannot
	// compare against another table's column), so it is enforced here.
	ErrDayIndexOutOfRange = errors.New("day_index is out of range for this template's day_count")
	// ErrTemplateMealNotFound means a slot references a meal that does not
	// exist or is not owned by the caller.
	ErrTemplateMealNotFound = errors.New("one or more meals do not exist or are not visible to you")
	// ErrDuplicateSlot means a non-snack slot was given twice for the same
	// day, translated from the template_slots_unique_slot_idx violation.
	ErrDuplicateSlot = errors.New("a non-snack slot already exists for this day")
	// ErrPlanConflict means applying a template without overwrite would
	// replace an existing, non-snack plan entry.
	ErrPlanConflict = errors.New("applying this template would overwrite existing plan entries")
)

// TemplateSlot is one slot of a diet template, with the referenced meal's
// name inlined so clients don't need a second round trip to render it.
type TemplateSlot struct {
	ID       uuid.UUID
	DayIndex int
	Slot     string
	MealID   uuid.UUID
	MealName string
	Portion  float64
}

// DietTemplate is a reusable meal schedule.
type DietTemplate struct {
	ID                uuid.UUID
	OwnerID           uuid.UUID
	Name              string
	DayCount          int
	SharedWithPartner bool
	Slots             []TemplateSlot
	CreatedAt         time.Time
	UpdatedAt         time.Time
}

// CreateDietTemplateInput is the data needed to create a template. It always
// starts with no slots; add them with ReplaceSlots.
type CreateDietTemplateInput struct {
	Name              string
	DayCount          int
	SharedWithPartner bool
}

// UpdateDietTemplateInput is a partial update. day_count is immutable after
// creation (see the plan's Global Constraints), so it has no field here.
type UpdateDietTemplateInput struct {
	Name              *string
	SharedWithPartner *bool
}

// TemplateSlotInput is one slot of a ReplaceSlots call.
type TemplateSlotInput struct {
	DayIndex int
	Slot     string // "breakfast" | "lunch" | "dinner" | "snack"
	MealID   uuid.UUID
	Portion  float64
}

// DietTemplateCursor is an opaque position in the alphabetical template list.
type DietTemplateCursor struct {
	Name string
	ID   uuid.UUID
}

// ListDietTemplatesInput selects a page of the caller's alphabetical
// template list.
type ListDietTemplatesInput struct {
	Cursor *DietTemplateCursor
	Limit  int
}

// DietTemplateSummary is a template without its slots, for the list endpoint.
type DietTemplateSummary struct {
	ID                uuid.UUID
	Name              string
	DayCount          int
	SharedWithPartner bool
	CreatedAt         time.Time
	UpdatedAt         time.Time
}

// DietTemplatePage is one page of template summaries plus the cursor for the
// next one (nil on the last page).
type DietTemplatePage struct {
	Items      []DietTemplateSummary
	NextCursor *DietTemplateCursor
}

// ApplyTemplateInput selects where and how a template's slots are copied
// into plan_entries.
type ApplyTemplateInput struct {
	StartDate time.Time
	Overwrite bool
}

// DietTemplates implements reusable meal-schedule templates. A template is
// owned by one user; the owner's active partner may read it, and copy it,
// when shared_with_partner is true (spec §3.6). Every write, and Apply, is
// owner-only.
//
// Sharing a template shares what its slots show: each slot's meal_id and meal
// name, even when the meal itself is not shared. The meal's own detail stays
// hidden (Meals.Get is 404 for it), and a copy of the template carries the
// meals along.
type DietTemplates struct {
	st *store.Store
}

// NewDietTemplates returns a DietTemplates service.
func NewDietTemplates(st *store.Store) *DietTemplates { return &DietTemplates{st: st} }

// Create adds a template owned by ownerID, with no slots. Add slots with
// ReplaceSlots.
func (s *DietTemplates) Create(ctx context.Context, ownerID uuid.UUID, in CreateDietTemplateInput) (DietTemplate, error) {
	var tpl DietTemplate
	err := s.st.InTx(ctx, func(q *sqlc.Queries) error {
		row, err := q.CreateDietTemplate(ctx, sqlc.CreateDietTemplateParams{
			OwnerID: ownerID, Name: in.Name, DayCount: toInt32(in.DayCount), SharedWithPartner: in.SharedWithPartner,
		})
		if store.IsForeignKeyViolation(err, "diet_templates_owner_id_fkey") {
			return ErrNotFound
		}
		if err != nil {
			return fmt.Errorf("create diet template: %w", err)
		}
		tpl, err = s.toTemplate(ctx, q, row, nil)
		return err
	})
	if err != nil {
		return DietTemplate{}, err
	}
	return tpl, nil
}

// Get returns a template with its slots: one owned by callerID, or one their
// active partner has shared.
func (s *DietTemplates) Get(ctx context.Context, callerID, id uuid.UUID) (DietTemplate, error) {
	partnerID, err := partnerOrNil(ctx, s.st.Queries, callerID, false)
	if err != nil {
		return DietTemplate{}, err
	}
	row, err := s.st.GetDietTemplateForUser(ctx, sqlc.GetDietTemplateForUserParams{ID: id, UserID: callerID, PartnerID: partnerID})
	if store.IsNotFound(err) {
		return DietTemplate{}, ErrDietTemplateNotFound
	}
	if err != nil {
		return DietTemplate{}, fmt.Errorf("get diet template: %w", err)
	}
	slotRows, err := s.st.GetTemplateSlots(ctx, id)
	if err != nil {
		return DietTemplate{}, fmt.Errorf("get template slots: %w", err)
	}
	return s.toTemplate(ctx, s.st.Queries, row, slotRows)
}

// Update applies a partial update to a template owned by ownerID.
func (s *DietTemplates) Update(ctx context.Context, ownerID, id uuid.UUID, in UpdateDietTemplateInput) (DietTemplate, error) {
	var tpl DietTemplate
	err := s.st.InTx(ctx, func(q *sqlc.Queries) error {
		row, err := q.UpdateDietTemplate(ctx, sqlc.UpdateDietTemplateParams{ID: id, UserID: ownerID, Name: in.Name, SharedWithPartner: in.SharedWithPartner})
		if store.IsNotFound(err) {
			return ErrDietTemplateNotFound
		}
		if err != nil {
			return fmt.Errorf("update diet template: %w", err)
		}
		slotRows, err := q.GetTemplateSlots(ctx, id)
		if err != nil {
			return fmt.Errorf("get template slots: %w", err)
		}
		tpl, err = s.toTemplate(ctx, q, row, slotRows)
		return err
	})
	if err != nil {
		return DietTemplate{}, err
	}
	return tpl, nil
}

// Delete removes a template owned by ownerID. Its template_slots rows are
// removed by ON DELETE CASCADE; any plan_entries that came from it keep
// their row and have from_template_id set to NULL.
func (s *DietTemplates) Delete(ctx context.Context, ownerID, id uuid.UUID) error {
	n, err := s.st.DeleteDietTemplate(ctx, sqlc.DeleteDietTemplateParams{ID: id, UserID: ownerID})
	if err != nil {
		return fmt.Errorf("delete diet template: %w", err)
	}
	if n == 0 {
		return ErrDietTemplateNotFound
	}
	return nil
}

// List returns a page of the caller's own templates, alphabetically. The
// partner's shared templates are listed by ListPartner, never mixed in here.
func (s *DietTemplates) List(ctx context.Context, ownerID uuid.UUID, in ListDietTemplatesInput) (DietTemplatePage, error) {
	return s.list(ctx, ownerID, false, in)
}

// ListPartner returns a page of the templates callerID's active partner has
// shared, alphabetically. Without an active partner it is ErrPartnerNotLinked.
func (s *DietTemplates) ListPartner(ctx context.Context, callerID uuid.UUID, in ListDietTemplatesInput) (DietTemplatePage, error) {
	partnerID, err := activePartnerID(ctx, s.st.Queries, callerID, false)
	if err != nil {
		return DietTemplatePage{}, err
	}
	return s.list(ctx, partnerID, true, in)
}

func (s *DietTemplates) list(ctx context.Context, ownerID uuid.UUID, sharedOnly bool, in ListDietTemplatesInput) (DietTemplatePage, error) {
	if in.Limit < 1 {
		in.Limit = 1
	}
	params := sqlc.ListDietTemplatesForUserParams{UserID: ownerID, SharedOnly: sharedOnly, RowLimit: toRowLimit(in.Limit + 1)}
	if in.Cursor != nil {
		params.HasCursor = true
		params.CursorName = in.Cursor.Name
		params.CursorID = in.Cursor.ID
	}
	rows, err := s.st.ListDietTemplatesForUser(ctx, params)
	if err != nil {
		return DietTemplatePage{}, fmt.Errorf("list diet templates: %w", err)
	}
	var next *DietTemplateCursor
	if len(rows) > in.Limit {
		last := rows[in.Limit-1]
		next = &DietTemplateCursor{Name: last.Name, ID: last.ID}
		rows = rows[:in.Limit]
	}
	items := make([]DietTemplateSummary, len(rows))
	for i, r := range rows {
		items[i] = DietTemplateSummary{
			ID: r.ID, Name: r.Name, DayCount: int(r.DayCount),
			SharedWithPartner: r.SharedWithPartner, CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt,
		}
	}
	return DietTemplatePage{Items: items, NextCursor: next}, nil
}

// ReplaceSlots atomically replaces a template's full slot list. Every
// slot's day_index must be < the template's day_count, and every meal_id
// must exist and be owned by ownerID, before anything is written.
func (s *DietTemplates) ReplaceSlots(ctx context.Context, ownerID, id uuid.UUID, slots []TemplateSlotInput) (DietTemplate, error) {
	var tpl DietTemplate
	err := s.st.InTx(ctx, func(q *sqlc.Queries) error {
		// TouchDietTemplateForUser (an UPDATE, not a plain SELECT) takes the
		// row's write lock and bumps updated_at in one step. The lock
		// serializes two concurrent slot replaces on the same template:
		// without it, both transactions' DELETEs below can interleave under
		// READ COMMITTED and the second INSERT loop can hit
		// template_slots_unique_slot_idx unexpectedly, or the template can
		// end up with the union of both requests' slots. Same fix as
		// Meals.ReplaceIngredients / TouchMealForUser.
		row, err := q.TouchDietTemplateForUser(ctx, sqlc.TouchDietTemplateForUserParams{ID: id, UserID: ownerID})
		if store.IsNotFound(err) {
			return ErrDietTemplateNotFound
		}
		if err != nil {
			return fmt.Errorf("get diet template: %w", err)
		}

		for _, sl := range slots {
			if sl.DayIndex < 0 || sl.DayIndex >= int(row.DayCount) {
				return ErrDayIndexOutOfRange
			}
		}
		mealIDs := uniqueUUIDs(slots, func(sl TemplateSlotInput) uuid.UUID { return sl.MealID })
		if len(mealIDs) > 0 {
			mealRows, err := q.GetMealsForUser(ctx, sqlc.GetMealsForUserParams{Ids: mealIDs, UserID: ownerID})
			if err != nil {
				return fmt.Errorf("get meals: %w", err)
			}
			if len(mealRows) != len(mealIDs) {
				return ErrTemplateMealNotFound
			}
		}

		if err := q.DeleteTemplateSlots(ctx, id); err != nil {
			return fmt.Errorf("clear template slots: %w", err)
		}
		for _, sl := range slots {
			_, err := q.InsertTemplateSlot(ctx, sqlc.InsertTemplateSlotParams{
				TemplateID: id, DayIndex: toInt32(sl.DayIndex), Slot: sl.Slot, MealID: sl.MealID, Portion: sl.Portion,
			})
			if store.IsUniqueViolation(err, "template_slots_unique_slot_idx") {
				return ErrDuplicateSlot
			}
			// The meal existence check above ran in this same transaction,
			// so this is reachable only if the meal was deleted concurrently
			// between that check and this insert (deleting an in-use meal
			// is normally blocked by template_slots_meal_id_fkey, but this
			// row doesn't exist yet at check time). Translate the race into
			// the same 404 a meal that never existed would get, instead of
			// letting a raw foreign-key violation surface as a 500.
			if store.IsForeignKeyViolation(err, "template_slots_meal_id_fkey") {
				return ErrTemplateMealNotFound
			}
			if err != nil {
				return fmt.Errorf("insert template slot: %w", err)
			}
		}
		// Re-read rather than answering from the insert order: GetTemplateSlots
		// orders by day_index then meal-time position, which is what Get,
		// Update and Apply return, so a client that PUTs slots out of order
		// sees the same order here as on its next GET.
		slotRows, err := q.GetTemplateSlots(ctx, id)
		if err != nil {
			return fmt.Errorf("get template slots: %w", err)
		}
		tpl, err = s.toTemplate(ctx, q, row, slotRows)
		return err
	})
	if err != nil {
		return DietTemplate{}, err
	}
	return tpl, nil
}

// Copy creates a new template owned by callerID, with the same name,
// day_count and slots as the template at id, and shared_with_partner always
// false regardless of the original. The original is one callerID owns or one
// their active partner has shared.
//
// Copying an own template keeps its slots pointing at the same meals.
// Copying a partner's template copies the meals too, since its slots
// reference the partner's meals: each distinct meal is copied once, even when
// it fills several slots, with the same ingredient rules as Meals.Copy (one
// duplicate per distinct custom ingredient across the whole template, global
// ingredients shared). The meals are read through the template, so a meal the
// partner did not share on its own copies fine.
func (s *DietTemplates) Copy(ctx context.Context, callerID, id uuid.UUID) (DietTemplate, error) {
	var tpl DietTemplate
	err := s.st.InTx(ctx, func(q *sqlc.Queries) error {
		partnerID, err := partnerOrNil(ctx, q, callerID, false)
		if err != nil {
			return err
		}
		original, err := q.GetDietTemplateForUser(ctx, sqlc.GetDietTemplateForUserParams{ID: id, UserID: callerID, PartnerID: partnerID})
		if store.IsNotFound(err) {
			return ErrDietTemplateNotFound
		}
		if err != nil {
			return fmt.Errorf("get diet template: %w", err)
		}
		originalSlots, err := q.GetTemplateSlots(ctx, id)
		if err != nil {
			return fmt.Errorf("get template slots: %w", err)
		}
		mealFor, err := copiedMeals(ctx, q, callerID, original, originalSlots)
		if err != nil {
			return err
		}

		copyRow, err := q.CreateDietTemplate(ctx, sqlc.CreateDietTemplateParams{
			OwnerID: callerID, Name: original.Name, DayCount: original.DayCount, SharedWithPartner: false,
		})
		if store.IsForeignKeyViolation(err, "diet_templates_owner_id_fkey") {
			return ErrNotFound
		}
		if err != nil {
			return fmt.Errorf("create diet template copy: %w", err)
		}
		inserted := make([]sqlc.TemplateSlot, len(originalSlots))
		for i, sl := range originalSlots {
			ins, err := q.InsertTemplateSlot(ctx, sqlc.InsertTemplateSlotParams{
				TemplateID: copyRow.ID, DayIndex: sl.DayIndex, Slot: sl.Slot, MealID: mealFor(sl.MealID), Portion: sl.Portion,
			})
			if err != nil {
				return fmt.Errorf("copy template slot: %w", err)
			}
			inserted[i] = ins
		}
		tpl, err = s.toTemplate(ctx, q, copyRow, inserted)
		return err
	})
	if err != nil {
		return DietTemplate{}, err
	}
	return tpl, nil
}

// copiedMeals returns the meal id each of a template copy's slots should
// reference, given the original slot's meal id: the same id for an own
// template, a fresh private copy (made here, once per distinct meal) for a
// partner's.
func copiedMeals(ctx context.Context, q *sqlc.Queries, callerID uuid.UUID, original sqlc.DietTemplate, slots []sqlc.TemplateSlot) (func(uuid.UUID) uuid.UUID, error) {
	if original.OwnerID == callerID {
		return func(id uuid.UUID) uuid.UUID { return id }, nil
	}
	mealIDs := uniqueUUIDs(slots, func(r sqlc.TemplateSlot) uuid.UUID { return r.MealID })
	rows, err := q.GetMealsForUser(ctx, sqlc.GetMealsForUserParams{Ids: mealIDs, UserID: original.OwnerID})
	if err != nil {
		return nil, fmt.Errorf("get meals to copy: %w", err)
	}
	if len(rows) != len(mealIDs) {
		return nil, ErrTemplateMealNotFound
	}
	byID := make(map[uuid.UUID]sqlc.Meal, len(rows))
	for _, m := range rows {
		byID[m.ID] = m
	}
	ic := newIngredientCopier(q, original.OwnerID, callerID)
	copies := make(map[uuid.UUID]uuid.UUID, len(mealIDs))
	for _, id := range mealIDs {
		row, _, err := copyMeal(ctx, q, callerID, byID[id], ic)
		if err != nil {
			return nil, err
		}
		copies[id] = row.ID
	}
	return func(id uuid.UUID) uuid.UUID { return copies[id] }, nil
}

// Apply copies the template's slots into plan_entries starting at
// startDate: day_index 0 lands on startDate, day_index 1 on startDate+1,
// and so on. Without overwrite, it fails with ErrPlanConflict (writing
// nothing) if any non-snack target date+slot already has an entry; snack
// slots are always additive and never conflict. With overwrite, the same
// non-snack targets are replaced first. Returns the number of entries
// written.
//
// plan_entries.date (and template_slots' downstream targets) are
// sqlc.PlanEntry.Date/GetPlanEntriesForUserOnDatesParams.Dates of type
// pgtype.Date, not time.Time (see util.go's toPgDate/fromPgDate doc
// comment) — every date value crossing into a sqlc call or read back from
// one is converted at that boundary below.
func (s *DietTemplates) Apply(ctx context.Context, ownerID, id uuid.UUID, in ApplyTemplateInput) (int, error) {
	var written int
	err := s.st.InTx(ctx, func(q *sqlc.Queries) error {
		_, err := q.GetDietTemplateForUser(ctx, sqlc.GetDietTemplateForUserParams{ID: id, UserID: ownerID})
		if store.IsNotFound(err) {
			return ErrDietTemplateNotFound
		}
		if err != nil {
			return fmt.Errorf("get diet template: %w", err)
		}
		slotRows, err := q.GetTemplateSlots(ctx, id)
		if err != nil {
			return fmt.Errorf("get template slots: %w", err)
		}

		targetDates := make([]time.Time, len(slotRows))
		targetPgDates := make([]pgtype.Date, len(slotRows))
		for i, sl := range slotRows {
			targetDates[i] = in.StartDate.AddDate(0, 0, int(sl.DayIndex))
			targetPgDates[i] = toPgDate(targetDates[i])
		}

		if !in.Overwrite {
			existing, err := q.GetPlanEntriesForUserOnDates(ctx, sqlc.GetPlanEntriesForUserOnDatesParams{UserID: ownerID, Dates: targetPgDates})
			if err != nil {
				return fmt.Errorf("check existing plan entries: %w", err)
			}
			existingNonSnack := make(map[string]bool, len(existing))
			for _, e := range existing {
				if e.Slot != "snack" {
					existingNonSnack[fromPgDate(e.Date).Format("2006-01-02")+"|"+e.Slot] = true
				}
			}
			for i, sl := range slotRows {
				if sl.Slot == "snack" {
					continue
				}
				key := targetDates[i].Format("2006-01-02") + "|" + sl.Slot
				if existingNonSnack[key] {
					return ErrPlanConflict
				}
			}
		}

		for i, sl := range slotRows {
			if in.Overwrite && sl.Slot != "snack" {
				if _, err := q.DeletePlanEntryForUserOnDateSlot(ctx, sqlc.DeletePlanEntryForUserOnDateSlotParams{
					UserID: ownerID, Date: targetPgDates[i], Slot: sl.Slot,
				}); err != nil {
					return fmt.Errorf("clear existing plan entry: %w", err)
				}
			}
			fromID := id
			if _, err := q.InsertPlanEntry(ctx, sqlc.InsertPlanEntryParams{
				OwnerID: ownerID, Date: targetPgDates[i], Slot: sl.Slot, MealID: sl.MealID, Portion: sl.Portion, FromTemplateID: &fromID,
			}); err != nil {
				// A concurrent Apply (or a concurrent manual PUT
				// /plan/{date}/{slot}) targeting the same non-snack
				// date+slot can pass the conflict check above and then lose
				// this insert race: the loser's INSERT blocks on
				// plan_entries_unique_slot_idx and fails once the winner
				// commits. Translate that into the same ErrPlanConflict the
				// pre-write check above returns, instead of letting a raw
				// 23505 surface as a 500 — the transaction still rolls back
				// cleanly, so nothing is left half-applied.
				if store.IsUniqueViolation(err, "plan_entries_unique_slot_idx") {
					return ErrPlanConflict
				}
				return fmt.Errorf("insert plan entry: %w", err)
			}
			written++
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	return written, nil
}

// toTemplate builds a DietTemplate from a diet_templates row and its slot
// rows, fetching every referenced meal's name in one batch query (via q,
// which is either the plain store or a transaction's *sqlc.Queries).
func (s *DietTemplates) toTemplate(ctx context.Context, q *sqlc.Queries, row sqlc.DietTemplate, slotRows []sqlc.TemplateSlot) (DietTemplate, error) {
	slots := make([]TemplateSlot, len(slotRows))
	if len(slotRows) > 0 {
		mealIDs := uniqueUUIDs(slotRows, func(r sqlc.TemplateSlot) uuid.UUID { return r.MealID })
		mealRows, err := q.GetMealsForUser(ctx, sqlc.GetMealsForUserParams{Ids: mealIDs, UserID: row.OwnerID})
		if err != nil {
			return DietTemplate{}, fmt.Errorf("get meals: %w", err)
		}
		names := make(map[uuid.UUID]string, len(mealRows))
		for _, m := range mealRows {
			names[m.ID] = m.Name
		}
		for i, sl := range slotRows {
			name, ok := names[sl.MealID]
			if !ok {
				return DietTemplate{}, ErrTemplateMealNotFound
			}
			slots[i] = TemplateSlot{ID: sl.ID, DayIndex: int(sl.DayIndex), Slot: sl.Slot, MealID: sl.MealID, MealName: name, Portion: sl.Portion}
		}
	}
	return DietTemplate{
		ID: row.ID, OwnerID: row.OwnerID, Name: row.Name, DayCount: int(row.DayCount), SharedWithPartner: row.SharedWithPartner,
		Slots: slots, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt,
	}, nil
}
