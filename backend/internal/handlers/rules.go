package handlers

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/timeplus/agentshield/internal/config"
	"github.com/timeplus/agentshield/internal/models"
	tp "github.com/timeplus/agentshield/internal/timeplus"
)

// latestVersions is the catalog of the latest built-in rule versions.
// If the live version differs, the UI shows an "Upgrade" button.
var latestVersions = map[string]string{
	"rp-001": "1.0.0",
	"rp-002": "1.0.0",
	"rp-003": "1.0.0",
	"rp-004": "1.0.0",
}

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

func ListRules(cfg *config.Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		packs, err := queryCurrentRulePacks(&cfg.Timeplus)
		if err != nil {
			log.Printf("rules: list query failed: %v", err)
			packs = []models.RulePack{}
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

func GetRule(cfg *config.Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.Param("id")

		packs, err := queryCurrentRulePacks(&cfg.Timeplus)
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
		`, id))
		if err == nil && len(countRows) > 0 {
			if num, ok := countRows[0]["cnt"].(json.Number); ok {
				cnt, _ := num.Int64()
				pack.Detections24h = int(cnt)
			}
		}

		recentRows, _ := tp.Query(&cfg.Timeplus, fmt.Sprintf(`
			SELECT rule_id, rule_name, severity, agent_id, hook_name, message, detected_at
			FROM table(agentshield_security_events)
			WHERE rule_id = '%s'
			ORDER BY detected_at DESC
			LIMIT 50
		`, id))

		c.JSON(http.StatusOK, models.RuleDetailResponse{
			RulePack:         *pack,
			RecentDetections: rowsToSecurityEvents(recentRows),
		})
	}
}

func ToggleRule(cfg *config.Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.Param("id")

		packs, err := queryCurrentRulePacks(&cfg.Timeplus)
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

		// Flip enabled via INSERT ... SELECT — reads current state and flips it atomically
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
			exists, _ := mvExists(&cfg.Timeplus, id)
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

func UpgradeRule(cfg *config.Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.Param("id")

		latestVer, ok := latestVersions[id]
		if !ok {
			c.JSON(http.StatusBadRequest, gin.H{"error": "no upgrade available"})
			return
		}

		packs, err := queryCurrentRulePacks(&cfg.Timeplus)
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
		if current.Version == latestVer {
			c.JSON(http.StatusOK, gin.H{"message": "already at latest version"})
			return
		}

		var body struct {
			QuerySQL string `json:"query_sql"`
		}
		if err := c.ShouldBindJSON(&body); err != nil || body.QuerySQL == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "query_sql required in body"})
			return
		}

		escapedSQL := strings.ReplaceAll(body.QuerySQL, "'", "''")
		insertSQL := fmt.Sprintf(`INSERT INTO agentshield_rules (rule_id, name, version, description, query_sql, enabled, deleted, severity, category, required_telemetry, fp_rate, hook_sources, updated_at) VALUES
('%s','%s','%s','%s','%s',%v,false,'%s','%s','%s','%s','%s',now64(3))`,
			id,
			strings.ReplaceAll(current.Name, "'", "''"),
			latestVer,
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
			if err := tp.CreateRuleMV(&cfg.Timeplus, id, body.QuerySQL); err != nil {
				log.Printf("rules: upgrade create MV %s: %v", id, err)
			}
		}

		c.JSON(http.StatusOK, gin.H{"version": latestVer})
	}
}

func UninstallRule(cfg *config.Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.Param("id")

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
GROUP BY rule_id`, id)

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

// ListCatalog returns the builtin rule catalog with each entry's installed status.
func ListCatalog(cfg *config.Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		installed, err := queryCurrentRulePacks(&cfg.Timeplus)
		if err != nil {
			log.Printf("rules: catalog query failed: %v", err)
		}
		installedIDs := map[string]bool{}
		for _, p := range installed {
			installedIDs[p.ID] = true
		}

		type CatalogItem struct {
			ID          string `json:"id"`
			Name        string `json:"name"`
			Version     string `json:"version"`
			Description string `json:"description"`
			Severity    string `json:"severity"`
			Category    string `json:"category"`
			FPRate      string `json:"fp_rate"`
			HookSources string `json:"hook_sources"`
			Installed   bool   `json:"installed"`
		}
		catalog := tp.Catalog()
		items := make([]CatalogItem, 0, len(catalog))
		for _, e := range catalog {
			items = append(items, CatalogItem{
				ID:          e.ID,
				Name:        e.Name,
				Version:     e.Version,
				Description: e.Description,
				Severity:    e.Severity,
				Category:    e.Category,
				FPRate:      e.FPRate,
				HookSources: e.HookSources,
				Installed:   installedIDs[e.ID],
			})
		}
		c.JSON(http.StatusOK, gin.H{"catalog": items})
	}
}

// ReinstallRule re-inserts a previously deleted (uninstalled) rule and creates its MV.
// It reads the original data from the builtin catalog — so it always reinstalls
// the canonical version, not whatever custom SQL may have been in the stream.
func ReinstallRule(cfg *config.Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.Param("id")

		// Find in builtin catalog
		var found *tp.CatalogEntry
		for _, e := range tp.Catalog() {
			if e.ID == id {
				entry := e
				found = &entry
				break
			}
		}
		if found == nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "not in builtin catalog"})
			return
		}

		escapedSQL := strings.ReplaceAll(found.QuerySQL, "'", "''")
		insertSQL := fmt.Sprintf(
			`INSERT INTO agentshield_rules (rule_id, name, version, description, query_sql, enabled, deleted, severity, category, required_telemetry, fp_rate, hook_sources, updated_at) VALUES ('%s','%s','%s','%s','%s',true,false,'%s','%s','%s','%s','%s',now64(3))`,
			id,
			strings.ReplaceAll(found.Name, "'", "''"),
			found.Version,
			strings.ReplaceAll(found.Description, "'", "''"),
			escapedSQL,
			found.Severity, found.Category, "",
			found.FPRate, found.HookSources,
		)
		if err := tp.ExecHTTP(&cfg.Timeplus, insertSQL); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "reinstall insert failed"})
			return
		}

		if err := tp.CreateRuleMV(&cfg.Timeplus, id, found.QuerySQL); err != nil {
			log.Printf("rules: reinstall create MV %s: %v", id, err)
		}

		c.JSON(http.StatusOK, gin.H{"reinstalled": true, "rule_id": id})
	}
}

