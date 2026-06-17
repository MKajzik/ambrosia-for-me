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
	date := c.Query("date")

	rows, err := h.db.Query(
		"SELECT id, name, date, meal_type, recipe, created_at, updated_at FROM meals WHERE date = ? ORDER BY meal_type",
		date,
	)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	defer rows.Close()

	var meals []model.Meal
	for rows.Next() {
		var m model.Meal
		if err := rows.Scan(&m.ID, &m.Name, &m.Date, &m.MealType, &m.Recipe, &m.CreatedAt, &m.UpdatedAt); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		meals = append(meals, m)
	}

	if meals == nil {
		meals = []model.Meal{}
	}

	c.JSON(http.StatusOK, meals)
}

func (h *MealHandler) Create(c *gin.Context) {
	var m model.Meal
	if err := c.ShouldBindJSON(&m); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	result, err := h.db.Exec(
		"INSERT INTO meals (name, date, meal_type, recipe) VALUES (?, ?, ?, ?)",
		m.Name, m.Date, m.MealType, m.Recipe,
	)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	m.ID, _ = result.LastInsertId()
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
		"SELECT id, name, date, meal_type, recipe, created_at, updated_at FROM meals WHERE id = ?", id,
	).Scan(&m.ID, &m.Name, &m.Date, &m.MealType, &m.Recipe, &m.CreatedAt, &m.UpdatedAt)

	if err == sql.ErrNoRows {
		c.JSON(http.StatusNotFound, gin.H{"error": "meal not found"})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
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

	_, err = h.db.Exec(
		"UPDATE meals SET name=?, date=?, meal_type=?, recipe=?, updated_at=CURRENT_TIMESTAMP WHERE id=?",
		m.Name, m.Date, m.MealType, m.Recipe, id,
	)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	m.ID = id
	c.JSON(http.StatusOK, m)
}

func (h *MealHandler) Delete(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}

	_, err = h.db.Exec("DELETE FROM meals WHERE id=?", id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusNoContent, nil)
}
