# Configuration UI Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Let a logged-in user view the full effective AgentGuard configuration (with each key's source), edit editable keys from a `/settings` page, persist them to `config.yaml`, and be told clearly when a restart or DLP re-provision is required — without shell access.

**Architecture:** A single declarative **key registry** in `internal/config` describes every config key (type, env var, flag, restart/reprovision flags, read-only). Helper functions resolve each key's source (default/file/env/flag), read its live value, coerce+validate incoming values, apply hot changes to the live `*config.Config`, and merge-write only changed keys to the YAML file. New `GET/PUT /api/config` handlers plus `POST /api/config/test-timeplus` and `POST /api/config/test-dlp` sit in the existing protected (session-auth) group. A metadata-driven React `Settings` page renders sections generically. DLP re-provision reuses the existing `POST /api/rules/sync` (`SyncDLP`).

**Tech Stack:** Go 1.x + Gin + Viper (backend), React 18 + React Router v6 + Tailwind (frontend). Spec: `docs/superpowers/specs/2026-06-12-configuration-ui-design.md`.

**Build/test note (macOS):** always use `CGO_ENABLED=0` for `go build`/`go test` (see CLAUDE.md).

---

## File Structure

**Backend (new):**
- `backend/internal/config/registry.go` — `KeyMeta`, `KeyType`, `Registry()`, `MetaFor()`, section list.
- `backend/internal/config/registry_test.go` — registry invariants.
- `backend/internal/config/sources.go` — `Source`, `FileKeys()`, `ResolveSource()`, `EffectiveValue()`, `ApplyChange()`, `CoerceAndValidate()`, `SaveChanges()`.
- `backend/internal/config/sources_test.go` — source detection, coercion/validation, merge-write.
- `backend/internal/handlers/settings.go` — `GetConfig`, `UpdateConfig`, `TestTimeplusConfig`, `TestDLPConfig` handlers + response shaping.
- `backend/internal/handlers/settings_test.go` — handler tests.

**Backend (modified):**
- `backend/internal/config/config.go` — add `ProvisionedDLP` to `Config`; load `provisioned.dlp.*`; extend `Save()` to persist the snapshot (NOT `dlp.api_key`).
- `backend/internal/handlers/setup.go` — `Provision` records the DLP snapshot before `Save`.
- `backend/cmd/server/main.go` — collect changed CLI flags; register the four new routes.

**Frontend (new):**
- `frontend/src/pages/Settings.jsx` — the settings page.

**Frontend (modified):**
- `frontend/src/api/client.js` — `getConfig`, `updateConfig`, `testTimeplusConfig`, `testDLPConfig`.
- `frontend/src/App.jsx` — import + `/settings` route.
- `frontend/src/components/Sidebar.jsx` — "Settings" nav item.

---

## Task 1: Config key registry

**Files:**
- Create: `backend/internal/config/registry.go`
- Test: `backend/internal/config/registry_test.go`

- [ ] **Step 1: Write the failing test**

Create `backend/internal/config/registry_test.go`:

```go
package config

import "testing"

func TestRegistryCoversAllKeys(t *testing.T) {
	reg := Registry()
	if len(reg) == 0 {
		t.Fatal("registry is empty")
	}
	// Spot-check a representative key from each section.
	want := []string{
		"server.port", "timeplus.host", "timeplus.native-port",
		"timeplus.user", "timeplus.password", "agentguard.url",
		"catalog.dir", "holds.fail_policy", "dlp.enabled",
		"dlp.api_key", "dlp.embedding_dim", "setup.complete",
	}
	for _, k := range want {
		if _, ok := MetaFor(k); !ok {
			t.Errorf("registry missing key %q", k)
		}
	}
}

func TestRegistryMetaInvariants(t *testing.T) {
	for _, m := range Registry() {
		if m.Key == "" || m.Section == "" || m.Label == "" {
			t.Errorf("key %q: empty Key/Section/Label", m.Key)
		}
		if m.Type == TypeEnum && len(m.EnumValues) == 0 {
			t.Errorf("key %q: enum with no EnumValues", m.Key)
		}
	}
	// Read-only & env-only invariants.
	if m, _ := MetaFor("setup.complete"); !m.ReadOnly {
		t.Error("setup.complete must be ReadOnly")
	}
	if m, _ := MetaFor("timeplus.password"); !m.ReadOnly || m.Type != TypeSecret {
		t.Error("timeplus.password must be ReadOnly secret")
	}
	if m, _ := MetaFor("dlp.api_key"); m.Type != TypeSecret || m.ReadOnly {
		t.Error("dlp.api_key must be an editable secret")
	}
	if m, _ := MetaFor("server.port"); !m.Restart {
		t.Error("server.port must be Restart")
	}
	if m, _ := MetaFor("dlp.embedding_dim"); !m.Reprovision {
		t.Error("dlp.embedding_dim must be Reprovision")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd backend && CGO_ENABLED=0 go test ./internal/config/ -run TestRegistry -v`
Expected: FAIL — `undefined: Registry` / `MetaFor` / `TypeEnum` etc.

- [ ] **Step 3: Write the implementation**

Create `backend/internal/config/registry.go`:

