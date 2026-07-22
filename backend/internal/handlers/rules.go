package handlers

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/timeplus/agentguard/internal/auth"
	"github.com/timeplus/agentguard/internal/catalog"
	"github.com/timeplus/agentguard/internal/config"
	"github.com/timeplus/agentguard/internal/holdregistry"
	"github.com/timeplus/agentguard/internal/models"
	"github.com/timeplus/agentguard/internal/rules"
	tp "github.com/timeplus/agentguard/internal/timeplus"
)

// escapeSQLStr escapes a string for embedding in a Proton/ClickHouse single-quoted
// string literal (quotes and backslashes; backslashes first because Proton's
// ValuesBlockInputFormat interprets \' as an escaped single-quote, which would
// cause premature string termination if we only escaped the quote character).
//
// It is a package-wide helper (holds, threats, cost_budgets, activity, ...) and
// delegates to rules.EscapeSQLString — the canonical full escaper — so the two
// byte-identical copies can never drift. Distinct from escapeSQL (helpers.go),
// which is deliberately quote-only for identifier-ish values.
func escapeSQLStr(s string) string {
	return rules.EscapeSQLString(s)
}

// sessionUser returns the authenticated Timeplus user for audit metadata
// (updated_by), or "unknown" when the context carries none.
func sessionUser(c *gin.Context) string {
	if u, ok := c.Get("timeplus_user"); ok {
		if s, ok := u.(string); ok && s != "" {
			return s
		}
	}
	return "unknown"
}

// mutationBody is the optional JSON body shared by install/configure: the full
// parameter override map plus an audit reason.
type mutationBody struct {
	Overrides map[string]any `json:"overrides"`
	Reason    string         `json:"reason"`
}

// decodeMutationBody parses an optional {overrides, reason} body. It uses
// json.Decoder.UseNumber() so large integer override values reach the compiler
// without float64 rounding (per the rules package's JSON-consumer contract). An
// empty body is valid and yields a nil override map (install-with-defaults /
// configure-reset-all). Unknown fields are rejected: a nil Overrides map means
// "reset every parameter to its default", so a typo'd key (e.g. {"overides":…})
// must fail loudly rather than silently decode into a destructive full reset
// that redeploys the MV with defaults and returns 200.
func decodeMutationBody(c *gin.Context) (mutationBody, error) {
	var body mutationBody
	dec := json.NewDecoder(c.Request.Body)
	dec.UseNumber()
	dec.DisallowUnknownFields()
	if err := dec.Decode(&body); err != nil && !errors.Is(err, io.EOF) {
		return mutationBody{}, err
	}
	return body, nil
}

// respondRuleError maps a lifecycle-service error onto an HTTP status:
//   - *ValidationError       → 400 (per-parameter field errors)
//   - ErrAlreadyInstalled    → 409
//   - *ConflictError         → 409 (upgrade conflicts / definition drift)
//   - ErrNotFound            → 404
//   - *DefinitionError       → 500 (defect in the rule definition itself)
//   - *CriticalError         → 500 (rollback failed; MV may be out of sync)
//   - anything else          → 500 (MV/preflight failure or Timeplus error)
//
// Distinguishing a Timeplus-availability failure (503) from a preflight rejection
// (500) is not reliable from the wrapped error, so non-typed errors default to
// 500; read handlers that only ever surface store errors use 503 directly.
func respondRuleError(c *gin.Context, err error) {
	var ve *rules.ValidationError
	var de *rules.DefinitionError
	var ce *rules.ConflictError
	var crit *rules.CriticalError
	switch {
	// CriticalError is checked first: it Unwraps to its underlying cause, so the
	// errors.Is/As cases below would otherwise traverse into that cause and could
	// misclassify a critical rollback failure (always a 500) as a 404/400.
	case errors.As(err, &crit):
		log.Printf("rules: CRITICAL: %v", crit)
		c.JSON(http.StatusInternalServerError, gin.H{"error": crit.Error()})
	case errors.Is(err, rules.ErrNotFound):
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
	case errors.Is(err, rules.ErrAlreadyInstalled):
		c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
	case errors.Is(err, catalog.ErrUDFConflict): // UDF name collision — operator must rename or clone
		c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
	case errors.As(err, &ve):
		c.JSON(http.StatusBadRequest, gin.H{"error": ve.Error(), "fields": ve.Fields})
	case errors.As(err, &ce):
		c.JSON(http.StatusConflict, gin.H{"error": ce.Message, "conflicts": ce.Conflicts, "warnings": ce.Warnings})
	case errors.As(err, &de):
		c.JSON(http.StatusInternalServerError, gin.H{"error": de.Error()})
	default:
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
	}
}

