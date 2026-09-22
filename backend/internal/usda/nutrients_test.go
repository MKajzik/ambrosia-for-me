package usda_test

import (
	"testing"

	"github.com/InzKazik/mealplanner/backend/internal/service"
	"github.com/InzKazik/mealplanner/backend/internal/usda"
)

func TestMapNutrients(t *testing.T) {
	in := []usda.FoodNutrient{
		{NutrientNumber: "208", Value: 64.7}, // calories
		{NutrientNumber: "203", Value: 0.15}, // protein
		{NutrientNumber: "999", Value: 1},    // unrecognised, ignored
	}
	got := usda.MapNutrients(in)
	if len(got) != 2 {
		t.Fatalf("MapNutrients returned %d entries, want 2 (unrecognised numbers ignored): %v", len(got), got)
	}
	if got[service.NutrientCalories] != 64.7 {
		t.Errorf("calories = %v, want 64.7", got[service.NutrientCalories])
	}
	if got[service.NutrientProtein] != 0.15 {
		t.Errorf("protein = %v, want 0.15", got[service.NutrientProtein])
	}
}
