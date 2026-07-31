package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// APIDocs serves Swagger UI at /docs with an inline OpenAPI 3.0 spec.
func APIDocs() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Data(http.StatusOK, "text/html; charset=utf-8", []byte(swaggerHTML))
	}
}

// APIRedoc serves Redoc UI at /docs/redoc.
func APIRedoc() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Data(http.StatusOK, "text/html; charset=utf-8", []byte(redocHTML))
	}
}

// APISpec serves the raw OpenAPI JSON at /docs/openapi.json.
func APISpec() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Data(http.StatusOK, "application/json; charset=utf-8", []byte(openAPISpec))
	}
}

const redocHTML = `<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="UTF-8"/>
  <title>AgentGuard API — Redoc</title>
  <meta name="viewport" content="width=device-width, initial-scale=1"/>
  <style>
    body { margin: 0; }
    .topbar { display: flex; align-items: center; justify-content: space-between; padding: 12px 24px; background: #1d1a25; border-bottom: 1px solid #2c2834; }
    .topbar-title { font-family: sans-serif; font-size: 15px; font-weight: 600; color: #e7dff0; }
    .topbar-link { font-family: sans-serif; font-size: 12px; color: #ffb0cf; text-decoration: none; }
    .topbar-link:hover { text-decoration: underline; }
  </style>
</head>
<body>
  <div class="topbar">
    <span class="topbar-title">AgentGuard API</span>
    <a class="topbar-link" href="/docs">Switch to Swagger UI</a>
  </div>
  <redoc spec-url="/docs/openapi.json" hide-download-button></redoc>
  <script src="https://cdn.redoc.ly/redoc/latest/bundles/redoc.standalone.js"></script>
</body>
</html>`

const swaggerHTML = `<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="UTF-8"/>
  <title>AgentGuard API</title>
  <meta name="viewport" content="width=device-width, initial-scale=1"/>
  <link rel="stylesheet" href="https://unpkg.com/swagger-ui-dist@5/swagger-ui.css"/>
  <style>
    body { margin: 0; background: #15121d; }
    .swagger-ui .topbar { background: #1d1a25; border-bottom: 1px solid #2c2834; }
    .swagger-ui .topbar-wrapper img { content: url('data:image/svg+xml,<svg/>'); }
    .swagger-ui .topbar .topbar-wrapper a span { display: none; }
    .swagger-ui .info .title { color: #e7dff0; }
    .alt-link { position: fixed; top: 14px; right: 20px; z-index: 9999; font-family: sans-serif; font-size: 12px; color: #ffb0cf; text-decoration: none; }
    .alt-link:hover { text-decoration: underline; }
  </style>
</head>
<body>
  <a class="alt-link" href="/docs/redoc">Switch to Redoc</a>
  <div id="swagger-ui"></div>
  <script src="https://unpkg.com/swagger-ui-dist@5/swagger-ui-bundle.js"></script>
  <script>
    SwaggerUIBundle({
      url: '/docs/openapi.json',
      dom_id: '#swagger-ui',
      presets: [SwaggerUIBundle.presets.apis, SwaggerUIBundle.SwaggerUIStandalonePreset],
      layout: 'BaseLayout',
      deepLinking: true,
      defaultModelsExpandDepth: 1,
      defaultModelExpandDepth: 2,
    })
  </script>
</body>
</html>`