// instanceToPack projects a Rule Instance snapshot into the list/summary model.
func instanceToPack(inst rules.RuleInstance) models.RulePack {
	return models.RulePack{
		ID:                inst.RuleID,
		Name:              inst.Name,
		Version:           inst.DefinitionVersion,
		Description:       inst.Description,
		RenderedSQL:       inst.RenderedSQL,
		Enabled:           inst.Enabled,
		Severity:          inst.Severity,
		Category:          inst.Category,
		RequiredTelemetry: inst.RequiredTelemetry,
		FPRate:            inst.FPRate,
		HookSources:       inst.HookSources,
		BlockPolicy:       inst.BlockPolicy,
		Revision:          inst.Revision,
		SnapshotID:        inst.SnapshotID,
	}
}

// ListRules returns all installed (non-deleted) rules with detection summaries.
func ListRules(cfg *config.Config, mgr *catalog.Manager, lc *rules.Lifecycle) gin.HandlerFunc {
	return func(c *gin.Context) {
		reqCfg := auth.RequestCfg(c, cfg)
		svc := lc.For(&reqCfg.Timeplus)

		instances, err := svc.List()
		if err != nil {
			log.Printf("rules: ListRules: %v", err)
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": err.Error()})
			return
		}

		packs := make([]models.RulePack, 0, len(instances))
		for _, inst := range instances {
			p := instanceToPack(inst)
			if def, ok := mgr.Lookup(inst.RuleID); ok && rules.CanonicalDefinitionHash(def) != inst.DefinitionHash {
				p.Upgradeable = true
				p.LatestVersion = def.Version
			}
			packs = append(packs, p)
		}

		if counts, err := queryDetectionCounts(&reqCfg.Timeplus); err != nil {
			log.Printf("rules: queryDetectionCounts: %v", err)
		} else {
			for i := range packs {
				packs[i].Detections24h = counts[packs[i].ID]
			}
		}

		recentDetections := queryRecentDetections(&reqCfg.Timeplus)

		totalDetections := 0
		activeCount := 0
		for _, r := range packs {
			totalDetections += r.Detections24h
			if r.Enabled {
				activeCount++
			}
		}
		density := 0
		if len(packs) > 0 {
			density = activeCount * 100 / len(packs)
		}

		c.JSON(http.StatusOK, models.RulesResponse{
			RulePacks:          packs,
			TotalDetections24h: totalDetections,
			ActiveRuleDensity:  density,
			RecentDetections:   recentDetections,
		})
	}
}

// GetRule returns the full read model for one installed rule: instance state,
// parameter schema, defaults, overrides, effective values, rendered SQL, MV
// health, the pending-upgrade plan (if any), and recent detections.
func GetRule(cfg *config.Config, lc *rules.Lifecycle) gin.HandlerFunc {
	return func(c *gin.Context) {
		reqCfg := auth.RequestCfg(c, cfg)
		id := c.Param("id")
		svc := lc.For(&reqCfg.Timeplus)

		view, err := svc.Describe(id)
		if err != nil {
			if errors.Is(err, rules.ErrNotFound) {
				c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
				return
			}
			log.Printf("rules: GetRule describe %s: %v", id, err)
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": err.Error()})
			return
		}

		var detections24h int
		if rows, err := tp.Query(&reqCfg.Timeplus, fmt.Sprintf(`
			SELECT count() AS cnt
			FROM table(agentguard_security_events)
			WHERE rule_id = '%s' AND detected_at >= now() - interval 24 hour
		`, escapeSQL(id))); err != nil {
			log.Printf("rules: GetRule detection count %s: %v", id, err)
		} else if len(rows) > 0 {
			detections24h = int(parseInt64(rows[0]["cnt"]))
		}

		// Recent detections are best-effort: a hiccup here must not fail an
		// otherwise-complete rule read (the instance/schema/MV health are the
		// load-bearing parts).
		var recentDetections []models.SecurityEvent
		if recentRows, err := tp.Query(&reqCfg.Timeplus, fmt.Sprintf(`
			SELECT rule_id, rule_name, severity, agent_id, hook_name, message, detected_at
			FROM table(agentguard_security_events)
			WHERE rule_id = '%s'
			ORDER BY detected_at DESC
			LIMIT 50
		`, escapeSQL(id))); err != nil {
			log.Printf("rules: GetRule recent detections %s: %v", id, err)
		} else {
			recentDetections = rowsToSecurityEvents(recentRows)
		}

		c.JSON(http.StatusOK, gin.H{
			"instance":          view.Instance,
			"in_catalog":        view.InCatalog,
			"definition":        view.Definition,
			"schema":            view.Schema,
			"defaults":          view.Defaults,
			"effective":         view.Effective,
			"overrides":         view.Instance.Overrides,
			"rendered_sql":      view.Instance.RenderedSQL,
			"upgrade":           view.Upgrade,
			"mv":                view.MV,
			"detections_24h":    detections24h,
			"recent_detections": recentDetections,
		})
	}
}

