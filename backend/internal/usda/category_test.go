package usda_test

import (
	"testing"

	"github.com/InzKazik/mealplanner/backend/internal/usda"
)

func TestMapCategory(t *testing.T) {
	tests := []struct {
		usda  string
		want  string
		known bool
	}{
		{"Fruits and Fruit Juices", "produce", true},
		{"Vegetables and Vegetable Products", "produce", true},
		{"Dairy and Egg Products", "dairy_eggs", true},
		{"Finfish and Shellfish Products", "meat_seafood", true},
		{"Nut and Seed Products", "legumes_nuts_seeds", true},
		{"Some New Category FDC Adds Later", "other", false},
	}
	for _, tt := range tests {
		t.Run(tt.usda, func(t *testing.T) {
			got, known := usda.MapCategory(tt.usda)
			if got != tt.want || known != tt.known {
				t.Errorf("MapCategory(%q) = (%q, %v), want (%q, %v)", tt.usda, got, known, tt.want, tt.known)
			}
		})
	}
}
