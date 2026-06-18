package model

import "time"

// Meal represents a meal with a type, instructions, and ingredient list.
type Meal struct {
	ID           int64     `json:"id" readOnly:"true"`
	Name         string    `json:"name" binding:"required"`
	MealType     string    `json:"meal_type" binding:"required,oneof=breakfast lunch dinner snack"`
	Instructions string    `json:"instructions"`
	Ingredients  []MealIngredient `json:"ingredients"`
	CreatedAt    time.Time `json:"created_at" readOnly:"true"`
	UpdatedAt    time.Time `json:"updated_at" readOnly:"true"`
}
