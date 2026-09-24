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

// MealsService is what the meals handlers need from the meals service.
type MealsService interface {
	Create(ctx context.Context, ownerID uuid.UUID, in service.CreateMealInput) (service.Meal, error)
	Get(ctx context.Context, ownerID, id uuid.UUID) (service.Meal, error)
	Update(ctx context.Context, ownerID, id uuid.UUID, in service.UpdateMealInput) (service.Meal, error)
	Delete(ctx context.Context, ownerID, id uuid.UUID) error
	List(ctx context.Context, ownerID uuid.UUID, in service.ListMealsInput) (service.MealPage, error)
	ReplaceIngredients(ctx context.Context, ownerID, id uuid.UUID, items []service.MealIngredientInput) (service.Meal, error)
	Copy(ctx context.Context, callerID, id uuid.UUID) (service.Meal, error)
	ListPartner(ctx context.Context, callerID uuid.UUID, in service.ListMealsInput) (service.MealPage, error)
}

const defaultMealLimit = 20

func (s *server) ListMeals(w http.ResponseWriter, r *http.Request, params api.ListMealsParams) {
	s.serveMealList(w, r, params.Cursor, params.Limit, s.meals.List)
}

func (s *server) ListPartnerMeals(w http.ResponseWriter, r *http.Request, params api.ListPartnerMealsParams) {
	s.serveMealList(w, r, params.Cursor, params.Limit, s.meals.ListPartner)
}

// serveMealList answers a cursor-paginated meal listing; list is either the
// caller's own listing or the partner's.
func (s *server) serveMealList(w http.ResponseWriter, r *http.Request, cursorParam *string, limitParam *int,
	list func(context.Context, uuid.UUID, service.ListMealsInput) (service.MealPage, error)) {
	userID, ok := requireUser(w, r)
	if !ok {
		return
	}
	limit := defaultMealLimit
	if limitParam != nil {
		limit = *limitParam
	}
	var cursor *service.MealCursor
	if cursorParam != nil {
		c, ok := decodeMealCursor(*cursorParam)
		if !ok {
			WriteValidationProblem(w, "cursor is invalid", []FieldError{{Field: "cursor", Code: FieldInvalidForm}})
			return
		}
		cursor = &c
	}
	page, err := list(r.Context(), userID, service.ListMealsInput{Cursor: cursor, Limit: limit})
	if err != nil {
		s.writeServiceError(w, r, err)
		return
	}
	next := ""
	if page.NextCursor != nil {
		next = encodeMealCursor(*page.NextCursor)
	}
	writeJSON(w, http.StatusOK, toMealList(page.Items, next))
}

func (s *server) CreateMeal(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUser(w, r)
	if !ok {
		return
	}
	var req api.CreateMealRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	in := service.CreateMealInput{
		Name: req.Name, Notes: nullableStringToPtr(req.Notes), Servings: req.Servings,
	}
	if req.SharedWithPartner != nil {
		in.SharedWithPartner = *req.SharedWithPartner
	}
	meal, err := s.meals.Create(r.Context(), userID, in)
	if err != nil {
		s.writeServiceError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, toAPIMeal(meal, userID))
}

func (s *server) GetMeal(w http.ResponseWriter, r *http.Request, id uuid.UUID) {
	userID, ok := requireUser(w, r)
	if !ok {
		return
	}
	meal, err := s.meals.Get(r.Context(), userID, id)
	if err != nil {
		s.writeServiceError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, toAPIMeal(meal, userID))
}

func (s *server) UpdateMeal(w http.ResponseWriter, r *http.Request, id uuid.UUID) {
	userID, ok := requireUser(w, r)
	if !ok {
		return
	}
	var req api.UpdateMealRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	in := service.UpdateMealInput{
		Name: req.Name, Notes: toOptionalString(req.Notes), Servings: req.Servings, SharedWithPartner: req.SharedWithPartner,
	}
	meal, err := s.meals.Update(r.Context(), userID, id, in)
	if err != nil {
		s.writeServiceError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, toAPIMeal(meal, userID))
}

