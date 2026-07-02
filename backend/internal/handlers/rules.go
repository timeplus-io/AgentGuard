package handlers

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/timeplus/agentguard/internal/auth"
	"github.com/timeplus/agentguard/internal/catalog"
	"github.com/timeplus/agentguard/internal/config"
	"github.com/timeplus/agentguard/internal/holdregistry"
	"github.com/timeplus/agentguard/internal/models"
	tp "github.com/timeplus/agentguard/internal/timeplus"
)

// escapeSQLStr escapes a string for embedding in a Proton/ClickHouse single-quoted
// string literal. Backslashes must be escaped first because Proton's
// ValuesBlockInputFormat interprets \' as an escaped single-quote, which would
// cause premature string termination if we only escaped the quote character.
func escapeSQLStr(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, "'", "''")
	return s
}

const sqlDeletedRules = `
SELECT
  rule_id,
  arg_max(name,               updated_at) AS name,
  arg_max(version,            updated_at) AS version,
  arg_max(description,        updated_at) AS description,
  arg_max(severity,           updated_at) AS severity,
  arg_max(category,           updated_at) AS category,
  arg_max(fp_rate,            updated_at) AS fp_rate,
  arg_max(hook_sources,       updated_at) AS hook_sources
FROM table(agentguard_rules)
GROUP BY rule_id
HAVING arg_max(deleted, updated_at) = true`

const sqlCurrentRules = `
SELECT
  rule_id,
  arg_max(name,               updated_at) AS name,
  arg_max(version,            updated_at) AS version,
  arg_max(description,        updated_at) AS description,
  arg_max(query_sql,          updated_at) AS query_sql,
  arg_max(enabled,            updated_at) AS enabled,
  arg_max(severity,           updated_at) AS severity,
  arg_max(category,           updated_at) AS category,
  arg_max(required_telemetry, updated_at) AS required_telemetry,
  arg_max(fp_rate,            updated_at) AS fp_rate,
  arg_max(hook_sources,       updated_at) AS hook_sources,
  arg_max(block_policy,       updated_at) AS block_policy
FROM table(agentguard_rules)
GROUP BY rule_id
HAVING arg_max(deleted, updated_at) = false`

