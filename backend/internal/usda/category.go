// Package usda loads the USDA FoodData Central Foundation Foods dataset.
package usda

// categoryMap translates FoodData Central food-group names to our shopping
// categories. A food group not listed here falls back to "other"; Import
// logs a warning when that happens so the table can be extended.
var categoryMap = map[string]string{
	"Dairy and Egg Products":              "dairy_eggs",
	"Spices and Herbs":                    "spices_herbs",
	"Baby Foods":                          "other",
	"Fats and Oils":                       "condiments_oils",
	"Poultry Products":                    "meat_seafood",
	"Soups, Sauces, and Gravies":          "condiments_oils",
	"Sausages and Luncheon Meats":         "meat_seafood",
	"Breakfast Cereals":                   "grains_bread",
	"Fruits and Fruit Juices":             "produce",
	"Pork Products":                       "meat_seafood",
	"Vegetables and Vegetable Products":   "produce",
	"Nut and Seed Products":               "legumes_nuts_seeds",
	"Beef Products":                       "meat_seafood",
	"Beverages":                           "beverages",
	"Finfish and Shellfish Products":      "meat_seafood",
	"Legumes and Legume Products":         "legumes_nuts_seeds",
	"Lamb, Veal, and Game Products":       "meat_seafood",
	"Baked Products":                      "grains_bread",
	"Sweets":                              "sweets_snacks",
	"Cereal Grains and Pasta":             "grains_bread",
	"Fast Foods":                          "other",
	"Meals, Entrees, and Side Dishes":     "other",
	"Snacks":                              "sweets_snacks",
	"American Indian/Alaska Native Foods": "other",
	"Restaurant Foods":                    "other",
}

// MapCategory returns our shopping category for a FoodData Central food
// group, and whether it was recognised. An unrecognised group maps to
// "other".
func MapCategory(usdaCategory string) (category string, known bool) {
	c, ok := categoryMap[usdaCategory]
	if !ok {
		return "other", false
	}
	return c, true
}