// InstallRule installs a catalog rule using defaults plus optional typed overrides.
func InstallRule(cfg *config.Config, lc *rules.Lifecycle) gin.HandlerFunc {
	return func(c *gin.Context) {
		reqCfg := auth.RequestCfg(c, cfg)
		id := c.Param("id")
		body, err := decodeMutationBody(c)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body"})
			return
		}
		reason := body.Reason
		if reason == "" {
			reason = "install"
		}
		inst, err := lc.For(&reqCfg.Timeplus).Install(id, body.Overrides, sessionUser(c), reason)
		if err != nil {
			log.Printf("rules: InstallRule %s: %v", id, err)
			respondRuleError(c, err)
			return
		}
		c.JSON(http.StatusOK, inst)
	}
}

// ConfigureRule replaces an installed rule's full parameter override map.
// Omitted keys reset to the definition default.
func ConfigureRule(cfg *config.Config, lc *rules.Lifecycle) gin.HandlerFunc {
	return func(c *gin.Context) {
		reqCfg := auth.RequestCfg(c, cfg)
		id := c.Param("id")
		body, err := decodeMutationBody(c)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body"})
			return
		}
		reason := body.Reason
		if reason == "" {
			reason = "configure"
		}
		inst, err := lc.For(&reqCfg.Timeplus).Configure(id, body.Overrides, sessionUser(c), reason)
		if err != nil {
			log.Printf("rules: ConfigureRule %s: %v", id, err)
			respondRuleError(c, err)
			return
		}
		c.JSON(http.StatusOK, inst)
	}
}

// ToggleRule flips a rule's enabled flag through the lifecycle service.
func ToggleRule(cfg *config.Config, lc *rules.Lifecycle, holdReg *holdregistry.Registry, broadcaster *alertBroadcaster) gin.HandlerFunc {
	return func(c *gin.Context) {
		reqCfg := auth.RequestCfg(c, cfg)
		id := c.Param("id")

		inst, err := lc.For(&reqCfg.Timeplus).Toggle(id, sessionUser(c), "toggle")
		if err != nil {
			log.Printf("rules: ToggleRule %s: %v", id, err)
			respondRuleError(c, err)
			return
		}
		// Disabling a rule must also abandon any pending holds — the operator's
		// intent is "stop blocking," and stale holds otherwise stay pending until
		// timeout and re-pop after every page reload. Post-persistence side effect.
		if !inst.Enabled {
			AbandonHoldsForRule(reqCfg, holdReg, broadcaster, id, "rule disabled")
		}
		c.JSON(http.StatusOK, inst)
	}
}

