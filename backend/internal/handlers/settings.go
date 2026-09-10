package handlers

import (
	"bytes"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/timeplus/agentguard/internal/auth"
	"github.com/timeplus/agentguard/internal/catalog"
	"github.com/timeplus/agentguard/internal/config"
	tp "github.com/timeplus/agentguard/internal/timeplus"
)

// configKeyView is the per-key shape returned by GET /api/config.
type configKeyView struct {
	Key         string   `json:"key"`
	Label       string   `json:"label"`
	Description string   `json:"description"`
	Type        string   `json:"type"`
	Value       any      `json:"value"` // left nil (→ JSON null) for secrets; never echoes the secret
	Set         bool     `json:"set"`   // for secrets: whether a value exists
	Source      string   `json:"source"`
	EnvVar      string   `json:"env_var,omitempty"`
	Flag        string   `json:"flag,omitempty"`
	Editable    bool     `json:"editable"`
	Restart     bool         `json:"restart"`
	Reprovision bool         `json:"reprovision"`
	EnumValues  []string     `json:"enum_values,omitempty"`
	Options     []optionView `json:"options,omitempty"` // selectable values for TypeList keys
}

// optionView is one selectable value for a list-typed config key (e.g. a rule
// package: Value is the id, Label the localized display name).
type optionView struct {
	Value string `json:"value"`
	Label string `json:"label"`
}

type configSectionView struct {
	ID    string          `json:"id"`
	Title string          `json:"title"`
	Keys  []configKeyView `json:"keys"`
}

// buildConfigView assembles the full GET /api/config response from the live
// cfg plus the file-key set and the changed-flags set captured at startup.
func buildConfigView(cfg *config.Config, changedFlags map[string]bool, listOptions map[string][]optionView) gin.H {
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
			Options: listOptions[m.Key],
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

// reprovisionRequired is retained for API compatibility but is always false:
// no DLP setting is a hand-entered, provision-baked value anymore. embedding_dim
// is auto-detected from the model at provision (probed via dlp_embed); everything
// else (models, endpoint, timeout, tokens, entity catalog) is read live from the
// agentguard_dlp_config mutable stream. Operators re-provision on demand (e.g.
// after switching the embedding model) via the Settings action, not a drift flag.
func reprovisionRequired(cfg *config.Config) bool {
	return false
}

// packageListOptions builds the selectable options for the rules.visible_packages
// list key: every rule package, id → localized name.
func packageListOptions(mgr *catalog.Manager, locale string) map[string][]optionView {
	if mgr == nil {
		return nil
	}
	pkgs := mgr.Packages()
	opts := make([]optionView, 0, len(pkgs))
	for _, p := range pkgs {
		lp := p.Localized(locale)
		opts = append(opts, optionView{Value: lp.ID, Label: lp.Name})
	}
	return map[string][]optionView{"rules.visible_packages": opts}
}

// GetConfig returns the full effective configuration with per-key sources.
func GetConfig(cfg *config.Config, mgr *catalog.Manager, changedFlags map[string]bool) gin.HandlerFunc {
	return func(c *gin.Context) {
		opts := packageListOptions(mgr, resolveLocale(c))
		config.Mu.RLock()
		view := buildConfigView(cfg, changedFlags, opts)
		config.Mu.RUnlock()
		c.JSON(http.StatusOK, view)
	}
}

// UpdateConfig validates a batch of key changes, merge-writes them to the
// config file, and hot-applies non-restart keys to the live config. The batch
// is atomic: any invalid/unknown/read-only/overridden key rejects all changes.
func UpdateConfig(cfg *config.Config, mgr *catalog.Manager, changedFlags map[string]bool) gin.HandlerFunc {
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
		prevSentryEnabled := cfg.Sentry.Enabled
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
		sentryCfg := cfg.Sentry // snapshot for provisioning outside the lock
		view := buildConfigView(cfg, changedFlags, packageListOptions(mgr, resolveLocale(c)))
		config.Mu.Unlock()

		resp := gin.H{
			"saved":                saved,
			"restart_required":     view["restart_required"],
			"reprovision_required": view["reprovision_required"],
		}

		// Enabling Sentry at runtime must also provision it (#226):
		// InitSentry only ever ran from the setup wizard / rules sync, and
		// both skip a disabled Sentry — so a deployment provisioned with
		// Sentry off had no agentguard_sentry_* streams, and the Sentry page
		// 404'd (UNKNOWN_STREAM) after this toggle. SyncSentry is idempotent;
		// a failure (e.g. Timeplus unreachable) never fails the config save —
		// it is surfaced in the response, and the Sentry page offers a
		// "Provision now" recovery via POST /api/rules/sync.
		if !prevSentryEnabled && sentryCfg.Enabled {
			rcfg := auth.RequestCfg(c, cfg)
			if err := tp.SyncSentry(rcfg.Timeplus, sentryCfg); err != nil {
				log.Printf("settings: provision Sentry on enable: %v", err)
				resp["sentry_provisioned"] = false
				resp["sentry_provision_error"] = err.Error()
			} else {
				resp["sentry_provisioned"] = true
			}
		}

		c.JSON(http.StatusOK, resp)
	}
}

