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

// IngredientsService is what the ingredients handlers need from the
// ingredients service.
type IngredientsService interface {
	Create(ctx context.Context, ownerID uuid.UUID, in service.CreateIngredientInput) (service.Ingredient, error)
	Update(ctx context.Context, ownerID, id uuid.UUID, in service.UpdateIngredientInput) (service.Ingredient, error)
	Delete(ctx context.Context, ownerID, id uuid.UUID) error
	List(ctx context.Context, userID uuid.UUID, in service.ListIngredientsInput) (service.IngredientPage, error)
	Search(ctx context.Context, userID uuid.UUID, query string, category *string, limit int) ([]service.Ingredient, error)
}

const defaultIngredientLimit = 20

func (s *server) ListIngredients(w http.ResponseWriter, r *http.Request, params api.ListIngredientsParams) {
	userID, ok := requireUser(w, r)
	if !ok {
		return
	}
	limit := defaultIngredientLimit
	if params.Limit != nil {
		limit = *params.Limit
	}
	var category *string
	if params.Category != nil {
		c := string(*params.Category)
		category = &c
	}

	if params.Q != nil {
		items, err := s.ingredients.Search(r.Context(), userID, *params.Q, category, limit)
		if err != nil {
			s.writeServiceError(w, r, err)
			return
		}
		writeJSON(w, http.StatusOK, toIngredientList(items, ""))
		return
	}

	var cursor *service.IngredientCursor
	if params.Cursor != nil {
		c, ok := decodeIngredientCursor(*params.Cursor)
		if !ok {
			WriteValidationProblem(w, "cursor is invalid", []FieldError{{Field: "cursor", Code: FieldInvalidForm}})
			return
		}
		cursor = &c
	}
	page, err := s.ingredients.List(r.Context(), userID, service.ListIngredientsInput{Category: category, Cursor: cursor, Limit: limit})
	if err != nil {
		s.writeServiceError(w, r, err)
		return
	}
	next := ""
	if page.NextCursor != nil {
		next = encodeIngredientCursor(*page.NextCursor)
	}
	writeJSON(w, http.StatusOK, toIngredientList(page.Items, next))
}

func (s *server) CreateIngredient(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUser(w, r)
	if !ok {
		return
	}
	var req api.CreateIngredientRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	in := service.CreateIngredientInput{
		Name: req.Name, Category: string(req.Category),
		GramsPerPiece: nullableToPtr(req.GramsPerPiece), DensityGPerMl: nullableToPtr(req.DensityGPerMl),
	}
	if req.Nutrients != nil {
		in.Nutrients = nutrientsFromAPI(*req.Nutrients)
	}
	ing, err := s.ingredients.Create(r.Context(), userID, in)
	if err != nil {
		s.writeServiceError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, toAPIIngredient(ing))
}

func (s *server) UpdateIngredient(w http.ResponseWriter, r *http.Request, id uuid.UUID) {
	userID, ok := requireUser(w, r)
	if !ok {
		return
	}
	var req api.UpdateIngredientRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	in := service.UpdateIngredientInput{
		Name:          req.Name,
		GramsPerPiece: toOptional(req.GramsPerPiece),
		DensityGPerMl: toOptional(req.DensityGPerMl),
	}
	if req.Category != nil {
		c := string(*req.Category)
		in.Category = &c
	}
	if req.Nutrients != nil {
		n := nutrientsFromAPI(*req.Nutrients)
		in.Nutrients = n
	}
	ing, err := s.ingredients.Update(r.Context(), userID, id, in)
	if err != nil {
		s.writeServiceError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, toAPIIngredient(ing))
}

func (s *server) DeleteIngredient(w http.ResponseWriter, r *http.Request, id uuid.UUID) {
	userID, ok := requireUser(w, r)
	if !ok {
		return
	}
	if err := s.ingredients.Delete(r.Context(), userID, id); err != nil {
		s.writeServiceError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func nullableToPtr(n nullable.Nullable[float64]) *float64 {
	if !n.IsSpecified() || n.IsNull() {
		return nil
	}
	v := n.MustGet()
	return &v
}

func toIngredientList(items []service.Ingredient, nextCursor string) api.IngredientList {
	list := api.IngredientList{Items: make([]api.Ingredient, len(items))}
	for i, ing := range items {
		list.Items[i] = toAPIIngredient(ing)
	}
	if nextCursor == "" {
		list.NextCursor = nullable.NewNullNullable[string]()
	} else {
		list.NextCursor = nullable.NewNullableWithValue(nextCursor)
	}
	return list
}

func toAPIIngredient(ing service.Ingredient) api.Ingredient {
	return api.Ingredient{
		Id: ing.ID, Name: ing.Name, Category: api.IngredientCategory(ing.Category), IsCustom: ing.IsCustom,
		GramsPerPiece: toNullable(ing.GramsPerPiece), DensityGPerMl: toNullable(ing.DensityGPerMl),
		Nutrients: nutrientsToAPI(ing.Nutrients),
		CreatedAt: ing.CreatedAt, UpdatedAt: ing.UpdatedAt,
	}
}

type ingredientCursorPayload struct {
	Name string    `json:"n"`
	ID   uuid.UUID `json:"i"`
}

func encodeIngredientCursor(c service.IngredientCursor) string {
	b, _ := json.Marshal(ingredientCursorPayload{Name: c.Name, ID: c.ID})
	return base64.RawURLEncoding.EncodeToString(b)
}

func decodeIngredientCursor(s string) (service.IngredientCursor, bool) {
	b, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return service.IngredientCursor{}, false
	}
	var p ingredientCursorPayload
	if err := json.Unmarshal(b, &p); err != nil || p.Name == "" || p.ID == uuid.Nil {
		return service.IngredientCursor{}, false
	}
	return service.IngredientCursor{Name: p.Name, ID: p.ID}, true
}
