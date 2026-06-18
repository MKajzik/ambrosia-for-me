package handler

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/kazik/mealPlanner/internal/db"
)

func init() {
	gin.SetMode(gin.TestMode)
}

func setupTestDB(t *testing.T) *sql.DB {
	t.Helper()
	database, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	t.Cleanup(func() { database.Close() })
	return database
}

func setupRouter(database *sql.DB) *gin.Engine {
	r := gin.New()

	ah := NewAuthHandler(database, "test-secret")
	rl := NewRateLimiter(100, 200) // high limits for tests
	auth := r.Group("/auth")
	{
		auth.POST("/register", RateLimitMiddleware(rl), ah.Register)
		auth.POST("/login", RateLimitMiddleware(rl), ah.Login)
		auth.POST("/refresh", ah.Refresh)
		auth.POST("/logout", ah.Logout)
		auth.DELETE("/account", AuthMiddleware("test-secret"), ah.Delete)
	}

	ih := NewIngredientHandler(database)
	mh := NewMealHandler(database)
	mph := NewMealPlanHandler(database)
	sh := NewShoppingListHandler(database)

	protected := r.Group("", AuthMiddleware("test-secret"))
	{
		ingredients := protected.Group("/ingredients")
		{
			ingredients.GET("", ih.List)
			ingredients.POST("", ih.Create)
			ingredients.GET("/:id", ih.Get)
			ingredients.PUT("/:id", ih.Update)
			ingredients.DELETE("/:id", ih.Delete)
		}

		meals := protected.Group("/meals")
		{
			meals.GET("", mh.List)
			meals.POST("", mh.Create)
			meals.GET("/:id", mh.Get)
			meals.PUT("/:id", mh.Update)
			meals.DELETE("/:id", mh.Delete)
		}

		mealPlans := protected.Group("/meal-plans")
		{
			mealPlans.GET("", mph.List)
			mealPlans.POST("", mph.Create)
			mealPlans.GET("/:id", mph.Get)
			mealPlans.PUT("/:id", mph.Update)
			mealPlans.DELETE("/:id", mph.Delete)
			mealPlans.GET("/:id/shopping-list", sh.Generate)
		}
	}

	return r
}

func doJSON(r *gin.Engine, method, path string, body any) *httptest.ResponseRecorder {
	return doJSONAuth(r, method, path, body, "")
}

func doJSONAuth(r *gin.Engine, method, path string, body any, token string) *httptest.ResponseRecorder {
	var buf bytes.Buffer
	if body != nil {
		json.NewEncoder(&buf).Encode(body)
	}
	req := httptest.NewRequest(method, path, &buf)
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func registerAndLogin(t *testing.T, r *gin.Engine, username, password string) string {
	t.Helper()
	w := doJSON(r, http.MethodPost, "/auth/register", map[string]any{
		"username": username,
		"password": password,
	})
	if w.Code != http.StatusCreated {
		t.Fatalf("register %q: status = %d, body = %s", username, w.Code, w.Body.String())
	}
	var resp map[string]any
	json.Unmarshal(w.Body.Bytes(), &resp)
	return resp["access_token"].(string)
}

func createIngredient(t *testing.T, r *gin.Engine, token, name string) int64 {
	t.Helper()
	body := map[string]any{
		"name":               name,
		"base_unit":          "g",
		"category":           "Inne",
		"calories_per_100":   100,
		"protein_per_100":    10,
		"fat_per_100":        5,
		"carbs_per_100":      15,
		"fiber_per_100":      2,
		"salt_per_100":       0.5,
		"sugars_per_100":     3,
		"saturated_fat_per_100": 1,
	}
	w := doJSONAuth(r, http.MethodPost, "/ingredients", body, token)
	if w.Code != http.StatusCreated {
		t.Fatalf("create ingredient %q: status = %d, body = %s", name, w.Code, w.Body.String())
	}
	var resp map[string]any
	json.Unmarshal(w.Body.Bytes(), &resp)
	return int64(resp["id"].(float64))
}

func createMeal(t *testing.T, r *gin.Engine, token, name, mealType string, ingredients []map[string]any) int64 {
	t.Helper()
	body := map[string]any{
		"name":        name,
		"meal_type":   mealType,
		"instructions": "test instructions",
	}
	if ingredients != nil {
		body["ingredients"] = ingredients
	}
	w := doJSONAuth(r, http.MethodPost, "/meals", body, token)
	if w.Code != http.StatusCreated {
		t.Fatalf("create meal %q: status = %d, body = %s", name, w.Code, w.Body.String())
	}
	var resp map[string]any
	json.Unmarshal(w.Body.Bytes(), &resp)
	return int64(resp["id"].(float64))
}

// --- Ingredient Tests ---

func TestIngredientCreate(t *testing.T) {
	database := setupTestDB(t)
	r := setupRouter(database)
	token := registerAndLogin(t, r, "testuser", "testpass123")

	body := map[string]any{
		"name":               "Chicken Breast",
		"base_unit":          "g",
		"category":           "Mięso",
		"calories_per_100":   165,
		"protein_per_100":    31,
		"fat_per_100":        3.6,
		"carbs_per_100":      0,
		"fiber_per_100":      0,
		"salt_per_100":       0.1,
		"sugars_per_100":     0,
		"saturated_fat_per_100": 1,
	}
	w := doJSONAuth(r, http.MethodPost, "/ingredients", body, token)

	if w.Code != http.StatusCreated {
		t.Errorf("status = %d, want %d. body: %s", w.Code, http.StatusCreated, w.Body.String())
	}

	var resp map[string]any
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["name"] != "Chicken Breast" {
		t.Errorf("name = %v, want %v", resp["name"], "Chicken Breast")
	}
	if resp["id"] == nil {
		t.Error("response missing id")
	}
}

func TestIngredientCreateValidation(t *testing.T) {
	database := setupTestDB(t)
	r := setupRouter(database)
	token := registerAndLogin(t, r, "testuser", "testpass123")

	body := map[string]any{"name": "Test"} // missing required fields
	w := doJSONAuth(r, http.MethodPost, "/ingredients", body, token)
	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestIngredientList(t *testing.T) {
	database := setupTestDB(t)
	r := setupRouter(database)
	token := registerAndLogin(t, r, "testuser", "testpass123")

	createIngredient(t, r, token, "Apple")
	createIngredient(t, r, token, "Banana")

	w := doJSONAuth(r, http.MethodGet, "/ingredients", nil, token)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}

	var resp []map[string]any
	json.Unmarshal(w.Body.Bytes(), &resp)
	if len(resp) != 2 {
		t.Errorf("count = %d, want 2", len(resp))
	}
}

func TestIngredientListSearch(t *testing.T) {
	database := setupTestDB(t)
	r := setupRouter(database)
	token := registerAndLogin(t, r, "testuser", "testpass123")

	createIngredient(t, r, token, "Apple")
	createIngredient(t, r, token, "Banana")

	w := doJSONAuth(r, http.MethodGet, "/ingredients?search=App", nil, token)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}

	var resp []map[string]any
	json.Unmarshal(w.Body.Bytes(), &resp)
	if len(resp) != 1 {
		t.Errorf("count = %d, want 1", len(resp))
	}
}

