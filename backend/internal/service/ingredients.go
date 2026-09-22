// Package service holds business rules. It calls the store and never speaks HTTP.
package service

import (
	"context"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/google/uuid"

	"github.com/InzKazik/mealplanner/backend/internal/store"
	"github.com/InzKazik/mealplanner/backend/internal/store/sqlc"
)

// Nutrient keys, matching the ingredient_nutrients.nutrient_key enum. This is
// the shared vocabulary between httpapi, service and usda.
const (
	NutrientCalories      = "calories"
	NutrientProtein       = "protein"
	NutrientCarbohydrates = "carbohydrates"
	NutrientSugar         = "sugar"
	NutrientFibre         = "fibre"
	NutrientFat           = "fat"
	NutrientSaturatedFat  = "saturated_fat"
	NutrientSodium        = "sodium"
	NutrientPotassium     = "potassium"
	NutrientCalcium       = "calcium"
	NutrientIron          = "iron"
	NutrientMagnesium     = "magnesium"
	NutrientZinc          = "zinc"
	NutrientVitaminA      = "vitamin_a"
	NutrientVitaminC      = "vitamin_c"
	NutrientVitaminD      = "vitamin_d"
	NutrientVitaminB12    = "vitamin_b12"
	NutrientFolate        = "folate"
)

// ErrIngredientNotFound means the ingredient does not exist, or exists but is
// not visible to the caller: global ingredients are always visible, a custom
// ingredient only to its owner.
var ErrIngredientNotFound = errors.New("ingredient not found")

