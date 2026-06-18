package model

import "time"

// MealPlan represents a named meal plan covering a date range.
type MealPlan struct {
	ID        int64     `json:"id" readOnly:"true"`
	Name      string    `json:"name" binding:"required"`
	StartDate string    `json:"start_date" binding:"required"` // YYYY-MM-DD
	EndDate   string    `json:"end_date" binding:"required"`   // YYYY-MM-DD
	CreatedAt time.Time `json:"created_at" readOnly:"true"`
	UpdatedAt time.Time `json:"updated_at" readOnly:"true"`
}

// MealPlanEntry is a single assignment of a meal to a date+slot.
type MealPlanEntry struct {
	ID         int64     `json:"id" readOnly:"true"`
	MealPlanID int64     `json:"meal_plan_id"`
	Date       string    `json:"date" binding:"required"` // YYYY-MM-DD
	MealType   string    `json:"meal_type" binding:"required,oneof=breakfast lunch dinner snack"`
	MealID     int64     `json:"meal_id" binding:"required"`
	CreatedAt  time.Time `json:"created_at" readOnly:"true"`
	UpdatedAt  time.Time `json:"updated_at" readOnly:"true"`
}

// MealPlanEntryWithMeal extends MealPlanEntry with the meal name and recipe.
type MealPlanEntryWithMeal struct {
	MealPlanEntry
	MealName   string `json:"meal_name"`
	MealRecipe string `json:"meal_recipe"`
}