func TestIngredientGet(t *testing.T) {
	database := setupTestDB(t)
	r := setupRouter(database)
	token := registerAndLogin(t, r, "testuser", "testpass123")

	id := createIngredient(t, r, token, "Rice")

	w := doJSONAuth(r, http.MethodGet, "/ingredients/1", nil, token) // first insert gets id=1
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d. body: %s", w.Code, http.StatusOK, w.Body.String())
	}
	_ = id

	var resp map[string]any
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["name"] != "Rice" {
		t.Errorf("name = %v, want Rice", resp["name"])
	}
}

func TestIngredientGetNotFound(t *testing.T) {
	database := setupTestDB(t)
	r := setupRouter(database)
	token := registerAndLogin(t, r, "testuser", "testpass123")

	w := doJSONAuth(r, http.MethodGet, "/ingredients/999", nil, token)
	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", w.Code, http.StatusNotFound)
	}
}

func TestIngredientGetInvalidID(t *testing.T) {
	database := setupTestDB(t)
	r := setupRouter(database)
	token := registerAndLogin(t, r, "testuser", "testpass123")

	w := doJSONAuth(r, http.MethodGet, "/ingredients/abc", nil, token)
	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestIngredientUpdate(t *testing.T) {
	database := setupTestDB(t)
	r := setupRouter(database)
	token := registerAndLogin(t, r, "testuser", "testpass123")

	createIngredient(t, r, token, "Rice")

	body := map[string]any{
		"name":               "Brown Rice",
		"base_unit":          "g",
		"category":           "Produkty zbożowe",
		"calories_per_100":   123,
		"protein_per_100":    2.7,
		"fat_per_100":        1,
		"carbs_per_100":      26,
		"fiber_per_100":      1.8,
		"salt_per_100":       0,
		"sugars_per_100":     0.4,
		"saturated_fat_per_100": 0.3,
	}
	w := doJSONAuth(r, http.MethodPut, "/ingredients/1", body, token)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body: %s", w.Code, w.Body.String())
	}

	var resp map[string]any
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["name"] != "Brown Rice" {
		t.Errorf("name = %v, want Brown Rice", resp["name"])
	}
}

func TestIngredientUpdateNotFound(t *testing.T) {
	database := setupTestDB(t)
	r := setupRouter(database)
	token := registerAndLogin(t, r, "testuser", "testpass123")

	body := map[string]any{
		"name":               "X",
		"base_unit":          "g",
		"category":           "Inne",
		"calories_per_100":   100,
	}
	w := doJSONAuth(r, http.MethodPut, "/ingredients/999", body, token)
	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", w.Code, http.StatusNotFound)
	}
}

func TestIngredientDelete(t *testing.T) {
	database := setupTestDB(t)
	r := setupRouter(database)
	token := registerAndLogin(t, r, "testuser", "testpass123")

	createIngredient(t, r, token, "Rice")

	w := doJSONAuth(r, http.MethodDelete, "/ingredients/1", nil, token)
	if w.Code != http.StatusNoContent {
		t.Errorf("status = %d, want %d", w.Code, http.StatusNoContent)
	}

	w = doJSONAuth(r, http.MethodGet, "/ingredients/1", nil, token)
	if w.Code != http.StatusNotFound {
		t.Errorf("after delete: status = %d, want %d", w.Code, http.StatusNotFound)
	}
}

