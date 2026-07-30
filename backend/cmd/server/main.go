package main

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"mime"
	"net"
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
	"github.com/spf13/pflag"
	"github.com/spf13/viper"
	"github.com/timeplus/agentguard/internal/auth"
	"github.com/timeplus/agentguard/internal/catalog"
	"github.com/timeplus/agentguard/internal/config"
	"github.com/timeplus/agentguard/internal/handlers"
	"github.com/timeplus/agentguard/internal/holdregistry"
	"github.com/timeplus/agentguard/internal/rbac"
	"github.com/timeplus/agentguard/internal/rules"
	"github.com/timeplus/agentguard/internal/secrets"
	"github.com/timeplus/agentguard/internal/sentry"
	"github.com/timeplus/agentguard/internal/web"
)

const gracefulShutdownTimeout = 5 * time.Second

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
		// Runtime failures (bind error, config load) are operational, not usage
		// mistakes — don't dump the full flag help. main() prints the error itself,
		// so silence Cobra's own error print to avoid a duplicate line.
		SilenceUsage:  true,
		SilenceErrors: true,
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

	// Record the config file path so Provision writes to the same file the
	// server reads from (important when --config points to a non-default path).
	if p := viper.ConfigFileUsed(); p != "" {
		cfg.ConfigPath = p
	} else if cfgFile != "" {
		cfg.ConfigPath = cfgFile
	} else {
		cfg.ConfigPath = "config.yaml"
	}

	// Credential-at-rest encryption (#234): resolve the master key (env var
	// AGENTGUARD_ENCRYPTION_KEY, else an auto-generated key file next to
	// config.yaml). Failing to establish a key is fatal — starting without
	// it would silently store credentials in plaintext.
	if err := secrets.Init(filepath.Dir(cfg.ConfigPath)); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to initialize credential encryption: %v\n", err)
		os.Exit(1)
	}

	// Capture which CLI flags were explicitly set, so the config UI can mark
	// flag-overridden keys read-only and show the correct source.
	changedFlags := map[string]bool{}
	cmd.Flags().Visit(func(f *pflag.Flag) {
		changedFlags[f.Name] = true
	})

	frontendFS, err := web.FS()
	if err != nil {
		return fmt.Errorf("loading frontend assets: %w", err)
	}

	// Reject definitions whose parameter template/defaults don't compile — at
	// Save/Import time (client error) and as local-dir files load (skipped) —
	// rather than at first install. Injected via an option so it is set before
	// the catalog loads its local directory, and to keep the catalog package
	// free of an internal/rules import.
	mgr, err := catalog.New(catalog.SeedFS, cfg.CatalogDir, catalog.WithSemanticValidator(rules.ValidateDefinition))
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
	// RBAC (#243): the store reads/writes access-control streams with the
	// service account (cfg.Timeplus), independent of the caller's credentials.
	rbacStore := &rbac.Store{TP: &cfg.Timeplus}
	rbacResolver := rbac.NewResolver(rbacStore)
	// lifecycle owns the process-wide per-rule mutation locks; handlers bind it
	// to each request's Timeplus config via lifecycle.For(cfg).
	lifecycle := rules.NewLifecycle(mgr)
	sentryApprovals := sentry.NewApprovalRegistry()
	if os.Getenv("SENTRY_DEMO_WRITE") == "1" {
		fmt.Println("Sentry: demo_write gated tool ENABLED (SENTRY_DEMO_WRITE=1) — approval-flow verification only")
	}

	// Default to release mode; set GIN_MODE=debug to enable verbose request logging.
	if os.Getenv("GIN_MODE") == "" {
		gin.SetMode(gin.ReleaseMode)
	}
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
		api.GET("/auth/status", auth.AuthStatus(cfg, store, rbacResolver))
		api.POST("/auth/login", auth.Login(cfg, store))
		api.POST("/auth/logout", auth.Logout(store))
		api.POST("/setup/test-connection", handlers.TestConnection())
		api.POST("/setup/provision", handlers.Provision(cfg, store))
		// /alerts/webhook is called by Timeplus (server-to-server), not by the browser,
		// so it must be reachable without a session cookie.
		api.POST("/alerts/webhook", handlers.AlertWebhook(cfg))
		// /holds is called by agent plugins (server-to-server) — no session cookie.
		api.POST("/holds", handlers.CreateHold(cfg, holdReg, handlers.GlobalAlertBroadcaster))
		// /holds/_abandon-session/:session_id — unauthenticated (plugin calls fire-and-forget on SessionEnd).
		api.POST("/holds/_abandon-session/:session_id", handlers.AbandonSession(cfg, holdReg))

		// All existing routes — require a valid session cookie, then a role
		// permission for the matched route (rbac.Middleware; no-op until RBAC
		// is provisioned). See internal/rbac/middleware.go for the route map.
		protected := api.Group("/", auth.AuthRequired(store), rbac.Middleware(rbacResolver))
		{
			protected.GET("/dashboard", handlers.GetDashboard(cfg))
			protected.GET("/agents", handlers.ListAgents(cfg))
			protected.GET("/agents/:id", handlers.GetAgent(cfg))
			protected.GET("/agents/:id/sessions", handlers.ListSessions(cfg))
			protected.GET("/agents/:id/sessions/:session_id", handlers.GetSession(cfg))
			protected.GET("/agents/:id/sessions/:session_id/runs/:run_id/trace", handlers.GetRunTrace(cfg))
			protected.GET("/agents/:id/sessions/:session_id/timeline", handlers.GetSessionTimeline(cfg))
			protected.GET("/agents/:id/events", handlers.ListEvents(cfg))
			protected.GET("/metrics/llm", handlers.GetLLMMetrics(cfg))
			protected.GET("/metrics/tools", handlers.GetToolMetrics(cfg))
			protected.GET("/threats", handlers.ListThreats(cfg, mgr))
			protected.GET("/threats/:id", handlers.GetThreat(cfg, mgr))
			protected.POST("/threats/:id/acknowledge", handlers.AcknowledgeThreat(cfg))
			protected.POST("/threats/:id/clear", handlers.ClearThreat(cfg))
			protected.GET("/cost", handlers.GetCost(cfg))
			protected.GET("/cost/pricing", handlers.ListPricing(cfg))
			protected.POST("/cost/pricing", handlers.UpsertPricing(cfg))
			protected.DELETE("/cost/pricing/:provider/:model", handlers.DeletePricing(cfg))
			protected.GET("/cost/budget", handlers.GetCostBudgets(cfg))
			protected.POST("/cost/budget", handlers.UpsertCostBudget(cfg))
			protected.GET("/rules", handlers.ListRules(cfg, mgr, lifecycle))
			protected.GET("/rules/catalog", handlers.ListCatalog(cfg, mgr, lifecycle))
			protected.POST("/rules/catalog", handlers.CreateCatalogRule(mgr))
			protected.POST("/rules/catalog/import", handlers.ImportCatalog(mgr))
			protected.GET("/rules/catalog/export", handlers.ExportCatalog(mgr))
			protected.POST("/rules/catalog/compile", handlers.CompileCatalogRule())
			protected.GET("/rules/catalog/:id", handlers.GetCatalogRule(mgr))
			protected.PUT("/rules/catalog/:id", handlers.UpdateCatalogRule(cfg, mgr, lifecycle))
			protected.DELETE("/rules/catalog/:id", handlers.DeleteCatalogRule(cfg, mgr, lifecycle))
			protected.POST("/rules/catalog/:id/clone", handlers.CloneCatalogRule(mgr))
			protected.GET("/rules/mv-status", handlers.MVStatus(cfg, lifecycle))
			protected.POST("/rules/sync", handlers.SyncMVs(cfg, mgr, lifecycle))
			protected.POST("/rules", handlers.CreateRule(cfg, mgr, lifecycle))
			protected.GET("/rules/:id", handlers.GetRule(cfg, lifecycle))
			protected.POST("/rules/:id/install", handlers.InstallRule(cfg, lifecycle))
			protected.PUT("/rules/:id/configuration", handlers.ConfigureRule(cfg, lifecycle))
			protected.POST("/rules/:id/toggle", handlers.ToggleRule(cfg, lifecycle, holdReg, handlers.GlobalAlertBroadcaster))
			protected.POST("/rules/:id/upgrade", handlers.UpgradeRule(cfg, lifecycle))
			protected.POST("/rules/:id/policy", handlers.SetRuleBlockPolicy(cfg, lifecycle, holdReg, handlers.GlobalAlertBroadcaster))
			protected.DELETE("/rules/:id", handlers.UninstallRule(cfg, lifecycle, holdReg, handlers.GlobalAlertBroadcaster))
			protected.GET("/holds", handlers.ListHolds(cfg))
			protected.GET("/holds/:hold_id", handlers.GetHold(cfg))
			protected.POST("/holds/:hold_id/decide", handlers.DecideHold(cfg, holdReg, handlers.GlobalAlertBroadcaster))
			protected.POST("/holds/_drain", handlers.DrainHolds(cfg, holdReg))
			protected.GET("/activity", handlers.ListActivity(cfg))
			protected.GET("/notifications/stream", handlers.NotificationsSSEHandler())
			protected.GET("/dlp/status", handlers.DLPStatusHandler(cfg))
			protected.POST("/dlp/query", handlers.DLPQuery(cfg))
			protected.GET("/dlp/messages", handlers.ListEnrichedMessages(cfg))
			protected.GET("/dlp/messages/:id", handlers.GetEnrichedMessage(cfg))
			protected.GET("/dlp/profiles", handlers.DLPProfilesHandler(cfg))
			protected.GET("/dlp/config", handlers.GetDLPConfig(cfg))
			protected.PUT("/dlp/config/:key", handlers.UpsertDLPConfig(cfg))
			protected.POST("/sentry/chat", handlers.SentryChat(cfg, mgr, lifecycle, sentryApprovals, holdReg))
			protected.POST("/sentry/approvals/:proposal_id/decide", handlers.DecideSentryApproval(sentryApprovals))
			protected.GET("/sentry/conversations", handlers.ListSentryConversations(cfg))
			protected.GET("/sentry/conversations/:id", handlers.GetSentryConversation(cfg))
			protected.PATCH("/sentry/conversations/:id", handlers.RenameSentryConversation(cfg))
			protected.DELETE("/sentry/conversations/:id", handlers.DeleteSentryConversation(cfg))
			protected.GET("/sentry/status", handlers.SentryStatus(cfg))
			protected.GET("/sentry/config", handlers.GetSentryConfig(cfg))
			protected.PUT("/sentry/config/:key", handlers.UpsertSentryConfig(cfg))
			protected.POST("/sentry/config/test", handlers.TestSentryConfig(cfg))
			protected.POST("/onboarding/generate", handlers.GenerateConfig)
			protected.GET("/onboarding/install-script", handlers.ServeInstallScript)
			protected.POST("/proton/query", handlers.ProtonQueryHandler(cfg))
			protected.GET("/config", handlers.GetConfig(cfg, changedFlags))
			protected.PUT("/config", handlers.UpdateConfig(cfg, changedFlags))
			protected.POST("/config/test-timeplus", handlers.TestTimeplusConfig(cfg))
			protected.POST("/config/test-dlp", handlers.TestDLPConfig(cfg))

			// RBAC administration (#243) — gated by api:access:* via the middleware.
			protected.GET("/permissions", handlers.ListPermissions())
			protected.GET("/roles", handlers.ListRoles(rbacStore))
			protected.POST("/roles", handlers.CreateRole(rbacStore, rbacResolver))
			protected.PUT("/roles/:name", handlers.UpdateRole(rbacStore, rbacResolver))
			protected.DELETE("/roles/:name", handlers.DeleteRole(rbacStore, rbacResolver))
			protected.GET("/users", handlers.ListUsers(rbacStore))
			protected.PUT("/users/:user/role", handlers.AssignUserRole(rbacStore, rbacResolver))
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

	ln, err := net.Listen("tcp", fmt.Sprintf(":%d", cfg.Server.Port))
	if err != nil {
		return fmt.Errorf("listen: %w", err)
	}

	// Clean up holds left 'pending' by a previous process — a crash, or a
	// graceful-shutdown drain that signalled the plugin but (by design) wrote
	// nothing to Timeplus. startedAt is captured before Serve accepts any
	// request, so the reconcile's strict created_at threshold can never touch a
	// hold this process owns. Background + best-effort: never blocks startup.
	if cfg.SetupComplete {
		startedAt := time.Now().UTC()
		go handlers.ReconcileOrphanedHolds(cfg, holdReg, startedAt)

		// Activate RBAC on a deployment that completed setup before RBAC
		// existed (#243). The setup wizard seeds RBAC for new installs; this
		// migrates already-provisioned ones so the streams exist and the
		// service account becomes the initial admin. Background + retrying,
		// since the server starts independently of Timeplus.
		go func() {
			for attempt := 1; attempt <= 30; attempt++ {
				if err := rbac.Migrate(&cfg.Timeplus, cfg.Timeplus.User); err != nil {
					fmt.Printf("rbac: migration attempt %d failed (Timeplus not ready?): %v\n", attempt, err)
					time.Sleep(5 * time.Second)
					continue
				}
				rbacResolver.Invalidate()
				fmt.Printf("rbac: access control ready (admin bootstrap user: %q)\n", cfg.Timeplus.User)
				return
			}
			fmt.Println("rbac: migration gave up after retries; access control remains inactive")
		}()
	}

	shutdownCh, stopSignals := shutdownSignalChannel()
	defer stopSignals()
	return serveWithGracefulShutdown(ln, r, holdReg, cfg.Holds.FailPolicy, shutdownCh)
}

