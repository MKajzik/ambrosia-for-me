// Package httpapi holds the HTTP layer: routing, request decoding and response
// encoding. It calls services and never touches SQL.
package httpapi

import (
	"context"
	"log/slog"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/cors"

	"github.com/InzKazik/mealplanner/backend/internal/api"
)

// Deps are the collaborators the router needs. Logger, Ready, Auth, Tokens and
// WebOrigin are required (WebOrigin must be a single origin, never "*"):
// NewRouter panics without them rather than failing on the first request.
type Deps struct {
	Logger *slog.Logger
	// Ready reports whether the service can take traffic (for example the
	// database answers). It backs GET /v1/readyz.
	Ready func(ctx context.Context) error
	// WebOrigin is the single browser origin allowed by CORS.
	WebOrigin string
	// Auth implements the account endpoints.
	Auth AuthService
	// Ingredients implements the ingredient catalog endpoints.
	Ingredients IngredientsService
	// Meals implements the meals endpoints.
	Meals MealsService
	// DietTemplates implements the diet-template endpoints.
	DietTemplates DietTemplatesService
	// Plan implements the plan endpoints.
	Plan PlanService
	// ShoppingLists implements the shopping-list endpoints and their event
	// streams.
	ShoppingLists ShoppingListsService
	// Tokens validates access tokens for secured operations.
	Tokens TokenParser
	// Limits are the rate limits; zero values use the defaults.
	Limits RateLimits
	// TrustedProxies is how many reverse proxies sit in front of the API and
	// append to X-Forwarded-For. 0 means the peer address is the client.
	TrustedProxies int
}

// NewRouter returns the root handler with every /v1 route and all middleware.
func NewRouter(d Deps) http.Handler {
	if d.Logger == nil || d.Ready == nil || d.Auth == nil || d.Ingredients == nil || d.Meals == nil || d.DietTemplates == nil || d.Plan == nil ||
		d.ShoppingLists == nil || d.Tokens == nil || d.WebOrigin == "" || d.WebOrigin == "*" {
		panic("httpapi: Deps.Logger, Ready, Auth, Ingredients, Meals, DietTemplates, Plan, ShoppingLists and Tokens are required, and WebOrigin must be a single origin (not empty or *)")
	}
	limits := d.Limits.withDefaults()
	spec, err := api.GetSpec()
	if err != nil {
		panic("httpapi: load embedded OpenAPI document: " + err.Error())
	}

	r := chi.NewRouter()

	r.Use(requestID)
	r.Use(withAuthState)
	r.Use(clientIP(d.TrustedProxies))
	// bodyLimit sits outside the request logger so http.MaxBytesReader gets
	// the real ResponseWriter, which it needs to tell net/http to stop reading.
	r.Use(bodyLimit)
	r.Use(requestLogger(d.Logger))
	r.Use(recoverer(d.Logger))
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins: []string{d.WebOrigin},
		AllowedMethods: []string{http.MethodGet, http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete, http.MethodOptions},
		AllowedHeaders: []string{"Authorization", "Content-Type"},
		ExposedHeaders: []string{requestIDHeader, "Retry-After", "X-RateLimit-Limit", "X-RateLimit-Remaining", "X-RateLimit-Reset"},
		MaxAge:         300,
	}))
	r.Use(authIPLimiter(limits.AuthPerMinute))

	r.NotFound(func(w http.ResponseWriter, _ *http.Request) {
		WriteProblem(w, http.StatusNotFound, CodeNotFound, "")
	})
	r.MethodNotAllowed(func(w http.ResponseWriter, req *http.Request) {
		if allowed := allowedMethods(r, req.URL.Path); len(allowed) > 0 {
			w.Header().Set("Allow", strings.Join(allowed, ", "))
		}
		WriteProblem(w, http.StatusMethodNotAllowed, CodeMethodNotAllowed, "")
	})

	srv := &server{
		logger: d.Logger, ready: d.Ready, auth: d.Auth, ingredients: d.Ingredients, meals: d.Meals,
		dietTemplates: d.DietTemplates, plan: d.Plan, shoppingLists: d.ShoppingLists,
	}
	api.HandlerWithOptions(srv, api.ChiServerOptions{
		BaseURL:    "/v1",
		BaseRouter: r,
		// The generated wrapper applies these in order, so the last one is the
		// outermost: the validator (which authenticates) runs first, then the
		// per-user rate limit, then the handler.
		Middlewares: []api.MiddlewareFunc{
			userLimiter(limits.UserPerMinute),
			openAPIValidator(spec, d.Tokens, d.Logger),
		},
		ErrorHandlerFunc: func(w http.ResponseWriter, req *http.Request, err error) {
			d.Logger.WarnContext(req.Context(), "generated wrapper rejected a request",
				slog.String("request_id", RequestID(req.Context())), slog.Any("err", err))
			WriteProblem(w, http.StatusBadRequest, CodeValidationFailed, "invalid request parameter")
		},
	})
	return r
}

// allowedMethods lists the methods mux has a handler for at path, so a 405
// response can carry the Allow header RFC 9110 requires.
func allowedMethods(mux *chi.Mux, path string) []string {
	var allowed []string
	for _, m := range []string{http.MethodGet, http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete} {
		if mux.Match(chi.NewRouteContext(), m, path) {
			allowed = append(allowed, m)
		}
	}
	return allowed
}
