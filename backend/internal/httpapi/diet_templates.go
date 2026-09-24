package httpapi

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"

	"github.com/google/uuid"
	"github.com/oapi-codegen/nullable"

	"github.com/InzKazik/mealplanner/backend/internal/api"
	"github.com/InzKazik/mealplanner/backend/internal/service"
)

// DietTemplatesService is what the diet-template handlers need from the
// diet templates service.
type DietTemplatesService interface {
	Create(ctx context.Context, ownerID uuid.UUID, in service.CreateDietTemplateInput) (service.DietTemplate, error)
	Get(ctx context.Context, ownerID, id uuid.UUID) (service.DietTemplate, error)
	Update(ctx context.Context, ownerID, id uuid.UUID, in service.UpdateDietTemplateInput) (service.DietTemplate, error)
	Delete(ctx context.Context, ownerID, id uuid.UUID) error
	List(ctx context.Context, ownerID uuid.UUID, in service.ListDietTemplatesInput) (service.DietTemplatePage, error)
	ReplaceSlots(ctx context.Context, ownerID, id uuid.UUID, slots []service.TemplateSlotInput) (service.DietTemplate, error)
	Copy(ctx context.Context, callerID, id uuid.UUID) (service.DietTemplate, error)
	Apply(ctx context.Context, ownerID, id uuid.UUID, in service.ApplyTemplateInput) (int, error)
	ListPartner(ctx context.Context, callerID uuid.UUID, in service.ListDietTemplatesInput) (service.DietTemplatePage, error)
}

const defaultDietTemplateLimit = 20

func (s *server) ListDietTemplates(w http.ResponseWriter, r *http.Request, params api.ListDietTemplatesParams) {
	s.serveDietTemplateList(w, r, params.Cursor, params.Limit, s.dietTemplates.List)
}

func (s *server) ListPartnerDietTemplates(w http.ResponseWriter, r *http.Request, params api.ListPartnerDietTemplatesParams) {
	s.serveDietTemplateList(w, r, params.Cursor, params.Limit, s.dietTemplates.ListPartner)
}

// serveDietTemplateList answers a cursor-paginated template listing; list is
// either the caller's own listing or the partner's.
func (s *server) serveDietTemplateList(w http.ResponseWriter, r *http.Request, cursorParam *string, limitParam *int,
	list func(context.Context, uuid.UUID, service.ListDietTemplatesInput) (service.DietTemplatePage, error)) {
	userID, ok := requireUser(w, r)
	if !ok {
		return
	}
	limit := defaultDietTemplateLimit
	if limitParam != nil {
		limit = *limitParam
	}
	var cursor *service.DietTemplateCursor
	if cursorParam != nil {
		c, ok := decodeDietTemplateCursor(*cursorParam)
		if !ok {
			WriteValidationProblem(w, "cursor is invalid", []FieldError{{Field: "cursor", Code: FieldInvalidForm}})
			return
		}
		cursor = &c
	}
	page, err := list(r.Context(), userID, service.ListDietTemplatesInput{Cursor: cursor, Limit: limit})
	if err != nil {
		s.writeServiceError(w, r, err)
		return
	}
	next := ""
	if page.NextCursor != nil {
		next = encodeDietTemplateCursor(*page.NextCursor)
	}
	writeJSON(w, http.StatusOK, toDietTemplateList(page.Items, next))
}

func (s *server) CreateDietTemplate(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUser(w, r)
	if !ok {
		return
	}
	var req api.CreateDietTemplateRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	in := service.CreateDietTemplateInput{Name: req.Name, DayCount: req.DayCount}
	if req.SharedWithPartner != nil {
		in.SharedWithPartner = *req.SharedWithPartner
	}
	tpl, err := s.dietTemplates.Create(r.Context(), userID, in)
	if err != nil {
		s.writeServiceError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, toAPIDietTemplate(tpl, userID))
}

func (s *server) GetDietTemplate(w http.ResponseWriter, r *http.Request, id uuid.UUID) {
	userID, ok := requireUser(w, r)
	if !ok {
		return
	}
	tpl, err := s.dietTemplates.Get(r.Context(), userID, id)
	if err != nil {
		s.writeServiceError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, toAPIDietTemplate(tpl, userID))
}

func (s *server) UpdateDietTemplate(w http.ResponseWriter, r *http.Request, id uuid.UUID) {
	userID, ok := requireUser(w, r)
	if !ok {
		return
	}
	var req api.UpdateDietTemplateRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	tpl, err := s.dietTemplates.Update(r.Context(), userID, id, service.UpdateDietTemplateInput{Name: req.Name, SharedWithPartner: req.SharedWithPartner})
	if err != nil {
		s.writeServiceError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, toAPIDietTemplate(tpl, userID))
}

