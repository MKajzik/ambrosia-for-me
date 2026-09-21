// Command migrate applies pending database migrations and exits.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"

	"github.com/InzKazik/mealplanner/backend/internal/db"
)

func main() {
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		fmt.Fprintln(os.Stderr, "migrate: DATABASE_URL is required")
		os.Exit(2)
	}
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))

	applied, err := db.Migrate(context.Background(), url)
	if err != nil {
		fmt.Fprintln(os.Stderr, "migrate:", err)
		os.Exit(1)
	}
	logger.Info("migrations applied", "count", applied)
}
