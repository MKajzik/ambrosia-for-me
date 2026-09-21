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

// Deps are the collaborators the router needs.
type Deps struct {
	Logger *slog.Logger
	// Ready reports whether the service can take traffic (for example the
	// database answers). It backs GET /v1/readyz.
	Ready func(ctx context.Context) error
	// WebOrigin is the single browser origin allowed by CORS.
	WebOrigin string
}

// NewRouter returns the root handler with every /v1 route and all middleware.
func NewRouter(d Deps) http.Handler {
	r := chi.NewRouter()

	r.Use(requestID)
	r.Use(requestLogger(d.Logger))
	r.Use(recoverer(d.Logger))
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins: []string{d.WebOrigin},
		AllowedMethods: []string{http.MethodGet, http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete, http.MethodOptions},
		AllowedHeaders: []string{"Authorization", "Content-Type"},
		ExposedHeaders: []string{requestIDHeader},
		MaxAge:         300,
	}))

	r.NotFound(func(w http.ResponseWriter, _ *http.Request) {
		WriteProblem(w, http.StatusNotFound, CodeNotFound, "")
	})
	r.MethodNotAllowed(func(w http.ResponseWriter, req *http.Request) {
		if allowed := allowedMethods(r, req.URL.Path); len(allowed) > 0 {
			w.Header().Set("Allow", strings.Join(allowed, ", "))
		}
		WriteProblem(w, http.StatusMethodNotAllowed, CodeMethodNotAllowed, "")
	})

	api.HandlerWithOptions(&server{logger: d.Logger, ready: d.Ready}, api.ChiServerOptions{
		BaseURL:    "/v1",
		BaseRouter: r,
		ErrorHandlerFunc: func(w http.ResponseWriter, _ *http.Request, err error) {
			WriteProblem(w, http.StatusBadRequest, CodeValidationFailed, err.Error())
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
