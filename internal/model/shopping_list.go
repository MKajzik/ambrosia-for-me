package model

// ShoppingItem is a single ingredient line in a shopping list.
type ShoppingItem struct {
	ID             int64   `json:"id,omitempty"`
	IngredientID   int64   `json:"ingredient_id"`
	IngredientName string  `json:"ingredient_name"`
	TotalQuantity  float64 `json:"total_quantity"`
	Unit           string  `json:"unit"`
	Checked        bool    `json:"checked"`
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

// ShoppingListSaved is a persisted shopping list with DB-backed items.
type ShoppingListSaved struct {
	ID         int64              `json:"id" readOnly:"true"`
	MealPlanID int64              `json:"meal_plan_id"`
	Shared     bool               `json:"shared"`
	FromDate   string             `json:"from_date"`
	ToDate     string             `json:"to_date"`
	Categories []ShoppingCategory `json:"categories"`
}
