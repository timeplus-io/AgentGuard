package handlers

import (
	"encoding/json"
	"log"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/timeplus/agentguard/internal/auth"
	"github.com/timeplus/agentguard/internal/config"
	tp "github.com/timeplus/agentguard/internal/timeplus"
)

// dlpConfigRowView is one row of agentguard_dlp_config as returned to the
// operator, with any api_key redacted (api_key_set reports whether one exists).
type dlpConfigRowView struct {
	Key       string          `json:"key"`
	Value     json.RawMessage `json:"value"`
	APIKeySet bool            `json:"api_key_set"`
	UpdatedAt string          `json:"updated_at"`
	UpdatedBy string          `json:"updated_by"`
}

// redactAPIKey parses a config value, blanks any "api_key" field, and reports
// whether a non-empty key was present. Non-object/!JSON values pass through.
func redactAPIKey(value string) (json.RawMessage, bool) {
	var m map[string]any
	if err := json.Unmarshal([]byte(value), &m); err != nil {
		return json.RawMessage(value), false
	}
	had := false
	if v, ok := m["api_key"]; ok {
		if s, ok := v.(string); ok && s != "" {
			had = true
		}
		m["api_key"] = ""
	}
	b, err := json.Marshal(m)
	if err != nil {
		return json.RawMessage(value), had
	}
	return json.RawMessage(b), had
}

// GetDLPConfig handles GET /api/dlp/config — returns the live config rows with
// api keys redacted.
func GetDLPConfig(cfg *config.Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		rc := auth.RequestCfg(c, cfg)
		rows, err := tp.ReadDLPConfig(&rc.Timeplus)
		if err != nil {
			log.Printf("[dlp] GetDLPConfig read failed: %v", err)
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "DLP config unavailable; is DLP provisioned?"})
			return
		}
		out := make([]dlpConfigRowView, 0, len(rows))
		for _, r := range rows {
			redacted, hadKey := redactAPIKey(r.Value)
			out = append(out, dlpConfigRowView{
				Key: r.Key, Value: redacted, APIKeySet: hadKey,
				UpdatedAt: r.UpdatedAt, UpdatedBy: r.UpdatedBy,
			})
		}
		c.JSON(http.StatusOK, gin.H{"config": out, "keys": tp.DLPConfigKeys()})
	}
}

// preserveAPIKey injects the stored api_key into a new value when the submitted
// value omits it or leaves it blank, so a GET→edit→PUT round-trip (which never
// echoes the key) doesn't wipe it. Pass through for keys without an api_key.
func preserveAPIKey(tpCfg *config.TimeplusConfig, key, value string) (string, error) {
	var m map[string]any
	if err := json.Unmarshal([]byte(value), &m); err != nil {
		return value, nil // non-object value (e.g. nothing to preserve)
	}
	v, present := m["api_key"]
	s, _ := v.(string)
	if present && s != "" {
		return value, nil // caller supplied a new key
	}
	existing, err := tp.ReadDLPConfigValue(tpCfg, key)
	if err != nil {
		return "", err
	}
	if existing == "" {
		return value, nil
	}
	var em map[string]any
	if err := json.Unmarshal([]byte(existing), &em); err != nil {
		return value, nil
	}
	old, ok := em["api_key"].(string)
	if !ok || old == "" {
		return value, nil
	}
	m["api_key"] = old
	b, err := json.Marshal(m)
	if err != nil {
		return value, nil
	}
	return string(b), nil
}

// UpsertDLPConfig handles PUT /api/dlp/config/:key — upserts one config row.
// Behavior changes take effect live (ingest MV on the next event, funnel on the
// next query) with no re-provision.
func UpsertDLPConfig(cfg *config.Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		rc := auth.RequestCfg(c, cfg)
		key := c.Param("key")
		if !tp.IsDLPConfigKey(key) {
			c.JSON(http.StatusBadRequest, gin.H{"error": "unknown config key: " + key})
			return
		}
		var body struct {
			Value json.RawMessage `json:"value"`
		}
		if err := c.ShouldBindJSON(&body); err != nil || len(body.Value) == 0 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "request body must be {\"value\": <json>}"})
			return
		}
		value := string(body.Value)
		if err := tp.ValidateDLPConfigValue(key, value); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		merged, err := preserveAPIKey(&rc.Timeplus, key, value)
		if err != nil {
			log.Printf("[dlp] UpsertDLPConfig preserve-key failed: %v", err)
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "DLP config unavailable; is DLP provisioned?"})
			return
		}

		actor := ""
		if v, ok := c.Get("timeplus_user"); ok {
			if s, ok := v.(string); ok {
				actor = s
			}
		}
		if err := tp.WriteDLPConfig(&rc.Timeplus, key, merged, actor); err != nil {
			log.Printf("[dlp] UpsertDLPConfig write failed: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "config upsert failed"})
			return
		}
		c.JSON(http.StatusOK, gin.H{"ok": true, "key": key})
	}
}
