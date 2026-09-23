package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/google/uuid"
	"github.com/oapi-codegen/nullable"

	"github.com/InzKazik/mealplanner/backend/internal/api"
	"github.com/InzKazik/mealplanner/backend/internal/service"
)

// AuthService is what the account handlers need from the auth service.
type AuthService interface {
	Register(ctx context.Context, in service.RegisterInput) (service.Session, error)
	Login(ctx context.Context, email, password string) (service.Session, error)
	Refresh(ctx context.Context, rawToken string) (service.Session, error)
	Logout(ctx context.Context, rawToken string) error
	GetUser(ctx context.Context, id uuid.UUID) (service.User, error)
	UpdateUser(ctx context.Context, id uuid.UUID, in service.UpdateInput) (service.User, error)
	DeleteUser(ctx context.Context, id uuid.UUID) error
}

func (s *server) RegisterUser(w http.ResponseWriter, r *http.Request) {
	var req api.RegisterRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	sess, err := s.auth.Register(r.Context(), service.RegisterInput{
		Email: string(req.Email), Password: req.Password, DisplayName: req.DisplayName,
	})
	if err != nil {
		s.writeServiceError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, toAuthResponse(sess))
}

func (s *server) LoginUser(w http.ResponseWriter, r *http.Request) {
	var req api.LoginRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	sess, err := s.auth.Login(r.Context(), string(req.Email), req.Password)
	if err != nil {
		s.writeServiceError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, toAuthResponse(sess))
}

func (s *server) RefreshSession(w http.ResponseWriter, r *http.Request) {
	var req api.RefreshRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	sess, err := s.auth.Refresh(r.Context(), req.RefreshToken)
	if err != nil {
		s.writeServiceError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, toAuthResponse(sess))
}

