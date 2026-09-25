package service

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/InzKazik/mealplanner/backend/internal/store"
	"github.com/InzKazik/mealplanner/backend/internal/store/sqlc"
)

// ingredientCopier gives a copy of a partner's meal the ingredients it needs
// in the caller's library. A global (USDA) ingredient stays a shared
// reference. A custom ingredient is duplicated, with its nutrient rows, into
// the caller's library, once per copier: one copier serves one copy
// operation, so an ingredient used on several lines, or in several meals of
// one template copy, is duplicated a single time. Copying twice duplicates
// twice; there is no dedupe across operations.
//
// When the caller copies their own meal, from == to and remap is the
// identity, so an own copy keeps referencing the same ingredients.
type ingredientCopier struct {
	q      *sqlc.Queries
	from   uuid.UUID // owner of the meals being copied
	to     uuid.UUID // the caller, who will own the copies
	copied map[uuid.UUID]uuid.UUID
}

func newIngredientCopier(q *sqlc.Queries, from, to uuid.UUID) *ingredientCopier {
	return &ingredientCopier{q: q, from: from, to: to, copied: make(map[uuid.UUID]uuid.UUID)}
}

// remap returns the id a copied meal line should reference for the source
// ingredient id.
func (c *ingredientCopier) remap(ctx context.Context, id uuid.UUID) (uuid.UUID, error) {
	if c.from == c.to {
		return id, nil
	}
	if mapped, ok := c.copied[id]; ok {
		return mapped, nil
	}
	// Resolved with the source owner's visibility, exactly like the meal
	// itself is read: a partner never gets to browse the owner's ingredients,
	// only to receive the ones a shared meal uses.
	rows, err := c.q.GetIngredientsForUser(ctx, sqlc.GetIngredientsForUserParams{Ids: []uuid.UUID{id}, UserID: &c.from})
	if err != nil {
		return uuid.Nil, fmt.Errorf("get ingredient to copy: %w", err)
	}
	if len(rows) == 0 {
		return uuid.Nil, ErrMealIngredientNotFound
	}
	src := rows[0]
	if src.OwnerID == nil {
		c.copied[id] = id
		return id, nil
	}
	dup, err := c.q.CreateIngredient(ctx, sqlc.CreateIngredientParams{
		Name: src.Name, Category: src.Category, OwnerID: &c.to,
		GramsPerPiece: src.GramsPerPiece, DensityGPerMl: src.DensityGPerMl,
	})
	if store.IsForeignKeyViolation(err, "ingredients_owner_id_fkey") {
		return uuid.Nil, ErrNotFound
	}
	if err != nil {
		return uuid.Nil, fmt.Errorf("copy ingredient: %w", err)
	}
	nutrients, err := c.q.GetIngredientNutrients(ctx, []uuid.UUID{id})
	if err != nil {
		return uuid.Nil, fmt.Errorf("get nutrients to copy: %w", err)
	}
	for _, n := range nutrients {
		if err := c.q.UpsertIngredientNutrient(ctx, sqlc.UpsertIngredientNutrientParams{
			IngredientID: dup.ID, NutrientKey: n.NutrientKey, AmountPer100g: n.AmountPer100g,
		}); err != nil {
			return uuid.Nil, fmt.Errorf("copy nutrient: %w", err)
		}
	}
	c.copied[id] = dup.ID
	return dup.ID, nil
}

// copyMeal writes a copy of original owned by callerID, always private
// (shared_with_partner = false), with its ingredient lines remapped through
// ic. It returns the new meals row and its lines.
func copyMeal(ctx context.Context, q *sqlc.Queries, callerID uuid.UUID, original sqlc.Meal, ic *ingredientCopier) (sqlc.Meal, []sqlc.MealIngredient, error) {
	lines, err := q.GetMealIngredients(ctx, original.ID)
	if err != nil {
		return sqlc.Meal{}, nil, fmt.Errorf("get meal ingredients: %w", err)
	}
	row, err := q.CreateMeal(ctx, sqlc.CreateMealParams{
		OwnerID: callerID, Name: original.Name, Notes: original.Notes,
		Servings: original.Servings, SharedWithPartner: false,
	})
	if store.IsForeignKeyViolation(err, "meals_owner_id_fkey") {
		return sqlc.Meal{}, nil, ErrNotFound
	}
	if err != nil {
		return sqlc.Meal{}, nil, fmt.Errorf("create meal copy: %w", err)
	}
	inserted := make([]sqlc.MealIngredient, len(lines))
	for i, line := range lines {
		ingredientID, err := ic.remap(ctx, line.IngredientID)
		if err != nil {
			return sqlc.Meal{}, nil, err
		}
		ins, err := q.InsertMealIngredient(ctx, sqlc.InsertMealIngredientParams{
			MealID: row.ID, IngredientID: ingredientID, Quantity: line.Quantity, Unit: line.Unit, Position: line.Position,
		})
		if err != nil {
			return sqlc.Meal{}, nil, fmt.Errorf("copy meal ingredient: %w", err)
		}
		inserted[i] = ins
	}
	return row, inserted, nil
}
