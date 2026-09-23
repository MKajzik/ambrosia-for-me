package httpapi

import (
	"context"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/oapi-codegen/nullable"
	openapi_types "github.com/oapi-codegen/runtime/types"

	"github.com/InzKazik/mealplanner/backend/internal/api"
	"github.com/InzKazik/mealplanner/backend/internal/service"
)

// PlanService is what the plan handlers need from the plan service.
type PlanService interface {
	GetRange(ctx context.Context, ownerID uuid.UUID, from, to time.Time) (service.PlanRange, error)
	SetEntry(ctx context.Context, ownerID uuid.UUID, date time.Time, slot string, in service.SetPlanEntryInput) (service.PlanEntry, error)
	DeleteEntry(ctx context.Context, ownerID uuid.UUID, date time.Time, slot string) error
}

func (s *server) GetPlan(w http.ResponseWriter, r *http.Request, params api.GetPlanParams) {
	userID, ok := requireUser(w, r)
	if !ok {
		return
	}
	rng, err := s.plan.GetRange(r.Context(), userID, params.From.Time, params.To.Time)
	if err != nil {
		s.writeServiceError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, toAPIPlanRange(rng))
}

func (s *server) SetPlanEntry(w http.ResponseWriter, r *http.Request, date openapi_types.Date, slot api.Slot) {
	userID, ok := requireUser(w, r)
	if !ok {
		return
	}
	var req api.SetPlanEntryRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	portion := 1.0
	if req.Portion != nil {
		portion = *req.Portion
	}
	entry, err := s.plan.SetEntry(r.Context(), userID, date.Time, string(slot), service.SetPlanEntryInput{MealID: req.MealId, Portion: portion})
	if err != nil {
		s.writeServiceError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, toAPIPlanEntry(entry))
}

func (s *server) DeletePlanEntry(w http.ResponseWriter, r *http.Request, date openapi_types.Date, slot api.Slot) {
	userID, ok := requireUser(w, r)
	if !ok {
		return
	}
	if err := s.plan.DeleteEntry(r.Context(), userID, date.Time, string(slot)); err != nil {
		s.writeServiceError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func toAPIPlanEntry(e service.PlanEntry) api.PlanEntry {
	fromTemplate := nullable.NewNullNullable[openapi_types.UUID]()
	if e.FromTemplateID != nil {
		fromTemplate = nullable.NewNullableWithValue(openapi_types.UUID(*e.FromTemplateID))
	}
	return api.PlanEntry{
		Id: e.ID, Date: openapi_types.Date{Time: e.Date}, Slot: api.Slot(e.Slot),
		MealId: e.MealID, MealName: e.MealName, Portion: e.Portion,
		FromTemplateId: fromTemplate,
		CreatedAt:      e.CreatedAt, UpdatedAt: e.UpdatedAt,
	}
}

func toAPIDailyTotal(d service.DailyTotal) api.DailyTotal {
	entries := make([]api.PlanEntry, len(d.Entries))
	for i, e := range d.Entries {
		entries[i] = toAPIPlanEntry(e)
	}
	return api.DailyTotal{Date: openapi_types.Date{Time: d.Date}, Entries: entries, NutritionPerDay: nutrientsToAPI(d.NutritionPerDay)}
}

func toAPIPlanRange(r service.PlanRange) api.PlanRange {
	days := make([]api.DailyTotal, len(r.Days))
	for i, d := range r.Days {
		days[i] = toAPIDailyTotal(d)
	}
	return api.PlanRange{
		From: openapi_types.Date{Time: r.From}, To: openapi_types.Date{Time: r.To}, Days: days,
		Targets: api.Targets{
			TargetKcal: toNullable(r.Targets.Kcal), TargetProteinG: toNullable(r.Targets.ProteinG),
			TargetCarbsG: toNullable(r.Targets.CarbsG), TargetFatG: toNullable(r.Targets.FatG),
		},
	}
}
