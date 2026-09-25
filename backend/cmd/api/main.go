// Command api runs the Meal Planner HTTP API.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/InzKazik/mealplanner/backend/internal/auth"
	"github.com/InzKazik/mealplanner/backend/internal/config"
	"github.com/InzKazik/mealplanner/backend/internal/db"
	"github.com/InzKazik/mealplanner/backend/internal/httpapi"
	"github.com/InzKazik/mealplanner/backend/internal/service"
	"github.com/InzKazik/mealplanner/backend/internal/store"
)

const (
	shutdownTimeout = 10 * time.Second
	accessTokenTTL  = 15 * time.Minute
	refreshTokenTTL = 30 * 24 * time.Hour
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "api:", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load(os.Getenv)
	if err != nil {
		return err
	}

	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(logger)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	// After the first signal, restore default handling so a second Ctrl-C
	// force-quits instead of being swallowed while the server drains.
	go func() {
		<-ctx.Done()
		stop()
	}()

	ln, err := net.Listen("tcp", cfg.Addr)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", cfg.Addr, err)
	}
	logger.Info("api listening", "addr", ln.Addr().String())

	return serve(ctx, cfg, logger, ln)
}

// serve runs the API on ln until ctx is cancelled, then drains in-flight
// requests for up to shutdownTimeout. It returns nil after a clean shutdown.
func serve(ctx context.Context, cfg config.Config, logger *slog.Logger, ln net.Listener) error {
	pool, err := db.Connect(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()

	st := store.New(pool)
	tokens := auth.NewTokenIssuer([]byte(cfg.JWTSecret), accessTokenTTL, time.Now)
	accounts := service.NewAuth(st, auth.NewHasher(auth.DefaultHashParams), tokens, refreshTokenTTL, time.Now)
	ingredients := service.NewIngredients(st)
	meals := service.NewMeals(st)
	dietTemplates := service.NewDietTemplates(st)
	plan := service.NewPlan(st, meals)
	listEvents := service.NewListEventHub()
	shoppingLists := service.NewShoppingLists(st, listEvents)
	partners := service.NewPartners(st, listEvents, time.Now)
	// DeleteUser removes an account's lists with a raw DELETE, which publishes
	// no list_deleted event, so the hub is told to close the streams itself.
	accounts.OnUserDeleted(listEvents.CloseUser)

	srv := &http.Server{
		Handler: httpapi.NewRouter(httpapi.Deps{
			Logger:         logger,
			Ready:          pool.Ping,
			WebOrigin:      cfg.WebOrigin,
			Auth:           accounts,
			Ingredients:    ingredients,
			Meals:          meals,
			DietTemplates:  dietTemplates,
			Plan:           plan,
			ShoppingLists:  shoppingLists,
			Partners:       partners,
			Tokens:         tokens,
			TrustedProxies: cfg.TrustedProxies,
			Limits:         httpapi.RateLimits{AuthPerMinute: cfg.AuthRateLimitPerMinute},
		}),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		// WriteTimeout bounds every ordinary response. The shopping-list event
		// stream clears it for its own response (see StreamShoppingListEvents).
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  60 * time.Second,
	}
	// Shutdown stops accepting connections and waits for active ones to go
	// idle, but never cancels a request's context, so an open event stream
	// would never finish on its own. Closing the hub ends every stream
	// (their handlers return), while ordinary in-flight requests still drain
	// normally. Cancelling a BaseContext instead would also abort those
	// ordinary requests mid-write, defeating the graceful drain.
	srv.RegisterOnShutdown(listEvents.Close)

	serveErr := make(chan error, 1)
	go func() { serveErr <- srv.Serve(ln) }()

	select {
	case err := <-serveErr:
		return fmt.Errorf("server stopped unexpectedly: %w", err)
	case <-ctx.Done():
	}

	logger.Info("shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		_ = srv.Close()
		return fmt.Errorf("graceful shutdown: %w", err)
	}
	if err := <-serveErr; !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("server: %w", err)
	}
	return nil
}