```go
package config

// KeyType classifies a config key for UI rendering and value coercion.
type KeyType string

const (
	TypeString KeyType = "string"
	TypeInt    KeyType = "int"
	TypeBool   KeyType = "bool"
	TypeSecret KeyType = "secret"
	TypeEnum   KeyType = "enum"
)

// KeyMeta declares everything the API and UI need to know about one config key.
type KeyMeta struct {
	Key         string   // viper dotted key, e.g. "dlp.api_key"
	Section     string   // section id, e.g. "dlp"
	Label       string   // human label
	Description string   // help text
	Type        KeyType
	EnumValues  []string // for TypeEnum
	EnvVar      string   // overriding env var name, "" if none
	FlagName    string   // overriding CLI flag name, "" if none
	Restart     bool     // change takes effect only after server restart
	Reprovision bool     // change needs DLP re-provision to take effect
	ReadOnly    bool     // never editable from the UI
}

// SectionMeta gives sections a stable display order and title.
type SectionMeta struct {
	ID    string
	Title string
}

// Sections returns config sections in display order.
func Sections() []SectionMeta {
	return []SectionMeta{
		{"server", "Server"},
		{"timeplus", "Timeplus Connection"},
		{"agentguard", "AgentGuard"},
		{"catalog", "Catalog"},
		{"holds", "Approval Holds"},
		{"dlp", "Semantic DLP"},
		{"setup", "Setup"},
	}
}

// Registry returns every config key in display order.
func Registry() []KeyMeta {
	return []KeyMeta{
		// ── server ──
		{Key: "server.port", Section: "server", Label: "HTTP port",
			Description: "Port the AgentGuard server listens on.",
			Type:        TypeInt, EnvVar: "SERVER_PORT", FlagName: "port", Restart: true},

		// ── timeplus ──
		{Key: "timeplus.host", Section: "timeplus", Label: "Host",
			Description: "Timeplus Enterprise hostname.",
			Type:        TypeString, EnvVar: "TIMEPLUS_HOST", FlagName: "timeplus-host"},
		{Key: "timeplus.port", Section: "timeplus", Label: "HTTP port",
			Description: "Timeplus HTTP (Proton) port.",
			Type:        TypeInt, EnvVar: "TIMEPLUS_PORT", FlagName: "timeplus-port"},
		{Key: "timeplus.native-port", Section: "timeplus", Label: "Native port",
			Description: "Timeplus native TCP port.",
			Type:        TypeInt, EnvVar: "TIMEPLUS_NATIVE_PORT", FlagName: "timeplus-native-port"},
		{Key: "timeplus.user", Section: "timeplus", Label: "Service user",
			Description: "Service account username. Set via the TIMEPLUS_USER environment variable only.",
			Type:        TypeString, EnvVar: "TIMEPLUS_USER", ReadOnly: true},
		{Key: "timeplus.password", Section: "timeplus", Label: "Service password",
			Description: "Service account password. Set via the TIMEPLUS_PASSWORD environment variable only.",
			Type:        TypeSecret, EnvVar: "TIMEPLUS_PASSWORD", ReadOnly: true},

		// ── agentguard ──
		{Key: "agentguard.url", Section: "agentguard", Label: "Webhook base URL",
			Description: "URL Timeplus uses to call back into AgentGuard (alerts, holds).",
			Type:        TypeString, EnvVar: "AGENTGUARD_URL", FlagName: "agentguard-url"},

		// ── catalog ──
		{Key: "catalog.dir", Section: "catalog", Label: "Catalog directory",
			Description: "Filesystem directory the rule catalog is loaded from.",
			Type:        TypeString, EnvVar: "CATALOG_DIR", FlagName: "catalog-dir", Restart: true},

		// ── holds ──
		{Key: "holds.mv_wait_ms", Section: "holds", Label: "MV wait (ms)",
			Description: "Milliseconds to wait for a rule MV to evaluate before deciding a hold.",
			Type:        TypeInt, EnvVar: "AGENTGUARD_HOLD_MV_WAIT_MS"},
		{Key: "holds.timeout_seconds", Section: "holds", Label: "Hold timeout (s)",
			Description: "Seconds a hold waits for a human decision before failing (max 540).",
			Type:        TypeInt, EnvVar: "AGENTGUARD_HOLD_TIMEOUT_SECONDS"},
		{Key: "holds.fail_policy", Section: "holds", Label: "Fail policy",
			Description: "What to do when a hold times out or the server shuts down.",
			Type:        TypeEnum, EnumValues: []string{"deny", "allow"}, EnvVar: "AGENTGUARD_HOLDS_FAIL_POLICY"},
		{Key: "holds.disabled", Section: "holds", Label: "Disable holds",
			Description: "When true, no PreToolUse holds are created.",
			Type:        TypeBool, EnvVar: "AGENTGUARD_HOLDS_DISABLED"},

		// ── dlp ──
		{Key: "dlp.enabled", Section: "dlp", Label: "Enable Semantic DLP",
			Description: "Turn on the Semantic DLP enrichment + query pipeline.",
			Type:        TypeBool, EnvVar: "DLP_ENABLED"},
		{Key: "dlp.base_url", Section: "dlp", Label: "LLM base URL",
			Description: "OpenAI-compatible base URL for DLP embedding/risk/summary models.",
			Type:        TypeString, EnvVar: "DLP_BASE_URL"},
		{Key: "dlp.api_key", Section: "dlp", Label: "API key",
			Description: "Bearer key for the DLP LLM endpoint. Write-only; never displayed.",
			Type:        TypeSecret, EnvVar: "DLP_API_KEY"},
		{Key: "dlp.embedding_model", Section: "dlp", Label: "Embedding model",
			Description: "Model used to embed content. Baked into UDFs — change needs re-provision.",
			Type:        TypeString, EnvVar: "DLP_EMBEDDING_MODEL", Reprovision: true},
		{Key: "dlp.embedding_dim", Section: "dlp", Label: "Embedding dimension",
			Description: "Vector dimension. Baked into the DLP stream — change needs re-provision (stream recreate).",
			Type:        TypeInt, EnvVar: "DLP_EMBEDDING_DIM", Reprovision: true},
		{Key: "dlp.risk_model", Section: "dlp", Label: "Risk model",
			Description: "Model used for risk classification. Baked into UDFs — change needs re-provision.",
			Type:        TypeString, EnvVar: "DLP_RISK_MODEL", Reprovision: true},
		{Key: "dlp.summary_model", Section: "dlp", Label: "Summary model",
			Description: "Model used for query summarization. Baked into UDFs — change needs re-provision.",
			Type:        TypeString, EnvVar: "DLP_SUMMARY_MODEL", Reprovision: true},

		// ── setup ──
		{Key: "setup.complete", Section: "setup", Label: "Setup complete",
			Description: "Whether the setup wizard has been run.",
			Type:        TypeBool, EnvVar: "SETUP_COMPLETE", ReadOnly: true},
	}
}

// MetaFor returns the KeyMeta for a dotted key.
func MetaFor(key string) (KeyMeta, bool) {
	for _, m := range Registry() {
		if m.Key == key {
			return m, true
		}
	}
	return KeyMeta{}, false
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd backend && CGO_ENABLED=0 go test ./internal/config/ -run TestRegistry -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add backend/internal/config/registry.go backend/internal/config/registry_test.go
git commit -m "feat(config): config key registry for settings UI (#180)"
```

---

## Task 2: Coercion + validation

**Files:**
- Create: `backend/internal/config/sources.go`
- Test: `backend/internal/config/sources_test.go`

- [ ] **Step 1: Write the failing test**

Create `backend/internal/config/sources_test.go`:

```go
package config

import "testing"

func TestCoerceAndValidate(t *testing.T) {
	cases := []struct {
		key     string
		raw     any
		wantErr bool
		want    any
	}{
		{"server.port", float64(9090), false, 9090},      // JSON numbers arrive as float64
		{"server.port", float64(0), true, nil},           // out of range
		{"server.port", float64(70000), true, nil},       // out of range
		{"server.port", "abc", true, nil},                // wrong type
		{"dlp.enabled", true, false, true},
		{"dlp.enabled", "yes", true, nil},                // wrong type
		{"holds.fail_policy", "allow", false, "allow"},
		{"holds.fail_policy", "maybe", true, nil},        // not in enum
		{"dlp.embedding_dim", float64(768), false, 768},
		{"dlp.embedding_dim", float64(0), true, nil},     // must be >= 1
		{"dlp.base_url", "http://x:4000/v1", false, "http://x:4000/v1"},
		{"dlp.base_url", "not a url", true, nil},
		{"agentguard.url", "https://h:8080", false, "https://h:8080"},
		{"dlp.api_key", "sk-123", false, "sk-123"},
		{"holds.timeout_seconds", float64(-1), true, nil},
		{"nope.key", "x", true, nil},                     // unknown key
	}
	for _, c := range cases {
		got, err := CoerceAndValidate(c.key, c.raw)
		if c.wantErr {
			if err == nil {
				t.Errorf("%s=%v: expected error, got nil", c.key, c.raw)
			}
			continue
		}
		if err != nil {
			t.Errorf("%s=%v: unexpected error %v", c.key, c.raw, err)
			continue
		}
		if got != c.want {
			t.Errorf("%s=%v: got %v (%T), want %v (%T)", c.key, c.raw, got, got, c.want, c.want)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd backend && CGO_ENABLED=0 go test ./internal/config/ -run TestCoerceAndValidate -v`
Expected: FAIL — `undefined: CoerceAndValidate`

- [ ] **Step 3: Write the implementation**

Create `backend/internal/config/sources.go` (coercion/validation portion; later steps add more to this file):

```go
package config

import (
	"errors"
	"fmt"
	"net/url"
)

// CoerceAndValidate converts a raw JSON value (float64/bool/string) to the Go
// type the key expects and validates it. Returns the coerced value or an error
// suitable for surfacing to the UI. Unknown keys are an error.
func CoerceAndValidate(key string, raw any) (any, error) {
	meta, ok := MetaFor(key)
	if !ok {
		return nil, fmt.Errorf("unknown config key %q", key)
	}
	switch meta.Type {
	case TypeBool:
		b, ok := raw.(bool)
		if !ok {
			return nil, errors.New("expected a boolean")
		}
		return b, nil

	case TypeInt:
		i, err := toInt(raw)
		if err != nil {
			return nil, err
		}
		if err := validateInt(key, i); err != nil {
			return nil, err
		}
		return i, nil

	case TypeEnum:
		s, ok := raw.(string)
		if !ok {
			return nil, errors.New("expected a string")
		}
		for _, v := range meta.EnumValues {
			if v == s {
				return s, nil
			}
		}
		return nil, fmt.Errorf("must be one of %v", meta.EnumValues)

	case TypeString, TypeSecret:
		s, ok := raw.(string)
		if !ok {
			return nil, errors.New("expected a string")
		}
		if err := validateString(key, s); err != nil {
			return nil, err
		}
		return s, nil
	}
	return nil, fmt.Errorf("unsupported type for key %q", key)
}

func toInt(raw any) (int, error) {
	switch n := raw.(type) {
	case float64: // JSON numbers
		if n != float64(int(n)) {
			return 0, errors.New("expected a whole number")
		}
		return int(n), nil
	case int:
		return n, nil
	default:
		return 0, errors.New("expected a number")
	}
}

func validateInt(key string, v int) error {
	switch key {
	case "server.port", "timeplus.port", "timeplus.native-port":
		if v < 1 || v > 65535 {
			return errors.New("must be between 1 and 65535")
		}
	case "dlp.embedding_dim":
		if v < 1 {
			return errors.New("must be at least 1")
		}
	case "holds.mv_wait_ms", "holds.timeout_seconds":
		if v < 0 {
			return errors.New("must be zero or greater")
		}
	}
	return nil
}

func validateString(key, v string) error {
	switch key {
	case "agentguard.url", "dlp.base_url":
		if v == "" {
			return errors.New("must not be empty")
		}
		u, err := url.ParseRequestURI(v)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
			return errors.New("must be a valid http(s) URL")
		}
	}
	return nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd backend && CGO_ENABLED=0 go test ./internal/config/ -run TestCoerceAndValidate -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add backend/internal/config/sources.go backend/internal/config/sources_test.go
git commit -m "feat(config): value coercion + validation for config keys (#180)"
```

