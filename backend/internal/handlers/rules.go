package handlers

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/timeplus/agentshield/internal/catalog"
	"github.com/timeplus/agentshield/internal/config"
	"github.com/timeplus/agentshield/internal/models"
	tp "github.com/timeplus/agentshield/internal/timeplus"
)

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
FROM table(agentshield_rules)
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
  arg_max(hook_sources,       updated_at) AS hook_sources
FROM table(agentshield_rules)
GROUP BY rule_id
HAVING arg_max(deleted, updated_at) = false`

func ListRules(cfg *config.Config, mgr *catalog.Manager) gin.HandlerFunc {
	return func(c *gin.Context) {
		packs, err := queryCurrentRulePacks(&cfg.Timeplus, mgr)
		if err != nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": err.Error()})
			return
		}

		if counts, err := queryDetectionCounts(&cfg.Timeplus); err == nil {
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
		id := c.Param("id")

		packs, err := queryCurrentRulePacks(&cfg.Timeplus, mgr)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "query failed"})
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
			FROM table(agentshield_security_events)
			WHERE rule_id = '%s'
			  AND detected_at >= now() - interval 24 hour
		`, escapeSQL(id)))
		if err == nil && len(countRows) > 0 {
			if num, ok := countRows[0]["cnt"].(json.Number); ok {
				cnt, _ := num.Int64()
				pack.Detections24h = int(cnt)
			}
		}

		recentRows, err := tp.Query(&cfg.Timeplus, fmt.Sprintf(`
			SELECT rule_id, rule_name, severity, agent_id, hook_name, message, detected_at
			FROM table(agentshield_security_events)
			WHERE rule_id = '%s'
			ORDER BY detected_at DESC
			LIMIT 50
		`, escapeSQL(id)))
		if err != nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": err.Error()})
			return
		}

		c.JSON(http.StatusOK, models.RuleDetailResponse{
			RulePack:         *pack,
			RecentDetections: rowsToSecurityEvents(recentRows),
		})
	}
}

func ToggleRule(cfg *config.Config, mgr *catalog.Manager) gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.Param("id")

		packs, err := queryCurrentRulePacks(&cfg.Timeplus, mgr)
		if err != nil {
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

		sql := fmt.Sprintf(`
INSERT INTO agentshield_rules (rule_id, name, version, description, query_sql, enabled, deleted, severity, category, required_telemetry, fp_rate, hook_sources, updated_at)
SELECT
  rule_id,
  arg_max(name,               updated_at) AS name,
  arg_max(version,            updated_at) AS version,
  arg_max(description,        updated_at) AS description,
  arg_max(query_sql,          updated_at) AS query_sql,
  NOT arg_max(enabled,        updated_at) AS enabled,
  false                                   AS deleted,
  arg_max(severity,           updated_at) AS severity,
  arg_max(category,           updated_at) AS category,
  arg_max(required_telemetry, updated_at) AS required_telemetry,
  arg_max(fp_rate,            updated_at) AS fp_rate,
  arg_max(hook_sources,       updated_at) AS hook_sources,
  now64(3)                                AS updated_at
FROM table(agentshield_rules)
WHERE rule_id = '%s'
GROUP BY rule_id`, id)

		if err := tp.ExecHTTP(&cfg.Timeplus, sql); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "toggle failed"})
			return
		}

		newEnabled := !current.Enabled
		if newEnabled {
			exists, err := mvExists(&cfg.Timeplus, id)
			if err != nil {
				c.JSON(http.StatusServiceUnavailable, gin.H{"error": "MV status check failed"})
				return
			}
			if exists {
				if err := tp.ResumeRuleMV(&cfg.Timeplus, id); err != nil {
					log.Printf("rules: toggle resume MV %s: %v", id, err)
				}
			} else {
				if err := tp.CreateRuleMV(&cfg.Timeplus, id, current.QuerySQL); err != nil {
					log.Printf("rules: toggle create MV %s: %v", id, err)
				}
			}
		} else {
			if err := tp.PauseRuleMV(&cfg.Timeplus, id); err != nil {
				log.Printf("rules: toggle pause MV %s: %v", id, err)
			}
		}

		current.Enabled = newEnabled
		c.JSON(http.StatusOK, current)
	}
}