// TestTimeplusConfig runs SELECT 1 against a candidate host/port using the
// session's Timeplus credentials. Body: {"host":"...","port":3218}.
func TestTimeplusConfig(cfg *config.Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		var body struct {
			Host string `json:"host" binding:"required"`
			Port int    `json:"port" binding:"required"`
		}
		if err := c.ShouldBindJSON(&body); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": err.Error()})
			return
		}
		rc := auth.RequestCfg(c, cfg)
		testCfg := config.TimeplusConfig{
			Host: body.Host, Port: body.Port, Database: rc.Timeplus.Database,
			User: rc.Timeplus.User, Password: rc.Timeplus.Password,
		}
		if _, err := tp.Query(&testCfg, "SELECT 1 AS ok"); err != nil {
			c.JSON(http.StatusOK, gin.H{"ok": false, "error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"ok": true})
	}
}

// TestDLPConfig validates a candidate DLP model endpoint: reachability, a real
// embedding call (dimension match), and a real chat call (non-empty content,
// thinking-model detection). Always HTTP 200; failures are reported in-band.
func TestDLPConfig(cfg *config.Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		var body struct {
			BaseURL        string `json:"base_url"`
			APIKey         string `json:"api_key"`
			EmbeddingModel string `json:"embedding_model"`
			EmbeddingDim   int    `json:"embedding_dim"`
			RiskModel      string   `json:"risk_model"`
			ChatModels     []string `json:"chat_models"`
		}
		_ = c.ShouldBindJSON(&body)

		config.Mu.RLock()
		d := cfg.DLP
		config.Mu.RUnlock()

		baseURL := strings.TrimRight(orStr(body.BaseURL, d.BaseURL), "/")
		key := orStr(body.APIKey, d.APIKey)
		embModel := orStr(body.EmbeddingModel, d.EmbeddingModel)
		expectedDim := body.EmbeddingDim
		if expectedDim == 0 {
			expectedDim = d.EmbeddingDim
		}
		riskModel := orStr(body.RiskModel, d.RiskModel)
		maxTokens := d.MaxOutputTokens
		if maxTokens <= 0 {
			maxTokens = 512
		}

		if _, err := url.ParseRequestURI(baseURL + "/models"); err != nil {
			c.JSON(http.StatusOK, gin.H{"ok": false, "checks": gin.H{
				"reachable": gin.H{"ok": false, "error": "invalid base_url"}}})
			return
		}
		client := &http.Client{Timeout: 10 * time.Second}

		reach := dlpCheckReachable(c, client, baseURL, key)
		checks := gin.H{"reachable": reach}
		ok := reach["ok"] == true
		if ok {
			emb := dlpCheckEmbedding(c, client, baseURL, key, embModel, expectedDim)
			checks["embedding"] = emb
			ok = emb["ok"] == true

			// Validate each distinct chat model; fall back to the risk model.
			chatModels := dedupeNonEmpty(body.ChatModels)
			if len(chatModels) == 0 {
				chatModels = []string{riskModel}
			}
			chats := make([]gin.H, 0, len(chatModels))
			for _, m := range chatModels {
				ch := dlpCheckChat(c, client, baseURL, key, m, maxTokens)
				ch["model"] = m
				chats = append(chats, ch)
				if ch["ok"] != true {
					ok = false
				}
			}
			checks["chats"] = chats
			checks["chat"] = chats[0] // back-compat: Settings editor reads this
		}
		c.JSON(http.StatusOK, gin.H{"ok": ok, "checks": checks})
	}
}

