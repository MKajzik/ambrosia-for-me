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

// Errors returned by Meals. Handlers map them to problem responses.
var (
	// ErrMealNotFound means the meal does not exist, or the caller may not
	// see it: it belongs to someone other than the caller and the caller's
	// active partner, or to the partner but is not shared. Writes to a
	// partner's meal, shared or not, are also ErrMealNotFound.
	ErrMealNotFound = errors.New("meal not found")
	// ErrMealIngredientNotFound means a meal_ingredients row references an
	// ingredient that no longer exists or is no longer visible to the meal's
	// owner. In practice this should be unreachable: ingredients.Delete
	// refuses to delete an ingredient a meal still references (see
	// ErrIngredientInUse in ingredients.go), and ownership of a custom
	// ingredient never changes. Kept as a defensive check rather than a
	// documented guarantee.
	ErrMealIngredientNotFound = errors.New("one or more ingredients do not exist or are not visible to you")
	// ErrUnitNotConvertible means a meal_ingredients row's unit is "piece" or
	// "ml" but the ingredient has no grams_per_piece/density_g_per_ml. This
	// can surface long after the row was validated at write time, if the
	// ingredient is later edited to clear that field (ingredients has no
	// awareness of meals, so nothing prevents that edit).
	ErrUnitNotConvertible = errors.New("ingredient does not have the data needed to convert this unit")
	// ErrMealInUse means the meal cannot be deleted because a diet
	// template's slot or a plan entry still references it.
	ErrMealInUse = errors.New("meal is in use")
)

// allNutrientKeys is the ordered set of the 18 tracked nutrient keys, used to
// detect a key that is missing from at least one ingredient contributing to
// a meal (see toMeal).
var allNutrientKeys = []string{
	NutrientCalories, NutrientProtein, NutrientCarbohydrates, NutrientSugar, NutrientFibre, NutrientFat,
	NutrientSaturatedFat, NutrientSodium, NutrientPotassium, NutrientCalcium, NutrientIron, NutrientMagnesium,
	NutrientZinc, NutrientVitaminA, NutrientVitaminC, NutrientVitaminD, NutrientVitaminB12, NutrientFolate,
}

// MealIngredient is one line of a meal, with the referenced ingredient's name
// and category inlined so clients don't need a second round trip to render
// the list.
type MealIngredient struct {
	ID                 uuid.UUID
	IngredientID       uuid.UUID
	IngredientName     string
	IngredientCategory string
	Quantity           float64
	Unit               string
	Position           int
}

// Meal is a meal as the rest of the application sees it. NutritionPerServing
// is computed on read: the meal's ingredient contributions, summed and
// divided by Servings. A key absent from the map means at least one
// ingredient's amount for it is unknown (see toMeal); a meal with no
// ingredients has every key present at 0.
type Meal struct {
	ID                  uuid.UUID
	OwnerID             uuid.UUID
	Name                string
	Notes               *string
	Servings            float64
	SharedWithPartner   bool
	Ingredients         []MealIngredient
	NutritionPerServing map[string]float64
	CreatedAt           time.Time
	UpdatedAt           time.Time
}

// CreateMealInput is the data needed to create a meal. It always starts with
// an empty ingredient list; add ingredients with ReplaceIngredients.
type CreateMealInput struct {
	Name              string
	Notes             *string
	Servings          float64
	SharedWithPartner bool
}

// UpdateMealInput is a partial update to a meal. Notes, when Specified, may
// set the column to nil (clearing it) via Set[string](nil).
type UpdateMealInput struct {
	Name              *string
	Notes             Optional[string]
	Servings          *float64
	SharedWithPartner *bool
}

// MealIngredientInput is one line of a ReplaceIngredients call.
type MealIngredientInput struct {
	IngredientID uuid.UUID
	Quantity     float64
	Unit         string // "g" | "ml" | "piece"
}

// MealCursor is an opaque position in the alphabetical meal list.
type MealCursor struct {
	Name string
	ID   uuid.UUID
}

// ListMealsInput selects a page of the caller's alphabetical meal list.
type ListMealsInput struct {
	Cursor *MealCursor
	Limit  int
}

