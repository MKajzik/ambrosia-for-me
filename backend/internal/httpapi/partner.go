package httpapi

import (
	"context"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/oapi-codegen/nullable"

	"github.com/InzKazik/mealplanner/backend/internal/api"
	"github.com/InzKazik/mealplanner/backend/internal/service"
)

// PartnerService is what the partner handlers need from the partners service.
type PartnerService interface {
	Invite(ctx context.Context, callerID uuid.UUID) (service.Invite, error)
	Accept(ctx context.Context, callerID uuid.UUID, code string) (service.Partnership, error)
	Get(ctx context.Context, callerID uuid.UUID) (service.Partnership, error)
	Unlink(ctx context.Context, callerID uuid.UUID) error
}

func (s *server) CreatePartnerInvite(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUser(w, r)
	if !ok {
		return
	}
	inv, err := s.partners.Invite(r.Context(), userID)
	if err != nil {
		s.writeServiceError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, api.PartnerInvite{Code: inv.Code, ExpiresAt: inv.ExpiresAt})
}

func (s *server) AcceptPartnerInvite(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUser(w, r)
	if !ok {
		return
	}
	var req api.AcceptPartnerInviteRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	p, err := s.partners.Accept(r.Context(), userID, req.Code)
	if err != nil {
		s.writeServiceError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, toAPIPartnership(p))
}

func (s *server) GetPartner(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUser(w, r)
	if !ok {
		return
	}
	p, err := s.partners.Get(r.Context(), userID)
	if err != nil {
		s.writeServiceError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, toAPIPartnership(p))
}

func (s *server) UnlinkPartner(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUser(w, r)
	if !ok {
		return
	}
	if err := s.partners.Unlink(r.Context(), userID); err != nil {
		s.writeServiceError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func toAPIPartnership(p service.Partnership) api.Partnership {
	return api.Partnership{
		Status:      api.PartnershipStatus(p.Status),
		DisplayName: toNullableString(p.DisplayName),
		LinkedAt:    toNullableTime(p.LinkedAt),
		ExpiresAt:   toNullableTime(p.ExpiresAt),
	}
}

func toNullableTime(v *time.Time) nullable.Nullable[time.Time] {
	if v == nil {
		return nullable.NewNullNullable[time.Time]()
	}
	return nullable.NewNullableWithValue(*v)
}
