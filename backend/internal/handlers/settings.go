package handlers

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/timeplus/agentguard/internal/auth"
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
			Host: body.Host, Port: body.Port,
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
			RiskModel      string `json:"risk_model"`
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
			chat := dlpCheckChat(c, client, baseURL, key, riskModel, maxTokens)
			checks["embedding"] = emb
			checks["chat"] = chat
			ok = emb["ok"] == true && chat["ok"] == true
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
	return gin.H{"ok": dim == expected, "dim": dim, "expected": expected}
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
