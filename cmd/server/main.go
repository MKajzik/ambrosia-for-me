package main

import (
	"log"

	"github.com/gin-gonic/gin"
	"github.com/kazik/mealPlanner/internal/config"
	"github.com/kazik/mealPlanner/internal/db"
	"github.com/kazik/mealPlanner/internal/handler"
)

func main() {
	cfg := config.Load()

	database, err := db.Open(cfg.DBPath)
	if err != nil {
		log.Fatalf("failed to open database: %v", err)
	}
	defer func() { _ = database.Close() }()

	r := gin.Default()

	r.GET("/health", func(c *gin.Context) {
		c.JSON(200, gin.H{"status": "ok"})
	})

	ingredientHandler := handler.NewIngredientHandler(database)
	mealHandler := handler.NewMealHandler(database)
	mealPlanHandler := handler.NewMealPlanHandler(database)
	shoppingListHandler := handler.NewShoppingListHandler(database)

	ingredients := r.Group("/ingredients")
	{
		ingredients.GET("", ingredientHandler.List)
		ingredients.POST("", ingredientHandler.Create)
		ingredients.GET("/:id", ingredientHandler.Get)
		ingredients.PUT("/:id", ingredientHandler.Update)
		ingredients.DELETE("/:id", ingredientHandler.Delete)
	}

	meals := r.Group("/meals")
	{
		meals.GET("", mealHandler.List)
		meals.POST("", mealHandler.Create)
		meals.GET("/:id", mealHandler.Get)
		meals.PUT("/:id", mealHandler.Update)
		meals.DELETE("/:id", mealHandler.Delete)
	}

	mealPlans := r.Group("/meal-plans")
	{
		mealPlans.GET("", mealPlanHandler.List)
		mealPlans.POST("", mealPlanHandler.Create)
		mealPlans.GET("/:id", mealPlanHandler.Get)
		mealPlans.PUT("/:id", mealPlanHandler.Update)
		mealPlans.DELETE("/:id", mealPlanHandler.Delete)
		mealPlans.GET("/:id/shopping-list", shoppingListHandler.Generate)
	}

	log.Printf("starting server on :%s", cfg.Port)
	if err := r.Run(":" + cfg.Port); err != nil {
		log.Fatalf("server failed: %v", err)
	}
}