func (s *server) DeleteMeal(w http.ResponseWriter, r *http.Request, id uuid.UUID) {
	userID, ok := requireUser(w, r)
	if !ok {
		return
	}
	if err := s.meals.Delete(r.Context(), userID, id); err != nil {
		s.writeServiceError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *server) ReplaceMealIngredients(w http.ResponseWriter, r *http.Request, id uuid.UUID) {
	userID, ok := requireUser(w, r)
	if !ok {
		return
	}
	var req api.ReplaceMealIngredientsRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	items := make([]service.MealIngredientInput, len(req.Items))
	for i, it := range req.Items {
		items[i] = service.MealIngredientInput{IngredientID: it.IngredientId, Quantity: it.Quantity, Unit: string(it.Unit)}
	}
	meal, err := s.meals.ReplaceIngredients(r.Context(), userID, id, items)
	if err != nil {
		s.writeServiceError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, toAPIMeal(meal, userID))
}

func (s *server) CopyMeal(w http.ResponseWriter, r *http.Request, id uuid.UUID) {
	userID, ok := requireUser(w, r)
	if !ok {
		return
	}
	meal, err := s.meals.Copy(r.Context(), userID, id)
	if err != nil {
		s.writeServiceError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, toAPIMeal(meal, userID))
}

func nullableStringToPtr(n nullable.Nullable[string]) *string {
	if !n.IsSpecified() || n.IsNull() {
		return nil
	}
	v := n.MustGet()
	return &v
}

func toOptionalString(n nullable.Nullable[string]) service.Optional[string] {
	switch {
	case !n.IsSpecified():
		return service.Optional[string]{}
	case n.IsNull():
		return service.Set[string](nil)
	default:
		v := n.MustGet()
		return service.Set(&v)
	}
}

func toNullableString(v *string) nullable.Nullable[string] {
	if v == nil {
		return nullable.NewNullNullable[string]()
	}
	return nullable.NewNullableWithValue(*v)
}

func toMealList(items []service.MealSummary, nextCursor string) api.MealList {
	list := api.MealList{Items: make([]api.MealSummary, len(items))}
	for i, m := range items {
		list.Items[i] = toAPIMealSummary(m)
	}
	if nextCursor == "" {
		list.NextCursor = nullable.NewNullNullable[string]()
	} else {
		list.NextCursor = nullable.NewNullableWithValue(nextCursor)
	}
	return list
}

func toAPIMealSummary(m service.MealSummary) api.MealSummary {
	return api.MealSummary{
		Id: m.ID, Name: m.Name, Notes: toNullableString(m.Notes), Servings: m.Servings,
		SharedWithPartner: m.SharedWithPartner, CreatedAt: m.CreatedAt, UpdatedAt: m.UpdatedAt,
	}
}

func toAPIMeal(m service.Meal, viewer uuid.UUID) api.Meal {
	ingredients := make([]api.MealIngredient, len(m.Ingredients))
	for i, mi := range m.Ingredients {
		ingredients[i] = api.MealIngredient{
			Id: mi.ID, IngredientId: mi.IngredientID, IngredientName: mi.IngredientName,
			IngredientCategory: api.IngredientCategory(mi.IngredientCategory),
			Quantity:           mi.Quantity, Unit: api.Unit(mi.Unit), Position: mi.Position,
		}
	}
	return api.Meal{
		Id: m.ID, Name: m.Name, Notes: toNullableString(m.Notes), Servings: m.Servings,
		SharedWithPartner: m.SharedWithPartner, IsOwner: m.OwnerID == viewer, Ingredients: ingredients,
		NutritionPerServing: nutrientsToAPI(m.NutritionPerServing),
		CreatedAt:           m.CreatedAt, UpdatedAt: m.UpdatedAt,
	}
}

type mealCursorPayload struct {
	Name string    `json:"n"`
	ID   uuid.UUID `json:"i"`
}

func encodeMealCursor(c service.MealCursor) string {
	b, _ := json.Marshal(mealCursorPayload{Name: c.Name, ID: c.ID})
	return base64.RawURLEncoding.EncodeToString(b)
}

func decodeMealCursor(s string) (service.MealCursor, bool) {
	b, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return service.MealCursor{}, false
	}
	var p mealCursorPayload
	if err := json.Unmarshal(b, &p); err != nil || p.Name == "" || p.ID == uuid.Nil {
		return service.MealCursor{}, false
	}
	return service.MealCursor{Name: p.Name, ID: p.ID}, true
}
