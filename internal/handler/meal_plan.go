package handler

import (
	"database/sql"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/kazik/mealPlanner/internal/model"
)

type MealPlanHandler struct {
	db *sql.DB
}

func NewMealPlanHandler(db *sql.DB) *MealPlanHandler {
	return &MealPlanHandler{db: db}
}

// List godoc
// @Summary      List meal plans
// @Description  Returns all meal plans ordered by start date descending
// @Tags         meal-plans
// @Produce      json
// @Success      200  {array}   model.MealPlan
// @Failure      500  {object}  map[string]string
// @Router       /meal-plans [get]
func (h *MealPlanHandler) List(c *gin.Context) {
	rows, err := h.db.Query(
		"SELECT id, name, start_date, end_date, created_at, updated_at FROM meal_plans ORDER BY start_date DESC",
	)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	defer rows.Close()

	plans := []model.MealPlan{}
	for rows.Next() {
		var p model.MealPlan
		if err := rows.Scan(&p.ID, &p.Name, &p.StartDate, &p.EndDate, &p.CreatedAt, &p.UpdatedAt); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		plans = append(plans, p)
	}

	c.JSON(http.StatusOK, plans)
}

type mealPlanRequest struct {
	model.MealPlan
	Entries []model.MealPlanEntry `json:"entries"`
}