// SetRuleBlockPolicy sets log_only|auto_block|hold without toggling enabled.
func SetRuleBlockPolicy(cfg *config.Config, lc *rules.Lifecycle, holdReg *holdregistry.Registry, broadcaster *alertBroadcaster) gin.HandlerFunc {
	return func(c *gin.Context) {
		reqCfg := auth.RequestCfg(c, cfg)
		id := c.Param("id")
		var body struct {
			BlockPolicy string `json:"block_policy"`
			Reason      string `json:"reason"`
		}
		if err := c.ShouldBindJSON(&body); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body"})
			return
		}
		reason := body.Reason
		if reason == "" {
			reason = "set policy"
		}
		inst, err := lc.For(&reqCfg.Timeplus).SetPolicy(id, body.BlockPolicy, sessionUser(c), reason)
		if err != nil {
			log.Printf("rules: SetRuleBlockPolicy %s: %v", id, err)
			respondRuleError(c, err)
			return
		}
		// Switching to log_only must abandon pending holds created under the old
		// policy — the operator has declared this rule shouldn't block anything.
		if inst.BlockPolicy == "log_only" {
			AbandonHoldsForRule(reqCfg, holdReg, broadcaster, id, "rule policy changed to log_only")
		}
		c.JSON(http.StatusOK, inst)
	}
}

// UpgradeRule upgrades an installed rule to the current catalog definition,
// preserving compatible overrides. There is no request body; raw SQL editing of
// installed rules is not supported.
func UpgradeRule(cfg *config.Config, lc *rules.Lifecycle) gin.HandlerFunc {
	return func(c *gin.Context) {
		reqCfg := auth.RequestCfg(c, cfg)
		id := c.Param("id")

		inst, err := lc.For(&reqCfg.Timeplus).Upgrade(id, sessionUser(c), "upgrade")
		if err != nil {
			log.Printf("rules: UpgradeRule %s: %v", id, err)
			respondRuleError(c, err)
			return
		}
		c.JSON(http.StatusOK, inst)
	}
}

// UninstallRule soft-deletes a rule and drops its MV through the service.
func UninstallRule(cfg *config.Config, lc *rules.Lifecycle, holdReg *holdregistry.Registry, broadcaster *alertBroadcaster) gin.HandlerFunc {
	return func(c *gin.Context) {
		reqCfg := auth.RequestCfg(c, cfg)
		id := c.Param("id")

		if _, err := lc.For(&reqCfg.Timeplus).Uninstall(id, sessionUser(c), "uninstall"); err != nil {
			log.Printf("rules: UninstallRule %s: %v", id, err)
			respondRuleError(c, err)
			return
		}
		// An uninstalled rule blocks nothing; abandon any pending holds.
		AbandonHoldsForRule(reqCfg, holdReg, broadcaster, id, "rule uninstalled")
		c.JSON(http.StatusOK, gin.H{"deleted": true})
	}
}

// CreateRule is the thin create-custom-rule path: it builds a zero-parameter
// custom definition (sql_template ≡ rendered SQL), saves it to the catalog, and
// installs it through the same lifecycle service. It is NOT a raw-SQL editor for
// installed rules.
func CreateRule(cfg *config.Config, mgr *catalog.Manager, lc *rules.Lifecycle) gin.HandlerFunc {
	return func(c *gin.Context) {
		reqCfg := auth.RequestCfg(c, cfg)
		var body struct {
			ID          string   `json:"id"`
			Name        string   `json:"name"`
			Description string   `json:"description"`
			QuerySQL    string   `json:"query_sql"`
			Severity    string   `json:"severity"`
			Category    string   `json:"category"`
			FPRate      string   `json:"fp_rate"`
			HookSources string   `json:"hook_sources"`
			UDFs        []string `json:"udfs"`
		}
		if err := c.ShouldBindJSON(&body); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body"})
			return
		}
		if body.Name == "" || body.Description == "" || body.QuerySQL == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "name, description, and query_sql are required"})
			return
		}
		if body.Severity == "" {
			body.Severity = "warning"
		}

		ruleID := body.ID
		if ruleID == "" {
			ruleID = fmt.Sprintf("custom-%d", time.Now().UnixNano())
		}

		// Store the submitted query verbatim — sentinels and all. The compiler
		// bakes the '__rule_id__'/'__rule_name__' identity at render time
		// (rules.Compile), rejecting a name that carries '{{'/'}}' then, so a later
		// rename through the editor re-bakes the new name instead of being stuck
		// with a create-time literal.
		def := catalog.Definition{
			ID:          ruleID,
			Name:        body.Name,
			Version:     "1.0.0",
			Description: body.Description,
			SQLTemplate: body.QuerySQL,
			Severity:    body.Severity,
			Category:    body.Category,
			FPRate:      body.FPRate,
			HookSources: body.HookSources,
			Author:      "custom",
			Tags:        []string{},
			UDFs:        body.UDFs,
			Origin:      catalog.OriginCustom,
		}
		// SaveIfAbsent claims the ID atomically: the existence check and the write
		// happen under one lock, so a duplicate ID is rejected with ErrExists (→409)
		// rather than the old non-atomic ValidateID()+Save() where two concurrent
		// creates of the same ID could both pass the guard and overwrite each other.
		// It also runs full definition validation (bad id/template/UDF → 400/409).
		// Mirrors CreateCatalogRule so the legacy and catalog create paths share the
		// same atomic claim; the concurrency guarantee is proven at the catalog layer
		// by TestSaveIfAbsent_ConcurrentSameIDExactlyOneWins.
		if err := mgr.SaveIfAbsent(def); err != nil {
			log.Printf("rules: CreateRule save %s: %v", ruleID, err)
			respondCatalogError(c, err)
			return
		}

		inst, err := lc.For(&reqCfg.Timeplus).Install(ruleID, nil, sessionUser(c), "create custom rule")
		if err != nil {
			// Roll back only the definition THIS request created (SaveIfAbsent
			// succeeded above, so we own it) — never a definition an earlier or
			// concurrent request installed under the same ID. Also matters because an
			// omitted id auto-generates a fresh one on every retry.
			if delErr := mgr.Delete(ruleID); delErr != nil {
				log.Printf("rules: CreateRule rollback delete %s: %v", ruleID, delErr)
			}
			log.Printf("rules: CreateRule install %s: %v", ruleID, err)
			respondRuleError(c, err)
			return
		}
		c.JSON(http.StatusCreated, inst)
	}
}