func shutdownSignalChannel() (<-chan os.Signal, func()) {
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGINT, syscall.SIGTERM)
	return ch, func() {
		signal.Stop(ch)
		close(ch)
	}
}

func shutdownDecision(failPolicy string) holdregistry.Decision {
	return holdregistry.Decision{
		Allow:  failPolicy == "allow",
		Reason: "AgentGuard is shutting down",
	}
}

func serveWithGracefulShutdown(
	ln net.Listener,
	handler http.Handler,
	holdReg *holdregistry.Registry,
	failPolicy string,
	shutdownCh <-chan os.Signal,
) error {
	baseCtx, cancelBase := context.WithCancel(context.Background())
	defer cancelBase()

	srv := &http.Server{
		Handler: handler,
		BaseContext: func(net.Listener) context.Context {
			return baseCtx
		},
	}

	errCh := make(chan error, 1)
	go func() {
		errCh <- srv.Serve(ln)
	}()

	select {
	case err := <-errCh:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return fmt.Errorf("http server: %w", err)
	case sig := <-shutdownCh:
		fmt.Printf("AgentGuard shutting down (%s)\n", sig)
	}

	holdReg.Drain(shutdownDecision(failPolicy))
	cancelBase()

	ctx, cancel := context.WithTimeout(context.Background(), gracefulShutdownTimeout)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		return fmt.Errorf("shutdown http server: %w", err)
	}
	if err := <-errCh; err != nil && !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("http server: %w", err)
	}
	return nil
}