// MealSummary is a meal without its ingredients or computed nutrition, for
// the list endpoint (which would otherwise pay for a nutrition computation
// per meal on every page).
type MealSummary struct {
	ID                uuid.UUID
	Name              string
	Notes             *string
	Servings          float64
	SharedWithPartner bool
	CreatedAt         time.Time
	UpdatedAt         time.Time
}

// MealPage is one page of meal summaries plus the cursor for the next one
// (nil on the last page).
type MealPage struct {
	Items      []MealSummary
	NextCursor *MealCursor
}

// ingredientReader is the subset of ingredient reads Meals needs. Both
// *store.Store (outside a transaction) and *sqlc.Queries (the tx-scoped
// queries InTx hands its callback) satisfy it, so toMeal works in both
// contexts without duplicating its logic.
type ingredientReader interface {
	GetIngredientsForUser(ctx context.Context, arg sqlc.GetIngredientsForUserParams) ([]sqlc.Ingredient, error)
	GetIngredientNutrients(ctx context.Context, ingredientIds []uuid.UUID) ([]sqlc.IngredientNutrient, error)
}

// Meals implements meals built from ingredients. A meal is owned by one user;
// the owner's active partner may read it, and copy it, when shared_with_partner
// is true (spec §3.6). Every write is owner-only.
type Meals struct {
	st *store.Store
}

// NewMeals returns a Meals service.
func NewMeals(st *store.Store) *Meals { return &Meals{st: st} }

// Create adds a meal owned by ownerID, with no ingredients. Add ingredients
// with ReplaceIngredients.
func (s *Meals) Create(ctx context.Context, ownerID uuid.UUID, in CreateMealInput) (Meal, error) {
	var meal Meal
	err := s.st.InTx(ctx, func(q *sqlc.Queries) error {
		row, err := q.CreateMeal(ctx, sqlc.CreateMealParams{
			OwnerID: ownerID, Name: in.Name, Notes: in.Notes,
			Servings: in.Servings, SharedWithPartner: in.SharedWithPartner,
		})
		if store.IsForeignKeyViolation(err, "meals_owner_id_fkey") {
			// Mirrors Ingredients.Create: an access token for a user that no
			// longer exists is unauthorized, not a 500.
			return ErrNotFound
		}
		if err != nil {
			return fmt.Errorf("create meal: %w", err)
		}
		meal, err = s.toMeal(ctx, q, row, nil)
		return err
	})
	if err != nil {
		return Meal{}, err
	}
	return meal, nil
}

// Get returns a meal with its ingredients and computed nutrition: one owned
// by callerID, or one their active partner has shared. Nutrition and
// ingredient names are resolved with the meal owner's visibility, so a shared
// meal that uses the partner's custom ingredients reads fine.
func (s *Meals) Get(ctx context.Context, callerID, id uuid.UUID) (Meal, error) {
	partnerID, err := partnerOrNil(ctx, s.st.Queries, callerID, false)
	if err != nil {
		return Meal{}, err
	}
	return s.get(ctx, callerID, partnerID, id)
}

// GetOwn is Get restricted to meals owned by ownerID. Plan uses it: an entry's
// meal is always its owner's, so resolving a partner for every meal in a
// plan range would only add queries.
func (s *Meals) GetOwn(ctx context.Context, ownerID, id uuid.UUID) (Meal, error) {
	return s.get(ctx, ownerID, nil, id)
}

func (s *Meals) get(ctx context.Context, callerID uuid.UUID, partnerID *uuid.UUID, id uuid.UUID) (Meal, error) {
	row, err := s.st.GetMealForUser(ctx, sqlc.GetMealForUserParams{ID: id, UserID: callerID, PartnerID: partnerID})
	if store.IsNotFound(err) {
		return Meal{}, ErrMealNotFound
	}
	if err != nil {
		return Meal{}, fmt.Errorf("get meal: %w", err)
	}
	miRows, err := s.st.GetMealIngredients(ctx, id)
	if err != nil {
		return Meal{}, fmt.Errorf("get meal ingredients: %w", err)
	}
	return s.toMeal(ctx, s.st.Queries, row, miRows)
}