func UpgradeRule(cfg *config.Config, mgr *catalog.Manager) gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.Param("id")

		catalogEntry, inCatalog := mgr.Lookup(id)

		packs, err := queryCurrentRulePacks(&cfg.Timeplus, mgr)
		if err != nil {
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

		escapedSQL := strings.ReplaceAll(targetSQL, "'", "''")
		insertSQL := fmt.Sprintf(`INSERT INTO agentshield_rules (rule_id, name, version, description, query_sql, enabled, deleted, severity, category, required_telemetry, fp_rate, hook_sources, updated_at) VALUES
('%s','%s','%s','%s','%s',%v,false,'%s','%s','%s','%s','%s',now64(3))`,
			id,
			strings.ReplaceAll(current.Name, "'", "''"),
			targetVersion,
			strings.ReplaceAll(current.Description, "'", "''"),
			escapedSQL,
			current.Enabled,
			current.Severity, current.Category,
			current.RequiredTelemetry, current.FPRate, current.HookSources,
		)
		if err := tp.ExecHTTP(&cfg.Timeplus, insertSQL); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "upgrade insert failed"})
			return
		}

		if err := tp.DropRuleMV(&cfg.Timeplus, id); err != nil {
			log.Printf("rules: upgrade drop MV %s: %v", id, err)
		}
		if current.Enabled {
			if err := tp.CreateRuleMV(&cfg.Timeplus, id, targetSQL); err != nil {
				log.Printf("rules: upgrade create MV %s: %v", id, err)
			}
		}

		c.JSON(http.StatusOK, gin.H{"version": targetVersion})
	}
}

func UninstallRule(cfg *config.Config, mgr *catalog.Manager) gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.Param("id")

		packs, err := queryCurrentRulePacks(&cfg.Timeplus, mgr)
		if err != nil {
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

		sql := fmt.Sprintf(`
INSERT INTO agentshield_rules (rule_id, name, version, description, query_sql, enabled, deleted, severity, category, required_telemetry, fp_rate, hook_sources, updated_at)
SELECT
  rule_id,
  arg_max(name,               updated_at) AS name,
  arg_max(version,            updated_at) AS version,
  arg_max(description,        updated_at) AS description,
  arg_max(query_sql,          updated_at) AS query_sql,
  arg_max(enabled,            updated_at) AS enabled,
  true                                    AS deleted,
  arg_max(severity,           updated_at) AS severity,
  arg_max(category,           updated_at) AS category,
  arg_max(required_telemetry, updated_at) AS required_telemetry,
  arg_max(fp_rate,            updated_at) AS fp_rate,
  arg_max(hook_sources,       updated_at) AS hook_sources,
  now64(3)                                AS updated_at
FROM table(agentshield_rules)
WHERE rule_id = '%s'
GROUP BY rule_id`, escapeSQL(id))

		if err := tp.ExecHTTP(&cfg.Timeplus, sql); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "uninstall failed"})
			return
		}

		if err := tp.DropRuleMV(&cfg.Timeplus, id); err != nil {
			log.Printf("rules: uninstall drop MV %s: %v", id, err)
		}

		c.JSON(http.StatusOK, gin.H{"deleted": true})
	}
}

