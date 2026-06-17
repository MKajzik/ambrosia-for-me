package model

type ShoppingItem struct {
	IngredientID   int64   `json:"ingredient_id"`
	IngredientName string  `json:"ingredient_name"`
	TotalQuantity  float64 `json:"total_quantity"`
	Unit           string  `json:"unit"`
}

type ShoppingCategory struct {
	Name  string         `json:"name"`
	Items []ShoppingItem `json:"items"`
}

type ShoppingList struct {
	MealPlanID int64              `json:"meal_plan_id"`
	FromDate   string             `json:"from_date"`
	ToDate     string             `json:"to_date"`
	Categories []ShoppingCategory `json:"categories"`
}