// Update applies a partial update to a meal owned by ownerID.
func (s *Meals) Update(ctx context.Context, ownerID, id uuid.UUID, in UpdateMealInput) (Meal, error) {
	var meal Meal
	err := s.st.InTx(ctx, func(q *sqlc.Queries) error {
		row, err := q.UpdateMeal(ctx, sqlc.UpdateMealParams{
			ID: id, UserID: ownerID, Name: in.Name,
			SetNotes: in.Notes.Specified, Notes: in.Notes.Value,
			Servings: in.Servings, SharedWithPartner: in.SharedWithPartner,
		})
		if store.IsNotFound(err) {
			return ErrMealNotFound
		}
		if err != nil {
			return fmt.Errorf("update meal: %w", err)
		}
		miRows, err := q.GetMealIngredients(ctx, id)
		if err != nil {
			return fmt.Errorf("get meal ingredients: %w", err)
		}
		meal, err = s.toMeal(ctx, q, row, miRows)
		return err
	})
	if err != nil {
		return Meal{}, err
	}
	return meal, nil
}

// Delete removes a meal owned by ownerID. Its meal_ingredients rows are
// removed by ON DELETE CASCADE. Fails with ErrMealInUse if a diet template's
// slot or a plan entry still references the meal.
func (s *Meals) Delete(ctx context.Context, ownerID, id uuid.UUID) error {
	n, err := s.st.DeleteMeal(ctx, sqlc.DeleteMealParams{ID: id, UserID: ownerID})
	if store.IsForeignKeyViolation(err, "template_slots_meal_id_fkey") || store.IsForeignKeyViolation(err, "plan_entries_meal_id_fkey") {
		return ErrMealInUse
	}
	if err != nil {
		return fmt.Errorf("delete meal: %w", err)
	}
	if n == 0 {
		return ErrMealNotFound
	}
	return nil
}

// List returns a page of the caller's own meals, alphabetically. The
// partner's shared meals are listed by ListPartner, never mixed in here.
func (s *Meals) List(ctx context.Context, ownerID uuid.UUID, in ListMealsInput) (MealPage, error) {
	return s.list(ctx, ownerID, false, in)
}

// ListPartner returns a page of the meals callerID's active partner has
// shared, alphabetically. Without an active partner it is ErrPartnerNotLinked.
func (s *Meals) ListPartner(ctx context.Context, callerID uuid.UUID, in ListMealsInput) (MealPage, error) {
	partnerID, err := activePartnerID(ctx, s.st.Queries, callerID, false)
	if err != nil {
		return MealPage{}, err
	}
	return s.list(ctx, partnerID, true, in)
}

func (s *Meals) list(ctx context.Context, ownerID uuid.UUID, sharedOnly bool, in ListMealsInput) (MealPage, error) {
	if in.Limit < 1 {
		in.Limit = 1
	}
	params := sqlc.ListMealsForUserParams{UserID: ownerID, SharedOnly: sharedOnly, RowLimit: toRowLimit(in.Limit + 1)}
	if in.Cursor != nil {
		params.HasCursor = true
		params.CursorName = in.Cursor.Name
		params.CursorID = in.Cursor.ID
	}
	rows, err := s.st.ListMealsForUser(ctx, params)
	if err != nil {
		return MealPage{}, fmt.Errorf("list meals: %w", err)
	}

	var next *MealCursor
	if len(rows) > in.Limit {
		last := rows[in.Limit-1]
		next = &MealCursor{Name: last.Name, ID: last.ID}
		rows = rows[:in.Limit]
	}
	items := make([]MealSummary, len(rows))
	for i, r := range rows {
		items[i] = MealSummary{
			ID: r.ID, Name: r.Name, Notes: r.Notes, Servings: r.Servings,
			SharedWithPartner: r.SharedWithPartner, CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt,
		}
	}
	return MealPage{Items: items, NextCursor: next}, nil
}

