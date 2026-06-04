package main

import (
	"context"
	"fmt"
	"io/fs"
	"mime"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	// Embed IANA tzdata so the Proton native driver can resolve the server's
	// timezone (e.g. America/Los_Angeles) during the handshake without relying
	// on /usr/share/zoneinfo in the container image. See issue #117.
	_ "time/tzdata"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"github.com/timeplus/agentguard/internal/auth"
	"github.com/timeplus/agentguard/internal/catalog"
	"github.com/timeplus/agentguard/internal/config"
	"github.com/timeplus/agentguard/internal/handlers"
	"github.com/timeplus/agentguard/internal/holdregistry"
	"github.com/timeplus/agentguard/internal/supervisor"
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

	// Persistent so subcommands (e.g. `up`) inherit the same config flags.
	flags := cmd.PersistentFlags()
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

	cmd.AddCommand(newUpCmd())

	return cmd
}

// loadCLIConfig loads configuration honoring --config. It is shared by the
// `serve` and `up` modes so their --config semantics match: it reads
// setup.complete directly from the file (bypassing viper AutomaticEnv's
// nested-key limitation) and records ConfigPath so Provision writes back to the
// same file the server reads from (important when --config is a non-default path).
func loadCLIConfig(cmd *cobra.Command) (*config.Config, error) {
	cfgFile, _ := cmd.Flags().GetString("config")
	if cfgFile != "" {
		viper.SetConfigFile(cfgFile)
	}

	cfg, err := config.Load()
	if err != nil {
		return nil, fmt.Errorf("loading config: %w", err)
	}

	if cfgFile != "" {
		if fileCfg, err := config.LoadFrom(cfgFile); err == nil {
			cfg.SetupComplete = fileCfg.SetupComplete
		}
	}

	if p := viper.ConfigFileUsed(); p != "" {
		cfg.ConfigPath = p
	} else if cfgFile != "" {
		cfg.ConfigPath = cfgFile
	} else {
		cfg.ConfigPath = "config.yaml"
	}
	return cfg, nil
}

