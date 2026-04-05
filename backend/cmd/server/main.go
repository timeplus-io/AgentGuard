package main

import (
	"fmt"
	"os"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"github.com/timeplus/agentshield/internal/config"
	"github.com/timeplus/agentshield/internal/handlers"
)

func main() {
	if err := newRootCmd().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func newRootCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "agentshield",
		Short: "AgentShield — AI agent security monitoring platform",
		RunE:  runServer,
	}

	flags := cmd.Flags()
	flags.String("config", "", "config file (default: ./config.yaml)")
	flags.Int("port", 8080, "server port")
	flags.String("timeplus-host", "localhost", "Proton/Timeplus host")
	flags.Int("timeplus-port", 3218, "Proton/Timeplus HTTP port")
	flags.String("timeplus-user", "", "Proton/Timeplus username")
	flags.String("timeplus-password", "", "Proton/Timeplus password")

	// Bind flags → viper keys so Load() sees them
	viper.BindPFlag("server.port", flags.Lookup("port"))
	viper.BindPFlag("timeplus.host", flags.Lookup("timeplus-host"))
	viper.BindPFlag("timeplus.port", flags.Lookup("timeplus-port"))
	viper.BindPFlag("timeplus.user", flags.Lookup("timeplus-user"))
	viper.BindPFlag("timeplus.password", flags.Lookup("timeplus-password"))

	cmd.PersistentPreRunE = func(cmd *cobra.Command, args []string) error {
		if cfgFile, _ := flags.GetString("config"); cfgFile != "" {
			viper.SetConfigFile(cfgFile)
		}
		return nil
	}

	return cmd
}

func runServer(cmd *cobra.Command, args []string) error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	fmt.Printf("AgentShield starting on :%d\n", cfg.Server.Port)
	fmt.Printf("Proton endpoint: %s:%d\n", cfg.Timeplus.Host, cfg.Timeplus.Port)

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
		api.POST("/proton/query", handlers.ProtonQueryHandler(cfg))
	}

	// Serve frontend static files
	r.Static("/assets", "./public/assets")
	r.Static("/static", "./public/static")
	r.StaticFile("/favicon.ico", "./public/favicon.ico")
	r.NoRoute(func(c *gin.Context) {
		c.File("./public/index.html")
	})

	return r.Run(fmt.Sprintf(":%d", cfg.Server.Port))
}
