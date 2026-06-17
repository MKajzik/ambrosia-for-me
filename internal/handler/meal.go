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

func (h *MealHandler) List(c *gin.Context) {
	mealType := c.Query("meal_type")

	query := "SELECT id, name, meal_type, instructions, created_at, updated_at FROM meals"
	args := []any{}
	if mealType != "" {
		query += " WHERE meal_type = ?"
		args = append(args, mealType)
	}
	query += " ORDER BY name"

	rows, err := h.db.Query(query, args...)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	defer rows.Close()

	meals := []model.Meal{}
	for rows.Next() {
		var m model.Meal
		if err := rows.Scan(&m.ID, &m.Name, &m.MealType, &m.Instructions, &m.CreatedAt, &m.UpdatedAt); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		m.Ingredients = []model.MealIngredient{}
		meals = append(meals, m)
	}

	c.JSON(http.StatusOK, meals)
}

func (h *MealHandler) Create(c *gin.Context) {
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
		"INSERT INTO meals (name, meal_type, instructions) VALUES (?, ?, ?)",
		m.Name, m.MealType, m.Instructions,
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

func (h *MealHandler) Get(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}

	var m model.Meal
	err = h.db.QueryRow(
		"SELECT id, name, meal_type, instructions, created_at, updated_at FROM meals WHERE id = ?", id,
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

func (h *MealHandler) Update(c *gin.Context) {
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
		"UPDATE meals SET name=?, meal_type=?, instructions=?, updated_at=CURRENT_TIMESTAMP WHERE id=?",
		m.Name, m.MealType, m.Instructions, id,
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

func (h *MealHandler) Delete(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}

	result, err := h.db.Exec("DELETE FROM meals WHERE id=?", id)
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