func orStr(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

// dedupeNonEmpty trims, drops blanks, and removes duplicates, preserving order.
func dedupeNonEmpty(in []string) []string {
	seen := make(map[string]bool, len(in))
	out := make([]string, 0, len(in))
	for _, s := range in {
		s = strings.TrimSpace(s)
		if s == "" || seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	return out
}

func dlpCheckReachable(c *gin.Context, client *http.Client, baseURL, key string) gin.H {
	req, _ := http.NewRequestWithContext(c.Request.Context(), http.MethodGet, baseURL+"/models", nil)
	if key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}
	resp, err := client.Do(req)
	if err != nil {
		return gin.H{"ok": false, "error": err.Error()}
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return gin.H{"ok": false, "error": "HTTP " + resp.Status}
	}
	return gin.H{"ok": true}
}

func dlpCheckEmbedding(c *gin.Context, client *http.Client, baseURL, key, model string, expected int) gin.H {
	payload, _ := json.Marshal(gin.H{"model": model, "input": []string{"ping"}})
	req, _ := http.NewRequestWithContext(c.Request.Context(), http.MethodPost, baseURL+"/embeddings", bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	if key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}
	resp, err := client.Do(req)
	if err != nil {
		return gin.H{"ok": false, "error": err.Error()}
	}
	defer resp.Body.Close()
	var parsed struct {
		Data []struct {
			Embedding []float64 `json:"embedding"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil || len(parsed.Data) == 0 {
		return gin.H{"ok": false, "error": "no embedding returned"}
	}
	dim := len(parsed.Data[0].Embedding)
	okDim := dim == expected
	if expected <= 0 {
		// Dimension unknown (pre-provision): success is any non-empty vector.
		okDim = dim > 0
	}
	return gin.H{"ok": okDim, "dim": dim, "expected": expected}
}

func dlpCheckChat(c *gin.Context, client *http.Client, baseURL, key, model string, maxTokens int) gin.H {
	payload, _ := json.Marshal(gin.H{
		"model":       model,
		"messages":    []gin.H{{"role": "user", "content": "Reply with the single word OK."}},
		"temperature": 0,
		"max_tokens":  maxTokens,
	})
	req, _ := http.NewRequestWithContext(c.Request.Context(), http.MethodPost, baseURL+"/chat/completions", bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	if key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}
	resp, err := client.Do(req)
	if err != nil {
		return gin.H{"ok": false, "error": err.Error()}
	}
	defer resp.Body.Close()
	var parsed struct {
		Choices []struct {
			Message struct {
				Content          string `json:"content"`
				Reasoning        string `json:"reasoning"`
				ReasoningContent string `json:"reasoning_content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil || len(parsed.Choices) == 0 {
		return gin.H{"ok": false, "error": "no chat response"}
	}
	content := strings.TrimSpace(parsed.Choices[0].Message.Content)
	if content != "" {
		return gin.H{"ok": true, "thinking_detected": false, "note": ""}
	}
	msg := parsed.Choices[0].Message
	if strings.TrimSpace(msg.Reasoning) != "" || strings.TrimSpace(msg.ReasoningContent) != "" {
		return gin.H{"ok": false, "thinking_detected": true,
			"note": "model returned empty content with reasoning — raise max_output_tokens or use a direct-response model"}
	}
	return gin.H{"ok": false, "thinking_detected": false, "note": "empty content"}
}
