package model

// ShoppingItem is a single ingredient line in a shopping list.
type ShoppingItem struct {
	IngredientID   int64   `json:"ingredient_id"`
	IngredientName string  `json:"ingredient_name"`
	TotalQuantity  float64 `json:"total_quantity"`
	Unit           string  `json:"unit"`
}

// ShoppingCategory groups shopping items by ingredient category.
type ShoppingCategory struct {
	Name  string         `json:"name"`
	Items []ShoppingItem `json:"items"`
}

// ShoppingList is the full shopping list for a meal plan date range.
type ShoppingList struct {
	MealPlanID int64              `json:"meal_plan_id"`
	FromDate   string             `json:"from_date"`
	ToDate     string             `json:"to_date"`
	Categories []ShoppingCategory `json:"categories"`
}