func ListRules(cfg *config.Config, mgr *catalog.Manager) gin.HandlerFunc {
	return func(c *gin.Context) {
		cfg := auth.RequestCfg(c, cfg)
		packs, err := queryCurrentRulePacks(&cfg.Timeplus, mgr)
		if err != nil {
			log.Printf("rules: ListRules queryCurrentRulePacks: %v", err)
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": err.Error()})
			return
		}

		if counts, err := queryDetectionCounts(&cfg.Timeplus); err != nil {
			log.Printf("rules: queryDetectionCounts: %v", err)
		} else {
			for i := range packs {
				packs[i].Detections24h = counts[packs[i].ID]
			}
		}

		recentDetections := queryRecentDetections(&cfg.Timeplus)

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

func GetRule(cfg *config.Config, mgr *catalog.Manager) gin.HandlerFunc {
	return func(c *gin.Context) {
		cfg := auth.RequestCfg(c, cfg)
		id := c.Param("id")

		packs, err := queryCurrentRulePacks(&cfg.Timeplus, mgr)
		if err != nil {
			log.Printf("rules: GetRule queryCurrentRulePacks: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		var pack *models.RulePack
		for i := range packs {
			if packs[i].ID == id {
				pack = &packs[i]
				break
			}
		}
		if pack == nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
			return
		}

		countRows, err := tp.Query(&cfg.Timeplus, fmt.Sprintf(`
			SELECT count() AS cnt
			FROM table(agentguard_security_events)
			WHERE rule_id = '%s'
			  AND detected_at >= now() - interval 24 hour
		`, escapeSQL(id)))
		if err != nil {
			log.Printf("rules: GetRule detection count query rule=%s: %v", id, err)
		} else if len(countRows) > 0 {
			if num, ok := countRows[0]["cnt"].(json.Number); ok {
				cnt, _ := num.Int64()
				pack.Detections24h = int(cnt)
			}
		}

		recentRows, err := tp.Query(&cfg.Timeplus, fmt.Sprintf(`
			SELECT rule_id, rule_name, severity, agent_id, hook_name, message, detected_at
			FROM table(agentguard_security_events)
			WHERE rule_id = '%s'
			ORDER BY detected_at DESC
			LIMIT 50
		`, escapeSQL(id)))
		if err != nil {
			log.Printf("rules: GetRule recent detections query: %v", err)
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": err.Error()})
			return
		}

		c.JSON(http.StatusOK, models.RuleDetailResponse{
			RulePack:         *pack,
			RecentDetections: rowsToSecurityEvents(recentRows),
		})
	}
}

func ToggleRule(cfg *config.Config, mgr *catalog.Manager, holdReg *holdregistry.Registry, broadcaster *alertBroadcaster) gin.HandlerFunc {
	return func(c *gin.Context) {
		cfg := auth.RequestCfg(c, cfg)
		id := c.Param("id")

		packs, err := queryCurrentRulePacks(&cfg.Timeplus, mgr)
		if err != nil {
			log.Printf("rules: ToggleRule queryCurrentRulePacks: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		var current *models.RulePack
		for i := range packs {
			if packs[i].ID == id {
				current = &packs[i]
				break
			}
		}
		if current == nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
			return
		}

		// Run the MV operation FIRST. If it fails, we return 500 without touching
		// the agentguard_rules row — so the frontend's revert-on-error reflects
		// reality, and the rule's enabled state stays consistent with whether
		// the MV is actually running.
		newEnabled := !current.Enabled
		if newEnabled {
			exists, err := mvExists(&cfg.Timeplus, id)
			if err != nil {
				log.Printf("rules: toggle %s mvExists: %v", id, err)
				c.JSON(http.StatusServiceUnavailable, gin.H{"error": "MV status check failed"})
				return
			}
			if exists {
				if err := tp.ResumeRuleMV(&cfg.Timeplus, id); err != nil {
					log.Printf("rules: toggle %s resume MV: %v", id, err)
					c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("MV resume failed: %v", err)})
					return
				}
			} else {
				catalogEntry, _ := mgr.Lookup(id)
				if err := tp.CreateRuleMV(&cfg.Timeplus, id, current.QuerySQL, catalogEntry.UDFs); err != nil {
					log.Printf("rules: toggle %s create MV: %v", id, err)
					c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("MV create failed: %v", err)})
					return
				}
			}
		} else {
			if err := tp.PauseRuleMV(&cfg.Timeplus, id); err != nil {
				log.Printf("rules: toggle %s pause MV: %v", id, err)
				c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("MV pause failed: %v", err)})
				return
			}
		}

		// MV op succeeded — now write the new enabled state.
		// Use newEnabled directly (already computed from the state read at the top
		// of this handler) instead of re-reading from Timeplus. Timeplus is an
		// append-only log with eventual consistency; reading back NOT arg_max(enabled)
		// in a rapid second toggle may see the pre-first-toggle state and flip the
		// wrong direction.
		sql := fmt.Sprintf(
			`INSERT INTO agentguard_rules (rule_id, name, version, description, query_sql, enabled, deleted, severity, category, required_telemetry, fp_rate, hook_sources, block_policy, updated_at) VALUES ('%s','%s','%s','%s','%s',%v,false,'%s','%s','%s','%s','%s','%s',now64(3))`,
			escapeSQLStr(id),
			escapeSQLStr(current.Name),
			escapeSQLStr(current.Version),
			escapeSQLStr(current.Description),
			escapeSQLStr(current.QuerySQL),
			newEnabled,
			escapeSQLStr(current.Severity),
			escapeSQLStr(current.Category),
			escapeSQLStr(current.RequiredTelemetry),
			escapeSQLStr(current.FPRate),
			escapeSQLStr(current.HookSources),
			escapeSQLStr(current.BlockPolicy),
		)

		if err := tp.ExecHTTP(&cfg.Timeplus, sql); err != nil {
			log.Printf("rules: toggle %s db update: %v", id, err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "toggle failed"})
			return
		}

		current.Enabled = newEnabled
		// Disabling a rule must also abandon any pending holds for it — the
		// operator's intent is "stop blocking," and stale holds otherwise stay
		// pending until timeout (~5 min) and re-pop after every page reload.
		if !newEnabled {
			AbandonHoldsForRule(cfg, holdReg, broadcaster, id, "rule disabled")
		}
		c.JSON(http.StatusOK, current)
	}
}

