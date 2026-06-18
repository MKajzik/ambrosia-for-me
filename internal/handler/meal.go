package handler

import (
	"database/sql"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/kazik/mealPlanner/internal/model"
)

type MealHandler struct {
	db *sql.DB
}

func NewMealHandler(db *sql.DB) *MealHandler {
	return &MealHandler{db: db}
}

// List godoc
// @Summary      List meals
// @Description  Returns all meals for the authenticated user, optionally filtered by type
// @Tags         meals
// @Security     BearerAuth
// @Produce      json
// @Param        meal_type  query     string  false  "Filter by type"  Enums(breakfast, lunch, dinner, snack)
// @Success      200        {array}   model.Meal
// @Failure      401        {object}  map[string]string
// @Failure      500        {object}  map[string]string
// @Router       /meals [get]
func (h *MealHandler) List(c *gin.Context) {
	userID := c.GetInt64("userID")
	mealType := c.Query("meal_type")
	includePartner := c.Query("include_partner") == "true"

	query := "SELECT m.id, m.name, m.meal_type, m.instructions, m.created_at, m.updated_at, '' FROM meals m WHERE m.user_id = ?"
	args := []any{userID}
	if mealType != "" {
		query += " AND m.meal_type = ?"
		args = append(args, mealType)
	}

	if includePartner {
		var partnerID *int64
		h.db.QueryRow("SELECT partner_id FROM users WHERE id = ?", userID).Scan(&partnerID)
		if partnerID != nil {
			query = `SELECT m.id, m.name, m.meal_type, m.instructions, m.created_at, m.updated_at, u.username
				FROM meals m JOIN users u ON u.id = m.user_id
				WHERE (m.user_id = ? OR m.user_id = ?)`
			args = []any{userID, *partnerID}
			if mealType != "" {
				query += " AND m.meal_type = ?"
				args = append(args, mealType)
			}
		}
	}
	query += " ORDER BY m.name"

	rows, err := h.db.Query(query, args...)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	defer rows.Close()

	meals := []map[string]any{}
	for rows.Next() {
		var m model.Meal
		var owner string
		if err := rows.Scan(&m.ID, &m.Name, &m.MealType, &m.Instructions, &m.CreatedAt, &m.UpdatedAt, &owner); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		entry := map[string]any{
			"id":           m.ID,
			"name":         m.Name,
			"meal_type":    m.MealType,
			"instructions": m.Instructions,
			"created_at":   m.CreatedAt,
			"updated_at":   m.UpdatedAt,
			"ingredients":  []model.MealIngredient{},
		}
		if includePartner && owner != "" {
			entry["owner"] = owner
		}
		meals = append(meals, entry)
	}

	c.JSON(http.StatusOK, meals)
}

