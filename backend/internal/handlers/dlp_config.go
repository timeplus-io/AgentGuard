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

// blankAPIKey blanks a non-empty "api_key" on m, returning whether one existed.
func blankAPIKey(m map[string]any) bool {
	v, ok := m["api_key"]
	if !ok {
		return false
	}
	s, _ := v.(string)
	m["api_key"] = ""
	return s != ""
}

// redactAPIKey parses a config value and blanks every "api_key" — the top-level
// one and each per-model one under "models" — reporting whether any non-empty key
// was present. Non-object/!JSON values pass through.
func redactAPIKey(value string) (json.RawMessage, bool) {
	var m map[string]any
	if err := json.Unmarshal([]byte(value), &m); err != nil {
		return json.RawMessage(value), false
	}
	had := blankAPIKey(m)
	if models, ok := m["models"].(map[string]any); ok {
		for _, mv := range models {
			if entry, ok := mv.(map[string]any); ok {
				if blankAPIKey(entry) {
					had = true
				}
			}
		}
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

// fillAPIKey copies existing["api_key"] into m when m's is missing or blank, so a
// GET→edit→PUT round-trip (which never echoes keys) doesn't wipe a stored key.
func fillAPIKey(m, existing map[string]any) {
	if m == nil || existing == nil {
		return
	}
	if v, present := m["api_key"]; present {
		if s, _ := v.(string); s != "" {
			return // caller supplied a new key
		}
	}
	if old, ok := existing["api_key"].(string); ok && old != "" {
		m["api_key"] = old
	}
}

// preserveAPIKey injects stored api keys (top-level and per-model) into a new
// value wherever the submitted value omits or blanks them. Pass through for
// values with no api_key anywhere.
func preserveAPIKey(tpCfg *config.TimeplusConfig, key, value string) (string, error) {
	var m map[string]any
	if err := json.Unmarshal([]byte(value), &m); err != nil {
		return value, nil // non-object value (e.g. nothing to preserve)
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
	fillAPIKey(m, em)
	if models, ok := m["models"].(map[string]any); ok {
		emodels, _ := em["models"].(map[string]any)
		for name, mv := range models {
			entry, ok := mv.(map[string]any)
			if !ok {
				continue
			}
			var eentry map[string]any
			if emodels != nil {
				eentry, _ = emodels[name].(map[string]any)
			}
			fillAPIKey(entry, eentry)
		}
	}
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
