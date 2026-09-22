package httpapi

import (
	"github.com/oapi-codegen/nullable"

	"github.com/InzKazik/mealplanner/backend/internal/api"
	"github.com/InzKazik/mealplanner/backend/internal/service"
)

// nutrientsFromAPI returns one map entry per key present in n with a non-null
// value; an absent or explicitly null key is simply omitted.
func nutrientsFromAPI(n api.NutrientAmounts) map[string]float64 {
	m := make(map[string]float64, 18)
	set := func(key string, v nullable.Nullable[float64]) {
		if v.IsSpecified() && !v.IsNull() {
			m[key] = v.MustGet()
		}
	}
	set(service.NutrientCalories, n.Calories)
	set(service.NutrientProtein, n.Protein)
	set(service.NutrientCarbohydrates, n.Carbohydrates)
	set(service.NutrientSugar, n.Sugar)
	set(service.NutrientFibre, n.Fibre)
	set(service.NutrientFat, n.Fat)
	set(service.NutrientSaturatedFat, n.SaturatedFat)
	set(service.NutrientSodium, n.Sodium)
	set(service.NutrientPotassium, n.Potassium)
	set(service.NutrientCalcium, n.Calcium)
	set(service.NutrientIron, n.Iron)
	set(service.NutrientMagnesium, n.Magnesium)
	set(service.NutrientZinc, n.Zinc)
	set(service.NutrientVitaminA, n.VitaminA)
	set(service.NutrientVitaminC, n.VitaminC)
	set(service.NutrientVitaminD, n.VitaminD)
	set(service.NutrientVitaminB12, n.VitaminB12)
	set(service.NutrientFolate, n.Folate)
	return m
}

// nutrientsToAPI renders a nutrient map with every key present, null where
// the ingredient has no value.
func nutrientsToAPI(m map[string]float64) api.NutrientAmounts {
	get := func(key string) nullable.Nullable[float64] {
		if v, ok := m[key]; ok {
			return nullable.NewNullableWithValue(v)
		}
		return nullable.NewNullNullable[float64]()
	}
	return api.NutrientAmounts{
		Calories: get(service.NutrientCalories), Protein: get(service.NutrientProtein),
		Carbohydrates: get(service.NutrientCarbohydrates), Sugar: get(service.NutrientSugar),
		Fibre: get(service.NutrientFibre), Fat: get(service.NutrientFat),
		SaturatedFat: get(service.NutrientSaturatedFat), Sodium: get(service.NutrientSodium),
		Potassium: get(service.NutrientPotassium), Calcium: get(service.NutrientCalcium),
		Iron: get(service.NutrientIron), Magnesium: get(service.NutrientMagnesium),
		Zinc: get(service.NutrientZinc), VitaminA: get(service.NutrientVitaminA),
		VitaminC: get(service.NutrientVitaminC), VitaminD: get(service.NutrientVitaminD),
		VitaminB12: get(service.NutrientVitaminB12), Folate: get(service.NutrientFolate),
	}
}
