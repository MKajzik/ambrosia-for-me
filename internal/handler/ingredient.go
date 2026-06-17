package handler

import (
	"database/sql"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/kazik/mealPlanner/internal/model"
)

type IngredientHandler struct {
	db *sql.DB
}

func NewIngredientHandler(db *sql.DB) *IngredientHandler {
	return &IngredientHandler{db: db}
}

func (h *IngredientHandler) List(c *gin.Context) {
	search := c.Query("search")

	query := `SELECT id, name, base_unit, category, calories_per_100, protein_per_100, fat_per_100,
	 carbs_per_100, fiber_per_100, salt_per_100, sugars_per_100, saturated_fat_per_100,
	 package_size, created_at, updated_at FROM ingredients`
	args := []any{}
	if search != "" {
		query += " WHERE name LIKE ?"
		args = append(args, "%"+search+"%")
	}
	query += " ORDER BY name"

	rows, err := h.db.Query(query, args...)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	defer rows.Close()

	ingredients := []model.Ingredient{}
	for rows.Next() {
		var ing model.Ingredient
		if err := rows.Scan(
			&ing.ID, &ing.Name, &ing.BaseUnit, &ing.Category, &ing.CaloriesPer100, &ing.ProteinPer100,
			&ing.FatPer100, &ing.CarbsPer100, &ing.FiberPer100, &ing.SaltPer100,
			&ing.SugarsPer100, &ing.SaturatedFatPer100, &ing.PackageSize,
			&ing.CreatedAt, &ing.UpdatedAt,
		); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		ingredients = append(ingredients, ing)
	}

	c.JSON(http.StatusOK, ingredients)
}

func (h *IngredientHandler) Create(c *gin.Context) {
	var ing model.Ingredient
	if err := c.ShouldBindJSON(&ing); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	result, err := h.db.Exec(
		`INSERT INTO ingredients (name, base_unit, category, calories_per_100, protein_per_100, fat_per_100,
		 carbs_per_100, fiber_per_100, salt_per_100, sugars_per_100, saturated_fat_per_100, package_size)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		ing.Name, ing.BaseUnit, ing.Category, ing.CaloriesPer100, ing.ProteinPer100, ing.FatPer100,
		ing.CarbsPer100, ing.FiberPer100, ing.SaltPer100, ing.SugarsPer100, ing.SaturatedFatPer100,
		ing.PackageSize,
	)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	ing.ID, _ = result.LastInsertId()
	c.JSON(http.StatusCreated, ing)
}

func (h *IngredientHandler) Get(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}

	var ing model.Ingredient
	err = h.db.QueryRow(
		`SELECT id, name, base_unit, category, calories_per_100, protein_per_100, fat_per_100,
		 carbs_per_100, fiber_per_100, salt_per_100, sugars_per_100, saturated_fat_per_100,
		 package_size, created_at, updated_at
		 FROM ingredients WHERE id = ?`, id,
	).Scan(
		&ing.ID, &ing.Name, &ing.BaseUnit, &ing.Category, &ing.CaloriesPer100, &ing.ProteinPer100,
		&ing.FatPer100, &ing.CarbsPer100, &ing.FiberPer100, &ing.SaltPer100,
		&ing.SugarsPer100, &ing.SaturatedFatPer100, &ing.PackageSize,
		&ing.CreatedAt, &ing.UpdatedAt,
	)

	if err == sql.ErrNoRows {
		c.JSON(http.StatusNotFound, gin.H{"error": "ingredient not found"})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, ing)
}

func (h *IngredientHandler) Update(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}

	var ing model.Ingredient
	if err := c.ShouldBindJSON(&ing); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	result, err := h.db.Exec(
		`UPDATE ingredients SET name=?, base_unit=?, category=?, calories_per_100=?, protein_per_100=?,
		 fat_per_100=?, carbs_per_100=?, fiber_per_100=?, salt_per_100=?, sugars_per_100=?,
		 saturated_fat_per_100=?, package_size=?, updated_at=CURRENT_TIMESTAMP WHERE id=?`,
		ing.Name, ing.BaseUnit, ing.Category, ing.CaloriesPer100, ing.ProteinPer100, ing.FatPer100,
		ing.CarbsPer100, ing.FiberPer100, ing.SaltPer100, ing.SugarsPer100, ing.SaturatedFatPer100,
		ing.PackageSize, id,
	)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	rowsAffected, _ := result.RowsAffected()
	if rowsAffected == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "ingredient not found"})
		return
	}

	ing.ID = id
	c.JSON(http.StatusOK, ing)
}

func (h *IngredientHandler) Delete(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}

	result, err := h.db.Exec("DELETE FROM ingredients WHERE id=?", id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	rowsAffected, _ := result.RowsAffected()
	if rowsAffected == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "ingredient not found"})
		return
	}

	c.JSON(http.StatusNoContent, nil)
}