// ListCatalog returns all catalog definitions annotated with install status.
func ListCatalog(cfg *config.Config, mgr *catalog.Manager, lc *rules.Lifecycle) gin.HandlerFunc {
	return func(c *gin.Context) {
		reqCfg := auth.RequestCfg(c, cfg)
		installed, err := lc.For(&reqCfg.Timeplus).List()
		if err != nil {
			log.Printf("rules: ListCatalog list: %v", err)
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": fmt.Sprintf("catalog query failed: %v", err)})
			return
		}
		installedMap := make(map[string]rules.RuleInstance, len(installed))
		for _, inst := range installed {
			installedMap[inst.RuleID] = inst
		}

		type CatalogItem struct {
			ID                string   `json:"id"`
			Name              string   `json:"name"`
			Version           string   `json:"version"`
			Description       string   `json:"description"`
			Severity          string   `json:"severity"`
			Category          string   `json:"category"`
			FPRate            string   `json:"fp_rate"`
			HookSources       string   `json:"hook_sources"`
			RequiredTelemetry string   `json:"required_telemetry"`
			Author            string   `json:"author"`
			Tags              []string `json:"tags"`
			Installed         bool     `json:"installed"`
			Enabled           bool     `json:"enabled"`
			InstalledVersion  string   `json:"installed_version,omitempty"`
			Upgradeable       bool     `json:"upgradeable"`
		}

		entries := mgr.All()
		items := make([]CatalogItem, 0, len(entries))
		for _, e := range entries {
			item := CatalogItem{
				ID:                e.ID,
				Name:              e.Name,
				Version:           e.Version,
				Description:       e.Description,
				Severity:          e.Severity,
				Category:          e.Category,
				FPRate:            e.FPRate,
				HookSources:       e.HookSources,
				RequiredTelemetry: e.RequiredTelemetry,
				Author:            e.Author,
				Tags:              e.Tags,
			}
			if inst, ok := installedMap[e.ID]; ok {
				item.Installed = true
				item.Enabled = inst.Enabled
				item.InstalledVersion = inst.DefinitionVersion
				item.Upgradeable = rules.CanonicalDefinitionHash(e) != inst.DefinitionHash
			}
			items = append(items, item)
		}

		// Installed rules absent from the catalog (orphaned).
		for _, inst := range installed {
			if !mgr.Exists(inst.RuleID) {
				items = append(items, CatalogItem{
					ID:               inst.RuleID,
					Name:             inst.Name,
					Version:          inst.DefinitionVersion,
					Description:      inst.Description,
					Severity:         inst.Severity,
					Category:         inst.Category,
					FPRate:           inst.FPRate,
					HookSources:      inst.HookSources,
					Author:           "unknown",
					Tags:             []string{"orphaned"},
					Installed:        true,
					Enabled:          inst.Enabled,
					InstalledVersion: inst.DefinitionVersion,
				})
			}
		}

		c.JSON(http.StatusOK, gin.H{"catalog": items})
	}
}