func TestIngredientDeleteNotFound(t *testing.T) {
	database := setupTestDB(t)
	r := setupRouter(database)
	token := registerAndLogin(t, r, "testuser", "testpass123")

	w := doJSONAuth(r, http.MethodDelete, "/ingredients/999", nil, token)
	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", w.Code, http.StatusNotFound)
	}
}

// --- Meal Tests ---

func TestMealCreate(t *testing.T) {
	database := setupTestDB(t)
	r := setupRouter(database)
	token := registerAndLogin(t, r, "testuser", "testpass123")

	body := map[string]any{
		"name":         "Oatmeal",
		"meal_type":    "breakfast",
		"instructions": "Cook oats with milk",
	}
	w := doJSONAuth(r, http.MethodPost, "/meals", body, token)
	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, body: %s", w.Code, w.Body.String())
	}

	var resp map[string]any
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["name"] != "Oatmeal" {
		t.Errorf("name = %v, want Oatmeal", resp["name"])
	}
	ingredients := resp["ingredients"].([]any)
	if len(ingredients) != 0 {
		t.Errorf("ingredients count = %d, want 0", len(ingredients))
	}
}

func TestMealCreateWithIngredients(t *testing.T) {
	database := setupTestDB(t)
	r := setupRouter(database)
	token := registerAndLogin(t, r, "testuser", "testpass123")

	ingID := createIngredient(t, r, token, "Oats")

	body := map[string]any{
		"name":         "Oatmeal",
		"meal_type":    "breakfast",
		"instructions": "Cook oats",
		"ingredients": []map[string]any{
			{"ingredient_id": ingID, "quantity": 100, "unit": "g"},
		},
	}
	w := doJSONAuth(r, http.MethodPost, "/meals", body, token)
	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, body: %s", w.Code, w.Body.String())
	}

	var resp map[string]any
	json.Unmarshal(w.Body.Bytes(), &resp)
	ingredients := resp["ingredients"].([]any)
	if len(ingredients) != 1 {
		t.Fatalf("ingredients count = %d, want 1", len(ingredients))
	}
	ing := ingredients[0].(map[string]any)
	if ing["ingredient_id"].(float64) != float64(ingID) {
		t.Errorf("ingredient_id = %v, want %v", ing["ingredient_id"], ingID)
	}
}

func TestMealCreateValidation(t *testing.T) {
	database := setupTestDB(t)
	r := setupRouter(database)
	token := registerAndLogin(t, r, "testuser", "testpass123")

	body := map[string]any{"name": "Test"} // missing meal_type
	w := doJSONAuth(r, http.MethodPost, "/meals", body, token)
	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestMealList(t *testing.T) {
	database := setupTestDB(t)
	r := setupRouter(database)
	token := registerAndLogin(t, r, "testuser", "testpass123")

	createMeal(t, r, token, "Oatmeal", "breakfast", nil)
	createMeal(t, r, token, "Salad", "lunch", nil)

	w := doJSONAuth(r, http.MethodGet, "/meals", nil, token)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}

	var resp []map[string]any
	json.Unmarshal(w.Body.Bytes(), &resp)
	if len(resp) != 2 {
		t.Errorf("count = %d, want 2", len(resp))
	}
}

func TestMealListFilter(t *testing.T) {
	database := setupTestDB(t)
	r := setupRouter(database)
	token := registerAndLogin(t, r, "testuser", "testpass123")

	createMeal(t, r, token, "Oatmeal", "breakfast", nil)
	createMeal(t, r, token, "Salad", "lunch", nil)

	w := doJSONAuth(r, http.MethodGet, "/meals?meal_type=breakfast", nil, token)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}

	var resp []map[string]any
	json.Unmarshal(w.Body.Bytes(), &resp)
	if len(resp) != 1 {
		t.Errorf("count = %d, want 1", len(resp))
	}
}

func TestMealGet(t *testing.T) {
	database := setupTestDB(t)
	r := setupRouter(database)
	token := registerAndLogin(t, r, "testuser", "testpass123")

	ingID := createIngredient(t, r, token, "Oats")
	mealID := createMeal(t, r, token, "Oatmeal", "breakfast", []map[string]any{
		{"ingredient_id": ingID, "quantity": 80, "unit": "g"},
	})

	w := doJSONAuth(r, http.MethodGet, "/meals/1", nil, token)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body: %s", w.Code, w.Body.String())
	}

	var resp map[string]any
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["name"] != "Oatmeal" {
		t.Errorf("name = %v, want Oatmeal", resp["name"])
	}
	ingredients := resp["ingredients"].([]any)
	if len(ingredients) != 1 {
		t.Errorf("ingredients count = %d, want 1", len(ingredients))
	}
	_ = mealID
}

func TestMealGetNotFound(t *testing.T) {
	database := setupTestDB(t)
	r := setupRouter(database)
	token := registerAndLogin(t, r, "testuser", "testpass123")

	w := doJSONAuth(r, http.MethodGet, "/meals/999", nil, token)
	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", w.Code, http.StatusNotFound)
	}
}