const openAPISpec = `{
  "openapi": "3.0.3",
  "info": {
    "title": "AgentGuard API",
    "version": "1.0.0",
    "description": "AgentGuard is a real-time security monitoring and governance platform for AI coding agents (OpenClaw, Claude Code).\n\nAll data is sourced from **Timeplus Enterprise** streaming SQL. The server falls back gracefully when Timeplus is unavailable.\n\n### Authentication\nMost endpoints require an active session. Call \u0060POST /auth/login\u0060 with Timeplus credentials to receive an \u0060agentguard_session\u0060 httpOnly cookie. Include \u0060credentials: 'include'\u0060 on all fetch calls. The exceptions are \u0060/auth/*\u0060 and \u0060/setup/*\u0060, which are unauthenticated.\n\nOn first launch the setup wizard must run before login is possible. Call \u0060GET /auth/status\u0060 to check \u0060setup_complete\u0060 and \u0060logged_in\u0060.\n\n### Agent ID format\nMany endpoints accept an \u0060:id\u0060 path parameter in the compound form \u0060{deploymentId}__{agentId}\u0060 (double underscore separator). Both parts must match \u0060^[a-zA-Z0-9._:-]+$\u0060.\n\n### Time filtering\n\u0060since\u0060 / \u0060until\u0060 query parameters accept **RFC 3339** timestamps, e.g. \u00602026-04-01T00:00:00Z\u0060.\n\n### Pagination\nList endpoints accept \u0060?page=1&limit=15\u0060 and return \u0060total\u0060 so clients can compute page count."
  },
  "servers": [
    { "url": "/api", "description": "AgentGuard backend (same origin)" }
  ],
  "tags": [
    { "name": "Dashboard",   "description": "High-level summary counters shown on the main dashboard." },
    { "name": "Agents",      "description": "Agent fleet — each unique (deployment_id, agent_id) pair registered via hook events." },
    { "name": "Sessions",    "description": "Sessions and runs within an agent. A session groups one user conversation; a run is a single LLM turn within that session." },
    { "name": "Metrics",     "description": "Aggregated LLM token usage and tool-call statistics." },
    { "name": "Threats",     "description": "Security detections emitted by active rule materialized views." },
    { "name": "Cost",        "description": "Token-based cost estimates computed from configurable per-model pricing." },
    { "name": "Rules",       "description": "Security rule packs. Each enabled rule has a corresponding Proton materialized view that writes matches to agentguard_security_events." },
    { "name": "Catalog",     "description": "File-based rule catalog. The initial catalog ships embedded in the binary as YAML files; users can override entries or add new ones via import. Proton tracks *install state*; the catalog tracks *definitions*." },
    { "name": "Alerts",      "description": "Real-time alert delivery — SSE notification stream." },
    { "name": "Proton",      "description": "Streaming proxy — send arbitrary SQL to Timeplus and receive NDJSON rows." },
    { "name": "Onboarding",  "description": "Agent onboarding wizard — generates configuration snippets for connecting agents." },
    { "name": "Auth",        "description": "Session authentication — login, logout, and status check. These endpoints are **unauthenticated** (no session cookie required)." },
    { "name": "Setup",       "description": "First-time setup wizard — connection test and resource provisioning. These endpoints are **unauthenticated** and are only used before setup is complete." },
    { "name": "Holds",       "description": "Human-in-the-loop holds for tool-call approval. Agent plugins call POST /holds synchronously before executing a tool; the request blocks until a human approves/denies, the session ends, or the timeout expires. Dashboard users interact via the other holds endpoints." },
    { "name": "Activity",    "description": "Combined chronological feed of hold events and (optionally) auto-allowed tool-call hook events. Powers the real-time activity view in the dashboard." }
  ],
  "security": [
    { "cookieAuth": [] }
  ],
  "paths": {

    "/dashboard": {
      "get": {
        "tags": ["Dashboard"],
        "summary": "Get dashboard summary stats",
        "description": "Returns high-level counters for the main dashboard: total agents seen, total hook events ingested, active threats detected, and number of enabled security rules.",
        "operationId": "getDashboard",
        "responses": {
          "200": {
            "description": "Dashboard summary",
            "content": { "application/json": { "schema": { "$ref": "#/components/schemas/Dashboard" },
              "example": {
                "total_agents": 4,
                "total_events": 18432,
                "total_threats": 7,
                "active_rules": 3
              }
            }}
          },
          "503": { "description": "Timeplus unavailable — server returns cached or zero values" }
        }
      }
    },

    "/agents": {
      "get": {
        "tags": ["Agents"],
        "summary": "List agents",
        "description": "Returns a paginated list of all unique agents observed via hook events. Each row aggregates event counts, session counts, token usage, and the last-seen timestamp.\n\nAgents are identified by the compound \u0060(deployment_id, agent_id)\u0060 pair. The \u0060id\u0060 field returned is \u0060{deployment_id}__{agent_id}\u0060 and is used as the path parameter for detail endpoints.",
        "operationId": "listAgents",
        "parameters": [
          { "name": "page",  "in": "query", "description": "1-based page number.", "schema": { "type": "integer", "default": 1, "minimum": 1 } },
          { "name": "limit", "in": "query", "description": "Rows per page.", "schema": { "type": "integer", "default": 15, "minimum": 1, "maximum": 200 } }
        ],
        "responses": {
          "200": {
            "description": "Paginated agent list",
            "content": { "application/json": { "schema": {
              "type": "object",
              "properties": {
                "agents": { "type": "array", "items": { "$ref": "#/components/schemas/Agent" } },
                "total":  { "type": "integer", "description": "Total number of agents (for pagination)." },
                "page":   { "type": "integer" },
                "limit":  { "type": "integer" }
              }
            }, "example": {
              "agents": [{
                "agent_id": "claude-code",
                "deployment_id": "eng-team",
                "deployment_name": "Engineering Team",
                "event_count": 5200,
                "session_count": 34,
                "total_tokens": 1280000,
                "llm_call_count": 412,
                "last_seen": "2026-04-09T14:22:01Z"
              }],
              "total": 4, "page": 1, "limit": 15
            }}}
          }
        }
      }
    },

    "/agents/{id}": {
      "get": {
        "tags": ["Agents"],
        "summary": "Get agent detail",
        "description": "Returns detailed statistics for a single agent, including per-token-type breakdown (input / output / cache-read), block count, first-seen timestamp, and the list of LLM models used.\n\nOptional \u0060since\u0060 / \u0060until\u0060 filters restrict the aggregation window.",
        "operationId": "getAgent",
        "parameters": [
          { "name": "id", "in": "path", "required": true,
            "description": "Compound agent ID: \u0060{deploymentId}__{agentId}\u0060. Example: \u0060eng-team__claude-code\u0060.",
            "schema": { "type": "string" }, "example": "eng-team__claude-code" },
          { "name": "since", "in": "query", "description": "Return events on or after this RFC 3339 timestamp.", "schema": { "type": "string", "format": "date-time" } },
          { "name": "until", "in": "query", "description": "Return events on or before this RFC 3339 timestamp.", "schema": { "type": "string", "format": "date-time" } }
        ],
        "responses": {
          "200": { "description": "Agent detail", "content": { "application/json": { "schema": { "$ref": "#/components/schemas/AgentDetail" },
            "example": {
              "agent_id": "claude-code",
              "deployment_id": "eng-team",
              "deployment_name": "Engineering Team",
              "event_count": 5200,
              "session_count": 34,
              "total_tokens": 1280000,
              "input_tokens": 820000,
              "output_tokens": 410000,
              "cache_read_tokens": 50000,
              "llm_call_count": 412,
              "block_count": 3,
              "first_seen": "2026-03-01T09:00:00Z",
              "last_seen": "2026-04-09T14:22:01Z",
              "models": ["claude-sonnet-4-6", "claude-opus-4-6"]
            }
          }}},
          "400": { "description": "Invalid agent id — must use \u0060{deploymentId}__{agentId}\u0060 format." },
          "404": { "description": "No events found for this agent." },
          "503": { "description": "Timeplus unavailable." }
        }
      }
    },

    "/agents/{id}/sessions": {
      "get": {
        "tags": ["Sessions"],
        "summary": "List sessions for an agent",
        "description": "Returns all sessions for the given agent, ordered by \u0060last_seen\u0060 descending (newest first).\n\nA **session** maps to a single user conversation. Temporary sessions (\u0060temp:*\u0060) and slug-generator internals are automatically filtered out. Only sessions that contain at least one run are returned.\n\nThe \u0060session_type\u0060 is derived from the \u0060session_key\u0060 pattern:\n- \u0060Web UI\u0060 — interactive dashboard session\n- \u0060HTTP API\u0060 — OpenAI-compat API client\n- \u0060Cron\u0060 — scheduled trigger\n- \u0060Sub-agent\u0060 — spawned by another agent",
        "operationId": "listSessions",
        "parameters": [
          { "name": "id", "in": "path", "required": true,
            "description": "Compound agent ID: \u0060{deploymentId}__{agentId}\u0060.",
            "schema": { "type": "string" }, "example": "eng-team__claude-code" },
          { "name": "since", "in": "query", "description": "Only include sessions with events on or after this RFC 3339 timestamp.", "schema": { "type": "string", "format": "date-time" } },
          { "name": "until", "in": "query", "description": "Only include sessions with events on or before this RFC 3339 timestamp.", "schema": { "type": "string", "format": "date-time" } }
        ],
        "responses": {
          "200": { "description": "Session list (newest first)", "content": { "application/json": {
            "schema": { "type": "array", "items": { "$ref": "#/components/schemas/Session" } },
            "example": [{
              "session_id": "a1b2c3d4-0000-0000-0000-000000000001",
              "session_key": "announce:v1:agent:claude-code:openai-user:abc123",
              "session_type": "HTTP API",
              "run_count": 8,
              "event_count": 142,
              "first_seen": "2026-04-09T10:00:00Z",
              "last_seen": "2026-04-09T10:45:00Z"
            }]
          }}}
        }
      }
    },

    "/agents/{id}/sessions/{session_id}": {
      "get": {
        "tags": ["Sessions"],
        "summary": "Get session detail with runs",
        "description": "Returns full detail for a single session, including its status (\u0060active\u0060 / \u0060ended\u0060) and an ordered list of **runs**.\n\nA **run** corresponds to one LLM turn — typically triggered by a user message. Each run shows its model, tool-call count, duration, and whether any calls were blocked.\n\nThe \u0060status\u0060 field is \u0060ended\u0060 when a \u0060session_end\u0060 hook event has been recorded for this session.",
        "operationId": "getSession",
        "parameters": [
          { "name": "id", "in": "path", "required": true, "description": "Compound agent ID.", "schema": { "type": "string" }, "example": "eng-team__claude-code" },
          { "name": "session_id", "in": "path", "required": true, "description": "Session UUID.", "schema": { "type": "string" }, "example": "a1b2c3d4-0000-0000-0000-000000000001" }
        ],
        "responses": {
          "200": { "description": "Session detail", "content": { "application/json": { "schema": { "$ref": "#/components/schemas/SessionDetail" },
            "example": {
              "session_id": "a1b2c3d4-0000-0000-0000-000000000001",
              "session_key": "announce:v1:agent:claude-code:openai-user:abc123",
              "session_type": "HTTP API",
              "status": "ended",
              "total_tool_calls": 23,
              "runs": [{
                "run_id": "chatcmpl_4ff08efb-c086-4927-9305-20ef7f95d329",
                "start_time": "2026-04-09T10:00:00Z",
                "end_time": "2026-04-09T10:00:18Z",
                "event_count": 14,
                "model": "claude-sonnet-4-6",
                "tool_count": 5,
                "has_block": false
              }]
            }
          }}},
          "404": { "description": "Session not found." }
        }
      }
    },

    "/agents/{id}/sessions/{session_id}/runs/{run_id}/trace": {
      "get": {
        "tags": ["Sessions"],
        "summary": "Get waterfall trace for a run",
        "description": "Builds a waterfall trace from raw \u0060agentguard_hook_events\u0060 — no OTel dependency required.\n\nEach hook event becomes a **child span** whose duration is the gap to the next event. The root span covers the full run window. Span kinds:\n- \u0060run\u0060 — root span for the entire LLM turn\n- \u0060llm\u0060 — events related to model inference (\u0060llm_input\u0060, \u0060llm_output\u0060, \u0060before_model_resolve\u0060, etc.)\n- \u0060tool\u0060 — tool-call lifecycle (\u0060before_tool_call\u0060, \u0060after_tool_call\u0060, \u0060tool_result_persist\u0060)\n- \u0060lifecycle\u0060 — all other hook events\n\nSpan status is \u0060blocked\u0060 when \u0060hook_decision = block\u0060, \u0060error\u0060 when \u0060event_data.error\u0060 is set, otherwise \u0060ok\u0060.",
        "operationId": "getRunTrace",
        "parameters": [
          { "name": "id", "in": "path", "required": true, "description": "Compound agent ID.", "schema": { "type": "string" }, "example": "eng-team__claude-code" },
          { "name": "session_id", "in": "path", "required": true, "description": "Session UUID.", "schema": { "type": "string" }, "example": "a1b2c3d4-0000-0000-0000-000000000001" },
          { "name": "run_id", "in": "path", "required": true,
            "description": "Run ID. Supports multiple formats: \u0060chatcmpl_{uuid}\u0060 (HTTP API), \u0060announce:v1:agent:...:run:{uuid}\u0060 (sub-agent), or plain UUID.",
            "schema": { "type": "string" }, "example": "chatcmpl_4ff08efb-c086-4927-9305-20ef7f95d329" }
        ],
        "responses": {
          "200": { "description": "Waterfall trace spans", "content": { "application/json": { "schema": { "$ref": "#/components/schemas/TraceResponse" },
            "example": {
              "spans": [
                { "span_id": "chatcmpl_4ff08efb", "parent_id": "", "kind": "run", "name": "run", "start_ms": 1744200000000, "duration_ms": 18400, "status": "ok", "attributes": {} },
                { "span_id": "evt-0", "parent_id": "chatcmpl_4ff08efb", "kind": "llm", "name": "llm_input", "start_ms": 1744200000000, "duration_ms": 120, "status": "ok", "attributes": { "model": "claude-sonnet-4-6" } },
                { "span_id": "evt-2", "parent_id": "chatcmpl_4ff08efb", "kind": "tool", "name": "before_tool_call", "start_ms": 1744200000800, "duration_ms": 450, "status": "ok", "attributes": { "tool_name": "Read" } }
              ]
            }
          }}},
          "404": { "description": "No events found for this run." }
        }
      }
    },

    "/agents/{id}/events": {
      "get": {
        "tags": ["Agents"],
        "summary": "List hook events for an agent",
        "description": "Returns raw \u0060agentguard_hook_events\u0060 rows for the given agent, ordered newest-first. Useful for audit trails and forensic investigation.\n\nEach event record includes the hook name, timestamp, tool name (if applicable), model, hook decision, and the raw \u0060event_data\u0060 JSON payload.",
        "operationId": "listEvents",
        "parameters": [
          { "name": "id", "in": "path", "required": true, "description": "Compound agent ID.", "schema": { "type": "string" }, "example": "eng-team__claude-code" },
          { "name": "page",  "in": "query", "schema": { "type": "integer", "default": 1 } },
          { "name": "limit", "in": "query", "schema": { "type": "integer", "default": 50 } },
          { "name": "since", "in": "query", "description": "RFC 3339 lower bound on event_time.", "schema": { "type": "string", "format": "date-time" } },
          { "name": "until", "in": "query", "description": "RFC 3339 upper bound on event_time.", "schema": { "type": "string", "format": "date-time" } }
        ],
        "responses": {
          "200": { "description": "Paginated hook events", "content": { "application/json": { "schema": {
            "type": "object",
            "properties": {
              "events": { "type": "array", "items": { "$ref": "#/components/schemas/HookEvent" } },
              "total":  { "type": "integer" }
            }
          }, "example": {
            "events": [{
              "hook_name": "before_tool_call",
              "event_time": "2026-04-09T10:00:01Z",
              "session_id": "a1b2c3d4-0000-0000-0000-000000000001",
              "run_id": "chatcmpl_4ff08efb-c086-4927-9305-20ef7f95d329",
              "agent_id": "claude-code",
              "deployment_id": "eng-team",
              "tool_name": "Bash",
              "provider": "anthropic",
              "model": "claude-sonnet-4-6",
              "hook_decision": "observe",
              "block_reason": "",
              "event_data": "{\"command\":\"ls -la\"}"
            }],
            "total": 5200
          }}}}
        }
      }
    },

    "/metrics/llm": {
      "get": {
        "tags": ["Metrics"],
        "summary": "Get LLM token usage metrics",
        "description": "Returns aggregated LLM token consumption broken down by model and provider. Includes total calls, input tokens, output tokens, and cache-read tokens.\n\nData is read from \u0060agentguard_hook_events\u0060 where \u0060hook_name = 'llm_output'\u0060 and token counts are stored in the \u0060event_data\u0060 JSON field.",
        "operationId": "getLLMMetrics",
        "parameters": [
          { "name": "since", "in": "query", "description": "RFC 3339 lower bound.", "schema": { "type": "string", "format": "date-time" } },
          { "name": "until", "in": "query", "description": "RFC 3339 upper bound.", "schema": { "type": "string", "format": "date-time" } }
        ],
        "responses": {
          "200": { "description": "LLM token usage metrics", "content": { "application/json": { "schema": { "$ref": "#/components/schemas/LLMMetrics" },
            "example": {
              "period": { "since": "2026-04-01T00:00:00Z", "until": "2026-04-09T23:59:59Z" },
              "filters": {},
              "tokens": { "input": 820000, "output": 410000, "total": 1280000, "cache_read": 50000, "cache_write": 0 },
              "calls": { "total": 412, "success": 410, "failed": 2, "success_rate": 0.9951 },
              "stop_reasons": { "end_turn": 408, "max_tokens": 4 },
              "latency_ms": { "p50": 2100, "p95": 8400, "p99": 14200, "avg": 2850, "min": 320, "max": 18400, "samples": 412 },
              "throughput": [{ "bucket": "2026-04-09T10:00:00Z", "llm_calls": 12, "input_tokens": 24000, "output_tokens": 12000, "total_tokens": 36000 }]
            }
          }}}
        }
      }
    },

    "/metrics/tools": {
      "get": {
        "tags": ["Metrics"],
        "summary": "Get tool usage metrics",
        "description": "Returns tool-call counts aggregated by tool name, including the number of calls that were blocked by a security rule.\n\nDerived from \u0060agentguard_hook_events\u0060 where \u0060hook_name = 'before_tool_call'\u0060.",
        "operationId": "getToolMetrics",
        "parameters": [
          { "name": "since", "in": "query", "description": "RFC 3339 lower bound.", "schema": { "type": "string", "format": "date-time" } },
          { "name": "until", "in": "query", "description": "RFC 3339 upper bound.", "schema": { "type": "string", "format": "date-time" } }
        ],
        "responses": {
          "200": { "description": "Tool usage metrics", "content": { "application/json": { "schema": { "$ref": "#/components/schemas/ToolMetrics" },
            "example": {
              "period": { "since": "2026-04-01T00:00:00Z" },
              "filters": {},
              "summary": { "total_calls": 1840, "success_calls": 1830, "failed_calls": 10, "success_rate": 0.9946 },
              "by_tool": [
                { "tool_name": "Read",  "total_calls": 820, "success_calls": 820, "failed_calls": 0, "success_rate": 1.0, "latency_ms": { "p50": 45, "p95": 120, "p99": 280, "avg": 58, "min": 10, "max": 840, "samples": 820 } },
                { "tool_name": "Bash",  "total_calls": 310, "success_calls": 302, "failed_calls": 8, "success_rate": 0.9742, "latency_ms": { "p50": 380, "p95": 2100, "p99": 4800, "avg": 620, "min": 80, "max": 8200, "samples": 310 } }
              ]
            }
          }}}
        }
      }
    },

    "/threats": {
      "get": {
        "tags": ["Threats"],
        "summary": "List security threats",
        "description": "Returns detections written to \u0060agentguard_security_events\u0060 by active rule materialized views, ordered newest-first.\n\nEach detection record includes the triggering rule, severity, affected agent, session context, and the raw hook event data that caused the match.",
        "operationId": "listThreats",
        "parameters": [
          { "name": "severity", "in": "query", "description": "Filter by severity level.", "schema": { "type": "string", "enum": ["critical", "warning", "info"] } },
          { "name": "status",   "in": "query", "description": "Filter by threat lifecycle status.", "schema": { "type": "string", "enum": ["open", "acknowledged", "cleared"] } },
          { "name": "agent_id", "in": "query", "description": "Filter to a specific agent_id.", "schema": { "type": "string" } },
          { "name": "page",  "in": "query", "schema": { "type": "integer", "default": 1 } },
          { "name": "limit", "in": "query", "schema": { "type": "integer", "default": 20 } }
        ],
        "responses": {
          "200": { "description": "Threat list", "content": { "application/json": { "schema": {
            "type": "object",
            "properties": {
              "threats": { "type": "array", "items": { "$ref": "#/components/schemas/Threat" } },
              "total": { "type": "integer" },
              "page":  { "type": "integer" },
              "limit": { "type": "integer" }
            }
          }, "example": {
            "threats": [{
              "threat_id": "a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4",
              "rule_id": "rp-001",
              "rule_name": "Prompt Injection Shield",
              "severity": "critical",
              "agent_id": "claude-code",
              "deployment_id": "eng-team",
              "session_id": "a1b2c3d4-0000-0000-0000-000000000001",
              "event_count": 3,
              "first_seen": "2026-04-09T10:00:00Z",
              "last_seen": "2026-04-09T10:05:00Z",
              "status": "open",
              "events": [{ "detected_at": "2026-04-09T10:00:00Z", "run_id": "chatcmpl_4ff08efb", "hook_name": "llm_input", "tool_name": "", "message": "Prompt injection detected", "event_data": "{}" }]
            }],
            "total": 7, "page": 1, "limit": 20
          }}}}
        }
      }
    },

    "/threats/{id}": {
      "get": {
        "tags": ["Threats"],
        "summary": "Get threat detail",
        "description": "Returns a single deduplicated threat by its computed threat_id (MD5 of agent_id+deployment_id+session_id+rule_id). Includes up to 50 raw security events and, if a hold is currently pending for this threat, a pending_hold object with Approve/Deny context.",
        "operationId": "getThreat",
        "parameters": [
          { "name": "id", "in": "path", "required": true, "description": "Threat ID — MD5 hex string computed from (agent_id, deployment_id, session_id, rule_id).", "schema": { "type": "string" } }
        ],
        "responses": {
          "200": { "description": "Threat detail", "content": { "application/json": { "schema": { "$ref": "#/components/schemas/ThreatDetailResponse" } } } },
          "404": { "description": "Threat not found." },
          "503": { "description": "Timeplus unavailable." }
        }
      }
    },

    "/threats/{id}/acknowledge": {
      "post": {
        "tags": ["Threats"],
        "summary": "Acknowledge a threat",
        "description": "Sets the threat status to acknowledged. The :id must be the MD5 computed from the four body fields. Acknowledged threats auto-reopen when new security events arrive.",
        "operationId": "acknowledgeThreat",
        "parameters": [
          { "name": "id", "in": "path", "required": true, "description": "Threat ID (MD5 hex).", "schema": { "type": "string" } }
        ],
        "requestBody": {
          "required": true,
          "content": { "application/json": { "schema": { "$ref": "#/components/schemas/ThreatStatusRequest" },
            "example": { "agent_id": "claude-code", "deployment_id": "eng-team", "session_id": "a1b2c3d4-0000-0000-0000-000000000001", "rule_id": "rp-001" }
          }}
        },
        "responses": {
          "200": { "description": "Status updated.", "content": { "application/json": { "example": { "ok": true, "status": "acknowledged" } } } },
          "400": { "description": "Missing required fields or body identity does not match :id." }
        }
      }
    },

    "/threats/{id}/clear": {
      "post": {
        "tags": ["Threats"],
        "summary": "Clear a threat",
        "description": "Sets the threat status to cleared. Same request body as acknowledge. Cleared threats auto-reopen when new security events arrive.",
        "operationId": "clearThreat",
        "parameters": [
          { "name": "id", "in": "path", "required": true, "description": "Threat ID (MD5 hex).", "schema": { "type": "string" } }
        ],
        "requestBody": {
          "required": true,
          "content": { "application/json": { "schema": { "$ref": "#/components/schemas/ThreatStatusRequest" },
            "example": { "agent_id": "claude-code", "deployment_id": "eng-team", "session_id": "a1b2c3d4-0000-0000-0000-000000000001", "rule_id": "rp-001" }
          }}
        },
        "responses": {
          "200": { "description": "Status updated.", "content": { "application/json": { "example": { "ok": true, "status": "cleared" } } } },
          "400": { "description": "Missing required fields or body identity does not match :id." }
        }
      }
    },

    "/cost": {
      "get": {
        "tags": ["Cost"],
        "summary": "Get cost governance summary",
        "description": "Returns token-based cost estimates computed by joining \u0060agentguard_hook_events\u0060 token counts with the \u0060agentguard_pricing\u0060 table.\n\nIncludes a daily spend time-series and a breakdown by model. Costs are in USD. Pricing data is seeded with public list prices and can be updated via the \u0060/cost/pricing\u0060 endpoints.\n\n**Period semantics:** all month-to-date windows use UTC calendar months. The prior-period delta compares the current month-to-date to the same elapsed window in the previous month, clipped to the start of the current month when the previous month is shorter (e.g. March 31 has 30 elapsed days but February has only 28, so the prior window is clipped to end at March 1 UTC).\n\n**Budget-derived fields** (\u0060forecast_budget_delta_pct\u0060, \u0060budget_amount\u0060, \u0060budget_status\u0060) are only populated when at least one per-deployment budget is configured via \u0060/cost/budget\u0060.",
        "operationId": "getCost",
        "parameters": [
          { "name": "since", "in": "query", "description": "RFC 3339 lower bound for the aggregation window.", "schema": { "type": "string", "format": "date-time" } },
          { "name": "until", "in": "query", "description": "RFC 3339 upper bound.", "schema": { "type": "string", "format": "date-time" } }
        ],
        "responses": {
          "200": { "description": "Cost summary", "content": { "application/json": { "schema": { "$ref": "#/components/schemas/CostResponse" },
            "example": {
              "month_to_date": 12.84,
              "month_to_date_prev": 10.50,
              "month_to_date_delta_pct": 22.3,
              "forecasted": 38.52,
              "forecast_budget_delta_pct": -22.96,
              "budget_amount": 50.00,
              "budget_status": "under",
              "avg_token_cost": 8.25,
              "avg_token_cost_prev": 7.92,
              "avg_token_cost_delta_pct": 4.17,
              "cache_savings": 1.20,
              "wasted_spend": 0.43,
              "daily_spend": [
                { "day": "2026-04-08", "amount": 4.20 },
                { "day": "2026-04-09", "amount": 8.64 }
              ],
              "token_breakdown": [
                { "label": "Input",  "percentage": 64, "tokens": 820000, "amount": 8.20 },
                { "label": "Output", "percentage": 32, "tokens": 410000, "amount": 4.10 }
              ],
              "agent_model_costs": [
                { "model": "claude-sonnet-4-6", "agent_name": "Engineering Team", "cost": 11.52, "requests": "390", "provider": "anthropic", "agent_id": "claude-code", "input_tokens": 780000, "output_tokens": 390000, "cache_tokens": 48000 }
              ]
            }
          }}}
        }
      }
    },

    "/cost/pricing": {
      "get": {
        "tags": ["Cost"],
        "summary": "List model pricing entries",
        "description": "Returns all entries in the \u0060agentguard_pricing\u0060 mutable stream. Each entry defines the cost per million tokens (input, output, cache-read) for a provider/model combination.\n\nDefault entries are seeded at startup for Anthropic and OpenAI models.",
        "operationId": "listPricing",
        "responses": {
          "200": { "description": "Pricing table", "content": { "application/json": { "schema": {
            "type": "object",
            "properties": { "pricing": { "type": "array", "items": { "$ref": "#/components/schemas/PricingEntry" } } }
          }, "example": {
            "pricing": [
              { "provider": "anthropic", "model": "claude-sonnet-4-6", "input_cost_per_million": 3.0, "output_cost_per_million": 15.0, "cache_read_cost_per_million": 0.3 },
              { "provider": "openai",    "model": "gpt-4o",             "input_cost_per_million": 2.5, "output_cost_per_million": 10.0, "cache_read_cost_per_million": 1.25 }
            ]
          }}}}
        }
      },
      "post": {
        "tags": ["Cost"],
        "summary": "Upsert a model pricing entry",
        "description": "Inserts or updates a pricing entry for a provider/model pair. Uses Proton's mutable stream primary-key upsert semantics — sending the same \u0060(provider, model)\u0060 key overwrites the previous record.",
        "operationId": "upsertPricing",
        "requestBody": {
          "required": true,
          "content": { "application/json": { "schema": { "$ref": "#/components/schemas/PricingEntry" },
            "example": { "provider": "anthropic", "model": "claude-haiku-4-5", "input_cost_per_million": 0.8, "output_cost_per_million": 4.0, "cache_read_cost_per_million": 0.08 }
          }}
        },
        "responses": {
          "200": { "description": "Entry upserted successfully." },
          "400": { "description": "Missing or invalid fields." }
        }
      }
    },

    "/cost/pricing/{provider}/{model}": {
      "delete": {
        "tags": ["Cost"],
        "summary": "Delete a model pricing entry",
        "description": "Removes the pricing record for the given provider/model pair from \u0060agentguard_pricing\u0060.",
        "operationId": "deletePricing",
        "parameters": [
          { "name": "provider", "in": "path", "required": true, "description": "Provider name, e.g. \u0060anthropic\u0060 or \u0060openai\u0060.", "schema": { "type": "string" }, "example": "anthropic" },
          { "name": "model",    "in": "path", "required": true, "description": "Model name, e.g. \u0060claude-sonnet-4-6\u0060.", "schema": { "type": "string" }, "example": "claude-sonnet-4-6" }
        ],
        "responses": {
          "200": { "description": "Deleted." },
          "404": { "description": "Entry not found." }
        }
      }
    },

    "/cost/budget": {
      "get": {
        "tags": ["Cost"],
        "summary": "List per-deployment monthly budgets",
        "description": "Returns the current set of active monthly budgets reconstructed from the \u0060agentguard_cost_budgets\u0060 append-only stream via \u0060arg_max(field, updated_at) GROUP BY deployment_id HAVING NOT deleted\u0060.\n\nWhen at least one budget is configured, GET /cost populates \u0060forecast_budget_delta_pct\u0060, \u0060budget_amount\u0060, and \u0060budget_status\u0060.",
        "operationId": "getCostBudgets",
        "responses": {
          "200": { "description": "Active budgets", "content": { "application/json": { "schema": {
            "type": "object",
            "properties": { "budgets": { "type": "array", "items": { "$ref": "#/components/schemas/CostBudget" } } }
          }, "example": {
            "budgets": [
              { "deployment_id": "production", "monthly_budget": 30.0, "currency": "USD", "updated_by": "ops@example.com", "updated_at": "2026-05-14 09:32:11.241" },
              { "deployment_id": "staging",    "monthly_budget": 20.0, "currency": "USD", "updated_by": "ops@example.com", "updated_at": "2026-05-14 09:32:23.117" }
            ]
          }}}}
        }
      },
      "post": {
        "tags": ["Cost"],
        "summary": "Upsert a per-deployment monthly budget",
        "description": "Appends one row to the \u0060agentguard_cost_budgets\u0060 stream. \u0060updated_by\u0060 is the logged-in Timeplus username from the session — clients do not need to send it.\n\nSet \u0060deleted: true\u0060 to retire a budget; the stream is append-only so the audit trail is preserved.",
        "operationId": "upsertCostBudget",
        "requestBody": {
          "required": true,
          "content": { "application/json": { "schema": { "$ref": "#/components/schemas/CostBudgetUpsert" },
            "example": { "deployment_id": "production", "monthly_budget": 30.0, "currency": "USD" }
          }}
        },
        "responses": {
          "200": { "description": "Upserted." },
          "400": { "description": "Missing or invalid fields." }
        }
      }
    },

    "/rules": {
      "get": {
        "tags": ["Rules"],
        "summary": "List installed rules",
        "description": "Returns all non-deleted rules from the \u0060agentguard_rules\u0060 append-only stream. Current state is computed via \u0060arg_max(field, revision) GROUP BY rule_id\u0060.",
        "operationId": "listRules",
        "responses": {
          "200": { "description": "Installed rules with detection summary", "content": { "application/json": { "schema": { "$ref": "#/components/schemas/RulesResponse" },
            "example": {
              "rule_packs": [{
                "id": "rp-001",
                "name": "Prompt Injection Shield",
                "version": "1.0.0",
                "description": "Detects direct and indirect prompt injection attacks.",
                "enabled": true,
                "severity": "critical",
                "category": "prompt_injection",
                "fp_rate": "medium",
                "hook_sources": "llm_input",
                "block_policy": "hold",
                "detections_24h": 3
              }],
              "total_detections_24h": 3,
              "active_rule_density": 75,
              "recent_detections": [{ "rule_id": "rp-001", "rule_name": "Prompt Injection Shield", "severity": "critical", "agent_id": "claude-code", "hook_name": "llm_input", "message": "Injection detected", "detected_at": "2026-04-09T10:00:00Z" }]
            }}}}
        }
      },
      "post": {
        "tags": ["Rules"],
        "summary": "Create a custom rule",
        "description": "Thin create-custom-rule path: creates a zero-parameter custom definition (origin \u0060custom\u0060, sql_template equals the rendered SQL), saves it to the local catalog, and installs it (enabled, \u0060log_only\u0060) through the same lifecycle service. This is NOT a raw-SQL editor for installed rules.\n\nAn optional \u0060id\u0060 can be specified (letters, digits, hyphens, underscores; max 64 chars); if omitted a \u0060custom-\u0060 prefixed ID is auto-generated. The \u0060query_sql\u0060 must be a valid Proton \u0060SELECT\u0060 over \u0060agentguard_hook_events\u0060 outputting the \u0060agentguard_security_events\u0060 columns. The literal tokens \u0060'__rule_id__'\u0060 and \u0060'__rule_name__'\u0060 are substituted at create time.",
        "operationId": "createRule",
        "requestBody": {
          "required": true,
          "content": { "application/json": { "schema": { "$ref": "#/components/schemas/RuleInput" },
            "example": {
              "name": "Exfil Watch",
              "description": "Alerts when an agent reads and then immediately uploads a file.",
              "query_sql": "SELECT event_time AS detected_at, 'custom-exfil' AS rule_id, 'Exfil Watch' AS rule_name, 'critical' AS severity, agent_id, deployment_id, session_id, run_id, hook_name, tool_name, concat('Possible exfil by ', agent_id) AS message, event_data FROM agentguard_hook_events WHERE hook_name = 'before_tool_call' AND lower(event_data) LIKE '%upload%'",
              "severity": "critical",
              "category": "dlp",
              "fp_rate": "low",
              "hook_sources": "before_tool_call"
            }
          }}
        },
        "responses": {
          "201": { "description": "Custom rule created and installed.", "content": { "application/json": { "schema": { "$ref": "#/components/schemas/RuleInstance" } } } },
          "409": { "description": "Rule ID already exists in the catalog." },
          "400": { "description": "Invalid or missing fields." }
        }
      }
    },

    "/rules/catalog": {
      "get": {
        "tags": ["Catalog"],
        "summary": "List rule catalog",
        "description": "Returns all rule definitions from the file-based catalog (embedded initial rules merged with any local overrides), each annotated with install state from \u0060agentguard_rules\u0060.\n\nRules installed in Proton but absent from the catalog are included with \u0060tags: [\"orphaned\"]\u0060.\n\nUse this endpoint to power an install/uninstall UI — show the catalog, highlight installed entries, and call \u0060/rules/{id}/install\u0060 or \u0060DELETE /rules/{id}\u0060 accordingly. When \u0060upgradeable\u0060 is true the catalog has a newer version than what is installed — call \u0060/rules/{id}/upgrade\u0060 to update.",
        "operationId": "getRulesCatalog",
        "responses": {
          "200": { "description": "Rule catalog with installed status", "content": { "application/json": { "schema": {
            "type": "object",
            "properties": { "catalog": { "type": "array", "items": { "$ref": "#/components/schemas/CatalogItem" } } }
          }, "example": {
            "catalog": [
              { "id": "rp-001", "name": "Prompt Injection Shield", "version": "1.0.0", "description": "Detects prompt injection attacks.", "severity": "critical", "category": "prompt_injection", "fp_rate": "medium", "hook_sources": "llm_input", "author": "Timeplus", "tags": ["security", "prompt-injection"], "installed": true, "enabled": true, "installed_version": "1.0.0", "upgradeable": false },
              { "id": "rp-002", "name": "DLP Sentinel",            "version": "1.0.0", "description": "Prevents PII and secret leakage.",  "severity": "critical", "category": "dlp",              "fp_rate": "low",    "hook_sources": "tool_result_persist,after_tool_call,llm_output", "author": "Timeplus", "tags": ["security", "dlp"], "installed": false, "enabled": false, "upgradeable": false }
            ]
          }}}}
        }
      },
      "post": {
        "tags": ["Catalog"],
        "summary": "Create a custom rule definition",
        "description": "Creates a new custom catalog definition (\u0060origin\u0060 forced to \u0060custom\u0060) and publishes it to the local catalog. This validates and saves the definition only — it does NOT install the rule. Install separately via \u0060POST /rules/{id}/install\u0060.\n\nUnlike the thin \u0060POST /rules\u0060 path, the body is a full \u0060Definition\u0060 (parameter schema in \u0060config[]\u0060, \u0060{{ .Config.<key> }}\u0060 placeholders in \u0060sql_template\u0060). Use \u0060POST /rules/catalog/compile\u0060 to preview the rendered SQL before saving.",
        "operationId": "createCatalogRule",
        "requestBody": {
          "required": true,
          "content": { "application/json": { "schema": { "type": "object" },
            "example": {
              "id": "custom-secret-scan", "name": "Secret Scanner", "version": "1.0.0",
              "description": "Flags tool calls whose input matches a secret pattern.",
              "severity": "critical", "category": "dlp", "hook_sources": "before_tool_call",
              "sql_template": "SELECT event_time AS detected_at, 'custom-secret-scan' AS rule_id FROM agentguard_hook_events WHERE match(event_data, {{ .Config.pattern }})",
              "config": [ { "key": "pattern", "label": "Secret pattern", "type": "regex", "default": "AKIA[0-9A-Z]{16}" } ]
            }
          }}
        },
        "responses": {
          "201": { "description": "Custom definition created.", "content": { "application/json": { "schema": { "type": "object" } } } },
          "409": { "description": "A definition with this ID already exists." },
          "400": { "description": "Invalid definition (bad template, defaulting, or schema)." }
        }
      }
    },

    "/rules/catalog/compile": {
      "post": {
        "tags": ["Catalog"],
        "summary": "Compile a definition (preview, no persistence)",
        "description": "Pure validate-and-compile preview: structurally validates the submitted definition, compiles it against the supplied overrides, and returns the rendered SQL, definition hash, effective values, and canonical overrides. It writes NOTHING — no catalog file, no materialized view, no Timeplus query. Use it to power a live authoring preview.\n\nAuthoring errors map to 400: a malformed or inconsistent \u0060sql_template\u0060 is a \u0060DefinitionError\u0060 (\u0060kind\u0060: \u0060bad_template\u0060/\u0060bad_default\u0060); an out-of-range or unknown override is a \u0060ValidationError\u0060 (per-parameter \u0060fields\u0060).",
        "operationId": "compileCatalogRule",
        "requestBody": {
          "required": true,
          "content": { "application/json": { "schema": {
            "type": "object",
            "properties": {
              "definition": { "type": "object", "description": "Candidate Definition to validate and compile." },
              "overrides": { "type": "object", "description": "Optional parameter overrides keyed by config key." }
            }
          },
            "example": {
              "definition": { "id": "demo", "name": "Demo", "version": "1.0.0", "sql_template": "SELECT 1 WHERE score > {{ .Config.threshold }}", "config": [ { "key": "threshold", "label": "Threshold", "type": "integer", "default": 15, "min": 1, "max": 100 } ] },
              "overrides": { "threshold": 40 }
            }
          }}
        },
        "responses": {
          "200": { "description": "Compiled preview.", "content": { "application/json": { "schema": {
            "type": "object",
            "properties": {
              "rendered_sql":    { "type": "string" },
              "definition_hash": { "type": "string" },
              "effective":       { "type": "object" },
              "overrides":       { "type": "object" }
            }
          }, "example": { "rendered_sql": "SELECT 1 WHERE score > 40", "definition_hash": "ab12…", "effective": { "threshold": 40 }, "overrides": { "threshold": 40 } } }}},
          "400": { "description": "Bad template (DefinitionError) or bad override (ValidationError)." }
        }
      }
    },

    "/rules/catalog/{id}": {
      "get": {
        "tags": ["Catalog"],
        "summary": "Get one catalog definition",
        "description": "Returns the full catalog \u0060Definition\u0060 for an ID — including the \u0060config[]\u0060 parameter schema (each entry's \u0060key\u0060, \u0060type\u0060, \u0060default\u0060, bounds, \u0060aliases\u0060) and the \u0060sql_template\u0060 — from the merged catalog (local overrides win over embedded seed). Use this to populate an authoring/edit form.",
        "operationId": "getCatalogRule",
        "parameters": [ { "name": "id", "in": "path", "required": true, "schema": { "type": "string" }, "example": "rp-003" } ],
        "responses": {
          "200": { "description": "Full catalog definition.", "content": { "application/json": { "schema": { "type": "object" } } } },
          "404": { "description": "No definition with this ID in the catalog." }
        }
      },
      "put": {
        "tags": ["Catalog"],
        "summary": "Edit a custom rule definition (atomic publish)",
        "description": "Edits an existing CUSTOM definition (the path \u0060id\u0060 wins over any body id; \u0060origin\u0060 is forced to \u0060custom\u0060). Builtin/imported definitions are rejected with 409 — clone them first via \u0060POST /rules/catalog/{id}/clone\u0060.\n\nWhen an instance is installed, the edit is an atomic publish-and-redeploy: the definition is republished, recompiled against the instance's stored overrides, and its materialized view replaced — rolling BOTH the definition and the MV back on any failure. When no instance is installed, the publish is the whole operation. A failure whose compensating rollback also fails returns 500 with a manual-recourse message (\u0060/api/rules/sync\u0060).",
        "operationId": "updateCatalogRule",
        "parameters": [ { "name": "id", "in": "path", "required": true, "schema": { "type": "string" }, "example": "custom-secret-scan" } ],
        "requestBody": { "required": true, "content": { "application/json": { "schema": { "type": "object" } } } },
        "responses": {
          "200": { "description": "Definition published (and instance redeployed when installed).", "content": { "application/json": { "schema": {
            "type": "object",
            "properties": { "definition": { "type": "object" }, "instance": { "$ref": "#/components/schemas/RuleInstance" } }
          } } } },
          "404": { "description": "No definition with this ID in the catalog." },
          "409": { "description": "Definition is builtin/imported (clone first) or conflicts with the installed instance." },
          "400": { "description": "Invalid edited definition." },
          "500": { "description": "Redeploy failed and rollback also failed; run /api/rules/sync." }
        }
      }
    },

    "/rules/catalog/{id}/clone": {
      "post": {
        "tags": ["Catalog"],
        "summary": "Clone a catalog definition to a new custom rule",
        "description": "Copies the source catalog definition (\u0060id\u0060 path param) to a new custom definition with the supplied new ID and name, rescoping its UDF function names so they do not collide with the source rule's UDFs in the Timeplus function registry. The clone is saved as \u0060origin: custom\u0060 and is NOT installed.",
        "operationId": "cloneCatalogRule",
        "parameters": [ { "name": "id", "in": "path", "required": true, "description": "Source definition ID to clone.", "schema": { "type": "string" }, "example": "rp-003" } ],
        "requestBody": {
          "required": true,
          "content": { "application/json": { "schema": {
            "type": "object", "required": ["id", "name"],
            "properties": { "id": { "type": "string", "description": "New custom rule ID." }, "name": { "type": "string", "description": "New custom rule name." } }
          }, "example": { "id": "rp-003-tuned", "name": "Score Rule (tuned)" } }}
        },
        "responses": {
          "201": { "description": "Custom clone created.", "content": { "application/json": { "schema": { "type": "object" } } } },
          "404": { "description": "Source definition not found." },
          "409": { "description": "Target ID already exists." },
          "400": { "description": "Missing/invalid id or name." }
        }
      }
    },

    "/rules/catalog/import": {
      "post": {
        "tags": ["Catalog"],
        "summary": "Import catalog rules from a zip archive",
        "description": "Uploads a zip file containing YAML rule definition files and merges them into the local catalog directory.\n\nMerge strategy (per rule ID, keyed on the **existing** entry's origin):\n- **New ID** — always added.\n- **Builtin** — never overwritten, not even with \u0060?force=true\u0060 (reported as \u0060skipped_builtin\u0060). A local shadow would replace builtin detection SQL under the builtin's identity; clone it instead.\n- **Custom** — overwritten only with \u0060?force=true\u0060 (otherwise \u0060skipped_custom\u0060).\n- **Imported** — overwritten when the imported version is newer, or with \u0060?force=true\u0060; otherwise skipped.\n\nNo rules are auto-installed after import — call \u0060/rules/{id}/install\u0060 explicitly.",
        "operationId": "importCatalog",
        "parameters": [
          { "name": "force", "in": "query", "description": "If true, overwrite existing imported/custom entries even when the imported version is not newer. Builtin entries are never overwritten.", "schema": { "type": "boolean", "default": false } }
        ],
        "requestBody": {
          "required": true,
          "content": {
            "multipart/form-data": {
              "schema": {
                "type": "object",
                "required": ["file"],
                "properties": {
                  "file": { "type": "string", "format": "binary", "description": "Zip archive containing one or more YAML rule files (one rule per file, named {id}.yaml)." }
                }
              }
            }
          }
        },
        "responses": {
          "200": { "description": "Import summary", "content": { "application/json": { "schema": { "$ref": "#/components/schemas/ImportResult" },
            "example": { "added": ["custom-my-rule"], "updated": ["rp-imported-pack"], "skipped": ["rp-002"], "skipped_builtin": ["rp-001"], "skipped_custom": ["custom-mine"], "errors": [] }
          }}},
          "400": { "description": "Missing file field, invalid zip format, or YAML parse error." }
        }
      }
    },

    "/rules/catalog/export": {
      "get": {
        "tags": ["Catalog"],
        "summary": "Export all catalog rules as a zip archive",
        "description": "Streams a zip archive containing every rule in the merged catalog as a YAML file (named \u0060{id}.yaml\u0060 per rule).\n\nThe export includes both the embedded initial rules and any local overrides or custom rules saved in the catalog directory. Use this to back up the catalog or share it with another AgentGuard instance.",
        "operationId": "exportCatalog",
        "responses": {
          "200": {
            "description": "Zip archive download.",
            "headers": {
              "Content-Disposition": { "schema": { "type": "string" }, "description": "attachment; filename=\"agentguard-catalog.zip\"" }
            },
            "content": { "application/zip": { "schema": { "type": "string", "format": "binary" } } }
          },
          "500": { "description": "Failed to build zip archive." }
        }
      }
    },

    "/rules/mv-status": {
      "get": {
        "tags": ["Rules"],
        "summary": "Materialized view health status",
        "description": "Returns each rule's materialized-view health (\u0060mv_rule_{id}\u0060): whether the MV exists, its Proton failure state (from system.v_failed_mat_views), and a healthy flag (a disabled rule with no MV is healthy). Useful for diagnosing rules marked enabled whose MV is missing or failing.",
        "operationId": "getMVStatus",
        "responses": {
          "200": { "description": "MV health per rule", "content": { "application/json": { "schema": {
            "type": "object",
            "properties": { "mvs": { "type": "array", "items": {
              "type": "object",
              "properties": {
                "rule_id":      { "type": "string", "description": "Rule identifier." },
                "mv_name":      { "type": "string", "description": "Proton MV name, e.g. \u0060mv_rule_rp001\u0060." },
                "rule_enabled": { "type": "boolean", "description": "Whether the rule is currently enabled." },
                "mv_exists":    { "type": "boolean", "description": "Whether the MV currently exists in the active database." },
                "failed_state": { "type": "string", "description": "Non-Executing state from system.v_failed_mat_views, when failing (omitted otherwise)." },
                "healthy":      { "type": "boolean", "description": "True when the rule is disabled (no MV expected) or its MV exists and is not failing." }
              }
            } } }
          }, "example": { "mvs": [
            { "rule_id": "rp-001", "mv_name": "mv_rule_rp001", "rule_enabled": true, "mv_exists": true, "healthy": true },
            { "rule_id": "rp-003", "mv_name": "mv_rule_rp003", "rule_enabled": true, "mv_exists": false, "healthy": false }
          ] }}}}
        }
      }
    },

    "/rules/sync": {
      "post": {
        "tags": ["Rules"],
        "summary": "Reconcile MV state",
        "description": "Reconciles each MV with the current instance snapshots without a restart: an enabled rule whose MV is missing is recreated from its snapshot SQL + UDFs; a disabled rule whose MV still exists has it dropped. Does not depend on the catalog definition, only the last-known-good snapshot artifacts.",
        "operationId": "syncMVs",
        "responses": {
          "200": { "description": "Per-rule reconciliation results.", "content": { "application/json": { "example": { "results": [ { "rule_id": "rp-001", "action": "ok" }, { "rule_id": "rp-003", "action": "created" } ] } } } }
        }
      }
    },

    "/rules/{id}": {
      "get": {
        "tags": ["Rules"],
        "summary": "Get rule by ID",
        "description": "Returns the full read model for one installed rule: the current instance snapshot, the definition's parameter schema, defaults, the instance's overrides, effective values, the read-only deployed rendered SQL, MV health, the pending-upgrade plan (when the catalog has moved past the installed definition), and recent detections. Current state is reconstructed via \u0060arg_max(field, revision) GROUP BY rule_id\u0060.",
        "operationId": "getRule",
        "parameters": [
          { "name": "id", "in": "path", "required": true, "description": "Rule ID, e.g. \u0060rp-001\u0060 or \u0060custom-a1b2c3\u0060.", "schema": { "type": "string" }, "example": "rp-001" }
        ],
        "responses": {
          "200": { "description": "Rule detail with recent detections", "content": { "application/json": { "schema": { "$ref": "#/components/schemas/RuleDetailResponse" } } } },
          "404": { "description": "Rule not found or deleted." }
        }
      },
      "delete": {
        "tags": ["Rules"],
        "summary": "Uninstall a rule",
        "description": "Soft-deletes a rule by appending a \u0060deleted=true\u0060 record to \u0060agentguard_rules\u0060, then drops the corresponding materialized view.\n\nThe rule remains in the stream history and will appear in the catalog as a reinstallable entry (for custom rules) or as \u0060installed: false\u0060 (for builtin rules).",
        "operationId": "uninstallRule",
        "parameters": [
          { "name": "id", "in": "path", "required": true, "description": "Rule ID.", "schema": { "type": "string" }, "example": "rp-001" }
        ],
        "responses": {
          "200": { "description": "Rule uninstalled.", "content": { "application/json": { "example": { "deleted": true, "rule_id": "rp-001" } } } },
          "404": { "description": "Rule not found." }
        }
      }
    },

    "/rules/{id}/install": {
      "post": {
        "tags": ["Rules"],
        "summary": "Install a rule from the catalog",
        "description": "Installs a catalog rule using its parameter defaults plus any supplied typed overrides, rendering and preflighting the SQL before deploying the MV. The rule is installed **enabled** with a \u0060log_only\u0060 policy. Reinstalling a previously uninstalled rule continues its revision sequence.",
        "operationId": "installRule",
        "parameters": [
          { "name": "id", "in": "path", "required": true, "description": "Catalog rule ID, e.g. \u0060rp-002\u0060.", "schema": { "type": "string" }, "example": "rp-002" }
        ],
        "requestBody": {
          "required": false,
          "content": { "application/json": { "schema": { "type": "object", "properties": { "overrides": { "type": "object", "description": "Typed parameter overrides keyed by parameter key; omitted keys use defaults." }, "reason": { "type": "string", "description": "Audit reason." } } }, "example": { "overrides": { "command_chain_threshold": 25 } } } }
        },
        "responses": {
          "200": { "description": "Installed instance snapshot.", "content": { "application/json": { "schema": { "$ref": "#/components/schemas/RuleInstance" } } } },
          "400": { "description": "Invalid parameter overrides (per-parameter field errors)." },
          "404": { "description": "Rule not in catalog." },
          "409": { "description": "Rule is already installed." }
        }
      }
    },

    "/rules/{id}/configuration": {
      "put": {
        "tags": ["Rules"],
        "summary": "Replace a rule's parameter configuration",
        "description": "Replaces the full parameter override map for an installed rule; any parameter omitted from the body resets to its definition default. Values are typed-validated and the candidate SQL is preflighted before the MV is replaced (enabled rules) or the snapshot is appended (disabled rules). The definition must still match the installed one — if the catalog has moved on, upgrade first (409).",
        "operationId": "configureRule",
        "parameters": [
          { "name": "id", "in": "path", "required": true, "description": "Rule ID.", "schema": { "type": "string" }, "example": "rp-003" }
        ],
        "requestBody": {
          "required": true,
          "content": { "application/json": { "schema": {
            "type": "object",
            "properties": {
              "overrides": { "type": "object", "description": "Full typed parameter override map keyed by parameter key; omitted keys reset to defaults." },
              "reason": { "type": "string", "description": "Audit reason recorded on the snapshot." }
            }
          }, "example": { "overrides": { "dangerous_tools": ["bash", "shell", "exec"], "command_chain_threshold": 25 } } }}
        },
        "responses": {
          "200": { "description": "Reconfigured instance snapshot.", "content": { "application/json": { "schema": { "$ref": "#/components/schemas/RuleInstance" } } } },
          "400": { "description": "Invalid parameter values (per-parameter field errors)." },
          "404": { "description": "Rule not installed." },
          "409": { "description": "Definition has changed since install; upgrade before configuring." }
        }
      }
    },

    "/rules/{id}/toggle": {
      "post": {
        "tags": ["Rules"],
        "summary": "Toggle rule enabled/disabled",
        "description": "Flips the \u0060enabled\u0060 flag through the lifecycle service.\n\n- Enabling deploys the MV from the instance's last-known-good rendered SQL + UDFs.\n- Disabling **drops** the MV (so re-enabling always redeploys the current snapshot) and abandons any pending holds for the rule.\n\nReturns the new instance snapshot.",
        "operationId": "toggleRule",
        "parameters": [
          { "name": "id", "in": "path", "required": true, "description": "Rule ID.", "schema": { "type": "string" }, "example": "rp-001" }
        ],
        "responses": {
          "200": { "description": "Updated instance snapshot.", "content": { "application/json": { "schema": { "$ref": "#/components/schemas/RuleInstance" } } } },
          "404": { "description": "Rule not found." }
        }
      }
    },

    "/rules/{id}/upgrade": {
      "post": {
        "tags": ["Rules"],
        "summary": "Upgrade rule SQL and rebuild its MV",
        "description": "Upgrades an installed rule to the current catalog definition, preserving compatible overrides (including alias migrations) and re-rendering the SQL. There is NO request body — raw SQL editing of installed rules is not supported. A no-op when already at the catalog definition; blocked with 409 when an override is incompatible with the new schema. The rule's \u0060enabled\u0060 state is preserved.",
        "operationId": "upgradeRule",
        "parameters": [
          { "name": "id", "in": "path", "required": true, "description": "Rule ID.", "schema": { "type": "string" }, "example": "rp-003" }
        ],
        "responses": {
          "200": { "description": "Upgraded instance snapshot (or the unchanged current instance when already up to date).", "content": { "application/json": { "schema": { "$ref": "#/components/schemas/RuleInstance" } } } },
          "404": { "description": "Rule not found." },
          "409": { "description": "Overrides incompatible with the new definition; the body carries conflicts and warnings." }
        }
      }
    },

    "/rules/{id}/policy": {
      "post": {
        "tags": ["Rules"],
        "summary": "Set rule block policy",
        "description": "Sets the enforcement mode for a rule without toggling its enabled state. The block_policy controls what happens when the rule matches a tool call via the POST /holds synchronous path:\n\n- \u0060log_only\u0060 — detect and log only; never create a hold or block the tool call.\n- \u0060hold\u0060 — pause the tool call for human review; apply fail-policy on timeout.\n- \u0060auto_block\u0060 — immediately block the tool call without human review.\n\nSwitching to \u0060log_only\u0060 automatically abandons any pending holds for this rule so in-flight requests are not left waiting.",
        "operationId": "setRuleBlockPolicy",
        "parameters": [
          { "name": "id", "in": "path", "required": true, "description": "Rule ID.", "schema": { "type": "string" }, "example": "rp-001" }
        ],
        "requestBody": {
          "required": true,
          "content": { "application/json": { "schema": {
            "type": "object",
            "required": ["block_policy"],
            "properties": { "block_policy": { "type": "string", "enum": ["log_only", "hold", "auto_block"], "description": "Enforcement mode for this rule." } }
          }, "example": { "block_policy": "hold" }
          }}
        },
        "responses": {
          "200": { "description": "Updated instance snapshot.", "content": { "application/json": { "schema": { "$ref": "#/components/schemas/RuleInstance" } } } },
          "400": { "description": "block_policy must be log_only|hold|auto_block." },
          "404": { "description": "Rule not found." }
        }
      }
    },

    "/holds": {
      "post": {
        "tags": ["Holds"],
        "summary": "Submit a tool call for evaluation (plugin endpoint)",
        "description": "Called synchronously by agent plugins (Claude Code, OpenClaw) on every PreToolUse event. This endpoint is **unauthenticated** — it is called server-to-server from the agent process, not from the browser.\n\nThe request blocks until one of:\n- The tool call is auto-allowed (no matching active rules with hold/auto_block policy).\n- The tool call is auto-blocked (matching rule with \u0060auto_block\u0060 policy).\n- A human approves or denies the hold via POST /holds/{hold_id}/decide.\n- The session ends (SessionEnd hook fires POST /holds/_abandon-session/{session_id}).\n- The configured \u0060holds.timeout_seconds\u0060 elapses (fail-policy applied).\n\nThe \u0060TIMEPLUS_USER\u0060 and \u0060TIMEPLUS_PASSWORD\u0060 env vars must be set on the AgentGuard server for this endpoint to function.",
        "operationId": "createHold",
        "security": [],
        "requestBody": {
          "required": true,
          "content": { "application/json": { "schema": { "$ref": "#/components/schemas/HoldRequest" },
            "example": { "deployment_id": "eng-team", "agent_id": "claude-code", "session_id": "a1b2c3d4-0000-0000-0000-000000000001", "run_id": "chatcmpl_4ff08efb", "tool_name": "Bash", "tool_input": { "command": "rm -rf /tmp/secrets" }, "event_time": "2026-04-09T10:00:00Z", "hook_payload": {}, "agent_type": "claudecode" }
          }}
        },
        "responses": {
          "200": { "description": "Decision for the tool call.", "content": { "application/json": { "schema": { "$ref": "#/components/schemas/HoldResponse" },
            "example": { "decision": "allow" }
          }}},
          "400": { "description": "Invalid request body." },
          "503": { "description": "Service credentials not configured or Timeplus unavailable." }
        }
      },
      "get": {
        "tags": ["Holds"],
        "summary": "List holds",
        "description": "Returns holds filtered by status. Defaults to pending holds (holds awaiting human decision). Holds are resolved via POST /holds/{hold_id}/decide.",
        "operationId": "listHolds",
        "parameters": [
          { "name": "status", "in": "query", "description": "Status to filter by. Defaults to pending.", "schema": { "type": "string", "enum": ["pending", "approved", "denied", "timeout", "abandoned"], "default": "pending" } },
          { "name": "limit", "in": "query", "description": "Max rows to return (1-500, default 100).", "schema": { "type": "integer", "default": 100, "minimum": 1, "maximum": 500 } }
        ],
        "responses": {
          "200": { "description": "Hold list", "content": { "application/json": { "schema": {
            "type": "object",
            "properties": {
              "holds": { "type": "array", "items": { "$ref": "#/components/schemas/Hold" } },
              "total": { "type": "integer" }
            }
          }, "example": {
            "holds": [{
              "hold_id": "e1f2g3h4-0000-0000-0000-000000000001",
              "deployment_id": "eng-team",
              "agent_id": "claude-code",
              "session_id": "a1b2c3d4-0000-0000-0000-000000000001",
              "run_id": "chatcmpl_4ff08efb",
              "tool_name": "Bash",
              "rule_id": "rp-003",
              "severity": "warning",
              "args_summary": "{\"command\":\"rm -rf /tmp/secrets\"}",
              "message": "Sensitive shell command matches Privilege Guard rule",
              "created_at": "2026-04-09T10:00:05Z",
              "status": "pending"
            }],
            "total": 1
          }}}}
        }
      }
    },

    "/holds/_abandon-session/{session_id}": {
      "post": {
        "tags": ["Holds"],
        "summary": "Abandon all pending holds for a session (plugin endpoint)",
        "description": "Called fire-and-forget by the agent plugin's SessionEnd hook when a session terminates. Marks every pending hold for the session as \u0060abandoned\u0060 and wakes the blocked POST /holds goroutine so the plugin can exit cleanly. This endpoint is **unauthenticated**.",
        "operationId": "abandonSession",
        "security": [],
        "parameters": [
          { "name": "session_id", "in": "path", "required": true, "description": "Session UUID that is ending.", "schema": { "type": "string" }, "example": "a1b2c3d4-0000-0000-0000-000000000001" }
        ],
        "responses": {
          "200": { "description": "Holds abandoned.", "content": { "application/json": { "example": { "abandoned": 2 } } } },
          "503": { "description": "Service credentials not configured or Timeplus unavailable." }
        }
      }
    },

    "/holds/_drain": {
      "post": {
        "tags": ["Holds"],
        "summary": "Drain all pending holds (operator endpoint)",
        "description": "Marks every pending hold as \u0060abandoned\u0060 and applies fail-policy to each blocked plugin request. Use during maintenance or incident response to unblock all agents immediately.",
        "operationId": "drainHolds",
        "responses": {
          "200": { "description": "All pending holds drained.", "content": { "application/json": { "example": { "drained": 5 } } } },
          "503": { "description": "Timeplus unavailable." }
        }
      }
    },

    "/holds/{hold_id}": {
      "get": {
        "tags": ["Holds"],
        "summary": "Get hold detail",
        "description": "Returns the current state of a single hold plus a full status-change timeline (all rows for this hold_id ordered by updated_at).",
        "operationId": "getHold",
        "parameters": [
          { "name": "hold_id", "in": "path", "required": true, "description": "Hold UUID.", "schema": { "type": "string" }, "example": "e1f2g3h4-0000-0000-0000-000000000001" }
        ],
        "responses": {
          "200": { "description": "Hold detail with timeline", "content": { "application/json": { "example": {
            "hold": {
              "hold_id": "e1f2g3h4-0000-0000-0000-000000000001",
              "tool_name": "Bash",
              "status": "pending",
              "created_at": "2026-04-09T10:00:05Z"
            },
            "timeline": [
              { "status": "pending", "decided_by": "", "decided_at": null, "decided_reason": "", "updated_at": "2026-04-09T10:00:05Z" }
            ]
          }}}},
          "404": { "description": "Hold not found." }
        }
      }
    },

    "/holds/{hold_id}/decide": {
      "post": {
        "tags": ["Holds"],
        "summary": "Approve or deny a hold",
        "description": "Submits a human decision for a pending hold. The decision is persisted to \u0060agentguard_holds\u0060 and the blocked POST /holds goroutine is woken so the plugin receives the answer immediately.\n\nIf the hold has already been decided (approved, denied, timeout, abandoned), this call is idempotent — it returns the existing decision without writing a new row.",
        "operationId": "decideHold",
        "parameters": [
          { "name": "hold_id", "in": "path", "required": true, "description": "Hold UUID.", "schema": { "type": "string" } }
        ],
        "requestBody": {
          "required": true,
          "content": { "application/json": { "schema": { "$ref": "#/components/schemas/HoldDecideRequest" },
            "example": { "decision": "approved", "decided_by": "alice@example.com", "decided_reason": "Verified command is safe" }
          }}
        },
        "responses": {
          "200": { "description": "Decision recorded.", "content": { "application/json": { "example": { "ok": true } } } },
          "400": { "description": "decision must be approved|denied." },
          "404": { "description": "Hold not found." }
        }
      }
    },

    "/activity": {
      "get": {
        "tags": ["Activity"],
        "summary": "List activity events",
        "description": "Returns a combined chronological feed of hold events and (optionally) auto-allowed tool-call hook events, ordered newest-first.\n\nBy default only hold events (those that triggered a rule hold/block decision) are returned. Pass \u0060include_allowed=true\u0060 to also include hook events that were auto-allowed without triggering any rule — useful for a full tool-call audit trail per session.",
        "operationId": "listActivity",
        "parameters": [
          { "name": "session_id",      "in": "query", "description": "Filter to a single session.", "schema": { "type": "string" } },
          { "name": "status",          "in": "query", "description": "Filter hold events by status.", "schema": { "type": "string", "enum": ["pending", "approved", "denied", "timeout", "abandoned"] } },
          { "name": "include_allowed", "in": "query", "description": "Also include auto-allowed hook events (default false). Raises the max limit to 500.", "schema": { "type": "boolean", "default": false } },
          { "name": "limit",           "in": "query", "description": "Max events to return (1-100, or 1-500 when include_allowed=true). Default 100.", "schema": { "type": "integer", "default": 100 } }
        ],
        "responses": {
          "200": { "description": "Activity event feed", "content": { "application/json": { "schema": { "$ref": "#/components/schemas/ActivityResponse" },
            "example": {
              "events": [{
                "id": "e1f2g3h4-0000-0000-0000-000000000001",
                "event_time": "2026-04-09T10:00:05Z",
                "tool_name": "Bash",
                "status": "approved",
                "rule_id": "rp-003",
                "rule_name": "Privilege Guard",
                "severity": "warning",
                "agent_id": "claude-code",
                "session_id": "a1b2c3d4-0000-0000-0000-000000000001",
                "run_id": "chatcmpl_4ff08efb",
                "args_summary": "{\"command\":\"ls /tmp\"}",
                "message": "Sensitive tool usage",
                "decided_by": "alice@example.com",
                "decided_reason": "Safe command",
                "is_hold_event": true
              }],
              "total": 1
            }
          }}}
        }
      }
    },

    "/onboarding/install-script": {
      "get": {
        "tags": ["Onboarding"],
        "summary": "Download the AgentGuard install script",
        "description": "Serves the shell script that automates plugin installation and configuration for an agent. The response is a downloadable \u0060agentguard-install.sh\u0060 file.",
        "operationId": "getInstallScript",
        "responses": {
          "200": {
            "description": "Shell script download.",
            "headers": { "Content-Disposition": { "schema": { "type": "string" }, "description": "attachment; filename=\"agentguard-install.sh\"" } },
            "content": { "text/x-shellscript": { "schema": { "type": "string", "format": "binary" } } }
          }
        }
      }
    },

    "/notifications/stream": {
      "get": {
        "tags": ["Alerts"],
        "summary": "Live alert SSE stream",
        "description": "Server-Sent Events stream that pushes real-time security alert notifications to connected clients.\n\nEach event is a JSON-encoded \u0060Alert\u0060 object. The stream keeps the connection alive with periodic comments (\u0060: keepalive\u0060).\n\nConnect with \u0060EventSource\u0060 in the browser or \u0060curl -N\u0060:\n\u0060\u0060\u0060\ncurl -N http://localhost:8080/api/notifications/stream\n\u0060\u0060\u0060",
        "operationId": "notificationsStream",
        "responses": {
          "200": {
            "description": "SSE stream — one JSON alert per \u0060data:\u0060 line.",
            "content": { "text/event-stream": { "schema": { "type": "string" },
              "example": "data: {\"rule_id\":\"rp-001\",\"severity\":\"critical\",\"agent_id\":\"claude-code\",\"message\":\"Prompt injection detected\"}\n\n"
            }}
          }
        }
      }
    },

    "/notifications/unconfirmed": {
      "get": {
        "tags": ["Alerts"],
        "summary": "List unconfirmed alert notifications",
        "description": "Returns unconfirmed threat notifications, newest-first, for SSE-reconnect backfill (a browser that missed live events while disconnected). Backed by the \u0060agentguard_notifications\u0060 mutable stream; \u0060id\u0060 is the stable identity key \u0060deployment_id|agent_id|session_id|rule_id\u0060 used by \u0060POST /notifications/confirm\u0060.\n\nBest-effort: if the underlying query fails (e.g. stream not yet provisioned) this returns \u0060200\u0060 with an empty list rather than an error, so the toast UI stays quiet.",
        "operationId": "listUnconfirmedNotifications",
        "responses": {
          "200": { "description": "Unconfirmed notifications, max 20.", "content": { "application/json": { "schema": { "$ref": "#/components/schemas/UnconfirmedNotificationsResponse" },
            "example": {
              "notifications": [{
                "id": "eng-team|claude-code|a1b2c3d4-0000-0000-0000-000000000001|rp-001",
                "rule_id": "rp-001",
                "rule_name": "Prompt Injection Guard",
                "severity": "critical",
                "agent_id": "claude-code",
                "deployment_id": "eng-team",
                "session_id": "a1b2c3d4-0000-0000-0000-000000000001",
                "message": "Prompt injection detected",
                "detected_at": "2026-04-09 10:00:05.000"
              }]
            }
          }}}
        }
      }
    },

    "/notifications/confirm": {
      "post": {
        "tags": ["Alerts"],
        "summary": "Confirm an alert notification",
        "description": "Marks a threat notification as seen — the user dismissed or clicked the toast. Upserts \u0060confirmed=true\u0060 into the \u0060agentguard_notifications\u0060 mutable stream for the given identity; idempotent.",
        "operationId": "confirmNotification",
        "requestBody": {
          "required": true,
          "content": { "application/json": { "schema": { "$ref": "#/components/schemas/NotificationConfirmRequest" },
            "example": { "agent_id": "claude-code", "deployment_id": "eng-team", "session_id": "a1b2c3d4-0000-0000-0000-000000000001", "rule_id": "rp-001" }
          }}
        },
        "responses": {
          "200": { "description": "Confirmed.", "content": { "application/json": { "example": { "ok": true } } } },
          "400": { "description": "Missing agent_id or rule_id." },
          "500": { "description": "Upsert into agentguard_notifications failed." }
        }
      }
    },

    "/proton/query": {
      "post": {
        "tags": ["Proton"],
        "summary": "Streaming SQL proxy (NDJSON)",
        "description": "Proxies a SQL query directly to Proton and streams result rows back as **newline-delimited JSON** (NDJSON). This endpoint does not buffer results — it streams rows as Proton produces them, making it suitable for long-running or live streaming queries.\n\nThe frontend uses this via \u0060@timeplus/proton-javascript-driver\u0060 to power live streaming panels (live threat feed, live agent activity, agent-detail traces).\n\nSend a \u0060POST\u0060 with \u0060{\"sql\": \"...\"}\u0060 and consume the response body as an NDJSON stream. Use an \u0060AbortController\u0060 to cancel.",
        "operationId": "protonQuery",
        "requestBody": {
          "required": true,
          "content": { "application/json": { "schema": {
            "type": "object",
            "required": ["sql"],
            "properties": { "sql": { "type": "string", "description": "Any valid Proton SQL. Omit \u0060table()\u0060 wrapper for live streaming; use \u0060table()\u0060 for historical batch queries." } }
          }, "example": { "sql": "SELECT hook_name, event_time, agent_id FROM agentguard_hook_events WHERE deployment_id = 'eng-team' LIMIT 100" }
          }}
        },
        "responses": {
          "200": {
            "description": "NDJSON stream — one JSON object per line.",
            "content": { "application/x-ndjson": { "schema": { "type": "string" },
              "example": "{\"hook_name\":\"before_tool_call\",\"event_time\":\"2026-04-09 10:00:01.000\",\"agent_id\":\"claude-code\"}\n{\"hook_name\":\"after_tool_call\",\"event_time\":\"2026-04-09 10:00:01.450\",\"agent_id\":\"claude-code\"}\n"
            }}
          },
          "400": { "description": "Empty or missing SQL." },
          "503": { "description": "Proton unavailable." }
        }
      }
    },

    "/onboarding/generate": {
      "post": {
        "tags": ["Onboarding"],
        "summary": "Generate an agent configuration snippet",
        "description": "Generates a configuration snippet for connecting an AI agent to AgentGuard. Used by the onboarding wizard.",
        "operationId": "generateConfig",
        "requestBody": {
          "required": true,
          "content": { "application/json": { "schema": {
            "type": "object",
            "properties": {
              "agentType":      { "type": "string", "enum": ["openclaw", "claude-code"], "description": "Target agent framework." },
              "endpoint":       { "type": "string", "description": "Timeplus ingest endpoint URL." },
              "captureContent": { "type": "boolean", "description": "Whether to capture full prompt/response content." }
            }
          }, "example": { "agentType": "openclaw", "endpoint": "http://localhost:3218", "captureContent": false }
          }}
        },
        "responses": {
          "200": { "description": "Generated config object.", "content": { "application/json": { "example": { "config": { "timeplusUrl": "http://localhost:3218", "stream": "agentguard_hook_events" } } } } }
        }
      }
    },

    "/auth/status": {
      "get": {
        "tags": ["Auth"],
        "summary": "Get authentication and setup status",
        "description": "Returns whether the server has been provisioned (setup wizard completed) and whether the current request carries a valid session.\n\nThis is the first call the frontend makes on load. If \u0060setup_complete\u0060 is false, show the setup wizard. If \u0060logged_in\u0060 is false, show the login page. Otherwise proceed to the dashboard.",
        "operationId": "authStatus",
        "security": [],
        "responses": {
          "200": {
            "description": "Current auth state",
            "content": { "application/json": { "schema": { "$ref": "#/components/schemas/AuthStatus" },
              "example": { "setup_complete": true, "logged_in": true, "username": "default" }
            }}
          }
        }
      }
    },

    "/auth/login": {
      "post": {
        "tags": ["Auth"],
        "summary": "Log in with Timeplus credentials",
        "description": "Validates the supplied credentials against Timeplus by running a lightweight test query. On success, creates a server-side session (8-hour expiry) and sets an \u0060agentguard_session\u0060 httpOnly cookie.\n\nCredentials are never written to disk — they live only in the in-memory session store for the duration of the session.",
        "operationId": "authLogin",
        "security": [],
        "requestBody": {
          "required": true,
          "content": { "application/json": { "schema": { "$ref": "#/components/schemas/LoginRequest" },
            "example": { "username": "default", "password": "" }
          }}
        },
        "responses": {
          "200": {
            "description": "Login successful. Sets \u0060agentguard_session\u0060 httpOnly cookie.",
            "headers": {
              "Set-Cookie": { "schema": { "type": "string" }, "description": "agentguard_session=<token>; HttpOnly; SameSite=Strict; Path=/" }
            },
            "content": { "application/json": { "schema": { "$ref": "#/components/schemas/LoginResponse" },
              "example": { "username": "default" }
            }}
          },
          "400": { "description": "Missing username or password field." },
          "401": { "description": "Credentials rejected by Timeplus." },
          "503": { "description": "Timeplus unreachable during credential validation." }
        }
      }
    },

    "/auth/logout": {
      "post": {
        "tags": ["Auth"],
        "summary": "Log out",
        "description": "Destroys the current server-side session and clears the \u0060agentguard_session\u0060 cookie by setting \u0060Max-Age=-1\u0060. Safe to call even if no session exists.",
        "operationId": "authLogout",
        "security": [],
        "responses": {
          "200": {
            "description": "Session destroyed.",
            "content": { "application/json": { "example": { "ok": true } } }
          }
        }
      }
    },

    "/setup/test-connection": {
      "post": {
        "tags": ["Setup"],
        "summary": "Test a Timeplus connection",
        "description": "Attempts to connect to Timeplus at the supplied host/port with the given credentials by executing a lightweight \u0060SELECT 1\u0060 query. Used by the setup wizard to validate connection parameters before provisioning.\n\nThis endpoint is unauthenticated — it can be called before setup is complete.",
        "operationId": "testConnection",
        "security": [],
        "requestBody": {
          "required": true,
          "content": { "application/json": { "schema": { "$ref": "#/components/schemas/TestConnectionRequest" },
            "example": { "host": "localhost", "port": 3218, "username": "default", "password": "" }
          }}
        },
        "responses": {
          "200": {
            "description": "Connection test result (always HTTP 200; check \u0060ok\u0060 field).",
            "content": { "application/json": { "schema": {
              "type": "object",
              "properties": {
                "ok":    { "type": "boolean", "description": "True when Timeplus accepted the connection and credentials." },
                "error": { "type": "string",  "description": "Human-readable error message when ok=false." }
              }
            }, "example": { "ok": true } }}
          }
        }
      }
    },

    "/setup/provision": {
      "post": {
        "tags": ["Setup"],
        "summary": "Provision all AgentGuard resources (SSE)",
        "description": "Runs the full provisioning sequence: creates Timeplus streams, materialized views, UDFs, alert rules, and seeds pricing data. Progress is streamed as **Server-Sent Events** so the frontend can display a live log.\n\nEach SSE event is a JSON object \u0060{status, msg}\u0060 where \u0060status\u0060 is one of:\n- \u0060running\u0060 — step in progress\n- \u0060ok\u0060 — step completed successfully\n- \u0060error\u0060 — step failed (provisioning continues for subsequent steps)\n- \u0060done\u0060 — all steps finished; call \u0060POST /auth/login\u0060 next to start an authenticated session\n\nThis endpoint is unauthenticated. On success it writes \u0060config.yaml\u0060 with \u0060setup_complete: true\u0060 and the connection details (no credentials).",
        "operationId": "provision",
        "security": [],
        "requestBody": {
          "required": true,
          "content": { "application/json": { "schema": { "$ref": "#/components/schemas/ProvisionRequest" },
            "example": { "host": "localhost", "port": 3218, "native_port": 8463, "username": "default", "password": "" }
          }}
        },
        "responses": {
          "200": {
            "description": "SSE stream of provisioning progress events.",
            "content": { "text/event-stream": { "schema": { "type": "string" },
              "example": "data: {\"status\":\"running\",\"msg\":\"Creating stream agentguard_hook_events\"}\n\ndata: {\"status\":\"ok\",\"msg\":\"Stream agentguard_hook_events ready\"}\n\ndata: {\"status\":\"done\",\"msg\":\"Provisioning complete\"}\n\n"
            }}
          },
          "400": { "description": "Missing required fields in request body." }
        }
      }
    }
  },

  "components": {
    "schemas": {
      "Dashboard": {
        "type": "object",
        "description": "Full dashboard response with KPIs, live threat feed, threat categories, and recent agents.",
        "properties": {
          "kpis": {
            "type": "object",
            "description": "High-level KPI counters.",
            "properties": {
              "total_agents":          { "type": "integer", "description": "Number of distinct (deployment_id, agent_id) pairs." },
              "agents_delta":          { "type": "string",  "description": "Human-readable delta vs prior period, e.g. +2." },
              "active_threats":        { "type": "integer", "description": "Threats currently in open state." },
              "critical_escalations":  { "type": "integer", "description": "Critical-severity threats in the last 24h." },
              "total_cost_24h":        { "type": "string",  "description": "Formatted total cost for the last 24h, e.g. $4.20." },
              "cost_status":           { "type": "string",  "description": "Budget status label, e.g. under_budget." },
              "active_rules":          { "type": "integer", "description": "Number of rules currently enabled." }
            }
          },
          "live_threat_feed":   { "type": "array", "items": { "$ref": "#/components/schemas/Threat" }, "description": "Most recent open threats." },
          "threat_categories":  { "type": "array", "items": { "type": "object", "properties": { "name": { "type": "string" }, "percentage": { "type": "integer" } } }, "description": "Threat count breakdown by category." },
          "recent_agents":      { "type": "array", "items": { "$ref": "#/components/schemas/Agent" }, "description": "Most recently active agents." }
        }
      },
      "Agent": {
        "type": "object",
        "description": "Aggregated stats for one agent (deployment_id + agent_id pair).",
        "properties": {
          "agent_id":        { "type": "string",  "description": "Agent identifier as reported by the plugin." },
          "deployment_id":   { "type": "string",  "description": "Deployment/team identifier." },
          "deployment_name": { "type": "string",  "description": "Human-readable deployment name." },
          "agent_type":      { "type": "string",  "description": "Plugin type: claudecode or openclaw.", "enum": ["claudecode", "openclaw", ""] },
          "status":          { "type": "string",  "description": "Derived activity status.", "enum": ["active", "idle"] },
          "event_count":     { "type": "integer", "description": "Total hook events from this agent." },
          "session_count":   { "type": "integer", "description": "Number of distinct sessions." },
          "run_count":       { "type": "integer", "description": "Number of distinct LLM runs." },
          "tool_call_count": { "type": "integer", "description": "Total before_tool_call events." },
          "total_tokens":    { "type": "integer", "description": "Sum of input + output + cache-read tokens." },
          "llm_call_count":  { "type": "integer", "description": "Number of llm_output hook events (one per LLM turn)." },
          "last_seen":       { "type": "string", "format": "date-time", "description": "Timestamp of the most recent hook event." }
        }
      },
      "AgentDetail": {
        "description": "GET /agents/{id} returns the same Agent schema with all fields populated including run_count, tool_call_count, and status.",
        "$ref": "#/components/schemas/Agent"
      },
      "Session": {
        "type": "object",
        "description": "A single user conversation session within an agent.",
        "properties": {
          "session_id":   { "type": "string",  "description": "UUID identifying the session." },
          "session_key":  { "type": "string",  "description": "Raw session key, e.g. announce:v1:agent:...:openai-user:abc." },
          "session_type": { "type": "string",  "description": "Derived session type.", "enum": ["Web UI", "HTTP API", "Cron", "Sub-agent", "Unknown"] },
          "status":       { "type": "string",  "description": "active when no session_end event exists; ended otherwise.", "enum": ["active", "ended"] },
          "model":        { "type": "string",  "description": "LLM model used in this session (from the most recent llm_output event)." },
          "provider":     { "type": "string",  "description": "LLM provider, e.g. anthropic." },
          "run_count":    { "type": "integer", "description": "Number of distinct LLM turns (runs) in this session." },
          "event_count":  { "type": "integer", "description": "Total hook events in this session." },
          "first_seen":   { "type": "string",  "format": "date-time" },
          "last_seen":    { "type": "string",  "format": "date-time" }
        }
      },
      "SessionDetail": {
        "type": "object",
        "description": "Full session detail including ordered run list.",
        "properties": {
          "session_id":       { "type": "string" },
          "session_key":      { "type": "string" },
          "session_type":     { "type": "string" },
          "status":           { "type": "string", "enum": ["active", "ended"], "description": "ended when a session_end hook event exists for this session." },
          "model":            { "type": "string",  "description": "LLM model used in this session." },
          "provider":         { "type": "string",  "description": "LLM provider, e.g. anthropic." },
          "total_tool_calls": { "type": "integer", "description": "Total before_tool_call events across all runs." },
          "runs":             { "type": "array", "items": { "$ref": "#/components/schemas/Run" } }
        }
      },
      "Run": {
        "type": "object",
        "description": "A single LLM turn within a session.",
        "properties": {
          "run_id":      { "type": "string",  "description": "Run identifier. Formats: chatcmpl_{uuid}, announce:v1:...:run:{uuid}." },
          "start_time":  { "type": "string",  "format": "date-time", "description": "Timestamp of first hook event in this run." },
          "end_time":    { "type": "string",  "format": "date-time", "description": "Timestamp of last hook event." },
          "event_count": { "type": "integer", "description": "Total hook events in this run." },
          "model":       { "type": "string",  "description": "LLM model name used, e.g. claude-sonnet-4-6." },
          "tool_count":  { "type": "integer", "description": "Number of before_tool_call events." },
          "has_block":   { "type": "boolean", "description": "True if any hook_decision=block event exists in this run." }
        }
      },
      "TraceResponse": {
        "type": "object",
        "description": "Waterfall trace built from hook events for one run.",
        "properties": {
          "spans": { "type": "array", "items": { "$ref": "#/components/schemas/TraceSpan" } }
        }
      },
      "TraceSpan": {
        "type": "object",
        "description": "A single span in the waterfall. The root span (kind=run) covers the full run window; child spans correspond to individual hook events.",
        "properties": {
          "span_id":     { "type": "string",  "description": "Unique span identifier. Root span uses the run_id; child spans use evt-{index}." },
          "parent_id":   { "type": "string",  "description": "Parent span_id. Empty string for the root span." },
          "kind":        { "type": "string",  "description": "Span category.", "enum": ["run", "llm", "tool", "lifecycle"] },
          "name":        { "type": "string",  "description": "Hook name (e.g. before_tool_call) or 'run' for the root span." },
          "start_ms":    { "type": "integer", "format": "int64", "description": "Span start in Unix milliseconds." },
          "duration_ms": { "type": "integer", "format": "int64", "description": "Duration in milliseconds. Gap to next event, or 0 for the last span." },
          "status":      { "type": "string",  "description": "ok | blocked | error.", "enum": ["ok", "blocked", "error"] },
          "attributes":  { "type": "object",  "additionalProperties": { "type": "string" }, "description": "Key-value metadata: tool_name, model, provider, hook_decision, block_reason, duration_ms, error." }
        }
      },
      "HookEvent": {
        "type": "object",
        "description": "A raw hook event from agentguard_hook_events returned by GET /agents/{id}/events.",
        "properties": {
          "hook_name":     { "type": "string", "description": "Lifecycle hook name, e.g. before_tool_call, llm_output, session_end." },
          "event_time":    { "type": "string", "format": "date-time" },
          "session_id":    { "type": "string" },
          "run_id":        { "type": "string" },
          "tool_name":     { "type": "string", "description": "Tool name for tool-call hooks, empty otherwise." },
          "provider":      { "type": "string", "description": "LLM provider, e.g. anthropic." },
          "model":         { "type": "string", "description": "LLM model name." },
          "hook_decision": { "type": "string", "description": "Plugin decision: observe (pass-through) or block.", "enum": ["observe", "block", ""] },
          "block_reason":  { "type": "string", "description": "Human-readable reason when hook_decision=block." },
          "event_data":    { "type": "string", "description": "JSON payload with hook-specific fields: tokens, durationMs, toolCallId, error, etc." },
          "agent_type":    { "type": "string", "description": "Agent plugin type: claudecode or openclaw.", "enum": ["claudecode", "openclaw", ""] }
        }
      },
      "LLMMetrics": {
        "type": "object",
        "description": "LLM token usage and call statistics returned by GET /metrics/llm.",
        "properties": {
          "period":  { "$ref": "#/components/schemas/MetricsPeriod" },
          "filters": { "$ref": "#/components/schemas/MetricsFilters" },
          "tokens": {
            "type": "object",
            "description": "Cumulative token counts.",
            "properties": {
              "input":       { "type": "integer", "format": "int64" },
              "output":      { "type": "integer", "format": "int64" },
              "total":       { "type": "integer", "format": "int64" },
              "cache_read":  { "type": "integer", "format": "int64" },
              "cache_write": { "type": "integer", "format": "int64" }
            }
          },
          "calls": {
            "type": "object",
            "description": "LLM API call success/failure summary.",
            "properties": {
              "total":        { "type": "integer" },
              "success":      { "type": "integer" },
              "failed":       { "type": "integer" },
              "success_rate": { "type": "number", "nullable": true, "description": "null when total=0." }
            }
          },
          "stop_reasons": { "type": "object", "additionalProperties": { "type": "integer" }, "description": "Count of each stop reason (end_turn, max_tokens, tool_use, etc.)." },
          "latency_ms": { "$ref": "#/components/schemas/LatencyStats" },
          "throughput": {
            "type": "array",
            "description": "Time-bucketed LLM call and token throughput.",
            "items": {
              "type": "object",
              "properties": {
                "bucket":        { "type": "string", "format": "date-time" },
                "llm_calls":     { "type": "integer" },
                "input_tokens":  { "type": "integer", "format": "int64" },
                "output_tokens": { "type": "integer", "format": "int64" },
                "total_tokens":  { "type": "integer", "format": "int64" }
              }
            }
          }
        }
      },
      "MetricsPeriod": {
        "type": "object",
        "description": "The time range of a metrics query (both RFC 3339 strings, omitted when unbounded).",
        "properties": {
          "since": { "type": "string", "format": "date-time" },
          "until": { "type": "string", "format": "date-time" }
        }
      },
      "MetricsFilters": {
        "type": "object",
        "description": "Active dimension filters echoed back in metrics responses.",
        "properties": {
          "agent_id":  { "type": "string" },
          "model":     { "type": "string" },
          "tool_name": { "type": "string" }
        }
      },
      "LatencyStats": {
        "type": "object",
        "description": "Latency percentile statistics in milliseconds.",
        "properties": {
          "p50":     { "type": "number" },
          "p95":     { "type": "number" },
          "p99":     { "type": "number" },
          "avg":     { "type": "number" },
          "min":     { "type": "number" },
          "max":     { "type": "number" },
          "samples": { "type": "integer" }
        }
      },
      "ToolMetrics": {
        "type": "object",
        "description": "Tool-call statistics returned by GET /metrics/tools.",
        "properties": {
          "period":  { "$ref": "#/components/schemas/MetricsPeriod" },
          "filters": { "$ref": "#/components/schemas/MetricsFilters" },
          "summary": {
            "type": "object",
            "description": "Aggregate tool call counts.",
            "properties": {
              "total_calls":   { "type": "integer" },
              "success_calls": { "type": "integer" },
              "failed_calls":  { "type": "integer" },
              "success_rate":  { "type": "number", "nullable": true }
            }
          },
          "by_tool": {
            "type": "array",
            "description": "Per-tool-name breakdown.",
            "items": {
              "type": "object",
              "properties": {
                "tool_name":     { "type": "string" },
                "total_calls":   { "type": "integer" },
                "success_calls": { "type": "integer" },
                "failed_calls":  { "type": "integer" },
                "success_rate":  { "type": "number", "nullable": true },
                "latency_ms":    { "$ref": "#/components/schemas/LatencyStats" }
              }
            }
          }
        }
      },
      "ThreatEvent": {
        "type": "object",
        "description": "A single raw security event that contributed to a deduplicated threat.",
        "properties": {
          "detected_at": { "type": "string", "description": "Detection timestamp (Proton datetime64 string)." },
          "run_id":      { "type": "string" },
          "hook_name":   { "type": "string" },
          "tool_name":   { "type": "string" },
          "message":     { "type": "string" },
          "event_data":  { "type": "string", "description": "Raw JSON payload from the triggering hook event." }
        }
      },
      "Threat": {
        "type": "object",
        "description": "A deduplicated security threat keyed by (agent_id, deployment_id, session_id, rule_id). Includes aggregated event counts and up to 50 individual detection events. Status lifecycle: open -> acknowledged -> cleared (auto-reopens on new events).",
        "properties": {
          "threat_id":        { "type": "string",  "description": "MD5 hex string computed from (agent_id, deployment_id, session_id, rule_id). Used as the :id path parameter." },
          "rule_id":          { "type": "string" },
          "rule_name":        { "type": "string" },
          "severity":         { "type": "string", "enum": ["critical", "warning", "info"] },
          "agent_id":         { "type": "string" },
          "deployment_id":    { "type": "string" },
          "session_id":       { "type": "string" },
          "event_count":      { "type": "integer", "format": "int64", "description": "Total number of security events for this threat key." },
          "first_seen":       { "type": "string",  "description": "Timestamp of the earliest detection (Proton datetime string)." },
          "last_seen":        { "type": "string",  "description": "Timestamp of the most recent detection." },
          "status":           { "type": "string",  "enum": ["open", "acknowledged", "cleared"], "description": "Lifecycle status. Reverts to open if new events arrive after acknowledged or cleared." },
          "status_updated_at":{ "type": "string",  "description": "When status was last changed." },
          "events":           { "type": "array", "items": { "$ref": "#/components/schemas/ThreatEvent" }, "description": "Up to 50 individual detection events, newest first." }
        }
      },
      "ThreatDetailResponse": {
        "type": "object",
        "description": "Threat detail response from GET /threats/{id}. Extends Threat with an optional pending_hold field.",
        "allOf": [
          { "$ref": "#/components/schemas/Threat" },
          {
            "type": "object",
            "properties": {
              "pending_hold": {
                "description": "Present when a hold for this threat's rule+session is currently pending human decision.",
                "oneOf": [
                  { "$ref": "#/components/schemas/PendingHold" },
                  { "type": "null" }
                ]
              }
            }
          }
        ]
      },
      "ThreatStatusRequest": {
        "type": "object",
        "description": "Request body for POST /threats/{id}/acknowledge and POST /threats/{id}/clear. All four fields are required; the server verifies that MD5(agent_id, deployment_id, session_id, rule_id) matches the :id path parameter.",
        "required": ["agent_id", "deployment_id", "session_id", "rule_id"],
        "properties": {
          "agent_id":      { "type": "string" },
          "deployment_id": { "type": "string" },
          "session_id":    { "type": "string" },
          "rule_id":       { "type": "string" }
        }
      },
      "PendingHold": {
        "type": "object",
        "description": "A hold that is currently pending human review for this threat's rule+session.",
        "properties": {
          "hold_id":      { "type": "string" },
          "tool_name":    { "type": "string" },
          "args_summary": { "type": "string", "description": "Truncated JSON of the tool arguments (max 2048 bytes)." },
          "created_at":   { "type": "string", "format": "date-time" }
        }
      },
      "CostResponse": {
        "type": "object",
        "description": "Cost governance summary returned by GET /cost. All month-to-date windows are computed in UTC calendar months.",
        "properties": {
          "month_to_date":             { "type": "number", "description": "Current UTC month-to-date spend in USD." },
          "month_to_date_prev":        { "type": "number", "description": "Spend over the same elapsed window in the previous UTC month, clipped to the start of the current month when the previous month is shorter." },
          "month_to_date_delta_pct":   { "type": "number", "nullable": true, "description": "Percent change vs month_to_date_prev. Omitted (or null) when month_to_date_prev is zero." },
          "forecasted":                { "type": "number", "description": "Projected month-end cost (USD) from naive linear extrapolation of the current daily rate." },
          "forecast_budget_delta_pct": { "type": "number", "nullable": true, "description": "Signed percent the forecast represents of the configured budget. Positive = over, negative = under. Omitted when no budget is configured." },
          "budget_amount":             { "type": "number", "nullable": true, "description": "Sum of currently-active per-deployment monthly budgets. Omitted when no budget is configured." },
          "budget_status":             { "type": "string", "description": "One of over | under | on (within +/- 5%% of budget). Empty string when no budget is configured.", "enum": ["over", "under", "on", ""] },
          "avg_token_cost":            { "type": "number", "description": "Current MTD average cost per million tokens (USD). Previously this was an all-time average; switched to MTD in #123 so the prior-period delta compares like-for-like windows." },
          "avg_token_cost_prev":       { "type": "number", "description": "Prior-period MTD average cost per million tokens (USD), same window as month_to_date_prev." },
          "avg_token_cost_delta_pct":  { "type": "number", "nullable": true, "description": "Percent change vs avg_token_cost_prev. Omitted when prior tokens are zero." },
          "cache_savings":             { "type": "number", "description": "Estimated savings from prompt cache hits (USD)." },
          "wasted_spend":              { "type": "number", "description": "Estimated cost of failed/retried calls (USD)." },
          "daily_spend": {
            "type": "array",
            "description": "Daily cost time-series.",
            "items": {
              "type": "object",
              "properties": {
                "day":    { "type": "string", "description": "Calendar date, YYYY-MM-DD." },
                "amount": { "type": "number", "description": "Cost in USD." }
              }
            }
          },
          "token_breakdown": {
            "type": "array",
            "description": "Cost breakdown by token type (Input, Output, Cache Read).",
            "items": {
              "type": "object",
              "properties": {
                "label":      { "type": "string" },
                "percentage": { "type": "integer" },
                "tokens":     { "type": "integer", "format": "int64" },
                "amount":     { "type": "number" }
              }
            }
          },
          "agent_model_costs": {
            "type": "array",
            "description": "Cost breakdown by agent and model.",
            "items": {
              "type": "object",
              "properties": {
                "model":          { "type": "string" },
                "agent_name":     { "type": "string" },
                "cost":           { "type": "number" },
                "requests":       { "type": "string" },
                "provider":       { "type": "string" },
                "agent_id":       { "type": "string" },
                "input_tokens":   { "type": "integer", "format": "int64" },
                "output_tokens":  { "type": "integer", "format": "int64" },
                "cache_tokens":   { "type": "integer", "format": "int64" }
              }
            }
          }
        }
      },
      "PricingEntry": {
        "type": "object",
        "description": "Per-million-token pricing for one provider/model pair.",
        "required": ["provider", "model", "input_cost_per_million", "output_cost_per_million"],
        "properties": {
          "provider":                   { "type": "string",  "description": "Provider name, e.g. anthropic, openai.", "example": "anthropic" },
          "model":                      { "type": "string",  "description": "Model identifier as it appears in hook events.", "example": "claude-sonnet-4-6" },
          "input_cost_per_million":     { "type": "number",  "description": "Cost in USD per 1 million input tokens.", "example": 3.0 },
          "output_cost_per_million":    { "type": "number",  "description": "Cost in USD per 1 million output tokens.", "example": 15.0 },
          "cache_read_cost_per_million":{ "type": "number",  "description": "Cost in USD per 1 million cache-read tokens.", "example": 0.3 }
        }
      },
      "CostBudget": {
        "type": "object",
        "description": "Current state of one per-deployment monthly budget, reconstructed from the agentguard_cost_budgets append-only stream.",
        "properties": {
          "deployment_id":  { "type": "string", "description": "Deployment identifier this budget applies to.", "example": "production" },
          "monthly_budget": { "type": "number", "description": "Monthly spend cap in the canonical currency.", "example": 30.0 },
          "currency":       { "type": "string", "description": "Currency code. Only USD is supported in v1.", "example": "USD" },
          "updated_by":     { "type": "string", "description": "Timeplus username of the operator who last set the budget." },
          "updated_at":     { "type": "string", "description": "When the budget was last updated.", "format": "date-time" }
        }
      },
      "CostBudgetUpsert": {
        "type": "object",
        "description": "POST body for upserting (or deleting) a per-deployment monthly budget. updated_by is taken from the session and ignored if sent.",
        "required": ["deployment_id", "monthly_budget"],
        "properties": {
          "deployment_id":  { "type": "string",  "description": "Deployment identifier. Allowed alphabet: [a-zA-Z0-9._:/-].", "example": "production" },
          "monthly_budget": { "type": "number",  "description": "Monthly spend cap. Must be >= 0.", "example": 30.0 },
          "currency":       { "type": "string",  "description": "Currency code. Defaults to USD if omitted.", "example": "USD" },
          "deleted":        { "type": "boolean", "description": "Soft-delete flag. Setting true retires the budget; subsequent GETs omit it.", "default": false }
        }
      },
      "RulePack": {
        "type": "object",
        "description": "The list/summary projection of an installed Rule Instance snapshot. Installed rules have no raw-SQL edit surface; rendered_sql is read-only.",
        "properties": {
          "id":                 { "type": "string",  "description": "Rule identifier: rp-{N} for builtins, custom-{id} for user-created.", "example": "rp-001" },
          "name":               { "type": "string",  "example": "Prompt Injection Shield" },
          "version":            { "type": "string",  "description": "definition_version of this snapshot.", "example": "1.0.0" },
          "description":        { "type": "string" },
          "rendered_sql":       { "type": "string",  "description": "Read-only deployed SELECT used by this rule's materialized view (compiled from the definition's sql_template with the instance's parameter overrides)." },
          "enabled":            { "type": "boolean", "description": "Whether the rule's MV is currently deployed." },
          "severity":           { "type": "string",  "enum": ["critical", "warning", "info"] },
          "category":           { "type": "string",  "description": "Rule category: prompt_injection, dlp, privilege, supply_chain, custom." },
          "required_telemetry": { "type": "string",  "description": "Telemetry prerequisite note (e.g. requires OTel logs)." },
          "fp_rate":            { "type": "string",  "description": "Expected false-positive rate.", "enum": ["low", "medium", "high"] },
          "hook_sources":       { "type": "string",  "description": "Comma-separated list of hook names this rule inspects." },
          "block_policy":       { "type": "string",  "description": "Enforcement mode for holds integration.", "enum": ["log_only", "hold", "auto_block"] },
          "detections_24h":     { "type": "integer", "description": "Number of security events generated by this rule in the last 24 hours." },
          "latest_version":     { "type": "string",  "description": "Catalog version when it differs from the installed definition (omitted when up to date)." },
          "revision":           { "type": "integer", "description": "Per-rule monotonic snapshot revision (the current-state ordering key, not updated_at)." },
          "snapshot_id":        { "type": "string",  "description": "Unique identifier of this instance snapshot." },
          "upgradeable":        { "type": "boolean", "description": "True when the catalog definition_hash differs from the installed one — call /rules/{id}/upgrade." }
        }
      },
      "RuleInstance": {
        "type": "object",
        "description": "A complete Rule Instance snapshot — one immutable revision of an installed rule. Current state is the highest-revision snapshot per rule_id.",
        "properties": {
          "rule_id":            { "type": "string" },
          "snapshot_id":        { "type": "string", "description": "Unique ID of this snapshot." },
          "revision":           { "type": "integer", "description": "Per-rule monotonic ordering key." },
          "name":               { "type": "string" },
          "definition_version": { "type": "string" },
          "definition_hash":    { "type": "string", "description": "SHA-256 of the canonical definition content this instance was rendered from." },
          "description":        { "type": "string" },
          "rendered_sql":       { "type": "string", "description": "Deployed, read-only rendered SELECT." },
          "deployed_udfs":      { "type": "array", "items": { "type": "string" }, "description": "UDF DDL deployed alongside the MV." },
          "overrides":          { "type": "object", "description": "Canonical normalized parameter overrides (deviations from default)." },
          "enabled":            { "type": "boolean" },
          "deleted":            { "type": "boolean" },
          "severity":           { "type": "string", "enum": ["critical", "warning", "info"] },
          "category":           { "type": "string" },
          "required_telemetry": { "type": "string" },
          "fp_rate":            { "type": "string", "enum": ["low", "medium", "high"] },
          "hook_sources":       { "type": "string" },
          "block_policy":       { "type": "string", "enum": ["log_only", "hold", "auto_block"] },
          "update_reason":      { "type": "string" },
          "updated_by":         { "type": "string", "description": "Authenticated Timeplus user who made this change." },
          "updated_at":         { "type": "string", "format": "date-time", "description": "Server-stamped audit timestamp (NOT the ordering key)." }
        }
      },
      "RulesResponse": {
        "type": "object",
        "description": "Response from GET /rules.",
        "properties": {
          "rule_packs":           { "type": "array", "items": { "$ref": "#/components/schemas/RulePack" } },
          "total_detections_24h": { "type": "integer", "description": "Sum of detections_24h across all installed rules." },
          "active_rule_density":  { "type": "integer", "description": "Percentage of catalog rules that are currently enabled." },
          "recent_detections":    { "type": "array", "items": { "$ref": "#/components/schemas/SecurityEvent" }, "description": "Latest security events across all rules." }
        }
      },
      "RuleDetailResponse": {
        "type": "object",
        "description": "Response from GET /rules/{id}: the full instance snapshot plus the current definition's parameter schema, defaults, the instance's overrides, effective values, deployed (read-only) rendered SQL, MV health, the pending-upgrade plan (if the catalog has moved past the installed definition), and recent detections.",
        "properties": {
          "instance":     { "type": "object", "description": "The complete current Rule Instance snapshot (rule_id, revision, snapshot_id, definition_version/hash, rendered_sql, deployed_udfs, overrides, enabled, deleted, block_policy, severity, audit metadata)." },
          "in_catalog":   { "type": "boolean", "description": "Whether a catalog definition currently backs this rule." },
          "definition":   { "type": "object", "description": "The current catalog definition (when in_catalog)." },
          "schema":       { "type": "array", "items": { "type": "object" }, "description": "Ordered parameter schema from the current definition." },
          "defaults":     { "type": "object", "description": "Normalized default value per parameter key." },
          "overrides":    { "type": "object", "description": "The instance's persisted parameter overrides (deviations from default)." },
          "effective":    { "type": "object", "description": "Defaults overlaid with overrides, normalized." },
          "rendered_sql": { "type": "string", "description": "The deployed, read-only rendered SELECT." },
          "upgrade":      { "type": "object", "description": "Pending-upgrade plan (preserved overrides, warnings, conflicts) when the catalog definition_hash differs from the installed one; omitted otherwise.", "nullable": true },
          "mv":           { "type": "object", "description": "Deployed-MV health: exists, failed_state, last_failed_at." },
          "detections_24h":    { "type": "integer" },
          "recent_detections": { "type": "array", "items": { "$ref": "#/components/schemas/SecurityEvent" } }
        }
      },
      "SecurityEvent": {
        "type": "object",
        "description": "A single security detection event from agentguard_security_events.",
        "properties": {
          "rule_id":     { "type": "string" },
          "rule_name":   { "type": "string" },
          "severity":    { "type": "string", "enum": ["critical", "warning", "info"] },
          "agent_id":    { "type": "string" },
          "hook_name":   { "type": "string" },
          "message":     { "type": "string" },
          "detected_at": { "type": "string" }
        }
      },
      "CatalogItem": {
        "type": "object",
        "description": "A rule definition from the file-based catalog, annotated with its current install state in Proton.",
        "properties": {
          "id":                  { "type": "string",  "description": "Rule identifier, e.g. rp-001 or custom-my-rule.", "example": "rp-001" },
          "name":                { "type": "string",  "example": "Prompt Injection Shield" },
          "version":             { "type": "string",  "description": "Catalog version (semver X.Y.Z).", "example": "1.0.0" },
          "description":         { "type": "string" },
          "severity":            { "type": "string",  "enum": ["critical", "warning", "info"] },
          "category":            { "type": "string",  "description": "Rule category, e.g. prompt_injection, dlp, privilege, supply_chain." },
          "fp_rate":             { "type": "string",  "description": "Expected false-positive rate.", "enum": ["low", "medium", "high"] },
          "hook_sources":        { "type": "string",  "description": "Comma-separated hook names this rule inspects." },
          "required_telemetry":  { "type": "string",  "description": "Optional telemetry prerequisite note." },
          "author":              { "type": "string",  "description": "Rule author, e.g. Timeplus.", "example": "Timeplus" },
          "tags":                { "type": "array", "items": { "type": "string" }, "description": "Labels for filtering. Orphaned rules (installed but absent from catalog) carry the 'orphaned' tag.", "example": ["security", "prompt-injection"] },
          "installed":           { "type": "boolean", "description": "True when this rule exists in agentguard_rules and is not deleted." },
          "enabled":             { "type": "boolean", "description": "True when the rule is installed and its MV is active." },
          "installed_version":   { "type": "string",  "description": "Version string of the installed rule (omitted when not installed).", "example": "1.0.0" },
          "upgradeable":         { "type": "boolean", "description": "True when the catalog version is newer than the installed version — call /rules/{id}/upgrade to update." }
        }
      },
      "RuleInput": {
        "type": "object",
        "description": "Request body for creating a custom rule. Creates a zero-parameter custom definition and installs it.",
        "required": ["name", "description", "query_sql"],
        "properties": {
          "id":           { "type": "string",  "description": "Optional user-specified rule ID (letters, digits, hyphens, underscores; max 64 chars). Must be unique across the catalog. Auto-generated with a custom- prefix when omitted." },
          "name":         { "type": "string",  "description": "Display name for the rule." },
          "description":  { "type": "string" },
          "query_sql":    { "type": "string",  "description": "Proton SELECT targeting agentguard_hook_events; stored verbatim as the zero-parameter sql_template (rendered SQL). Must output all agentguard_security_events columns." },
          "severity":     { "type": "string",  "enum": ["critical", "warning", "info"] },
          "category":     { "type": "string",  "description": "Logical category for grouping, e.g. dlp, privilege." },
          "fp_rate":      { "type": "string",  "description": "Expected false-positive rate.", "enum": ["low", "medium", "high"] },
          "hook_sources": { "type": "string",  "description": "Comma-separated hook names for documentation purposes." }
        }
      },
      "ImportResult": {
        "type": "object",
        "description": "Summary of a catalog import operation.",
        "properties": {
          "added":   { "type": "array", "items": { "type": "string" }, "description": "Rule IDs that were newly added to the catalog." },
          "updated": { "type": "array", "items": { "type": "string" }, "description": "Rule IDs whose catalog entry was overwritten (newer version or force=true)." },
          "skipped": { "type": "array", "items": { "type": "string" }, "description": "Rule IDs skipped because the imported version was not newer than the existing imported entry." },
          "skipped_builtin": { "type": "array", "items": { "type": "string" }, "description": "Rule IDs skipped because the existing entry is builtin. Import never overwrites a builtin, even with force=true: a local shadow would replace builtin detection SQL under the builtin's identity. Clone the rule instead." },
          "skipped_custom": { "type": "array", "items": { "type": "string" }, "description": "Rule IDs skipped because the existing entry is a user-authored custom rule and force was not set." },
          "errors":  { "type": "array", "items": { "type": "string" }, "description": "Parse or write errors encountered per file (non-fatal — other files are still processed)." }
        }
      },
      "Alert": {
        "type": "object",
        "description": "Alert payload posted by the Timeplus UDF and pushed to SSE clients.",
        "properties": {
          "rule_id":      { "type": "string" },
          "rule_name":    { "type": "string" },
          "severity":     { "type": "string", "enum": ["critical", "warning", "info"] },
          "agent_id":     { "type": "string" },
          "deployment_id":{ "type": "string" },
          "message":      { "type": "string" },
          "detected_at":  { "type": "string", "description": "Detection timestamp (string, as sent by Timeplus UDF)." }
        }
      },
      "AuthStatus": {
        "type": "object",
        "description": "Server setup and session state returned by GET /auth/status.",
        "properties": {
          "setup_complete": { "type": "boolean", "description": "True when the setup wizard has been completed and config.yaml contains setup_complete=true." },
          "logged_in":      { "type": "boolean", "description": "True when the request carries a valid agentguard_session cookie." },
          "username":       { "type": "string",  "description": "Timeplus username of the active session. Empty when logged_in=false." }
        }
      },
      "LoginRequest": {
        "type": "object",
        "description": "Credentials for POST /auth/login.",
        "required": ["username", "password"],
        "properties": {
          "username": { "type": "string", "description": "Timeplus username.", "example": "default" },
          "password": { "type": "string", "description": "Timeplus password. May be an empty string for password-less deployments.", "example": "" }
        }
      },
      "LoginResponse": {
        "type": "object",
        "description": "Response body for a successful POST /auth/login.",
        "properties": {
          "username": { "type": "string", "description": "Timeplus username of the newly created session.", "example": "default" }
        }
      },
      "TestConnectionRequest": {
        "type": "object",
        "description": "Connection parameters for POST /setup/test-connection.",
        "required": ["host", "port", "username"],
        "properties": {
          "host":     { "type": "string",  "description": "Timeplus hostname or IP.", "example": "localhost" },
          "port":     { "type": "integer", "description": "Timeplus HTTP API port.", "example": 3218 },
          "username": { "type": "string",  "description": "Timeplus username.", "example": "default" },
          "password": { "type": "string",  "description": "Timeplus password.", "example": "" }
        }
      },
      "ProvisionRequest": {
        "type": "object",
        "description": "Full connection details for POST /setup/provision.",
        "required": ["host", "port", "native_port", "username"],
        "properties": {
          "host":           { "type": "string",  "description": "Timeplus hostname or IP.", "example": "localhost" },
          "port":           { "type": "integer", "description": "Timeplus HTTP API port.", "example": 3218 },
          "native_port":    { "type": "integer", "description": "Timeplus native TCP port.", "example": 8463 },
          "username":       { "type": "string",  "description": "Timeplus username.", "example": "default" },
          "password":       { "type": "string",  "description": "Timeplus password.", "example": "" }
        }
      },
      "ProvisionEvent": {
        "type": "object",
        "description": "A single SSE event from POST /setup/provision.",
        "properties": {
          "status": { "type": "string", "enum": ["running", "ok", "error", "done"], "description": "running=step in progress, ok=step succeeded, error=step failed (provisioning continues), done=all steps complete." },
          "msg":    { "type": "string", "description": "Human-readable message describing the provisioning step." }
        }
      },
      "Hold": {
        "type": "object",
        "description": "A hold record from agentguard_holds. Current state is computed inline via arg_max(field, updated_at) GROUP BY hold_id.",
        "properties": {
          "hold_id":        { "type": "string",  "description": "UUID for this hold." },
          "deployment_id":  { "type": "string" },
          "agent_id":       { "type": "string" },
          "session_id":     { "type": "string" },
          "run_id":         { "type": "string" },
          "tool_name":      { "type": "string",  "description": "The tool call that was intercepted." },
          "rule_id":        { "type": "string",  "description": "The rule that triggered the hold." },
          "severity":       { "type": "string",  "enum": ["critical", "warning", "info"] },
          "args_summary":   { "type": "string",  "description": "Truncated JSON of tool_input (max 2048 bytes)." },
          "message":        { "type": "string",  "description": "Human-readable reason the hold was created." },
          "created_at":     { "type": "string",  "format": "date-time" },
          "status":         { "type": "string",  "enum": ["pending", "approved", "denied", "timeout", "abandoned"] },
          "decided_by":     { "type": "string",  "description": "Email or identifier of the person who decided the hold. Empty for system decisions." },
          "decided_at":     { "type": "string",  "format": "date-time" },
          "decided_reason": { "type": "string" }
        }
      },
      "HoldRequest": {
        "type": "object",
        "description": "Request body sent by agent plugins to POST /holds.",
        "properties": {
          "deployment_id":   { "type": "string" },
          "agent_id":        { "type": "string" },
          "session_id":      { "type": "string" },
          "run_id":          { "type": "string" },
          "tool_name":       { "type": "string" },
          "tool_input":      { "type": "object", "description": "Full tool arguments as a JSON object." },
          "event_time":      { "type": "string", "format": "date-time", "description": "Timestamp when the PreToolUse hook fired." },
          "hook_payload":    { "type": "object", "description": "Full raw hook payload from the plugin." },
          "agent_type":      { "type": "string", "enum": ["claudecode", "openclaw"], "description": "Plugin type. Defaults to claudecode if empty." },
          "deployment_name": { "type": "string", "description": "Human-readable deployment name (optional)." }
        }
      },
      "HoldResponse": {
        "type": "object",
        "description": "Response returned by POST /holds to the waiting plugin.",
        "properties": {
          "decision": { "type": "string", "enum": ["allow", "block"], "description": "Whether the tool call should proceed." },
          "reason":   { "type": "string", "description": "Human-readable explanation (populated for block decisions)." },
          "hold_id":  { "type": "string", "description": "UUID of the hold record opened for human review (only present when a hold was created)." }
        }
      },
      "HoldDecideRequest": {
        "type": "object",
        "description": "Request body for POST /holds/{hold_id}/decide.",
        "required": ["decision"],
        "properties": {
          "decision":       { "type": "string", "enum": ["approved", "denied"], "description": "Human decision: approved allows the tool call, denied blocks it." },
          "decided_by":     { "type": "string", "description": "Email or display name of the reviewer." },
          "decided_reason": { "type": "string", "description": "Optional explanation for audit trail." }
        }
      },
      "ActivityEvent": {
        "type": "object",
        "description": "One event in the activity feed. Sourced from agentguard_holds (is_hold_event=true) or agentguard_hook_events when include_allowed=true (is_hold_event=false).",
        "properties": {
          "id":             { "type": "string",  "description": "hold_id for hold events; synthetic hex hash for hook-only events." },
          "event_time":     { "type": "string",  "format": "date-time" },
          "tool_name":      { "type": "string" },
          "status":         { "type": "string",  "enum": ["pending", "approved", "denied", "timeout", "abandoned", "auto_block", "allowed"], "description": "For hold events: the hold lifecycle status. For hook-only events: always allowed." },
          "rule_id":        { "type": "string" },
          "rule_name":      { "type": "string" },
          "severity":       { "type": "string",  "enum": ["critical", "warning", "info", ""] },
          "agent_id":       { "type": "string" },
          "session_id":     { "type": "string" },
          "run_id":         { "type": "string" },
          "args_summary":   { "type": "string",  "description": "Truncated JSON of tool arguments." },
          "message":        { "type": "string" },
          "decided_by":     { "type": "string" },
          "decided_at":     { "type": "string",  "format": "date-time", "nullable": true },
          "decided_reason": { "type": "string" },
          "hook_payload":   { "type": "string",  "description": "Raw event_data JSON; only populated for hook-only (allowed) events." },
          "is_hold_event":  { "type": "boolean", "description": "True when this event was sourced from agentguard_holds." }
        }
      },
      "ActivityResponse": {
        "type": "object",
        "description": "Response from GET /activity.",
        "properties": {
          "events": { "type": "array", "items": { "$ref": "#/components/schemas/ActivityEvent" } },
          "total":  { "type": "integer" }
        }
      },
      "UnconfirmedNotification": {
        "type": "object",
        "description": "One unconfirmed threat notification, as returned by GET /notifications/unconfirmed.",
        "properties": {
          "id":            { "type": "string", "description": "Stable identity key: deployment_id|agent_id|session_id|rule_id." },
          "rule_id":       { "type": "string" },
          "rule_name":     { "type": "string" },
          "severity":      { "type": "string", "enum": ["critical", "warning", "info", ""] },
          "agent_id":      { "type": "string" },
          "deployment_id": { "type": "string" },
          "session_id":    { "type": "string" },
          "message":       { "type": "string" },
          "detected_at":   { "type": "string", "description": "Timeplus datetime64 text, e.g. 2026-04-09 10:00:05.000 (NOT RFC 3339)." }
        }
      },
      "UnconfirmedNotificationsResponse": {
        "type": "object",
        "description": "Response from GET /notifications/unconfirmed.",
        "properties": {
          "notifications": { "type": "array", "items": { "$ref": "#/components/schemas/UnconfirmedNotification" } }
        }
      },
      "NotificationConfirmRequest": {
        "type": "object",
        "description": "Request body for POST /notifications/confirm. deployment_id and session_id may legitimately be empty on older/partial events.",
        "required": ["agent_id", "rule_id"],
        "properties": {
          "agent_id":      { "type": "string" },
          "deployment_id": { "type": "string" },
          "session_id":    { "type": "string" },
          "rule_id":       { "type": "string" }
        }
      }
    },
    "securitySchemes": {
      "cookieAuth": {
        "type": "apiKey",
        "in": "cookie",
        "name": "agentguard_session",
        "description": "Session cookie set by POST /auth/login. Include credentials: 'include' on all fetch calls. 8-hour expiry; credentials are stored only in the server's in-memory session store — never on disk."
      }
    }
  }
}`