// MVStatus reports each rule's MV existence and Proton failure state.
func MVStatus(cfg *config.Config, lc *rules.Lifecycle) gin.HandlerFunc {
	return func(c *gin.Context) {
		reqCfg := auth.RequestCfg(c, cfg)
		instances, err := lc.For(&reqCfg.Timeplus).List()
		if err != nil {
			log.Printf("rules: MVStatus list: %v", err)
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": err.Error()})
			return
		}

		rows, err := tp.Query(&reqCfg.Timeplus, `
			SELECT name FROM system.tables
			WHERE name LIKE 'mv_rule_%' AND database = current_database()
		`)
		if err != nil {
			log.Printf("rules: MVStatus system.tables: %v", err)
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": err.Error()})
			return
		}
		existing := map[string]bool{}
		for _, row := range rows {
			if name, ok := row["name"].(string); ok {
				existing[name] = true
			}
		}

		// Failed-MV state is a health signal; a paused/missing MV for a disabled
		// rule is expected, not a failure (cross-referenced via RuleEnabled below).
		failed, ferr := tp.FailedMVs(&reqCfg.Timeplus)
		if ferr != nil {
			log.Printf("rules: MVStatus failed-mv read: %v", ferr)
		}

		type MVInfo struct {
			RuleID      string `json:"rule_id"`
			MVName      string `json:"mv_name"`
			RuleEnabled bool   `json:"rule_enabled"`
			MVExists    bool   `json:"mv_exists"`
			FailedState string `json:"failed_state,omitempty"`
			Healthy     bool   `json:"healthy"`
		}
		result := make([]MVInfo, 0, len(instances))
		for _, inst := range instances {
			mvName := tp.MVName(inst.RuleID)
			exists := existing[mvName]
			fm, isFailed := failed[mvName]
			info := MVInfo{
				RuleID:      inst.RuleID,
				MVName:      mvName,
				RuleEnabled: inst.Enabled,
				MVExists:    exists,
				Healthy:     !inst.Enabled || (exists && !isFailed),
			}
			if isFailed {
				info.FailedState = fm.State
			}
			result = append(result, info)
		}
		c.JSON(http.StatusOK, gin.H{"mvs": result})
	}
}

// recordDLPProvisioned persists the DLP model snapshot to the live config and
// config.yaml after a successful (re)provision, so GET /api/config stops flagging
// "re-provision required" for values that were just applied (notably
// embedding_dim). No-op when DLP is disabled or nothing changed. Mirrors the
// snapshot the setup wizard writes (setup.go).
func recordDLPProvisioned(cfg *config.Config, dlp config.DLPConfig, detectedDim int) {
	if !dlp.Enabled {
		return
	}
	// Prefer the probed dimension; fall back to the last-known/configured value
	// when the endpoint was unreachable at provision time.
	embDim := detectedDim
	if embDim <= 0 {
		embDim = dlp.EmbeddingDim
	}
	next := config.ProvisionedDLP{
		Recorded:              true,
		EmbeddingModel:        dlp.EmbeddingModel,
		EmbeddingDim:          embDim,
		RiskModel:             dlp.RiskModel,
		SummaryModel:          dlp.SummaryModel,
		RequestTimeoutSeconds: dlp.RequestTimeoutSeconds,
		MaxOutputTokens:       dlp.MaxOutputTokens,
	}
	config.Mu.Lock()
	if cfg.ProvisionedDLP == next && cfg.DLP.EmbeddingDim == embDim {
		config.Mu.Unlock()
		return
	}
	cfg.DLP.EmbeddingDim = embDim // reflect the detected dim in the live config
	cfg.ProvisionedDLP = next
	snapshot := *cfg
	path := cfg.ConfigPath
	config.Mu.Unlock()

	if path == "" {
		return
	}
	if err := config.Save(&snapshot, path); err != nil {
		log.Printf("rules: SyncMVs persist provisioned DLP snapshot: %v", err)
	}
}

