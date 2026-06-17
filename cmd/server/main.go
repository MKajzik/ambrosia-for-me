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
	defer database.Close()

	r := gin.Default()

	mealHandler := handler.NewMealHandler(database)

	r.GET("/health", func(c *gin.Context) {
		c.JSON(200, gin.H{"status": "ok"})
	})

	meals := r.Group("/meals")
	{
		meals.GET("", mealHandler.List)
		meals.POST("", mealHandler.Create)
		meals.GET("/:id", mealHandler.Get)
		meals.PUT("/:id", mealHandler.Update)
		meals.DELETE("/:id", mealHandler.Delete)
	}

	log.Printf("starting server on :%s", cfg.Port)
	if err := r.Run(":" + cfg.Port); err != nil {
		log.Fatalf("server failed: %v", err)
	}
}