func TestMealUpdate(t *testing.T) {
	database := setupTestDB(t)
	r := setupRouter(database)
	token := registerAndLogin(t, r, "testuser", "testpass123")

	ingID := createIngredient(t, r, token, "Oats")
	createMeal(t, r, token, "Oatmeal", "breakfast", []map[string]any{
		{"ingredient_id": ingID, "quantity": 80, "unit": "g"},
	})

	body := map[string]any{
		"name":         "Updated Oatmeal",
		"meal_type":    "breakfast",
		"instructions": "New instructions",
		"ingredients": []map[string]any{
			{"ingredient_id": ingID, "quantity": 100, "unit": "g"},
		},
	}
	w := doJSONAuth(r, http.MethodPut, "/meals/1", body, token)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body: %s", w.Code, w.Body.String())
	}

	var resp map[string]any
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["name"] != "Updated Oatmeal" {
		t.Errorf("name = %v, want Updated Oatmeal", resp["name"])
	}
}

func TestMealUpdateNotFound(t *testing.T) {
	database := setupTestDB(t)
	r := setupRouter(database)
	token := registerAndLogin(t, r, "testuser", "testpass123")

	body := map[string]any{
		"name":      "X",
		"meal_type": "breakfast",
	}
	w := doJSONAuth(r, http.MethodPut, "/meals/999", body, token)
	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", w.Code, http.StatusNotFound)
	}
}

func TestMealDelete(t *testing.T) {
	database := setupTestDB(t)
	r := setupRouter(database)
	token := registerAndLogin(t, r, "testuser", "testpass123")

	ingID := createIngredient(t, r, token, "Oats")
	createMeal(t, r, token, "Oatmeal", "breakfast", []map[string]any{
		{"ingredient_id": ingID, "quantity": 80, "unit": "g"},
	})

	w := doJSONAuth(r, http.MethodDelete, "/meals/1", nil, token)
	if w.Code != http.StatusNoContent {
		t.Errorf("status = %d, want %d", w.Code, http.StatusNoContent)
	}

	// meal_ingredients should be cascade deleted
	var count int
	database.QueryRow("SELECT COUNT(*) FROM meal_ingredients WHERE meal_id = 1").Scan(&count)
	if count != 0 {
		t.Errorf("meal_ingredients count = %d after delete, want 0", count)
	}
}

func TestMealDeleteNotFound(t *testing.T) {
	database := setupTestDB(t)
	r := setupRouter(database)
	token := registerAndLogin(t, r, "testuser", "testpass123")

	w := doJSONAuth(r, http.MethodDelete, "/meals/999", nil, token)
	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", w.Code, http.StatusNotFound)
	}
}

// --- MealPlan Tests ---

func TestMealPlanCreate(t *testing.T) {
	database := setupTestDB(t)
	r := setupRouter(database)
	token := registerAndLogin(t, r, "testuser", "testpass123")

	body := map[string]any{
		"name":       "Week 1",
		"start_date": "2025-01-06",
		"end_date":   "2025-01-12",
	}
	w := doJSONAuth(r, http.MethodPost, "/meal-plans", body, token)
	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, body: %s", w.Code, w.Body.String())
	}

	var resp map[string]any
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["name"] != "Week 1" {
		t.Errorf("name = %v, want Week 1", resp["name"])
	}
}

func TestMealPlanCreateWithEntries(t *testing.T) {
	database := setupTestDB(t)
	r := setupRouter(database)
	token := registerAndLogin(t, r, "testuser", "testpass123")

	mealID := createMeal(t, r, token, "Oatmeal", "breakfast", nil)

	body := map[string]any{
		"name":       "Week 1",
		"start_date": "2025-01-06",
		"end_date":   "2025-01-12",
		"entries": []map[string]any{
			{"date": "2025-01-06", "meal_type": "breakfast", "meal_id": mealID},
		},
	}
	w := doJSONAuth(r, http.MethodPost, "/meal-plans", body, token)
	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, body: %s", w.Code, w.Body.String())
	}
}