// SyncMVs reconciles Proton MVs with the current rule snapshots (via the lifecycle
// service) without a restart, and best-effort re-provisions DLP so endpoint/model
// config changes apply without re-running the setup wizard.
func SyncMVs(cfg *config.Config, mgr *catalog.Manager, lc *rules.Lifecycle) gin.HandlerFunc {
	shared := cfg // process-wide config, for persisting the DLP provision snapshot
	return func(c *gin.Context) {
		reqCfg := auth.RequestCfg(c, cfg)
		results, err := lc.For(&reqCfg.Timeplus).Sync()
		if err != nil {
			log.Printf("rules: SyncMVs: %v", err)
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": err.Error()})
			return
		}
		// Orphaned installed rules: reconcile just rebuilt their MVs from the
		// instance snapshot (so they keep detecting), but their catalog definition
		// is gone — they cannot be edited or upgraded until it is restored or the
		// rule is reinstalled. Surface them loudly here (a WARNING plus an explicit
		// response field) rather than only as a passive in_catalog:false that a
		// caller has to go looking for. Sync results already enumerate every
		// installed rule_id, so this needs no extra query.
		orphaned := orphanedInstalledRuleIDs(results, mgr)
		if len(orphaned) > 0 {
			log.Printf("rules: SyncMVs: WARNING: %d installed rule(s) have no catalog definition and were reconciled from their last-deployed snapshot: %v — restore the definition or reinstall to edit/upgrade them", len(orphaned), orphaned)
		}
		// Best-effort DLP re-provision so endpoint/model config changes apply
		// without the setup wizard. Failure must not fail rule-MV sync.
		if err := tp.SyncDLP(reqCfg.Timeplus, reqCfg.DLP); err != nil {
			log.Printf("rules: SyncMVs DLP sync: %v", err)
		} else {
			// Probe the model for the embedding dimension and record what we just
			// provisioned, so GET /api/config reflects the auto-detected dim. The
			// probe must use the request-scoped config: on deployments where the
			// Timeplus credentials come only from session login, the process-wide
			// cfg carries none, the probe fails, and its discarded error silently
			// leaves the stale configured dim in the provision snapshot.
			detectedDim, _ := tp.ProbeEmbeddingDim(&reqCfg.Timeplus)
			recordDLPProvisioned(shared, reqCfg.DLP, detectedDim)
		}
		// Best-effort Sentry re-provision, same rationale as DLP above.
		if reqCfg.Sentry.Enabled {
			if err := tp.SyncSentry(reqCfg.Timeplus, reqCfg.Sentry, reqCfg.DLP); err != nil {
				log.Printf("rules: SyncMVs Sentry sync: %v", err)
			}
		}

		c.JSON(http.StatusOK, gin.H{"results": results, "orphaned": orphaned})
	}
}

// orphanedInstalledRuleIDs returns the rule_ids among sync results whose catalog
// definition is absent: installed rules whose MV reconcile keeps running from the
// snapshot but which can no longer be edited or upgraded (the definition was
// dropped, e.g. a non-compiling local file skipped at load). Returns a non-nil
// empty slice so the JSON field serializes as [] rather than null.
func orphanedInstalledRuleIDs(results []rules.SyncResult, mgr *catalog.Manager) []string {
	orphaned := []string{}
	for _, r := range results {
		if !mgr.Exists(r.RuleID) {
			orphaned = append(orphaned, r.RuleID)
		}
	}
	return orphaned
}

// threatOpenCheckSQL builds the open-threat dedup query for the alert webhook.
// The three identifiers arrive on the UNAUTHENTICATED /api/alerts/webhook
// endpoint, so they must go through escapeSQLStr (backslash-aware): with the
// quote-only escapeSQL, a value like `\' OR (...) --` terminates the literal,
// because Proton reads \' as an escaped single quote. Extracted from
// isThreatAlreadyOpen so the escaping is pinned by a unit test.
func threatOpenCheckSQL(agentID, deploymentID, ruleID string) string {
	if deploymentID != "" {
		return fmt.Sprintf(
			`SELECT count() AS cnt FROM table(agentguard_threats) WHERE agent_id = '%s' AND deployment_id = '%s' AND rule_id = '%s' AND (status = '' OR status = 'open')`,
			escapeSQLStr(agentID), escapeSQLStr(deploymentID), escapeSQLStr(ruleID),
		)
	}
	return fmt.Sprintf(
		`SELECT count() AS cnt FROM table(agentguard_threats) WHERE agent_id = '%s' AND rule_id = '%s' AND (status = '' OR status = 'open')`,
		escapeSQLStr(agentID), escapeSQLStr(ruleID),
	)
}

