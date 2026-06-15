package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/timeplus/agentguard/internal/config"
)

// configKeyView is the per-key shape returned by GET /api/config.
type configKeyView struct {
	Key         string   `json:"key"`
	Label       string   `json:"label"`
	Description string   `json:"description"`
	Type        string   `json:"type"`
	Value       any      `json:"value,omitempty"` // omitted for secrets
	Set         bool     `json:"set"`             // for secrets: whether a value exists
	Source      string   `json:"source"`
	EnvVar      string   `json:"env_var,omitempty"`
	Flag        string   `json:"flag,omitempty"`
	Editable    bool     `json:"editable"`
	Restart     bool     `json:"restart"`
	Reprovision bool     `json:"reprovision"`
	EnumValues  []string `json:"enum_values,omitempty"`
}

type configSectionView struct {
	ID    string          `json:"id"`
	Title string          `json:"title"`
	Keys  []configKeyView `json:"keys"`
}

// buildConfigView assembles the full GET /api/config response from the live
// cfg plus the file-key set and the changed-flags set captured at startup.
func buildConfigView(cfg *config.Config, changedFlags map[string]bool) gin.H {
	fileKeys, _ := config.FileKeys(cfg.ConfigPath)

	bySection := map[string][]configKeyView{}
	for _, m := range config.Registry() {
		src := config.ResolveSource(m, fileKeys, changedFlags)
		editable := !m.ReadOnly && src != config.SourceEnv && src != config.SourceFlag

		view := configKeyView{
			Key: m.Key, Label: m.Label, Description: m.Description,
			Type: string(m.Type), Source: string(src),
			EnvVar: m.EnvVar, Flag: m.FlagName, Editable: editable,
			Restart: m.Restart, Reprovision: m.Reprovision, EnumValues: m.EnumValues,
		}
		val := config.EffectiveValue(cfg, m.Key)
		if m.Type == config.TypeSecret {
			s, _ := val.(string)
			view.Set = s != ""
		} else {
			view.Value = val
			view.Set = true
		}
		bySection[m.Section] = append(bySection[m.Section], view)
	}

	sections := make([]configSectionView, 0, len(config.Sections()))
	for _, s := range config.Sections() {
		sections = append(sections, configSectionView{ID: s.ID, Title: s.Title, Keys: bySection[s.ID]})
	}

	return gin.H{
		"restart_required":     restartRequired(cfg, fileKeys, changedFlags),
		"reprovision_required": reprovisionRequired(cfg),
		"sections":             sections,
	}
}

// restartRequired is true when a restart-flagged key's file/default value
// (i.e. what the server would load on restart) differs from the live value,
// excluding keys currently masked by an env var or CLI flag (those are
// read-only and already reflected in the live config).
func restartRequired(cfg *config.Config, fileKeys, changedFlags map[string]bool) bool {
	fileCfg, err := config.LoadFrom(cfg.ConfigPath)
	if err != nil {
		return false
	}
	for _, m := range config.Registry() {
		if !m.Restart {
			continue
		}
		src := config.ResolveSource(m, fileKeys, changedFlags)
		if src == config.SourceEnv || src == config.SourceFlag {
			continue
		}
		if config.EffectiveValue(fileCfg, m.Key) != config.EffectiveValue(cfg, m.Key) {
			return true
		}
	}
	return false
}

// reprovisionRequired is true when DLP is enabled, a provision snapshot exists,
// and any baked-in DLP model setting differs from that snapshot.
func reprovisionRequired(cfg *config.Config) bool {
	if !cfg.DLP.Enabled || !cfg.ProvisionedDLP.Recorded {
		return false
	}
	p := cfg.ProvisionedDLP
	return cfg.DLP.EmbeddingModel != p.EmbeddingModel ||
		cfg.DLP.EmbeddingDim != p.EmbeddingDim ||
		cfg.DLP.RiskModel != p.RiskModel ||
		cfg.DLP.SummaryModel != p.SummaryModel
}

// GetConfig returns the full effective configuration with per-key sources.
func GetConfig(cfg *config.Config, changedFlags map[string]bool) gin.HandlerFunc {
	return func(c *gin.Context) {
		config.Mu.RLock()
		view := buildConfigView(cfg, changedFlags)
		config.Mu.RUnlock()
		c.JSON(http.StatusOK, view)
	}
}

// UpdateConfig validates a batch of key changes, merge-writes them to the
// config file, and hot-applies non-restart keys to the live config. The batch
// is atomic: any invalid/unknown/read-only/overridden key rejects all changes.
func UpdateConfig(cfg *config.Config, changedFlags map[string]bool) gin.HandlerFunc {
	return func(c *gin.Context) {
		var body map[string]any
		if err := c.ShouldBindJSON(&body); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		if len(body) == 0 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "no changes provided"})
			return
		}

		fileKeys, _ := config.FileKeys(cfg.ConfigPath)
		errs := map[string]string{}
		coerced := map[string]any{}

		for key, raw := range body {
			meta, ok := config.MetaFor(key)
			if !ok {
				errs[key] = "unknown config key"
				continue
			}
			if meta.ReadOnly {
				errs[key] = "this key is read-only"
				continue
			}
			src := config.ResolveSource(meta, fileKeys, changedFlags)
			if src == config.SourceEnv {
				errs[key] = "set by environment variable " + meta.EnvVar + "; remove it to edit here"
				continue
			}
			if src == config.SourceFlag {
				errs[key] = "set by the --" + meta.FlagName + " flag; file edits have no effect"
				continue
			}
			val, err := config.CoerceAndValidate(key, raw)
			if err != nil {
				errs[key] = err.Error()
				continue
			}
			coerced[key] = val
		}

		if len(errs) > 0 {
			c.JSON(http.StatusBadRequest, gin.H{"errors": errs})
			return
		}

		// Persist all changed keys to the file (restart keys included).
		if err := config.SaveChanges(cfg.ConfigPath, coerced); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		// Hot-apply only non-restart keys to the live config.
		config.Mu.Lock()
		for key, val := range coerced {
			meta, _ := config.MetaFor(key)
			if meta.Restart {
				continue
			}
			_ = config.ApplyChange(cfg, key, val)
		}
		saved := make([]string, 0, len(coerced))
		for k := range coerced {
			saved = append(saved, k)
		}
		view := buildConfigView(cfg, changedFlags)
		config.Mu.Unlock()

		c.JSON(http.StatusOK, gin.H{
			"saved":                saved,
			"restart_required":     view["restart_required"],
			"reprovision_required": view["reprovision_required"],
		})
	}
}
