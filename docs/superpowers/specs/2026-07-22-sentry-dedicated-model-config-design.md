# Sentry — Dedicated Model Config (chat + embedding) with UI

> **Status:** Approved design
> **Issue:** [#207](https://github.com/timeplus-io/AgentGuard/issues/207) (parent), [#209](https://github.com/timeplus-io/AgentGuard/issues/209) (UI); ships on top of PR [#208](https://github.com/timeplus-io/AgentGuard/pull/208)
> **Date:** 2026-07-22
> **Supersedes:** the `sentry_chat`-row-in-`agentguard_dlp_config` config approach from the Phase 1 spec (`2026-07-22-sentry-agent-design.md` §4) — Sentry no longer reads DLP config.

## 1. Goal

Give Sentry its **own** model configuration — a dedicated chat (LLM) endpoint/model and a dedicated embedding endpoint/model — fully decoupled from the Semantic DLP `shared` row and model catalog, with a configuration UI reachable whenever `sentry.enabled: true` (independent of DLP being enabled or provisioned).

Motivation: Sentry and DLP may legitimately use different models/providers. Today Sentry piggybacks on `agentguard_dlp_config`'s `shared` row, which (a) couples the two, (b) hides Sentry's endpoint behind DLP-branded UI, and (c) makes the model-config UI unreachable when `dlp.enabled: false` (`GET /api/dlp/config` → 503). A misconfigured shared row also produced a confusing mid-chat `dial tcp :4000 connection refused`.

Decisions settled with the user (brainstorming):
1. **Dedicated `agentguard_sentry_config` mutable stream** (not new rows in `agentguard_dlp_config`).
2. **UI in the existing "Sentry Agent" Settings section** (not a `/sentry` panel).
3. **`dlp_search` keeps DLP's `dlp_embed`** — it searches the DLP-enriched vector space, which must match DLP's embedding model; Sentry's embedding model is exclusively for Sentry memory (Phase 2).
4. **Seed-if-absent boot defaults**, runtime stream authoritative (the DLP pattern).
5. **A Sentry `test-connection` endpoint** validating the resolved chat endpoint (real completion + tool-calling capability) and an embedding call, mirroring `POST /api/config/test-dlp`.

## 2. Data model — `agentguard_sentry_config` (mutable)

New stream provisioned by `InitSentry`, same seed-if-absent style as `agentguard_dlp_config`. `IF NOT EXISTS` DDL, keyed on `key` (re-insert = upsert).

```sql
CREATE MUTABLE STREAM IF NOT EXISTS agentguard_sentry_config
(
  key        string,
  value      string,                     -- JSON
  updated_at datetime64(3) DEFAULT now64(3),
  updated_by string
)
PRIMARY KEY (key)
```

Three rows (keys defined as consts in `internal/timeplus/sentry_config_store.go`):

- **`shared`** (`SentryCfgKeyShared`) — Sentry's endpoint + model catalog:
  ```json
  {"base_url": "...", "api_key": "...", "timeout": 60,
   "models": {"chat": {"model": "gpt-4o"},
              "embedding": {"model": "text-embedding-3-small"}}}
  ```
  Each catalog entry may override `base_url`/`api_key`, else inherits the top-level.
- **`chat`** (`SentryCfgKeyChat`) — the fields the Phase 1 `sentry_chat` row held, minus nothing:
  ```json
  {"model": "chat", "system_prompt": "...", "max_tokens": 1024,
   "max_iterations": 12, "turn_token_budget": 32000,
   "sql_timeout_ms": 30000, "sql_max_rows": 1000, "tool_result_max_bytes": 16384}
  ```
  `model` is a **name** looked up in `shared.models`.
- **`embed`** (`SentryCfgKeyEmbed`) — for Sentry memory (Phase 2); provisioned + editable now, not yet consumed by the loop:
  ```json
  {"model": "embedding", "embedding_dim": 1536}
  ```

Resolution reuses the existing pure UDF `dlp_config_resolve(shared, config)` unchanged (it folds any two JSON strings and resolves `model` against the merged `models` catalog). No new UDF.

## 3. Config store — `internal/timeplus/sentry_config_store.go`

Mirrors `dlp_config_store.go`:
- `SentryCfgKeyShared = "shared"`, `SentryCfgKeyChat = "chat"`, `SentryCfgKeyEmbed = "embed"`.
- `SentryConfigKeys() []string`, `IsSentryConfigKey(key) bool`.
- `ReadSentryConfig(cfg) ([]SentryConfigRow, error)`, `ReadSentryConfigValue(cfg, key)`.
- `WriteSentryConfig(cfg, key, value, updatedBy)` — native `PrepareBatch` upsert (JSON-safe), via the existing `Connect` helper.
- `ValidateSentryConfigValue(key, value)` — JSON well-formedness; for `chat`, `system_prompt` (if present) must be a string; for `shared`, `models` (if present) must be an object of `{model,...}` entries; for `embed`, `embedding_dim` (if present) must be a number.
- `SentryConfigRows(sentryCfg) map[string]string` — renders the seed defaults from boot config.

**Provisioning** (`InitSentry`, `sentry.go`): create `agentguard_sentry_config` + ensure `dlp_config_resolve` UDF exists (already done), then `seedSentryConfig` writes any absent row from boot defaults. The Phase 1 `sentry_chat` key is **removed** from `agentguard_dlp_config` — drop `SentryCfgKeyChat` from `DLPConfigKeys()` and its `ValidateDLPConfigValue` case (`dlp_config_store.go:24,121`), and drop the seed from `InitSentry`. `SyncSentry` re-runs provisioning.

## 4. Boot defaults — `config.yaml` `sentry:` section

Extend `SentryConfig` (`internal/config/config.go`) and the registry with seed-if-absent boot defaults (mirroring the `dlp.*` keys, incl. `EnvVar` + `BindEnv` + `Save()` lines — the three wiring points Phase 1 Task 1 established):

| Key | Default | Notes |
|---|---|---|
| `sentry.enabled` | false | (existing) |
| `sentry.skill_dir` | "" | (existing) |
| `sentry.base_url` | "" | Seeds `shared.base_url` |
| `sentry.api_key` | "" | Secret; seeds `shared.api_key` |
| `sentry.chat_model` | "" | Seeds a `chat` catalog entry + `chat.model` name |
| `sentry.embedding_model` | "" | Seeds an `embedding` catalog entry + `embed.model` name |
| `sentry.embedding_dim` | 1536 | Seeds `embed.embedding_dim` |
| `sentry.request_timeout_seconds` | 60 | Seeds `shared.timeout` (clamped like DLP) |
| `sentry.max_output_tokens` | 1024 | Seeds `chat.max_tokens` |

`SentryChatDefaults` is replaced by `SentryConfigRows(sentryCfg)` producing all three rows. When `base_url`/models are empty the rows still seed (empty endpoint) so the UI has something to edit; `LoadChatCfg` still errors clearly if the resolved `base_url` is empty.

## 5. Agent loop config load

`sentry.LoadChatCfg(tpCfg)` (`internal/sentry/llm.go`) changes its two sub-queries from `agentguard_dlp_config` / `DLPCfgKeyShared` + `SentryCfgKeyChat` to `agentguard_sentry_config` / `SentryCfgKeyShared` + `SentryCfgKeyChat`, still wrapping in `dlp_config_resolve`. The `ChatCfg` struct, `applyDefaults`, and clamps are unchanged. No other loop code changes. (`LoadEmbedCfg` for memory is Phase 2 — not built here, but the `embed` row exists for it.)

## 6. HTTP handlers — `internal/handlers/sentry_config.go`

Mirrors `dlp_config.go` + `TestDLPConfig`:

| Method | Path | Behavior |
|---|---|---|
| GET | `/api/sentry/config` | All `agentguard_sentry_config` rows; `api_key` redacted, `APIKeySet` flag; returns `keys: SentryConfigKeys()`. Reachable whenever authed (no DLP dependency). |
| PUT | `/api/sentry/config/:key` | Validate (`IsSentryConfigKey` + `ValidateSentryConfigValue`) → merge-preserve stored `api_key` if blank → `WriteSentryConfig`. `updated_by` = session user. Live, no re-provision. |
| POST | `/api/sentry/config/test` | Probe the resolved endpoint: reachable (`GET /models`), a **chat completion that requests a tool and asserts a tool_call comes back** (tool-calling capability), and an embedding call. Returns `{ok, checks:{reachable, chat:{ok, tool_calling}, embedding}}`. Never 5xx on unreachable. |

`SentryStatus` extended: `config_provisioned` (stream present), `base_url_set`, `model`, plus the existing fields. Routes registered in the `protected` group next to the other `/sentry/*` routes.

## 7. Frontend — `SentryConfigEditor` in the Sentry Settings section

New `frontend/src/components/SentryConfigEditor.jsx` (mirroring `DLPConfigEditor.jsx`), rendered from the "Sentry Agent" settings section (the section registered in Phase 1 Task 1; wire it in `SettingsSection.jsx` the way the DLP section embeds its editor). It edits:
- **Endpoint** (`shared`): `base_url`, `api_key` (blank preserves stored), `timeout`, and an add/remove **models catalog** (name → model/base_url/api_key).
- **Chat** (`chat`): catalog model name, `system_prompt` (textarea), and the budget knobs with their clamp ceilings shown.
- **Embedding** (`embed`): catalog model name, `embedding_dim` — labeled "used by Sentry memory (Phase 2)".
- **Test connection** button → `POST /api/sentry/config/test`, showing per-check pass/fail (reachable / chat+tool-calling / embedding) before Save.

API client (`api/client.js`): `getSentryConfig`, `putSentryConfig(key, value)`, `testSentryConfig(payload)` — mirroring the `getDLPConfig`/`putDLPConfig`/`testDLPConfig` trio. The editor reads via `GET /api/sentry/config`, which does **not** 503 on `dlp.enabled=false`. A "not configured" hint on the `/sentry` page (driven by `sentryStatus().base_url_set`) links to the section. This closes #209.

## 8. Migration / compatibility

Repo convention is no backward-compat shims (`docs/design.md` §5.1.3). Provisioning introduces `agentguard_sentry_config` and stops seeding/reading `sentry_chat` in `agentguard_dlp_config`. Because PR #208 is unmerged, no deployed Sentry config exists — this is a clean introduction, not a data migration. A re-provision on an environment that already ran #208's provisioner simply creates the new stream and leaves the orphaned `sentry_chat` DLP row unused (harmless; may be dropped by a one-line cleanup in `InitSentry`).

## 9. Testing

- **Unit (Go):** `SentryConfigRows` valid JSON + expected keys/defaults; `ValidateSentryConfigValue` accept/reject matrix (bad JSON, non-string prompt, bad models shape, bad dim); `IsSentryConfigKey`; `LoadChatCfg` builds the sub-queries against `agentguard_sentry_config` (assert the SQL text targets the new stream + keys); `/api/sentry/config` PUT validation + 400s; test-connection handler response shape with the mock model. Reuse the DLP config-store tests as templates.
- **Integration:** update `sentry_integration.sh` to point the **new** `agentguard_sentry_config` `shared` row (via `PUT /api/sentry/config/shared`) at the mock and add a `chat` catalog entry, replacing the Phase 1 step that pointed DLP's shared row. Assert the chat round-trip still passes live. Add a `POST /api/sentry/config/test` assertion against the mock (reachable + tool-calling + embedding all ok).
- **Frontend:** `npm run lint` + `npm run build` clean.

## 10. Docs

- `docs/sentry.md`: replace the "Configuring the chat endpoint (the `shared` row)" section — Sentry config now lives in `agentguard_sentry_config` (its own `shared`/`chat`/`embed` rows), edited in **Settings → Sentry Agent**, reachable without DLP; document the three rows, the test-connection, and that embedding is for Phase 2 memory. Update the `sentry_chat` references.
- `CLAUDE.md`: update the streams table (add `agentguard_sentry_config`; note `sentry_chat` no longer in `agentguard_dlp_config`), the API table (3 new `/api/sentry/config*` rows), and the Sentry subsection.

## 11. Out of scope

- Consuming the `embed` model — Sentry memory itself is Phase 2 (#207); this ships the config + UI so the model is ready.
- Auto-detecting `embedding_dim` by probing (DLP does this via `ProbeEmbeddingDim`) — Sentry uses the configured value for now; revisit with Phase 2 memory provisioning.
- Per-model cost attribution / budget integration.