func UpgradeRule(cfg *config.Config, mgr *catalog.Manager) gin.HandlerFunc {
	return func(c *gin.Context) {
		cfg := auth.RequestCfg(c, cfg)
		id := c.Param("id")

		catalogEntry, inCatalog := mgr.Lookup(id)

		packs, err := queryCurrentRulePacks(&cfg.Timeplus, mgr)
		if err != nil {
			log.Printf("rules: UpgradeRule queryCurrentRulePacks: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "query failed"})
			return
		}
		var current *models.RulePack
		for i := range packs {
			if packs[i].ID == id {
				current = &packs[i]
				break
			}
		}
		if current == nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
			return
		}

		// Determine the target version and SQL.
		// Body query_sql takes precedence (user-provided edit); fall back to catalog.
		var body struct {
			QuerySQL string `json:"query_sql"`
		}
		_ = c.ShouldBindJSON(&body)

		targetSQL := body.QuerySQL
		targetVersion := current.Version
		if inCatalog {
			if body.QuerySQL == "" {
				targetSQL = catalogEntry.QuerySQL
			}
			targetVersion = catalogEntry.Version
		}
		if targetSQL == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "query_sql required"})
			return
		}
		if targetVersion == current.Version && body.QuerySQL == "" {
			c.JSON(http.StatusOK, gin.H{"message": "already at latest version"})
			return
		}

		escapedSQL := escapeSQLStr(targetSQL)
		insertSQL := fmt.Sprintf(`INSERT INTO agentguard_rules (rule_id, name, version, description, query_sql, enabled, deleted, severity, category, required_telemetry, fp_rate, hook_sources, block_policy, updated_at) VALUES
('%s','%s','%s','%s','%s',%v,false,'%s','%s','%s','%s','%s','%s',now64(3))`,
			id,
			escapeSQLStr(current.Name),
			targetVersion,
			escapeSQLStr(current.Description),
			escapedSQL,
			current.Enabled,
			current.Severity, current.Category,
			current.RequiredTelemetry, current.FPRate, current.HookSources,
			current.BlockPolicy,
		)
		if err := tp.ExecHTTP(&cfg.Timeplus, insertSQL); err != nil {
			log.Printf("rules: upgrade %s insert: %v", id, err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("upgrade insert failed: %v", err)})
			return
		}

		if err := tp.DropRuleMV(&cfg.Timeplus, id); err != nil {
			log.Printf("rules: upgrade %s drop MV: %v", id, err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("MV drop failed: %v", err)})
			return
		}
		if current.Enabled {
			if err := tp.CreateRuleMV(&cfg.Timeplus, id, targetSQL, catalogEntry.UDFs); err != nil {
				log.Printf("rules: upgrade %s create MV: %v", id, err)
				c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("MV create failed: %v", err)})
				return
			}
		}

		c.JSON(http.StatusOK, gin.H{"version": targetVersion})
	}
}