// MVStatus returns the existence and enabled state of every rule's MV.
func MVStatus(cfg *config.Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		packs, err := queryCurrentRulePacks(&cfg.Timeplus)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "query failed"})
			return
		}

		// Query all mv_rule_* views from system.tables
		rows, _ := tp.Query(&cfg.Timeplus, `
			SELECT name FROM system.tables
			WHERE name LIKE 'mv_rule_%' AND database = current_database()
		`)
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
				Healthy:     !p.Enabled || mvExists, // disabled rules don't need an MV
			})
		}
		c.JSON(http.StatusOK, gin.H{"mvs": result})
	}
}

// SyncMVs reconciles Proton MVs with the current rule enabled states without
// requiring a server restart. Equivalent to the startupSyncMVs init step.
func SyncMVs(cfg *config.Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		packs, err := queryCurrentRulePacks(&cfg.Timeplus)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "query failed"})
			return
		}

		// Query existing MVs
		rows, _ := tp.Query(&cfg.Timeplus, `
			SELECT name FROM system.tables
			WHERE name LIKE 'mv_rule_%' AND database = current_database()
		`)
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
		// Future: fan out to Slack/PagerDuty via settings integrations
		c.JSON(http.StatusOK, gin.H{"received": true})
	}
}

// ── Helpers ───────────────────────────────────────────────────────────────────

func queryCurrentRulePacks(cfg *config.TimeplusConfig) ([]models.RulePack, error) {
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
		if latest, ok := latestVersions[id]; ok && latest != pack.Version {
			pack.LatestVersion = latest
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
		mvName,
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

func strVal(row map[string]any, key string) string {
	v, _ := row[key].(string)
	return v
}

// boolVal reads a bool column from a Proton HTTP JSON row.
// Proton serialises bool as 0/1 (json.Number), not JSON true/false.
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
