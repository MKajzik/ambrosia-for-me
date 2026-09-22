package httpapi

import (
	"net/http"

	openapi_types "github.com/oapi-codegen/runtime/types"

	"github.com/InzKazik/mealplanner/backend/internal/api"
)

// MealsService is what the meal handlers need from the meals service.
type MealsService interface {
	// TODO: Add meal service interface methods
}

func (s *server) ListMeals(w http.ResponseWriter, r *http.Request, params api.ListMealsParams) {
	// TODO: Implement list meals
	w.WriteHeader(http.StatusNotImplemented)
}

func (s *server) CreateMeal(w http.ResponseWriter, r *http.Request) {
	// TODO: Implement create meal
	w.WriteHeader(http.StatusNotImplemented)
}

func (s *server) GetMeal(w http.ResponseWriter, r *http.Request, id openapi_types.UUID) {
	// TODO: Implement get meal
	w.WriteHeader(http.StatusNotImplemented)
}

func (s *server) UpdateMeal(w http.ResponseWriter, r *http.Request, id openapi_types.UUID) {
	// TODO: Implement update meal
	w.WriteHeader(http.StatusNotImplemented)
}

func (s *server) DeleteMeal(w http.ResponseWriter, r *http.Request, id openapi_types.UUID) {
	// TODO: Implement delete meal
	w.WriteHeader(http.StatusNotImplemented)
}

func (s *server) ReplaceMealIngredients(w http.ResponseWriter, r *http.Request, id openapi_types.UUID) {
	// TODO: Implement replace meal ingredients
	w.WriteHeader(http.StatusNotImplemented)
}

func (s *server) CopyMeal(w http.ResponseWriter, r *http.Request, id openapi_types.UUID) {
	// TODO: Implement copy meal
	w.WriteHeader(http.StatusNotImplemented)
}
