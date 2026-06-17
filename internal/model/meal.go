package model

import "time"

type Meal struct {
	ID           int64     `json:"id"`
	Name         string    `json:"name" binding:"required"`
	MealType     string    `json:"meal_type" binding:"required,oneof=breakfast lunch dinner snack"`
	Instructions string    `json:"instructions"`
	Ingredients  []MealIngredient `json:"ingredients"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}
