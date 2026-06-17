package model

import "time"

type Ingredient struct {
	ID                 int64     `json:"id"`
	Name               string    `json:"name" binding:"required"`
	BaseUnit           string    `json:"base_unit" binding:"required,oneof=g ml pcs"`
	Category           string    `json:"category" binding:"required,oneof=Owoce Warzywa Mięso Ryby Nabiał Pieczywo 'Produkty zbożowe' Przyprawy Napoje Słodycze Tłuszcze Inne"`
	CaloriesPer100     float64   `json:"calories_per_100" binding:"required"`
	ProteinPer100      float64   `json:"protein_per_100"`
	FatPer100          float64   `json:"fat_per_100"`
	CarbsPer100        float64   `json:"carbs_per_100"`
	FiberPer100        float64   `json:"fiber_per_100"`
	SaltPer100         float64   `json:"salt_per_100"`
	SugarsPer100       float64   `json:"sugars_per_100"`
	SaturatedFatPer100 float64   `json:"saturated_fat_per_100"`
	PackageSize        *float64  `json:"package_size"`
	CreatedAt          time.Time `json:"created_at"`
	UpdatedAt          time.Time `json:"updated_at"`
}

type MealIngredient struct {
	ID           int64   `json:"id"`
	MealID       int64   `json:"meal_id"`
	IngredientID int64   `json:"ingredient_id" binding:"required"`
	Quantity     float64 `json:"quantity" binding:"required,gt=0"`
	Unit         string  `json:"unit" binding:"required,oneof=g ml l pcs pkg"`
}