---

## Task 3: Source detection + effective value + apply

**Files:**
- Modify: `backend/internal/config/sources.go`
- Test: `backend/internal/config/sources_test.go`

- [ ] **Step 1: Write the failing test**

Append to `backend/internal/config/sources_test.go`:

```go
func TestResolveSource(t *testing.T) {
	fileKeys := map[string]bool{"dlp.base_url": true}
	flags := map[string]bool{"port": true}

	m, _ := MetaFor("server.port")
	if got := ResolveSource(m, fileKeys, flags); got != SourceFlag {
		t.Errorf("server.port: got %v, want flag", got)
	}

	t.Setenv("DLP_API_KEY", "sk-env")
	mk, _ := MetaFor("dlp.api_key")
	if got := ResolveSource(mk, fileKeys, flags); got != SourceEnv {
		t.Errorf("dlp.api_key: got %v, want env", got)
	}

	mb, _ := MetaFor("dlp.base_url")
	if got := ResolveSource(mb, fileKeys, flags); got != SourceFile {
		t.Errorf("dlp.base_url: got %v, want file", got)
	}

	mh, _ := MetaFor("timeplus.host")
	if got := ResolveSource(mh, fileKeys, flags); got != SourceDefault {
		t.Errorf("timeplus.host: got %v, want default", got)
	}
}

func TestEffectiveAndApply(t *testing.T) {
	cfg := &Config{}
	cfg.DLP.Enabled = false
	cfg.Server.Port = 8080

	if got := EffectiveValue(cfg, "server.port"); got != 8080 {
		t.Errorf("EffectiveValue server.port: got %v", got)
	}
	if err := ApplyChange(cfg, "server.port", 9090); err != nil {
		t.Fatalf("ApplyChange: %v", err)
	}
	if cfg.Server.Port != 9090 {
		t.Errorf("ApplyChange server.port: cfg=%d", cfg.Server.Port)
	}
	if err := ApplyChange(cfg, "dlp.enabled", true); err != nil {
		t.Fatalf("ApplyChange dlp.enabled: %v", err)
	}
	if !cfg.DLP.Enabled {
		t.Error("ApplyChange dlp.enabled: cfg still false")
	}
	if err := ApplyChange(cfg, "dlp.api_key", "sk-x"); err != nil {
		t.Fatalf("ApplyChange dlp.api_key: %v", err)
	}
	if cfg.DLP.APIKey != "sk-x" {
		t.Error("ApplyChange dlp.api_key not applied")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd backend && CGO_ENABLED=0 go test ./internal/config/ -run 'TestResolveSource|TestEffectiveAndApply' -v`
Expected: FAIL — `undefined: ResolveSource` / `EffectiveValue` / `ApplyChange` / `SourceFlag`

- [ ] **Step 3: Write the implementation**

Append to `backend/internal/config/sources.go` (add `"os"` to the import block):

```go
// Source indicates where a key's effective value comes from.
type Source string

const (
	SourceDefault Source = "default"
	SourceFile    Source = "file"
	SourceEnv     Source = "env"
	SourceFlag    Source = "flag"
)

// ResolveSource returns the highest-precedence source supplying a key:
// flag > env > file > default.
func ResolveSource(meta KeyMeta, fileKeys, changedFlags map[string]bool) Source {
	if meta.FlagName != "" && changedFlags[meta.FlagName] {
		return SourceFlag
	}
	if meta.EnvVar != "" && os.Getenv(meta.EnvVar) != "" {
		return SourceEnv
	}
	if fileKeys[meta.Key] {
		return SourceFile
	}
	return SourceDefault
}

// EffectiveValue returns the live value of a key from cfg as a comparable
// scalar (int, bool, or string). Secrets are returned verbatim; callers that
// expose values to clients must mask secrets themselves.
func EffectiveValue(cfg *Config, key string) any {
	switch key {
	case "server.port":
		return cfg.Server.Port
	case "timeplus.host":
		return cfg.Timeplus.Host
	case "timeplus.port":
		return cfg.Timeplus.Port
	case "timeplus.native-port":
		return cfg.Timeplus.NativePort
	case "timeplus.user":
		return cfg.Timeplus.User
	case "timeplus.password":
		return cfg.Timeplus.Password
	case "agentguard.url":
		return cfg.AgentGuardURL
	case "catalog.dir":
		return cfg.CatalogDir
	case "holds.mv_wait_ms":
		return cfg.Holds.MVWaitMs
	case "holds.timeout_seconds":
		return cfg.Holds.TimeoutSeconds
	case "holds.fail_policy":
		return cfg.Holds.FailPolicy
	case "holds.disabled":
		return cfg.Holds.Disabled
	case "dlp.enabled":
		return cfg.DLP.Enabled
	case "dlp.base_url":
		return cfg.DLP.BaseURL
	case "dlp.api_key":
		return cfg.DLP.APIKey
	case "dlp.embedding_model":
		return cfg.DLP.EmbeddingModel
	case "dlp.embedding_dim":
		return cfg.DLP.EmbeddingDim
	case "dlp.risk_model":
		return cfg.DLP.RiskModel
	case "dlp.summary_model":
		return cfg.DLP.SummaryModel
	case "setup.complete":
		return cfg.SetupComplete
	}
	return nil
}

// ApplyChange writes a single coerced value into cfg. The value must already
// be coerced via CoerceAndValidate (int/bool/string). Callers must hold Mu.
func ApplyChange(cfg *Config, key string, value any) error {
	switch key {
	case "server.port":
		return setInt(&cfg.Server.Port, value)
	case "timeplus.host":
		return setString(&cfg.Timeplus.Host, value)
	case "timeplus.port":
		return setInt(&cfg.Timeplus.Port, value)
	case "timeplus.native-port":
		return setInt(&cfg.Timeplus.NativePort, value)
	case "agentguard.url":
		return setString(&cfg.AgentGuardURL, value)
	case "catalog.dir":
		return setString(&cfg.CatalogDir, value)
	case "holds.mv_wait_ms":
		return setInt(&cfg.Holds.MVWaitMs, value)
	case "holds.timeout_seconds":
		return setInt(&cfg.Holds.TimeoutSeconds, value)
	case "holds.fail_policy":
		return setString(&cfg.Holds.FailPolicy, value)
	case "holds.disabled":
		return setBool(&cfg.Holds.Disabled, value)
	case "dlp.enabled":
		return setBool(&cfg.DLP.Enabled, value)
	case "dlp.base_url":
		return setString(&cfg.DLP.BaseURL, value)
	case "dlp.api_key":
		return setString(&cfg.DLP.APIKey, value)
	case "dlp.embedding_model":
		return setString(&cfg.DLP.EmbeddingModel, value)
	case "dlp.embedding_dim":
		return setInt(&cfg.DLP.EmbeddingDim, value)
	case "dlp.risk_model":
		return setString(&cfg.DLP.RiskModel, value)
	case "dlp.summary_model":
		return setString(&cfg.DLP.SummaryModel, value)
	}
	return fmt.Errorf("key %q cannot be applied", key)
}

func setInt(dst *int, v any) error {
	i, ok := v.(int)
	if !ok {
		return fmt.Errorf("expected int, got %T", v)
	}
	*dst = i
	return nil
}

func setString(dst *string, v any) error {
	s, ok := v.(string)
	if !ok {
		return fmt.Errorf("expected string, got %T", v)
	}
	*dst = s
	return nil
}

func setBool(dst *bool, v any) error {
	b, ok := v.(bool)
	if !ok {
		return fmt.Errorf("expected bool, got %T", v)
	}
	*dst = b
	return nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd backend && CGO_ENABLED=0 go test ./internal/config/ -run 'TestResolveSource|TestEffectiveAndApply' -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add backend/internal/config/sources.go backend/internal/config/sources_test.go
git commit -m "feat(config): source detection, effective value, hot apply (#180)"
```