// ReplaceIngredients atomically replaces a meal's full ingredient list.
// Every row is validated — the referenced ingredient must exist and be
// visible to ownerID, and its unit must be convertible — before anything is
// written, by building (and therefore fully computing) the candidate meal
// first; if that fails, the existing ingredients are left untouched.
func (s *Meals) ReplaceIngredients(ctx context.Context, ownerID, id uuid.UUID, items []MealIngredientInput) (Meal, error) {
	var meal Meal
	err := s.st.InTx(ctx, func(q *sqlc.Queries) error {
		// TouchMealForUser (an UPDATE, not a plain SELECT) takes the row's
		// write lock and bumps updated_at in one step. The lock serializes
		// two concurrent replaces on the same meal: without it, both
		// transactions' DELETEs below can interleave under READ COMMITTED
		// and the second INSERT loop hits the meal_ingredients (meal_id,
		// position) unique constraint, a raw 23505 that nothing translates.
		row, err := q.TouchMealForUser(ctx, sqlc.TouchMealForUserParams{ID: id, UserID: ownerID})
		if store.IsNotFound(err) {
			return ErrMealNotFound
		}
		if err != nil {
			return fmt.Errorf("get meal: %w", err)
		}

		candidates := make([]sqlc.MealIngredient, len(items))
		for i, it := range items {
			candidates[i] = sqlc.MealIngredient{
				MealID: id, IngredientID: it.IngredientID, Quantity: it.Quantity, Unit: it.Unit, Position: int32(i),
			}
		}
		if _, err := s.toMeal(ctx, q, row, candidates); err != nil {
			return err
		}

		if err := q.ReplaceMealIngredients(ctx, id); err != nil {
			return fmt.Errorf("clear meal ingredients: %w", err)
		}
		inserted := make([]sqlc.MealIngredient, len(candidates))
		for i, c := range candidates {
			ins, err := q.InsertMealIngredient(ctx, sqlc.InsertMealIngredientParams{
				MealID: id, IngredientID: c.IngredientID, Quantity: c.Quantity, Unit: c.Unit, Position: c.Position,
			})
			if err != nil {
				return fmt.Errorf("insert meal ingredient: %w", err)
			}
			inserted[i] = ins
		}
		// Rebuild against the inserted rows so the response carries real
		// (database-generated) line ids, not the zero-valued ones the
		// pre-write validation pass used.
		meal, err = s.toMeal(ctx, q, row, inserted)
		return err
	})
	if err != nil {
		return Meal{}, err
	}
	return meal, nil
}

// Copy creates a new meal owned by callerID, with the same name, notes,
// servings and ingredients as the meal at id, and shared_with_partner always
// false regardless of the original. The original is one callerID owns or one
// their active partner has shared. Copying a partner's meal duplicates each
// distinct custom ingredient it uses into callerID's library (with its
// nutrient rows) and leaves global ingredients as shared references.
func (s *Meals) Copy(ctx context.Context, callerID, id uuid.UUID) (Meal, error) {
	var meal Meal
	err := s.st.InTx(ctx, func(q *sqlc.Queries) error {
		partnerID, err := partnerOrNil(ctx, q, callerID, false)
		if err != nil {
			return err
		}
		original, err := q.GetMealForUser(ctx, sqlc.GetMealForUserParams{ID: id, UserID: callerID, PartnerID: partnerID})
		if store.IsNotFound(err) {
			return ErrMealNotFound
		}
		if err != nil {
			return fmt.Errorf("get meal: %w", err)
		}
		copyRow, inserted, err := copyMeal(ctx, q, callerID, original, newIngredientCopier(q, original.OwnerID, callerID))
		if err != nil {
			return err
		}
		meal, err = s.toMeal(ctx, q, copyRow, inserted)
		return err
	})
	if err != nil {
		return Meal{}, err
	}
	return meal, nil
}