// Create godoc
// @Summary      Create meal
// @Description  Creates a new meal with ingredients for the authenticated user
// @Tags         meals
// @Security     BearerAuth
// @Accept       json
// @Produce      json
// @Param        meal  body      model.Meal  true  "Meal to create"
// @Success      201   {object}  model.Meal
// @Failure      400   {object}  map[string]string
// @Failure      401   {object}  map[string]string
// @Failure      500   {object}  map[string]string
// @Router       /meals [post]
func (h *MealHandler) Create(c *gin.Context) {
	userID := c.GetInt64("userID")

	var m model.Meal
	if err := c.ShouldBindJSON(&m); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	tx, err := h.db.Begin()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	defer func() { _ = tx.Rollback() }()

	result, err := tx.Exec(
		"INSERT INTO meals (user_id, name, meal_type, instructions) VALUES (?, ?, ?, ?)",
		userID, m.Name, m.MealType, m.Instructions,
	)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	m.ID, _ = result.LastInsertId()

	for i := range m.Ingredients {
		res, err := tx.Exec(
			"INSERT INTO meal_ingredients (meal_id, ingredient_id, quantity, unit) VALUES (?, ?, ?, ?)",
			m.ID, m.Ingredients[i].IngredientID, m.Ingredients[i].Quantity, m.Ingredients[i].Unit,
		)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		m.Ingredients[i].ID, _ = res.LastInsertId()
		m.Ingredients[i].MealID = m.ID
	}

	if err := tx.Commit(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	if m.Ingredients == nil {
		m.Ingredients = []model.MealIngredient{}
	}

	c.JSON(http.StatusCreated, m)
}

// Get godoc
// @Summary      Get meal
// @Description  Returns a single meal with ingredients by ID (own only)
// @Tags         meals
// @Security     BearerAuth
// @Produce      json
// @Param        id   path      int  true  "Meal ID"
// @Success      200  {object}  model.Meal
// @Failure      400  {object}  map[string]string
// @Failure      401  {object}  map[string]string
// @Failure      404  {object}  map[string]string
// @Router       /meals/{id} [get]
func (h *MealHandler) Get(c *gin.Context) {
	userID := c.GetInt64("userID")
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}

	var m model.Meal
	err = h.db.QueryRow(
		"SELECT id, name, meal_type, instructions, created_at, updated_at FROM meals WHERE id = ? AND user_id = ?", id, userID,
	).Scan(&m.ID, &m.Name, &m.MealType, &m.Instructions, &m.CreatedAt, &m.UpdatedAt)

	if err == sql.ErrNoRows {
		c.JSON(http.StatusNotFound, gin.H{"error": "meal not found"})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	rows, err := h.db.Query(
		"SELECT id, meal_id, ingredient_id, quantity, unit FROM meal_ingredients WHERE meal_id = ?", id,
	)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	defer rows.Close()

	m.Ingredients = []model.MealIngredient{}
	for rows.Next() {
		var mi model.MealIngredient
		if err := rows.Scan(&mi.ID, &mi.MealID, &mi.IngredientID, &mi.Quantity, &mi.Unit); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		m.Ingredients = append(m.Ingredients, mi)
	}

	c.JSON(http.StatusOK, m)
}

// Update godoc
// @Summary      Update meal
// @Description  Replaces a meal and its ingredients by ID (own only)
// @Tags         meals
// @Security     BearerAuth
// @Accept       json
// @Produce      json
// @Param        id   path      int        true  "Meal ID"
// @Param        meal body      model.Meal true  "Updated meal"
// @Success      200  {object}  model.Meal
// @Failure      400  {object}  map[string]string
// @Failure      401  {object}  map[string]string
// @Failure      404  {object}  map[string]string
// @Failure      500  {object}  map[string]string
// @Router       /meals/{id} [put]
func (h *MealHandler) Update(c *gin.Context) {
	userID := c.GetInt64("userID")
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}

	var m model.Meal
	if err := c.ShouldBindJSON(&m); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	tx, err := h.db.Begin()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	defer func() { _ = tx.Rollback() }()

	result, err := tx.Exec(
		"UPDATE meals SET name=?, meal_type=?, instructions=?, updated_at=CURRENT_TIMESTAMP WHERE id=? AND user_id=?",
		m.Name, m.MealType, m.Instructions, id, userID,
	)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	rowsAffected, _ := result.RowsAffected()
	if rowsAffected == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "meal not found"})
		return
	}

	if _, err := tx.Exec("DELETE FROM meal_ingredients WHERE meal_id=?", id); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	for i := range m.Ingredients {
		res, err := tx.Exec(
			"INSERT INTO meal_ingredients (meal_id, ingredient_id, quantity, unit) VALUES (?, ?, ?, ?)",
			id, m.Ingredients[i].IngredientID, m.Ingredients[i].Quantity, m.Ingredients[i].Unit,
		)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		m.Ingredients[i].ID, _ = res.LastInsertId()
		m.Ingredients[i].MealID = id
	}

	if err := tx.Commit(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	m.ID = id
	if m.Ingredients == nil {
		m.Ingredients = []model.MealIngredient{}
	}
	c.JSON(http.StatusOK, m)
}

// Delete godoc
// @Summary      Delete meal
// @Description  Deletes a meal and its ingredients by ID (own only)
// @Tags         meals
// @Security     BearerAuth
// @Param        id   path  int  true  "Meal ID"
// @Success      204
// @Failure      400  {object}  map[string]string
// @Failure      401  {object}  map[string]string
// @Failure      404  {object}  map[string]string
// @Router       /meals/{id} [delete]
func (h *MealHandler) Delete(c *gin.Context) {
	userID := c.GetInt64("userID")
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}

	result, err := h.db.Exec("DELETE FROM meals WHERE id=? AND user_id=?", id, userID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	rowsAffected, _ := result.RowsAffected()
	if rowsAffected == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "meal not found"})
		return
	}

	c.JSON(http.StatusNoContent, nil)
}