---

## Task 4: File-keys reader + merge-write

**Files:**
- Modify: `backend/internal/config/sources.go`
- Test: `backend/internal/config/sources_test.go`

- [ ] **Step 1: Write the failing test**

Append to `backend/internal/config/sources_test.go` (add `"os"`, `"path/filepath"` to imports):

```go
func TestFileKeysAndSaveChanges(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	seed := "" +
		"server:\n  port: 8080\n" +
		"dlp:\n  enabled: false\n" +
		"provisioned:\n  dlp:\n    recorded: true\n    embedding_dim: 1536\n"
	if err := os.WriteFile(path, []byte(seed), 0o644); err != nil {
		t.Fatal(err)
	}

	keys, err := FileKeys(path)
	if err != nil {
		t.Fatalf("FileKeys: %v", err)
	}
	if !keys["server.port"] || !keys["dlp.enabled"] {
		t.Errorf("FileKeys missing expected keys: %v", keys)
	}

	// Merge-write only changed keys; unrelated keys must survive.
	err = SaveChanges(path, map[string]any{
		"dlp.enabled": true,
		"dlp.api_key": "sk-merge",
	})
	if err != nil {
		t.Fatalf("SaveChanges: %v", err)
	}

	reloaded, err := LoadFrom(path)
	if err != nil {
		t.Fatalf("LoadFrom: %v", err)
	}
	if !reloaded.DLP.Enabled {
		t.Error("dlp.enabled not persisted")
	}
	if reloaded.DLP.APIKey != "sk-merge" {
		t.Error("dlp.api_key not persisted")
	}
	if reloaded.Server.Port != 8080 {
		t.Errorf("unrelated server.port lost: %d", reloaded.Server.Port)
	}
	// provisioned.dlp.* must survive a merge-write.
	k2, _ := FileKeys(path)
	if !k2["provisioned.dlp.recorded"] {
		t.Error("provisioned.dlp.recorded lost on merge-write")
	}

	// File mode must be 0600.
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("file mode = %o, want 600", perm)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd backend && CGO_ENABLED=0 go test ./internal/config/ -run TestFileKeysAndSaveChanges -v`
Expected: FAIL — `undefined: FileKeys` / `SaveChanges`

- [ ] **Step 3: Write the implementation**

Append to `backend/internal/config/sources.go` (add `"github.com/spf13/viper"` to imports):

```go
// FileKeys returns the set of dotted keys present in the YAML file at path.
// A missing file yields an empty set (not an error).
func FileKeys(path string) (map[string]bool, error) {
	out := map[string]bool{}
	v := viper.New()
	v.SetConfigFile(path)
	if err := v.ReadInConfig(); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return out, nil
		}
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}
	for _, k := range v.AllKeys() {
		out[k] = true
	}
	return out, nil
}

// SaveChanges merge-writes only the given keys into the YAML file at path,
// preserving every other existing key, then enforces 0600 permissions.
// A missing file is created. Values must be pre-coerced.
func SaveChanges(path string, changes map[string]any) error {
	v := viper.New()
	v.SetConfigType("yaml")
	v.SetConfigFile(path)
	if err := v.ReadInConfig(); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("reading %s: %w", path, err)
	}
	for k, val := range changes {
		v.Set(k, val)
	}
	if err := v.WriteConfigAs(path); err != nil {
		return fmt.Errorf("writing %s: %w", path, err)
	}
	if err := os.Chmod(path, 0o600); err != nil {
		return fmt.Errorf("chmod %s: %w", path, err)
	}
	return nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd backend && CGO_ENABLED=0 go test ./internal/config/ -run TestFileKeysAndSaveChanges -v`
Expected: PASS. (Note: `LoadFrom` must load `dlp.api_key` — it already does, see config.go:165.)

- [ ] **Step 5: Commit**

```bash
git add backend/internal/config/sources.go backend/internal/config/sources_test.go
git commit -m "feat(config): file-key reader + merge-write SaveChanges (#180)"
```

---

## Task 5: Provisioned DLP snapshot in Config

**Files:**
- Modify: `backend/internal/config/config.go`
- Test: `backend/internal/config/config_test.go` (create if absent)

- [ ] **Step 1: Write the failing test**

Create or append to `backend/internal/config/config_test.go`:

```go
package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadAndSaveProvisionedSnapshot(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")

	cfg := &Config{}
	cfg.Server.Port = 8080
	cfg.ProvisionedDLP.Recorded = true
	cfg.ProvisionedDLP.EmbeddingModel = "text-embedding-3-small"
	cfg.ProvisionedDLP.EmbeddingDim = 1536
	cfg.ProvisionedDLP.RiskModel = "gpt-4o-mini"
	cfg.ProvisionedDLP.SummaryModel = "gpt-4o"

	if err := Save(cfg, path); err != nil {
		t.Fatalf("Save: %v", err)
	}

	got, err := LoadFrom(path)
	if err != nil {
		t.Fatalf("LoadFrom: %v", err)
	}
	if !got.ProvisionedDLP.Recorded {
		t.Error("Recorded not round-tripped")
	}
	if got.ProvisionedDLP.EmbeddingDim != 1536 {
		t.Errorf("EmbeddingDim = %d, want 1536", got.ProvisionedDLP.EmbeddingDim)
	}
	if got.ProvisionedDLP.RiskModel != "gpt-4o-mini" {
		t.Errorf("RiskModel = %q", got.ProvisionedDLP.RiskModel)
	}

	// Save must NOT persist dlp.api_key (avoid duplicating env secret).
	cfg.DLP.APIKey = "sk-should-not-write"
	if err := Save(cfg, path); err != nil {
		t.Fatal(err)
	}
	keys, _ := FileKeys(path)
	if keys["dlp.api_key"] {
		t.Error("Save wrote dlp.api_key; it must not")
	}
}

func TestLoadFromMissingSnapshotDefaults(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte("server:\n  port: 8080\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := LoadFrom(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.ProvisionedDLP.Recorded {
		t.Error("Recorded should default to false when absent")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd backend && CGO_ENABLED=0 go test ./internal/config/ -run 'Provisioned|MissingSnapshot' -v`
Expected: FAIL — `cfg.ProvisionedDLP undefined`

- [ ] **Step 3: Write the implementation**

In `backend/internal/config/config.go`, add the struct after `DLPConfig` (around line 51):

```go
// ProvisionedDLP records the DLP model settings that were baked into UDFs and
// the enriched stream at the last successful provision. Used to detect drift
// ("re-provision required") when the live DLP config changes afterward.
type ProvisionedDLP struct {
	Recorded       bool   `mapstructure:"recorded"`
	EmbeddingModel string `mapstructure:"embedding_model"`
	EmbeddingDim   int    `mapstructure:"embedding_dim"`
	RiskModel      string `mapstructure:"risk_model"`
	SummaryModel   string `mapstructure:"summary_model"`
}
```

Add the field to `Config` (after the `DLP` field, ~line 60):

```go
	ProvisionedDLP ProvisionedDLP // loaded from provisioned.dlp.* in config.yaml
```

In `load()`, add defaults next to the other `v.SetDefault` lines (~line 115):

```go
	v.SetDefault("provisioned.dlp.recorded", false)
	v.SetDefault("provisioned.dlp.embedding_model", "")
	v.SetDefault("provisioned.dlp.embedding_dim", 0)
	v.SetDefault("provisioned.dlp.risk_model", "")
	v.SetDefault("provisioned.dlp.summary_model", "")
```

In `load()`, populate the struct after the DLP block (~line 169):