// toMeal builds a Meal from a meals row and its (not-yet-necessarily-saved)
// meal_ingredients rows: it fetches every referenced ingredient once (via r,
// which is either the plain store or a transaction's *sqlc.Queries),
// validates each line's unit convertibility, and computes nutrition per
// serving in the same pass. Called both to build a real response and, from
// ReplaceIngredients, to validate a candidate list before writing it.
func (s *Meals) toMeal(ctx context.Context, r ingredientReader, row sqlc.Meal, miRows []sqlc.MealIngredient) (Meal, error) {
	items := make([]MealIngredient, len(miRows))
	totals := make(map[string]float64, len(allNutrientKeys))
	for _, k := range allNutrientKeys {
		totals[k] = 0
	}
	unknown := make(map[string]bool, len(allNutrientKeys))

	if len(miRows) > 0 {
		ids := uniqueUUIDs(miRows, func(r sqlc.MealIngredient) uuid.UUID { return r.IngredientID })
		ingredientRows, err := r.GetIngredientsForUser(ctx, sqlc.GetIngredientsForUserParams{Ids: ids, UserID: &row.OwnerID})
		if err != nil {
			return Meal{}, fmt.Errorf("get ingredients: %w", err)
		}
		if len(ingredientRows) != len(ids) {
			return Meal{}, ErrMealIngredientNotFound
		}
		byID := make(map[uuid.UUID]sqlc.Ingredient, len(ingredientRows))
		for _, ing := range ingredientRows {
			byID[ing.ID] = ing
		}

		nutrientRows, err := r.GetIngredientNutrients(ctx, ids)
		if err != nil {
			return Meal{}, fmt.Errorf("get ingredient nutrients: %w", err)
		}
		nutrientsByID := make(map[uuid.UUID]map[string]float64, len(ids))
		for _, n := range nutrientRows {
			if nutrientsByID[n.IngredientID] == nil {
				nutrientsByID[n.IngredientID] = map[string]float64{}
			}
			nutrientsByID[n.IngredientID][string(n.NutrientKey)] = n.AmountPer100g
		}

		for i, mi := range miRows {
			ing, ok := byID[mi.IngredientID]
			if !ok {
				return Meal{}, ErrMealIngredientNotFound
			}
			grams, err := gramsFor(mi.Quantity, mi.Unit, ing.GramsPerPiece, ing.DensityGPerMl)
			if err != nil {
				return Meal{}, err
			}
			lineNutrients := nutrientsByID[mi.IngredientID]
			for _, k := range allNutrientKeys {
				amount, ok := lineNutrients[k]
				if !ok {
					unknown[k] = true
					continue
				}
				totals[k] += amount / 100 * grams
			}
			items[i] = MealIngredient{
				ID: mi.ID, IngredientID: mi.IngredientID, IngredientName: ing.Name, IngredientCategory: ing.Category,
				Quantity: mi.Quantity, Unit: mi.Unit, Position: int(mi.Position),
			}
		}
	}

	perServing := make(map[string]float64, len(allNutrientKeys))
	for _, k := range allNutrientKeys {
		if unknown[k] {
			continue
		}
		perServing[k] = totals[k] / row.Servings
	}

	return Meal{
		ID: row.ID, OwnerID: row.OwnerID, Name: row.Name, Notes: row.Notes, Servings: row.Servings, SharedWithPartner: row.SharedWithPartner,
		Ingredients: items, NutritionPerServing: perServing, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt,
	}, nil
}

// gramsFor converts quantity in unit to grams, using the ingredient's
// conversion factors. unit is one of "g", "ml", "piece" — enforced by the
// meal_ingredients.unit CHECK constraint and, on the write path, the
// OpenAPI enum, so the default case is unreachable in practice.
func gramsFor(quantity float64, unit string, gramsPerPiece, densityGPerMl *float64) (float64, error) {
	switch unit {
	case "g":
		return quantity, nil
	case "ml":
		if densityGPerMl == nil {
			return 0, ErrUnitNotConvertible
		}
		return quantity * *densityGPerMl, nil
	case "piece":
		if gramsPerPiece == nil {
			return 0, ErrUnitNotConvertible
		}
		return quantity * *gramsPerPiece, nil
	default:
		return 0, fmt.Errorf("meals: unknown unit %q", unit)
	}
}