func (s *server) DeleteDietTemplate(w http.ResponseWriter, r *http.Request, id uuid.UUID) {
	userID, ok := requireUser(w, r)
	if !ok {
		return
	}
	if err := s.dietTemplates.Delete(r.Context(), userID, id); err != nil {
		s.writeServiceError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *server) ReplaceTemplateSlots(w http.ResponseWriter, r *http.Request, id uuid.UUID) {
	userID, ok := requireUser(w, r)
	if !ok {
		return
	}
	var req api.ReplaceTemplateSlotsRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	slots := make([]service.TemplateSlotInput, len(req.Items))
	for i, it := range req.Items {
		portion := 1.0
		if it.Portion != nil {
			portion = *it.Portion
		}
		slots[i] = service.TemplateSlotInput{DayIndex: it.DayIndex, Slot: string(it.Slot), MealID: it.MealId, Portion: portion}
	}
	tpl, err := s.dietTemplates.ReplaceSlots(r.Context(), userID, id, slots)
	if err != nil {
		s.writeServiceError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, toAPIDietTemplate(tpl, userID))
}

func (s *server) ApplyDietTemplate(w http.ResponseWriter, r *http.Request, id uuid.UUID) {
	userID, ok := requireUser(w, r)
	if !ok {
		return
	}
	var req api.ApplyDietTemplateRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	overwrite := false
	if req.Overwrite != nil {
		overwrite = *req.Overwrite
	}
	if _, err := s.dietTemplates.Apply(r.Context(), userID, id, service.ApplyTemplateInput{StartDate: req.StartDate.Time, Overwrite: overwrite}); err != nil {
		s.writeServiceError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *server) CopyDietTemplate(w http.ResponseWriter, r *http.Request, id uuid.UUID) {
	userID, ok := requireUser(w, r)
	if !ok {
		return
	}
	tpl, err := s.dietTemplates.Copy(r.Context(), userID, id)
	if err != nil {
		s.writeServiceError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, toAPIDietTemplate(tpl, userID))
}

func toDietTemplateList(items []service.DietTemplateSummary, nextCursor string) api.DietTemplateList {
	list := api.DietTemplateList{Items: make([]api.DietTemplateSummary, len(items))}
	for i, t := range items {
		list.Items[i] = toAPIDietTemplateSummary(t)
	}
	if nextCursor == "" {
		list.NextCursor = nullable.NewNullNullable[string]()
	} else {
		list.NextCursor = nullable.NewNullableWithValue(nextCursor)
	}
	return list
}

func toAPIDietTemplateSummary(t service.DietTemplateSummary) api.DietTemplateSummary {
	return api.DietTemplateSummary{
		Id: t.ID, Name: t.Name, DayCount: t.DayCount, SharedWithPartner: t.SharedWithPartner,
		CreatedAt: t.CreatedAt, UpdatedAt: t.UpdatedAt,
	}
}

func toAPIDietTemplate(t service.DietTemplate, viewer uuid.UUID) api.DietTemplate {
	slots := make([]api.TemplateSlot, len(t.Slots))
	for i, sl := range t.Slots {
		slots[i] = api.TemplateSlot{
			Id: sl.ID, DayIndex: sl.DayIndex, Slot: api.Slot(sl.Slot),
			MealId: sl.MealID, MealName: sl.MealName, Portion: sl.Portion,
		}
	}
	return api.DietTemplate{
		Id: t.ID, Name: t.Name, DayCount: t.DayCount, SharedWithPartner: t.SharedWithPartner, IsOwner: t.OwnerID == viewer,
		Slots: slots, CreatedAt: t.CreatedAt, UpdatedAt: t.UpdatedAt,
	}
}

type dietTemplateCursorPayload struct {
	Name string    `json:"n"`
	ID   uuid.UUID `json:"i"`
}

func encodeDietTemplateCursor(c service.DietTemplateCursor) string {
	b, _ := json.Marshal(dietTemplateCursorPayload{Name: c.Name, ID: c.ID})
	return base64.RawURLEncoding.EncodeToString(b)
}

func decodeDietTemplateCursor(s string) (service.DietTemplateCursor, bool) {
	b, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return service.DietTemplateCursor{}, false
	}
	var p dietTemplateCursorPayload
	if err := json.Unmarshal(b, &p); err != nil || p.Name == "" || p.ID == uuid.Nil {
		return service.DietTemplateCursor{}, false
	}
	return service.DietTemplateCursor{Name: p.Name, ID: p.ID}, true
}
