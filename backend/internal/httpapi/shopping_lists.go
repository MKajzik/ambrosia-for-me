package httpapi

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/oapi-codegen/nullable"
	openapi_types "github.com/oapi-codegen/runtime/types"

	"github.com/InzKazik/mealplanner/backend/internal/api"
	"github.com/InzKazik/mealplanner/backend/internal/service"
)

// ShoppingListsService is what the shopping-list handlers need from the
// shopping lists service.
type ShoppingListsService interface {
	Create(ctx context.Context, ownerID uuid.UUID, in service.CreateShoppingListInput) (service.ShoppingList, error)
	Get(ctx context.Context, ownerID, id uuid.UUID) (service.ShoppingList, error)
	List(ctx context.Context, ownerID uuid.UUID, in service.ListShoppingListsInput) (service.ShoppingListPage, error)
	Update(ctx context.Context, ownerID, id uuid.UUID, in service.UpdateShoppingListInput) (service.ShoppingList, error)
	Delete(ctx context.Context, ownerID, id uuid.UUID) error
	Generate(ctx context.Context, ownerID uuid.UUID, in service.GenerateShoppingListInput) (service.ShoppingList, bool, error)
	AddItem(ctx context.Context, ownerID, listID uuid.UUID, in service.CreateShoppingItemInput) (service.ShoppingItem, error)
	UpdateItem(ctx context.Context, ownerID, listID, itemID uuid.UUID, in service.UpdateShoppingItemInput) (service.ShoppingItem, error)
	DeleteItem(ctx context.Context, ownerID, listID, itemID uuid.UUID) error
}

const defaultShoppingListLimit = 20

func (s *server) ListShoppingLists(w http.ResponseWriter, r *http.Request, params api.ListShoppingListsParams) {
	userID, ok := requireUser(w, r)
	if !ok {
		return
	}
	limit := defaultShoppingListLimit
	if params.Limit != nil {
		limit = *params.Limit
	}
	var cursor *service.ShoppingListCursor
	if params.Cursor != nil {
		c, ok := decodeShoppingListCursor(*params.Cursor)
		if !ok {
			WriteValidationProblem(w, "cursor is invalid", []FieldError{{Field: "cursor", Code: FieldInvalidForm}})
			return
		}
		cursor = &c
	}
	page, err := s.shoppingLists.List(r.Context(), userID, service.ListShoppingListsInput{Cursor: cursor, Limit: limit})
	if err != nil {
		s.writeServiceError(w, r, err)
		return
	}
	list := api.ShoppingListPage{Items: make([]api.ShoppingListSummary, len(page.Items)), NextCursor: nullable.NewNullNullable[string]()}
	for i, l := range page.Items {
		list.Items[i] = api.ShoppingListSummary{
			Id: l.ID, Name: l.Name, SharedWithPartner: l.SharedWithPartner,
			SourceFrom: toNullableDate(l.SourceFrom), SourceTo: toNullableDate(l.SourceTo),
			CreatedAt: l.CreatedAt, UpdatedAt: l.UpdatedAt,
		}
	}
	if page.NextCursor != nil {
		list.NextCursor = nullable.NewNullableWithValue(encodeShoppingListCursor(*page.NextCursor))
	}
	writeJSON(w, http.StatusOK, list)
}

func (s *server) CreateShoppingList(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUser(w, r)
	if !ok {
		return
	}
	var req api.CreateShoppingListRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	in := service.CreateShoppingListInput{Name: req.Name}
	if req.SharedWithPartner != nil {
		in.SharedWithPartner = *req.SharedWithPartner
	}
	list, err := s.shoppingLists.Create(r.Context(), userID, in)
	if err != nil {
		s.writeServiceError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, toAPIShoppingList(list))
}

func (s *server) GenerateShoppingList(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUser(w, r)
	if !ok {
		return
	}
	var req api.GenerateShoppingListRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	list, created, err := s.shoppingLists.Generate(r.Context(), userID, service.GenerateShoppingListInput{
		From: req.From.Time, To: req.To.Time, Name: req.Name, ListID: req.ListId,
	})
	if err != nil {
		s.writeServiceError(w, r, err)
		return
	}
	status := http.StatusOK
	if created {
		status = http.StatusCreated
	}
	writeJSON(w, status, toAPIShoppingList(list))
}

func (s *server) GetShoppingList(w http.ResponseWriter, r *http.Request, id uuid.UUID) {
	userID, ok := requireUser(w, r)
	if !ok {
		return
	}
	list, err := s.shoppingLists.Get(r.Context(), userID, id)
	if err != nil {
		s.writeServiceError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, toAPIShoppingList(list))
}

func (s *server) UpdateShoppingList(w http.ResponseWriter, r *http.Request, id uuid.UUID) {
	userID, ok := requireUser(w, r)
	if !ok {
		return
	}
	var req api.UpdateShoppingListRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	list, err := s.shoppingLists.Update(r.Context(), userID, id, service.UpdateShoppingListInput{
		Name: req.Name, SharedWithPartner: req.SharedWithPartner,
	})
	if err != nil {
		s.writeServiceError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, toAPIShoppingList(list))
}

