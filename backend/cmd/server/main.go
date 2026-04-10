package main

import (
	"fmt"
	"io/fs"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"github.com/timeplus/agentshield/internal/catalog"
	"github.com/timeplus/agentshield/internal/config"
	"github.com/timeplus/agentshield/internal/handlers"
	"github.com/timeplus/agentshield/internal/timeplus"
	"github.com/timeplus/agentshield/internal/web"
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
	flags.String("timeplus-user", "default", "Proton/Timeplus username")
	flags.String("timeplus-password", "", "Proton/Timeplus password")
	flags.Int("timeplus-native-port", 8463, "Proton/Timeplus native TCP port")
	flags.String("catalog-dir", "", "catalog directory for rule overrides and custom rules (default: ./catalog)")

	viper.BindPFlag("server.port", flags.Lookup("port"))
	viper.BindPFlag("catalog.dir", flags.Lookup("catalog-dir"))
	viper.BindPFlag("timeplus.host", flags.Lookup("timeplus-host"))
	viper.BindPFlag("timeplus.port", flags.Lookup("timeplus-port"))
	viper.BindPFlag("timeplus.user", flags.Lookup("timeplus-user"))
	viper.BindPFlag("timeplus.password", flags.Lookup("timeplus-password"))
	viper.BindPFlag("timeplus.native-port", flags.Lookup("timeplus-native-port"))

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

	frontendFS, err := web.FS()
	if err != nil {
		return fmt.Errorf("loading frontend assets: %w", err)
	}

	// Initialize the rule catalog (embedded initial rules + local overrides/additions).
	mgr, err := catalog.New(catalog.InitialFS, cfg.CatalogDir)
	if err != nil {
		return fmt.Errorf("loading catalog: %w", err)
	}

	fmt.Printf("AgentShield starting on :%d\n", cfg.Server.Port)
	fmt.Printf("Proton endpoint: %s:%d (native: %d)\n", cfg.Timeplus.Host, cfg.Timeplus.Port, cfg.Timeplus.NativePort)
	fmt.Printf("Catalog directory: %s (%d entries)\n", cfg.CatalogDir, len(mgr.All()))

	if err := timeplus.Init(&cfg.Timeplus, mgr.All()); err != nil {
		fmt.Printf("warning: timeplus init failed: %v\n", err)
		// Non-fatal — server continues without OTel resources pre-provisioned.
	}

	r := gin.Default()
	r.Use(cors.New(cors.Config{
		AllowOrigins:     []string{"http://localhost:5173", "http://localhost:3000"},
		AllowMethods:     []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowHeaders:     []string{"Origin", "Content-Type", "Authorization"},
		AllowCredentials: true,
	}))

	r.GET("/docs", handlers.APIDocs())
	r.GET("/docs/redoc", handlers.APIRedoc())
	r.GET("/docs/openapi.json", handlers.APISpec())

	api := r.Group("/api")
	{
		api.GET("/dashboard", handlers.GetDashboard(cfg))
		api.GET("/agents", handlers.ListAgents(cfg))
		api.GET("/agents/:id", handlers.GetAgent(cfg))
		api.GET("/agents/:id/sessions", handlers.ListSessions(cfg))
		api.GET("/agents/:id/sessions/:session_id", handlers.GetSession(cfg))
		api.GET("/agents/:id/sessions/:session_id/runs/:run_id/trace", handlers.GetRunTrace(cfg))
		api.GET("/agents/:id/events", handlers.ListEvents(cfg))
		api.GET("/metrics/llm", handlers.GetLLMMetrics(cfg))
		api.GET("/metrics/tools", handlers.GetToolMetrics(cfg))
		api.GET("/threats", handlers.ListThreats(cfg))
		api.POST("/threats/:id/acknowledge", handlers.AcknowledgeThreat(cfg))
		api.POST("/threats/:id/clear", handlers.ClearThreat(cfg))
		api.GET("/cost", handlers.GetCost(cfg))
		api.GET("/cost/pricing", handlers.ListPricing(cfg))
		api.POST("/cost/pricing", handlers.UpsertPricing(cfg))
		api.DELETE("/cost/pricing/:provider/:model", handlers.DeletePricing(cfg))
		api.GET("/rules", handlers.ListRules(cfg, mgr))
		api.GET("/rules/catalog", handlers.ListCatalog(cfg, mgr))
		api.POST("/rules/catalog/import", handlers.ImportCatalog(mgr))
		api.GET("/rules/catalog/export", handlers.ExportCatalog(mgr))
		api.GET("/rules/mv-status", handlers.MVStatus(cfg, mgr))
		api.POST("/rules/sync", handlers.SyncMVs(cfg, mgr))
		api.POST("/rules", handlers.CreateRule(cfg, mgr))
		api.GET("/rules/:id", handlers.GetRule(cfg, mgr))
		api.POST("/rules/:id/install", handlers.InstallRule(cfg, mgr))
		api.POST("/rules/:id/toggle", handlers.ToggleRule(cfg, mgr))
		api.POST("/rules/:id/upgrade", handlers.UpgradeRule(cfg, mgr))
		api.DELETE("/rules/:id", handlers.UninstallRule(cfg))
		api.POST("/alerts/webhook", handlers.AlertWebhook(cfg))
		api.GET("/notifications/stream", handlers.NotificationsSSEHandler())
		api.GET("/investigate/tables", handlers.ListTables(cfg))
		api.POST("/investigate/query", handlers.RunQuery)
		api.GET("/settings", handlers.GetSettings)
		api.PUT("/settings", handlers.UpdateSettings)
		api.POST("/onboarding/generate", handlers.GenerateConfig)
		api.POST("/proton/query", handlers.ProtonQueryHandler(cfg))
	}

	// Serve the SPA. Read files directly from the fs.FS to avoid http.FileServer's
	// directory-traversal checks. Unknown paths fall back to index.html so that
	// React Router can handle client-side navigation.
	r.NoRoute(func(c *gin.Context) {
		name := strings.TrimPrefix(c.Request.URL.Path, "/")
		if name == "" {
			name = "index.html"
		}

		data, err := fs.ReadFile(frontendFS, name)
		if err != nil {
			// Not a static asset — serve index.html for SPA routing.
			data, err = fs.ReadFile(frontendFS, "index.html")
			if err != nil {
				c.Status(http.StatusNotFound)
				return
			}
			name = "index.html"
		}

		mimeType := mime.TypeByExtension(filepath.Ext(name))
		if mimeType == "" {
			mimeType = "application/octet-stream"
		}
		c.Data(http.StatusOK, mimeType, data)
	})

	return r.Run(fmt.Sprintf(":%d", cfg.Server.Port))
}
