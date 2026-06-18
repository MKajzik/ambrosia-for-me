package handler

import (
	"database/sql"
	"net/http"
	"sort"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/kazik/mealPlanner/internal/model"
)

type ShoppingListHandler struct {
	db *sql.DB
}

func NewShoppingListHandler(db *sql.DB) *ShoppingListHandler {
	return &ShoppingListHandler{db: db}
}

type aggregatedItem struct {
	name     string
	quantity float64
	baseUnit string
	pkgSize  *float64
	category string
}

// Generate godoc
// @Summary      Generate shopping list
// @Description  Returns an aggregated shopping list for a meal plan, grouped by category (own only)
// @Tags         shopping-list
// @Security     BearerAuth
// @Produce      json
// @Param        id         path      int     true   "Meal plan ID"
// @Param        from_date  query     string  false  "Start date (YYYY-MM-DD, defaults to plan start)"
// @Param        to_date    query     string  false  "End date (YYYY-MM-DD, defaults to plan end)"
// @Success      200        {object}  model.ShoppingList
// @Failure      400        {object}  map[string]string
// @Failure      401        {object}  map[string]string
// @Failure      404        {object}  map[string]string
// @Failure      500        {object}  map[string]string
// @Router       /meal-plans/{id}/shopping-list [get]
func (h *ShoppingListHandler) Generate(c *gin.Context) {
	userID := c.GetInt64("userID")
	planID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid plan id"})
		return
	}

	var plan model.MealPlan
	err = h.db.QueryRow(
		"SELECT id, name, start_date, end_date FROM meal_plans WHERE id = ? AND user_id = ?", planID, userID,
	).Scan(&plan.ID, &plan.Name, &plan.StartDate, &plan.EndDate)

	if err == sql.ErrNoRows {
		c.JSON(http.StatusNotFound, gin.H{"error": "meal plan not found"})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	fromDate := c.DefaultQuery("from_date", plan.StartDate)
	toDate := c.DefaultQuery("to_date", plan.EndDate)

	if fromDate < plan.StartDate {
		fromDate = plan.StartDate
	}
	if toDate > plan.EndDate {
		toDate = plan.EndDate
	}
	if fromDate > toDate {
		c.JSON(http.StatusBadRequest, gin.H{"error": "from_date must be <= to_date"})
		return
	}

	rows, err := h.db.Query(
		`SELECT mi.ingredient_id, i.name, mi.quantity, mi.unit, i.base_unit, i.package_size, i.category
		 FROM meal_plan_entries mpe
		 JOIN meal_ingredients mi ON mi.meal_id = mpe.meal_id
		 JOIN ingredients i ON i.id = mi.ingredient_id
		 WHERE mpe.meal_plan_id = ? AND mpe.date >= ? AND mpe.date <= ?`,
		planID, fromDate, toDate,
	)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	defer rows.Close()

	agg := map[int64]*aggregatedItem{}

	for rows.Next() {
		var (
			ingredientID int64
			name         string
			qty          float64
			unit         string
			baseUnit     string
			pkgSize      *float64
			category     string
		)
		if err := rows.Scan(&ingredientID, &name, &qty, &unit, &baseUnit, &pkgSize, &category); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		normalizedQty := normalizeToBase(qty, unit, baseUnit, pkgSize)

		if existing, ok := agg[ingredientID]; ok {
			existing.quantity += normalizedQty
		} else {
			agg[ingredientID] = &aggregatedItem{
				name:     name,
				quantity: normalizedQty,
				baseUnit: baseUnit,
				pkgSize:  pkgSize,
				category: category,
			}
		}
	}

	categories := map[string][]model.ShoppingItem{}
	for id, item := range agg {
		categories[item.category] = append(categories[item.category], model.ShoppingItem{
			IngredientID:   id,
			IngredientName: item.name,
			TotalQuantity:  item.quantity,
			Unit:           item.baseUnit,
		})
	}

	result := model.ShoppingList{
		MealPlanID: planID,
		FromDate:   fromDate,
		ToDate:     toDate,
		Categories: []model.ShoppingCategory{},
	}

	catNames := make([]string, 0, len(categories))
	for name := range categories {
		catNames = append(catNames, name)
	}
	sort.Strings(catNames)

	for _, catName := range catNames {
		items := categories[catName]
		sort.Slice(items, func(i, j int) bool {
			return items[i].IngredientName < items[j].IngredientName
		})
		result.Categories = append(result.Categories, model.ShoppingCategory{
			Name:  catName,
			Items: items,
		})
	}

	c.JSON(http.StatusOK, result)
}

func normalizeToBase(qty float64, unit string, baseUnit string, pkgSize *float64) float64 {
	switch unit {
	case "g":
		return qty
	case "kg":
		return qty * 1000
	case "ml":
		return qty
	case "l":
		return qty * 1000
	case "pcs":
		return qty
	case "pkg":
		if pkgSize != nil {
			return qty * *pkgSize
		}
		return qty
	default:
		return qty
	}
}
