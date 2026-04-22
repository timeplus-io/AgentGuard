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
	"github.com/timeplus/agentguard/internal/auth"
	"github.com/timeplus/agentguard/internal/catalog"
	"github.com/timeplus/agentguard/internal/config"
	"github.com/timeplus/agentguard/internal/handlers"
	"github.com/timeplus/agentguard/internal/holdregistry"
	"github.com/timeplus/agentguard/internal/web"
)

func main() {
	if err := newRootCmd().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func newRootCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "agentguard",
		Short: "AgentGuard — AI agent security monitoring platform",
		RunE:  runServer,
	}

	flags := cmd.Flags()
	flags.String("config", "", "config file (default: ./config.yaml)")
	flags.Int("port", 8080, "server port")
	flags.String("timeplus-host", "localhost", "Timeplus host")
	flags.Int("timeplus-port", 3218, "Timeplus HTTP port")
	flags.Int("timeplus-native-port", 8463, "Timeplus native TCP port")
	flags.String("catalog-dir", "", "catalog directory (default: ./catalog)")
	flags.String("agentguard-url", "", "URL Timeplus uses to call back AgentGuard (default: http://localhost:8080)")

	_ = viper.BindPFlag("server.port", flags.Lookup("port"))
	_ = viper.BindPFlag("catalog.dir", flags.Lookup("catalog-dir"))
	_ = viper.BindPFlag("timeplus.host", flags.Lookup("timeplus-host"))
	_ = viper.BindPFlag("timeplus.port", flags.Lookup("timeplus-port"))
	_ = viper.BindPFlag("timeplus.native-port", flags.Lookup("timeplus-native-port"))
	_ = viper.BindPFlag("agentguard.url", flags.Lookup("agentguard-url"))

	cmd.PersistentPreRunE = func(cmd *cobra.Command, args []string) error {
		if cfgFile, _ := flags.GetString("config"); cfgFile != "" {
			viper.SetConfigFile(cfgFile)
		}
		return nil
	}

	return cmd
}

