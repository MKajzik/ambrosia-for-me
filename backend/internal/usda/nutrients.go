package usda

import (
	"strings"

	"github.com/InzKazik/mealplanner/backend/internal/service"
)

// nutrientNumberMap translates FoodData Central nutrient numbers to our
// nutrient keys. Only numbers we track appear here; every other nutrient
// FoodData Central reports is ignored. Confirmed against live API responses
// for every number except "401" (Vitamin C), which was not checked against a
// live response during development; it is USDA's long-stable, well-
// documented nutrient number for total ascorbic acid.
var nutrientNumberMap = map[string]string{
	"208": service.NutrientCalories,
	"203": service.NutrientProtein,
	"205": service.NutrientCarbohydrates,
	"269": service.NutrientSugar,
	"291": service.NutrientFibre,
	"204": service.NutrientFat,
	"606": service.NutrientSaturatedFat,
	"307": service.NutrientSodium,
	"306": service.NutrientPotassium,
	"301": service.NutrientCalcium,
	"303": service.NutrientIron,
	"304": service.NutrientMagnesium,
	"309": service.NutrientZinc,
	"320": service.NutrientVitaminA,
	"401": service.NutrientVitaminC,
	"328": service.NutrientVitaminD,
	"418": service.NutrientVitaminB12,
	"417": service.NutrientFolate,
}

// nutrientUnitMap gives the unit FoodData Central is expected to report each
// nutrient in (its own spelling: "KCAL", "G", "MG", "UG", as seen in live API
// responses), matching the units documented on NutrientAmounts in
// openapi.yaml. Comparison against it is case-insensitive.
var nutrientUnitMap = map[string]string{
	service.NutrientCalories:      "KCAL",
	service.NutrientProtein:       "G",
	service.NutrientCarbohydrates: "G",
	service.NutrientSugar:         "G",
	service.NutrientFibre:         "G",
	service.NutrientFat:           "G",
	service.NutrientSaturatedFat:  "G",
	service.NutrientSodium:        "MG",
	service.NutrientPotassium:     "MG",
	service.NutrientCalcium:       "MG",
	service.NutrientIron:          "MG",
	service.NutrientMagnesium:     "MG",
	service.NutrientZinc:          "MG",
	service.NutrientVitaminC:      "MG",
	service.NutrientVitaminA:      "UG",
	service.NutrientVitaminD:      "UG",
	service.NutrientVitaminB12:    "UG",
	service.NutrientFolate:        "UG",
}

// UnitMismatch describes a recognised nutrient whose FoodData Central unit
// did not match nutrientUnitMap, so MapNutrients skipped it rather than
// import a value that may be in the wrong unit.
type UnitMismatch struct {
	NutrientKey string
	WantUnit    string
	GotUnit     string
}

// MapNutrients converts a food's nutrient list to our nutrient keys, per
// 100 g. Unrecognised nutrient numbers are ignored. A recognised nutrient
// whose reported unit doesn't match nutrientUnitMap is skipped and reported
// in mismatches instead of being imported — mirroring MapCategory's
// (category, known) shape, the caller (Import) decides how to log it.
func MapNutrients(in []FoodNutrient) (out map[string]float64, mismatches []UnitMismatch) {
	out = make(map[string]float64, len(nutrientNumberMap))
	for _, n := range in {
		key, ok := nutrientNumberMap[n.NutrientNumber]
		if !ok {
			continue
		}
		want := nutrientUnitMap[key]
		if !strings.EqualFold(want, n.UnitName) {
			mismatches = append(mismatches, UnitMismatch{NutrientKey: key, WantUnit: want, GotUnit: n.UnitName})
			continue
		}
		out[key] = n.Value
	}
	return out, mismatches
}