// isThreatAlreadyOpen queries agentguard_threats to check whether an open
// threat already exists for the given (agent_id, deployment_id, rule_id)
// combination. When deployment_id is empty the check is by (agent_id, rule_id)
// alone. Returns false on any error so the caller fails open.
func isThreatAlreadyOpen(cfg *config.Config, agentID, deploymentID, ruleID string) bool {
	if agentID == "" || ruleID == "" {
		return false
	}
	rows, err := tp.Query(&cfg.Timeplus, threatOpenCheckSQL(agentID, deploymentID, ruleID))
	if err != nil || len(rows) == 0 {
		return false
	}
	return parseInt64(rows[0]["cnt"]) > 0
}

func AlertWebhook(cfg *config.Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		queryCfg, errMsg := pluginCfg(cfg)
		if errMsg != "" {
			log.Printf("alert: AlertWebhook: %s", errMsg)
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": errMsg})
			return
		}

		var payload models.AlertWebhookPayload
		if err := c.ShouldBindJSON(&payload); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid payload"})
			return
		}
		log.Printf("alert: rule=%s severity=%s agent=%s deployment=%s msg=%s",
			payload.RuleID, payload.Severity, payload.AgentID, payload.DeploymentID, payload.Message)

		// Deduplicate: suppress the SSE notification when an open threat already
		// exists for this (agent, deployment, rule) — the MV occasionally fires
		// multiple times for the same active threat.
		if isThreatAlreadyOpen(queryCfg, payload.AgentID, payload.DeploymentID, payload.RuleID) {
			log.Printf("alert: suppressed duplicate notification for rule=%s agent=%s (threat already open)",
				payload.RuleID, payload.AgentID)
			c.JSON(http.StatusOK, gin.H{"received": true, "suppressed": true})
			return
		}

		GlobalAlertBroadcaster.Publish(payload)
		c.JSON(http.StatusOK, gin.H{"received": true})
	}
}

// ── Read helpers ────────────────────────────────────────────────────────────────

func queryDetectionCounts(cfg *config.TimeplusConfig) (map[string]int, error) {
	rows, err := tp.Query(cfg, `
		SELECT rule_id, count() AS cnt
		FROM table(agentguard_security_events)
		WHERE detected_at >= now() - interval 24 hour
		GROUP BY rule_id
	`)
	if err != nil {
		return nil, err
	}
	result := make(map[string]int, len(rows))
	for _, row := range rows {
		result[parseStr(row["rule_id"])] = int(parseInt64(row["cnt"]))
	}
	return result, nil
}

func queryRecentDetections(cfg *config.TimeplusConfig) []models.SecurityEvent {
	rows, err := tp.Query(cfg, `
		SELECT rule_id, rule_name, severity, agent_id, hook_name, message, detected_at
		FROM table(agentguard_security_events)
		ORDER BY detected_at DESC
		LIMIT 10
	`)
	if err != nil {
		log.Printf("rules: queryRecentDetections: %v", err)
		return nil
	}
	if len(rows) == 0 {
		return nil
	}
	return rowsToSecurityEvents(rows)
}

func rowsToSecurityEvents(rows []tp.Row) []models.SecurityEvent {
	events := make([]models.SecurityEvent, 0, len(rows))
	for _, row := range rows {
		events = append(events, models.SecurityEvent{
			RuleID:     parseStr(row["rule_id"]),
			RuleName:   parseStr(row["rule_name"]),
			Severity:   parseStr(row["severity"]),
			AgentID:    parseStr(row["agent_id"]),
			HookName:   parseStr(row["hook_name"]),
			Message:    parseStr(row["message"]),
			DetectedAt: parseStr(row["detected_at"]),
		})
	}
	return events
}