```go
	cfg.ProvisionedDLP.Recorded = v.GetBool("provisioned.dlp.recorded")
	cfg.ProvisionedDLP.EmbeddingModel = v.GetString("provisioned.dlp.embedding_model")
	cfg.ProvisionedDLP.EmbeddingDim = v.GetInt("provisioned.dlp.embedding_dim")
	cfg.ProvisionedDLP.RiskModel = v.GetString("provisioned.dlp.risk_model")
	cfg.ProvisionedDLP.SummaryModel = v.GetString("provisioned.dlp.summary_model")
```

In `Save()`, add snapshot writes before `return v.WriteConfigAs(path)` (~line 229). Do **not** add a `dlp.api_key` line:

```go
	v.Set("provisioned.dlp.recorded", cfg.ProvisionedDLP.Recorded)
	v.Set("provisioned.dlp.embedding_model", cfg.ProvisionedDLP.EmbeddingModel)
	v.Set("provisioned.dlp.embedding_dim", cfg.ProvisionedDLP.EmbeddingDim)
	v.Set("provisioned.dlp.risk_model", cfg.ProvisionedDLP.RiskModel)
	v.Set("provisioned.dlp.summary_model", cfg.ProvisionedDLP.SummaryModel)
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd backend && CGO_ENABLED=0 go test ./internal/config/ -v`
Expected: PASS (all config tests)

- [ ] **Step 5: Commit**

```bash
git add backend/internal/config/config.go backend/internal/config/config_test.go
git commit -m "feat(config): persist provisioned DLP snapshot for drift detection (#180)"
```

---

## Task 6: GET /api/config handler

**Files:**
- Create: `backend/internal/handlers/settings.go`
- Test: `backend/internal/handlers/settings_test.go`

- [ ] **Step 1: Write the failing test**

Create `backend/internal/handlers/settings_test.go`:

```go
package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/timeplus/agentguard/internal/config"
)

func newConfigForTest(t *testing.T) *config.Config {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte("dlp:\n  base_url: http://seed:4000/v1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.LoadFrom(path)
	if err != nil {
		t.Fatal(err)
	}
	cfg.ConfigPath = path
	return cfg
}

func TestGetConfigShapeAndSecrets(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cfg := newConfigForTest(t)
	cfg.DLP.APIKey = "sk-secret"

	r := gin.New()
	r.GET("/api/config", GetConfig(cfg, map[string]bool{}))

	req := httptest.NewRequest(http.MethodGet, "/api/config", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	var resp struct {
		RestartRequired     bool `json:"restart_required"`
		ReprovisionRequired bool `json:"reprovision_required"`
		Sections            []struct {
			ID   string `json:"id"`
			Keys []struct {
				Key      string `json:"key"`
				Type     string `json:"type"`
				Value    any    `json:"value"`
				Set      bool   `json:"set"`
				Source   string `json:"source"`
				Editable bool   `json:"editable"`
			} `json:"keys"`
		} `json:"sections"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	var sawSecret, sawFileSource bool
	for _, s := range resp.Sections {
		for _, k := range s.Keys {
			if k.Key == "dlp.api_key" {
				sawSecret = true
				if k.Value != nil {
					t.Error("secret value must not be echoed")
				}
				if !k.Set {
					t.Error("secret should report set=true")
				}
			}
			if k.Key == "dlp.base_url" {
				if k.Source != "file" {
					t.Errorf("dlp.base_url source = %q, want file", k.Source)
				}
				sawFileSource = true
			}
			if k.Key == "setup.complete" && k.Editable {
				t.Error("setup.complete must not be editable")
			}
		}
	}
	if !sawSecret || !sawFileSource {
		t.Error("expected dlp.api_key and dlp.base_url in response")
	}
}

