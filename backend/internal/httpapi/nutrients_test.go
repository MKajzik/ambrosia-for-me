package httpapi

import (
	"testing"

	"github.com/InzKazik/mealplanner/backend/internal/api"
	"github.com/InzKazik/mealplanner/backend/internal/service"
)

// TestNutrientRoundTripKeepsAllEighteenKeys guards against nutrientsToAPI or
// nutrientsFromAPI silently dropping one of the 18 nutrient keys. Because
// nullable.Nullable[T] marshals an unspecified field as its zero value (0),
// not as an absence, a dropped field would produce a legal-looking but wrong
// 0 rather than a loud failure, so this test names any missing, extra or
// wrong-value key explicitly instead of relying on marshaling to catch it.
func TestNutrientRoundTripKeepsAllEighteenKeys(t *testing.T) {
	want := map[string]float64{
		service.NutrientCalories:      1,
		service.NutrientProtein:       2,
		service.NutrientCarbohydrates: 3,
		service.NutrientSugar:         4,
		service.NutrientFibre:         5,
		service.NutrientFat:           6,
		service.NutrientSaturatedFat:  7,
		service.NutrientSodium:        8,
		service.NutrientPotassium:     9,
		service.NutrientCalcium:       10,
		service.NutrientIron:          11,
		service.NutrientMagnesium:     12,
		service.NutrientZinc:          13,
		service.NutrientVitaminA:      14,
		service.NutrientVitaminC:      15,
		service.NutrientVitaminD:      16,
		service.NutrientVitaminB12:    17,
		service.NutrientFolate:        18,
	}
	if len(want) != 18 {
		t.Fatalf("test setup: want has %d keys, expected 18", len(want))
	}

	// Round-trip: map -> NutrientAmounts (nutrientsToAPI) -> NutrientAmountsInput
	// (identical field set and order, so a plain type conversion carries every
	// field across) -> map (nutrientsFromAPI).
	amounts := nutrientsToAPI(want)
	got := nutrientsFromAPI(api.NutrientAmountsInput(amounts))

	if len(got) != 18 {
		t.Errorf("round trip produced %d keys, want 18: %v", len(got), got)
	}
	for key, wantVal := range want {
		gotVal, ok := got[key]
		if !ok {
			t.Errorf("round trip is missing key %q (want %v)", key, wantVal)
			continue
		}
		if gotVal != wantVal {
			t.Errorf("round trip key %q = %v, want %v", key, gotVal, wantVal)
		}
	}
	for key, gotVal := range got {
		if _, ok := want[key]; !ok {
			t.Errorf("round trip produced unexpected extra key %q = %v", key, gotVal)
		}
	}
}