func TestMealPlanCreateInvalidDates(t *testing.T) {
	database := setupTestDB(t)
	r := setupRouter(database)
	token := registerAndLogin(t, r, "testuser", "testpass123")

	body := map[string]any{
		"name":       "Bad Plan",
		"start_date": "2025-01-12",
		"end_date":   "2025-01-06",
	}
	w := doJSONAuth(r, http.MethodPost, "/meal-plans", body, token)
	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestMealPlanCreateEntryOutOfRange(t *testing.T) {
	database := setupTestDB(t)
	r := setupRouter(database)
	token := registerAndLogin(t, r, "testuser", "testpass123")

	mealID := createMeal(t, r, token, "Oatmeal", "breakfast", nil)

	body := map[string]any{
		"name":       "Week 1",
		"start_date": "2025-01-06",
		"end_date":   "2025-01-12",
		"entries": []map[string]any{
			{"date": "2025-01-15", "meal_type": "breakfast", "meal_id": mealID},
		},
	}
	w := doJSONAuth(r, http.MethodPost, "/meal-plans", body, token)
	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestMealPlanList(t *testing.T) {
	database := setupTestDB(t)
	r := setupRouter(database)
	token := registerAndLogin(t, r, "testuser", "testpass123")

	doJSONAuth(r, http.MethodPost, "/meal-plans", map[string]any{
		"name": "Plan A", "start_date": "2025-01-01", "end_date": "2025-01-07",
	}, token)
	doJSONAuth(r, http.MethodPost, "/meal-plans", map[string]any{
		"name": "Plan B", "start_date": "2025-02-01", "end_date": "2025-02-07",
	}, token)

	w := doJSONAuth(r, http.MethodGet, "/meal-plans", nil, token)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}

	var resp []map[string]any
	json.Unmarshal(w.Body.Bytes(), &resp)
	if len(resp) != 2 {
		t.Errorf("count = %d, want 2", len(resp))
	}
}

func TestMealPlanGet(t *testing.T) {
	database := setupTestDB(t)
	r := setupRouter(database)
	token := registerAndLogin(t, r, "testuser", "testpass123")

	mealID := createMeal(t, r, token, "Oatmeal", "breakfast", nil)

	doJSONAuth(r, http.MethodPost, "/meal-plans", map[string]any{
		"name": "Week 1", "start_date": "2025-01-06", "end_date": "2025-01-07",
		"entries": []map[string]any{
			{"date": "2025-01-06", "meal_type": "breakfast", "meal_id": mealID},
		},
	}, token)

	w := doJSONAuth(r, http.MethodGet, "/meal-plans/1", nil, token)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body: %s", w.Code, w.Body.String())
	}

	var resp map[string]any
	json.Unmarshal(w.Body.Bytes(), &resp)
	entries := resp["entries"].([]any)
	if len(entries) != 1 {
		t.Errorf("entries count = %d, want 1", len(entries))
	}
	entry := entries[0].(map[string]any)
	if entry["meal_name"] != "Oatmeal" {
		t.Errorf("meal_name = %v, want Oatmeal", entry["meal_name"])
	}
}

func TestMealPlanGetNotFound(t *testing.T) {
	database := setupTestDB(t)
	r := setupRouter(database)
	token := registerAndLogin(t, r, "testuser", "testpass123")

	w := doJSONAuth(r, http.MethodGet, "/meal-plans/999", nil, token)
	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", w.Code, http.StatusNotFound)
	}
}

func TestMealPlanUpdate(t *testing.T) {
	database := setupTestDB(t)
	r := setupRouter(database)
	token := registerAndLogin(t, r, "testuser", "testpass123")

	mealID := createMeal(t, r, token, "Oatmeal", "breakfast", nil)

	doJSONAuth(r, http.MethodPost, "/meal-plans", map[string]any{
		"name": "Week 1", "start_date": "2025-01-06", "end_date": "2025-01-07",
		"entries": []map[string]any{
			{"date": "2025-01-06", "meal_type": "breakfast", "meal_id": mealID},
		},
	}, token)

	body := map[string]any{
		"name":       "Updated Week 1",
		"start_date": "2025-01-06",
		"end_date":   "2025-01-08",
		"entries": []map[string]any{
			{"date": "2025-01-06", "meal_type": "breakfast", "meal_id": mealID},
			{"date": "2025-01-07", "meal_type": "lunch", "meal_id": mealID},
		},
	}
	w := doJSONAuth(r, http.MethodPut, "/meal-plans/1", body, token)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body: %s", w.Code, w.Body.String())
	}

	var resp map[string]any
	json.Unmarshal(w.Body.Bytes(), &resp)
	entries := resp["entries"].([]any)
	if len(entries) != 2 {
		t.Errorf("entries count = %d, want 2", len(entries))
	}
}

func TestMealPlanUpdateNotFound(t *testing.T) {
	database := setupTestDB(t)
	r := setupRouter(database)
	token := registerAndLogin(t, r, "testuser", "testpass123")

	body := map[string]any{
		"name": "X", "start_date": "2025-01-01", "end_date": "2025-01-02",
	}
	w := doJSONAuth(r, http.MethodPut, "/meal-plans/999", body, token)
	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", w.Code, http.StatusNotFound)
	}
}

func TestMealPlanDelete(t *testing.T) {
	database := setupTestDB(t)
	r := setupRouter(database)
	token := registerAndLogin(t, r, "testuser", "testpass123")

	mealID := createMeal(t, r, token, "Oatmeal", "breakfast", nil)

	doJSONAuth(r, http.MethodPost, "/meal-plans", map[string]any{
		"name": "Week 1", "start_date": "2025-01-06", "end_date": "2025-01-07",
		"entries": []map[string]any{
			{"date": "2025-01-06", "meal_type": "breakfast", "meal_id": mealID},
		},
	}, token)

	w := doJSONAuth(r, http.MethodDelete, "/meal-plans/1", nil, token)
	if w.Code != http.StatusNoContent {
		t.Errorf("status = %d, want %d", w.Code, http.StatusNoContent)
	}

	var count int
	database.QueryRow("SELECT COUNT(*) FROM meal_plan_entries WHERE meal_plan_id = 1").Scan(&count)
	if count != 0 {
		t.Errorf("entries count = %d after delete, want 0", count)
	}
}

