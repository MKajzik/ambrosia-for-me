package model

import "time"

type Meal struct {
	ID        int64     `json:"id"`
	Name      string    `json:"name" binding:"required"`
	Date      string    `json:"date" binding:"required"` // YYYY-MM-DD
	MealType  string    `json:"meal_type" binding:"required,oneof=breakfast lunch dinner snack"`
	Recipe    string    `json:"recipe"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}
