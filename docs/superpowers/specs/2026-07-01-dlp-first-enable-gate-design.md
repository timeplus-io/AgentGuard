# Semantic DLP: first-time enable config + validation gate

**Date:** 2026-07-01
**Branch:** feature/189-dlp-config-mutable-stream (issue #189)
**Status:** approved

## Problem

The first-time path to turn on Semantic DLP is a single **"Enable Semantic DLP"**
button on `/dlp` (`SetupCTA` in `frontend/src/pages/SemanticDLP.jsx`). Clicking it
flips `dlp.enabled` and calls `syncRules()`, which runs `InitDLP` and seeds the
`agentguard_dlp_config` stream from whatever `config.yaml` DLP defaults exist —
often the wrong endpoint (e.g. `http://localhost:4000/v1`).

Because every DLP UDF **fails open**, a misconfigured endpoint does not error:
the pipeline provisions successfully and then silently produces empty embeddings
and zero risk scores. The operator only discovers the models never worked after
inspecting enriched rows. Worse, the live editor that could fix it
(`DLPConfigEditor`, Settings) reads the `agentguard_dlp_config` stream, which does
not exist until the *first* provision — so there is no way to correct the endpoint
before the broken pipeline is created.

## Goal

Before any stream / UDF / MV is created, require the operator to configure the
model endpoint and models and **validate that every distinct model actually
responds**. Provision only after a green validation.

## Decisions (confirmed)

1. **UX shape:** inline config + validate gate on `/dlp` (no modal, no route
   change). Replaces the single-button `SetupCTA` in the `!provisioned` slot.
2. **Gate strictness:** hard gate. "Enable & provision" is disabled until every
   configured model validates OK.
3. **Validation scope:** each distinct model id — embedding validated once, plus
   each distinct chat model id (deduped across chat/risk and summary).

## Design

### Pre-provision config source

Before the config stream exists, the seed values live in `config.yaml` DLP keys,
edited via `PUT /api/config` (`api.updateConfig`). Relevant keys:

- `dlp.base_url`, `dlp.api_key`
- `dlp.embedding_model`
- `dlp.risk_model` — seeds the shared `chat` catalog model used by
  risk / understand / rerank (this is the existing seed mapping in
  `DLPConfigRows`)
- `dlp.summary_model`
- `dlp.enabled`

`InitDLP` seeds the `agentguard_dlp_config` stream from these seed-if-absent and
auto-detects `embedding_dim` by probing the embedding model at provision.

### The card: `DLPEnableWizard`

A single inline card rendered by `SemanticDLP` whenever `!provisioned` (both the
"disabled" and "enabled-but-not-provisioned" sub-states), replacing `SetupCTA`.
The `SetupCTA` missing-pieces list is folded into the card when
`status.enabled` but pieces are missing.

Fields, prefilled from `api.getConfig()`:

- **Base URL** (text) + **API key** (password; shows "· set" when one exists,
  blank = keep existing)
- **Embedding model** (text, model id)
- **Chat model** (text, model id) — written to `dlp.risk_model`
- **Summary model** (text, model id) — defaults to the chat model; a separate
  validation call only when it differs

Actions: **Validate**, and (gated) **Enable & provision**.

### Validation

Reuses `POST /api/config/test-dlp` (`TestDLPConfig` in
`backend/internal/handlers/settings.go`), which already checks reachability, a
real `/embeddings` call, and a real `/chat/completions` call — and works
pre-provision because it reads candidate values from the request body, not the
stream. Two minimal backend extensions:

1. **Multiple chat models.** Accept optional `chat_models: []string`. When
   present, run one `dlpCheckChat` per **distinct** id and return
   `checks.chats: [{model, ok, note, thinking_detected}]`. Preserve the existing
   single `checks.chat` (set it to `chats[0]`) so the Settings editor's Test
   button is unaffected. When `chat_models` is absent, behavior is unchanged
   (`chat_models` defaults to `[risk_model]`).
2. **Unknown-dimension embedding.** In `dlpCheckEmbedding`, when `expected <= 0`
   (the pre-provision case — the dimension is auto-detected at provision, not yet
   known), success = a non-empty vector was returned. Report the detected `dim`
   for display. Existing callers that pass a positive expected dim keep the
   dim-match behavior.

Overall pass = `reachable.ok ∧ embedding.ok ∧ every chats[].ok`.

Distinct chat set = dedupe(`{chat/risk model, summary model}`). Embedding is
validated once.

### Gate enforcement

- **Enable & provision** is disabled until a validation run passes.
- Editing any field after a pass clears the pass → the operator must re-validate.
  This closes the "validate, then swap the endpoint, then provision" hole.
- Nothing is persisted during validation; candidate values go straight to
  `test-dlp`.

### Enable action (only after a green validation)

1. `api.updateConfig({ 'dlp.base_url', 'dlp.api_key' (only if changed),
   'dlp.embedding_model', 'dlp.risk_model' ← chat model, 'dlp.summary_model',
   'dlp.enabled': true })`
2. `api.syncRules()` → `InitDLP` seeds the config stream from those values,
   probes `embedding_dim`, provisions stream / UDFs / MV.
3. Reload `api.dlpStatus()`; on `provisioned` the card disappears and the normal
   DLP UI renders.

## Scope boundaries

- **Post-provision is unchanged.** `DLPConfigEditor` (Settings) remains the
  live-edit tool once the stream exists.
- The card targets the true first-time state (nothing provisioned). If a partial
  pipeline exists (stream present, MV missing), the config-stream seed only fills
  **absent** rows, so endpoint/model edits made in the card would not overwrite an
  existing stream row. The card notes that once provisioned, endpoint/model edits
  move to Settings. `syncRules()` still completes the missing MV/UDFs.
- `dlp.enabled` set via an environment variable takes precedence over
  `updateConfig`; the card keeps that note and still allows provisioning an
  already-enabled deployment.

## Changes

- **Backend** (`backend/internal/handlers/settings.go`):
  - `TestDLPConfig`: accept `chat_models []string`; emit `checks.chats`; keep
    `checks.chat = chats[0]`.
  - `dlpCheckEmbedding`: `expected <= 0` ⇒ ok when `dim > 0`.
- **Frontend:**
  - New `frontend/src/components/DLPEnableWizard.jsx`.
  - `frontend/src/pages/SemanticDLP.jsx`: render the wizard in the `!provisioned`
    branch instead of `SetupCTA` (fold the missing-pieces list into the wizard).
  - `frontend/src/api/client.js`: `testDLPConfig` passes `chat_models`.
- **Tests:**
  - Backend: `TestDLPConfig` with `chat_models` (multiple + dedupe) and the
    `expected = 0` embedding path.
  - Frontend: `npm run build` clean.

## Non-goals

- No change to the post-provision `DLPConfigEditor` flow.
- No per-model endpoint override validation in the card (per-model base_url/api_key
  is a Settings-only, post-provision concern).
- No backfill of previously-enriched rows.