func runServer(cmd *cobra.Command, args []string) error {
	cfgFile, _ := cmd.Flags().GetString("config")
	if cfgFile != "" {
		viper.SetConfigFile(cfgFile)
	}

	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	// viper's AutomaticEnv() prevents nested YAML keys (like setup.complete) from
	// being read correctly via the global viper. Read setup.complete directly from
	// the config file using a fresh viper instance that has no AutomaticEnv.
	if cfgFile != "" {
		if fileCfg, err := config.LoadFrom(cfgFile); err == nil {
			cfg.SetupComplete = fileCfg.SetupComplete
		}
	}

	// Record the config file path so Provision writes to the same file the
	// server reads from (important when --config points to a non-default path).
	if p := viper.ConfigFileUsed(); p != "" {
		cfg.ConfigPath = p
	} else if cfgFile != "" {
		cfg.ConfigPath = cfgFile
	} else {
		cfg.ConfigPath = "config.yaml"
	}

	frontendFS, err := web.FS()
	if err != nil {
		return fmt.Errorf("loading frontend assets: %w", err)
	}

	mgr, err := catalog.New(catalog.SeedFS, cfg.CatalogDir)
	if err != nil {
		return fmt.Errorf("loading catalog: %w", err)
	}

	fmt.Printf("AgentGuard starting on :%d\n", cfg.Server.Port)
	fmt.Printf("Timeplus endpoint: %s:%d (native: %d)\n", cfg.Timeplus.Host, cfg.Timeplus.Port, cfg.Timeplus.NativePort)
	fmt.Printf("Catalog directory: %s (%d entries)\n", cfg.CatalogDir, len(mgr.All()))
	fmt.Printf("AgentGuard webhook URL: %s\n", cfg.AgentGuardURL)
	fmt.Printf("Setup complete: %v\n", cfg.SetupComplete)
	// Note: timeplus.Init() is no longer called at startup. All Timeplus resources
	// are provisioned during the one-time /api/setup/provision flow.
	//
	// Schema additions made AFTER the original provision (like the holds feature)
	// are applied lazily by timeplus.MigrateHoldsSchema, called from the auth
	// Login handler on first successful login (when credentials become available
	// — they are deliberately not persisted to the config file).

	store := auth.NewSessionStore()
	holdReg := holdregistry.New()

	r := gin.Default()
	r.Use(cors.New(cors.Config{
		AllowOrigins:     []string{"http://localhost:5173", "http://localhost:3000"},
		AllowMethods:     []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowHeaders:     []string{"Origin", "Content-Type", "Authorization"},
		AllowCredentials: true,
	}))

	// API documentation routes — unauthenticated, served at root level.
	r.GET("/docs", handlers.APIDocs())
	r.GET("/docs/redoc", handlers.APIRedoc())
	r.GET("/docs/openapi.json", handlers.APISpec())

	api := r.Group("/api")
	{
		// Unauthenticated — must be reachable before login/setup.
		api.GET("/auth/status", auth.AuthStatus(cfg, store))
		api.POST("/auth/login", auth.Login(cfg, store))
		api.POST("/auth/logout", auth.Logout(store))
		api.POST("/setup/test-connection", handlers.TestConnection())
		api.POST("/setup/provision", handlers.Provision(cfg, mgr, store))
		// /alerts/webhook is called by Timeplus (server-to-server), not by the browser,
		// so it must be reachable without a session cookie.
		api.POST("/alerts/webhook", handlers.AlertWebhook(cfg, store))
		// /holds is called by agent plugins (server-to-server) — no session cookie.
		api.POST("/holds", handlers.CreateHold(cfg, store, holdReg, handlers.GlobalAlertBroadcaster))
		// /holds/_abandon-session/:session_id — unauthenticated (plugin calls fire-and-forget on SessionEnd).
		api.POST("/holds/_abandon-session/:session_id", handlers.AbandonSession(cfg, store, holdReg))

		// All existing routes — require a valid session cookie.
		protected := api.Group("/", auth.AuthRequired(store))
		{
			protected.GET("/dashboard", handlers.GetDashboard(cfg))
			protected.GET("/agents", handlers.ListAgents(cfg))
			protected.GET("/agents/:id", handlers.GetAgent(cfg))
			protected.GET("/agents/:id/sessions", handlers.ListSessions(cfg))
			protected.GET("/agents/:id/sessions/:session_id", handlers.GetSession(cfg))
			protected.GET("/agents/:id/sessions/:session_id/runs/:run_id/trace", handlers.GetRunTrace(cfg))
			protected.GET("/agents/:id/events", handlers.ListEvents(cfg))
			protected.GET("/metrics/llm", handlers.GetLLMMetrics(cfg))
			protected.GET("/metrics/tools", handlers.GetToolMetrics(cfg))
			protected.GET("/threats", handlers.ListThreats(cfg))
			protected.GET("/threats/:id", handlers.GetThreat(cfg))
			protected.POST("/threats/:id/acknowledge", handlers.AcknowledgeThreat(cfg))
			protected.POST("/threats/:id/clear", handlers.ClearThreat(cfg))
			protected.GET("/cost", handlers.GetCost(cfg))
			protected.GET("/cost/pricing", handlers.ListPricing(cfg))
			protected.POST("/cost/pricing", handlers.UpsertPricing(cfg))
			protected.DELETE("/cost/pricing/:provider/:model", handlers.DeletePricing(cfg))
			protected.GET("/rules", handlers.ListRules(cfg, mgr))
			protected.GET("/rules/catalog", handlers.ListCatalog(cfg, mgr))
			protected.POST("/rules/catalog/import", handlers.ImportCatalog(mgr))
			protected.GET("/rules/catalog/export", handlers.ExportCatalog(mgr))
			protected.GET("/rules/mv-status", handlers.MVStatus(cfg, mgr))
			protected.POST("/rules/sync", handlers.SyncMVs(cfg, mgr))
			protected.POST("/rules", handlers.CreateRule(cfg, mgr))
			protected.GET("/rules/:id", handlers.GetRule(cfg, mgr))
			protected.POST("/rules/:id/install", handlers.InstallRule(cfg, mgr))
			protected.POST("/rules/:id/toggle", handlers.ToggleRule(cfg, mgr))
			protected.POST("/rules/:id/upgrade", handlers.UpgradeRule(cfg, mgr))
			protected.POST("/rules/:id/policy", handlers.SetRuleBlockPolicy(cfg, mgr))
			protected.DELETE("/rules/:id", handlers.UninstallRule(cfg, mgr))
			protected.GET("/holds", handlers.ListHolds(cfg))
			protected.GET("/holds/:hold_id", handlers.GetHold(cfg))
			protected.POST("/holds/:hold_id/decide", handlers.DecideHold(cfg, holdReg, handlers.GlobalAlertBroadcaster))
			protected.POST("/holds/_drain", handlers.DrainHolds(cfg, store, holdReg))
			protected.GET("/notifications/stream", handlers.NotificationsSSEHandler())
			protected.GET("/investigate/tables", handlers.ListTables(cfg))
			protected.POST("/onboarding/generate", handlers.GenerateConfig)
			protected.POST("/proton/query", handlers.ProtonQueryHandler(cfg))
		}
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