func (s *server) LogoutUser(w http.ResponseWriter, r *http.Request) {
	var req api.RefreshRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if err := s.auth.Logout(r.Context(), req.RefreshToken); err != nil {
		s.writeServiceError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *server) GetMe(w http.ResponseWriter, r *http.Request) {
	id, ok := requireUser(w, r)
	if !ok {
		return
	}
	u, err := s.auth.GetUser(r.Context(), id)
	if err != nil {
		s.writeServiceError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, toAPIUser(u))
}

func (s *server) UpdateMe(w http.ResponseWriter, r *http.Request) {
	id, ok := requireUser(w, r)
	if !ok {
		return
	}
	var req api.UpdateProfileRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	u, err := s.auth.UpdateUser(r.Context(), id, service.UpdateInput{
		DisplayName:    req.DisplayName,
		TargetKcal:     toOptional(req.TargetKcal),
		TargetProteinG: toOptional(req.TargetProteinG),
		TargetCarbsG:   toOptional(req.TargetCarbsG),
		TargetFatG:     toOptional(req.TargetFatG),
	})
	if err != nil {
		s.writeServiceError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, toAPIUser(u))
}

func (s *server) DeleteMe(w http.ResponseWriter, r *http.Request) {
	id, ok := requireUser(w, r)
	if !ok {
		return
	}
	if err := s.auth.DeleteUser(r.Context(), id); err != nil {
		s.writeServiceError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// requireUser returns the authenticated user's ID. The validator guarantees it
// on secured routes; the check is a second line of defence.
func requireUser(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	id, ok := UserID(r.Context())
	if !ok {
		w.Header().Set("WWW-Authenticate", "Bearer")
		WriteProblem(w, http.StatusUnauthorized, CodeUnauthorized, "")
	}
	return id, ok
}

// decodeJSON decodes the request body. The validator has already checked it
// against the schema, so a failure here is unexpected but still answered as a
// client error.
func decodeJSON(w http.ResponseWriter, r *http.Request, v any) bool {
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		WriteValidationProblem(w, "request body is not valid JSON", nil)
		return false
	}
	return true
}

func (s *server) writeServiceError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, service.ErrEmailTaken):
		WriteProblem(w, http.StatusConflict, CodeEmailTaken, "")
	case errors.Is(err, service.ErrInvalidCredentials):
		WriteProblem(w, http.StatusUnauthorized, CodeInvalidCredentials, "")
	case errors.Is(err, service.ErrInvalidRefreshToken):
		WriteProblem(w, http.StatusUnauthorized, CodeInvalidRefreshToken, "")
	case errors.Is(err, service.ErrIngredientNotFound):
		WriteProblem(w, http.StatusNotFound, CodeNotFound, "")
	case errors.Is(err, service.ErrIngredientInUse):
		WriteProblem(w, http.StatusConflict, CodeIngredientInUse, "")
	case errors.Is(err, service.ErrIngredientInUseByUnconvertibleUnit):
		// Same problem code as the read-time ErrUnitNotConvertible below:
		// both mean "this ingredient lacks the data needed to convert a unit
		// a meal uses it with." This is the write-time rejection.
		WriteProblem(w, http.StatusConflict, CodeUnitNotConvertible, "")
	case errors.Is(err, service.ErrMealNotFound):
		WriteProblem(w, http.StatusNotFound, CodeNotFound, "")
	case errors.Is(err, service.ErrMealIngredientNotFound):
		WriteProblem(w, http.StatusBadRequest, CodeInvalidIngredient, "")
	case errors.Is(err, service.ErrUnitNotConvertible):
		WriteProblem(w, http.StatusConflict, CodeUnitNotConvertible, "")
	case errors.Is(err, service.ErrMealInUse):
		WriteProblem(w, http.StatusConflict, CodeMealInUse, "")
	case errors.Is(err, service.ErrDietTemplateNotFound):
		WriteProblem(w, http.StatusNotFound, CodeNotFound, "")
	case errors.Is(err, service.ErrDayIndexOutOfRange):
		WriteProblem(w, http.StatusBadRequest, CodeDayIndexOutOfRange, "")
	case errors.Is(err, service.ErrTemplateMealNotFound):
		WriteProblem(w, http.StatusBadRequest, CodeInvalidMeal, "")
	case errors.Is(err, service.ErrDuplicateSlot):
		WriteProblem(w, http.StatusConflict, CodeDuplicateSlot, "")
	case errors.Is(err, service.ErrPlanConflict):
		WriteProblem(w, http.StatusConflict, CodePlanConflict, "")
	case errors.Is(err, service.ErrPlanEntryNotFound):
		WriteProblem(w, http.StatusNotFound, CodeNotFound, "")
	case errors.Is(err, service.ErrPlanMealNotFound):
		WriteProblem(w, http.StatusBadRequest, CodeInvalidMeal, "")
	case errors.Is(err, service.ErrPlanRangeTooLong):
		WriteProblem(w, http.StatusBadRequest, CodePlanRangeTooLong, "")
	case errors.Is(err, service.ErrNotFound):
		// The signed-in user's own account no longer exists: a valid access
		// token for a deleted user is simply no longer authorized. Reused by
		// Ingredients.Create for the same reason (see ingredients.go).
		w.Header().Set("WWW-Authenticate", "Bearer")
		WriteProblem(w, http.StatusUnauthorized, CodeUnauthorized, "")
	default:
		s.logger.ErrorContext(r.Context(), "unhandled service error",
			slog.String("request_id", RequestID(r.Context())), slog.Any("err", err))
		WriteProblem(w, http.StatusInternalServerError, CodeInternal, "")
	}
}

func toAuthResponse(s service.Session) api.AuthResponse {
	return api.AuthResponse{
		AccessToken:  s.AccessToken,
		RefreshToken: s.RefreshToken,
		TokenType:    api.AuthResponseTokenTypeBearer,
		ExpiresIn:    int(s.ExpiresIn.Seconds()),
		User:         toAPIUser(s.User),
	}
}

func toAPIUser(u service.User) api.User {
	return api.User{
		Id:             u.ID,
		Email:          u.Email,
		DisplayName:    u.DisplayName,
		TargetKcal:     toNullable(u.TargetKcal),
		TargetProteinG: toNullable(u.TargetProteinG),
		TargetCarbsG:   toNullable(u.TargetCarbsG),
		TargetFatG:     toNullable(u.TargetFatG),
		CreatedAt:      u.CreatedAt,
		UpdatedAt:      u.UpdatedAt,
	}
}

// toNullable renders a nullable column as an explicit JSON null when unset.
func toNullable(v *float64) nullable.Nullable[float64] {
	if v == nil {
		return nullable.NewNullNullable[float64]()
	}
	return nullable.NewNullableWithValue(*v)
}

// toOptional maps a PATCH field to "unchanged", "clear" or "set".
func toOptional(n nullable.Nullable[float64]) service.Optional[float64] {
	switch {
	case !n.IsSpecified():
		return service.Optional[float64]{}
	case n.IsNull():
		return service.Set[float64](nil)
	default:
		v := n.MustGet()
		return service.Set(&v)
	}
}