func runServer(cmd *cobra.Command, args []string) error {
	cfg, err := loadCLIConfig(cmd)
	if err != nil {
		return err
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
	store := auth.NewSessionStore()
	holdReg := holdregistry.New()

	// Default to release mode; set GIN_MODE=debug to enable verbose request logging.
	if os.Getenv("GIN_MODE") == "" {
		gin.SetMode(gin.ReleaseMode)
	}

	r := buildRouter(cfg, store, holdReg, mgr, frontendFS, nil)
	return r.Run(fmt.Sprintf(":%d", cfg.Server.Port))
}

// buildRouter constructs the full Gin engine. When sup is non-nil (all-in-one
// `up` mode) it also registers the supervisor status/retry routes.
func buildRouter(
	cfg *config.Config,
	store *auth.SessionStore,
	holdReg *holdregistry.Registry,
	mgr *catalog.Manager,
	frontendFS fs.FS,
	sup *supervisor.Supervisor,
) *gin.Engine {
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
		api.GET("/auth/status", auth.AuthStatus(cfg, store))
		api.POST("/auth/login", auth.Login(cfg, store))
		api.POST("/auth/logout", auth.Logout(store))
		api.POST("/setup/test-connection", handlers.TestConnection())
		api.POST("/setup/provision", handlers.Provision(cfg, mgr, store))
		api.POST("/alerts/webhook", handlers.AlertWebhook(cfg))
		api.POST("/holds", handlers.CreateHold(cfg, holdReg, handlers.GlobalAlertBroadcaster))
		api.POST("/holds/_abandon-session/:session_id", handlers.AbandonSession(cfg, holdReg))

		// All-in-one supervisor routes — registered only in `up` mode.
		if sup != nil {
			api.GET("/supervisor/status", handlers.SupervisorStatus(sup))
			api.POST("/supervisor/retry", handlers.SupervisorRetry(sup, cfg, store))
		}

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
			protected.GET("/cost/budget", handlers.GetCostBudgets(cfg))
			protected.POST("/cost/budget", handlers.UpsertCostBudget(cfg))
			protected.GET("/rules", handlers.ListRules(cfg, mgr))
			protected.GET("/rules/catalog", handlers.ListCatalog(cfg, mgr))
			protected.POST("/rules/catalog/import", handlers.ImportCatalog(mgr))
			protected.GET("/rules/catalog/export", handlers.ExportCatalog(mgr))
			protected.GET("/rules/mv-status", handlers.MVStatus(cfg, mgr))
			protected.POST("/rules/sync", handlers.SyncMVs(cfg, mgr))
			protected.POST("/rules", handlers.CreateRule(cfg, mgr))
			protected.GET("/rules/:id", handlers.GetRule(cfg, mgr))
			protected.POST("/rules/:id/install", handlers.InstallRule(cfg, mgr))
			protected.POST("/rules/:id/toggle", handlers.ToggleRule(cfg, mgr, holdReg, handlers.GlobalAlertBroadcaster))
			protected.POST("/rules/:id/upgrade", handlers.UpgradeRule(cfg, mgr))
			protected.POST("/rules/:id/policy", handlers.SetRuleBlockPolicy(cfg, mgr, holdReg, handlers.GlobalAlertBroadcaster))
			protected.DELETE("/rules/:id", handlers.UninstallRule(cfg, mgr))
			protected.GET("/holds", handlers.ListHolds(cfg))
			protected.GET("/holds/:hold_id", handlers.GetHold(cfg))
			protected.POST("/holds/:hold_id/decide", handlers.DecideHold(cfg, holdReg, handlers.GlobalAlertBroadcaster))
			protected.POST("/holds/_drain", handlers.DrainHolds(cfg, holdReg))
			protected.GET("/activity", handlers.ListActivity(cfg))
			protected.GET("/notifications/stream", handlers.NotificationsSSEHandler())
			protected.GET("/investigate/tables", handlers.ListTables(cfg))
			protected.POST("/onboarding/generate", handlers.GenerateConfig)
			protected.GET("/onboarding/install-script", handlers.ServeInstallScript)
			protected.POST("/proton/query", handlers.ProtonQueryHandler(cfg))
		}
	}

	r.NoRoute(func(c *gin.Context) {
		name := strings.TrimPrefix(c.Request.URL.Path, "/")
		if name == "" {
			name = "index.html"
		}
		data, err := fs.ReadFile(frontendFS, name)
		if err != nil {
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

	return r
}

func newUpCmd() *cobra.Command {
	up := &cobra.Command{
		Use:   "up",
		Short: "All-in-one: supervise a child timeplusd, then serve AgentGuard",
		RunE:  runUp,
	}
	flags := up.Flags()
	flags.String("timeplusd-bin", "", "path to timeplusd (default: $TIMEPLUSD_BIN, then $PATH, then embedded)")
	flags.StringSlice("timeplusd-args", defaultTimeplusdArgs(), "arguments passed to timeplusd")
	flags.String("data-dir", "/app/data", "AgentGuard state dir (holds the extracted timeplusd binary; engine data persistence is documented in Dockerfile.aio)")
	// Shared config flags (--port, --config, --timeplus-*, --catalog-dir, …) are
	// inherited from the root command's persistent flags.
	_ = viper.BindPFlag("supervisor.timeplusd_bin", flags.Lookup("timeplusd-bin"))
	_ = viper.BindPFlag("supervisor.data_dir", flags.Lookup("data-dir"))
	return up
}

func defaultTimeplusdArgs() []string {
	if p := os.Getenv("TIMEPLUSD_CONFIG"); p != "" {
		return []string{"server", "--config-file", p}
	}
	const imageConfig = "/etc/timeplusd-server/config.yaml"
	if _, err := os.Stat(imageConfig); err == nil {
		return []string{"server", "--config-file", imageConfig}
	}
	return []string{"server"}
}

func runUp(cmd *cobra.Command, args []string) error {
	cfg, err := loadCLIConfig(cmd)
	if err != nil {
		return err
	}

	frontendFS, err := web.FS()
	if err != nil {
		return fmt.Errorf("loading frontend assets: %w", err)
	}
	mgr, err := catalog.New(catalog.SeedFS, cfg.CatalogDir)
	if err != nil {
		return fmt.Errorf("loading catalog: %w", err)
	}
	store := auth.NewSessionStore()
	holdReg := holdregistry.New()

	if os.Getenv("GIN_MODE") == "" {
		gin.SetMode(gin.ReleaseMode)
	}

	// In all-in-one mode the engine is local; force the client at localhost.
	cfg.Timeplus.Host = "localhost"

	// Pre-flight: resolve timeplusd BEFORE binding HTTP (pre-supervisor failure, spec §5.1/§8).
	binFlag, _ := cmd.Flags().GetString("timeplusd-bin")
	dataDir, _ := cmd.Flags().GetString("data-dir")
	tpArgs, _ := cmd.Flags().GetStringSlice("timeplusd-args")
	binPath, err := supervisor.NewResolver(binFlag, dataDir).Resolve()
	if err != nil {
		return fmt.Errorf("pre-flight: %w", err) // fail fast, no HTTP bind
	}

	creds := supervisor.DefaultCredentialsFromEnv()
	// Service creds for server-to-server handlers (plugin → /api/holds etc.).
	cfg.Timeplus.User = creds.User()
	cfg.Timeplus.Password = creds.Password()

	sup := supervisor.New(cfg, mgr, creds, binPath, tpArgs)

	// HTTP-FIRST: start serving immediately (state = starting).
	r := buildRouter(cfg, store, holdReg, mgr, frontendFS, sup)
	srv := &http.Server{Addr: fmt.Sprintf(":%d", cfg.Server.Port), Handler: r}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	fmt.Printf("AgentGuard (all-in-one) starting on :%d; timeplusd=%s\n", cfg.Server.Port, binPath)
	// HTTP listener first so the UI/API is reachable before Boot execs timeplusd.
	serveErr := make(chan error, 1)
	go func() {
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			serveErr <- err
		}
	}()
	go func() { sup.Boot(ctx) }()

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)

	select {
	case <-sig:
		fmt.Println("shutting down…")
	case err := <-sup.Fatal():
		fmt.Fprintf(os.Stderr, "timeplusd exited after ready: %v — shutting down\n", err)
		// Drain gracefully below, then exit non-zero. The deferred os.Exit runs
		// last (after shutCancel); cobra observes the nil return first, which is fine.
		defer os.Exit(1) // fail-fast so the orchestrator restarts the unit
	case err := <-serveErr:
		return fmt.Errorf("http server: %w", err)
	}

	// Graceful drain: stop accepting HTTP, then gracefully terminate the child
	// (SIGTERM → grace → SIGKILL, owned by Stop) BEFORE cancelling the boot
	// context. The boot ctx backs exec.CommandContext, whose cancellation would
	// otherwise SIGKILL timeplusd immediately and bypass the graceful path. Running
	// Stop first means cancel() only fires on an already-terminated child; it stays
	// the crash/early-return backstop against orphaning the engine.
	shutCtx, shutCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutCancel()
	_ = srv.Shutdown(shutCtx)
	sup.Stop(10 * time.Second)
	cancel()
	return nil
}
