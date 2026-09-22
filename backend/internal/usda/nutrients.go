package usda

import "github.com/InzKazik/mealplanner/backend/internal/service"

// nutrientNumberMap translates FoodData Central nutrient numbers to our
// nutrient keys. Only numbers we track appear here; every other nutrient
// FoodData Central reports is ignored. Confirmed against live API responses
// except "401" (Vitamin C): verify it against Task 8's fixture and correct
// this table if it differs.
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

// MapNutrients converts a food's nutrient list to our nutrient keys, per
// 100 g. Unrecognised nutrient numbers are ignored.
func MapNutrients(in []FoodNutrient) map[string]float64 {
	out := make(map[string]float64, len(nutrientNumberMap))
	for _, n := range in {
		if key, ok := nutrientNumberMap[n.NutrientNumber]; ok {
			out[key] = n.Value
		}
	}
	return out
}
