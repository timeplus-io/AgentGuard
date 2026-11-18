# AgentGuard

[![CI](https://github.com/timeplus-io/agentguard/actions/workflows/ci.yml/badge.svg)](https://github.com/timeplus-io/agentguard/actions/workflows/ci.yml)
[![npm: Claude Code plugin](https://img.shields.io/npm/v/%40timeplus%2Fagentguard-claudecode-plugin?label=claudecode-plugin&logo=npm)](https://www.npmjs.com/package/@timeplus/agentguard-claudecode-plugin)
[![npm: OpenClaw plugin](https://img.shields.io/npm/v/%40timeplus%2Fagentguard-openclaw-plugin?label=openclaw-plugin&logo=npm)](https://www.npmjs.com/package/@timeplus/agentguard-openclaw-plugin)
[![PyPI: Hermes plugin](https://img.shields.io/pypi/v/agentguard-hermes-plugin?label=hermes-plugin&logo=pypi)](https://pypi.org/project/agentguard-hermes-plugin/)

Data collection for AgentGuard: the plugins and SDKs that stream AI-agent telemetry (sessions, prompts, LLM calls, tool calls) from **Claude Code**, **OpenClaw**, and **Hermes Agent** into [Timeplus](https://timeplus.com).

AgentGuard itself — the streaming pipeline, detection rules, and dashboard — is a **Timeplus app** and lives in [timeplus-io/apps](https://github.com/timeplus-io/apps/tree/main/apps/agentguard) (`io.timeplus.agentguard`, database `ag`). This repository only contains the collection side.

```
 Claude Code ─┐  hook plugin ──┐
 OpenClaw    ─┼─ hook plugin ──┼──▶ ag.agentguard_hook_events ─┐
 Hermes      ─┘  hook plugin ──┘                                ├─▶ AgentGuard Timeplus app
 Claude Code / OpenInference ── OTLP/HTTP :4318 ──▶ ag.otel_* ──┘   (CIM, metrics, rules, dashboard)
└──────────── this repo ────────────┘                              └──── timeplus-io/apps ────┘
```

> **The standalone AgentGuard server is retired.** The Go backend, React UI, Docker image, setup wizard, and `install.sh` installer used to live here. They were removed after the move to the Timeplus app; the last release that contains them is [`v0.9.3`](https://github.com/timeplus-io/agentguard/tree/v0.9.3).

## Quick start

1. **Install the AgentGuard app** on Timeplus Enterprise — see the [app README](https://github.com/timeplus-io/apps/tree/main/apps/agentguard). It creates database `ag` with the `agentguard_hook_events` stream and an OTLP/HTTP input (default port `4318`).
2. **Install a plugin** on each agent host and point it at Timeplus:

   | Agent | Package | Guide |
   |---|---|---|
   | Claude Code | `npm install -g @timeplus/agentguard-claudecode-plugin` | [README](agents/claudecode/agentguard-plugin/README.md) |
   | OpenClaw | `openclaw plugins install @timeplus/agentguard-openclaw-plugin` | [README](agents/openclaw/agentguard-plugin/README.md) |
   | Hermes Agent | `pip install agentguard-hermes-plugin` | [README](agents/hermes/agentguard-plugin/README.md) |

   [`docs/plugin-install.md`](docs/plugin-install.md) covers configuration for all three. An agent can also do the install for you with the [`skills/agentguard`](skills/agentguard/SKILL.md) skill.
3. **Verify** events are arriving:

   ```sql
   SELECT agent_type, hook_name, agent_id, event_time
   FROM table(ag.agentguard_hook_events)
   ORDER BY event_time DESC LIMIT 10;
   ```

## Configuration at a glance

Plugins are configured with environment variables (Claude Code, Hermes) or plugin config (OpenClaw). The essentials:

| Variable | Default | Description |
|---|---|---|
| `AGENTGUARD_TIMEPLUS_URL` | `http://localhost:3218` | Timeplus base URL — timeplusd HTTP port in `timeplusd` mode, the REST API base (port 8000) in `rest` mode |
| `AGENTGUARD_INGEST_API` | `timeplusd` | `timeplusd` (direct ingest on 3218) or `rest` (Timeplus REST API, for deployments where only port 8000 is exposed) |
| `AGENTGUARD_USERNAME` / `AGENTGUARD_PASSWORD` | `proton` / _(empty)_ | Timeplus credentials (Basic auth in both modes) |
| `AGENTGUARD_DATABASE` | `ag` | Database owning the AgentGuard streams (the app's `db_name`) |
| `AGENTGUARD_STREAM` | `agentguard_hook_events` | Target stream |
| `AGENTGUARD_AGENT_ID` | hostname | Identifies this agent instance |
| `AGENTGUARD_DEPLOYMENT_ID` / `AGENTGUARD_DEPLOYMENT_NAME` | | Groups agents under a logical deployment |

Full reference: [`docs/plugin-install.md`](docs/plugin-install.md).

## Repository layout

| Path | Contents |
|---|---|
| `agents/claudecode/` | Claude Code hook plugin (TypeScript, npm) + local install Makefile + OTel guide |
| `agents/openclaw/` | OpenClaw plugin (TypeScript, npm) + Docker dev harness + scenario tests |
| `agents/hermes/` | Hermes Agent plugin (Python, PyPI) + Docker dev harness |
| `agents/litellm/`, `agents/paperclip/`, `agents/xearxng/` | Supporting dev/demo environments (LLM proxy, Paperclip showcase, search) |
| `sdk/python/`, `sdk/javascript/` | OpenTelemetry-based SDKs for instrumenting custom agents |
| `skills/agentguard/` | Agent skill that installs and configures the plugins |
| `docs/` | [Plugin install](docs/plugin-install.md), [CIM event model](docs/cim.md), [skills](docs/skills.md) and [memory](docs/memory.md) monitoring |

## Development

```bash
make build            # build all three plugins
make test             # run all plugin unit tests
make package-plugins  # pack plugins into packages/ (npm tarballs + Hermes wheel)
```

Per plugin:

```bash
cd agents/claudecode/agentguard-plugin && npm install && npm test && npm run typecheck && npm run build
cd agents/openclaw/agentguard-plugin  && npm install && npm test && npm run build
cd agents/hermes/agentguard-plugin    && python3.12 -m unittest discover -s tests   # needs Python >= 3.10
```

## Releasing plugins

Plugins are versioned and published on their own; nothing in CI publishes them. Bump the version in `agents/claudecode/agentguard-plugin/package.json`, `agents/openclaw/agentguard-plugin/package.json`, and `agents/hermes/agentguard-plugin/pyproject.toml`, merge, then:

```bash
cd agents/claudecode/agentguard-plugin && npm publish   # prepublishOnly rebuilds dist/
cd agents/openclaw/agentguard-plugin  && npm publish
cd agents/hermes/agentguard-plugin    && make publish   # hatch build + twine upload
```

## A note on tool-call holds

The plugins still contain the synchronous hold-and-wait path (`AGENTGUARD_HOLDS_ENABLED`, `AGENTGUARD_URL`), which called the standalone server's `/api/holds` endpoint. The Timeplus app has no hold capability, so leave `AGENTGUARD_HOLDS_ENABLED=false` (the default); the plugins then run in observe-only mode.
