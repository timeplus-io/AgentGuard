# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What This Project Is

This repository is the **data-collection side of AgentGuard**: the agent plugins and SDKs that stream AI-agent telemetry (sessions, prompts, LLM calls, tool calls) into Timeplus Enterprise.

AgentGuard itself — streams, CIM normalization MVs, metrics, detection rules, dashboard — is a **Timeplus app** (`io.timeplus.agentguard`, `db_name: ag`) that lives in the [timeplus-io/apps](https://github.com/timeplus-io/apps/tree/main/apps/agentguard) repo (local checkout: `~/Code/timeplus/apps`). Nothing in this repo creates streams or MVs; the plugins only write into what the app installs.

The standalone AgentGuard (Go/Gin backend, React UI, Docker image, setup wizard, `agents/install.sh`, `make tpapp` generator) was removed from this repo. The last release containing it is tag `v0.9.3`; look there (`git show v0.9.3:<path>`) if you need the old server code or its docs. Do not reintroduce a server, UI, or installer script here.

## Repository Layout

| Path | Contents |
|---|---|
| `agents/claudecode/agentguard-plugin/` | Claude Code hook plugin — TypeScript, bundled with esbuild, npm `@timeplus/agentguard-claudecode-plugin` (bin `agentguard-hook`) |
| `agents/claudecode/Makefile` | Local dev install: builds the plugin and patches hooks + env + OTel vars into `~/.claude/settings.json` (`make install`, `make unconfigure unconfigure-otel`) |
| `agents/openclaw/agentguard-plugin/` | OpenClaw plugin — TypeScript, built with `tsc`, npm `@timeplus/agentguard-openclaw-plugin` |
| `agents/openclaw/` | Docker dev harness for OpenClaw (`Makefile`), plugin design doc, OTel guide, scenario tests in `tests/` |
| `agents/hermes/agentguard-plugin/` | Hermes Agent plugin — Python (>= 3.10), PyPI `agentguard-hermes-plugin`; `plugin.py` (CLI + gateway) and `handler.py` (gateway hook) |
| `agents/codex/agentguard-plugin/` | OpenAI Codex plugin — Python (>= 3.10), `agentguard-codex` hook command built on the Python SDK; handles all 14 Codex hooks |
| `agents/hermes/` | Docker dev harness for Hermes |
| `agents/litellm/`, `agents/paperclip/`, `agents/xearxng/` | Supporting dev/demo environments (LLM proxy, Paperclip showcase, search) — not shipped |
| `sdk/python/`, `sdk/javascript/` | OpenTelemetry-based SDKs for instrumenting custom agents |
| `skills/agentguard/SKILL.md` | Agent skill that installs and configures the plugins — the supported guided-install path |
| `docs/` | `plugin-install.md` (install + config reference), `cim.md` (event model the app derives from plugin events), `skills.md`, `memory.md`, `research/` |

## Development Commands

```bash
make build            # build all four plugins
make test             # run all plugin unit tests
make package-plugins  # pack into packages/ (npm tarballs + Hermes and Codex wheels)
```

Per plugin:

```bash
# Claude Code — esbuild does not typecheck, so run typecheck too
cd agents/claudecode/agentguard-plugin && npm install && npm test && npm run typecheck && npm run build
npx vitest run src/normalize.test.ts            # single test file

# OpenClaw — tsc build typechecks
cd agents/openclaw/agentguard-plugin && npm install && npm test && npm run build

# Hermes — needs Python >= 3.10 (system python3 on macOS is 3.9 and fails)
cd agents/hermes/agentguard-plugin && python3.12 -m unittest discover -s tests -v
```

CI (`.github/workflows/ci.yml`) runs test + build for the two npm plugins and the Codex plugin, and the Hermes unit tests. The release workflows are the two SDK publish workflows and the Claude Code and Codex plugin publish workflows (see Releasing Plugins / Releasing SDKs); the other plugins are not published by CI.

## How Collection Works

Two channels feed the app:

1. **Hook events** — each plugin normalizes agent lifecycle hooks into one `HookEvent` row and POSTs it to the `agentguard_hook_events` stream in database `ag`, tagged with `agent_type` (`claudecode` / `openclaw` / `hermes`). The app's `mv_cim_*` MVs turn these into `agentguard_cim_event` (see `docs/cim.md`).
2. **OTel** — Claude Code (`CLAUDE_CODE_ENABLE_TELEMETRY=1`), OpenClaw's `diagnostics-otel` plugin, and the SDKs export OTLP/HTTP to the app's OTel input (default port `4318`) for token/cost/latency metrics. See `agents/*/otel_timeplus.md`.

### Ingest modes

Selected by `AGENTGUARD_INGEST_API` (OpenClaw: `ingestApi`); same Basic-auth username/password and same payload in both:

- `timeplusd` (default) — `POST {url}/timeplusd/v2/ingest/{db}/streams/{stream}` on port 3218.
- `rest` — the Timeplus REST API, `POST {url}/{workspace}/api/v1beta2/streams/{stream}/ingest?db={db}` on port 8000, for deployments where 3218 is not exposed. `workspace` stays `default`.

### Per-plugin shape

- **Claude Code** (`src/index.ts`): a short-lived process per hook. Reads the event from stdin → `normalize.ts` → `timeplus-client.ts` `send()`. On `UserPromptSubmit` / `Stop` it also parses `transcript_path` (`transcript.ts`) and emits synthetic `prompt_context` / `llm_context` events. Must never block or fail the agent: errors are swallowed, exit 0. `run_id` always equals `session_id`.
- **OpenClaw** (`src/hooks.ts`, `src/index.ts`): long-lived in the gateway; registers all lifecycle hooks and batches events (`flushMs`, `batchSize`) through `TimeplusClient`. Config comes from `plugins.entries.agentguard.config` in `openclaw.json` with env-var fallbacks (`src/config.ts`, schema in `openclaw.plugin.json`).
- **Hermes** (`agentguard_hermes/plugin.py`, `handler.py`): stdlib-only (`urllib`), configured purely by env vars. `conversation_history` is stripped before ingest. `plugin.py` and `handler.py` duplicate the ingest logic on purpose (two Hermes load paths) — change both.
- **Codex** (`agentguard_codex/`): short-lived process per hook, mirroring Claude Code's shape but written in Python on top of the SDK. Exports OTel spans only: five hooks go through the SDK's `AgentGuardHooks` (`session` / `llm` / `tool` spans), the other nine each emit a span named after the hook via `sink.py`. `events.py` `HANDLERS` and the sample `hooks.json` must list the same events (a test checks). The plugin never writes stdout. The move to `agentguard_hook_events` is an issue #308 follow-up.

### Holds (legacy)

All three plugins still carry the synchronous hold-and-wait path (`holds.ts`, `AGENTGUARD_HOLDS_ENABLED`, `AGENTGUARD_URL`, `AGENTGUARD_HOLDS_FAIL_POLICY`), which called the retired server's `/api/holds`. The Timeplus app has no hold capability, so holds default to off and must stay off; with holds off, `PreToolUse` returns allow immediately and the event goes down the normal async path. Don't treat holds-related leftovers (sync `PreToolUse` hook entry with `timeout: 600000`, `AGENTGUARD_URL` defaults) as bugs.

## Conventions

- **Config is env vars / plugin config only.** Any config change must be reflected in all of: the plugin's `README.md`, `docs/plugin-install.md`, `skills/agentguard/SKILL.md`, and (Claude Code) `agents/claudecode/Makefile` / (OpenClaw) `openclaw.plugin.json`.
- **Keep the three hook-event plugins aligned** (Claude Code, OpenClaw, Hermes): a new config key or event-shape change usually applies to all three. The Codex plugin is separate: it exports OTel spans through the Python SDK and does not write `HookEvent` rows. The `HookEvent` column set must match the app's `agentguard_hook_events` DDL (in the apps repo) — a schema change needs a coordinated app change.
- **Defaults**: database `ag`, stream `agentguard_hook_events`, holds off.
- **Naming**: say "Timeplus" / "Timeplus Enterprise" (never "Proton") and "Timeplus REST API" for the port-8000 server (never its internal name). No Timeplus Cloud, no API keys — Basic auth only.

## Releasing Plugins

Plugins are versioned and published independently; there is no product release tag. The OpenClaw and Hermes plugins are published manually; the Claude Code and Codex plugins are published by CI (see below).

1. Bump `version` in `agents/openclaw/agentguard-plugin/package.json` (+ lockfile) and/or `agents/hermes/agentguard-plugin/pyproject.toml`.
2. Merge, then publish manually: `npm publish` in `agents/openclaw/agentguard-plugin` (`prepublishOnly` rebuilds `dist/`, which is gitignored), and `make publish` in `agents/hermes/agentguard-plugin` (hatch build + twine upload).
3. Verify what is live with `npm view <pkg> version` / `npm pack <pkg>@<ver>` before announcing.

**Claude Code plugin:** bump `version` in `agents/claudecode/agentguard-plugin/package.json` (+ lockfile), merge, then publish a GitHub Release tagged `claudecode-plugin-v<version>`. `.github/workflows/publish-claudecode-plugin.yml` tests, typechecks, builds and publishes `@timeplus/agentguard-claudecode-plugin` to npm (`NPM_TOKEN` secret, with provenance); it fails on a tag/version mismatch or an already-published version, and a manual run tests, builds and packs without uploading.

**Codex plugin:** bump `version` in `agents/codex/agentguard-plugin/pyproject.toml` and `__version__` in `agentguard_codex/__init__.py`, merge, then publish a GitHub Release tagged `codex-plugin-v<version>`. `.github/workflows/publish-codex-plugin.yml` publishes `timeplus-agentguard-codex-plugin` to PyPI (Trusted Publishing, environment `pypi`); it fails on a tag/version mismatch, and a manual run builds without uploading.

## Releasing SDKs

The SDKs are versioned independently of the plugins and of each other, and are published by CI from a GitHub Release. The Python SDK publishes to PyPI as `timeplus-agentguard` (the import name stays `agentguard`; PyPI rejects the bare name as too similar to an existing project); the JavaScript SDK publishes to npm as `@timeplus/agentguard`.

1. Bump `version` in `sdk/python/pyproject.toml` and/or `sdk/javascript/package.json` (+ lockfile), and merge.
2. Publish a GitHub Release with a tag of `sdk-python-v<version>` or `sdk-js-v<version>`. `.github/workflows/publish-sdk-python.yml` (PyPI Trusted Publishing, environment `pypi`) or `publish-sdk-javascript.yml` (`NPM_TOKEN` secret) picks it up; each fails if the tag does not match the package version and ignores other tags.
3. Running either workflow manually (`workflow_dispatch`) builds and validates without uploading.
4. Local fallback: `make publish-sdk-python` (needs `hatch`, `twine`, and `sdk/python/.pypirc`) / `make publish-sdk-js` (needs `npm login`).
5. Verify with `pip index versions timeplus-agentguard` / `npm view @timeplus/agentguard version`.
