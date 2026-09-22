package usda

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/InzKazik/mealplanner/backend/internal/store"
	"github.com/InzKazik/mealplanner/backend/internal/store/sqlc"
)

// Stats summarizes an import run.
type Stats struct {
	Imported        int
	UnknownCategory int
	UnitMismatch    int
}

// Import fetches every Foundation Foods item and upserts it, keyed by
// usda_fdc_id, so rerunning is safe.
func Import(ctx context.Context, st *store.Store, client *Client, logger *slog.Logger) (Stats, error) {
	foods, err := client.FetchFoundationFoods(ctx)
	if err != nil {
		return Stats{}, fmt.Errorf("fetch foundation foods: %w", err)
	}

	var stats Stats
	for _, f := range foods {
		category, known := MapCategory(f.FoodCategory)
		if !known {
			stats.UnknownCategory++
			logger.WarnContext(ctx, "unmapped USDA food category, using other",
				slog.String("category", f.FoodCategory), slog.Int("fdc_id", int(f.FdcID)))
		}
		fdcID := f.FdcID
		err := st.InTx(ctx, func(q *sqlc.Queries) error {
			ing, err := q.UpsertUSDAIngredient(ctx, sqlc.UpsertUSDAIngredientParams{
				Name: f.Description, Category: category, UsdaFdcID: &fdcID,
			})
			if err != nil {
				return fmt.Errorf("upsert ingredient %d: %w", fdcID, err)
			}
			nutrients, mismatches := MapNutrients(f.FoodNutrients)
			for _, m := range mismatches {
				stats.UnitMismatch++
				logger.WarnContext(ctx, "unexpected USDA nutrient unit, skipping",
					slog.String("nutrient", m.NutrientKey), slog.String("want_unit", m.WantUnit),
					slog.String("got_unit", m.GotUnit), slog.Int("fdc_id", int(fdcID)))
			}
			for key, amount := range nutrients {
				if err := q.UpsertIngredientNutrient(ctx, sqlc.UpsertIngredientNutrientParams{
					IngredientID: ing.ID, NutrientKey: sqlc.NutrientKey(key), AmountPer100g: amount,
				}); err != nil {
					return fmt.Errorf("upsert nutrient %s for %d: %w", key, fdcID, err)
				}
			}
			return nil
		})
		if err != nil {
			return stats, err
		}
		stats.Imported++
	}
	return stats, nil
}