// ListCatalog returns all catalog entries with their install status from Proton.
func ListCatalog(cfg *config.Config, mgr *catalog.Manager) gin.HandlerFunc {
	return func(c *gin.Context) {
		installed, err := queryCurrentRulePacks(&cfg.Timeplus, mgr)
		if err != nil {
			log.Printf("rules: catalog query failed: %v", err)
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
		id := c.Param("id")

		entry, ok := mgr.Lookup(id)
		if !ok {
			c.JSON(http.StatusNotFound, gin.H{"error": "rule not found in catalog"})
			return
		}

		escapedSQL := strings.ReplaceAll(entry.QuerySQL, "'", "''")
		insertSQL := fmt.Sprintf(
			`INSERT INTO agentshield_rules (rule_id, name, version, description, query_sql, enabled, deleted, severity, category, required_telemetry, fp_rate, hook_sources, updated_at) VALUES ('%s','%s','%s','%s','%s',true,false,'%s','%s','%s','%s','%s',now64(3))`,
			id,
			strings.ReplaceAll(entry.Name, "'", "''"),
			entry.Version,
			strings.ReplaceAll(entry.Description, "'", "''"),
			escapedSQL,
			entry.Severity, entry.Category,
			entry.RequiredTelemetry, entry.FPRate, entry.HookSources,
		)
		if err := tp.ExecHTTP(&cfg.Timeplus, insertSQL); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "install insert failed"})
			return
		}
		if err := tp.CreateRuleMV(&cfg.Timeplus, id, entry.QuerySQL); err != nil {
			log.Printf("rules: install create MV %s: %v", id, err)
		}
		c.JSON(http.StatusOK, gin.H{"installed": true, "rule_id": id})
	}
}

// MVStatus returns the existence and enabled state of every rule's MV.
func MVStatus(cfg *config.Config, mgr *catalog.Manager) gin.HandlerFunc {
	return func(c *gin.Context) {
		packs, err := queryCurrentRulePacks(&cfg.Timeplus, mgr)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "query failed"})
			return
		}

		rows, err := tp.Query(&cfg.Timeplus, `
			SELECT name FROM system.tables
			WHERE name LIKE 'mv_rule_%' AND database = current_database()
		`)
		if err != nil {
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
func SyncMVs(cfg *config.Config, mgr *catalog.Manager) gin.HandlerFunc {
	return func(c *gin.Context) {
		packs, err := queryCurrentRulePacks(&cfg.Timeplus, mgr)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "query failed"})
			return
		}

		rows, err := tp.Query(&cfg.Timeplus, `
			SELECT name FROM system.tables
			WHERE name LIKE 'mv_rule_%' AND database = current_database()
		`)
		if err != nil {
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
					if err := tp.CreateRuleMV(&cfg.Timeplus, p.ID, p.QuerySQL); err != nil {
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

		c.JSON(http.StatusOK, gin.H{"results": results})
	}
}

func AlertWebhook(cfg *config.Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		var payload models.AlertWebhookPayload
		if err := c.ShouldBindJSON(&payload); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid payload"})
			return
		}
		log.Printf("alert: rule=%s severity=%s agent=%s msg=%s",
			payload.RuleID, payload.Severity, payload.AgentID, payload.Message)
		GlobalAlertBroadcaster.Publish(payload)
		c.JSON(http.StatusOK, gin.H{"received": true})
	}
}

