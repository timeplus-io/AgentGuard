# DLP First-Time Enable Config + Validation Gate — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make the first-time Semantic DLP enable flow force the operator to configure the model endpoint + models and validate that every distinct model actually responds, provisioning the pipeline only after a green validation.

**Architecture:** The single "Enable Semantic DLP" button on `/dlp` becomes an inline config + validate card (`DLPEnableWizard`) shown whenever DLP is not fully provisioned. It prefills from the `config.yaml` seed values (`api.getConfig()`), validates candidate values via the existing `POST /api/config/test-dlp` endpoint (extended to validate multiple chat models and to accept "dimension unknown"), and — only after a pass — writes the seed config keys via `api.updateConfig` and provisions via `api.syncRules`. Post-provision editing is unchanged (Settings `DLPConfigEditor`).

**Tech Stack:** Go 1.x / Gin (backend), React 18 + Vite + Tailwind (frontend), Vitest (frontend unit tests).

## Global Constraints

- Build the backend with `CGO_ENABLED=0` (macOS 26+ Apple Silicon requires a static binary): `CGO_ENABLED=0 go build ./...`, `CGO_ENABLED=0 go test ./...`.
- No mock data layer; do not add one.
- Every DLP UDF fails open — this feature adds a *pre-provision* gate; it must not change the fail-open runtime behavior.
- `POST /api/config/test-dlp` must never return 5xx on an unreachable/misconfigured endpoint — failures are reported in-band with HTTP 200.
- Follow the "Digital Sentry" dark theme already used in `SemanticDLP.jsx` (surfaces `#211e29`/`#2c2834`, gradient CTA `from-[#ffb0cf] to-[#f256a3]` with `text-[#3d0023]`, `rounded-sm`).
- Backend response back-compat: the existing single `checks.chat` object must remain present (the Settings editor's Test button reads it).

---

## Task 1: Backend — validate multiple chat models + unknown embedding dimension

**Files:**
- Modify: `backend/internal/handlers/settings.go` (`TestDLPConfig` ~L233-280, `dlpCheckEmbedding` ~L306-328; add `dedupeNonEmpty` helper)
- Test: `backend/internal/handlers/settings_test.go`

**Interfaces:**
- Consumes: existing `dlpCheckReachable`, `dlpCheckEmbedding`, `dlpCheckChat`, `orStr`.
- Produces: `POST /api/config/test-dlp` now accepts optional request field `chat_models: []string`. Response `checks` gains `chats: [{model, ok, note, thinking_detected, ...}]`; `checks.chat` remains equal to `chats[0]`. When request field `embedding_dim` is `<= 0`, the embedding check passes if a non-empty vector is returned (no exact-dimension match). Behavior with `chat_models` omitted and `embedding_dim` omitted/positive is unchanged.

- [ ] **Step 1: Write the failing tests**

Add to `backend/internal/handlers/settings_test.go`:

```go
func TestDLPConfigMultipleChatModels(t *testing.T) {
	srv := stubModelServer(t, 1024, false)
	defer srv.Close()
	cfg := &config.Config{DLP: config.DLPConfig{
		BaseURL: srv.URL, EmbeddingModel: "e", EmbeddingDim: 1024,
		RiskModel: "r", MaxOutputTokens: 512,
	}}
	// Duplicate "a" must be deduped to a single chat check.
	out := postTestDLP(t, cfg, `{"base_url":"`+srv.URL+`","chat_models":["a","b","a"]}`)
	if out["ok"] != true {
		t.Fatalf("ok = %v, want true (out=%v)", out["ok"], out)
	}
	checks := out["checks"].(map[string]any)
	chats, ok := checks["chats"].([]any)
	if !ok {
		t.Fatalf("checks.chats missing or wrong type: %v", checks["chats"])
	}
	if len(chats) != 2 {
		t.Fatalf("len(chats) = %d, want 2 (deduped)", len(chats))
	}
	models := map[string]bool{}
	for _, ch := range chats {
		m := ch.(map[string]any)
		if m["ok"] != true {
			t.Errorf("chat %v not ok", m)
		}
		models[m["model"].(string)] = true
	}
	if !models["a"] || !models["b"] {
		t.Errorf("chats models = %v, want a and b", models)
	}
	// Back-compat: single checks.chat still present.
	if _, has := checks["chat"]; !has {
		t.Error("checks.chat must remain present for back-compat")
	}
}

func TestDLPConfigChatModelsFallback(t *testing.T) {
	// No chat_models → falls back to [risk_model]; checks.chats has 1 entry.
	srv := stubModelServer(t, 1024, false)
	defer srv.Close()
	cfg := &config.Config{DLP: config.DLPConfig{
		BaseURL: srv.URL, EmbeddingModel: "e", EmbeddingDim: 1024,
		RiskModel: "r", MaxOutputTokens: 512,
	}}
	out := postTestDLP(t, cfg, `{"base_url":"`+srv.URL+`"}`)
	checks := out["checks"].(map[string]any)
	chats := checks["chats"].([]any)
	if len(chats) != 1 {
		t.Fatalf("len(chats) = %d, want 1", len(chats))
	}
	if chats[0].(map[string]any)["model"] != "r" {
		t.Errorf("fallback chat model = %v, want r", chats[0])
	}
}

func TestDLPConfigEmbeddingUnknownDim(t *testing.T) {
	// embedding_dim:-1 → require a non-empty vector, not an exact match.
	// Server returns dim 768 while config says 1024; must still pass.
	srv := stubModelServer(t, 768, false)
	defer srv.Close()
	cfg := &config.Config{DLP: config.DLPConfig{
		BaseURL: srv.URL, EmbeddingModel: "e", EmbeddingDim: 1024,
		RiskModel: "r", MaxOutputTokens: 512,
	}}
	out := postTestDLP(t, cfg, `{"base_url":"`+srv.URL+`","embedding_dim":-1}`)
	if out["ok"] != true {
		t.Fatalf("ok = %v, want true for unknown-dim (out=%v)", out["ok"], out)
	}
	checks := out["checks"].(map[string]any)
	emb := checks["embedding"].(map[string]any)
	if emb["ok"] != true {
		t.Errorf("embedding.ok = %v, want true when dim>0 and expected<=0", emb["ok"])
	}
	if emb["dim"].(float64) != 768 {
		t.Errorf("embedding.dim = %v, want 768 (detected reported)", emb["dim"])
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `cd backend && CGO_ENABLED=0 go test ./internal/handlers/ -run 'TestDLPConfigMultipleChatModels|TestDLPConfigChatModelsFallback|TestDLPConfigEmbeddingUnknownDim' -v`
Expected: FAIL — `checks.chats` is nil (`checks["chats"]` type assertion fails), and the unknown-dim case fails because current `dlpCheckEmbedding` requires `dim == expected` (768 ≠ 1024).

- [ ] **Step 3: Extend `dlpCheckEmbedding` for unknown dimension**

In `backend/internal/handlers/settings.go`, replace the final two lines of `dlpCheckEmbedding` (the `dim := ...` / `return ...` at ~L326-327):

```go
	dim := len(parsed.Data[0].Embedding)
	okDim := dim == expected
	if expected <= 0 {
		// Dimension unknown (pre-provision): success is any non-empty vector.
		okDim = dim > 0
	}
	return gin.H{"ok": okDim, "dim": dim, "expected": expected}
```

- [ ] **Step 4: Add the dedupe helper**

In `backend/internal/handlers/settings.go`, add next to `orStr` (~L282):

```go
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
```

- [ ] **Step 5: Extend `TestDLPConfig` to accept `chat_models` and emit `checks.chats`**

In `backend/internal/handlers/settings.go`, add the request field to the `body` struct in `TestDLPConfig` (after `RiskModel`):

```go
			RiskModel      string   `json:"risk_model"`
			ChatModels     []string `json:"chat_models"`
```

Then replace the reachable/embedding/chat block (currently ~L268-278, from `reach := dlpCheckReachable(...)` through the final `c.JSON(...)`) with:

```go
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
```

Note: `expectedDim` currently falls back to `d.EmbeddingDim` only when `body.EmbeddingDim == 0` (see ~L251-254). A request sending `embedding_dim:-1` therefore keeps `expectedDim = -1`, which the new `dlpCheckEmbedding` treats as "any non-empty vector." Leave that fallback block as-is.

- [ ] **Step 6: Run the tests to verify they pass**

Run: `cd backend && CGO_ENABLED=0 go test ./internal/handlers/ -run 'TestDLPConfig' -v`
Expected: PASS — the three new tests plus the existing `TestDLPConfig`, `TestDLPPreflight*` (which send no `chat_models` and rely on `checks.chat`, still present).

- [ ] **Step 7: Full backend build + test + vet**

Run: `cd backend && CGO_ENABLED=0 go build ./... && CGO_ENABLED=0 go test ./... && go vet ./...`
Expected: all pass, no vet warnings.

- [ ] **Step 8: Commit**

```bash
git add backend/internal/handlers/settings.go backend/internal/handlers/settings_test.go
git commit -m "feat(dlp): test-dlp validates multiple chat models + unknown embedding dim (#189)

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

## Task 2: Frontend — pure helpers for the enable form

**Files:**
- Create: `frontend/src/utils/dlpEnable.js`
- Test: `frontend/src/utils/dlpEnable.test.js`

**Interfaces:**
- Produces (all pure, imported by Task 3):
  - `distinctChatModels(chatModel, summaryModel) -> string[]` — trimmed, non-empty, deduped, order preserved.
  - `formComplete(form) -> boolean` — true when `base_url`, `embedding_model`, and `chat_model` are all non-blank.
  - `buildTestPayload(form) -> object` — the `POST /api/config/test-dlp` body: `{ base_url, api_key, embedding_model, embedding_dim: -1, risk_model, chat_models }`.
  - `buildEnableChanges(form) -> object` — the `PUT /api/config` body: `dlp.base_url`, `dlp.embedding_model`, `dlp.risk_model` (= chat model), `dlp.summary_model` (= summary or chat model), `dlp.enabled: true`, and `dlp.api_key` only when the form's api_key is non-blank.
  - `form` shape: `{ base_url, api_key, embedding_model, chat_model, summary_model }`.

- [ ] **Step 1: Write the failing tests**

Create `frontend/src/utils/dlpEnable.test.js`:

```js
import { describe, it, expect } from 'vitest'
import {
  distinctChatModels,
  formComplete,
  buildTestPayload,
  buildEnableChanges,
} from './dlpEnable.js'

describe('distinctChatModels', () => {
  it('dedupes and drops blanks, preserving order', () => {
    expect(distinctChatModels('chat', 'chat')).toEqual(['chat'])
    expect(distinctChatModels('chat', 'sum')).toEqual(['chat', 'sum'])
    expect(distinctChatModels('chat', '')).toEqual(['chat'])
    expect(distinctChatModels(' a ', 'b')).toEqual(['a', 'b'])
  })
})

describe('formComplete', () => {
  const full = { base_url: 'u', embedding_model: 'e', chat_model: 'c' }
  it('requires base_url, embedding_model, chat_model', () => {
    expect(formComplete(full)).toBe(true)
    expect(formComplete({ ...full, base_url: '' })).toBe(false)
    expect(formComplete({ ...full, embedding_model: '  ' })).toBe(false)
    expect(formComplete({ ...full, chat_model: '' })).toBe(false)
  })
})

describe('buildTestPayload', () => {
  it('sends embedding_dim -1 and deduped chat_models', () => {
    const p = buildTestPayload({
      base_url: 'u', api_key: 'k', embedding_model: 'e',
      chat_model: 'c', summary_model: 'c',
    })
    expect(p).toEqual({
      base_url: 'u', api_key: 'k', embedding_model: 'e',
      embedding_dim: -1, risk_model: 'c', chat_models: ['c'],
    })
  })
  it('includes a distinct summary model', () => {
    const p = buildTestPayload({
      base_url: 'u', api_key: '', embedding_model: 'e',
      chat_model: 'c', summary_model: 's',
    })
    expect(p.chat_models).toEqual(['c', 's'])
  })
})

describe('buildEnableChanges', () => {
  it('maps chat->risk_model, defaults summary to chat, enables', () => {
    const c = buildEnableChanges({
      base_url: 'u', api_key: '', embedding_model: 'e',
      chat_model: 'c', summary_model: '',
    })
    expect(c).toEqual({
      'dlp.base_url': 'u',
      'dlp.embedding_model': 'e',
      'dlp.risk_model': 'c',
      'dlp.summary_model': 'c',
      'dlp.enabled': true,
    })
  })
  it('includes api_key only when non-blank', () => {
    const c = buildEnableChanges({
      base_url: 'u', api_key: 'sk-1', embedding_model: 'e',
      chat_model: 'c', summary_model: 's',
    })
    expect(c['dlp.api_key']).toBe('sk-1')
    expect(c['dlp.summary_model']).toBe('s')
  })
})
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `cd frontend && npx vitest run src/utils/dlpEnable.test.js`
Expected: FAIL — `dlpEnable.js` does not exist (import error).

- [ ] **Step 3: Implement the helpers**

Create `frontend/src/utils/dlpEnable.js`:

```js
// Pure helpers for the first-time Semantic DLP enable card (DLPEnableWizard).
// Pre-provision, config lives in config.yaml keys (edited via PUT /api/config);
// validation goes through POST /api/config/test-dlp with candidate values.

const trim = (s) => (s || '').trim()

// distinctChatModels returns the trimmed, non-empty, de-duplicated set of chat
// model ids to validate — the chat model plus a summary model when it differs.
export function distinctChatModels(chatModel, summaryModel) {
  const out = []
  for (const m of [chatModel, summaryModel]) {
    const t = trim(m)
    if (t && !out.includes(t)) out.push(t)
  }
  return out
}

// formComplete gates the Validate button: endpoint + embedding + chat required.
export function formComplete(form) {
  return ['base_url', 'embedding_model', 'chat_model'].every((k) => trim(form[k]) !== '')
}

// buildTestPayload builds the /api/config/test-dlp request. embedding_dim -1
// means "dimension unknown — accept any non-empty vector" (it's auto-detected at
// provision, so there is no dimension to match yet).
export function buildTestPayload(form) {
  return {
    base_url: trim(form.base_url),
    api_key: form.api_key || '',
    embedding_model: trim(form.embedding_model),
    embedding_dim: -1,
    risk_model: trim(form.chat_model),
    chat_models: distinctChatModels(form.chat_model, form.summary_model),
  }
}

// buildEnableChanges builds the PUT /api/config body written just before
// provisioning. The chat model seeds dlp.risk_model (the shared "chat" catalog
// model used by risk/understand/rerank); summary defaults to the chat model.
// api_key is included only when the operator entered one (blank preserves any
// existing value).
export function buildEnableChanges(form) {
  const chat = trim(form.chat_model)
  const summary = trim(form.summary_model) || chat
  const changes = {
    'dlp.base_url': trim(form.base_url),
    'dlp.embedding_model': trim(form.embedding_model),
    'dlp.risk_model': chat,
    'dlp.summary_model': summary,
    'dlp.enabled': true,
  }
  if (trim(form.api_key) !== '') changes['dlp.api_key'] = form.api_key
  return changes
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `cd frontend && npx vitest run src/utils/dlpEnable.test.js`
Expected: PASS — all four describe blocks green.

- [ ] **Step 5: Commit**

```bash
git add frontend/src/utils/dlpEnable.js frontend/src/utils/dlpEnable.test.js
git commit -m "feat(dlp): pure helpers for the first-time enable form (#189)

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

## Task 3: Frontend — `DLPEnableWizard` card + wire into `/dlp`

**Files:**
- Create: `frontend/src/components/DLPEnableWizard.jsx`
- Modify: `frontend/src/pages/SemanticDLP.jsx` (remove `SetupCTA` + its usage and the page-level `provision`/`provisioning`/`provisionError` state; render `DLPEnableWizard` in the `!provisioned` branch)

**Interfaces:**
- Consumes: `distinctChatModels`, `formComplete`, `buildTestPayload`, `buildEnableChanges` from `../utils/dlpEnable.js`; `api.getConfig`, `api.testDLPConfig`, `api.updateConfig`, `api.syncRules` from `../api/client` (all already exist; `testDLPConfig(payload)` passes the body through unchanged, so no client change is needed).
- Props: `DLPEnableWizard({ status, onProvisioned })` — `status` is the `api.dlpStatus()` object (`{ enabled, stream_exists, mv_exists, udfs }`); `onProvisioned` is called after a successful provision (the page passes `loadStatus`).

- [ ] **Step 1: Create the wizard component**

Create `frontend/src/components/DLPEnableWizard.jsx`:

```jsx
import { useCallback, useEffect, useState } from 'react'
import { api } from '../api/client'
import {
  formComplete,
  buildTestPayload,
  buildEnableChanges,
} from '../utils/dlpEnable.js'

const inputCls =
  'w-full rounded-sm bg-[#2c2834] text-gray-100 text-sm px-3 py-2 placeholder-gray-500 focus:outline-none focus:ring-1 focus:ring-[#ffb0cf]/50'
const labelCls = 'text-[11px] uppercase tracking-wide text-gray-400'

const EMPTY_FORM = {
  base_url: '', api_key: '', embedding_model: '', chat_model: '', summary_model: '',
}

// findConfigValue pulls a flat config key's value out of the /api/config shape.
function findConfigValue(cfg, key) {
  for (const s of cfg?.sections || []) {
    for (const k of s.keys || []) if (k.key === key) return k
  }
  return null
}

// missingPieces lists the not-yet-created Timeplus objects when DLP is enabled
// but partially provisioned.
function missingPieces(status) {
  const missing = []
  if (!status?.enabled) return missing
  if (!status.stream_exists) missing.push('agentguard_dlp_enriched stream')
  if (!status.mv_exists) missing.push('mv_dlp_enrich materialized view')
  for (const [fn, ok] of Object.entries(status.udfs || {})) {
    if (!ok) missing.push(`${fn} UDF`)
  }
  return missing
}

export default function DLPEnableWizard({ status, onProvisioned }) {
  const [form, setForm] = useState(EMPTY_FORM)
  const [apiKeySet, setApiKeySet] = useState(false)
  const [loadErr, setLoadErr] = useState(null)
  const [result, setResult] = useState(null)   // test-dlp response (invalidated on edit)
  const [validating, setValidating] = useState(false)
  const [enabling, setEnabling] = useState(false)
  const [error, setError] = useState(null)

  // Prefill from the config.yaml seed values.
  useEffect(() => {
    let cancelled = false
    api.getConfig()
      .then((cfg) => {
        if (cancelled) return
        const val = (k) => findConfigValue(cfg, k)?.value ?? ''
        setForm({
          base_url: String(val('dlp.base_url') || ''),
          api_key: '',
          embedding_model: String(val('dlp.embedding_model') || ''),
          chat_model: String(val('dlp.risk_model') || ''),
          summary_model: String(val('dlp.summary_model') || ''),
        })
        setApiKeySet(!!findConfigValue(cfg, 'dlp.api_key')?.set)
      })
      .catch((e) => setLoadErr(e?.message || 'Failed to load config'))
    return () => { cancelled = true }
  }, [])

  // Any edit invalidates a prior validation pass (closes the validate-then-swap hole).
  const setField = useCallback((patch) => {
    setForm((f) => ({ ...f, ...patch }))
    setResult(null)
    setError(null)
  }, [])

  const passed = result?.ok === true

  async function validate() {
    setValidating(true)
    setError(null)
    setResult(null)
    try {
      setResult(await api.testDLPConfig(buildTestPayload(form)))
    } catch (e) {
      setError(e?.message || 'Validation request failed')
    } finally {
      setValidating(false)
    }
  }

  async function enable() {
    setEnabling(true)
    setError(null)
    try {
      await api.updateConfig(buildEnableChanges(form))
      await api.syncRules()
      onProvisioned?.()
    } catch (e) {
      setError(e?.message || 'Provisioning failed')
    } finally {
      setEnabling(false)
    }
  }

  const missing = missingPieces(status)

  return (
    <section className="bg-[#211e29] rounded-sm p-8 max-w-2xl mx-auto flex flex-col gap-6">
      <div className="flex flex-col items-center text-center gap-2">
        <span className="material-symbols-outlined text-5xl text-[#ffb0cf]">policy</span>
        <h3 className="text-lg font-semibold text-gray-100">
          {status?.enabled ? 'Finish provisioning Semantic DLP' : 'Enable Semantic DLP'}
        </h3>
        <p className="text-xs text-gray-400 max-w-md">
          Configure your model endpoint and models, then validate that every model responds.
          Provisioning creates the enrichment stream, UDFs, and materialized view only after
          validation passes — no restart required.
        </p>
        {missing.length > 0 && (
          <ul className="text-[11px] text-gray-400 font-mono mt-1">
            {missing.map((m) => <li key={m}>✗ {m}</li>)}
          </ul>
        )}
      </div>

      {loadErr && <p className="text-xs text-[#ffb4ab]">{loadErr}</p>}

      {/* Endpoint */}
      <div className="grid grid-cols-1 sm:grid-cols-2 gap-3">
        <label className="space-y-1 sm:col-span-2"><span className={labelCls}>Base URL</span>
          <input className={inputCls} value={form.base_url}
            onChange={(e) => setField({ base_url: e.target.value })}
            placeholder="http://host.docker.internal:11434/v1" />
        </label>
        <label className="space-y-1 sm:col-span-2"><span className={labelCls}>
          API key {apiKeySet && <span className="text-emerald-400 normal-case">· set</span>}</span>
          <input className={inputCls} type="password" value={form.api_key}
            onChange={(e) => setField({ api_key: e.target.value })}
            placeholder={apiKeySet ? '•••••• (unchanged)' : 'none'} />
        </label>
        <label className="space-y-1"><span className={labelCls}>Embedding model</span>
          <input className={inputCls} value={form.embedding_model}
            onChange={(e) => setField({ embedding_model: e.target.value })}
            placeholder="mxbai-embed-large" />
        </label>
        <label className="space-y-1"><span className={labelCls}>Chat model</span>
          <input className={inputCls} value={form.chat_model}
            onChange={(e) => setField({ chat_model: e.target.value })}
            placeholder="used by risk / understand / rerank" />
        </label>
        <label className="space-y-1 sm:col-span-2"><span className={labelCls}>
          Summary model <span className="normal-case text-gray-500">(optional — defaults to chat model)</span></span>
          <input className={inputCls} value={form.summary_model}
            onChange={(e) => setField({ summary_model: e.target.value })}
            placeholder="(chat model)" />
        </label>
      </div>

      {/* Actions */}
      <div className="flex items-center gap-3 flex-wrap">
        <button onClick={validate} disabled={validating || !formComplete(form)}
          className="rounded-sm px-4 py-2 text-xs bg-gray-700 text-gray-100 hover:bg-gray-600 disabled:opacity-50">
          {validating ? 'Validating…' : 'Validate models'}
        </button>
        <button onClick={enable} disabled={!passed || enabling}
          className="rounded-sm px-4 py-2 text-xs font-semibold bg-gradient-to-br from-[#ffb0cf] to-[#f256a3] text-[#3d0023] hover:scale-95 transition-transform disabled:opacity-50 disabled:hover:scale-100">
          {enabling ? 'Provisioning…' : 'Enable & provision'}
        </button>
        {!passed && !validating && (
          <span className="text-[11px] text-gray-500">Validate all models before provisioning.</span>
        )}
      </div>

      {error && <p className="text-xs text-[#ffb4ab]">{error}</p>}

      {/* Validation results */}
      {result?.checks && (
        <div className="bg-[#2c2834] rounded-sm p-4 text-xs space-y-1">
          <div className={result.checks.reachable?.ok ? 'text-emerald-400' : 'text-error'}>
            {result.checks.reachable?.ok ? '✓ endpoint reachable'
              : `✗ endpoint: ${result.checks.reachable?.error || 'unreachable'}`}
          </div>
          {result.checks.embedding && (
            <div className={result.checks.embedding.ok ? 'text-emerald-400' : 'text-error'}>
              {result.checks.embedding.ok
                ? `✓ embedding model — vector dim ${result.checks.embedding.dim}`
                : `✗ embedding model: ${result.checks.embedding.error || 'no vector returned'}`}
            </div>
          )}
          {(result.checks.chats || []).map((ch) => (
            <div key={ch.model} className={ch.ok ? 'text-emerald-400' : 'text-error'}>
              {ch.ok ? `✓ chat model ${ch.model}`
                : `✗ chat model ${ch.model}: ${ch.note || 'failed'}`}
            </div>
          ))}
        </div>
      )}

      {!status?.enabled && (
        <p className="text-[11px] text-gray-500 text-center">
          If <code className="font-mono">DLP_ENABLED</code> is set as an environment variable it takes
          precedence — unset it to control this from the UI. Endpoint and model settings become
          editable in <span className="text-gray-400">Settings</span> once provisioned.
        </p>
      )}
    </section>
  )
}
```

- [ ] **Step 2: Wire the wizard into `SemanticDLP.jsx` and remove `SetupCTA`**

In `frontend/src/pages/SemanticDLP.jsx`:

Add the import near the other component imports (after `import { api } from '../api/client'`):

```jsx
import DLPEnableWizard from '../components/DLPEnableWizard'
```

Delete the entire `SetupCTA` function (the block starting with the `// ── Unprovisioned call-to-action ──` comment through its closing `}`, ~L83-127).

In the `SemanticDLP` component, delete the now-unused state and handler:
- Remove `const [provisioning, setProvisioning] = useState(false)`
- Remove `const [provisionError, setProvisionError] = useState(null)`
- Remove the entire `async function provision() { ... }` block (including its leading comment).

Replace the `!provisioned` branch in the render (currently `) : !provisioned ? (` … `<SetupCTA ... />` … ) with:

```jsx
          ) : !provisioned ? (
            <DLPEnableWizard status={status} onProvisioned={loadStatus} />
          ) : (
```

- [ ] **Step 3: Lint (catches unused imports/vars from the removal)**

Run: `cd frontend && npm run lint`
Expected: PASS — no `no-unused-vars` for `provisioning`/`provisionError`/`SetupCTA`. If lint flags anything, remove the leftover reference.

- [ ] **Step 4: Unit tests + production build**

Run: `cd frontend && npx vitest run && npm run build`
Expected: Vitest green (including `dlpEnable.test.js`); Vite build completes with no errors.

- [ ] **Step 5: Commit**

```bash
git add frontend/src/components/DLPEnableWizard.jsx frontend/src/pages/SemanticDLP.jsx
git commit -m "feat(dlp): first-time enable card gates provisioning on model validation (#189)

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

## Task 4: Docs

**Files:**
- Modify: `docs/dlp.md` (enable/provisioning section)
- Modify: `CLAUDE.md` (Semantic DLP section + `/api/config/test-dlp` row)

**Interfaces:** none (documentation only).

- [ ] **Step 1: Update `docs/dlp.md`**

Find the section describing enabling / provisioning DLP from the UI and add a short paragraph documenting the new gated flow. Use this content (adapt heading level to the surrounding doc):

```markdown
### First-time enable (UI)

The `/dlp` page shows a config + validation card until DLP is fully provisioned.
Enter the model endpoint (base URL, optional API key), the embedding model, the
chat model (used by risk / understand / rerank), and an optional summary model,
then click **Validate models**. Validation calls the real endpoint —
reachability, one embedding call (any non-empty vector; the dimension is
auto-detected at provision), and a chat completion per distinct chat model.
**Enable & provision** is blocked until every model validates; editing any field
after a pass requires re-validating. On success the values are written to the
`config.yaml` seed keys, `dlp.enabled` is turned on, and the pipeline is
provisioned (stream, UDFs, MV) with no restart. After provisioning, endpoint and
model settings are edited live in Settings via the `agentguard_dlp_config` stream.
```

- [ ] **Step 2: Update `CLAUDE.md`**

In the `POST /api/config/test-dlp` table row (in the API Endpoints table), update the Notes cell to:

```
Tests DLP `base_url` reachability (GET `/models`), a real embedding call, and a chat completion per `chat_models[]` entry (falls back to the risk model). `embedding_dim<=0` accepts any non-empty vector. Never 5xx on unreachable.
```

In the **Semantic DLP** section, add one sentence to the **Config** bullet (or a new bullet) noting the gated first-time enable:

```
- **First-time enable (UI)** — the `/dlp` page requires configuring the endpoint + models and passing a live validation of every distinct model (embedding + each chat model) before it writes the seed config keys, enables `dlp.enabled`, and provisions. Post-provision editing stays in Settings (`DLPConfigEditor`).
```

- [ ] **Step 3: Commit**

```bash
git add docs/dlp.md CLAUDE.md
git commit -m "docs(dlp): document gated first-time enable + test-dlp changes (#189)

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

## Final verification

- [ ] **Backend:** `cd backend && CGO_ENABLED=0 go build ./... && CGO_ENABLED=0 go test ./... && go vet ./...` — all pass.
- [ ] **Frontend:** `cd frontend && npx vitest run && npm run lint && npm run build` — all pass.
- [ ] **Manual smoke (optional, requires a running stack):** on a fresh/unprovisioned stack, open `/dlp`; confirm the card appears, that **Enable & provision** is disabled until **Validate models** turns every line green, that editing a field re-disables it, and that provisioning succeeds and the normal DLP UI renders.

## Self-review notes (coverage vs. spec)

- Spec "inline config+validate gate replacing SetupCTA" → Task 3.
- Spec "hard gate; edit-after-pass invalidates" → Task 3 (`passed` derived from `result`; `setField` clears `result`).
- Spec "validate each distinct model id; embedding once" → Task 1 (`chat_models` deduped, single embedding check) + Task 2 (`distinctChatModels`, `buildTestPayload`).
- Spec "unknown dim pre-provision → vector present" → Task 1 (`expected<=0`) + Task 2 (`embedding_dim:-1`).
- Spec "seed via config.yaml then syncRules" → Task 2 (`buildEnableChanges`) + Task 3 (`enable()`).
- Spec "back-compat checks.chat for Settings editor" → Task 1 Step 5 (`checks.chat = chats[0]`), Global Constraints.
- Spec "post-provision unchanged; env-var precedence note; partial-provision note" → Task 3 (notes rendered; wizard only shown when `!provisioned`).
- Spec "docs" → Task 4.
