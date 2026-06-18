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

// List godoc
// @Summary      List ingredients
// @Description  Returns all ingredients for the authenticated user, optionally filtered by name
// @Tags         ingredients
// @Security     BearerAuth
// @Produce      json
// @Param        search  query     string  false  "Filter by name (substring match)"
// @Success      200     {array}   model.Ingredient
// @Failure      401     {object}  map[string]string
// @Failure      500     {object}  map[string]string
// @Router       /ingredients [get]
func (h *IngredientHandler) List(c *gin.Context) {
	userID := c.GetInt64("userID")
	search := c.Query("search")

	query := `SELECT id, name, base_unit, category, calories_per_100, protein_per_100, fat_per_100,
	 carbs_per_100, fiber_per_100, salt_per_100, sugars_per_100, saturated_fat_per_100,
	 package_size, created_at, updated_at FROM ingredients WHERE user_id = ?`
	args := []any{userID}
	if search != "" {
		query += " AND name LIKE ?"
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

// Create godoc
// @Summary      Create ingredient
// @Description  Creates a new ingredient for the authenticated user
// @Tags         ingredients
// @Security     BearerAuth
// @Accept       json
// @Produce      json
// @Param        ingredient  body      model.Ingredient  true  "Ingredient to create"
// @Success      201         {object}  model.Ingredient
// @Failure      400         {object}  map[string]string
// @Failure      401         {object}  map[string]string
// @Failure      500         {object}  map[string]string
// @Router       /ingredients [post]
func (h *IngredientHandler) Create(c *gin.Context) {
	userID := c.GetInt64("userID")

	var ing model.Ingredient
	if err := c.ShouldBindJSON(&ing); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	result, err := h.db.Exec(
		`INSERT INTO ingredients (user_id, name, base_unit, category, calories_per_100, protein_per_100, fat_per_100,
		 carbs_per_100, fiber_per_100, salt_per_100, sugars_per_100, saturated_fat_per_100, package_size)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		userID, ing.Name, ing.BaseUnit, ing.Category, ing.CaloriesPer100, ing.ProteinPer100, ing.FatPer100,
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

// Get godoc
// @Summary      Get ingredient
// @Description  Returns a single ingredient by ID (own only)
// @Tags         ingredients
// @Security     BearerAuth
// @Produce      json
// @Param        id   path      int  true  "Ingredient ID"
// @Success      200  {object}  model.Ingredient
// @Failure      400  {object}  map[string]string
// @Failure      401  {object}  map[string]string
// @Failure      404  {object}  map[string]string
// @Router       /ingredients/{id} [get]
func (h *IngredientHandler) Get(c *gin.Context) {
	userID := c.GetInt64("userID")
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
		 FROM ingredients WHERE id = ? AND user_id = ?`, id, userID,
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

// Update godoc
// @Summary      Update ingredient
// @Description  Replaces an ingredient by ID (own only)
// @Tags         ingredients
// @Security     BearerAuth
// @Accept       json
// @Produce      json
// @Param        id         path      int               true  "Ingredient ID"
// @Param        ingredient body      model.Ingredient  true  "Updated ingredient"
// @Success      200        {object}  model.Ingredient
// @Failure      400        {object}  map[string]string
// @Failure      401        {object}  map[string]string
// @Failure      404        {object}  map[string]string
// @Failure      500        {object}  map[string]string
// @Router       /ingredients/{id} [put]
func (h *IngredientHandler) Update(c *gin.Context) {
	userID := c.GetInt64("userID")
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
		 saturated_fat_per_100=?, package_size=?, updated_at=CURRENT_TIMESTAMP WHERE id=? AND user_id=?`,
		ing.Name, ing.BaseUnit, ing.Category, ing.CaloriesPer100, ing.ProteinPer100, ing.FatPer100,
		ing.CarbsPer100, ing.FiberPer100, ing.SaltPer100, ing.SugarsPer100, ing.SaturatedFatPer100,
		ing.PackageSize, id, userID,
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

// Delete godoc
// @Summary      Delete ingredient
// @Description  Deletes an ingredient by ID (own only)
// @Tags         ingredients
// @Security     BearerAuth
// @Param        id   path  int  true  "Ingredient ID"
// @Success      204
// @Failure      400  {object}  map[string]string
// @Failure      401  {object}  map[string]string
// @Failure      404  {object}  map[string]string
// @Router       /ingredients/{id} [delete]
func (h *IngredientHandler) Delete(c *gin.Context) {
	userID := c.GetInt64("userID")
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}

	result, err := h.db.Exec("DELETE FROM ingredients WHERE id=? AND user_id=?", id, userID)
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
