package usda_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/InzKazik/mealplanner/backend/internal/usda"
)

func TestFetchFoundationFoodsPagesUntilEmpty(t *testing.T) {
	var calls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		switch r.URL.Query().Get("pageNumber") {
		case "1":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"foods":[{"fdcId":1,"description":"Apple","foodCategory":"Fruits and Fruit Juices","foodNutrients":[{"nutrientNumber":"208","nutrientName":"Energy","unitName":"KCAL","value":52}]}]}`))
		default:
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"foods":[]}`))
		}
	}))
	defer server.Close()

	client := usda.NewClient("test-key")
	client.BaseURL = server.URL
	client.HTTP = server.Client()

	foods, err := client.FetchFoundationFoods(context.Background())
	if err != nil {
		t.Fatalf("FetchFoundationFoods: %v", err)
	}
	if len(foods) != 1 || foods[0].FdcID != 1 || foods[0].Description != "Apple" || foods[0].FoodCategory != "Fruits and Fruit Juices" {
		t.Fatalf("foods = %+v", foods)
	}
	if len(foods[0].FoodNutrients) != 1 || foods[0].FoodNutrients[0].NutrientNumber != "208" || foods[0].FoodNutrients[0].Value != 52 {
		t.Errorf("nutrients = %+v", foods[0].FoodNutrients)
	}
	if foods[0].FoodNutrients[0].UnitName != "KCAL" {
		t.Errorf("unit name = %q, want KCAL", foods[0].FoodNutrients[0].UnitName)
	}
	if calls != 2 {
		t.Errorf("calls = %d, want 2 (one page of results, one empty page to stop)", calls)
	}
}

func TestFetchFoundationFoodsRetriesOnRateLimit(t *testing.T) {
	var calls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls == 1 {
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"foods":[]}`))
	}))
	defer server.Close()

	client := usda.NewClient("test-key")
	client.BaseURL = server.URL
	client.HTTP = server.Client()

	foods, err := client.FetchFoundationFoods(context.Background())
	if err != nil {
		t.Fatalf("FetchFoundationFoods: %v", err)
	}
	if len(foods) != 0 {
		t.Errorf("foods = %+v, want none", foods)
	}
	if calls < 2 {
		t.Errorf("calls = %d, want at least 2 (a retry after the 429)", calls)
	}
}