// CreateRule creates a new rule from user-provided data, saves it to the catalog,
// inserts it into Proton, and creates its MV.
func CreateRule(cfg *config.Config, mgr *catalog.Manager) gin.HandlerFunc {
	return func(c *gin.Context) {
		var body struct {
			ID          string `json:"id"`
			Name        string `json:"name"`
			Description string `json:"description"`
			QuerySQL    string `json:"query_sql"`
			Severity    string `json:"severity"`
			Category    string `json:"category"`
			FPRate      string `json:"fp_rate"`
			HookSources string `json:"hook_sources"`
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
		}

		// Persist to catalog first.
		if err := mgr.Save(entry); err != nil {
			log.Printf("rules: save catalog entry %s: %v", ruleID, err)
			// Non-fatal — continue to insert into Proton.
		}

		escapedSQL := strings.ReplaceAll(finalSQL, "'", "''")
		insertSQL := fmt.Sprintf(
			`INSERT INTO agentshield_rules (rule_id, name, version, description, query_sql, enabled, deleted, severity, category, required_telemetry, fp_rate, hook_sources, updated_at) VALUES ('%s','%s','1.0.0','%s','%s',true,false,'%s','%s','','%s','%s',now64(3))`,
			ruleID,
			escapedName,
			strings.ReplaceAll(body.Description, "'", "''"),
			escapedSQL,
			strings.ReplaceAll(body.Severity, "'", "''"),
			strings.ReplaceAll(body.Category, "'", "''"),
			strings.ReplaceAll(body.FPRate, "'", "''"),
			strings.ReplaceAll(body.HookSources, "'", "''"),
		)
		if err := tp.ExecHTTP(&cfg.Timeplus, insertSQL); err != nil {
			log.Printf("rules: create rule insert: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "insert failed"})
			return
		}

		if err := tp.CreateRuleMV(&cfg.Timeplus, ruleID, finalSQL); err != nil {
			log.Printf("rules: create custom MV %s: %v", ruleID, err)
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
		id := strVal(row, "rule_id")
		enabled := boolVal(row, "enabled")
		pack := models.RulePack{
			ID:                id,
			Name:              strVal(row, "name"),
			Version:           strVal(row, "version"),
			Description:       strVal(row, "description"),
			QuerySQL:          strVal(row, "query_sql"),
			Enabled:           enabled,
			Severity:          strVal(row, "severity"),
			Category:          strVal(row, "category"),
			RequiredTelemetry: strVal(row, "required_telemetry"),
			FPRate:            strVal(row, "fp_rate"),
			HookSources:       strVal(row, "hook_sources"),
		}
		// Check catalog for a newer version.
		if catalogEntry, ok := mgr.Lookup(id); ok {
			if versionGreater(catalogEntry.Version, pack.Version) {
				pack.LatestVersion = catalogEntry.Version
			}
		}
		packs = append(packs, pack)
	}
	return packs, nil
}

func queryDetectionCounts(cfg *config.TimeplusConfig) (map[string]int, error) {
	rows, err := tp.Query(cfg, `
		SELECT rule_id, count() AS cnt
		FROM table(agentshield_security_events)
		WHERE detected_at >= now() - interval 24 hour
		GROUP BY rule_id
	`)
	if err != nil {
		return nil, err
	}
	result := make(map[string]int, len(rows))
	for _, row := range rows {
		ruleID := strVal(row, "rule_id")
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
		FROM table(agentshield_security_events)
		ORDER BY detected_at DESC
		LIMIT 10
	`)
	if err != nil || len(rows) == 0 {
		return nil
	}
	return rowsToSecurityEvents(rows)
}

func rowsToSecurityEvents(rows []tp.Row) []models.SecurityEvent {
	events := make([]models.SecurityEvent, 0, len(rows))
	for _, row := range rows {
		events = append(events, models.SecurityEvent{
			RuleID:     strVal(row, "rule_id"),
			RuleName:   strVal(row, "rule_name"),
			Severity:   strVal(row, "severity"),
			AgentID:    strVal(row, "agent_id"),
			HookName:   strVal(row, "hook_name"),
			Message:    strVal(row, "message"),
			DetectedAt: strVal(row, "detected_at"),
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

// versionGreater returns true if version string a is strictly greater than b.
func versionGreater(a, b string) bool {
	aParts := strings.SplitN(a, ".", 3)
	bParts := strings.SplitN(b, ".", 3)
	for i := 0; i < 3; i++ {
		var av, bv int
		if i < len(aParts) {
			fmt.Sscanf(aParts[i], "%d", &av)
		}
		if i < len(bParts) {
			fmt.Sscanf(bParts[i], "%d", &bv)
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

func strVal(row map[string]any, key string) string {
	v, _ := row[key].(string)
	return v
}

func boolVal(row map[string]any, key string) bool {
	switch v := row[key].(type) {
	case bool:
		return v
	case json.Number:
		i, _ := v.Int64()
		return i != 0
	}
	return false
}