func UninstallRule(cfg *config.Config, mgr *catalog.Manager) gin.HandlerFunc {
	return func(c *gin.Context) {
		cfg := auth.RequestCfg(c, cfg)
		id := c.Param("id")

		packs, err := queryCurrentRulePacks(&cfg.Timeplus, mgr)
		if err != nil {
			log.Printf("rules: UninstallRule queryCurrentRulePacks: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		var current *models.RulePack
		for i := range packs {
			if packs[i].ID == id {
				current = &packs[i]
				break
			}
		}
		if current == nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "rule not found"})
			return
		}

		sql := fmt.Sprintf(
			`INSERT INTO agentguard_rules (rule_id, name, version, description, query_sql, enabled, deleted, severity, category, required_telemetry, fp_rate, hook_sources, block_policy, updated_at) VALUES ('%s','%s','%s','%s','%s',%v,true,'%s','%s','%s','%s','%s','%s',now64(3))`,
			escapeSQLStr(id),
			escapeSQLStr(current.Name),
			escapeSQLStr(current.Version),
			escapeSQLStr(current.Description),
			escapeSQLStr(current.QuerySQL),
			current.Enabled,
			escapeSQLStr(current.Severity),
			escapeSQLStr(current.Category),
			escapeSQLStr(current.RequiredTelemetry),
			escapeSQLStr(current.FPRate),
			escapeSQLStr(current.HookSources),
			escapeSQLStr(current.BlockPolicy),
		)

		if err := tp.ExecHTTP(&cfg.Timeplus, sql); err != nil {
			log.Printf("rules: UninstallRule db update: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "uninstall failed"})
			return
		}

		if err := tp.DropRuleMV(&cfg.Timeplus, id); err != nil {
			log.Printf("rules: UninstallRule DropRuleMV: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("MV drop failed: %v", err)})
			return
		}

		c.JSON(http.StatusOK, gin.H{"deleted": true})
	}
}

// ListCatalog returns all catalog entries with their install status from Proton.
func ListCatalog(cfg *config.Config, mgr *catalog.Manager) gin.HandlerFunc {
	return func(c *gin.Context) {
		cfg := auth.RequestCfg(c, cfg)
		installed, err := queryCurrentRulePacks(&cfg.Timeplus, mgr)
		if err != nil {
			log.Printf("rules: ListCatalog queryCurrentRulePacks: %v", err)
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": fmt.Sprintf("catalog query failed: %v", err)})
			return
		}
		installedMap := make(map[string]models.RulePack, len(installed))
		for _, p := range installed {
			installedMap[p.ID] = p
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
			if p, ok := installedMap[e.ID]; ok {
				item.Installed = true
				item.Enabled = p.Enabled
				item.InstalledVersion = p.Version
				item.Upgradeable = versionGreater(e.Version, p.Version)
			}
			items = append(items, item)
		}

		// Append installed rules not in the catalog (orphaned — deleted from catalog).
		for _, p := range installed {
			if !mgr.Exists(p.ID) {
				items = append(items, CatalogItem{
					ID:               p.ID,
					Name:             p.Name,
					Version:          p.Version,
					Description:      p.Description,
					Severity:         p.Severity,
					Category:         p.Category,
					FPRate:           p.FPRate,
					HookSources:      p.HookSources,
					Author:           "unknown",
					Tags:             []string{"orphaned"},
					Installed:        true,
					Enabled:          p.Enabled,
					InstalledVersion: p.Version,
				})
			}
		}

		c.JSON(http.StatusOK, gin.H{"catalog": items})
	}
}

// InstallRule installs a rule from the catalog into Proton and creates its MV.
func InstallRule(cfg *config.Config, mgr *catalog.Manager) gin.HandlerFunc {
	return func(c *gin.Context) {
		cfg := auth.RequestCfg(c, cfg)
		id := c.Param("id")

		entry, ok := mgr.Lookup(id)
		if !ok {
			c.JSON(http.StatusNotFound, gin.H{"error": "rule not found in catalog"})
			return
		}

		escapedSQL := escapeSQLStr(entry.QuerySQL)
		insertSQL := fmt.Sprintf(
			`INSERT INTO agentguard_rules (rule_id, name, version, description, query_sql, enabled, deleted, severity, category, required_telemetry, fp_rate, hook_sources, block_policy, updated_at) VALUES ('%s','%s','%s','%s','%s',true,false,'%s','%s','%s','%s','%s','log_only',now64(3))`,
			id,
			escapeSQLStr(entry.Name),
			entry.Version,
			escapeSQLStr(entry.Description),
			escapedSQL,
			entry.Severity, entry.Category,
			entry.RequiredTelemetry, entry.FPRate, entry.HookSources,
		)
		if err := tp.ExecHTTP(&cfg.Timeplus, insertSQL); err != nil {
			log.Printf("rules: InstallRule insert: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "install insert failed"})
			return
		}
		if err := tp.CreateRuleMV(&cfg.Timeplus, id, entry.QuerySQL, entry.UDFs); err != nil {
			log.Printf("rules: InstallRule CreateRuleMV: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("MV create failed: %v", err)})
			return
		}
		c.JSON(http.StatusOK, gin.H{"installed": true, "rule_id": id})
	}
}

// MVStatus returns the existence and enabled state of every rule's MV.
func MVStatus(cfg *config.Config, mgr *catalog.Manager) gin.HandlerFunc {
	return func(c *gin.Context) {
		cfg := auth.RequestCfg(c, cfg)
		packs, err := queryCurrentRulePacks(&cfg.Timeplus, mgr)
		if err != nil {
			log.Printf("rules: MVStatus queryCurrentRulePacks: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		rows, err := tp.Query(&cfg.Timeplus, `
			SELECT name FROM system.tables
			WHERE name LIKE 'mv_rule_%' AND database = current_database()
		`)
		if err != nil {
			log.Printf("rules: MVStatus system.tables query: %v", err)
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": err.Error()})
			return
		}
		existing := map[string]bool{}
		for _, row := range rows {
			if name, ok := row["name"].(string); ok {
				existing[name] = true
			}
		}

		type MVInfo struct {
			RuleID      string `json:"rule_id"`
			MVName      string `json:"mv_name"`
			RuleEnabled bool   `json:"rule_enabled"`
			MVExists    bool   `json:"mv_exists"`
			Healthy     bool   `json:"healthy"`
		}
		result := make([]MVInfo, 0, len(packs))
		for _, p := range packs {
			mvName := tp.MVName(p.ID)
			mvExists := existing[mvName]
			result = append(result, MVInfo{
				RuleID:      p.ID,
				MVName:      mvName,
				RuleEnabled: p.Enabled,
				MVExists:    mvExists,
				Healthy:     !p.Enabled || mvExists,
			})
		}
		c.JSON(http.StatusOK, gin.H{"mvs": result})
	}
}

// SyncMVs reconciles Proton MVs with current rule states without a server restart.
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

func SyncMVs(cfg *config.Config, mgr *catalog.Manager) gin.HandlerFunc {
	shared := cfg // process-wide config, for persisting the DLP provision snapshot
	return func(c *gin.Context) {
		cfg := auth.RequestCfg(c, cfg)
		packs, err := queryCurrentRulePacks(&cfg.Timeplus, mgr)
		if err != nil {
			log.Printf("rules: SyncMVs queryCurrentRulePacks: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		rows, err := tp.Query(&cfg.Timeplus, `
			SELECT name FROM system.tables
			WHERE name LIKE 'mv_rule_%' AND database = current_database()
		`)
		if err != nil {
			log.Printf("rules: SyncMVs system.tables query: %v", err)
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": err.Error()})
			return
		}
		existing := map[string]bool{}
		for _, row := range rows {
			if name, ok := row["name"].(string); ok {
				existing[name] = true
			}
		}

		type SyncResult struct {
			RuleID string `json:"rule_id"`
			Action string `json:"action"`
			Error  string `json:"error,omitempty"`
		}
		results := make([]SyncResult, 0, len(packs))

		for _, p := range packs {
			mvName := tp.MVName(p.ID)
			exists := existing[mvName]
			res := SyncResult{RuleID: p.ID}

			if p.Enabled {
				if !exists {
					catalogEntry, _ := mgr.Lookup(p.ID)
					if err := tp.CreateRuleMV(&cfg.Timeplus, p.ID, p.QuerySQL, catalogEntry.UDFs); err != nil {
						res.Action = "create_failed"
						res.Error = err.Error()
					} else {
						res.Action = "created"
					}
				} else {
					if err := tp.ResumeRuleMV(&cfg.Timeplus, p.ID); err != nil {
						res.Action = "resume_failed"
						res.Error = err.Error()
					} else {
						res.Action = "resumed"
					}
				}
			} else if exists {
				if err := tp.PauseRuleMV(&cfg.Timeplus, p.ID); err != nil {
					res.Action = "pause_failed"
					res.Error = err.Error()
				} else {
					res.Action = "paused"
				}
			} else {
				res.Action = "ok"
			}
			results = append(results, res)
		}

		// Best-effort DLP re-provision so endpoint/model config changes apply
		// without the setup wizard. Failure must not fail rule-MV sync.
		if err := tp.SyncDLP(cfg.Timeplus, cfg.DLP); err != nil {
			log.Printf("rules: SyncMVs DLP sync: %v", err)
		} else {
			// Probe the model for the embedding dimension and record what we just
			// provisioned, so GET /api/config reflects the auto-detected dim.
			detectedDim, _ := tp.ProbeEmbeddingDim(&cfg.Timeplus)
			recordDLPProvisioned(shared, cfg.DLP, detectedDim)
		}

		c.JSON(http.StatusOK, gin.H{"results": results})
	}
}

// isThreatAlreadyOpen queries agentguard_threats to check whether an open
// threat already exists for the given (agent_id, deployment_id, rule_id)
// combination. When deployment_id is empty the check is by (agent_id, rule_id)
// alone. Returns false on any error so the caller fails open.
func isThreatAlreadyOpen(cfg *config.Config, agentID, deploymentID, ruleID string) bool {
	if agentID == "" || ruleID == "" {
		return false
	}
	var sql string
	if deploymentID != "" {
		sql = fmt.Sprintf(
			`SELECT count() AS cnt FROM table(agentguard_threats) WHERE agent_id = '%s' AND deployment_id = '%s' AND rule_id = '%s' AND (status = '' OR status = 'open')`,
			escapeSQL(agentID), escapeSQL(deploymentID), escapeSQL(ruleID),
		)
	} else {
		sql = fmt.Sprintf(
			`SELECT count() AS cnt FROM table(agentguard_threats) WHERE agent_id = '%s' AND rule_id = '%s' AND (status = '' OR status = 'open')`,
			escapeSQL(agentID), escapeSQL(ruleID),
		)
	}
	rows, err := tp.Query(&cfg.Timeplus, sql)
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

// CreateRule creates a new rule from user-provided data, saves it to the catalog,
// inserts it into Proton, and creates its MV.
func CreateRule(cfg *config.Config, mgr *catalog.Manager) gin.HandlerFunc {
	return func(c *gin.Context) {
		cfg := auth.RequestCfg(c, cfg)
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

		// Determine rule ID: use provided or auto-generate.
		ruleID := body.ID
		if ruleID == "" {
			ruleID = fmt.Sprintf("custom-%d", time.Now().UnixNano())
		}
		if err := mgr.ValidateID(ruleID); err != nil {
			c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
			return
		}

		// Substitute template placeholders in SQL.
		escapedName := strings.ReplaceAll(body.Name, "'", "''")
		finalSQL := strings.ReplaceAll(body.QuerySQL, "'__rule_id__'", "'"+ruleID+"'")
		finalSQL = strings.ReplaceAll(finalSQL, "'__rule_name__'", "'"+escapedName+"'")

		entry := catalog.Entry{
			ID:          ruleID,
			Name:        body.Name,
			Version:     "1.0.0",
			Description: body.Description,
			QuerySQL:    finalSQL,
			Severity:    body.Severity,
			Category:    body.Category,
			FPRate:      body.FPRate,
			HookSources: body.HookSources,
			Author:      "custom",
			Tags:        []string{},
			UDFs:        body.UDFs,
		}

		// Persist to catalog first.
		if err := mgr.Save(entry); err != nil {
			log.Printf("rules: save catalog entry %s: %v", ruleID, err)
			// Non-fatal — continue to insert into Proton.
		}

		escapedSQL := escapeSQLStr(finalSQL)
		insertSQL := fmt.Sprintf(
			`INSERT INTO agentguard_rules (rule_id, name, version, description, query_sql, enabled, deleted, severity, category, required_telemetry, fp_rate, hook_sources, block_policy, updated_at) VALUES ('%s','%s','1.0.0','%s','%s',true,false,'%s','%s','','%s','%s','log_only',now64(3))`,
			ruleID,
			escapeSQLStr(body.Name),
			escapeSQLStr(body.Description),
			escapedSQL,
			escapeSQLStr(body.Severity),
			escapeSQLStr(body.Category),
			escapeSQLStr(body.FPRate),
			escapeSQLStr(body.HookSources),
		)
		if err := tp.ExecHTTP(&cfg.Timeplus, insertSQL); err != nil {
			log.Printf("rules: create rule insert: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "insert failed"})
			return
		}

		if err := tp.CreateRuleMV(&cfg.Timeplus, ruleID, finalSQL, body.UDFs); err != nil {
			log.Printf("rules: CreateRule CreateRuleMV %s: %v", ruleID, err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("MV create failed: %v", err)})
			return
		}

		c.JSON(http.StatusCreated, models.RulePack{
			ID:          ruleID,
			Name:        body.Name,
			Version:     "1.0.0",
			Description: body.Description,
			QuerySQL:    finalSQL,
			Enabled:     true,
			Severity:    body.Severity,
			Category:    body.Category,
			FPRate:      body.FPRate,
			HookSources: body.HookSources,
		})
	}
}

// ── Helpers ───────────────────────────────────────────────────────────────────

func queryCurrentRulePacks(cfg *config.TimeplusConfig, mgr *catalog.Manager) ([]models.RulePack, error) {
	rows, err := tp.Query(cfg, sqlCurrentRules)
	if err != nil {
		return nil, err
	}
	packs := make([]models.RulePack, 0, len(rows))
	for _, row := range rows {
		id := parseStr(row["rule_id"])
		enabled := parseBool(row["enabled"])
		pack := models.RulePack{
			ID:                id,
			Name:              parseStr(row["name"]),
			Version:           parseStr(row["version"]),
			Description:       parseStr(row["description"]),
			QuerySQL:          parseStr(row["query_sql"]),
			Enabled:           enabled,
			Severity:          parseStr(row["severity"]),
			Category:          parseStr(row["category"]),
			RequiredTelemetry: parseStr(row["required_telemetry"]),
			FPRate:            parseStr(row["fp_rate"]),
			HookSources:       parseStr(row["hook_sources"]),
			BlockPolicy:       parseStr(row["block_policy"]),
		}
		if pack.BlockPolicy == "" {
			pack.BlockPolicy = "log_only"
		}
		// Enrich from catalog: newer version and UDFs.
		if catalogEntry, ok := mgr.Lookup(id); ok {
			if versionGreater(catalogEntry.Version, pack.Version) {
				pack.LatestVersion = catalogEntry.Version
			}
			pack.UDFs = catalogEntry.UDFs
		}
		packs = append(packs, pack)
	}
	return packs, nil
}

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
		ruleID := parseStr(row["rule_id"])
		if num, ok := row["cnt"].(json.Number); ok {
			cnt, _ := num.Int64()
			result[ruleID] = int(cnt)
		}
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

func mvExists(cfg *config.TimeplusConfig, ruleID string) (bool, error) {
	mvName := tp.MVName(ruleID)
	rows, err := tp.Query(cfg, fmt.Sprintf(
		"SELECT count() AS cnt FROM system.tables WHERE name = '%s' AND database = current_database()",
		escapeSQL(mvName),
	))
	if err != nil || len(rows) == 0 {
		return false, err
	}
	if num, ok := rows[0]["cnt"].(json.Number); ok {
		cnt, _ := num.Int64()
		return cnt > 0, nil
	}
	return false, nil
}

// SetRuleBlockPolicy handles POST /api/rules/:id/policy
// Body: {"block_policy": "hold" | "auto_block" | "log_only"}
func SetRuleBlockPolicy(cfg *config.Config, mgr *catalog.Manager, holdReg *holdregistry.Registry, broadcaster *alertBroadcaster) gin.HandlerFunc {
	return func(c *gin.Context) {
		cfg := auth.RequestCfg(c, cfg)
		id := c.Param("id")
		var body struct {
			BlockPolicy string `json:"block_policy"`
		}
		if err := c.ShouldBindJSON(&body); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body"})
			return
		}
		switch body.BlockPolicy {
		case "hold", "auto_block", "log_only":
			// ok
		default:
			c.JSON(http.StatusBadRequest, gin.H{"error": "block_policy must be hold|auto_block|log_only"})
			return
		}

		packs, err := queryCurrentRulePacks(&cfg.Timeplus, mgr)
		if err != nil {
			log.Printf("rules: SetRuleBlockPolicy queryCurrentRulePacks: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "query failed"})
			return
		}
		var current *models.RulePack
		for i := range packs {
			if packs[i].ID == id {
				current = &packs[i]
				break
			}
		}
		if current == nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "rule not found"})
			return
		}

		sql := fmt.Sprintf(
			`INSERT INTO agentguard_rules (rule_id, name, version, description, query_sql, enabled, deleted, severity, category, required_telemetry, fp_rate, hook_sources, block_policy, updated_at) VALUES ('%s','%s','%s','%s','%s',%v,false,'%s','%s','%s','%s','%s','%s',now64(3))`,
			escapeSQLStr(id),
			escapeSQLStr(current.Name),
			escapeSQLStr(current.Version),
			escapeSQLStr(current.Description),
			escapeSQLStr(current.QuerySQL),
			current.Enabled,
			escapeSQLStr(current.Severity),
			escapeSQLStr(current.Category),
			escapeSQLStr(current.RequiredTelemetry),
			escapeSQLStr(current.FPRate),
			escapeSQLStr(current.HookSources),
			escapeSQLStr(body.BlockPolicy),
		)

		if err := tp.ExecHTTP(&cfg.Timeplus, sql); err != nil {
			log.Printf("rules: SetRuleBlockPolicy ExecHTTP: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("set policy failed: %v", err)})
			return
		}

		current.BlockPolicy = body.BlockPolicy
		// Switching to log_only must abandon any pending holds — those holds
		// were created under the old policy but the operator has now declared
		// "this rule shouldn't block anything." The next tool call won't trigger
		// a hold (queryPreExistingOpenThreats and waitForNewMVDetections both
		// skip log_only) but the in-flight ones would otherwise stay pending
		// until timeout.
		if body.BlockPolicy == "log_only" {
			AbandonHoldsForRule(cfg, holdReg, broadcaster, id, "rule policy changed to log_only")
		}
		c.JSON(http.StatusOK, current)
	}
}

// versionGreater returns true if version string a is strictly greater than b.
func versionGreater(a, b string) bool {
	aParts := strings.SplitN(a, ".", 3)
	bParts := strings.SplitN(b, ".", 3)
	for i := 0; i < 3; i++ {
		var av, bv int
		if i < len(aParts) {
			_, _ = fmt.Sscanf(aParts[i], "%d", &av)
		}
		if i < len(bParts) {
			_, _ = fmt.Sscanf(bParts[i], "%d", &bv)
		}
		if av > bv {
			return true
		}
		if av < bv {
			return false
		}
	}
	return false
}
