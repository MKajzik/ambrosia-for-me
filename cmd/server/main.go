// @title           MealPlanner API
// @version         1.0
// @description     REST API for meal planning, ingredients, and shopping lists.
// @host            localhost:8080
// @BasePath        /
// @schemes         http
// @securityDefinitions.apikey BearerAuth
// @in header
// @name Authorization
package main

import (
	"log"

	"github.com/gin-gonic/gin"
	swaggerFiles "github.com/swaggo/files"
	ginSwagger "github.com/swaggo/gin-swagger"
	"github.com/kazik/mealPlanner/internal/config"
	"github.com/kazik/mealPlanner/internal/db"
	"github.com/kazik/mealPlanner/internal/handler"
	_ "github.com/kazik/mealPlanner/docs"
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

	r.GET("/docs/*any", ginSwagger.WrapHandler(swaggerFiles.Handler))

	authHandler := handler.NewAuthHandler(database, cfg.JWTSecret)
	ingredientHandler := handler.NewIngredientHandler(database)
	mealHandler := handler.NewMealHandler(database)
	mealPlanHandler := handler.NewMealPlanHandler(database)
	shoppingListHandler := handler.NewShoppingListHandler(database)
	partnerHandler := handler.NewPartnerHandler(database)

	authLimiter := handler.NewRateLimiter(5, 10)

	auth := r.Group("/auth")
	{
		auth.POST("/register", handler.RateLimitMiddleware(authLimiter), authHandler.Register)
		auth.POST("/login", handler.RateLimitMiddleware(authLimiter), authHandler.Login)
		auth.POST("/refresh", authHandler.Refresh)
		auth.POST("/logout", authHandler.Logout)
		auth.DELETE("/account", handler.AuthMiddleware(cfg.JWTSecret), authHandler.Delete)
	}

	protected := r.Group("", handler.AuthMiddleware(cfg.JWTSecret))
	{
		ingredients := protected.Group("/ingredients")
		{
			ingredients.GET("", ingredientHandler.List)
			ingredients.POST("", ingredientHandler.Create)
			ingredients.GET("/:id", ingredientHandler.Get)
			ingredients.PUT("/:id", ingredientHandler.Update)
			ingredients.DELETE("/:id", ingredientHandler.Delete)
		}

		meals := protected.Group("/meals")
		{
			meals.GET("", mealHandler.List)
			meals.POST("", mealHandler.Create)
			meals.GET("/:id", mealHandler.Get)
			meals.PUT("/:id", mealHandler.Update)
			meals.DELETE("/:id", mealHandler.Delete)
		}

		mealPlans := protected.Group("/meal-plans")
		{
			mealPlans.GET("", mealPlanHandler.List)
			mealPlans.POST("", mealPlanHandler.Create)
			mealPlans.GET("/:id", mealPlanHandler.Get)
			mealPlans.PUT("/:id", mealPlanHandler.Update)
			mealPlans.DELETE("/:id", mealPlanHandler.Delete)
			mealPlans.GET("/:id/shopping-list", shoppingListHandler.Generate)
			mealPlans.POST("/:id/shopping-list/save", shoppingListHandler.Save)
		}

		shoppingLists := protected.Group("/shopping-lists")
		{
			shoppingLists.GET("/:id", shoppingListHandler.GetSaved)
			shoppingLists.PATCH("/:id/items/:itemId", shoppingListHandler.CheckItem)
		}

		partner := protected.Group("/partner")
		{
			partner.POST("/invite", partnerHandler.Invite)
			partner.POST("/accept", partnerHandler.Accept)
			partner.GET("", partnerHandler.Get)
			partner.DELETE("", partnerHandler.Disconnect)
		}
	}

	log.Printf("starting server on :%s", cfg.Port)
	if err := r.Run(":" + cfg.Port); err != nil {
		log.Fatalf("server failed: %v", err)
	}
}