func TestMealPlanDeleteNotFound(t *testing.T) {
	database := setupTestDB(t)
	r := setupRouter(database)
	token := registerAndLogin(t, r, "testuser", "testpass123")

	w := doJSONAuth(r, http.MethodDelete, "/meal-plans/999", nil, token)
	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", w.Code, http.StatusNotFound)
	}
}

// --- ShoppingList Tests ---

func TestShoppingListGenerate(t *testing.T) {
	database := setupTestDB(t)
	r := setupRouter(database)
	token := registerAndLogin(t, r, "testuser", "testpass123")

	ingID := createIngredient(t, r, token, "Oats")
	mealID := createMeal(t, r, token, "Oatmeal", "breakfast", []map[string]any{
		{"ingredient_id": ingID, "quantity": 80, "unit": "g"},
	})

	doJSONAuth(r, http.MethodPost, "/meal-plans", map[string]any{
		"name": "Week 1", "start_date": "2025-01-06", "end_date": "2025-01-07",
		"entries": []map[string]any{
			{"date": "2025-01-06", "meal_type": "breakfast", "meal_id": mealID},
			{"date": "2025-01-07", "meal_type": "breakfast", "meal_id": mealID},
		},
	}, token)

	w := doJSONAuth(r, http.MethodGet, "/meal-plans/1/shopping-list", nil, token)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body: %s", w.Code, w.Body.String())
	}

	var resp map[string]any
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["meal_plan_id"].(float64) != 1 {
		t.Errorf("meal_plan_id = %v, want 1", resp["meal_plan_id"])
	}

	categories := resp["categories"].([]any)
	if len(categories) == 0 {
		t.Fatal("categories is empty")
	}

	cat := categories[0].(map[string]any)
	items := cat["items"].([]any)
	if len(items) != 1 {
		t.Fatalf("items count = %d, want 1", len(items))
	}

	item := items[0].(map[string]any)
	if item["total_quantity"].(float64) != 160 {
		t.Errorf("total_quantity = %v, want 160", item["total_quantity"])
	}
}

func TestShoppingListGenerateWithDateRange(t *testing.T) {
	database := setupTestDB(t)
	r := setupRouter(database)
	token := registerAndLogin(t, r, "testuser", "testpass123")

	ingID := createIngredient(t, r, token, "Oats")
	mealID := createMeal(t, r, token, "Oatmeal", "breakfast", []map[string]any{
		{"ingredient_id": ingID, "quantity": 80, "unit": "g"},
	})

	doJSONAuth(r, http.MethodPost, "/meal-plans", map[string]any{
		"name": "Week 1", "start_date": "2025-01-06", "end_date": "2025-01-08",
		"entries": []map[string]any{
			{"date": "2025-01-06", "meal_type": "breakfast", "meal_id": mealID},
			{"date": "2025-01-07", "meal_type": "breakfast", "meal_id": mealID},
			{"date": "2025-01-08", "meal_type": "breakfast", "meal_id": mealID},
		},
	}, token)

	w := doJSONAuth(r, http.MethodGet, "/meal-plans/1/shopping-list?from_date=2025-01-06&to_date=2025-01-07", nil, token)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body: %s", w.Code, w.Body.String())
	}

	var resp map[string]any
	json.Unmarshal(w.Body.Bytes(), &resp)

	categories := resp["categories"].([]any)
	cat := categories[0].(map[string]any)
	items := cat["items"].([]any)
	item := items[0].(map[string]any)
	if item["total_quantity"].(float64) != 160 {
		t.Errorf("total_quantity = %v, want 160 (2 days, not 3)", item["total_quantity"])
	}
}

func TestShoppingListGenerateNotFound(t *testing.T) {
	database := setupTestDB(t)
	r := setupRouter(database)
	token := registerAndLogin(t, r, "testuser", "testpass123")

	w := doJSONAuth(r, http.MethodGet, "/meal-plans/999/shopping-list", nil, token)
	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", w.Code, http.StatusNotFound)
	}
}

func TestShoppingListUnitConversion(t *testing.T) {
	database := setupTestDB(t)
	r := setupRouter(database)
	token := registerAndLogin(t, r, "testuser", "testpass123")

	ingID := createIngredient(t, r, token, "Milk")
	mealID := createMeal(t, r, token, "Cereal", "breakfast", []map[string]any{
		{"ingredient_id": ingID, "quantity": 0.5, "unit": "l"},
	})

	doJSONAuth(r, http.MethodPost, "/meal-plans", map[string]any{
		"name": "Week 1", "start_date": "2025-01-06", "end_date": "2025-01-06",
		"entries": []map[string]any{
			{"date": "2025-01-06", "meal_type": "breakfast", "meal_id": mealID},
		},
	}, token)

	w := doJSONAuth(r, http.MethodGet, "/meal-plans/1/shopping-list", nil, token)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body: %s", w.Code, w.Body.String())
	}

	var resp map[string]any
	json.Unmarshal(w.Body.Bytes(), &resp)
	categories := resp["categories"].([]any)
	cat := categories[0].(map[string]any)
	items := cat["items"].([]any)
	item := items[0].(map[string]any)
	if item["total_quantity"].(float64) != 500 {
		t.Errorf("total_quantity = %v, want 500 (0.5L -> 500ml)", item["total_quantity"])
	}
}

// --- Auth Tests ---