// Create godoc
// @Summary      Create meal plan
// @Description  Creates a new meal plan with optional entries
// @Tags         meal-plans
// @Accept       json
// @Produce      json
// @Param        plan  body      mealPlanRequest  true  "Meal plan to create"
// @Success      201   {object}  mealPlanRequest
// @Failure      400   {object}  map[string]string
// @Failure      500   {object}  map[string]string
// @Router       /meal-plans [post]
func (h *MealPlanHandler) Create(c *gin.Context) {
	var req mealPlanRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if req.EndDate < req.StartDate {
		c.JSON(http.StatusBadRequest, gin.H{"error": "end_date must be >= start_date"})
		return
	}

	tx, err := h.db.Begin()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	defer func() { _ = tx.Rollback() }()

	result, err := tx.Exec(
		"INSERT INTO meal_plans (name, start_date, end_date) VALUES (?, ?, ?)",
		req.Name, req.StartDate, req.EndDate,
	)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	req.ID, _ = result.LastInsertId()

	for i := range req.Entries {
		if req.Entries[i].Date < req.StartDate || req.Entries[i].Date > req.EndDate {
			c.JSON(http.StatusBadRequest, gin.H{"error": "entry date must be within plan date range"})
			return
		}
		res, err := tx.Exec(
			"INSERT INTO meal_plan_entries (meal_plan_id, date, meal_type, meal_id) VALUES (?, ?, ?, ?)",
			req.ID, req.Entries[i].Date, req.Entries[i].MealType, req.Entries[i].MealID,
		)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		req.Entries[i].ID, _ = res.LastInsertId()
		req.Entries[i].MealPlanID = req.ID
	}

	if err := tx.Commit(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	if req.Entries == nil {
		req.Entries = []model.MealPlanEntry{}
	}

	c.JSON(http.StatusCreated, req)
}

// Get godoc
// @Summary      Get meal plan
// @Description  Returns a meal plan with its entries (including meal names)
// @Tags         meal-plans
// @Produce      json
// @Param        id   path      int  true  "Meal plan ID"
// @Success      200  {object}  map[string]interface{}
// @Failure      400  {object}  map[string]string
// @Failure      404  {object}  map[string]string
// @Router       /meal-plans/{id} [get]
func (h *MealPlanHandler) Get(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}

	var p model.MealPlan
	err = h.db.QueryRow(
		"SELECT id, name, start_date, end_date, created_at, updated_at FROM meal_plans WHERE id = ?", id,
	).Scan(&p.ID, &p.Name, &p.StartDate, &p.EndDate, &p.CreatedAt, &p.UpdatedAt)

	if err == sql.ErrNoRows {
		c.JSON(http.StatusNotFound, gin.H{"error": "meal plan not found"})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	rows, err := h.db.Query(
		`SELECT mpe.id, mpe.meal_plan_id, mpe.date, mpe.meal_type, mpe.meal_id,
		 mpe.created_at, mpe.updated_at, m.name, m.instructions
		 FROM meal_plan_entries mpe
		 JOIN meals m ON m.id = mpe.meal_id
		 WHERE mpe.meal_plan_id = ?
		 ORDER BY mpe.date, mpe.meal_type`, id,
	)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	defer rows.Close()

	entries := []model.MealPlanEntryWithMeal{}
	for rows.Next() {
		var e model.MealPlanEntryWithMeal
		if err := rows.Scan(
			&e.ID, &e.MealPlanID, &e.Date, &e.MealType, &e.MealID,
			&e.CreatedAt, &e.UpdatedAt, &e.MealName, &e.MealRecipe,
		); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		entries = append(entries, e)
	}

	c.JSON(http.StatusOK, gin.H{
		"id":         p.ID,
		"name":       p.Name,
		"start_date": p.StartDate,
		"end_date":   p.EndDate,
		"created_at": p.CreatedAt,
		"updated_at": p.UpdatedAt,
		"entries":    entries,
	})
}

// Update godoc
// @Summary      Update meal plan
// @Description  Replaces a meal plan and its entries by ID
// @Tags         meal-plans
// @Accept       json
// @Produce      json
// @Param        id   path      int               true  "Meal plan ID"
// @Param        plan body      mealPlanRequest   true  "Updated meal plan"
// @Success      200  {object}  mealPlanRequest
// @Failure      400  {object}  map[string]string
// @Failure      404  {object}  map[string]string
// @Failure      500  {object}  map[string]string
// @Router       /meal-plans/{id} [put]
func (h *MealPlanHandler) Update(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}

	var req mealPlanRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if req.EndDate < req.StartDate {
		c.JSON(http.StatusBadRequest, gin.H{"error": "end_date must be >= start_date"})
		return
	}

	tx, err := h.db.Begin()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	defer func() { _ = tx.Rollback() }()

	result, err := tx.Exec(
		"UPDATE meal_plans SET name=?, start_date=?, end_date=?, updated_at=CURRENT_TIMESTAMP WHERE id=?",
		req.Name, req.StartDate, req.EndDate, id,
	)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	rowsAffected, _ := result.RowsAffected()
	if rowsAffected == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "meal plan not found"})
		return
	}

	if _, err := tx.Exec("DELETE FROM meal_plan_entries WHERE meal_plan_id=?", id); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	for i := range req.Entries {
		if req.Entries[i].Date < req.StartDate || req.Entries[i].Date > req.EndDate {
			c.JSON(http.StatusBadRequest, gin.H{"error": "entry date must be within plan date range"})
			return
		}
		res, err := tx.Exec(
			"INSERT INTO meal_plan_entries (meal_plan_id, date, meal_type, meal_id) VALUES (?, ?, ?, ?)",
			id, req.Entries[i].Date, req.Entries[i].MealType, req.Entries[i].MealID,
		)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		req.Entries[i].ID, _ = res.LastInsertId()
		req.Entries[i].MealPlanID = id
	}

	if err := tx.Commit(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	req.ID = id
	if req.Entries == nil {
		req.Entries = []model.MealPlanEntry{}
	}
	c.JSON(http.StatusOK, req)
}

// Delete godoc
// @Summary      Delete meal plan
// @Description  Deletes a meal plan and its entries by ID
// @Tags         meal-plans
// @Param        id   path  int  true  "Meal plan ID"
// @Success      204
// @Failure      400  {object}  map[string]string
// @Failure      404  {object}  map[string]string
// @Failure      500  {object}  map[string]string
// @Router       /meal-plans/{id} [delete]
func (h *MealPlanHandler) Delete(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}

	result, err := h.db.Exec("DELETE FROM meal_plans WHERE id=?", id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	rowsAffected, _ := result.RowsAffected()
	if rowsAffected == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "meal plan not found"})
		return
	}

	c.JSON(http.StatusNoContent, nil)
}
