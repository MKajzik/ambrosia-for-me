package httpapi

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/InzKazik/mealplanner/backend/internal/api"
)

// server implements api.ServerInterface.
type server struct {
	logger *slog.Logger
	ready  func(context.Context) error
}

var _ api.ServerInterface = (*server)(nil)

func (s *server) GetHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, api.Health{Status: api.HealthStatusOk})
}

func (s *server) GetReady(w http.ResponseWriter, r *http.Request) {
	if err := s.ready(r.Context()); err != nil {
		s.logger.WarnContext(r.Context(), "readiness check failed",
			slog.String("request_id", RequestID(r.Context())),
			slog.Any("err", err),
		)
		WriteProblem(w, http.StatusServiceUnavailable, CodeNotReady, "database is not reachable")
		return
	}
	writeJSON(w, http.StatusOK, api.Health{Status: api.HealthStatusOk})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
