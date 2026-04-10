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
  <title>AgentShield API — Redoc</title>
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
    <span class="topbar-title">AgentShield API</span>
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
  <title>AgentShield API</title>
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
    "title": "AgentShield API",
    "version": "1.0.0",
    "description": "AgentShield is a real-time security monitoring and governance platform for AI coding agents (OpenClaw, Claude Code).\n\nAll data is sourced from **Timeplus Proton** streaming SQL. The server falls back gracefully when Proton is unavailable.\n\n### Agent ID format\nMany endpoints accept an \u0060:id\u0060 path parameter in the compound form \u0060{deploymentId}__{agentId}\u0060 (double underscore separator). Both parts must match \u0060^[a-zA-Z0-9._:-]+$\u0060.\n\n### Time filtering\n\u0060since\u0060 / \u0060until\u0060 query parameters accept **RFC 3339** timestamps, e.g. \u00602026-04-01T00:00:00Z\u0060.\n\n### Pagination\nList endpoints accept \u0060?page=1&limit=15\u0060 and return \u0060total\u0060 so clients can compute page count."
  },
  "servers": [
    { "url": "/api", "description": "AgentShield backend (same origin)" }
  ],
  "tags": [
    { "name": "Dashboard",   "description": "High-level summary counters shown on the main dashboard." },
    { "name": "Agents",      "description": "Agent fleet — each unique (deployment_id, agent_id) pair registered via hook events." },
    { "name": "Sessions",    "description": "Sessions and runs within an agent. A session groups one user conversation; a run is a single LLM turn within that session." },
    { "name": "Metrics",     "description": "Aggregated LLM token usage and tool-call statistics." },
    { "name": "Threats",     "description": "Security detections emitted by active rule materialized views." },
    { "name": "Cost",        "description": "Token-based cost estimates computed from configurable per-model pricing." },
    { "name": "Rules",       "description": "Security rule packs. Each enabled rule has a corresponding Proton materialized view that writes matches to agentshield_security_events." },
    { "name": "Alerts",      "description": "Real-time alert delivery — webhook receiver and SSE notification stream." },
    { "name": "Investigate", "description": "Ad-hoc SQL query interface against Proton streams." },
    { "name": "Settings",    "description": "Application-level configuration." },
    { "name": "Proton",      "description": "Streaming proxy — send arbitrary SQL to Proton and receive NDJSON rows." }
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
        "description": "Builds a waterfall trace from raw \u0060agentshield_hook_events\u0060 — no OTel dependency required.\n\nEach hook event becomes a **child span** whose duration is the gap to the next event. The root span covers the full run window. Span kinds:\n- \u0060run\u0060 — root span for the entire LLM turn\n- \u0060llm\u0060 — events related to model inference (\u0060llm_input\u0060, \u0060llm_output\u0060, \u0060before_model_resolve\u0060, etc.)\n- \u0060tool\u0060 — tool-call lifecycle (\u0060before_tool_call\u0060, \u0060after_tool_call\u0060, \u0060tool_result_persist\u0060)\n- \u0060lifecycle\u0060 — all other hook events\n\nSpan status is \u0060blocked\u0060 when \u0060hook_decision = block\u0060, \u0060error\u0060 when \u0060event_data.error\u0060 is set, otherwise \u0060ok\u0060.",
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
        "description": "Returns raw \u0060agentshield_hook_events\u0060 rows for the given agent, ordered newest-first. Useful for audit trails and forensic investigation.\n\nEach event record includes the hook name, timestamp, tool name (if applicable), model, hook decision, and the raw \u0060event_data\u0060 JSON payload.",
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
        "description": "Returns aggregated LLM token consumption broken down by model and provider. Includes total calls, input tokens, output tokens, and cache-read tokens.\n\nData is read from \u0060agentshield_hook_events\u0060 where \u0060hook_name = 'llm_output'\u0060 and token counts are stored in the \u0060event_data\u0060 JSON field.",
        "operationId": "getLLMMetrics",
        "parameters": [
          { "name": "since", "in": "query", "description": "RFC 3339 lower bound.", "schema": { "type": "string", "format": "date-time" } },
          { "name": "until", "in": "query", "description": "RFC 3339 upper bound.", "schema": { "type": "string", "format": "date-time" } }
        ],
        "responses": {
          "200": { "description": "LLM metrics by model", "content": { "application/json": { "schema": { "$ref": "#/components/schemas/LLMMetrics" },
            "example": {
              "total_calls": 412,
              "total_input_tokens": 820000,
              "total_output_tokens": 410000,
              "total_cache_read_tokens": 50000,
              "models": [
                { "model": "claude-sonnet-4-6", "provider": "anthropic", "call_count": 390, "input_tokens": 780000, "output_tokens": 390000, "cache_read_tokens": 48000 },
                { "model": "claude-opus-4-6",   "provider": "anthropic", "call_count": 22,  "input_tokens": 40000,  "output_tokens": 20000,  "cache_read_tokens": 2000 }
              ]
            }
          }}}
        }
      }
    },

    "/metrics/tools": {
      "get": {
        "tags": ["Metrics"],
        "summary": "Get tool usage metrics",
        "description": "Returns tool-call counts aggregated by tool name, including the number of calls that were blocked by a security rule.\n\nDerived from \u0060agentshield_hook_events\u0060 where \u0060hook_name = 'before_tool_call'\u0060.",
        "operationId": "getToolMetrics",
        "parameters": [
          { "name": "since", "in": "query", "description": "RFC 3339 lower bound.", "schema": { "type": "string", "format": "date-time" } },
          { "name": "until", "in": "query", "description": "RFC 3339 upper bound.", "schema": { "type": "string", "format": "date-time" } }
        ],
        "responses": {
          "200": { "description": "Tool usage breakdown", "content": { "application/json": { "schema": { "$ref": "#/components/schemas/ToolMetrics" },
            "example": {
              "total_calls": 1840,
              "tools": [
                { "tool_name": "Read",  "call_count": 820, "block_count": 0 },
                { "tool_name": "Bash",  "call_count": 310, "block_count": 2 },
                { "tool_name": "Write", "call_count": 280, "block_count": 0 }
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
        "description": "Returns detections written to \u0060agentshield_security_events\u0060 by active rule materialized views, ordered newest-first.\n\nEach detection record includes the triggering rule, severity, affected agent, session context, and the raw hook event data that caused the match.",
        "operationId": "listThreats",
        "parameters": [
          { "name": "severity", "in": "query", "description": "Filter by severity level.", "schema": { "type": "string", "enum": ["critical", "warning", "info"] } },
          { "name": "category", "in": "query", "description": "Filter by rule category, e.g. \u0060prompt_injection\u0060, \u0060dlp\u0060, \u0060privilege\u0060, \u0060supply_chain\u0060.", "schema": { "type": "string" } },
          { "name": "page",  "in": "query", "schema": { "type": "integer", "default": 1 } },
          { "name": "limit", "in": "query", "schema": { "type": "integer", "default": 50 } }
        ],
        "responses": {
          "200": { "description": "Threat list", "content": { "application/json": { "schema": {
            "type": "object",
            "properties": {
              "threats": { "type": "array", "items": { "$ref": "#/components/schemas/Threat" } },
              "total": { "type": "integer" }
            }
          }, "example": {
            "threats": [{
              "rule_id": "rp-001",
              "rule_name": "Prompt Injection Shield",
              "severity": "critical",
              "agent_id": "claude-code",
              "deployment_id": "eng-team",
              "session_id": "a1b2c3d4-0000-0000-0000-000000000001",
              "run_id": "chatcmpl_4ff08efb-c086-4927-9305-20ef7f95d329",
              "hook_name": "llm_input",
              "tool_name": "",
              "message": "Potential prompt injection in llm_input from agent claude-code",
              "detected_at": "2026-04-09T10:00:00Z"
            }],
            "total": 7
          }}}}
        }
      }
    },

    "/cost": {
      "get": {
        "tags": ["Cost"],
        "summary": "Get cost governance summary",
        "description": "Returns token-based cost estimates computed by joining \u0060agentshield_hook_events\u0060 token counts with the \u0060agentshield_pricing\u0060 table.\n\nIncludes a daily spend time-series and a breakdown by model. Costs are in USD. Pricing data is seeded with public list prices and can be updated via the \u0060/cost/pricing\u0060 endpoints.",
        "operationId": "getCost",
        "parameters": [
          { "name": "since", "in": "query", "description": "RFC 3339 lower bound for the aggregation window.", "schema": { "type": "string", "format": "date-time" } },
          { "name": "until", "in": "query", "description": "RFC 3339 upper bound.", "schema": { "type": "string", "format": "date-time" } }
        ],
        "responses": {
          "200": { "description": "Cost summary", "content": { "application/json": { "schema": { "$ref": "#/components/schemas/CostSummary" },
            "example": {
              "total_cost_usd": 12.84,
              "daily": [
                { "date": "2026-04-08", "cost_usd": 4.20, "input_tokens": 420000, "output_tokens": 210000 },
                { "date": "2026-04-09", "cost_usd": 8.64, "input_tokens": 820000, "output_tokens": 410000 }
              ],
              "by_model": [
                { "model": "claude-sonnet-4-6", "provider": "anthropic", "cost_usd": 11.52 },
                { "model": "claude-opus-4-6",   "provider": "anthropic", "cost_usd": 1.32 }
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
        "description": "Returns all entries in the \u0060agentshield_pricing\u0060 mutable stream. Each entry defines the cost per million tokens (input, output, cache-read) for a provider/model combination.\n\nDefault entries are seeded at startup for Anthropic and OpenAI models.",
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
        "description": "Removes the pricing record for the given provider/model pair from \u0060agentshield_pricing\u0060.",
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

    "/rules": {
      "get": {
        "tags": ["Rules"],
        "summary": "List installed rules",
        "description": "Returns all non-deleted rules from the \u0060agentshield_rules\u0060 append-only stream. Current state is computed via \u0060arg_max(field, updated_at) GROUP BY rule_id\u0060.",
        "operationId": "listRules",
        "responses": {
          "200": { "description": "Installed rules", "content": { "application/json": { "schema": {
            "type": "object",
            "properties": { "rule_packs": { "type": "array", "items": { "$ref": "#/components/schemas/RulePack" } } }
          }, "example": {
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
              "updated_at": "2026-04-01T00:00:00Z"
            }]
          }}}}
        }
      },
      "post": {
        "tags": ["Rules"],
        "summary": "Create a custom rule",
        "description": "Creates a new custom rule with a user-supplied SQL query. The \u0060rule_id\u0060 is auto-generated with a \u0060custom-\u0060 prefix.\n\nThe \u0060query_sql\u0060 must be a valid Proton \u0060SELECT\u0060 that queries \u0060agentshield_hook_events\u0060 and outputs columns matching \u0060agentshield_security_events\u0060: \u0060detected_at\u0060, \u0060rule_id\u0060, \u0060rule_name\u0060, \u0060severity\u0060, \u0060agent_id\u0060, \u0060deployment_id\u0060, \u0060session_id\u0060, \u0060run_id\u0060, \u0060hook_name\u0060, \u0060tool_name\u0060, \u0060message\u0060, \u0060event_data\u0060.",
        "operationId": "createRule",
        "requestBody": {
          "required": true,
          "content": { "application/json": { "schema": { "$ref": "#/components/schemas/RuleInput" },
            "example": {
              "name": "Exfil Watch",
              "description": "Alerts when an agent reads and then immediately uploads a file.",
              "query_sql": "SELECT event_time AS detected_at, 'custom-exfil' AS rule_id, 'Exfil Watch' AS rule_name, 'critical' AS severity, agent_id, deployment_id, session_id, run_id, hook_name, tool_name, concat('Possible exfil by ', agent_id) AS message, event_data FROM agentshield_hook_events WHERE hook_name = 'before_tool_call' AND lower(event_data) LIKE '%upload%'",
              "severity": "critical",
              "category": "dlp",
              "fp_rate": "low",
              "hook_sources": "before_tool_call"
            }
          }}
        },
        "responses": {
          "201": { "description": "Rule created and MV started.", "content": { "application/json": { "example": { "id": "custom-a1b2c3", "name": "Exfil Watch", "enabled": false } } } },
          "400": { "description": "Invalid or missing fields." }
        }
      }
    },

    "/rules/catalog": {
      "get": {
        "tags": ["Rules"],
        "summary": "List builtin rule catalog",
        "description": "Returns all builtin rule definitions (currently \u0060rp-001\u0060 through \u0060rp-004\u0060) plus any previously installed and then deleted custom rules, each annotated with an \u0060installed\u0060 flag indicating whether the rule is currently active in \u0060agentshield_rules\u0060.\n\nUse this endpoint to power an install/uninstall UI — show the catalog, highlight installed entries, and call \u0060/rules/{id}/install\u0060 or \u0060DELETE /rules/{id}\u0060 accordingly.",
        "operationId": "getRulesCatalog",
        "responses": {
          "200": { "description": "Rule catalog with installed status", "content": { "application/json": { "schema": {
            "type": "object",
            "properties": { "catalog": { "type": "array", "items": { "$ref": "#/components/schemas/CatalogItem" } } }
          }, "example": {
            "catalog": [
              { "id": "rp-001", "name": "Prompt Injection Shield", "version": "1.0.0", "description": "Detects prompt injection attacks.", "severity": "critical", "category": "prompt_injection", "fp_rate": "medium", "hook_sources": "llm_input", "installed": true, "is_custom": false },
              { "id": "rp-002", "name": "DLP Sentinel",            "version": "1.0.0", "description": "Prevents PII and secret leakage.",  "severity": "critical", "category": "dlp",              "fp_rate": "low",    "hook_sources": "tool_result_persist,after_tool_call,llm_output", "installed": false, "is_custom": false }
            ]
          }}}}
        }
      }
    },

    "/rules/mv-status": {
      "get": {
        "tags": ["Rules"],
        "summary": "Materialized view health status",
        "description": "Returns the current status of each rule's Proton materialized view (\u0060mv_rule_{id}\u0060). Useful for diagnosing rules that are marked enabled but whose MV is paused, missing, or in an error state.",
        "operationId": "getMVStatus",
        "responses": {
          "200": { "description": "MV status per rule", "content": { "application/json": { "schema": {
            "type": "array",
            "items": {
              "type": "object",
              "properties": {
                "rule_id": { "type": "string", "description": "Rule identifier." },
                "mv_name": { "type": "string", "description": "Proton MV name, e.g. \u0060mv_rule_rp_001\u0060." },
                "status":  { "type": "string", "description": "MV state reported by Proton: \u0060active\u0060, \u0060paused\u0060, or \u0060missing\u0060." }
              }
            }
          }, "example": [
            { "rule_id": "rp-001", "mv_name": "mv_rule_rp_001", "status": "active" },
            { "rule_id": "rp-003", "mv_name": "mv_rule_rp_003", "status": "paused" }
          ]}}}
        }
      }
    },

    "/rules/sync": {
      "post": {
        "tags": ["Rules"],
        "summary": "Reconcile MV state",
        "description": "Re-reads all rules from \u0060agentshield_rules\u0060 and reconciles each MV: creates missing MVs for enabled rules, resumes paused MVs, and pauses MVs for disabled rules. Equivalent to what happens at server startup, but callable at runtime without a restart.",
        "operationId": "syncMVs",
        "responses": {
          "200": { "description": "Sync complete.", "content": { "application/json": { "example": { "synced": 4, "created": 0, "resumed": 1, "paused": 0 } } } }
        }
      }
    },

    "/rules/{id}": {
      "get": {
        "tags": ["Rules"],
        "summary": "Get rule by ID",
        "description": "Returns the current state of a single rule. The current state is resolved from the append-only \u0060agentshield_rules\u0060 stream via \u0060arg_max(updated_at) GROUP BY rule_id\u0060.",
        "operationId": "getRule",
        "parameters": [
          { "name": "id", "in": "path", "required": true, "description": "Rule ID, e.g. \u0060rp-001\u0060 or \u0060custom-a1b2c3\u0060.", "schema": { "type": "string" }, "example": "rp-001" }
        ],
        "responses": {
          "200": { "description": "Rule detail", "content": { "application/json": { "schema": { "$ref": "#/components/schemas/RulePack" } } } },
          "404": { "description": "Rule not found or deleted." }
        }
      },
      "delete": {
        "tags": ["Rules"],
        "summary": "Uninstall a rule",
        "description": "Soft-deletes a rule by appending a \u0060deleted=true\u0060 record to \u0060agentshield_rules\u0060, then drops the corresponding materialized view.\n\nThe rule remains in the stream history and will appear in the catalog as a reinstallable entry (for custom rules) or as \u0060installed: false\u0060 (for builtin rules).",
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
        "description": "Installs a builtin rule (prefixed \u0060rp-\u0060) from the hardcoded catalog, or reinstalls a previously deleted custom rule from its last-known stream state.\n\nInstalling a rule does **not** automatically enable it — call \u0060/rules/{id}/toggle\u0060 afterwards to start the MV.",
        "operationId": "installRule",
        "parameters": [
          { "name": "id", "in": "path", "required": true, "description": "Catalog rule ID, e.g. \u0060rp-002\u0060.", "schema": { "type": "string" }, "example": "rp-002" }
        ],
        "responses": {
          "200": { "description": "Installed.", "content": { "application/json": { "example": { "installed": true, "rule_id": "rp-002" } } } },
          "404": { "description": "Rule not in catalog." },
          "409": { "description": "Rule is already installed." }
        }
      }
    },

    "/rules/{id}/toggle": {
      "post": {
        "tags": ["Rules"],
        "summary": "Toggle rule enabled/disabled",
        "description": "Flips the \u0060enabled\u0060 flag of a rule and atomically pauses or resumes its Proton materialized view.\n\n- Enabling a rule that has no MV will **create** the MV.\n- Disabling a rule will **pause** its MV (events stop being matched but the MV is not dropped).",
        "operationId": "toggleRule",
        "parameters": [
          { "name": "id", "in": "path", "required": true, "description": "Rule ID.", "schema": { "type": "string" }, "example": "rp-001" }
        ],
        "responses": {
          "200": { "description": "Updated rule state.", "content": { "application/json": { "schema": { "$ref": "#/components/schemas/RulePack" },
            "example": { "id": "rp-001", "name": "Prompt Injection Shield", "enabled": false, "updated_at": "2026-04-09T15:00:00Z" }
          }}},
          "404": { "description": "Rule not found." }
        }
      }
    },

    "/rules/{id}/upgrade": {
      "post": {
        "tags": ["Rules"],
        "summary": "Upgrade rule SQL and rebuild its MV",
        "description": "Replaces the \u0060query_sql\u0060 of an existing rule with a new version, then drops and recreates the MV with the new query. The rule's \u0060enabled\u0060 state is preserved.",
        "operationId": "upgradeRule",
        "parameters": [
          { "name": "id", "in": "path", "required": true, "description": "Rule ID.", "schema": { "type": "string" }, "example": "rp-003" }
        ],
        "requestBody": {
          "required": true,
          "content": { "application/json": { "schema": {
            "type": "object",
            "required": ["query_sql"],
            "properties": { "query_sql": { "type": "string", "description": "New SELECT statement targeting agentshield_hook_events." } }
          }, "example": { "query_sql": "SELECT event_time AS detected_at, 'rp-003' AS rule_id, 'Privilege Guard' AS rule_name, 'warning' AS severity, agent_id, deployment_id, session_id, run_id, hook_name, tool_name, concat('Sensitive tool: ', tool_name) AS message, event_data FROM agentshield_hook_events WHERE hook_name = 'before_tool_call' AND lower(tool_name) IN ('bash','shell','computer')" }
          }}
        },
        "responses": {
          "200": { "description": "Rule upgraded, MV rebuilt.", "content": { "application/json": { "example": { "upgraded": true, "rule_id": "rp-003" } } } },
          "404": { "description": "Rule not found." }
        }
      }
    },

    "/alerts/webhook": {
      "post": {
        "tags": ["Alerts"],
        "summary": "Receive a rule-triggered alert",
        "description": "Webhook endpoint called by the Proton \u0060notify_agentshield_udf\u0060 Python UDF. The UDF is invoked by the \u0060agentshield_notifier\u0060 Proton alert when new rows appear in \u0060agentshield_security_events\u0060.\n\nReceived alerts are fanned out to all connected SSE clients on \u0060/notifications/stream\u0060. You can also POST to this endpoint manually for testing.",
        "operationId": "alertWebhook",
        "requestBody": {
          "required": true,
          "content": { "application/json": { "schema": { "$ref": "#/components/schemas/Alert" },
            "example": {
              "rule_id": "rp-001",
              "rule_name": "Prompt Injection Shield",
              "severity": "critical",
              "agent_id": "claude-code",
              "message": "Potential prompt injection in llm_input from agent claude-code",
              "detected_at": "2026-04-09T10:00:00Z"
            }
          }}
        },
        "responses": {
          "200": { "description": "Alert accepted and forwarded to SSE clients." }
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

    "/investigate/tables": {
      "get": {
        "tags": ["Investigate"],
        "summary": "List queryable tables",
        "description": "Returns the names of Proton streams and views available for ad-hoc queries via \u0060/investigate/query\u0060.\n\nIncludes the core AgentShield streams (\u0060agentshield_hook_events\u0060, \u0060agentshield_security_events\u0060, \u0060agentshield_rules\u0060) as well as OTel streams (\u0060otel_traces\u0060, \u0060otel_logs\u0060, \u0060otel_metrics_*\u0060).",
        "operationId": "listTables",
        "responses": {
          "200": { "description": "Available table names", "content": { "application/json": { "schema": {
            "type": "object",
            "properties": { "tables": { "type": "array", "items": { "type": "string" } } }
          }, "example": { "tables": ["agentshield_hook_events", "agentshield_security_events", "agentshield_rules", "agentshield_pricing", "otel_traces", "otel_logs"] } }}}
        }
      }
    },

    "/investigate/query": {
      "post": {
        "tags": ["Investigate"],
        "summary": "Run an ad-hoc SQL query",
        "description": "Executes an arbitrary SQL SELECT against Proton and returns the result rows as a JSON array.\n\nUse \u0060table(stream_name)\u0060 to query historical (batch) data rather than streaming. Queries are forwarded directly to Proton — apply appropriate \u0060LIMIT\u0060 clauses to avoid large result sets.",
        "operationId": "runQuery",
        "requestBody": {
          "required": true,
          "content": { "application/json": { "schema": {
            "type": "object",
            "required": ["query"],
            "properties": { "query": { "type": "string", "description": "SQL SELECT statement." } }
          }, "example": { "query": "SELECT hook_name, count() AS n FROM table(agentshield_hook_events) GROUP BY hook_name ORDER BY n DESC LIMIT 10" }
          }}
        },
        "responses": {
          "200": { "description": "Query results", "content": { "application/json": { "example": {
            "rows": [
              { "hook_name": "before_tool_call", "n": 1840 },
              { "hook_name": "llm_output",       "n": 412 }
            ]
          }}}}
        }
      }
    },

    "/settings": {
      "get": {
        "tags": ["Settings"],
        "summary": "Get application settings",
        "description": "Returns current application-level configuration.",
        "operationId": "getSettings",
        "responses": {
          "200": { "description": "Current settings", "content": { "application/json": { "schema": { "$ref": "#/components/schemas/Settings" },
            "example": { "notifications_enabled": true, "alert_email": "security@example.com", "retention_days": 90 }
          }}}
        }
      },
      "put": {
        "tags": ["Settings"],
        "summary": "Update application settings",
        "description": "Replaces application-level configuration.",
        "operationId": "updateSettings",
        "requestBody": {
          "required": true,
          "content": { "application/json": { "schema": { "$ref": "#/components/schemas/Settings" },
            "example": { "notifications_enabled": true, "alert_email": "security@example.com", "retention_days": 30 }
          }}
        },
        "responses": {
          "200": { "description": "Updated settings.", "content": { "application/json": { "schema": { "$ref": "#/components/schemas/Settings" } } } }
        }
      }
    },

    "/proton/query": {
      "post": {
        "tags": ["Proton"],
        "summary": "Streaming SQL proxy (NDJSON)",
        "description": "Proxies a SQL query directly to Proton and streams result rows back as **newline-delimited JSON** (NDJSON). Unlike \u0060/investigate/query\u0060 this endpoint does not buffer results — it streams rows as Proton produces them, making it suitable for long-running or live streaming queries.\n\nThe frontend uses this via \u0060@timeplus/proton-javascript-driver\u0060 to power the real-time Investigate page.\n\nSend a \u0060POST\u0060 with \u0060{\"sql\": \"...\"}\u0060 and consume the response body as an NDJSON stream. Use an \u0060AbortController\u0060 to cancel.",
        "operationId": "protonQuery",
        "requestBody": {
          "required": true,
          "content": { "application/json": { "schema": {
            "type": "object",
            "required": ["sql"],
            "properties": { "sql": { "type": "string", "description": "Any valid Proton SQL. Omit \u0060table()\u0060 wrapper for live streaming; use \u0060table()\u0060 for historical batch queries." } }
          }, "example": { "sql": "SELECT hook_name, event_time, agent_id FROM agentshield_hook_events WHERE deployment_id = 'eng-team' LIMIT 100" }
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
        "tags": ["Settings"],
        "summary": "Generate an agent configuration snippet",
        "description": "Generates a configuration snippet for connecting an AI agent to AgentShield. Used by the onboarding wizard.",
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
          "200": { "description": "Generated config object.", "content": { "application/json": { "example": { "config": { "timeplusUrl": "http://localhost:3218", "stream": "agentshield_hook_events" } } } } }
        }
      }
    }
  },

  "components": {
    "schemas": {
      "Dashboard": {
        "type": "object",
        "description": "Top-level summary counters for the dashboard.",
        "properties": {
          "total_agents":  { "type": "integer", "description": "Number of distinct (deployment_id, agent_id) pairs seen in hook events." },
          "total_events":  { "type": "integer", "description": "Total hook events ingested across all agents." },
          "total_threats": { "type": "integer", "description": "Total detections written to agentshield_security_events." },
          "active_rules":  { "type": "integer", "description": "Number of rules currently marked enabled=true." }
        }
      },
      "Agent": {
        "type": "object",
        "description": "Aggregated stats for one agent (deployment_id + agent_id pair).",
        "properties": {
          "agent_id":       { "type": "string",  "description": "Agent identifier as reported by the plugin." },
          "deployment_id":  { "type": "string",  "description": "Deployment/team identifier." },
          "deployment_name":{ "type": "string",  "description": "Human-readable deployment name." },
          "event_count":    { "type": "integer", "description": "Total hook events from this agent." },
          "session_count":  { "type": "integer", "description": "Number of distinct sessions." },
          "total_tokens":   { "type": "integer", "description": "Sum of input + output + cache-read tokens." },
          "llm_call_count": { "type": "integer", "description": "Number of llm_output hook events (one per LLM turn)." },
          "last_seen":      { "type": "string", "format": "date-time", "description": "Timestamp of the most recent hook event." }
        }
      },
      "AgentDetail": {
        "allOf": [
          { "$ref": "#/components/schemas/Agent" },
          {
            "type": "object",
            "properties": {
              "input_tokens":       { "type": "integer", "description": "Prompt tokens consumed." },
              "output_tokens":      { "type": "integer", "description": "Completion tokens generated." },
              "cache_read_tokens":  { "type": "integer", "description": "Tokens served from Anthropic prompt cache." },
              "block_count":        { "type": "integer", "description": "Number of hook events where hook_decision=block." },
              "first_seen":         { "type": "string", "format": "date-time", "description": "Timestamp of the earliest hook event." },
              "models":             { "type": "array", "items": { "type": "string" }, "description": "Distinct model names used by this agent." }
            }
          }
        ]
      },
      "Session": {
        "type": "object",
        "description": "A single user conversation session within an agent.",
        "properties": {
          "session_id":   { "type": "string",  "description": "UUID identifying the session." },
          "session_key":  { "type": "string",  "description": "Raw session key from OpenClaw, e.g. announce:v1:agent:...:openai-user:abc." },
          "session_type": { "type": "string",  "description": "Derived session type.", "enum": ["Web UI", "HTTP API", "Cron", "Sub-agent", "Unknown"] },
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
        "description": "A raw hook event from agentshield_hook_events.",
        "properties": {
          "hook_name":     { "type": "string", "description": "OpenClaw lifecycle hook name, e.g. before_tool_call, llm_output, session_end." },
          "event_time":    { "type": "string", "format": "date-time" },
          "session_id":    { "type": "string" },
          "run_id":        { "type": "string" },
          "agent_id":      { "type": "string" },
          "deployment_id": { "type": "string" },
          "tool_name":     { "type": "string", "description": "Tool name for tool-call hooks, empty otherwise." },
          "provider":      { "type": "string", "description": "LLM provider, e.g. anthropic." },
          "model":         { "type": "string", "description": "LLM model name." },
          "hook_decision": { "type": "string", "description": "Plugin decision: observe (pass-through) or block.", "enum": ["observe", "block", ""] },
          "block_reason":  { "type": "string", "description": "Human-readable reason when hook_decision=block." },
          "event_data":    { "type": "string", "description": "JSON payload with hook-specific fields: tokens, durationMs, toolCallId, error, etc." }
        }
      },
      "LLMMetrics": {
        "type": "object",
        "properties": {
          "total_calls":             { "type": "integer", "description": "Total llm_output events across all agents." },
          "total_input_tokens":      { "type": "integer" },
          "total_output_tokens":     { "type": "integer" },
          "total_cache_read_tokens": { "type": "integer" },
          "models": {
            "type": "array",
            "description": "Per-model breakdown.",
            "items": {
              "type": "object",
              "properties": {
                "model":            { "type": "string" },
                "provider":         { "type": "string" },
                "call_count":       { "type": "integer" },
                "input_tokens":     { "type": "integer" },
                "output_tokens":    { "type": "integer" },
                "cache_read_tokens":{ "type": "integer" }
              }
            }
          }
        }
      },
      "ToolMetrics": {
        "type": "object",
        "properties": {
          "total_calls": { "type": "integer", "description": "Total before_tool_call events." },
          "tools": {
            "type": "array",
            "description": "Per-tool-name breakdown.",
            "items": {
              "type": "object",
              "properties": {
                "tool_name":   { "type": "string" },
                "call_count":  { "type": "integer" },
                "block_count": { "type": "integer", "description": "Calls where hook_decision=block." }
              }
            }
          }
        }
      },
      "Threat": {
        "type": "object",
        "description": "A security detection written by a rule materialized view.",
        "properties": {
          "rule_id":      { "type": "string" },
          "rule_name":    { "type": "string" },
          "severity":     { "type": "string", "enum": ["critical", "warning", "info"] },
          "agent_id":     { "type": "string" },
          "deployment_id":{ "type": "string" },
          "session_id":   { "type": "string" },
          "run_id":       { "type": "string" },
          "hook_name":    { "type": "string", "description": "Hook that triggered the detection." },
          "tool_name":    { "type": "string" },
          "message":      { "type": "string", "description": "Human-readable detection message." },
          "detected_at":  { "type": "string", "format": "date-time" },
          "event_data":   { "type": "string", "description": "Raw event_data JSON from the triggering hook event." }
        }
      },
      "CostSummary": {
        "type": "object",
        "properties": {
          "total_cost_usd": { "type": "number", "description": "Total estimated cost in USD for the queried period." },
          "daily": {
            "type": "array",
            "description": "Daily cost time-series.",
            "items": {
              "type": "object",
              "properties": {
                "date":          { "type": "string", "description": "Calendar date, YYYY-MM-DD." },
                "cost_usd":      { "type": "number" },
                "input_tokens":  { "type": "integer" },
                "output_tokens": { "type": "integer" }
              }
            }
          },
          "by_model": {
            "type": "array",
            "description": "Cost breakdown per model.",
            "items": {
              "type": "object",
              "properties": {
                "model":    { "type": "string" },
                "provider": { "type": "string" },
                "cost_usd": { "type": "number" }
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
      "RulePack": {
        "type": "object",
        "description": "An installed security rule and its current state.",
        "properties": {
          "id":           { "type": "string",  "description": "Rule identifier: rp-{N} for builtins, custom-{hash} for user-created.", "example": "rp-001" },
          "name":         { "type": "string",  "example": "Prompt Injection Shield" },
          "version":      { "type": "string",  "example": "1.0.0" },
          "description":  { "type": "string" },
          "enabled":      { "type": "boolean", "description": "Whether the rule's MV is currently active." },
          "severity":     { "type": "string",  "enum": ["critical", "warning", "info"] },
          "category":     { "type": "string",  "description": "Rule category: prompt_injection, dlp, privilege, supply_chain, custom." },
          "fp_rate":      { "type": "string",  "description": "Expected false-positive rate: low, medium, high.", "enum": ["low", "medium", "high"] },
          "hook_sources": { "type": "string",  "description": "Comma-separated list of hook names this rule inspects." },
          "updated_at":   { "type": "string",  "format": "date-time", "description": "Timestamp of the last state change." }
        }
      },
      "CatalogItem": {
        "type": "object",
        "description": "A builtin or reinstallable custom rule with its installed status.",
        "properties": {
          "id":           { "type": "string" },
          "name":         { "type": "string" },
          "version":      { "type": "string" },
          "description":  { "type": "string" },
          "severity":     { "type": "string",  "enum": ["critical", "warning", "info"] },
          "category":     { "type": "string" },
          "fp_rate":      { "type": "string" },
          "hook_sources": { "type": "string" },
          "installed":    { "type": "boolean", "description": "True when this rule exists in agentshield_rules and is not deleted." },
          "is_custom":    { "type": "boolean", "description": "True for user-created rules, false for builtins." }
        }
      },
      "RuleInput": {
        "type": "object",
        "description": "Request body for creating a custom rule.",
        "required": ["name", "query_sql", "severity", "category"],
        "properties": {
          "name":         { "type": "string",  "description": "Display name for the rule." },
          "description":  { "type": "string" },
          "query_sql":    { "type": "string",  "description": "Proton SELECT targeting agentshield_hook_events. Must output all agentshield_security_events columns." },
          "severity":     { "type": "string",  "enum": ["critical", "warning", "info"] },
          "category":     { "type": "string",  "description": "Logical category for grouping, e.g. dlp, privilege." },
          "fp_rate":      { "type": "string",  "description": "Expected false-positive rate.", "enum": ["low", "medium", "high"] },
          "hook_sources": { "type": "string",  "description": "Comma-separated hook names for documentation purposes." }
        }
      },
      "Alert": {
        "type": "object",
        "description": "Alert payload posted by the Proton UDF and pushed to SSE clients.",
        "properties": {
          "rule_id":     { "type": "string" },
          "rule_name":   { "type": "string" },
          "severity":    { "type": "string",  "enum": ["critical", "warning", "info"] },
          "agent_id":    { "type": "string" },
          "message":     { "type": "string" },
          "detected_at": { "type": "string",  "description": "Detection timestamp (string, as sent by Proton UDF)." }
        }
      },
      "Settings": {
        "type": "object",
        "properties": {
          "notifications_enabled": { "type": "boolean", "description": "Whether real-time alert notifications are enabled." },
          "alert_email":           { "type": "string",  "description": "Email address for alert delivery." },
          "retention_days":        { "type": "integer", "description": "Number of days to retain event data." }
        }
      }
    }
  }
}`