func (s *server) DeleteShoppingList(w http.ResponseWriter, r *http.Request, id uuid.UUID) {
	userID, ok := requireUser(w, r)
	if !ok {
		return
	}
	if err := s.shoppingLists.Delete(r.Context(), userID, id); err != nil {
		s.writeServiceError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *server) CreateShoppingItem(w http.ResponseWriter, r *http.Request, id uuid.UUID) {
	userID, ok := requireUser(w, r)
	if !ok {
		return
	}
	var req api.CreateShoppingItemRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	in := service.CreateShoppingItemInput{IngredientID: req.IngredientId, Name: req.Name, Quantity: req.Quantity}
	if req.Unit != nil {
		unit := string(*req.Unit)
		in.Unit = &unit
	}
	if req.Category != nil {
		category := string(*req.Category)
		in.Category = &category
	}
	item, err := s.shoppingLists.AddItem(r.Context(), userID, id, in)
	if err != nil {
		s.writeServiceError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, toAPIShoppingItem(item))
}

func (s *server) UpdateShoppingItem(w http.ResponseWriter, r *http.Request, id, itemID uuid.UUID) {
	userID, ok := requireUser(w, r)
	if !ok {
		return
	}
	var req api.UpdateShoppingItemRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	in := service.UpdateShoppingItemInput{
		Version: req.Version, Name: req.Name, Quantity: toOptional(req.Quantity), Checked: req.Checked,
	}
	switch {
	case !req.Unit.IsSpecified():
	case req.Unit.IsNull():
		in.Unit = service.Set[string](nil)
	default:
		unit := string(req.Unit.MustGet())
		in.Unit = service.Set(&unit)
	}
	if req.Category != nil {
		category := string(*req.Category)
		in.Category = &category
	}
	item, err := s.shoppingLists.UpdateItem(r.Context(), userID, id, itemID, in)
	var conflict *service.ShoppingItemVersionConflictError
	if errors.As(err, &conflict) {
		writeVersionConflict(w, conflict.Current)
		return
	}
	if err != nil {
		s.writeServiceError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, toAPIShoppingItem(item))
}

func (s *server) DeleteShoppingItem(w http.ResponseWriter, r *http.Request, id, itemID uuid.UUID) {
	userID, ok := requireUser(w, r)
	if !ok {
		return
	}
	if err := s.shoppingLists.DeleteItem(r.Context(), userID, id, itemID); err != nil {
		s.writeServiceError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// writeVersionConflict writes the 409 version_conflict problem with the
// item's current state as the "current" extension member (RFC 9457 allows
// extension members; the shape is ShoppingItemConflict in openapi.yaml).
func writeVersionConflict(w http.ResponseWriter, current service.ShoppingItem) {
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(http.StatusConflict)
	_ = json.NewEncoder(w).Encode(api.ShoppingItemConflict{
		Type: "urn:mealplanner:problem:" + CodeVersionConflict, Title: http.StatusText(http.StatusConflict),
		Status: http.StatusConflict, Code: CodeVersionConflict, Current: toAPIShoppingItem(current),
	})
}

func toAPIShoppingList(l service.ShoppingList) api.ShoppingList {
	items := make([]api.ShoppingItem, len(l.Items))
	for i, it := range l.Items {
		items[i] = toAPIShoppingItem(it)
	}
	return api.ShoppingList{
		Id: l.ID, Name: l.Name, SharedWithPartner: l.SharedWithPartner,
		SourceFrom: toNullableDate(l.SourceFrom), SourceTo: toNullableDate(l.SourceTo),
		Items: items, CreatedAt: l.CreatedAt, UpdatedAt: l.UpdatedAt,
	}
}

func toAPIShoppingItem(it service.ShoppingItem) api.ShoppingItem {
	unit := nullable.NewNullNullable[api.ShoppingItemUnit]()
	if it.Unit != nil {
		unit = nullable.NewNullableWithValue(api.ShoppingItemUnit(*it.Unit))
	}
	return api.ShoppingItem{
		Id: it.ID, ListId: it.ListID, IngredientId: toNullableUUID(it.IngredientID), Name: it.Name,
		Quantity: toNullable(it.Quantity), Unit: unit, Category: api.IngredientCategory(it.Category),
		Checked: it.Checked, CheckedBy: toNullableUUID(it.CheckedBy), Position: it.Position, Version: it.Version,
		Origin: api.ShoppingItemOrigin(it.Origin), CreatedAt: it.CreatedAt, UpdatedAt: it.UpdatedAt,
	}
}

func toNullableUUID(v *uuid.UUID) nullable.Nullable[openapi_types.UUID] {
	if v == nil {
		return nullable.NewNullNullable[openapi_types.UUID]()
	}
	return nullable.NewNullableWithValue(*v)
}

func toNullableDate(v *time.Time) nullable.Nullable[openapi_types.Date] {
	if v == nil {
		return nullable.NewNullNullable[openapi_types.Date]()
	}
	return nullable.NewNullableWithValue(openapi_types.Date{Time: *v})
}

type shoppingListCursorPayload struct {
	CreatedAt time.Time `json:"c"`
	ID        uuid.UUID `json:"i"`
}

func encodeShoppingListCursor(c service.ShoppingListCursor) string {
	b, _ := json.Marshal(shoppingListCursorPayload{CreatedAt: c.CreatedAt, ID: c.ID})
	return base64.RawURLEncoding.EncodeToString(b)
}

func decodeShoppingListCursor(s string) (service.ShoppingListCursor, bool) {
	b, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return service.ShoppingListCursor{}, false
	}
	var p shoppingListCursorPayload
	if err := json.Unmarshal(b, &p); err != nil || p.CreatedAt.IsZero() || p.ID == uuid.Nil {
		return service.ShoppingListCursor{}, false
	}
	return service.ShoppingListCursor{CreatedAt: p.CreatedAt, ID: p.ID}, true
}
