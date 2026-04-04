package main

import (
	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	"github.com/timeplus/agentshield/internal/handlers"
)

func main() {
	r := gin.Default()
	r.Use(cors.New(cors.Config{
		AllowOrigins:     []string{"http://localhost:5173", "http://localhost:3000"},
		AllowMethods:     []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowHeaders:     []string{"Origin", "Content-Type", "Authorization"},
		AllowCredentials: true,
	}))

	api := r.Group("/api")
	{
		api.GET("/dashboard", handlers.GetDashboard)
		api.GET("/agents", handlers.ListAgents)
		api.GET("/agents/:id", handlers.GetAgent)
		api.GET("/threats", handlers.ListThreats)
		api.GET("/cost", handlers.GetCost)
		api.GET("/rules", handlers.ListRules)
		api.POST("/rules/:id/toggle", handlers.ToggleRule)
		api.GET("/investigate/tables", handlers.ListTables)
		api.POST("/investigate/query", handlers.RunQuery)
		api.GET("/settings", handlers.GetSettings)
		api.PUT("/settings", handlers.UpdateSettings)
		api.POST("/onboarding/generate", handlers.GenerateConfig)
	}

	// Serve frontend static files
	r.Static("/assets", "./public/assets")
	r.Static("/static", "./public/static")
	r.StaticFile("/favicon.ico", "./public/favicon.ico")
	r.NoRoute(func(c *gin.Context) {
		c.File("./public/index.html")
	})

	r.Run(":8080")
}
