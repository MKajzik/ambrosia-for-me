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

// Save godoc
// @Summary      Save shopping list
// @Description  Generates and persists a shopping list for a meal plan. shared=true combines partner's plan ingredients.
// @Tags         shopping-list
// @Security     BearerAuth
// @Produce      json
// @Param        id      path      int    true   "Meal plan ID"
// @Param        shared  query     bool   false  "Include partner's meal plan"
// @Success      201     {object}  model.ShoppingListSaved
// @Failure      400     {object}  map[string]string
// @Failure      404     {object}  map[string]string
// @Router       /meal-plans/{id}/shopping-list/save [post]
func (h *ShoppingListHandler) Save(c *gin.Context) {
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

	shared := c.Query("shared") == "true"

	fromDate := plan.StartDate
	toDate := plan.EndDate

	// aggregate ingredients
	agg := map[int64]*aggregatedItem{}

	query := `SELECT mi.ingredient_id, i.name, mi.quantity, mi.unit, i.base_unit, i.package_size, i.category
		 FROM meal_plan_entries mpe
		 JOIN meal_ingredients mi ON mi.meal_id = mpe.meal_id
		 JOIN ingredients i ON i.id = mi.ingredient_id
		 WHERE mpe.meal_plan_id = ? AND mpe.date >= ? AND mpe.date <= ?`

	args := []any{planID, fromDate, toDate}

	if shared {
		var partnerID *int64
		h.db.QueryRow("SELECT partner_id FROM users WHERE id = ?", userID).Scan(&partnerID)
		if partnerID != nil {
			// get partner's meal plans overlapping same dates
			query = `SELECT mi.ingredient_id, i.name, mi.quantity, mi.unit, i.base_unit, i.package_size, i.category
				 FROM meal_plan_entries mpe
				 JOIN meal_ingredients mi ON mi.meal_id = mpe.meal_id
				 JOIN ingredients i ON i.id = mi.ingredient_id
				 JOIN meal_plans mp ON mp.id = mpe.meal_plan_id
				 WHERE ((mpe.meal_plan_id = ?) OR (mp.user_id = ? AND mpe.date >= ? AND mpe.date <= ?))
				 AND mpe.date >= ? AND mpe.date <= ?`
			args = []any{planID, *partnerID, fromDate, toDate, fromDate, toDate}
		}
	}

	rows, err := h.db.Query(query, args...)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	defer rows.Close()

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

	// persist
	tx, err := h.db.Begin()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	defer func() { _ = tx.Rollback() }()

	sharedInt := 0
	if shared {
		sharedInt = 1
	}
	res, err := tx.Exec(
		"INSERT INTO shopping_lists (user_id, meal_plan_id, shared, from_date, to_date) VALUES (?, ?, ?, ?, ?)",
		userID, planID, sharedInt, fromDate, toDate,
	)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	listID, _ := res.LastInsertId()

	sortOrder := 0
	for id, item := range agg {
		_, err := tx.Exec(
			"INSERT INTO shopping_list_items (shopping_list_id, ingredient_id, ingredient_name, total_quantity, unit, category, sort_order) VALUES (?, ?, ?, ?, ?, ?, ?)",
			listID, id, item.name, item.quantity, item.baseUnit, item.category, sortOrder,
		)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		sortOrder++
	}

	if err := tx.Commit(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// return saved list
	saved := model.ShoppingListSaved{
		ID:         listID,
		MealPlanID: planID,
		Shared:     shared,
		FromDate:   fromDate,
		ToDate:     toDate,
		Categories: []model.ShoppingCategory{},
	}

	catItems := map[string][]model.ShoppingItem{}
	for id, item := range agg {
		catItems[item.category] = append(catItems[item.category], model.ShoppingItem{
			IngredientID:   id,
			IngredientName: item.name,
			TotalQuantity:  item.quantity,
			Unit:           item.baseUnit,
		})
	}

	catNames := make([]string, 0, len(catItems))
	for name := range catItems {
		catNames = append(catNames, name)
	}
	sort.Strings(catNames)
	for _, catName := range catNames {
		items := catItems[catName]
		sort.Slice(items, func(i, j int) bool {
			return items[i].IngredientName < items[j].IngredientName
		})
		saved.Categories = append(saved.Categories, model.ShoppingCategory{
			Name:  catName,
			Items: items,
		})
	}

	c.JSON(http.StatusCreated, saved)
}

// GetSaved godoc
// @Summary      Get saved shopping list
// @Description  Returns a persisted shopping list with items and checked status
// @Tags         shopping-list
// @Security     BearerAuth
// @Produce      json
// @Param        id   path      int  true  "Shopping list ID"
// @Success      200  {object}  model.ShoppingListSaved
// @Failure      404  {object}  map[string]string
// @Router       /shopping-lists/{id} [get]
func (h *ShoppingListHandler) GetSaved(c *gin.Context) {
	userID := c.GetInt64("userID")
	listID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid list id"})
		return
	}

	var sl model.ShoppingListSaved
	err = h.db.QueryRow(
		"SELECT id, meal_plan_id, shared, from_date, to_date FROM shopping_lists WHERE id = ? AND user_id = ?",
		listID, userID,
	).Scan(&sl.ID, &sl.MealPlanID, &sl.Shared, &sl.FromDate, &sl.ToDate)
	if err == sql.ErrNoRows {
		c.JSON(http.StatusNotFound, gin.H{"error": "shopping list not found"})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	rows, err := h.db.Query(
		"SELECT id, ingredient_id, ingredient_name, total_quantity, unit, category, checked FROM shopping_list_items WHERE shopping_list_id = ? ORDER BY category, sort_order, ingredient_name",
		listID,
	)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	defer rows.Close()

	catMap := map[string][]model.ShoppingItem{}
	catOrder := []string{}
	for rows.Next() {
		var item model.ShoppingItem
		var checkedInt int
		var category string
		if err := rows.Scan(&item.ID, &item.IngredientID, &item.IngredientName, &item.TotalQuantity, &item.Unit, &category, &checkedInt); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		item.Checked = checkedInt != 0
		if _, exists := catMap[category]; !exists {
			catOrder = append(catOrder, category)
		}
		catMap[category] = append(catMap[category], item)
	}

	sl.Categories = []model.ShoppingCategory{}
	for _, catName := range catOrder {
		sl.Categories = append(sl.Categories, model.ShoppingCategory{
			Name:  catName,
			Items: catMap[catName],
		})
	}

	c.JSON(http.StatusOK, sl)
}

// CheckItem godoc
// @Summary      Toggle shopping list item
// @Description  Sets checked status on a shopping list item
// @Tags         shopping-list
// @Security     BearerAuth
// @Accept       json
// @Produce      json
// @Param        id       path      int   true  "Shopping list ID"
// @Param        item_id  path      int   true  "Item ID"
// @Param        body     body      map[string]bool  true  "Checked status"
// @Success      200      {object}  map[string]interface{}
// @Failure      400      {object}  map[string]string
// @Failure      404      {object}  map[string]string
// @Router       /shopping-lists/{id}/items/{item_id} [patch]
func (h *ShoppingListHandler) CheckItem(c *gin.Context) {
	userID := c.GetInt64("userID")
	listID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid list id"})
		return
	}
	itemID, err := strconv.ParseInt(c.Param("itemId"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid item id"})
		return
	}

	// verify ownership
	var exists int
	h.db.QueryRow("SELECT 1 FROM shopping_lists WHERE id = ? AND user_id = ?", listID, userID).Scan(&exists)
	if exists == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "shopping list not found"})
		return
	}

	var req struct {
		Checked bool `json:"checked"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	checkedInt := 0
	if req.Checked {
		checkedInt = 1
	}
	result, err := h.db.Exec(
		"UPDATE shopping_list_items SET checked = ? WHERE id = ? AND shopping_list_id = ?",
		checkedInt, itemID, listID,
	)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	rowsAffected, _ := result.RowsAffected()
	if rowsAffected == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "item not found"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"id": itemID, "checked": req.Checked})
}
