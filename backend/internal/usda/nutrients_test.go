package usda_test

import (
	"testing"

	"github.com/InzKazik/mealplanner/backend/internal/service"
	"github.com/InzKazik/mealplanner/backend/internal/usda"
)

func TestMapNutrients(t *testing.T) {
	in := []usda.FoodNutrient{
		{NutrientNumber: "208", Value: 64.7, UnitName: "KCAL"}, // calories
		{NutrientNumber: "203", Value: 0.15, UnitName: "G"},    // protein
		{NutrientNumber: "999", Value: 1, UnitName: "G"},       // unrecognised, ignored
	}
	got, mismatches := usda.MapNutrients(in)
	if len(got) != 2 {
		t.Fatalf("MapNutrients returned %d entries, want 2 (unrecognised numbers ignored): %v", len(got), got)
	}
	if got[service.NutrientCalories] != 64.7 {
		t.Errorf("calories = %v, want 64.7", got[service.NutrientCalories])
	}
	if got[service.NutrientProtein] != 0.15 {
		t.Errorf("protein = %v, want 0.15", got[service.NutrientProtein])
	}
	if len(mismatches) != 0 {
		t.Errorf("mismatches = %+v, want none (units all match)", mismatches)
	}
}

func TestMapNutrientsIsCaseInsensitiveAboutUnits(t *testing.T) {
	in := []usda.FoodNutrient{
		{NutrientNumber: "208", Value: 64.7, UnitName: "kcal"}, // calories, lowercase unit
	}
	got, mismatches := usda.MapNutrients(in)
	if got[service.NutrientCalories] != 64.7 {
		t.Errorf("calories = %v, want 64.7 (case-insensitive unit match)", got[service.NutrientCalories])
	}
	if len(mismatches) != 0 {
		t.Errorf("mismatches = %+v, want none", mismatches)
	}
}

func TestMapNutrientsSkipsAndReportsAMismatchedUnit(t *testing.T) {
	in := []usda.FoodNutrient{
		{NutrientNumber: "208", Value: 64.7, UnitName: "KCAL"}, // calories, correct unit
		{NutrientNumber: "307", Value: 5, UnitName: "G"},       // sodium reported in G instead of MG
	}
	got, mismatches := usda.MapNutrients(in)
	if _, ok := got[service.NutrientSodium]; ok {
		t.Errorf("got[sodium] = %v, want sodium skipped (unit mismatch)", got[service.NutrientSodium])
	}
	if got[service.NutrientCalories] != 64.7 {
		t.Errorf("calories = %v, want 64.7 (unaffected by sodium's mismatch)", got[service.NutrientCalories])
	}
	if len(mismatches) != 1 {
		t.Fatalf("mismatches = %+v, want exactly 1", mismatches)
	}
	m := mismatches[0]
	if m.NutrientKey != service.NutrientSodium || m.WantUnit != "MG" || m.GotUnit != "G" {
		t.Errorf("mismatch = %+v, want {NutrientKey:sodium WantUnit:MG GotUnit:G}", m)
	}
}