func TestGetConfigEnvLocksKey(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cfg := newConfigForTest(t)
	t.Setenv("DLP_API_KEY", "sk-env")

	r := gin.New()
	r.GET("/api/config", GetConfig(cfg, map[string]bool{}))
	req := httptest.NewRequest(http.MethodGet, "/api/config", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	var resp struct {
		Sections []struct {
			Keys []struct {
				Key      string `json:"key"`
				Source   string `json:"source"`
				Editable bool   `json:"editable"`
				EnvVar   string `json:"env_var"`
			} `json:"keys"`
		} `json:"sections"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	for _, s := range resp.Sections {
		for _, k := range s.Keys {
			if k.Key == "dlp.api_key" {
				if k.Source != "env" || k.Editable {
					t.Errorf("env-overridden key: source=%q editable=%v", k.Source, k.Editable)
				}
				if k.EnvVar != "DLP_API_KEY" {
					t.Errorf("env_var = %q", k.EnvVar)
				}
			}
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd backend && CGO_ENABLED=0 go test ./internal/handlers/ -run TestGetConfig -v`
Expected: FAIL — `undefined: GetConfig`

- [ ] **Step 3: Write the implementation**

Create `backend/internal/handlers/settings.go`:

```go
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
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd backend && CGO_ENABLED=0 go test ./internal/handlers/ -run TestGetConfig -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add backend/internal/handlers/settings.go backend/internal/handlers/settings_test.go
git commit -m "feat(api): GET /api/config with sources + restart/reprovision flags (#180)"
```

---

## Task 7: PUT /api/config handler

**Files:**
- Modify: `backend/internal/handlers/settings.go`
- Test: `backend/internal/handlers/settings_test.go`

- [ ] **Step 1: Write the failing test**

Append to `backend/internal/handlers/settings_test.go` (add `"bytes"`, `"strings"` to imports):

```go
func putConfig(t *testing.T, cfg *config.Config, body string) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.PUT("/api/config", UpdateConfig(cfg, map[string]bool{}))
	req := httptest.NewRequest(http.MethodPut, "/api/config", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func TestUpdateConfigHotApplyAndPersist(t *testing.T) {
	cfg := newConfigForTest(t)
	w := putConfig(t, cfg, `{"dlp.enabled": true, "dlp.api_key": "sk-new"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	// Hot-applied to live cfg.
	if !cfg.DLP.Enabled || cfg.DLP.APIKey != "sk-new" {
		t.Errorf("live cfg not hot-applied: %+v", cfg.DLP)
	}
	// Persisted to file.
	reloaded, _ := config.LoadFrom(cfg.ConfigPath)
	if !reloaded.DLP.Enabled || reloaded.DLP.APIKey != "sk-new" {
		t.Error("changes not persisted to file")
	}
}

func TestUpdateConfigRestartKeyNotHotApplied(t *testing.T) {
	cfg := newConfigForTest(t)
	orig := cfg.Server.Port
	w := putConfig(t, cfg, `{"server.port": 9099}`)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	// Restart key persisted but NOT hot-applied to the live cfg.
	if cfg.Server.Port != orig {
		t.Errorf("restart key was hot-applied: live port = %d", cfg.Server.Port)
	}
	reloaded, _ := config.LoadFrom(cfg.ConfigPath)
	if reloaded.Server.Port != 9099 {
		t.Errorf("restart key not persisted: %d", reloaded.Server.Port)
	}
	var resp struct {
		RestartRequired bool `json:"restart_required"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if !resp.RestartRequired {
		t.Error("expected restart_required=true after changing server.port")
	}
}

func TestUpdateConfigRejectsInvalidAtomically(t *testing.T) {
	cfg := newConfigForTest(t)
	w := putConfig(t, cfg, `{"dlp.enabled": true, "server.port": 0}`)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
	var resp struct {
		Errors map[string]string `json:"errors"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if _, ok := resp.Errors["server.port"]; !ok {
		t.Errorf("expected server.port error, got %v", resp.Errors)
	}
	// Atomic: the valid key in the batch must NOT have been applied.
	if cfg.DLP.Enabled {
		t.Error("partial apply: dlp.enabled changed despite batch error")
	}
}

func TestUpdateConfigRejectsReadOnlyAndOverridden(t *testing.T) {
	cfg := newConfigForTest(t)
	t.Setenv("DLP_BASE_URL", "http://env:4000/v1")

	w := putConfig(t, cfg, `{"setup.complete": false}`)
	if w.Code != http.StatusBadRequest {
		t.Errorf("read-only key: status = %d, want 400", w.Code)
	}
	w2 := putConfig(t, cfg, `{"dlp.base_url": "http://x:1/v1"}`)
	if w2.Code != http.StatusBadRequest {
		t.Errorf("env-overridden key: status = %d, want 400", w2.Code)
	}
}

func TestUpdateConfigClearSecret(t *testing.T) {
	cfg := newConfigForTest(t)
	cfg.DLP.APIKey = "sk-old"
	w := putConfig(t, cfg, `{"dlp.api_key": ""}`)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	if cfg.DLP.APIKey != "" {
		t.Error("empty secret should clear the value")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd backend && CGO_ENABLED=0 go test ./internal/handlers/ -run TestUpdateConfig -v`
Expected: FAIL — `undefined: UpdateConfig`

- [ ] **Step 3: Write the implementation**

Append to `backend/internal/handlers/settings.go`:

```go
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
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd backend && CGO_ENABLED=0 go test ./internal/handlers/ -run TestUpdateConfig -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add backend/internal/handlers/settings.go backend/internal/handlers/settings_test.go
git commit -m "feat(api): PUT /api/config atomic validate + merge-write + hot-apply (#180)"
```

---

## Task 8: Test endpoints (test-timeplus, test-dlp)

**Files:**
- Modify: `backend/internal/handlers/settings.go`
- Test: `backend/internal/handlers/settings_test.go`

- [ ] **Step 1: Write the failing test**

Append to `backend/internal/handlers/settings_test.go` (add `"net/http/httptest"` already imported; add `httptest.NewServer` usage):

```go
func TestTestDLPConfig(t *testing.T) {
	// Stub an OpenAI-compatible /models endpoint.
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/models" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if r.Header.Get("Authorization") != "Bearer sk-test" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"data":[]}`))
	}))
	defer upstream.Close()

	cfg := newConfigForTest(t)
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.POST("/api/config/test-dlp", TestDLPConfig(cfg))

	body := `{"base_url":"` + upstream.URL + `","api_key":"sk-test"}`
	req := httptest.NewRequest(http.MethodPost, "/api/config/test-dlp", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	var resp struct {
		OK bool `json:"ok"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if !resp.OK {
		t.Errorf("expected ok=true, body = %s", w.Body.String())
	}

	// Wrong key → ok:false, still HTTP 200.
	bad := `{"base_url":"` + upstream.URL + `","api_key":"wrong"}`
	req2 := httptest.NewRequest(http.MethodPost, "/api/config/test-dlp", strings.NewReader(bad))
	req2.Header.Set("Content-Type", "application/json")
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, req2)
	if w2.Code != http.StatusOK {
		t.Errorf("unreachable/auth-fail should be 200 with ok:false, got %d", w2.Code)
	}
	var resp2 struct {
		OK bool `json:"ok"`
	}
	_ = json.Unmarshal(w2.Body.Bytes(), &resp2)
	if resp2.OK {
		t.Error("expected ok=false for wrong key")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd backend && CGO_ENABLED=0 go test ./internal/handlers/ -run TestTestDLPConfig -v`
Expected: FAIL — `undefined: TestDLPConfig`

- [ ] **Step 3: Write the implementation**

Append to `backend/internal/handlers/settings.go` (add imports: `"io"`, `"net/url"`, `"strings"`, `"time"`, `"github.com/timeplus/agentguard/internal/auth"`, `tp "github.com/timeplus/agentguard/internal/timeplus"`):

```go
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

// TestDLPConfig checks that the DLP LLM endpoint is reachable by GETting
// base_url + "/models" with the bearer key. An absent api_key falls back to the
// currently-configured one. Body: {"base_url":"...","api_key":"..."}.
// Reachability failures return HTTP 200 with ok:false (never 5xx).
func TestDLPConfig(cfg *config.Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		var body struct {
			BaseURL string `json:"base_url" binding:"required"`
			APIKey  string `json:"api_key"`
		}
		if err := c.ShouldBindJSON(&body); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": err.Error()})
			return
		}
		key := body.APIKey
		if key == "" {
			config.Mu.RLock()
			key = cfg.DLP.APIKey
			config.Mu.RUnlock()
		}
		target := strings.TrimRight(body.BaseURL, "/") + "/models"
		if _, err := url.ParseRequestURI(target); err != nil {
			c.JSON(http.StatusOK, gin.H{"ok": false, "error": "invalid base_url"})
			return
		}

		client := &http.Client{Timeout: 8 * time.Second}
		req, err := http.NewRequestWithContext(c.Request.Context(), http.MethodGet, target, nil)
		if err != nil {
			c.JSON(http.StatusOK, gin.H{"ok": false, "error": err.Error()})
			return
		}
		if key != "" {
			req.Header.Set("Authorization", "Bearer "+key)
		}
		resp, err := client.Do(req)
		if err != nil {
			c.JSON(http.StatusOK, gin.H{"ok": false, "error": err.Error()})
			return
		}
		defer resp.Body.Close()
		_, _ = io.Copy(io.Discard, resp.Body)
		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			c.JSON(http.StatusOK, gin.H{"ok": true})
			return
		}
		c.JSON(http.StatusOK, gin.H{"ok": false, "error": "endpoint returned HTTP " + resp.Status})
	}
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd backend && CGO_ENABLED=0 go test ./internal/handlers/ -run TestTestDLPConfig -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add backend/internal/handlers/settings.go backend/internal/handlers/settings_test.go
git commit -m "feat(api): test-timeplus + test-dlp config validation endpoints (#180)"
```

---

## Task 9: Wire routes + flags + provision snapshot

**Files:**
- Modify: `backend/cmd/server/main.go`
- Modify: `backend/internal/handlers/setup.go`

- [ ] **Step 1: Record the DLP snapshot in Provision**

In `backend/internal/handlers/setup.go`, inside `Provision`, set the snapshot on `newCfg` before `config.Save` (after the existing `newCfg.SetupComplete = true`, ~line 120):

```go
		newCfg.ProvisionedDLP = config.ProvisionedDLP{
			Recorded:       true,
			EmbeddingModel: cfg.DLP.EmbeddingModel,
			EmbeddingDim:   cfg.DLP.EmbeddingDim,
			RiskModel:      cfg.DLP.RiskModel,
			SummaryModel:   cfg.DLP.SummaryModel,
		}
```

And mirror it onto the live cfg inside the existing `config.Mu.Lock()` block (after `cfg.SetupComplete = true`, ~line 132):

```go
		cfg.ProvisionedDLP = newCfg.ProvisionedDLP
```

- [ ] **Step 2: Collect changed flags + register routes in main.go**

In `backend/cmd/server/main.go`, inside `runServer`, after `cfg.ConfigPath` is set (~line 101), add:

```go
	// Capture which CLI flags were explicitly set, so the config UI can mark
	// flag-overridden keys read-only and show the correct source.
	changedFlags := map[string]bool{}
	cmd.Flags().Visit(func(f *pflag.Flag) {
		changedFlags[f.Name] = true
	})
```

Add the pflag import to the import block:

```go
	"github.com/spf13/pflag"
```

Register the four routes inside the `protected` group (after the existing `/dlp/...` routes, ~line 201):

```go
				protected.GET("/config", handlers.GetConfig(cfg, changedFlags))
				protected.PUT("/config", handlers.UpdateConfig(cfg, changedFlags))
				protected.POST("/config/test-timeplus", handlers.TestTimeplusConfig(cfg))
				protected.POST("/config/test-dlp", handlers.TestDLPConfig(cfg))
```

- [ ] **Step 3: Verify it compiles and the suite passes**

Run: `cd backend && CGO_ENABLED=0 go build ./... && CGO_ENABLED=0 go test ./...`
Expected: build succeeds; all tests PASS.

- [ ] **Step 4: Manual smoke (optional but recommended)**

Run: `cd backend && CGO_ENABLED=0 go build -o server cmd/server/main.go && ./server --config /tmp/ag-test.yaml &` then:
`curl -s http://localhost:8080/api/config` → expect HTTP 401 (no session) — confirms the route is registered behind auth. Stop the server afterward.

- [ ] **Step 5: Commit**

```bash
git add backend/cmd/server/main.go backend/internal/handlers/setup.go
git commit -m "feat(api): register /api/config routes + record DLP provision snapshot (#180)"
```

---

## Task 10: Frontend API client

**Files:**
- Modify: `frontend/src/api/client.js`

- [ ] **Step 1: Add the API functions**

In `frontend/src/api/client.js`, add to the `api` object (near the DLP section; `get`, `post` helpers already exist; add a `put` helper if absent). First add a `put` helper after the `post` function (~line 28):

```js
async function put(path, body) {
  const res = await fetch(`${BASE}${path}`, {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(body),
    credentials: 'include',
  })
  if (!res.ok) {
    let payload = null
    try { payload = await res.json() } catch { /* ignore */ }
    const err = new Error(payload?.error || `HTTP ${res.status}`)
    err.status = res.status
    err.errors = payload?.errors || null // per-key validation errors
    throw err
  }
  return res.json()
}
```

Then add to the `api` object:

```js
  // ── Configuration (Settings page) ───────────────────────────────────────────
  getConfig: () => get('/config'),
  updateConfig: (changes) => put('/config', changes),
  testTimeplusConfig: (host, port) => post('/config/test-timeplus', { host, port }),
  testDLPConfig: (base_url, api_key) => post('/config/test-dlp', { base_url, api_key }),
```

- [ ] **Step 2: Verify build/lint**

Run: `cd frontend && npm run lint`
Expected: no new lint errors.

- [ ] **Step 3: Commit**

```bash
git add frontend/src/api/client.js
git commit -m "feat(web): config API client functions (#180)"
```

---

## Task 11: Settings page

**Files:**
- Create: `frontend/src/pages/Settings.jsx`

- [ ] **Step 1: Create the page**

Create `frontend/src/pages/Settings.jsx`:

```jsx
import { useState, useEffect, useCallback } from 'react'
import Sidebar from '../components/Sidebar'
import TopNav from '../components/TopNav'
import PageHeader from '../components/PageHeader'
import Toggle from '../components/Toggle'
import { api } from '../api/client'

const SOURCE_LABEL = {
  default: 'default',
  file: 'config file',
  env: 'environment',
  flag: 'CLI flag',
}

function SourceChip({ k }) {
  const label = SOURCE_LABEL[k.source] || k.source
  const title =
    k.source === 'env' ? `Set by ${k.env_var} — file edits have no effect until the variable is removed`
    : k.source === 'flag' ? `Set by --${k.flag} — file edits have no effect`
    : undefined
  return (
    <span title={title} className="text-[10px] uppercase tracking-wide rounded-full px-2 py-0.5 bg-gray-700 text-gray-300">
      {label}
    </span>
  )
}

function KeyField({ k, draftValue, onChange }) {
  const disabled = !k.editable
  const value = draftValue
  const baseInput =
    'w-full bg-gray-900 text-gray-100 text-sm rounded-sm px-3 py-2 outline-none focus:bg-gray-800 disabled:opacity-50 disabled:cursor-not-allowed'

  let input
  if (k.type === 'bool') {
    input = <Toggle checked={!!value} onChange={(v) => onChange(v)} disabled={disabled} />
  } else if (k.type === 'enum') {
    input = (
      <select className={baseInput} value={value ?? ''} disabled={disabled} onChange={(e) => onChange(e.target.value)}>
        {k.enum_values?.map((opt) => <option key={opt} value={opt}>{opt}</option>)}
      </select>
    )
  } else if (k.type === 'secret') {
    input = (
      <input
        type="password"
        className={baseInput}
        disabled={disabled}
        placeholder={k.set ? '•••••••• (set)' : 'not set'}
        value={value ?? ''}
        onChange={(e) => onChange(e.target.value)}
      />
    )
  } else if (k.type === 'int') {
    input = (
      <input
        type="number"
        className={baseInput}
        disabled={disabled}
        value={value ?? ''}
        onChange={(e) => onChange(e.target.value === '' ? '' : Number(e.target.value))}
      />
    )
  } else {
    input = (
      <input
        type="text"
        className={baseInput}
        disabled={disabled}
        value={value ?? ''}
        onChange={(e) => onChange(e.target.value)}
      />
    )
  }

  return (
    <div className="py-3">
      <div className="flex items-center justify-between gap-3 mb-1">
        <div className="flex items-center gap-2">
          <span className="text-sm text-gray-100">{k.label}</span>
          <SourceChip k={k} />
          {k.restart && <span className="text-[10px] text-amber-400">requires restart</span>}
          {k.reprovision && <span className="text-[10px] text-amber-400">requires re-provision</span>}
        </div>
      </div>
      <p className="text-xs text-gray-400 mb-2">{k.description}</p>
      <div className="max-w-md">{input}</div>
    </div>
  )
}

export default function Settings() {
  const [data, setData] = useState(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState(null)
  const [drafts, setDrafts] = useState({}) // key -> new value
  const [fieldErrors, setFieldErrors] = useState({})
  const [saving, setSaving] = useState(false)
  const [banner, setBanner] = useState(null) // {type, msg}
  const [tpTest, setTpTest] = useState(null)
  const [dlpTest, setDlpTest] = useState(null)
  const [reprovisioning, setReprovisioning] = useState(false)

  const load = useCallback(() => {
    setLoading(true)
    api.getConfig()
      .then((d) => { setData(d); setError(null) })
      .catch((e) => setError(e.message))
      .finally(() => setLoading(false))
  }, [])

  useEffect(() => { load() }, [load])

  const setDraft = (key, value) => {
    setDrafts((d) => ({ ...d, [key]: value }))
    setFieldErrors((e) => { const n = { ...e }; delete n[key]; return n })
  }

  const dirtyKeys = Object.keys(drafts)

  const save = async () => {
    setSaving(true)
    setFieldErrors({})
    try {
      const res = await api.updateConfig(drafts)
      setDrafts({})
      setBanner(res.restart_required
        ? { type: 'warn', msg: 'Saved. Some changes require an AgentGuard restart to take effect.' }
        : { type: 'ok', msg: 'Configuration saved.' })
      load()
    } catch (e) {
      if (e.errors) setFieldErrors(e.errors)
      setBanner({ type: 'error', msg: e.message })
    } finally {
      setSaving(false)
    }
  }

  const discard = () => { setDrafts({}); setFieldErrors({}) }

  const draftFor = (k) => (k.key in drafts ? drafts[k.key] : (k.type === 'secret' ? '' : k.value))

  const testTimeplus = async () => {
    const host = drafts['timeplus.host'] ?? findValue('timeplus.host')
    const port = drafts['timeplus.port'] ?? findValue('timeplus.port')
    setTpTest({ pending: true })
    try {
      const r = await api.testTimeplusConfig(host, Number(port))
      setTpTest(r)
    } catch (e) { setTpTest({ ok: false, error: e.message }) }
  }

  const testDLP = async () => {
    const base = drafts['dlp.base_url'] ?? findValue('dlp.base_url')
    const key = drafts['dlp.api_key'] ?? ''
    setDlpTest({ pending: true })
    try {
      const r = await api.testDLPConfig(base, key)
      setDlpTest(r)
    } catch (e) { setDlpTest({ ok: false, error: e.message }) }
  }

  const reprovisionDLP = async () => {
    setReprovisioning(true)
    try {
      await api.syncRules()
      setBanner({ type: 'ok', msg: 'DLP re-provision triggered.' })
      load()
    } catch (e) {
      setBanner({ type: 'error', msg: `Re-provision failed: ${e.message}` })
    } finally {
      setReprovisioning(false)
    }
  }

  const findValue = (key) => {
    for (const s of data?.sections || []) {
      for (const k of s.keys) if (k.key === key) return k.value
    }
    return ''
  }

  return (
    <div className="min-h-screen bg-[#15121d] text-gray-100">
      <Sidebar />
      <TopNav />
      <main className="pl-sidebar pt-16 pb-24 min-h-screen">
        <div className="p-8 max-w-4xl">
          <PageHeader title="Settings" subtitle="View and edit AgentGuard server configuration. Keys set by environment variables or CLI flags are read-only." />

          {loading && <p className="text-gray-400 text-sm">Loading…</p>}
          {error && <p className="text-error text-sm">{error}</p>}

          {banner && (
            <div className={`mb-6 rounded-sm px-4 py-3 text-sm ${
              banner.type === 'error' ? 'bg-red-900/40 text-red-300'
              : banner.type === 'warn' ? 'bg-amber-900/40 text-amber-200'
              : 'bg-emerald-900/40 text-emerald-200'}`}>
              {banner.msg}
            </div>
          )}

          {data?.restart_required && (
            <div className="mb-6 rounded-sm px-4 py-3 text-sm bg-amber-900/40 text-amber-200">
              Saved configuration differs from the running server — restart AgentGuard to apply.
            </div>
          )}

          {data?.reprovision_required && (
            <div className="mb-6 rounded-sm px-4 py-3 text-sm bg-amber-900/40 text-amber-200 flex items-center justify-between gap-4">
              <span>DLP model settings differ from what's provisioned. Re-provision to apply baked-in model/dimension changes. (Changing the embedding dimension does not recreate the existing stream automatically.)</span>
              <button onClick={reprovisionDLP} disabled={reprovisioning}
                className="shrink-0 rounded-sm px-3 py-1.5 text-xs bg-gradient-to-br from-[#ffb0cf] to-[#f256a3] text-[#3d0023] font-medium disabled:opacity-60">
                {reprovisioning ? 'Re-provisioning…' : 'Re-provision DLP now'}
              </button>
            </div>
          )}

          {data?.sections?.map((section) => (
            <section key={section.id} className="mb-6 bg-gray-800 rounded-sm px-6 py-4">
              <h3 className="text-base font-medium text-gray-100 mb-2">{section.title}</h3>
              <div className="divide-y divide-gray-700/50">
                {section.keys.map((k) => (
                  <div key={k.key}>
                    <KeyField k={k} draftValue={draftFor(k)} onChange={(v) => setDraft(k.key, v)} />
                    {fieldErrors[k.key] && <p className="text-error text-xs pb-2">{fieldErrors[k.key]}</p>}
                  </div>
                ))}
              </div>

              {section.id === 'timeplus' && (
                <div className="mt-3 flex items-center gap-3">
                  <button onClick={testTimeplus} className="rounded-sm px-3 py-1.5 text-xs bg-gray-700 text-gray-100 hover:bg-gray-600">Test connection</button>
                  {tpTest?.pending && <span className="text-xs text-gray-400">Testing…</span>}
                  {tpTest && !tpTest.pending && (
                    <span className={`text-xs ${tpTest.ok ? 'text-emerald-400' : 'text-error'}`}>
                      {tpTest.ok ? 'Connection OK' : `Failed: ${tpTest.error}`}
                    </span>
                  )}
                </div>
              )}

              {section.id === 'dlp' && (
                <div className="mt-3 flex items-center gap-3">
                  <button onClick={testDLP} className="rounded-sm px-3 py-1.5 text-xs bg-gray-700 text-gray-100 hover:bg-gray-600">Test endpoint</button>
                  {dlpTest?.pending && <span className="text-xs text-gray-400">Testing…</span>}
                  {dlpTest && !dlpTest.pending && (
                    <span className={`text-xs ${dlpTest.ok ? 'text-emerald-400' : 'text-error'}`}>
                      {dlpTest.ok ? 'Endpoint reachable' : `Failed: ${dlpTest.error}`}
                    </span>
                  )}
                </div>
              )}
            </section>
          ))}
        </div>

        {dirtyKeys.length > 0 && (
          <div className="fixed bottom-0 left-0 right-0 pl-sidebar bg-gray-800/95 backdrop-blur-md py-3 z-30 flex items-center justify-between [&>*:first-child]:pl-10 [&>*:last-child]:pr-10">
            <span className="text-sm text-gray-300">{dirtyKeys.length} unsaved change{dirtyKeys.length > 1 ? 's' : ''}</span>
            <div className="flex gap-3">
              <button onClick={discard} disabled={saving} className="rounded-sm px-4 py-2 text-sm bg-gray-700 text-gray-100 hover:bg-gray-600">Discard</button>
              <button onClick={save} disabled={saving}
                className="rounded-sm px-4 py-2 text-sm bg-gradient-to-br from-[#ffb0cf] to-[#f256a3] text-[#3d0023] font-medium disabled:opacity-60">
                {saving ? 'Saving…' : 'Save'}
              </button>
            </div>
          </div>
        )}
      </main>
      <Footer />
    </div>
  )
}
```

- [ ] **Step 2: Verify lint**

Run: `cd frontend && npm run lint`
Expected: no new errors. (If `Footer` import path differs, check `frontend/src/components/` and adjust; the import must resolve.)

- [ ] **Step 3: Commit**

```bash
git add frontend/src/pages/Settings.jsx
git commit -m "feat(web): Settings page — metadata-driven config editor (#180)"
```

---

## Task 12: Route + sidebar nav

**Files:**
- Modify: `frontend/src/App.jsx`
- Modify: `frontend/src/components/Sidebar.jsx`

- [ ] **Step 1: Register the route**

In `frontend/src/App.jsx`, add the import next to the other page imports:

```jsx
import Settings from './pages/Settings'
```

Add the route inside `<Routes>` (after the `/dlp` route):

```jsx
            <Route path="/settings" element={<Settings />} />
```

- [ ] **Step 2: Add the sidebar nav item**

In `frontend/src/components/Sidebar.jsx`, append to `navItems`:

```jsx
  { label: 'Settings', to: '/settings', icon: 'settings' },
```

- [ ] **Step 3: Verify build**

Run: `cd frontend && npm run build`
Expected: production build succeeds.

- [ ] **Step 4: Manual verification**

Run `make dev` (backend + frontend). Log in, click **Settings** in the sidebar. Confirm:
- All sections render with current values and source chips.
- A key with `DLP_API_KEY`/`TIMEPLUS_USER` env set shows as read-only "environment".
- Editing `dlp.enabled` + `dlp.base_url`, Save → success; reload shows persisted values.
- Changing `server.port` → restart banner appears.
- "Test connection" and "Test endpoint" buttons return inline results.

- [ ] **Step 5: Commit**

```bash
git add frontend/src/App.jsx frontend/src/components/Sidebar.jsx
git commit -m "feat(web): /settings route + sidebar nav item (#180)"
```

---

## Task 13: Docs

**Files:**
- Modify: `CLAUDE.md`

- [ ] **Step 1: Document the new endpoints**

In `CLAUDE.md`, add to the API Endpoints table (after the `/api/proton/query` row):

```markdown
| GET | `/api/config` | `GetConfig` | Full effective config with per-key source (default/file/env/flag), restart/reprovision flags |
| PUT | `/api/config` | `UpdateConfig` | Atomic validate + merge-write changed keys to config.yaml; hot-applies non-restart keys |
| POST | `/api/config/test-timeplus` | `TestTimeplusConfig` | Tests a candidate Timeplus host/port with session credentials |
| POST | `/api/config/test-dlp` | `TestDLPConfig` | Tests DLP base_url reachability (GET /models) |
```

Add to the Routes table (after the `/dlp` row if present, else after `/investigate`):

```markdown
| `/settings` | `Settings` |
```

- [ ] **Step 2: Final full verification**

Run:
```bash
cd backend && CGO_ENABLED=0 go build ./... && CGO_ENABLED=0 go test ./...
cd ../frontend && npm run lint && npm run build
```
Expected: backend builds + all tests pass; frontend lints + builds.

- [ ] **Step 3: Commit**

```bash
git add CLAUDE.md
git commit -m "docs: document /api/config endpoints + /settings route (#180)"
```

---

## Self-Review Notes (addressed during planning)

- **Spec coverage:** hot-reload vs restart (registry `Restart` + banner) ✓; secrets write-only (`dlp.api_key` secret, GET masks, merge-write persists) ✓; precedence display + lock (`ResolveSource`, `editable`) ✓; re-provision drift (`ProvisionedDLP` snapshot + `/api/rules/sync` button) ✓; validation/test actions (test-timeplus, test-dlp) ✓; auth (protected group) ✓.
- **Re-provision correctness:** `/setup/provision` 409s post-setup, so the UI uses `POST /api/rules/sync` (`SyncDLP`) which the existing `api.syncRules()` already calls. ✓
- **Type consistency:** `KeyType`/`Source` constants, `CoerceAndValidate`/`ApplyChange`/`EffectiveValue`/`SaveChanges`/`FileKeys`/`buildConfigView` signatures are used identically across backend tasks. JSON numbers (`float64`) coerced to `int` in one place (`toInt`). ✓
- **Known limitation (documented):** deployments provisioned before this feature have no `provisioned.dlp.*` snapshot, so `reprovision_required` stays false until the next provision; `embedding_dim` changes are not auto-applied to an existing stream.