// Ingredient is an ingredient as the rest of the application sees it.
type Ingredient struct {
	ID            uuid.UUID
	Name          string
	Category      string
	IsCustom      bool
	GramsPerPiece *float64
	DensityGPerMl *float64
	Nutrients     map[string]float64
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

// CreateIngredientInput is the data needed to create a custom ingredient.
type CreateIngredientInput struct {
	Name          string
	Category      string
	GramsPerPiece *float64
	DensityGPerMl *float64
	Nutrients     map[string]float64
}

// UpdateIngredientInput is a partial update to a custom ingredient. Nutrients,
// when non-nil, replaces the full nutrient set (a key absent from the new map
// is removed, not left alone).
type UpdateIngredientInput struct {
	Name          *string
	Category      *string
	GramsPerPiece Optional[float64]
	DensityGPerMl Optional[float64]
	Nutrients     map[string]float64
}

// IngredientCursor is an opaque position in the alphabetical ingredient list.
type IngredientCursor struct {
	Name string
	ID   uuid.UUID
}

// ListIngredientsInput selects a page of the alphabetical ingredient list.
type ListIngredientsInput struct {
	Category *string
	Cursor   *IngredientCursor
	Limit    int
}

// IngredientPage is one page of ingredients plus the cursor for the next one
// (nil on the last page).
type IngredientPage struct {
	Items      []Ingredient
	NextCursor *IngredientCursor
}

// Ingredients implements the ingredient catalog: custom ingredients owned by
// a user, plus the shared global (USDA) catalog.
type Ingredients struct {
	st *store.Store
}

// NewIngredients returns an Ingredients service.
func NewIngredients(st *store.Store) *Ingredients { return &Ingredients{st: st} }

// Create adds a custom ingredient owned by ownerID.
func (s *Ingredients) Create(ctx context.Context, ownerID uuid.UUID, in CreateIngredientInput) (Ingredient, error) {
	var ing Ingredient
	err := s.st.InTx(ctx, func(q *sqlc.Queries) error {
		row, err := q.CreateIngredient(ctx, sqlc.CreateIngredientParams{
			Name: in.Name, Category: in.Category, OwnerID: &ownerID,
			GramsPerPiece: in.GramsPerPiece, DensityGPerMl: in.DensityGPerMl,
		})
		if store.IsForeignKeyViolation(err, "ingredients_owner_id_fkey") {
			// Mirrors GetUser: an access token for a user that no longer
			// exists is unauthorized, not a 500. See ErrNotFound in auth.go.
			return ErrNotFound
		}
		if err != nil {
			return fmt.Errorf("create ingredient: %w", err)
		}
		if err := upsertNutrients(ctx, q, row.ID, in.Nutrients); err != nil {
			return err
		}
		ing = toIngredient(row, in.Nutrients)
		return nil
	})
	if err != nil {
		return Ingredient{}, err
	}
	return ing, nil
}

// Update applies a partial update to a custom ingredient owned by ownerID.
func (s *Ingredients) Update(ctx context.Context, ownerID, id uuid.UUID, in UpdateIngredientInput) (Ingredient, error) {
	var ing Ingredient
	err := s.st.InTx(ctx, func(q *sqlc.Queries) error {
		row, err := q.UpdateIngredient(ctx, sqlc.UpdateIngredientParams{
			ID: id, UserID: &ownerID, Name: in.Name, Category: in.Category,
			SetGramsPerPiece: in.GramsPerPiece.Specified, GramsPerPiece: in.GramsPerPiece.Value,
			SetDensityGPerMl: in.DensityGPerMl.Specified, DensityGPerMl: in.DensityGPerMl.Value,
		})
		if store.IsNotFound(err) {
			return ErrIngredientNotFound
		}
		if err != nil {
			return fmt.Errorf("update ingredient: %w", err)
		}

		current, err := s.nutrientsFor(ctx, q, row.ID)
		if err != nil {
			return err
		}
		if in.Nutrients != nil {
			if err := q.ReplaceIngredientNutrients(ctx, row.ID); err != nil {
				return fmt.Errorf("replace nutrients: %w", err)
			}
			if err := upsertNutrients(ctx, q, row.ID, in.Nutrients); err != nil {
				return err
			}
			current = in.Nutrients
		}
		ing = toIngredient(row, current)
		return nil
	})
	if err != nil {
		return Ingredient{}, err
	}
	return ing, nil
}

// Delete removes a custom ingredient owned by ownerID.
func (s *Ingredients) Delete(ctx context.Context, ownerID, id uuid.UUID) error {
	n, err := s.st.DeleteIngredient(ctx, sqlc.DeleteIngredientParams{ID: id, UserID: &ownerID})
	if err != nil {
		return fmt.Errorf("delete ingredient: %w", err)
	}
	if n == 0 {
		return ErrIngredientNotFound
	}
	return nil
}

// List returns a page of the alphabetical ingredient catalog visible to userID.
func (s *Ingredients) List(ctx context.Context, userID uuid.UUID, in ListIngredientsInput) (IngredientPage, error) {
	// Defensive clamp: the OpenAPI schema enforces minimum:1 for every HTTP
	// caller, but a future non-HTTP caller (e.g. internal/usda) could reach
	// this service directly with Limit <= 0, which would panic below at
	// rows[in.Limit-1].
	if in.Limit < 1 {
		in.Limit = 1
	}
	params := sqlc.ListIngredientsParams{UserID: &userID, Category: in.Category, RowLimit: toRowLimit(in.Limit + 1)}
	if in.Cursor != nil {
		params.HasCursor = true
		params.CursorName = in.Cursor.Name
		params.CursorID = in.Cursor.ID
	}
	rows, err := s.st.ListIngredients(ctx, params)
	if err != nil {
		return IngredientPage{}, fmt.Errorf("list ingredients: %w", err)
	}

	var next *IngredientCursor
	if len(rows) > in.Limit {
		last := rows[in.Limit-1]
		next = &IngredientCursor{Name: last.Name, ID: last.ID}
		rows = rows[:in.Limit]
	}
	items, err := s.withNutrients(ctx, rows)
	if err != nil {
		return IngredientPage{}, err
	}
	return IngredientPage{Items: items, NextCursor: next}, nil
}

// Search returns the best-matching ingredients for query, visible to userID.
// Results are not paginated: a type-ahead search never needs a second page.
func (s *Ingredients) Search(ctx context.Context, userID uuid.UUID, query string, category *string, limit int) ([]Ingredient, error) {
	if limit < 1 {
		limit = 1
	}
	rows, err := s.st.SearchIngredients(ctx, sqlc.SearchIngredientsParams{
		UserID: &userID, Category: category, Query: query, RowLimit: toRowLimit(limit),
	})
	if err != nil {
		return nil, fmt.Errorf("search ingredients: %w", err)
	}
	return s.withNutrients(ctx, rows)
}

func (s *Ingredients) nutrientsFor(ctx context.Context, q *sqlc.Queries, id uuid.UUID) (map[string]float64, error) {
	rows, err := q.GetIngredientNutrients(ctx, []uuid.UUID{id})
	if err != nil {
		return nil, fmt.Errorf("get nutrients: %w", err)
	}
	return toNutrientMap(rows), nil
}

func (s *Ingredients) withNutrients(ctx context.Context, rows []sqlc.Ingredient) ([]Ingredient, error) {
	if len(rows) == 0 {
		return nil, nil
	}
	ids := make([]uuid.UUID, len(rows))
	for i, r := range rows {
		ids[i] = r.ID
	}
	nutrientRows, err := s.st.GetIngredientNutrients(ctx, ids)
	if err != nil {
		return nil, fmt.Errorf("get nutrients: %w", err)
	}
	byIngredient := make(map[uuid.UUID]map[string]float64, len(rows))
	for _, n := range nutrientRows {
		if byIngredient[n.IngredientID] == nil {
			byIngredient[n.IngredientID] = map[string]float64{}
		}
		byIngredient[n.IngredientID][string(n.NutrientKey)] = n.AmountPer100g
	}
	items := make([]Ingredient, len(rows))
	for i, r := range rows {
		items[i] = toIngredient(r, byIngredient[r.ID])
	}
	return items, nil
}

// toRowLimit safely narrows a caller-supplied limit to int32, clamping
// instead of overflowing (gosec G115).
func toRowLimit(n int) int32 {
	switch {
	case n < 0:
		return 0
	case n > math.MaxInt32:
		return math.MaxInt32
	default:
		return int32(n)
	}
}

func upsertNutrients(ctx context.Context, q *sqlc.Queries, ingredientID uuid.UUID, nutrients map[string]float64) error {
	for key, amount := range nutrients {
		if err := q.UpsertIngredientNutrient(ctx, sqlc.UpsertIngredientNutrientParams{
			IngredientID: ingredientID, NutrientKey: sqlc.NutrientKey(key), AmountPer100g: amount,
		}); err != nil {
			return fmt.Errorf("upsert nutrient %s: %w", key, err)
		}
	}
	return nil
}

func toNutrientMap(rows []sqlc.IngredientNutrient) map[string]float64 {
	m := make(map[string]float64, len(rows))
	for _, r := range rows {
		m[string(r.NutrientKey)] = r.AmountPer100g
	}
	return m
}

func toIngredient(row sqlc.Ingredient, nutrients map[string]float64) Ingredient {
	return Ingredient{
		ID: row.ID, Name: row.Name, Category: row.Category, IsCustom: row.OwnerID != nil,
		GramsPerPiece: row.GramsPerPiece, DensityGPerMl: row.DensityGPerMl,
		Nutrients: nutrients, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt,
	}
}