func TestRegister(t *testing.T) {
	database := setupTestDB(t)
	r := setupRouter(database)

	w := doJSON(r, http.MethodPost, "/auth/register", map[string]any{
		"username": "testuser",
		"password": "password123",
	})
	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, body: %s", w.Code, w.Body.String())
	}

	var resp map[string]any
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["access_token"] == nil {
		t.Error("response missing access_token")
	}
	if resp["refresh_token"] == nil {
		t.Error("response missing refresh_token")
	}
	user := resp["user"].(map[string]any)
	if user["username"] != "testuser" {
		t.Errorf("username = %v, want testuser", user["username"])
	}
}

func TestRegisterDuplicate(t *testing.T) {
	database := setupTestDB(t)
	r := setupRouter(database)

	doJSON(r, http.MethodPost, "/auth/register", map[string]any{
		"username": "testuser", "password": "password123",
	})
	w := doJSON(r, http.MethodPost, "/auth/register", map[string]any{
		"username": "testuser", "password": "password456",
	})
	if w.Code != http.StatusConflict {
		t.Errorf("status = %d, want %d", w.Code, http.StatusConflict)
	}
}

func TestRegisterValidation(t *testing.T) {
	database := setupTestDB(t)
	r := setupRouter(database)

	w := doJSON(r, http.MethodPost, "/auth/register", map[string]any{
		"username": "ab", "password": "short",
	})
	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestLogin(t *testing.T) {
	database := setupTestDB(t)
	r := setupRouter(database)

	doJSON(r, http.MethodPost, "/auth/register", map[string]any{
		"username": "testuser", "password": "password123",
	})

	w := doJSON(r, http.MethodPost, "/auth/login", map[string]any{
		"username": "testuser", "password": "password123",
	})
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body: %s", w.Code, w.Body.String())
	}

	var resp map[string]any
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["access_token"] == nil {
		t.Error("response missing access_token")
	}
	if resp["refresh_token"] == nil {
		t.Error("response missing refresh_token")
	}
}

func TestLoginWrongPassword(t *testing.T) {
	database := setupTestDB(t)
	r := setupRouter(database)

	doJSON(r, http.MethodPost, "/auth/register", map[string]any{
		"username": "testuser", "password": "password123",
	})

	w := doJSON(r, http.MethodPost, "/auth/login", map[string]any{
		"username": "testuser", "password": "wrongpassword",
	})
	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", w.Code, http.StatusUnauthorized)
	}
}

func TestLoginNonexistentUser(t *testing.T) {
	database := setupTestDB(t)
	r := setupRouter(database)

	w := doJSON(r, http.MethodPost, "/auth/login", map[string]any{
		"username": "nobody", "password": "password123",
	})
	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", w.Code, http.StatusUnauthorized)
	}
}

func TestRefreshToken(t *testing.T) {
	database := setupTestDB(t)
	r := setupRouter(database)

	w := doJSON(r, http.MethodPost, "/auth/register", map[string]any{
		"username": "testuser", "password": "password123",
	})
	var regResp map[string]any
	json.Unmarshal(w.Body.Bytes(), &regResp)
	refreshToken := regResp["refresh_token"].(string)

	w = doJSON(r, http.MethodPost, "/auth/refresh", map[string]any{
		"refresh_token": refreshToken,
	})
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body: %s", w.Code, w.Body.String())
	}

	var resp map[string]any
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["access_token"] == nil {
		t.Error("response missing access_token")
	}
	if resp["refresh_token"] == nil {
		t.Error("response missing refresh_token")
	}
	// old refresh token should be rotated (invalidated)
	w = doJSON(r, http.MethodPost, "/auth/refresh", map[string]any{
		"refresh_token": refreshToken,
	})
	if w.Code != http.StatusUnauthorized {
		t.Errorf("rotated token: status = %d, want %d", w.Code, http.StatusUnauthorized)
	}
}

func TestRefreshTokenInvalid(t *testing.T) {
	database := setupTestDB(t)
	r := setupRouter(database)

	w := doJSON(r, http.MethodPost, "/auth/refresh", map[string]any{
		"refresh_token": "invalidtoken",
	})
	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", w.Code, http.StatusUnauthorized)
	}
}

func TestLogout(t *testing.T) {
	database := setupTestDB(t)
	r := setupRouter(database)

	w := doJSON(r, http.MethodPost, "/auth/register", map[string]any{
		"username": "testuser", "password": "password123",
	})
	var resp map[string]any
	json.Unmarshal(w.Body.Bytes(), &resp)
	refreshToken := resp["refresh_token"].(string)

	w = doJSON(r, http.MethodPost, "/auth/logout", map[string]any{
		"refresh_token": refreshToken,
	})
	if w.Code != http.StatusNoContent {
		t.Errorf("status = %d, want %d", w.Code, http.StatusNoContent)
	}

	w = doJSON(r, http.MethodPost, "/auth/refresh", map[string]any{
		"refresh_token": refreshToken,
	})
	if w.Code != http.StatusUnauthorized {
		t.Errorf("after logout: status = %d, want %d", w.Code, http.StatusUnauthorized)
	}
}

