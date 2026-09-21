// Command api runs the Meal Planner HTTP API.
package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/InzKazik/mealplanner/backend/internal/httpapi"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))

	addr := os.Getenv("API_ADDR")
	if addr == "" {
		addr = ":8080"
	}

	srv := &http.Server{
		Addr: addr,
		Handler: httpapi.NewRouter(httpapi.Deps{
			Logger:    logger,
			Ready:     func(context.Context) error { return nil },
			WebOrigin: "http://localhost:3000",
		}),
		ReadHeaderTimeout: 5 * time.Second,
	}

	logger.Info("api listening", "addr", addr)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		logger.Error("server failed", "err", err)
		os.Exit(1)
	}
}
