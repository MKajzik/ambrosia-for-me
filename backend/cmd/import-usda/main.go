// Command import-usda loads the FoodData Central Foundation Foods dataset
// into the ingredients and ingredient_nutrients tables. It is idempotent,
// keyed by usda_fdc_id, so rerunning it is safe.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/InzKazik/mealplanner/backend/internal/store"
	"github.com/InzKazik/mealplanner/backend/internal/usda"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))

	apiKey := os.Getenv("FDC_API_KEY")
	if apiKey == "" {
		fmt.Fprintln(os.Stderr, "import-usda: FDC_API_KEY is required (register a free key at https://api.data.gov/signup/)")
		os.Exit(2)
	}
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		fmt.Fprintln(os.Stderr, "import-usda: DATABASE_URL is required")
		os.Exit(2)
	}

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		fmt.Fprintln(os.Stderr, "import-usda: connect to database:", err)
		os.Exit(1)
	}
	defer pool.Close()

	stats, err := usda.Import(ctx, store.New(pool), usda.NewClient(apiKey), logger)
	if err != nil {
		fmt.Fprintln(os.Stderr, "import-usda:", err)
		os.Exit(1)
	}
	logger.Info("usda import complete", "imported", stats.Imported, "unknown_category", stats.UnknownCategory)
}