func TestDeleteAccount(t *testing.T) {
	database := setupTestDB(t)
	r := setupRouter(database)

	token := registerAndLogin(t, r, "testuser", "password123")

	w := doJSONAuth(r, http.MethodDelete, "/auth/account", nil, token)
	if w.Code != http.StatusNoContent {
		t.Errorf("status = %d, want %d", w.Code, http.StatusNoContent)
	}

	w = doJSON(r, http.MethodPost, "/auth/login", map[string]any{
		"username": "testuser", "password": "password123",
	})
	if w.Code != http.StatusUnauthorized {
		t.Errorf("after delete: login status = %d, want %d", w.Code, http.StatusUnauthorized)
	}
}

func TestDeleteAccountNoAuth(t *testing.T) {
	database := setupTestDB(t)
	r := setupRouter(database)

	w := doJSON(r, http.MethodDelete, "/auth/account", nil)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", w.Code, http.StatusUnauthorized)
	}
}

func TestProtectedRouteNoAuth(t *testing.T) {
	database := setupTestDB(t)
	r := setupRouter(database)

	w := doJSON(r, http.MethodGet, "/ingredients", nil)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", w.Code, http.StatusUnauthorized)
	}
}

// --- Cross-User Ownership Tests ---

func TestCrossUserIngredientIsolation(t *testing.T) {
	database := setupTestDB(t)
	r := setupRouter(database)

	token1 := registerAndLogin(t, r, "user1", "password123")
	token2 := registerAndLogin(t, r, "user2", "password456")

	id := createIngredient(t, r, token1, "User1 Ingredient")

	w := doJSONAuth(r, http.MethodGet, "/ingredients/"+itoa(id), nil, token2)
	if w.Code != http.StatusNotFound {
		t.Errorf("cross-user get: status = %d, want %d", w.Code, http.StatusNotFound)
	}

	w = doJSONAuth(r, http.MethodGet, "/ingredients", nil, token2)
	var resp []map[string]any
	json.Unmarshal(w.Body.Bytes(), &resp)
	if len(resp) != 0 {
		t.Errorf("cross-user list: count = %d, want 0", len(resp))
	}
}

func TestCrossUserMealIsolation(t *testing.T) {
	database := setupTestDB(t)
	r := setupRouter(database)

	token1 := registerAndLogin(t, r, "user1", "password123")
	token2 := registerAndLogin(t, r, "user2", "password456")

	id := createMeal(t, r, token1, "User1 Meal", "breakfast", nil)

	w := doJSONAuth(r, http.MethodGet, "/meals/"+itoa(id), nil, token2)
	if w.Code != http.StatusNotFound {
		t.Errorf("cross-user get: status = %d, want %d", w.Code, http.StatusNotFound)
	}
}

func TestCrossUserMealPlanIsolation(t *testing.T) {
	database := setupTestDB(t)
	r := setupRouter(database)

	token1 := registerAndLogin(t, r, "user1", "password123")
	token2 := registerAndLogin(t, r, "user2", "password456")

	doJSONAuth(r, http.MethodPost, "/meal-plans", map[string]any{
		"name": "Plan", "start_date": "2025-01-01", "end_date": "2025-01-07",
	}, token1)

	w := doJSONAuth(r, http.MethodGet, "/meal-plans/1", nil, token2)
	if w.Code != http.StatusNotFound {
		t.Errorf("cross-user get: status = %d, want %d", w.Code, http.StatusNotFound)
	}

	w = doJSONAuth(r, http.MethodGet, "/meal-plans", nil, token2)
	var resp []map[string]any
	json.Unmarshal(w.Body.Bytes(), &resp)
	if len(resp) != 0 {
		t.Errorf("cross-user list: count = %d, want 0", len(resp))
	}
}

func itoa(id int64) string {
	return strconv.FormatInt(id, 10)
}

// --- normalizeToBase Tests ---

func TestNormalizeToBase(t *testing.T) {
	pkgSize := 500.0

	tests := []struct {
		name     string
		qty      float64
		unit     string
		baseUnit string
		pkgSize  *float64
		want     float64
	}{
		{"g", 100, "g", "g", nil, 100},
		{"kg", 1.5, "kg", "g", nil, 1500},
		{"ml", 200, "ml", "ml", nil, 200},
		{"l", 0.5, "l", "ml", nil, 500},
		{"pcs", 3, "pcs", "pcs", nil, 3},
		{"pkg with size", 2, "pkg", "g", &pkgSize, 1000},
		{"pkg no size", 2, "pkg", "pcs", nil, 2},
		{"unknown", 42, "unknown", "g", nil, 42},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := normalizeToBase(tt.qty, tt.unit, tt.baseUnit, tt.pkgSize)
			if got != tt.want {
				t.Errorf("normalizeToBase(%v, %q, %q, %v) = %v, want %v",
					tt.qty, tt.unit, tt.baseUnit, tt.pkgSize, got, tt.want)
			}
		})
	}
}

// --- Health Test ---

func TestHealthEndpoint(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/health", func(c *gin.Context) {
		c.JSON(200, gin.H{"status": "ok"})
	})

	w := doJSON(r, http.MethodGet, "/health", nil)
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}
	var resp map[string]any
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["status"] != "ok" {
		t.Errorf("status = %v, want ok", resp["status"])
	}
}
