---
title: AgentGuard API v1.0.0
language_tabs:
  - shell: curl
  - javascript: JS
  - python: Python
language_clients:
  - shell: ""
  - javascript: ""
  - python: ""
toc_footers: []
includes: []
search: false
highlight_theme: darkula
headingLevel: 2

---

<!-- Generator: Widdershins v4.0.1 -->

<h1 id="agentguard-api">AgentGuard API v1.0.0</h1>

> Scroll down for code samples, example requests and responses. Select a language for code samples from the tabs above or the mobile navigation menu.

AgentGuard is a real-time security monitoring and governance platform for AI coding agents (OpenClaw, Claude Code).

All data is sourced from **Timeplus Enterprise** streaming SQL. The server falls back gracefully when Timeplus is unavailable.

### Authentication
Most endpoints require an active session. Call `POST /auth/login` with Timeplus credentials to receive an `agentguard_session` httpOnly cookie. Include `credentials: 'include'` on all fetch calls. The exceptions are `/auth/*`, `/setup/*`, and `/alerts/webhook`, which are unauthenticated.

On first launch the setup wizard must run before login is possible. Call `GET /auth/status` to check `setup_complete` and `logged_in`.

### Agent ID format
Many endpoints accept an `:id` path parameter in the compound form `{deploymentId}__{agentId}` (double underscore separator). Both parts must match `^[a-zA-Z0-9._:-]+$`.

### Time filtering
`since` / `until` query parameters accept **RFC 3339** timestamps, e.g. `2026-04-01T00:00:00Z`.

### Pagination
List endpoints accept `?page=1&limit=15` and return `total` so clients can compute page count.

Base URLs:

* <a href="/api">/api</a>

# Authentication

* API Key (cookieAuth)
    - Parameter Name: **agentguard_session**, in: cookie. Session cookie set by POST /auth/login. Include credentials: 'include' on all fetch calls. 8-hour expiry; credentials are stored only in the server's in-memory session store — never on disk.

<h1 id="agentguard-api-dashboard">Dashboard</h1>

High-level summary counters shown on the main dashboard.

## Get dashboard summary stats

<a id="opIdgetDashboard"></a>

> Code samples

```shell
# You can also use wget
curl -X GET /api/dashboard \
  -H 'Accept: application/json'

```

```javascript

const headers = {
  'Accept':'application/json'
};

fetch('/api/dashboard',
{
  method: 'GET',

  headers: headers
})
.then(function(res) {
    return res.json();
}).then(function(body) {
    console.log(body);
});

```

```python
import requests
headers = {
  'Accept': 'application/json'
}

r = requests.get('/api/dashboard', headers = headers)

print(r.json())

```

`GET /dashboard`

Returns high-level counters for the main dashboard: total agents seen, total hook events ingested, active threats detected, and number of enabled security rules.

> Example responses

> 200 Response

```json
{
  "total_agents": 4,
  "total_events": 18432,
  "total_threats": 7,
  "active_rules": 3
}
```

<h3 id="get-dashboard-summary-stats-responses">Responses</h3>

|Status|Meaning|Description|Schema|
|---|---|---|---|
|200|[OK](https://tools.ietf.org/html/rfc7231#section-6.3.1)|Dashboard summary|[Dashboard](#schemadashboard)|
|503|[Service Unavailable](https://tools.ietf.org/html/rfc7231#section-6.6.4)|Timeplus unavailable — server returns cached or zero values|None|

<aside class="warning">
To perform this operation, you must be authenticated by means of one of the following methods:
cookieAuth
</aside>

<h1 id="agentguard-api-agents">Agents</h1>

Agent fleet — each unique (deployment_id, agent_id) pair registered via hook events.

## List agents

<a id="opIdlistAgents"></a>

> Code samples

```shell
# You can also use wget
curl -X GET /api/agents \
  -H 'Accept: application/json'

```

```javascript

const headers = {
  'Accept':'application/json'
};

fetch('/api/agents',
{
  method: 'GET',

  headers: headers
})
.then(function(res) {
    return res.json();
}).then(function(body) {
    console.log(body);
});

```

```python
import requests
headers = {
  'Accept': 'application/json'
}

r = requests.get('/api/agents', headers = headers)

print(r.json())

```

`GET /agents`

Returns a paginated list of all unique agents observed via hook events. Each row aggregates event counts, session counts, token usage, and the last-seen timestamp.

Agents are identified by the compound `(deployment_id, agent_id)` pair. The `id` field returned is `{deployment_id}__{agent_id}` and is used as the path parameter for detail endpoints.

<h3 id="list-agents-parameters">Parameters</h3>

|Name|In|Type|Required|Description|
|---|---|---|---|---|
|page|query|integer|false|1-based page number.|
|limit|query|integer|false|Rows per page.|

> Example responses

> 200 Response

```json
{
  "agents": [
    {
      "agent_id": "claude-code",
      "deployment_id": "eng-team",
      "deployment_name": "Engineering Team",
      "event_count": 5200,
      "session_count": 34,
      "total_tokens": 1280000,
      "llm_call_count": 412,
      "last_seen": "2026-04-09T14:22:01Z"
    }
  ],
  "total": 4,
  "page": 1,
  "limit": 15
}
```

<h3 id="list-agents-responses">Responses</h3>

|Status|Meaning|Description|Schema|
|---|---|---|---|
|200|[OK](https://tools.ietf.org/html/rfc7231#section-6.3.1)|Paginated agent list|Inline|

<h3 id="list-agents-responseschema">Response Schema</h3>

Status Code **200**

|Name|Type|Required|Restrictions|Description|
|---|---|---|---|---|
|» agents|[[Agent](#schemaagent)]|false|none|[Aggregated stats for one agent (deployment_id + agent_id pair).]|
|»» agent_id|string|false|none|Agent identifier as reported by the plugin.|
|»» deployment_id|string|false|none|Deployment/team identifier.|
|»» deployment_name|string|false|none|Human-readable deployment name.|
|»» agent_type|string|false|none|Plugin type: claudecode or openclaw.|
|»» status|string|false|none|Derived activity status.|
|»» event_count|integer|false|none|Total hook events from this agent.|
|»» session_count|integer|false|none|Number of distinct sessions.|
|»» run_count|integer|false|none|Number of distinct LLM runs.|
|»» tool_call_count|integer|false|none|Total before_tool_call events.|
|»» total_tokens|integer|false|none|Sum of input + output + cache-read tokens.|
|»» llm_call_count|integer|false|none|Number of llm_output hook events (one per LLM turn).|
|»» last_seen|string(date-time)|false|none|Timestamp of the most recent hook event.|
|» total|integer|false|none|Total number of agents (for pagination).|
|» page|integer|false|none|none|
|» limit|integer|false|none|none|

#### Enumerated Values

|Property|Value|
|---|---|
|agent_type|claudecode|
|agent_type|openclaw|
|agent_type||
|status|active|
|status|idle|

<aside class="warning">
To perform this operation, you must be authenticated by means of one of the following methods:
cookieAuth
</aside>

## Get agent detail

<a id="opIdgetAgent"></a>

> Code samples

```shell
# You can also use wget
curl -X GET /api/agents/{id} \
  -H 'Accept: application/json'

```

```javascript

const headers = {
  'Accept':'application/json'
};

fetch('/api/agents/{id}',
{
  method: 'GET',

  headers: headers
})
.then(function(res) {
    return res.json();
}).then(function(body) {
    console.log(body);
});

```

```python
import requests
headers = {
  'Accept': 'application/json'
}

r = requests.get('/api/agents/{id}', headers = headers)

print(r.json())

```

`GET /agents/{id}`

Returns detailed statistics for a single agent, including per-token-type breakdown (input / output / cache-read), block count, first-seen timestamp, and the list of LLM models used.

Optional `since` / `until` filters restrict the aggregation window.

<h3 id="get-agent-detail-parameters">Parameters</h3>

|Name|In|Type|Required|Description|
|---|---|---|---|---|
|id|path|string|true|Compound agent ID: `{deploymentId}__{agentId}`. Example: `eng-team__claude-code`.|
|since|query|string(date-time)|false|Return events on or after this RFC 3339 timestamp.|
|until|query|string(date-time)|false|Return events on or before this RFC 3339 timestamp.|

> Example responses

> 200 Response

```json
{
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
  "models": [
    "claude-sonnet-4-6",
    "claude-opus-4-6"
  ]
}
```

<h3 id="get-agent-detail-responses">Responses</h3>

|Status|Meaning|Description|Schema|
|---|---|---|---|
|200|[OK](https://tools.ietf.org/html/rfc7231#section-6.3.1)|Agent detail|[Agent](#schemaagent)|
|400|[Bad Request](https://tools.ietf.org/html/rfc7231#section-6.5.1)|Invalid agent id — must use `{deploymentId}__{agentId}` format.|None|
|404|[Not Found](https://tools.ietf.org/html/rfc7231#section-6.5.4)|No events found for this agent.|None|
|503|[Service Unavailable](https://tools.ietf.org/html/rfc7231#section-6.6.4)|Timeplus unavailable.|None|

<aside class="warning">
To perform this operation, you must be authenticated by means of one of the following methods:
cookieAuth
</aside>

## List hook events for an agent

<a id="opIdlistEvents"></a>

> Code samples

```shell
# You can also use wget
curl -X GET /api/agents/{id}/events \
  -H 'Accept: application/json'

```

```javascript

const headers = {
  'Accept':'application/json'
};

fetch('/api/agents/{id}/events',
{
  method: 'GET',

  headers: headers
})
.then(function(res) {
    return res.json();
}).then(function(body) {
    console.log(body);
});

```

```python
import requests
headers = {
  'Accept': 'application/json'
}

r = requests.get('/api/agents/{id}/events', headers = headers)

print(r.json())

```

`GET /agents/{id}/events`

Returns raw `agentguard_hook_events` rows for the given agent, ordered newest-first. Useful for audit trails and forensic investigation.

Each event record includes the hook name, timestamp, tool name (if applicable), model, hook decision, and the raw `event_data` JSON payload.

<h3 id="list-hook-events-for-an-agent-parameters">Parameters</h3>

|Name|In|Type|Required|Description|
|---|---|---|---|---|
|id|path|string|true|Compound agent ID.|
|page|query|integer|false|none|
|limit|query|integer|false|none|
|since|query|string(date-time)|false|RFC 3339 lower bound on event_time.|
|until|query|string(date-time)|false|RFC 3339 upper bound on event_time.|

> Example responses

> 200 Response

```json
{
  "events": [
    {
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
    }
  ],
  "total": 5200
}
```

<h3 id="list-hook-events-for-an-agent-responses">Responses</h3>

|Status|Meaning|Description|Schema|
|---|---|---|---|
|200|[OK](https://tools.ietf.org/html/rfc7231#section-6.3.1)|Paginated hook events|Inline|

<h3 id="list-hook-events-for-an-agent-responseschema">Response Schema</h3>

Status Code **200**

|Name|Type|Required|Restrictions|Description|
|---|---|---|---|---|
|» events|[[HookEvent](#schemahookevent)]|false|none|[A raw hook event from agentguard_hook_events returned by GET /agents/{id}/events.]|
|»» hook_name|string|false|none|Lifecycle hook name, e.g. before_tool_call, llm_output, session_end.|
|»» event_time|string(date-time)|false|none|none|
|»» session_id|string|false|none|none|
|»» run_id|string|false|none|none|
|»» tool_name|string|false|none|Tool name for tool-call hooks, empty otherwise.|
|»» provider|string|false|none|LLM provider, e.g. anthropic.|
|»» model|string|false|none|LLM model name.|
|»» hook_decision|string|false|none|Plugin decision: observe (pass-through) or block.|
|»» block_reason|string|false|none|Human-readable reason when hook_decision=block.|
|»» event_data|string|false|none|JSON payload with hook-specific fields: tokens, durationMs, toolCallId, error, etc.|
|»» agent_type|string|false|none|Agent plugin type: claudecode or openclaw.|
|» total|integer|false|none|none|

#### Enumerated Values

|Property|Value|
|---|---|
|hook_decision|observe|
|hook_decision|block|
|hook_decision||
|agent_type|claudecode|
|agent_type|openclaw|
|agent_type||

<aside class="warning">
To perform this operation, you must be authenticated by means of one of the following methods:
cookieAuth
</aside>

<h1 id="agentguard-api-sessions">Sessions</h1>

Sessions and runs within an agent. A session groups one user conversation; a run is a single LLM turn within that session.

## List sessions for an agent

<a id="opIdlistSessions"></a>

> Code samples

```shell
# You can also use wget
curl -X GET /api/agents/{id}/sessions \
  -H 'Accept: application/json'

```

```javascript

const headers = {
  'Accept':'application/json'
};

fetch('/api/agents/{id}/sessions',
{
  method: 'GET',

  headers: headers
})
.then(function(res) {
    return res.json();
}).then(function(body) {
    console.log(body);
});

```

```python
import requests
headers = {
  'Accept': 'application/json'
}

r = requests.get('/api/agents/{id}/sessions', headers = headers)

print(r.json())

```

`GET /agents/{id}/sessions`

Returns all sessions for the given agent, ordered by `last_seen` descending (newest first).

A **session** maps to a single user conversation. Temporary sessions (`temp:*`) and slug-generator internals are automatically filtered out. Only sessions that contain at least one run are returned.

The `session_type` is derived from the `session_key` pattern:
- `Web UI` — interactive dashboard session
- `HTTP API` — OpenAI-compat API client
- `Cron` — scheduled trigger
- `Sub-agent` — spawned by another agent

<h3 id="list-sessions-for-an-agent-parameters">Parameters</h3>

|Name|In|Type|Required|Description|
|---|---|---|---|---|
|id|path|string|true|Compound agent ID: `{deploymentId}__{agentId}`.|
|since|query|string(date-time)|false|Only include sessions with events on or after this RFC 3339 timestamp.|
|until|query|string(date-time)|false|Only include sessions with events on or before this RFC 3339 timestamp.|

> Example responses

> 200 Response

```json
[
  {
    "session_id": "a1b2c3d4-0000-0000-0000-000000000001",
    "session_key": "announce:v1:agent:claude-code:openai-user:abc123",
    "session_type": "HTTP API",
    "run_count": 8,
    "event_count": 142,
    "first_seen": "2026-04-09T10:00:00Z",
    "last_seen": "2026-04-09T10:45:00Z"
  }
]
```

<h3 id="list-sessions-for-an-agent-responses">Responses</h3>

|Status|Meaning|Description|Schema|
|---|---|---|---|
|200|[OK](https://tools.ietf.org/html/rfc7231#section-6.3.1)|Session list (newest first)|Inline|

<h3 id="list-sessions-for-an-agent-responseschema">Response Schema</h3>

Status Code **200**

|Name|Type|Required|Restrictions|Description|
|---|---|---|---|---|
|*anonymous*|[[Session](#schemasession)]|false|none|[A single user conversation session within an agent.]|
|» session_id|string|false|none|UUID identifying the session.|
|» session_key|string|false|none|Raw session key, e.g. announce:v1:agent:...:openai-user:abc.|
|» session_type|string|false|none|Derived session type.|
|» status|string|false|none|active when no session_end event exists; ended otherwise.|
|» model|string|false|none|LLM model used in this session (from the most recent llm_output event).|
|» provider|string|false|none|LLM provider, e.g. anthropic.|
|» run_count|integer|false|none|Number of distinct LLM turns (runs) in this session.|
|» event_count|integer|false|none|Total hook events in this session.|
|» first_seen|string(date-time)|false|none|none|
|» last_seen|string(date-time)|false|none|none|

#### Enumerated Values

|Property|Value|
|---|---|
|session_type|Web UI|
|session_type|HTTP API|
|session_type|Cron|
|session_type|Sub-agent|
|session_type|Unknown|
|status|active|
|status|ended|

<aside class="warning">
To perform this operation, you must be authenticated by means of one of the following methods:
cookieAuth
</aside>

## Get session detail with runs

<a id="opIdgetSession"></a>

> Code samples

```shell
# You can also use wget
curl -X GET /api/agents/{id}/sessions/{session_id} \
  -H 'Accept: application/json'

```

```javascript

const headers = {
  'Accept':'application/json'
};

fetch('/api/agents/{id}/sessions/{session_id}',
{
  method: 'GET',

  headers: headers
})
.then(function(res) {
    return res.json();
}).then(function(body) {
    console.log(body);
});

```

```python
import requests
headers = {
  'Accept': 'application/json'
}

r = requests.get('/api/agents/{id}/sessions/{session_id}', headers = headers)

print(r.json())

```

`GET /agents/{id}/sessions/{session_id}`

Returns full detail for a single session, including its status (`active` / `ended`) and an ordered list of **runs**.

A **run** corresponds to one LLM turn — typically triggered by a user message. Each run shows its model, tool-call count, duration, and whether any calls were blocked.

The `status` field is `ended` when a `session_end` hook event has been recorded for this session.

<h3 id="get-session-detail-with-runs-parameters">Parameters</h3>

|Name|In|Type|Required|Description|
|---|---|---|---|---|
|id|path|string|true|Compound agent ID.|
|session_id|path|string|true|Session UUID.|

> Example responses

> 200 Response

```json
{
  "session_id": "a1b2c3d4-0000-0000-0000-000000000001",
  "session_key": "announce:v1:agent:claude-code:openai-user:abc123",
  "session_type": "HTTP API",
  "status": "ended",
  "total_tool_calls": 23,
  "runs": [
    {
      "run_id": "chatcmpl_4ff08efb-c086-4927-9305-20ef7f95d329",
      "start_time": "2026-04-09T10:00:00Z",
      "end_time": "2026-04-09T10:00:18Z",
      "event_count": 14,
      "model": "claude-sonnet-4-6",
      "tool_count": 5,
      "has_block": false
    }
  ]
}
```

<h3 id="get-session-detail-with-runs-responses">Responses</h3>

|Status|Meaning|Description|Schema|
|---|---|---|---|
|200|[OK](https://tools.ietf.org/html/rfc7231#section-6.3.1)|Session detail|[SessionDetail](#schemasessiondetail)|
|404|[Not Found](https://tools.ietf.org/html/rfc7231#section-6.5.4)|Session not found.|None|

<aside class="warning">
To perform this operation, you must be authenticated by means of one of the following methods:
cookieAuth
</aside>

## Get waterfall trace for a run

<a id="opIdgetRunTrace"></a>

> Code samples

```shell
# You can also use wget
curl -X GET /api/agents/{id}/sessions/{session_id}/runs/{run_id}/trace \
  -H 'Accept: application/json'

```

```javascript

const headers = {
  'Accept':'application/json'
};

fetch('/api/agents/{id}/sessions/{session_id}/runs/{run_id}/trace',
{
  method: 'GET',

  headers: headers
})
.then(function(res) {
    return res.json();
}).then(function(body) {
    console.log(body);
});

```

```python
import requests
headers = {
  'Accept': 'application/json'
}

r = requests.get('/api/agents/{id}/sessions/{session_id}/runs/{run_id}/trace', headers = headers)

print(r.json())

```

`GET /agents/{id}/sessions/{session_id}/runs/{run_id}/trace`

Builds a waterfall trace from raw `agentguard_hook_events` — no OTel dependency required.

Each hook event becomes a **child span** whose duration is the gap to the next event. The root span covers the full run window. Span kinds:
- `run` — root span for the entire LLM turn
- `llm` — events related to model inference (`llm_input`, `llm_output`, `before_model_resolve`, etc.)
- `tool` — tool-call lifecycle (`before_tool_call`, `after_tool_call`, `tool_result_persist`)
- `lifecycle` — all other hook events

Span status is `blocked` when `hook_decision = block`, `error` when `event_data.error` is set, otherwise `ok`.

<h3 id="get-waterfall-trace-for-a-run-parameters">Parameters</h3>

|Name|In|Type|Required|Description|
|---|---|---|---|---|
|id|path|string|true|Compound agent ID.|
|session_id|path|string|true|Session UUID.|
|run_id|path|string|true|Run ID. Supports multiple formats: `chatcmpl_{uuid}` (HTTP API), `announce:v1:agent:...:run:{uuid}` (sub-agent), or plain UUID.|

> Example responses

> 200 Response

```json
{
  "spans": [
    {
      "span_id": "chatcmpl_4ff08efb",
      "parent_id": "",
      "kind": "run",
      "name": "run",
      "start_ms": 1744200000000,
      "duration_ms": 18400,
      "status": "ok",
      "attributes": {}
    },
    {
      "span_id": "evt-0",
      "parent_id": "chatcmpl_4ff08efb",
      "kind": "llm",
      "name": "llm_input",
      "start_ms": 1744200000000,
      "duration_ms": 120,
      "status": "ok",
      "attributes": {
        "model": "claude-sonnet-4-6"
      }
    },
    {
      "span_id": "evt-2",
      "parent_id": "chatcmpl_4ff08efb",
      "kind": "tool",
      "name": "before_tool_call",
      "start_ms": 1744200000800,
      "duration_ms": 450,
      "status": "ok",
      "attributes": {
        "tool_name": "Read"
      }
    }
  ]
}
```

<h3 id="get-waterfall-trace-for-a-run-responses">Responses</h3>

|Status|Meaning|Description|Schema|
|---|---|---|---|
|200|[OK](https://tools.ietf.org/html/rfc7231#section-6.3.1)|Waterfall trace spans|[TraceResponse](#schematraceresponse)|
|404|[Not Found](https://tools.ietf.org/html/rfc7231#section-6.5.4)|No events found for this run.|None|

<aside class="warning">
To perform this operation, you must be authenticated by means of one of the following methods:
cookieAuth
</aside>

<h1 id="agentguard-api-metrics">Metrics</h1>

Aggregated LLM token usage and tool-call statistics.

## Get LLM token usage metrics

<a id="opIdgetLLMMetrics"></a>

> Code samples

```shell
# You can also use wget
curl -X GET /api/metrics/llm \
  -H 'Accept: application/json'

```

```javascript

const headers = {
  'Accept':'application/json'
};

fetch('/api/metrics/llm',
{
  method: 'GET',

  headers: headers
})
.then(function(res) {
    return res.json();
}).then(function(body) {
    console.log(body);
});

```

```python
import requests
headers = {
  'Accept': 'application/json'
}

r = requests.get('/api/metrics/llm', headers = headers)

print(r.json())

```

`GET /metrics/llm`

Returns aggregated LLM token consumption broken down by model and provider. Includes total calls, input tokens, output tokens, and cache-read tokens.

Data is read from `agentguard_hook_events` where `hook_name = 'llm_output'` and token counts are stored in the `event_data` JSON field.

<h3 id="get-llm-token-usage-metrics-parameters">Parameters</h3>

|Name|In|Type|Required|Description|
|---|---|---|---|---|
|since|query|string(date-time)|false|RFC 3339 lower bound.|
|until|query|string(date-time)|false|RFC 3339 upper bound.|

> Example responses

> 200 Response

```json
{
  "period": {
    "since": "2026-04-01T00:00:00Z",
    "until": "2026-04-09T23:59:59Z"
  },
  "filters": {},
  "tokens": {
    "input": 820000,
    "output": 410000,
    "total": 1280000,
    "cache_read": 50000,
    "cache_write": 0
  },
  "calls": {
    "total": 412,
    "success": 410,
    "failed": 2,
    "success_rate": 0.9951
  },
  "stop_reasons": {
    "end_turn": 408,
    "max_tokens": 4
  },
  "latency_ms": {
    "p50": 2100,
    "p95": 8400,
    "p99": 14200,
    "avg": 2850,
    "min": 320,
    "max": 18400,
    "samples": 412
  },
  "throughput": [
    {
      "bucket": "2026-04-09T10:00:00Z",
      "llm_calls": 12,
      "input_tokens": 24000,
      "output_tokens": 12000,
      "total_tokens": 36000
    }
  ]
}
```

<h3 id="get-llm-token-usage-metrics-responses">Responses</h3>

|Status|Meaning|Description|Schema|
|---|---|---|---|
|200|[OK](https://tools.ietf.org/html/rfc7231#section-6.3.1)|LLM token usage metrics|[LLMMetrics](#schemallmmetrics)|

<aside class="warning">
To perform this operation, you must be authenticated by means of one of the following methods:
cookieAuth
</aside>

## Get tool usage metrics

<a id="opIdgetToolMetrics"></a>

> Code samples

```shell
# You can also use wget
curl -X GET /api/metrics/tools \
  -H 'Accept: application/json'

```

```javascript

const headers = {
  'Accept':'application/json'
};

fetch('/api/metrics/tools',
{
  method: 'GET',

  headers: headers
})
.then(function(res) {
    return res.json();
}).then(function(body) {
    console.log(body);
});

```

```python
import requests
headers = {
  'Accept': 'application/json'
}

r = requests.get('/api/metrics/tools', headers = headers)

print(r.json())

```

`GET /metrics/tools`

Returns tool-call counts aggregated by tool name, including the number of calls that were blocked by a security rule.

Derived from `agentguard_hook_events` where `hook_name = 'before_tool_call'`.

<h3 id="get-tool-usage-metrics-parameters">Parameters</h3>

|Name|In|Type|Required|Description|
|---|---|---|---|---|
|since|query|string(date-time)|false|RFC 3339 lower bound.|
|until|query|string(date-time)|false|RFC 3339 upper bound.|

> Example responses

> 200 Response

```json
{
  "period": {
    "since": "2026-04-01T00:00:00Z"
  },
  "filters": {},
  "summary": {
    "total_calls": 1840,
    "success_calls": 1830,
    "failed_calls": 10,
    "success_rate": 0.9946
  },
  "by_tool": [
    {
      "tool_name": "Read",
      "total_calls": 820,
      "success_calls": 820,
      "failed_calls": 0,
      "success_rate": 1,
      "latency_ms": {
        "p50": 45,
        "p95": 120,
        "p99": 280,
        "avg": 58,
        "min": 10,
        "max": 840,
        "samples": 820
      }
    },
    {
      "tool_name": "Bash",
      "total_calls": 310,
      "success_calls": 302,
      "failed_calls": 8,
      "success_rate": 0.9742,
      "latency_ms": {
        "p50": 380,
        "p95": 2100,
        "p99": 4800,
        "avg": 620,
        "min": 80,
        "max": 8200,
        "samples": 310
      }
    }
  ]
}
```

<h3 id="get-tool-usage-metrics-responses">Responses</h3>

|Status|Meaning|Description|Schema|
|---|---|---|---|
|200|[OK](https://tools.ietf.org/html/rfc7231#section-6.3.1)|Tool usage metrics|[ToolMetrics](#schematoolmetrics)|

<aside class="warning">
To perform this operation, you must be authenticated by means of one of the following methods:
cookieAuth
</aside>

<h1 id="agentguard-api-threats">Threats</h1>

Security detections emitted by active rule materialized views.

## List security threats

<a id="opIdlistThreats"></a>

> Code samples

```shell
# You can also use wget
curl -X GET /api/threats \
  -H 'Accept: application/json'

```

```javascript

const headers = {
  'Accept':'application/json'
};

fetch('/api/threats',
{
  method: 'GET',

  headers: headers
})
.then(function(res) {
    return res.json();
}).then(function(body) {
    console.log(body);
});

```

```python
import requests
headers = {
  'Accept': 'application/json'
}

r = requests.get('/api/threats', headers = headers)

print(r.json())

```

`GET /threats`

Returns detections written to `agentguard_security_events` by active rule materialized views, ordered newest-first.

Each detection record includes the triggering rule, severity, affected agent, session context, and the raw hook event data that caused the match.

<h3 id="list-security-threats-parameters">Parameters</h3>

|Name|In|Type|Required|Description|
|---|---|---|---|---|
|severity|query|string|false|Filter by severity level.|
|status|query|string|false|Filter by threat lifecycle status.|
|agent_id|query|string|false|Filter to a specific agent_id.|
|page|query|integer|false|none|
|limit|query|integer|false|none|

#### Enumerated Values

|Parameter|Value|
|---|---|
|severity|critical|
|severity|warning|
|severity|info|
|status|open|
|status|acknowledged|
|status|cleared|

> Example responses

> 200 Response

```json
{
  "threats": [
    {
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
      "events": [
        {
          "detected_at": "2026-04-09T10:00:00Z",
          "run_id": "chatcmpl_4ff08efb",
          "hook_name": "llm_input",
          "tool_name": "",
          "message": "Prompt injection detected",
          "event_data": "{}"
        }
      ]
    }
  ],
  "total": 7,
  "page": 1,
  "limit": 20
}
```

<h3 id="list-security-threats-responses">Responses</h3>

|Status|Meaning|Description|Schema|
|---|---|---|---|
|200|[OK](https://tools.ietf.org/html/rfc7231#section-6.3.1)|Threat list|Inline|

<h3 id="list-security-threats-responseschema">Response Schema</h3>

Status Code **200**

|Name|Type|Required|Restrictions|Description|
|---|---|---|---|---|
|» threats|[[Threat](#schemathreat)]|false|none|[A deduplicated security threat keyed by (agent_id, deployment_id, session_id, rule_id). Includes aggregated event counts and up to 50 individual detection events. Status lifecycle: open -> acknowledged -> cleared (auto-reopens on new events).]|
|»» threat_id|string|false|none|MD5 hex string computed from (agent_id, deployment_id, session_id, rule_id). Used as the :id path parameter.|
|»» rule_id|string|false|none|none|
|»» rule_name|string|false|none|none|
|»» severity|string|false|none|none|
|»» agent_id|string|false|none|none|
|»» deployment_id|string|false|none|none|
|»» session_id|string|false|none|none|
|»» event_count|integer(int64)|false|none|Total number of security events for this threat key.|
|»» first_seen|string|false|none|Timestamp of the earliest detection (Proton datetime string).|
|»» last_seen|string|false|none|Timestamp of the most recent detection.|
|»» status|string|false|none|Lifecycle status. Reverts to open if new events arrive after acknowledged or cleared.|
|»» status_updated_at|string|false|none|When status was last changed.|
|»» events|[[ThreatEvent](#schemathreatevent)]|false|none|Up to 50 individual detection events, newest first.|
|»»» detected_at|string|false|none|Detection timestamp (Proton datetime64 string).|
|»»» run_id|string|false|none|none|
|»»» hook_name|string|false|none|none|
|»»» tool_name|string|false|none|none|
|»»» message|string|false|none|none|
|»»» event_data|string|false|none|Raw JSON payload from the triggering hook event.|
|» total|integer|false|none|none|
|» page|integer|false|none|none|
|» limit|integer|false|none|none|

#### Enumerated Values

|Property|Value|
|---|---|
|severity|critical|
|severity|warning|
|severity|info|
|status|open|
|status|acknowledged|
|status|cleared|

<aside class="warning">
To perform this operation, you must be authenticated by means of one of the following methods:
cookieAuth
</aside>

## Get threat detail

<a id="opIdgetThreat"></a>

> Code samples

```shell
# You can also use wget
curl -X GET /api/threats/{id} \
  -H 'Accept: application/json'

```

```javascript

const headers = {
  'Accept':'application/json'
};

fetch('/api/threats/{id}',
{
  method: 'GET',

  headers: headers
})
.then(function(res) {
    return res.json();
}).then(function(body) {
    console.log(body);
});

```

```python
import requests
headers = {
  'Accept': 'application/json'
}

r = requests.get('/api/threats/{id}', headers = headers)

print(r.json())

```

`GET /threats/{id}`

Returns a single deduplicated threat by its computed threat_id (MD5 of agent_id+deployment_id+session_id+rule_id). Includes up to 50 raw security events and, if a hold is currently pending for this threat, a pending_hold object with Approve/Deny context.

<h3 id="get-threat-detail-parameters">Parameters</h3>

|Name|In|Type|Required|Description|
|---|---|---|---|---|
|id|path|string|true|Threat ID — MD5 hex string computed from (agent_id, deployment_id, session_id, rule_id).|

> Example responses

> 200 Response

```json
{
  "threat_id": "string",
  "rule_id": "string",
  "rule_name": "string",
  "severity": "critical",
  "agent_id": "string",
  "deployment_id": "string",
  "session_id": "string",
  "event_count": 0,
  "first_seen": "string",
  "last_seen": "string",
  "status": "open",
  "status_updated_at": "string",
  "events": [
    {
      "detected_at": "string",
      "run_id": "string",
      "hook_name": "string",
      "tool_name": "string",
      "message": "string",
      "event_data": "string"
    }
  ],
  "pending_hold": {
    "hold_id": "string",
    "tool_name": "string",
    "args_summary": "string",
    "created_at": "2019-08-24T14:15:22Z"
  }
}
```

<h3 id="get-threat-detail-responses">Responses</h3>

|Status|Meaning|Description|Schema|
|---|---|---|---|
|200|[OK](https://tools.ietf.org/html/rfc7231#section-6.3.1)|Threat detail|[ThreatDetailResponse](#schemathreatdetailresponse)|
|404|[Not Found](https://tools.ietf.org/html/rfc7231#section-6.5.4)|Threat not found.|None|
|503|[Service Unavailable](https://tools.ietf.org/html/rfc7231#section-6.6.4)|Timeplus unavailable.|None|

<aside class="warning">
To perform this operation, you must be authenticated by means of one of the following methods:
cookieAuth
</aside>

## Acknowledge a threat

<a id="opIdacknowledgeThreat"></a>

> Code samples

```shell
# You can also use wget
curl -X POST /api/threats/{id}/acknowledge \
  -H 'Content-Type: application/json' \
  -H 'Accept: application/json'

```

```javascript
const inputBody = '{
  "agent_id": "string",
  "deployment_id": "string",
  "session_id": "string",
  "rule_id": "string"
}';
const headers = {
  'Content-Type':'application/json',
  'Accept':'application/json'
};

fetch('/api/threats/{id}/acknowledge',
{
  method: 'POST',
  body: inputBody,
  headers: headers
})
.then(function(res) {
    return res.json();
}).then(function(body) {
    console.log(body);
});

```

```python
import requests
headers = {
  'Content-Type': 'application/json',
  'Accept': 'application/json'
}

r = requests.post('/api/threats/{id}/acknowledge', headers = headers)

print(r.json())

```

`POST /threats/{id}/acknowledge`

Sets the threat status to acknowledged. The :id must be the MD5 computed from the four body fields. Acknowledged threats auto-reopen when new security events arrive.

> Body parameter

```json
{
  "agent_id": "string",
  "deployment_id": "string",
  "session_id": "string",
  "rule_id": "string"
}
```

<h3 id="acknowledge-a-threat-parameters">Parameters</h3>

|Name|In|Type|Required|Description|
|---|---|---|---|---|
|id|path|string|true|Threat ID (MD5 hex).|
|body|body|[ThreatStatusRequest](#schemathreatstatusrequest)|true|none|

> Example responses

> 200 Response

```json
{
  "ok": true,
  "status": "acknowledged"
}
```

<h3 id="acknowledge-a-threat-responses">Responses</h3>

|Status|Meaning|Description|Schema|
|---|---|---|---|
|200|[OK](https://tools.ietf.org/html/rfc7231#section-6.3.1)|Status updated.|None|
|400|[Bad Request](https://tools.ietf.org/html/rfc7231#section-6.5.1)|Missing required fields or body identity does not match :id.|None|

<h3 id="acknowledge-a-threat-responseschema">Response Schema</h3>

<aside class="warning">
To perform this operation, you must be authenticated by means of one of the following methods:
cookieAuth
</aside>

## Clear a threat

<a id="opIdclearThreat"></a>

> Code samples

```shell
# You can also use wget
curl -X POST /api/threats/{id}/clear \
  -H 'Content-Type: application/json' \
  -H 'Accept: application/json'

```

```javascript
const inputBody = '{
  "agent_id": "string",
  "deployment_id": "string",
  "session_id": "string",
  "rule_id": "string"
}';
const headers = {
  'Content-Type':'application/json',
  'Accept':'application/json'
};

fetch('/api/threats/{id}/clear',
{
  method: 'POST',
  body: inputBody,
  headers: headers
})
.then(function(res) {
    return res.json();
}).then(function(body) {
    console.log(body);
});

```

```python
import requests
headers = {
  'Content-Type': 'application/json',
  'Accept': 'application/json'
}

r = requests.post('/api/threats/{id}/clear', headers = headers)

print(r.json())

```

`POST /threats/{id}/clear`

Sets the threat status to cleared. Same request body as acknowledge. Cleared threats auto-reopen when new security events arrive.

> Body parameter

```json
{
  "agent_id": "string",
  "deployment_id": "string",
  "session_id": "string",
  "rule_id": "string"
}
```

<h3 id="clear-a-threat-parameters">Parameters</h3>

|Name|In|Type|Required|Description|
|---|---|---|---|---|
|id|path|string|true|Threat ID (MD5 hex).|
|body|body|[ThreatStatusRequest](#schemathreatstatusrequest)|true|none|

> Example responses

> 200 Response

```json
{
  "ok": true,
  "status": "cleared"
}
```

<h3 id="clear-a-threat-responses">Responses</h3>

|Status|Meaning|Description|Schema|
|---|---|---|---|
|200|[OK](https://tools.ietf.org/html/rfc7231#section-6.3.1)|Status updated.|None|
|400|[Bad Request](https://tools.ietf.org/html/rfc7231#section-6.5.1)|Missing required fields or body identity does not match :id.|None|

<h3 id="clear-a-threat-responseschema">Response Schema</h3>

<aside class="warning">
To perform this operation, you must be authenticated by means of one of the following methods:
cookieAuth
</aside>

<h1 id="agentguard-api-cost">Cost</h1>

Token-based cost estimates computed from configurable per-model pricing.

## Get cost governance summary

<a id="opIdgetCost"></a>

> Code samples

```shell
# You can also use wget
curl -X GET /api/cost \
  -H 'Accept: application/json'

```

```javascript

const headers = {
  'Accept':'application/json'
};

fetch('/api/cost',
{
  method: 'GET',

  headers: headers
})
.then(function(res) {
    return res.json();
}).then(function(body) {
    console.log(body);
});

```

```python
import requests
headers = {
  'Accept': 'application/json'
}

r = requests.get('/api/cost', headers = headers)

print(r.json())

```

`GET /cost`

Returns token-based cost estimates computed by joining `agentguard_hook_events` token counts with the `agentguard_pricing` table.

Includes a daily spend time-series and a breakdown by model. Costs are in USD. Pricing data is seeded with public list prices and can be updated via the `/cost/pricing` endpoints.

<h3 id="get-cost-governance-summary-parameters">Parameters</h3>

|Name|In|Type|Required|Description|
|---|---|---|---|---|
|since|query|string(date-time)|false|RFC 3339 lower bound for the aggregation window.|
|until|query|string(date-time)|false|RFC 3339 upper bound.|

> Example responses

> 200 Response

```json
{
  "month_to_date": 12.84,
  "forecasted": 38.52,
  "avg_token_cost": 0.00000825,
  "cache_savings": 1.2,
  "wasted_spend": 0.43,
  "daily_spend": [
    {
      "day": "2026-04-08",
      "amount": 4.2
    },
    {
      "day": "2026-04-09",
      "amount": 8.64
    }
  ],
  "token_breakdown": [
    {
      "label": "Input",
      "percentage": 64,
      "tokens": 820000,
      "amount": 8.2
    },
    {
      "label": "Output",
      "percentage": 32,
      "tokens": 410000,
      "amount": 4.1
    }
  ],
  "agent_model_costs": [
    {
      "model": "claude-sonnet-4-6",
      "agent_name": "Engineering Team",
      "cost": 11.52,
      "requests": "390",
      "provider": "anthropic",
      "agent_id": "claude-code",
      "input_tokens": 780000,
      "output_tokens": 390000,
      "cache_tokens": 48000
    }
  ]
}
```

<h3 id="get-cost-governance-summary-responses">Responses</h3>

|Status|Meaning|Description|Schema|
|---|---|---|---|
|200|[OK](https://tools.ietf.org/html/rfc7231#section-6.3.1)|Cost summary|[CostResponse](#schemacostresponse)|

<aside class="warning">
To perform this operation, you must be authenticated by means of one of the following methods:
cookieAuth
</aside>

## List model pricing entries

<a id="opIdlistPricing"></a>

> Code samples

```shell
# You can also use wget
curl -X GET /api/cost/pricing \
  -H 'Accept: application/json'

```

```javascript

const headers = {
  'Accept':'application/json'
};

fetch('/api/cost/pricing',
{
  method: 'GET',

  headers: headers
})
.then(function(res) {
    return res.json();
}).then(function(body) {
    console.log(body);
});

```

```python
import requests
headers = {
  'Accept': 'application/json'
}

r = requests.get('/api/cost/pricing', headers = headers)

print(r.json())

```

`GET /cost/pricing`

Returns all entries in the `agentguard_pricing` mutable stream. Each entry defines the cost per million tokens (input, output, cache-read) for a provider/model combination.

Default entries are seeded at startup for Anthropic and OpenAI models.

> Example responses

> 200 Response

```json
{
  "pricing": [
    {
      "provider": "anthropic",
      "model": "claude-sonnet-4-6",
      "input_cost_per_million": 3,
      "output_cost_per_million": 15,
      "cache_read_cost_per_million": 0.3
    },
    {
      "provider": "openai",
      "model": "gpt-4o",
      "input_cost_per_million": 2.5,
      "output_cost_per_million": 10,
      "cache_read_cost_per_million": 1.25
    }
  ]
}
```

<h3 id="list-model-pricing-entries-responses">Responses</h3>

|Status|Meaning|Description|Schema|
|---|---|---|---|
|200|[OK](https://tools.ietf.org/html/rfc7231#section-6.3.1)|Pricing table|Inline|

<h3 id="list-model-pricing-entries-responseschema">Response Schema</h3>

Status Code **200**

|Name|Type|Required|Restrictions|Description|
|---|---|---|---|---|
|» pricing|[[PricingEntry](#schemapricingentry)]|false|none|[Per-million-token pricing for one provider/model pair.]|
|»» provider|string|true|none|Provider name, e.g. anthropic, openai.|
|»» model|string|true|none|Model identifier as it appears in hook events.|
|»» input_cost_per_million|number|true|none|Cost in USD per 1 million input tokens.|
|»» output_cost_per_million|number|true|none|Cost in USD per 1 million output tokens.|
|»» cache_read_cost_per_million|number|false|none|Cost in USD per 1 million cache-read tokens.|

<aside class="warning">
To perform this operation, you must be authenticated by means of one of the following methods:
cookieAuth
</aside>

## Upsert a model pricing entry

<a id="opIdupsertPricing"></a>

> Code samples

```shell
# You can also use wget
curl -X POST /api/cost/pricing \
  -H 'Content-Type: application/json'

```

```javascript
const inputBody = '{
  "provider": "anthropic",
  "model": "claude-sonnet-4-6",
  "input_cost_per_million": 3,
  "output_cost_per_million": 15,
  "cache_read_cost_per_million": 0.3
}';
const headers = {
  'Content-Type':'application/json'
};

fetch('/api/cost/pricing',
{
  method: 'POST',
  body: inputBody,
  headers: headers
})
.then(function(res) {
    return res.json();
}).then(function(body) {
    console.log(body);
});

```

```python
import requests
headers = {
  'Content-Type': 'application/json'
}

r = requests.post('/api/cost/pricing', headers = headers)

print(r.json())

```

`POST /cost/pricing`

Inserts or updates a pricing entry for a provider/model pair. Uses Proton's mutable stream primary-key upsert semantics — sending the same `(provider, model)` key overwrites the previous record.

> Body parameter

```json
{
  "provider": "anthropic",
  "model": "claude-sonnet-4-6",
  "input_cost_per_million": 3,
  "output_cost_per_million": 15,
  "cache_read_cost_per_million": 0.3
}
```

<h3 id="upsert-a-model-pricing-entry-parameters">Parameters</h3>

|Name|In|Type|Required|Description|
|---|---|---|---|---|
|body|body|[PricingEntry](#schemapricingentry)|true|none|

<h3 id="upsert-a-model-pricing-entry-responses">Responses</h3>

|Status|Meaning|Description|Schema|
|---|---|---|---|
|200|[OK](https://tools.ietf.org/html/rfc7231#section-6.3.1)|Entry upserted successfully.|None|
|400|[Bad Request](https://tools.ietf.org/html/rfc7231#section-6.5.1)|Missing or invalid fields.|None|

<aside class="warning">
To perform this operation, you must be authenticated by means of one of the following methods:
cookieAuth
</aside>

## Delete a model pricing entry

<a id="opIddeletePricing"></a>

> Code samples

```shell
# You can also use wget
curl -X DELETE /api/cost/pricing/{provider}/{model}

```

```javascript

fetch('/api/cost/pricing/{provider}/{model}',
{
  method: 'DELETE'

})
.then(function(res) {
    return res.json();
}).then(function(body) {
    console.log(body);
});

```

```python
import requests

r = requests.delete('/api/cost/pricing/{provider}/{model}')

print(r.json())

```

`DELETE /cost/pricing/{provider}/{model}`

Removes the pricing record for the given provider/model pair from `agentguard_pricing`.

<h3 id="delete-a-model-pricing-entry-parameters">Parameters</h3>

|Name|In|Type|Required|Description|
|---|---|---|---|---|
|provider|path|string|true|Provider name, e.g. `anthropic` or `openai`.|
|model|path|string|true|Model name, e.g. `claude-sonnet-4-6`.|

<h3 id="delete-a-model-pricing-entry-responses">Responses</h3>

|Status|Meaning|Description|Schema|
|---|---|---|---|
|200|[OK](https://tools.ietf.org/html/rfc7231#section-6.3.1)|Deleted.|None|
|404|[Not Found](https://tools.ietf.org/html/rfc7231#section-6.5.4)|Entry not found.|None|

<aside class="warning">
To perform this operation, you must be authenticated by means of one of the following methods:
cookieAuth
</aside>

<h1 id="agentguard-api-rules">Rules</h1>

Security rule packs. Each enabled rule has a corresponding Proton materialized view that writes matches to agentguard_security_events.

## List installed rules

<a id="opIdlistRules"></a>

> Code samples

```shell
# You can also use wget
curl -X GET /api/rules \
  -H 'Accept: application/json'

```

```javascript

const headers = {
  'Accept':'application/json'
};

fetch('/api/rules',
{
  method: 'GET',

  headers: headers
})
.then(function(res) {
    return res.json();
}).then(function(body) {
    console.log(body);
});

```

```python
import requests
headers = {
  'Accept': 'application/json'
}

r = requests.get('/api/rules', headers = headers)

print(r.json())

```

`GET /rules`

Returns all non-deleted rules from the `agentguard_rules` append-only snapshot stream. Current state is reconstructed via `arg_max(field, revision) GROUP BY rule_id` (ordered by the per-rule monotonic `revision`, not `updated_at`).

> **Note:** this reference is generated; the canonical, up-to-date OpenAPI spec is served at `/docs/openapi.json` (source: `backend/internal/handlers/docs.go`). For the editable-rule-parameters endpoints — `PUT /api/rules/{id}/configuration`, install-with-overrides, the bodyless `POST /api/rules/{id}/upgrade`, and the `RuleInstance` read model — consult that spec or `docs/design.md` §5.1 until this file is regenerated.

> Example responses

> 200 Response

```json
{
  "rule_packs": [
    {
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
    }
  ],
  "total_detections_24h": 3,
  "active_rule_density": 75,
  "recent_detections": [
    {
      "rule_id": "rp-001",
      "rule_name": "Prompt Injection Shield",
      "severity": "critical",
      "agent_id": "claude-code",
      "hook_name": "llm_input",
      "message": "Injection detected",
      "detected_at": "2026-04-09T10:00:00Z"
    }
  ]
}
```

<h3 id="list-installed-rules-responses">Responses</h3>

|Status|Meaning|Description|Schema|
|---|---|---|---|
|200|[OK](https://tools.ietf.org/html/rfc7231#section-6.3.1)|Installed rules with detection summary|[RulesResponse](#schemarulesresponse)|

<aside class="warning">
To perform this operation, you must be authenticated by means of one of the following methods:
cookieAuth
</aside>

## Create a custom rule

<a id="opIdcreateRule"></a>

> Code samples

```shell
# You can also use wget
curl -X POST /api/rules \
  -H 'Content-Type: application/json' \
  -H 'Accept: application/json'

```

```javascript
const inputBody = '{
  "id": "string",
  "name": "string",
  "description": "string",
  "query_sql": "string",
  "severity": "critical",
  "category": "string",
  "fp_rate": "low",
  "hook_sources": "string"
}';
const headers = {
  'Content-Type':'application/json',
  'Accept':'application/json'
};

fetch('/api/rules',
{
  method: 'POST',
  body: inputBody,
  headers: headers
})
.then(function(res) {
    return res.json();
}).then(function(body) {
    console.log(body);
});

```

```python
import requests
headers = {
  'Content-Type': 'application/json',
  'Accept': 'application/json'
}

r = requests.post('/api/rules', headers = headers)

print(r.json())

```

`POST /rules`

Creates a new custom rule with a user-supplied SQL query and saves it to the local catalog directory.

An optional `id` can be specified (letters, digits, hyphens, underscores; max 64 chars); if omitted a `custom-` prefixed ID is auto-generated. The ID must be unique across the catalog.

The `query_sql` must be a valid Proton `SELECT` that queries `agentguard_hook_events` and outputs columns matching `agentguard_security_events`: `detected_at`, `rule_id`, `rule_name`, `severity`, `agent_id`, `deployment_id`, `session_id`, `run_id`, `hook_name`, `tool_name`, `message`, `event_data`.

> Body parameter

```json
{
  "id": "string",
  "name": "string",
  "description": "string",
  "query_sql": "string",
  "severity": "critical",
  "category": "string",
  "fp_rate": "low",
  "hook_sources": "string"
}
```

<h3 id="create-a-custom-rule-parameters">Parameters</h3>

|Name|In|Type|Required|Description|
|---|---|---|---|---|
|body|body|[RuleInput](#schemaruleinput)|true|none|

> Example responses

> 201 Response

```json
{
  "id": "custom-a1b2c3",
  "name": "Exfil Watch",
  "enabled": false
}
```

<h3 id="create-a-custom-rule-responses">Responses</h3>

|Status|Meaning|Description|Schema|
|---|---|---|---|
|201|[Created](https://tools.ietf.org/html/rfc7231#section-6.3.2)|Rule created and MV started.|None|
|400|[Bad Request](https://tools.ietf.org/html/rfc7231#section-6.5.1)|Invalid or missing fields.|None|

<h3 id="create-a-custom-rule-responseschema">Response Schema</h3>

<aside class="warning">
To perform this operation, you must be authenticated by means of one of the following methods:
cookieAuth
</aside>

## Materialized view health status

<a id="opIdgetMVStatus"></a>

> Code samples

```shell
# You can also use wget
curl -X GET /api/rules/mv-status \
  -H 'Accept: application/json'

```

```javascript

const headers = {
  'Accept':'application/json'
};

fetch('/api/rules/mv-status',
{
  method: 'GET',

  headers: headers
})
.then(function(res) {
    return res.json();
}).then(function(body) {
    console.log(body);
});

```

```python
import requests
headers = {
  'Accept': 'application/json'
}

r = requests.get('/api/rules/mv-status', headers = headers)

print(r.json())

```

`GET /rules/mv-status`

Returns the current status of each rule's Proton materialized view (`mv_rule_{id}`). Useful for diagnosing rules that are marked enabled but whose MV is paused, missing, or in an error state.

> Example responses

> 200 Response

```json
[
  {
    "rule_id": "rp-001",
    "mv_name": "mv_rule_rp_001",
    "status": "active"
  },
  {
    "rule_id": "rp-003",
    "mv_name": "mv_rule_rp_003",
    "status": "paused"
  }
]
```

<h3 id="materialized-view-health-status-responses">Responses</h3>

|Status|Meaning|Description|Schema|
|---|---|---|---|
|200|[OK](https://tools.ietf.org/html/rfc7231#section-6.3.1)|MV status per rule|Inline|

<h3 id="materialized-view-health-status-responseschema">Response Schema</h3>

Status Code **200**

|Name|Type|Required|Restrictions|Description|
|---|---|---|---|---|
|» rule_id|string|false|none|Rule identifier.|
|» mv_name|string|false|none|Proton MV name, e.g. `mv_rule_rp_001`.|
|» status|string|false|none|MV state reported by Proton: `active`, `paused`, or `missing`.|

<aside class="warning">
To perform this operation, you must be authenticated by means of one of the following methods:
cookieAuth
</aside>

## Reconcile MV state

<a id="opIdsyncMVs"></a>

> Code samples

```shell
# You can also use wget
curl -X POST /api/rules/sync \
  -H 'Accept: application/json'

```

```javascript

const headers = {
  'Accept':'application/json'
};

fetch('/api/rules/sync',
{
  method: 'POST',

  headers: headers
})
.then(function(res) {
    return res.json();
}).then(function(body) {
    console.log(body);
});

```

```python
import requests
headers = {
  'Accept': 'application/json'
}

r = requests.post('/api/rules/sync', headers = headers)

print(r.json())

```

`POST /rules/sync`

Re-reads all rules from `agentguard_rules` and reconciles each MV: creates missing MVs for enabled rules, resumes paused MVs, and pauses MVs for disabled rules. Equivalent to what happens at server startup, but callable at runtime without a restart.

> Example responses

> 200 Response

```json
{
  "synced": 4,
  "created": 0,
  "resumed": 1,
  "paused": 0
}
```

<h3 id="reconcile-mv-state-responses">Responses</h3>

|Status|Meaning|Description|Schema|
|---|---|---|---|
|200|[OK](https://tools.ietf.org/html/rfc7231#section-6.3.1)|Sync complete.|None|

<h3 id="reconcile-mv-state-responseschema">Response Schema</h3>

<aside class="warning">
To perform this operation, you must be authenticated by means of one of the following methods:
cookieAuth
</aside>

## Get rule by ID

<a id="opIdgetRule"></a>

> Code samples

```shell
# You can also use wget
curl -X GET /api/rules/{id} \
  -H 'Accept: application/json'

```

```javascript

const headers = {
  'Accept':'application/json'
};

fetch('/api/rules/{id}',
{
  method: 'GET',

  headers: headers
})
.then(function(res) {
    return res.json();
}).then(function(body) {
    console.log(body);
});

```

```python
import requests
headers = {
  'Accept': 'application/json'
}

r = requests.get('/api/rules/{id}', headers = headers)

print(r.json())

```

`GET /rules/{id}`

Returns the current state of a single rule. The current state is resolved from the append-only `agentguard_rules` stream via `arg_max(updated_at) GROUP BY rule_id`.

<h3 id="get-rule-by-id-parameters">Parameters</h3>

|Name|In|Type|Required|Description|
|---|---|---|---|---|
|id|path|string|true|Rule ID, e.g. `rp-001` or `custom-a1b2c3`.|

> Example responses

> 200 Response

```json
{
  "id": "rp-001",
  "name": "Prompt Injection Shield",
  "version": "1.0.0",
  "description": "string",
  "query_sql": "string",
  "enabled": true,
  "severity": "critical",
  "category": "string",
  "required_telemetry": "string",
  "fp_rate": "low",
  "hook_sources": "string",
  "block_policy": "log_only",
  "detections_24h": 0,
  "latest_version": "string",
  "recent_detections": [
    {
      "rule_id": "string",
      "rule_name": "string",
      "severity": "critical",
      "agent_id": "string",
      "hook_name": "string",
      "message": "string",
      "detected_at": "string"
    }
  ]
}
```

<h3 id="get-rule-by-id-responses">Responses</h3>

|Status|Meaning|Description|Schema|
|---|---|---|---|
|200|[OK](https://tools.ietf.org/html/rfc7231#section-6.3.1)|Rule detail with recent detections|[RuleDetailResponse](#schemaruledetailresponse)|
|404|[Not Found](https://tools.ietf.org/html/rfc7231#section-6.5.4)|Rule not found or deleted.|None|

<aside class="warning">
To perform this operation, you must be authenticated by means of one of the following methods:
cookieAuth
</aside>

## Uninstall a rule

<a id="opIduninstallRule"></a>

> Code samples

```shell
# You can also use wget
curl -X DELETE /api/rules/{id} \
  -H 'Accept: application/json'

```

```javascript

const headers = {
  'Accept':'application/json'
};

fetch('/api/rules/{id}',
{
  method: 'DELETE',

  headers: headers
})
.then(function(res) {
    return res.json();
}).then(function(body) {
    console.log(body);
});

```

```python
import requests
headers = {
  'Accept': 'application/json'
}

r = requests.delete('/api/rules/{id}', headers = headers)

print(r.json())

```

`DELETE /rules/{id}`

Soft-deletes a rule by appending a `deleted=true` record to `agentguard_rules`, then drops the corresponding materialized view.

The rule remains in the stream history and will appear in the catalog as a reinstallable entry (for custom rules) or as `installed: false` (for builtin rules).

<h3 id="uninstall-a-rule-parameters">Parameters</h3>

|Name|In|Type|Required|Description|
|---|---|---|---|---|
|id|path|string|true|Rule ID.|

> Example responses

> 200 Response

```json
{
  "deleted": true,
  "rule_id": "rp-001"
}
```

<h3 id="uninstall-a-rule-responses">Responses</h3>

|Status|Meaning|Description|Schema|
|---|---|---|---|
|200|[OK](https://tools.ietf.org/html/rfc7231#section-6.3.1)|Rule uninstalled.|None|
|404|[Not Found](https://tools.ietf.org/html/rfc7231#section-6.5.4)|Rule not found.|None|

<h3 id="uninstall-a-rule-responseschema">Response Schema</h3>

<aside class="warning">
To perform this operation, you must be authenticated by means of one of the following methods:
cookieAuth
</aside>

## Install a rule from the catalog

<a id="opIdinstallRule"></a>

> Code samples

```shell
# You can also use wget
curl -X POST /api/rules/{id}/install \
  -H 'Accept: application/json'

```

```javascript

const headers = {
  'Accept':'application/json'
};

fetch('/api/rules/{id}/install',
{
  method: 'POST',

  headers: headers
})
.then(function(res) {
    return res.json();
}).then(function(body) {
    console.log(body);
});

```

```python
import requests
headers = {
  'Accept': 'application/json'
}

r = requests.post('/api/rules/{id}/install', headers = headers)

print(r.json())

```

`POST /rules/{id}/install`

Installs a builtin rule (prefixed `rp-`) from the hardcoded catalog, or reinstalls a previously deleted custom rule from its last-known stream state.

Installing a rule does **not** automatically enable it — call `/rules/{id}/toggle` afterwards to start the MV.

<h3 id="install-a-rule-from-the-catalog-parameters">Parameters</h3>

|Name|In|Type|Required|Description|
|---|---|---|---|---|
|id|path|string|true|Catalog rule ID, e.g. `rp-002`.|

> Example responses

> 200 Response

```json
{
  "installed": true,
  "rule_id": "rp-002"
}
```

<h3 id="install-a-rule-from-the-catalog-responses">Responses</h3>

|Status|Meaning|Description|Schema|
|---|---|---|---|
|200|[OK](https://tools.ietf.org/html/rfc7231#section-6.3.1)|Installed.|None|
|404|[Not Found](https://tools.ietf.org/html/rfc7231#section-6.5.4)|Rule not in catalog.|None|
|409|[Conflict](https://tools.ietf.org/html/rfc7231#section-6.5.8)|Rule is already installed.|None|

<h3 id="install-a-rule-from-the-catalog-responseschema">Response Schema</h3>

<aside class="warning">
To perform this operation, you must be authenticated by means of one of the following methods:
cookieAuth
</aside>

## Toggle rule enabled/disabled

<a id="opIdtoggleRule"></a>

> Code samples

```shell
# You can also use wget
curl -X POST /api/rules/{id}/toggle \
  -H 'Accept: application/json'

```

```javascript

const headers = {
  'Accept':'application/json'
};

fetch('/api/rules/{id}/toggle',
{
  method: 'POST',

  headers: headers
})
.then(function(res) {
    return res.json();
}).then(function(body) {
    console.log(body);
});

```

```python
import requests
headers = {
  'Accept': 'application/json'
}

r = requests.post('/api/rules/{id}/toggle', headers = headers)

print(r.json())

```

`POST /rules/{id}/toggle`

Flips the `enabled` flag of a rule and atomically pauses or resumes its Proton materialized view.

- Enabling a rule that has no MV will **create** the MV.
- Disabling a rule will **pause** its MV (events stop being matched but the MV is not dropped).

<h3 id="toggle-rule-enabled/disabled-parameters">Parameters</h3>

|Name|In|Type|Required|Description|
|---|---|---|---|---|
|id|path|string|true|Rule ID.|

> Example responses

> 200 Response

```json
{
  "id": "rp-001",
  "name": "Prompt Injection Shield",
  "enabled": false,
  "updated_at": "2026-04-09T15:00:00Z"
}
```

<h3 id="toggle-rule-enabled/disabled-responses">Responses</h3>

|Status|Meaning|Description|Schema|
|---|---|---|---|
|200|[OK](https://tools.ietf.org/html/rfc7231#section-6.3.1)|Updated rule state.|[RulePack](#schemarulepack)|
|404|[Not Found](https://tools.ietf.org/html/rfc7231#section-6.5.4)|Rule not found.|None|

<aside class="warning">
To perform this operation, you must be authenticated by means of one of the following methods:
cookieAuth
</aside>

## Upgrade rule SQL and rebuild its MV

<a id="opIdupgradeRule"></a>

> Code samples

```shell
# You can also use wget
curl -X POST /api/rules/{id}/upgrade \
  -H 'Content-Type: application/json' \
  -H 'Accept: application/json'

```

```javascript
const inputBody = '{
  "query_sql": "string"
}';
const headers = {
  'Content-Type':'application/json',
  'Accept':'application/json'
};

fetch('/api/rules/{id}/upgrade',
{
  method: 'POST',
  body: inputBody,
  headers: headers
})
.then(function(res) {
    return res.json();
}).then(function(body) {
    console.log(body);
});

```

```python
import requests
headers = {
  'Content-Type': 'application/json',
  'Accept': 'application/json'
}

r = requests.post('/api/rules/{id}/upgrade', headers = headers)

print(r.json())

```

`POST /rules/{id}/upgrade`

Replaces the `query_sql` of an existing rule with a new version, then drops and recreates the MV with the new query. The rule's `enabled` state is preserved.

> Body parameter

```json
{
  "query_sql": "string"
}
```

<h3 id="upgrade-rule-sql-and-rebuild-its-mv-parameters">Parameters</h3>

|Name|In|Type|Required|Description|
|---|---|---|---|---|
|id|path|string|true|Rule ID.|
|body|body|object|true|none|
|» query_sql|body|string|true|New SELECT statement targeting agentguard_hook_events.|

> Example responses

> 200 Response

```json
{
  "upgraded": true,
  "rule_id": "rp-003"
}
```

<h3 id="upgrade-rule-sql-and-rebuild-its-mv-responses">Responses</h3>

|Status|Meaning|Description|Schema|
|---|---|---|---|
|200|[OK](https://tools.ietf.org/html/rfc7231#section-6.3.1)|Rule upgraded, MV rebuilt.|None|
|404|[Not Found](https://tools.ietf.org/html/rfc7231#section-6.5.4)|Rule not found.|None|

<h3 id="upgrade-rule-sql-and-rebuild-its-mv-responseschema">Response Schema</h3>

<aside class="warning">
To perform this operation, you must be authenticated by means of one of the following methods:
cookieAuth
</aside>

## Set rule block policy

<a id="opIdsetRuleBlockPolicy"></a>

> Code samples

```shell
# You can also use wget
curl -X POST /api/rules/{id}/policy \
  -H 'Content-Type: application/json' \
  -H 'Accept: application/json'

```

```javascript
const inputBody = '{
  "block_policy": "log_only"
}';
const headers = {
  'Content-Type':'application/json',
  'Accept':'application/json'
};

fetch('/api/rules/{id}/policy',
{
  method: 'POST',
  body: inputBody,
  headers: headers
})
.then(function(res) {
    return res.json();
}).then(function(body) {
    console.log(body);
});

```

```python
import requests
headers = {
  'Content-Type': 'application/json',
  'Accept': 'application/json'
}

r = requests.post('/api/rules/{id}/policy', headers = headers)

print(r.json())

```

`POST /rules/{id}/policy`

Sets the enforcement mode for a rule without toggling its enabled state. The block_policy controls what happens when the rule matches a tool call via the POST /holds synchronous path:

- `log_only` — detect and log only; never create a hold or block the tool call.
- `hold` — pause the tool call for human review; apply fail-policy on timeout.
- `auto_block` — immediately block the tool call without human review.

Switching to `log_only` automatically abandons any pending holds for this rule so in-flight requests are not left waiting.

> Body parameter

```json
{
  "block_policy": "log_only"
}
```

<h3 id="set-rule-block-policy-parameters">Parameters</h3>

|Name|In|Type|Required|Description|
|---|---|---|---|---|
|id|path|string|true|Rule ID.|
|body|body|object|true|none|
|» block_policy|body|string|true|Enforcement mode for this rule.|

#### Enumerated Values

|Parameter|Value|
|---|---|
|» block_policy|log_only|
|» block_policy|hold|
|» block_policy|auto_block|

> Example responses

> 200 Response

```json
{
  "id": "rp-001",
  "name": "Prompt Injection Shield",
  "version": "1.0.0",
  "description": "string",
  "query_sql": "string",
  "enabled": true,
  "severity": "critical",
  "category": "string",
  "required_telemetry": "string",
  "fp_rate": "low",
  "hook_sources": "string",
  "block_policy": "log_only",
  "detections_24h": 0,
  "latest_version": "string"
}
```

<h3 id="set-rule-block-policy-responses">Responses</h3>

|Status|Meaning|Description|Schema|
|---|---|---|---|
|200|[OK](https://tools.ietf.org/html/rfc7231#section-6.3.1)|Updated rule state.|[RulePack](#schemarulepack)|
|400|[Bad Request](https://tools.ietf.org/html/rfc7231#section-6.5.1)|block_policy must be log_only|hold|auto_block.|None|
|404|[Not Found](https://tools.ietf.org/html/rfc7231#section-6.5.4)|Rule not found.|None|

<aside class="warning">
To perform this operation, you must be authenticated by means of one of the following methods:
cookieAuth
</aside>

<h1 id="agentguard-api-catalog">Catalog</h1>

File-based rule catalog. The initial catalog ships embedded in the binary as YAML files; users can override entries or add new ones via import. Proton tracks *install state*; the catalog tracks *definitions*.

## List rule catalog

<a id="opIdgetRulesCatalog"></a>

> Code samples

```shell
# You can also use wget
curl -X GET /api/rules/catalog \
  -H 'Accept: application/json'

```

```javascript

const headers = {
  'Accept':'application/json'
};

fetch('/api/rules/catalog',
{
  method: 'GET',

  headers: headers
})
.then(function(res) {
    return res.json();
}).then(function(body) {
    console.log(body);
});

```

```python
import requests
headers = {
  'Accept': 'application/json'
}

r = requests.get('/api/rules/catalog', headers = headers)

print(r.json())

```

`GET /rules/catalog`

Returns all rule definitions from the file-based catalog (embedded initial rules merged with any local overrides), each annotated with install state from `agentguard_rules`.

Rules installed in Proton but absent from the catalog are included with `tags: ["orphaned"]`.

Use this endpoint to power an install/uninstall UI — show the catalog, highlight installed entries, and call `/rules/{id}/install` or `DELETE /rules/{id}` accordingly. When `upgradeable` is true the catalog has a newer version than what is installed — call `/rules/{id}/upgrade` to update.

> Example responses

> 200 Response

```json
{
  "catalog": [
    {
      "id": "rp-001",
      "name": "Prompt Injection Shield",
      "version": "1.0.0",
      "description": "Detects prompt injection attacks.",
      "severity": "critical",
      "category": "prompt_injection",
      "fp_rate": "medium",
      "hook_sources": "llm_input",
      "author": "Timeplus",
      "tags": [
        "security",
        "prompt-injection"
      ],
      "installed": true,
      "enabled": true,
      "installed_version": "1.0.0",
      "upgradeable": false
    },
    {
      "id": "rp-002",
      "name": "DLP Sentinel",
      "version": "1.0.0",
      "description": "Prevents PII and secret leakage.",
      "severity": "critical",
      "category": "dlp",
      "fp_rate": "low",
      "hook_sources": "tool_result_persist,after_tool_call,llm_output",
      "author": "Timeplus",
      "tags": [
        "security",
        "dlp"
      ],
      "installed": false,
      "enabled": false,
      "upgradeable": false
    }
  ]
}
```

<h3 id="list-rule-catalog-responses">Responses</h3>

|Status|Meaning|Description|Schema|
|---|---|---|---|
|200|[OK](https://tools.ietf.org/html/rfc7231#section-6.3.1)|Rule catalog with installed status|Inline|

<h3 id="list-rule-catalog-responseschema">Response Schema</h3>

Status Code **200**

|Name|Type|Required|Restrictions|Description|
|---|---|---|---|---|
|» catalog|[[CatalogItem](#schemacatalogitem)]|false|none|[A rule definition from the file-based catalog, annotated with its current install state in Proton.]|
|»» id|string|false|none|Rule identifier, e.g. rp-001 or custom-my-rule.|
|»» name|string|false|none|none|
|»» version|string|false|none|Catalog version (semver X.Y.Z).|
|»» description|string|false|none|none|
|»» severity|string|false|none|none|
|»» category|string|false|none|Rule category, e.g. prompt_injection, dlp, privilege, supply_chain.|
|»» fp_rate|string|false|none|Expected false-positive rate.|
|»» hook_sources|string|false|none|Comma-separated hook names this rule inspects.|
|»» required_telemetry|string|false|none|Optional telemetry prerequisite note.|
|»» author|string|false|none|Rule author, e.g. Timeplus.|
|»» tags|[string]|false|none|Labels for filtering. Orphaned rules (installed but absent from catalog) carry the 'orphaned' tag.|
|»» installed|boolean|false|none|True when this rule exists in agentguard_rules and is not deleted.|
|»» enabled|boolean|false|none|True when the rule is installed and its MV is active.|
|»» installed_version|string|false|none|Version string of the installed rule (omitted when not installed).|
|»» upgradeable|boolean|false|none|True when the catalog version is newer than the installed version — call /rules/{id}/upgrade to update.|

#### Enumerated Values

|Property|Value|
|---|---|
|severity|critical|
|severity|warning|
|severity|info|
|fp_rate|low|
|fp_rate|medium|
|fp_rate|high|

<aside class="warning">
To perform this operation, you must be authenticated by means of one of the following methods:
cookieAuth
</aside>

## Import catalog rules from a zip archive

<a id="opIdimportCatalog"></a>

> Code samples

```shell
# You can also use wget
curl -X POST /api/rules/catalog/import \
  -H 'Content-Type: multipart/form-data' \
  -H 'Accept: application/json'

```

```javascript
const inputBody = '{
  "file": "string"
}';
const headers = {
  'Content-Type':'multipart/form-data',
  'Accept':'application/json'
};

fetch('/api/rules/catalog/import',
{
  method: 'POST',
  body: inputBody,
  headers: headers
})
.then(function(res) {
    return res.json();
}).then(function(body) {
    console.log(body);
});

```

```python
import requests
headers = {
  'Content-Type': 'multipart/form-data',
  'Accept': 'application/json'
}

r = requests.post('/api/rules/catalog/import', headers = headers)

print(r.json())

```

`POST /rules/catalog/import`

Uploads a zip file containing YAML rule definition files and merges them into the local catalog directory.

Merge strategy (per rule ID):
- **New ID** — always added.
- **Newer version** — overwrites the existing entry.
- **Same or older version** — skipped (pass `?force=true` to overwrite regardless).

No rules are auto-installed after import — call `/rules/{id}/install` explicitly.

> Body parameter

```yaml
file: string

```

<h3 id="import-catalog-rules-from-a-zip-archive-parameters">Parameters</h3>

|Name|In|Type|Required|Description|
|---|---|---|---|---|
|force|query|boolean|false|If true, overwrite existing entries even when the imported version is not newer.|
|body|body|object|true|none|
|» file|body|string(binary)|true|Zip archive containing one or more YAML rule files (one rule per file, named {id}.yaml).|

> Example responses

> 200 Response

```json
{
  "added": [
    "custom-my-rule"
  ],
  "updated": [
    "rp-001"
  ],
  "skipped": [
    "rp-002"
  ],
  "errors": []
}
```

<h3 id="import-catalog-rules-from-a-zip-archive-responses">Responses</h3>

|Status|Meaning|Description|Schema|
|---|---|---|---|
|200|[OK](https://tools.ietf.org/html/rfc7231#section-6.3.1)|Import summary|[ImportResult](#schemaimportresult)|
|400|[Bad Request](https://tools.ietf.org/html/rfc7231#section-6.5.1)|Missing file field, invalid zip format, or YAML parse error.|None|

<aside class="warning">
To perform this operation, you must be authenticated by means of one of the following methods:
cookieAuth
</aside>

## Export all catalog rules as a zip archive

<a id="opIdexportCatalog"></a>

> Code samples

```shell
# You can also use wget
curl -X GET /api/rules/catalog/export \
  -H 'Accept: application/zip'

```

```javascript

const headers = {
  'Accept':'application/zip'
};

fetch('/api/rules/catalog/export',
{
  method: 'GET',

  headers: headers
})
.then(function(res) {
    return res.json();
}).then(function(body) {
    console.log(body);
});

```

```python
import requests
headers = {
  'Accept': 'application/zip'
}

r = requests.get('/api/rules/catalog/export', headers = headers)

print(r.json())

```

`GET /rules/catalog/export`

Streams a zip archive containing every rule in the merged catalog as a YAML file (named `{id}.yaml` per rule).

The export includes both the embedded initial rules and any local overrides or custom rules saved in the catalog directory. Use this to back up the catalog or share it with another AgentGuard instance.

> Example responses

> 200 Response

<h3 id="export-all-catalog-rules-as-a-zip-archive-responses">Responses</h3>

|Status|Meaning|Description|Schema|
|---|---|---|---|
|200|[OK](https://tools.ietf.org/html/rfc7231#section-6.3.1)|Zip archive download.|string|
|500|[Internal Server Error](https://tools.ietf.org/html/rfc7231#section-6.6.1)|Failed to build zip archive.|None|

### Response Headers

|Status|Header|Type|Format|Description|
|---|---|---|---|---|
|200|Content-Disposition|string||attachment; filename="agentguard-catalog.zip"|

<aside class="warning">
To perform this operation, you must be authenticated by means of one of the following methods:
cookieAuth
</aside>

<h1 id="agentguard-api-alerts">Alerts</h1>

Real-time alert delivery — webhook receiver and SSE notification stream.

## Receive a rule-triggered alert

<a id="opIdalertWebhook"></a>

> Code samples

```shell
# You can also use wget
curl -X POST /api/alerts/webhook \
  -H 'Content-Type: application/json'

```

```javascript
const inputBody = '{
  "rule_id": "string",
  "rule_name": "string",
  "severity": "critical",
  "agent_id": "string",
  "deployment_id": "string",
  "message": "string",
  "detected_at": "string"
}';
const headers = {
  'Content-Type':'application/json'
};

fetch('/api/alerts/webhook',
{
  method: 'POST',
  body: inputBody,
  headers: headers
})
.then(function(res) {
    return res.json();
}).then(function(body) {
    console.log(body);
});

```

```python
import requests
headers = {
  'Content-Type': 'application/json'
}

r = requests.post('/api/alerts/webhook', headers = headers)

print(r.json())

```

`POST /alerts/webhook`

Webhook endpoint called by the Proton `notify_agentguard_udf` Python UDF. The UDF is invoked by the `agentguard_notifier` Proton alert when new rows appear in `agentguard_security_events`.

Received alerts are fanned out to all connected SSE clients on `/notifications/stream`. You can also POST to this endpoint manually for testing.

> Body parameter

```json
{
  "rule_id": "string",
  "rule_name": "string",
  "severity": "critical",
  "agent_id": "string",
  "deployment_id": "string",
  "message": "string",
  "detected_at": "string"
}
```

<h3 id="receive-a-rule-triggered-alert-parameters">Parameters</h3>

|Name|In|Type|Required|Description|
|---|---|---|---|---|
|body|body|[Alert](#schemaalert)|true|none|

<h3 id="receive-a-rule-triggered-alert-responses">Responses</h3>

|Status|Meaning|Description|Schema|
|---|---|---|---|
|200|[OK](https://tools.ietf.org/html/rfc7231#section-6.3.1)|Alert accepted and forwarded to SSE clients.|None|

<aside class="warning">
To perform this operation, you must be authenticated by means of one of the following methods:
cookieAuth
</aside>

## Live alert SSE stream

<a id="opIdnotificationsStream"></a>

> Code samples

```shell
# You can also use wget
curl -X GET /api/notifications/stream \
  -H 'Accept: text/event-stream'

```

```javascript

const headers = {
  'Accept':'text/event-stream'
};

fetch('/api/notifications/stream',
{
  method: 'GET',

  headers: headers
})
.then(function(res) {
    return res.json();
}).then(function(body) {
    console.log(body);
});

```

```python
import requests
headers = {
  'Accept': 'text/event-stream'
}

r = requests.get('/api/notifications/stream', headers = headers)

print(r.json())

```

`GET /notifications/stream`

Server-Sent Events stream that pushes real-time security alert notifications to connected clients.

Each event is a JSON-encoded `Alert` object. The stream keeps the connection alive with periodic comments (`: keepalive`).

Connect with `EventSource` in the browser or `curl -N`:
```
curl -N http://localhost:8080/api/notifications/stream
```

> Example responses

> 200 Response

<h3 id="live-alert-sse-stream-responses">Responses</h3>

|Status|Meaning|Description|Schema|
|---|---|---|---|
|200|[OK](https://tools.ietf.org/html/rfc7231#section-6.3.1)|SSE stream — one JSON alert per `data:` line.|string|

<aside class="warning">
To perform this operation, you must be authenticated by means of one of the following methods:
cookieAuth
</aside>

<h1 id="agentguard-api-investigate">Investigate</h1>

Ad-hoc SQL query interface against Proton streams.

## List queryable tables

<a id="opIdlistTables"></a>

> Code samples

```shell
# You can also use wget
curl -X GET /api/investigate/tables \
  -H 'Accept: application/json'

```

```javascript

const headers = {
  'Accept':'application/json'
};

fetch('/api/investigate/tables',
{
  method: 'GET',

  headers: headers
})
.then(function(res) {
    return res.json();
}).then(function(body) {
    console.log(body);
});

```

```python
import requests
headers = {
  'Accept': 'application/json'
}

r = requests.get('/api/investigate/tables', headers = headers)

print(r.json())

```

`GET /investigate/tables`

Returns the names of Proton streams and views available for ad-hoc queries via `/investigate/query`.

Includes the core AgentGuard streams (`agentguard_hook_events`, `agentguard_security_events`, `agentguard_rules`) as well as OTel streams (`otel_traces`, `otel_logs`, `otel_metrics_*`).

> Example responses

> 200 Response

```json
{
  "tables": [
    "agentguard_hook_events",
    "agentguard_security_events",
    "agentguard_rules",
    "agentguard_pricing",
    "otel_traces",
    "otel_logs"
  ]
}
```

<h3 id="list-queryable-tables-responses">Responses</h3>

|Status|Meaning|Description|Schema|
|---|---|---|---|
|200|[OK](https://tools.ietf.org/html/rfc7231#section-6.3.1)|Available table names|Inline|

<h3 id="list-queryable-tables-responseschema">Response Schema</h3>

Status Code **200**

|Name|Type|Required|Restrictions|Description|
|---|---|---|---|---|
|» tables|[string]|false|none|none|

<aside class="warning">
To perform this operation, you must be authenticated by means of one of the following methods:
cookieAuth
</aside>

<h1 id="agentguard-api-proton">Proton</h1>

Streaming proxy — send arbitrary SQL to Timeplus and receive NDJSON rows.

## Streaming SQL proxy (NDJSON)

<a id="opIdprotonQuery"></a>

> Code samples

```shell
# You can also use wget
curl -X POST /api/proton/query \
  -H 'Content-Type: application/json' \
  -H 'Accept: application/x-ndjson'

```

```javascript
const inputBody = '{
  "sql": "string"
}';
const headers = {
  'Content-Type':'application/json',
  'Accept':'application/x-ndjson'
};

fetch('/api/proton/query',
{
  method: 'POST',
  body: inputBody,
  headers: headers
})
.then(function(res) {
    return res.json();
}).then(function(body) {
    console.log(body);
});

```

```python
import requests
headers = {
  'Content-Type': 'application/json',
  'Accept': 'application/x-ndjson'
}

r = requests.post('/api/proton/query', headers = headers)

print(r.json())

```

`POST /proton/query`

Proxies a SQL query directly to Proton and streams result rows back as **newline-delimited JSON** (NDJSON). Unlike `/investigate/query` this endpoint does not buffer results — it streams rows as Proton produces them, making it suitable for long-running or live streaming queries.

The frontend uses this via `@timeplus/proton-javascript-driver` to power the real-time Investigate page.

Send a `POST` with `{"sql": "..."}` and consume the response body as an NDJSON stream. Use an `AbortController` to cancel.

> Body parameter

```json
{
  "sql": "string"
}
```

<h3 id="streaming-sql-proxy-(ndjson)-parameters">Parameters</h3>

|Name|In|Type|Required|Description|
|---|---|---|---|---|
|body|body|object|true|none|
|» sql|body|string|true|Any valid Proton SQL. Omit `table()` wrapper for live streaming; use `table()` for historical batch queries.|

> Example responses

> 200 Response

<h3 id="streaming-sql-proxy-(ndjson)-responses">Responses</h3>

|Status|Meaning|Description|Schema|
|---|---|---|---|
|200|[OK](https://tools.ietf.org/html/rfc7231#section-6.3.1)|NDJSON stream — one JSON object per line.|string|
|400|[Bad Request](https://tools.ietf.org/html/rfc7231#section-6.5.1)|Empty or missing SQL.|None|
|503|[Service Unavailable](https://tools.ietf.org/html/rfc7231#section-6.6.4)|Proton unavailable.|None|

<aside class="warning">
To perform this operation, you must be authenticated by means of one of the following methods:
cookieAuth
</aside>

<h1 id="agentguard-api-onboarding">Onboarding</h1>

Agent onboarding wizard — generates configuration snippets for connecting agents.

## Download the AgentGuard install script

<a id="opIdgetInstallScript"></a>

> Code samples

```shell
# You can also use wget
curl -X GET /api/onboarding/install-script \
  -H 'Accept: text/x-shellscript'

```

```javascript

const headers = {
  'Accept':'text/x-shellscript'
};

fetch('/api/onboarding/install-script',
{
  method: 'GET',

  headers: headers
})
.then(function(res) {
    return res.json();
}).then(function(body) {
    console.log(body);
});

```

```python
import requests
headers = {
  'Accept': 'text/x-shellscript'
}

r = requests.get('/api/onboarding/install-script', headers = headers)

print(r.json())

```

`GET /onboarding/install-script`

Serves the shell script that automates plugin installation and configuration for an agent. The response is a downloadable `agentguard-install.sh` file.

> Example responses

> 200 Response

<h3 id="download-the-agentguard-install-script-responses">Responses</h3>

|Status|Meaning|Description|Schema|
|---|---|---|---|
|200|[OK](https://tools.ietf.org/html/rfc7231#section-6.3.1)|Shell script download.|string|

### Response Headers

|Status|Header|Type|Format|Description|
|---|---|---|---|---|
|200|Content-Disposition|string||attachment; filename="agentguard-install.sh"|

<aside class="warning">
To perform this operation, you must be authenticated by means of one of the following methods:
cookieAuth
</aside>

## Generate an agent configuration snippet

<a id="opIdgenerateConfig"></a>

> Code samples

```shell
# You can also use wget
curl -X POST /api/onboarding/generate \
  -H 'Content-Type: application/json' \
  -H 'Accept: application/json'

```

```javascript
const inputBody = '{
  "agentType": "openclaw",
  "endpoint": "string",
  "captureContent": true
}';
const headers = {
  'Content-Type':'application/json',
  'Accept':'application/json'
};

fetch('/api/onboarding/generate',
{
  method: 'POST',
  body: inputBody,
  headers: headers
})
.then(function(res) {
    return res.json();
}).then(function(body) {
    console.log(body);
});

```

```python
import requests
headers = {
  'Content-Type': 'application/json',
  'Accept': 'application/json'
}

r = requests.post('/api/onboarding/generate', headers = headers)

print(r.json())

```

`POST /onboarding/generate`

Generates a configuration snippet for connecting an AI agent to AgentGuard. Used by the onboarding wizard.

> Body parameter

```json
{
  "agentType": "openclaw",
  "endpoint": "string",
  "captureContent": true
}
```

<h3 id="generate-an-agent-configuration-snippet-parameters">Parameters</h3>

|Name|In|Type|Required|Description|
|---|---|---|---|---|
|body|body|object|true|none|
|» agentType|body|string|false|Target agent framework.|
|» endpoint|body|string|false|Timeplus ingest endpoint URL.|
|» captureContent|body|boolean|false|Whether to capture full prompt/response content.|

#### Enumerated Values

|Parameter|Value|
|---|---|
|» agentType|openclaw|
|» agentType|claude-code|

> Example responses

> 200 Response

```json
{
  "config": {
    "timeplusUrl": "http://localhost:3218",
    "stream": "agentguard_hook_events"
  }
}
```

<h3 id="generate-an-agent-configuration-snippet-responses">Responses</h3>

|Status|Meaning|Description|Schema|
|---|---|---|---|
|200|[OK](https://tools.ietf.org/html/rfc7231#section-6.3.1)|Generated config object.|None|

<h3 id="generate-an-agent-configuration-snippet-responseschema">Response Schema</h3>

<aside class="warning">
To perform this operation, you must be authenticated by means of one of the following methods:
cookieAuth
</aside>

<h1 id="agentguard-api-auth">Auth</h1>

Session authentication — login, logout, and status check. These endpoints are **unauthenticated** (no session cookie required).

## Get authentication and setup status

<a id="opIdauthStatus"></a>

> Code samples

```shell
# You can also use wget
curl -X GET /api/auth/status \
  -H 'Accept: application/json'

```

```javascript

const headers = {
  'Accept':'application/json'
};

fetch('/api/auth/status',
{
  method: 'GET',

  headers: headers
})
.then(function(res) {
    return res.json();
}).then(function(body) {
    console.log(body);
});

```

```python
import requests
headers = {
  'Accept': 'application/json'
}

r = requests.get('/api/auth/status', headers = headers)

print(r.json())

```

`GET /auth/status`

Returns whether the server has been provisioned (setup wizard completed) and whether the current request carries a valid session.

This is the first call the frontend makes on load. If `setup_complete` is false, show the setup wizard. If `logged_in` is false, show the login page. Otherwise proceed to the dashboard.

> Example responses

> 200 Response

```json
{
  "setup_complete": true,
  "logged_in": true,
  "username": "default"
}
```

<h3 id="get-authentication-and-setup-status-responses">Responses</h3>

|Status|Meaning|Description|Schema|
|---|---|---|---|
|200|[OK](https://tools.ietf.org/html/rfc7231#section-6.3.1)|Current auth state|[AuthStatus](#schemaauthstatus)|

<aside class="success">
This operation does not require authentication
</aside>

## Log in with Timeplus credentials

<a id="opIdauthLogin"></a>

> Code samples

```shell
# You can also use wget
curl -X POST /api/auth/login \
  -H 'Content-Type: application/json' \
  -H 'Accept: application/json'

```

```javascript
const inputBody = '{
  "username": "default",
  "password": ""
}';
const headers = {
  'Content-Type':'application/json',
  'Accept':'application/json'
};

fetch('/api/auth/login',
{
  method: 'POST',
  body: inputBody,
  headers: headers
})
.then(function(res) {
    return res.json();
}).then(function(body) {
    console.log(body);
});

```

```python
import requests
headers = {
  'Content-Type': 'application/json',
  'Accept': 'application/json'
}

r = requests.post('/api/auth/login', headers = headers)

print(r.json())

```

`POST /auth/login`

Validates the supplied credentials against Timeplus by running a lightweight test query. On success, creates a server-side session (8-hour expiry) and sets an `agentguard_session` httpOnly cookie.

Credentials are never written to disk — they live only in the in-memory session store for the duration of the session.

> Body parameter

```json
{
  "username": "default",
  "password": ""
}
```

<h3 id="log-in-with-timeplus-credentials-parameters">Parameters</h3>

|Name|In|Type|Required|Description|
|---|---|---|---|---|
|body|body|[LoginRequest](#schemaloginrequest)|true|none|

> Example responses

> 200 Response

```json
{
  "username": "default"
}
```

<h3 id="log-in-with-timeplus-credentials-responses">Responses</h3>

|Status|Meaning|Description|Schema|
|---|---|---|---|
|200|[OK](https://tools.ietf.org/html/rfc7231#section-6.3.1)|Login successful. Sets `agentguard_session` httpOnly cookie.|[LoginResponse](#schemaloginresponse)|
|400|[Bad Request](https://tools.ietf.org/html/rfc7231#section-6.5.1)|Missing username or password field.|None|
|401|[Unauthorized](https://tools.ietf.org/html/rfc7235#section-3.1)|Credentials rejected by Timeplus.|None|
|503|[Service Unavailable](https://tools.ietf.org/html/rfc7231#section-6.6.4)|Timeplus unreachable during credential validation.|None|

### Response Headers

|Status|Header|Type|Format|Description|
|---|---|---|---|---|
|200|Set-Cookie|string||agentguard_session=<token>; HttpOnly; SameSite=Strict; Path=/|

<aside class="success">
This operation does not require authentication
</aside>

## Log out

<a id="opIdauthLogout"></a>

> Code samples

```shell
# You can also use wget
curl -X POST /api/auth/logout \
  -H 'Accept: application/json'

```

```javascript

const headers = {
  'Accept':'application/json'
};

fetch('/api/auth/logout',
{
  method: 'POST',

  headers: headers
})
.then(function(res) {
    return res.json();
}).then(function(body) {
    console.log(body);
});

```

```python
import requests
headers = {
  'Accept': 'application/json'
}

r = requests.post('/api/auth/logout', headers = headers)

print(r.json())

```

`POST /auth/logout`

Destroys the current server-side session and clears the `agentguard_session` cookie by setting `Max-Age=-1`. Safe to call even if no session exists.

> Example responses

> 200 Response

```json
{
  "ok": true
}
```

<h3 id="log-out-responses">Responses</h3>

|Status|Meaning|Description|Schema|
|---|---|---|---|
|200|[OK](https://tools.ietf.org/html/rfc7231#section-6.3.1)|Session destroyed.|None|

<h3 id="log-out-responseschema">Response Schema</h3>

<aside class="success">
This operation does not require authentication
</aside>

<h1 id="agentguard-api-setup">Setup</h1>

First-time setup wizard — connection test and resource provisioning. These endpoints are **unauthenticated** and are only used before setup is complete.

## Test a Timeplus connection

<a id="opIdtestConnection"></a>

> Code samples

```shell
# You can also use wget
curl -X POST /api/setup/test-connection \
  -H 'Content-Type: application/json' \
  -H 'Accept: application/json'

```

```javascript
const inputBody = '{
  "host": "localhost",
  "port": 3218,
  "username": "default",
  "password": ""
}';
const headers = {
  'Content-Type':'application/json',
  'Accept':'application/json'
};

fetch('/api/setup/test-connection',
{
  method: 'POST',
  body: inputBody,
  headers: headers
})
.then(function(res) {
    return res.json();
}).then(function(body) {
    console.log(body);
});

```

```python
import requests
headers = {
  'Content-Type': 'application/json',
  'Accept': 'application/json'
}

r = requests.post('/api/setup/test-connection', headers = headers)

print(r.json())

```

`POST /setup/test-connection`

Attempts to connect to Timeplus at the supplied host/port with the given credentials by executing a lightweight `SELECT 1` query. Used by the setup wizard to validate connection parameters before provisioning.

This endpoint is unauthenticated — it can be called before setup is complete.

> Body parameter

```json
{
  "host": "localhost",
  "port": 3218,
  "username": "default",
  "password": ""
}
```

<h3 id="test-a-timeplus-connection-parameters">Parameters</h3>

|Name|In|Type|Required|Description|
|---|---|---|---|---|
|body|body|[TestConnectionRequest](#schematestconnectionrequest)|true|none|

> Example responses

> 200 Response

```json
{
  "ok": true
}
```

<h3 id="test-a-timeplus-connection-responses">Responses</h3>

|Status|Meaning|Description|Schema|
|---|---|---|---|
|200|[OK](https://tools.ietf.org/html/rfc7231#section-6.3.1)|Connection test result (always HTTP 200; check `ok` field).|Inline|

<h3 id="test-a-timeplus-connection-responseschema">Response Schema</h3>

Status Code **200**

|Name|Type|Required|Restrictions|Description|
|---|---|---|---|---|
|» ok|boolean|false|none|True when Timeplus accepted the connection and credentials.|
|» error|string|false|none|Human-readable error message when ok=false.|

<aside class="success">
This operation does not require authentication
</aside>

## Provision all AgentGuard resources (SSE)

<a id="opIdprovision"></a>

> Code samples

```shell
# You can also use wget
curl -X POST /api/setup/provision \
  -H 'Content-Type: application/json' \
  -H 'Accept: text/event-stream'

```

```javascript
const inputBody = '{
  "host": "localhost",
  "port": 3218,
  "native_port": 8463,
  "username": "default",
  "password": "",
  "agentguard_url": "http://localhost:8080"
}';
const headers = {
  'Content-Type':'application/json',
  'Accept':'text/event-stream'
};

fetch('/api/setup/provision',
{
  method: 'POST',
  body: inputBody,
  headers: headers
})
.then(function(res) {
    return res.json();
}).then(function(body) {
    console.log(body);
});

```

```python
import requests
headers = {
  'Content-Type': 'application/json',
  'Accept': 'text/event-stream'
}

r = requests.post('/api/setup/provision', headers = headers)

print(r.json())

```

`POST /setup/provision`

Runs the full provisioning sequence: creates Timeplus streams, materialized views, UDFs, alert rules, and seeds pricing data. Progress is streamed as **Server-Sent Events** so the frontend can display a live log.

Each SSE event is a JSON object `{status, msg}` where `status` is one of:
- `running` — step in progress
- `ok` — step completed successfully
- `error` — step failed (provisioning continues for subsequent steps)
- `done` — all steps finished; call `POST /auth/login` next to start an authenticated session

This endpoint is unauthenticated. On success it writes `config.yaml` with `setup_complete: true` and the connection details (no credentials).

> Body parameter

```json
{
  "host": "localhost",
  "port": 3218,
  "native_port": 8463,
  "username": "default",
  "password": "",
  "agentguard_url": "http://localhost:8080"
}
```

<h3 id="provision-all-agentguard-resources-(sse)-parameters">Parameters</h3>

|Name|In|Type|Required|Description|
|---|---|---|---|---|
|body|body|[ProvisionRequest](#schemaprovisionrequest)|true|none|

> Example responses

> 200 Response

<h3 id="provision-all-agentguard-resources-(sse)-responses">Responses</h3>

|Status|Meaning|Description|Schema|
|---|---|---|---|
|200|[OK](https://tools.ietf.org/html/rfc7231#section-6.3.1)|SSE stream of provisioning progress events.|string|
|400|[Bad Request](https://tools.ietf.org/html/rfc7231#section-6.5.1)|Missing required fields in request body.|None|

<aside class="success">
This operation does not require authentication
</aside>

<h1 id="agentguard-api-holds">Holds</h1>

Human-in-the-loop holds for tool-call approval. Agent plugins call POST /holds synchronously before executing a tool; the request blocks until a human approves/denies, the session ends, or the timeout expires. Dashboard users interact via the other holds endpoints.

## Submit a tool call for evaluation (plugin endpoint)

<a id="opIdcreateHold"></a>

> Code samples

```shell
# You can also use wget
curl -X POST /api/holds \
  -H 'Content-Type: application/json' \
  -H 'Accept: application/json'

```

```javascript
const inputBody = '{
  "deployment_id": "string",
  "agent_id": "string",
  "session_id": "string",
  "run_id": "string",
  "tool_name": "string",
  "tool_input": {},
  "event_time": "2019-08-24T14:15:22Z",
  "hook_payload": {},
  "agent_type": "claudecode",
  "deployment_name": "string"
}';
const headers = {
  'Content-Type':'application/json',
  'Accept':'application/json'
};

fetch('/api/holds',
{
  method: 'POST',
  body: inputBody,
  headers: headers
})
.then(function(res) {
    return res.json();
}).then(function(body) {
    console.log(body);
});

```

```python
import requests
headers = {
  'Content-Type': 'application/json',
  'Accept': 'application/json'
}

r = requests.post('/api/holds', headers = headers)

print(r.json())

```

`POST /holds`

Called synchronously by agent plugins (Claude Code, OpenClaw) on every PreToolUse event. This endpoint is **unauthenticated** — it is called server-to-server from the agent process, not from the browser.

The request blocks until one of:
- The tool call is auto-allowed (no matching active rules with hold/auto_block policy).
- The tool call is auto-blocked (matching rule with `auto_block` policy).
- A human approves or denies the hold via POST /holds/{hold_id}/decide.
- The session ends (SessionEnd hook fires POST /holds/_abandon-session/{session_id}).
- The configured `holds.timeout_seconds` elapses (fail-policy applied).

The `TIMEPLUS_USER` and `TIMEPLUS_PASSWORD` env vars must be set on the AgentGuard server for this endpoint to function.

> Body parameter

```json
{
  "deployment_id": "string",
  "agent_id": "string",
  "session_id": "string",
  "run_id": "string",
  "tool_name": "string",
  "tool_input": {},
  "event_time": "2019-08-24T14:15:22Z",
  "hook_payload": {},
  "agent_type": "claudecode",
  "deployment_name": "string"
}
```

<h3 id="submit-a-tool-call-for-evaluation-(plugin-endpoint)-parameters">Parameters</h3>

|Name|In|Type|Required|Description|
|---|---|---|---|---|
|body|body|[HoldRequest](#schemaholdrequest)|true|none|

> Example responses

> 200 Response

```json
{
  "decision": "allow"
}
```

<h3 id="submit-a-tool-call-for-evaluation-(plugin-endpoint)-responses">Responses</h3>

|Status|Meaning|Description|Schema|
|---|---|---|---|
|200|[OK](https://tools.ietf.org/html/rfc7231#section-6.3.1)|Decision for the tool call.|[HoldResponse](#schemaholdresponse)|
|400|[Bad Request](https://tools.ietf.org/html/rfc7231#section-6.5.1)|Invalid request body.|None|
|503|[Service Unavailable](https://tools.ietf.org/html/rfc7231#section-6.6.4)|Service credentials not configured or Timeplus unavailable.|None|

<aside class="success">
This operation does not require authentication
</aside>

## List holds

<a id="opIdlistHolds"></a>

> Code samples

```shell
# You can also use wget
curl -X GET /api/holds \
  -H 'Accept: application/json'

```

```javascript

const headers = {
  'Accept':'application/json'
};

fetch('/api/holds',
{
  method: 'GET',

  headers: headers
})
.then(function(res) {
    return res.json();
}).then(function(body) {
    console.log(body);
});

```

```python
import requests
headers = {
  'Accept': 'application/json'
}

r = requests.get('/api/holds', headers = headers)

print(r.json())

```

`GET /holds`

Returns holds filtered by status. Defaults to pending holds (holds awaiting human decision). Holds are resolved via POST /holds/{hold_id}/decide.

<h3 id="list-holds-parameters">Parameters</h3>

|Name|In|Type|Required|Description|
|---|---|---|---|---|
|status|query|string|false|Status to filter by. Defaults to pending.|
|limit|query|integer|false|Max rows to return (1-500, default 100).|

#### Enumerated Values

|Parameter|Value|
|---|---|
|status|pending|
|status|approved|
|status|denied|
|status|timeout|
|status|abandoned|

> Example responses

> 200 Response

```json
{
  "holds": [
    {
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
    }
  ],
  "total": 1
}
```

<h3 id="list-holds-responses">Responses</h3>

|Status|Meaning|Description|Schema|
|---|---|---|---|
|200|[OK](https://tools.ietf.org/html/rfc7231#section-6.3.1)|Hold list|Inline|

<h3 id="list-holds-responseschema">Response Schema</h3>

Status Code **200**

|Name|Type|Required|Restrictions|Description|
|---|---|---|---|---|
|» holds|[[Hold](#schemahold)]|false|none|[A hold record from agentguard_holds. Current state is computed inline via arg_max(field, updated_at) GROUP BY hold_id.]|
|»» hold_id|string|false|none|UUID for this hold.|
|»» deployment_id|string|false|none|none|
|»» agent_id|string|false|none|none|
|»» session_id|string|false|none|none|
|»» run_id|string|false|none|none|
|»» tool_name|string|false|none|The tool call that was intercepted.|
|»» rule_id|string|false|none|The rule that triggered the hold.|
|»» severity|string|false|none|none|
|»» args_summary|string|false|none|Truncated JSON of tool_input (max 2048 bytes).|
|»» message|string|false|none|Human-readable reason the hold was created.|
|»» created_at|string(date-time)|false|none|none|
|»» status|string|false|none|none|
|»» decided_by|string|false|none|Email or identifier of the person who decided the hold. Empty for system decisions.|
|»» decided_at|string(date-time)|false|none|none|
|»» decided_reason|string|false|none|none|
|» total|integer|false|none|none|

#### Enumerated Values

|Property|Value|
|---|---|
|severity|critical|
|severity|warning|
|severity|info|
|status|pending|
|status|approved|
|status|denied|
|status|timeout|
|status|abandoned|

<aside class="warning">
To perform this operation, you must be authenticated by means of one of the following methods:
cookieAuth
</aside>

## Abandon all pending holds for a session (plugin endpoint)

<a id="opIdabandonSession"></a>

> Code samples

```shell
# You can also use wget
curl -X POST /api/holds/_abandon-session/{session_id} \
  -H 'Accept: application/json'

```

```javascript

const headers = {
  'Accept':'application/json'
};

fetch('/api/holds/_abandon-session/{session_id}',
{
  method: 'POST',

  headers: headers
})
.then(function(res) {
    return res.json();
}).then(function(body) {
    console.log(body);
});

```

```python
import requests
headers = {
  'Accept': 'application/json'
}

r = requests.post('/api/holds/_abandon-session/{session_id}', headers = headers)

print(r.json())

```

`POST /holds/_abandon-session/{session_id}`

Called fire-and-forget by the agent plugin's SessionEnd hook when a session terminates. Marks every pending hold for the session as `abandoned` and wakes the blocked POST /holds goroutine so the plugin can exit cleanly. This endpoint is **unauthenticated**.

<h3 id="abandon-all-pending-holds-for-a-session-(plugin-endpoint)-parameters">Parameters</h3>

|Name|In|Type|Required|Description|
|---|---|---|---|---|
|session_id|path|string|true|Session UUID that is ending.|

> Example responses

> 200 Response

```json
{
  "abandoned": 2
}
```

<h3 id="abandon-all-pending-holds-for-a-session-(plugin-endpoint)-responses">Responses</h3>

|Status|Meaning|Description|Schema|
|---|---|---|---|
|200|[OK](https://tools.ietf.org/html/rfc7231#section-6.3.1)|Holds abandoned.|None|
|503|[Service Unavailable](https://tools.ietf.org/html/rfc7231#section-6.6.4)|Service credentials not configured or Timeplus unavailable.|None|

<h3 id="abandon-all-pending-holds-for-a-session-(plugin-endpoint)-responseschema">Response Schema</h3>

<aside class="success">
This operation does not require authentication
</aside>

## Drain all pending holds (operator endpoint)

<a id="opIddrainHolds"></a>

> Code samples

```shell
# You can also use wget
curl -X POST /api/holds/_drain \
  -H 'Accept: application/json'

```

```javascript

const headers = {
  'Accept':'application/json'
};

fetch('/api/holds/_drain',
{
  method: 'POST',

  headers: headers
})
.then(function(res) {
    return res.json();
}).then(function(body) {
    console.log(body);
});

```

```python
import requests
headers = {
  'Accept': 'application/json'
}

r = requests.post('/api/holds/_drain', headers = headers)

print(r.json())

```

`POST /holds/_drain`

Marks every pending hold as `abandoned` and applies fail-policy to each blocked plugin request. Use during maintenance or incident response to unblock all agents immediately.

> Example responses

> 200 Response

```json
{
  "drained": 5
}
```

<h3 id="drain-all-pending-holds-(operator-endpoint)-responses">Responses</h3>

|Status|Meaning|Description|Schema|
|---|---|---|---|
|200|[OK](https://tools.ietf.org/html/rfc7231#section-6.3.1)|All pending holds drained.|None|
|503|[Service Unavailable](https://tools.ietf.org/html/rfc7231#section-6.6.4)|Timeplus unavailable.|None|

<h3 id="drain-all-pending-holds-(operator-endpoint)-responseschema">Response Schema</h3>

<aside class="warning">
To perform this operation, you must be authenticated by means of one of the following methods:
cookieAuth
</aside>

## Get hold detail

<a id="opIdgetHold"></a>

> Code samples

```shell
# You can also use wget
curl -X GET /api/holds/{hold_id} \
  -H 'Accept: application/json'

```

```javascript

const headers = {
  'Accept':'application/json'
};

fetch('/api/holds/{hold_id}',
{
  method: 'GET',

  headers: headers
})
.then(function(res) {
    return res.json();
}).then(function(body) {
    console.log(body);
});

```

```python
import requests
headers = {
  'Accept': 'application/json'
}

r = requests.get('/api/holds/{hold_id}', headers = headers)

print(r.json())

```

`GET /holds/{hold_id}`

Returns the current state of a single hold plus a full status-change timeline (all rows for this hold_id ordered by updated_at).

<h3 id="get-hold-detail-parameters">Parameters</h3>

|Name|In|Type|Required|Description|
|---|---|---|---|---|
|hold_id|path|string|true|Hold UUID.|

> Example responses

> 200 Response

```json
{
  "hold": {
    "hold_id": "e1f2g3h4-0000-0000-0000-000000000001",
    "tool_name": "Bash",
    "status": "pending",
    "created_at": "2026-04-09T10:00:05Z"
  },
  "timeline": [
    {
      "status": "pending",
      "decided_by": "",
      "decided_at": null,
      "decided_reason": "",
      "updated_at": "2026-04-09T10:00:05Z"
    }
  ]
}
```

<h3 id="get-hold-detail-responses">Responses</h3>

|Status|Meaning|Description|Schema|
|---|---|---|---|
|200|[OK](https://tools.ietf.org/html/rfc7231#section-6.3.1)|Hold detail with timeline|None|
|404|[Not Found](https://tools.ietf.org/html/rfc7231#section-6.5.4)|Hold not found.|None|

<h3 id="get-hold-detail-responseschema">Response Schema</h3>

<aside class="warning">
To perform this operation, you must be authenticated by means of one of the following methods:
cookieAuth
</aside>

## Approve or deny a hold

<a id="opIddecideHold"></a>

> Code samples

```shell
# You can also use wget
curl -X POST /api/holds/{hold_id}/decide \
  -H 'Content-Type: application/json' \
  -H 'Accept: application/json'

```

```javascript
const inputBody = '{
  "decision": "approved",
  "decided_by": "string",
  "decided_reason": "string"
}';
const headers = {
  'Content-Type':'application/json',
  'Accept':'application/json'
};

fetch('/api/holds/{hold_id}/decide',
{
  method: 'POST',
  body: inputBody,
  headers: headers
})
.then(function(res) {
    return res.json();
}).then(function(body) {
    console.log(body);
});

```

```python
import requests
headers = {
  'Content-Type': 'application/json',
  'Accept': 'application/json'
}

r = requests.post('/api/holds/{hold_id}/decide', headers = headers)

print(r.json())

```

`POST /holds/{hold_id}/decide`

Submits a human decision for a pending hold. The decision is persisted to `agentguard_holds` and the blocked POST /holds goroutine is woken so the plugin receives the answer immediately.

If the hold has already been decided (approved, denied, timeout, abandoned), this call is idempotent — it returns the existing decision without writing a new row.

> Body parameter

```json
{
  "decision": "approved",
  "decided_by": "string",
  "decided_reason": "string"
}
```

<h3 id="approve-or-deny-a-hold-parameters">Parameters</h3>

|Name|In|Type|Required|Description|
|---|---|---|---|---|
|hold_id|path|string|true|Hold UUID.|
|body|body|[HoldDecideRequest](#schemaholddeciderequest)|true|none|

> Example responses

> 200 Response

```json
{
  "ok": true
}
```

<h3 id="approve-or-deny-a-hold-responses">Responses</h3>

|Status|Meaning|Description|Schema|
|---|---|---|---|
|200|[OK](https://tools.ietf.org/html/rfc7231#section-6.3.1)|Decision recorded.|None|
|400|[Bad Request](https://tools.ietf.org/html/rfc7231#section-6.5.1)|decision must be approved|denied.|None|
|404|[Not Found](https://tools.ietf.org/html/rfc7231#section-6.5.4)|Hold not found.|None|

<h3 id="approve-or-deny-a-hold-responseschema">Response Schema</h3>

<aside class="warning">
To perform this operation, you must be authenticated by means of one of the following methods:
cookieAuth
</aside>

<h1 id="agentguard-api-activity">Activity</h1>

Combined chronological feed of hold events and (optionally) auto-allowed tool-call hook events. Powers the real-time activity view in the dashboard.

## List activity events

<a id="opIdlistActivity"></a>

> Code samples

```shell
# You can also use wget
curl -X GET /api/activity \
  -H 'Accept: application/json'

```

```javascript

const headers = {
  'Accept':'application/json'
};

fetch('/api/activity',
{
  method: 'GET',

  headers: headers
})
.then(function(res) {
    return res.json();
}).then(function(body) {
    console.log(body);
});

```

```python
import requests
headers = {
  'Accept': 'application/json'
}

r = requests.get('/api/activity', headers = headers)

print(r.json())

```

`GET /activity`

Returns a combined chronological feed of hold events and (optionally) auto-allowed tool-call hook events, ordered newest-first.

By default only hold events (those that triggered a rule hold/block decision) are returned. Pass `include_allowed=true` to also include hook events that were auto-allowed without triggering any rule — useful for a full tool-call audit trail per session.

<h3 id="list-activity-events-parameters">Parameters</h3>

|Name|In|Type|Required|Description|
|---|---|---|---|---|
|session_id|query|string|false|Filter to a single session.|
|status|query|string|false|Filter hold events by status.|
|include_allowed|query|boolean|false|Also include auto-allowed hook events (default false). Raises the max limit to 500.|
|limit|query|integer|false|Max events to return (1-100, or 1-500 when include_allowed=true). Default 100.|

#### Enumerated Values

|Parameter|Value|
|---|---|
|status|pending|
|status|approved|
|status|denied|
|status|timeout|
|status|abandoned|

> Example responses

> 200 Response

```json
{
  "events": [
    {
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
    }
  ],
  "total": 1
}
```

<h3 id="list-activity-events-responses">Responses</h3>

|Status|Meaning|Description|Schema|
|---|---|---|---|
|200|[OK](https://tools.ietf.org/html/rfc7231#section-6.3.1)|Activity event feed|[ActivityResponse](#schemaactivityresponse)|

<aside class="warning">
To perform this operation, you must be authenticated by means of one of the following methods:
cookieAuth
</aside>

# Schemas

<h2 id="tocS_Dashboard">Dashboard</h2>
<!-- backwards compatibility -->
<a id="schemadashboard"></a>
<a id="schema_Dashboard"></a>
<a id="tocSdashboard"></a>
<a id="tocsdashboard"></a>

```json
{
  "kpis": {
    "total_agents": 0,
    "agents_delta": "string",
    "active_threats": 0,
    "critical_escalations": 0,
    "total_cost_24h": "string",
    "cost_status": "string",
    "active_rules": 0
  },
  "live_threat_feed": [
    {
      "threat_id": "string",
      "rule_id": "string",
      "rule_name": "string",
      "severity": "critical",
      "agent_id": "string",
      "deployment_id": "string",
      "session_id": "string",
      "event_count": 0,
      "first_seen": "string",
      "last_seen": "string",
      "status": "open",
      "status_updated_at": "string",
      "events": [
        {
          "detected_at": "string",
          "run_id": "string",
          "hook_name": "string",
          "tool_name": "string",
          "message": "string",
          "event_data": "string"
        }
      ]
    }
  ],
  "threat_categories": [
    {
      "name": "string",
      "percentage": 0
    }
  ],
  "recent_agents": [
    {
      "agent_id": "string",
      "deployment_id": "string",
      "deployment_name": "string",
      "agent_type": "claudecode",
      "status": "active",
      "event_count": 0,
      "session_count": 0,
      "run_count": 0,
      "tool_call_count": 0,
      "total_tokens": 0,
      "llm_call_count": 0,
      "last_seen": "2019-08-24T14:15:22Z"
    }
  ]
}

```

Full dashboard response with KPIs, live threat feed, threat categories, and recent agents.

### Properties

|Name|Type|Required|Restrictions|Description|
|---|---|---|---|---|
|kpis|object|false|none|High-level KPI counters.|
|» total_agents|integer|false|none|Number of distinct (deployment_id, agent_id) pairs.|
|» agents_delta|string|false|none|Human-readable delta vs prior period, e.g. +2.|
|» active_threats|integer|false|none|Threats currently in open state.|
|» critical_escalations|integer|false|none|Critical-severity threats in the last 24h.|
|» total_cost_24h|string|false|none|Formatted total cost for the last 24h, e.g. $4.20.|
|» cost_status|string|false|none|Budget status label, e.g. under_budget.|
|» active_rules|integer|false|none|Number of rules currently enabled.|
|live_threat_feed|[[Threat](#schemathreat)]|false|none|Most recent open threats.|
|threat_categories|[object]|false|none|Threat count breakdown by category.|
|» name|string|false|none|none|
|» percentage|integer|false|none|none|
|recent_agents|[[Agent](#schemaagent)]|false|none|Most recently active agents.|

<h2 id="tocS_Agent">Agent</h2>
<!-- backwards compatibility -->
<a id="schemaagent"></a>
<a id="schema_Agent"></a>
<a id="tocSagent"></a>
<a id="tocsagent"></a>

```json
{
  "agent_id": "string",
  "deployment_id": "string",
  "deployment_name": "string",
  "agent_type": "claudecode",
  "status": "active",
  "event_count": 0,
  "session_count": 0,
  "run_count": 0,
  "tool_call_count": 0,
  "total_tokens": 0,
  "llm_call_count": 0,
  "last_seen": "2019-08-24T14:15:22Z"
}

```

Aggregated stats for one agent (deployment_id + agent_id pair).

### Properties

|Name|Type|Required|Restrictions|Description|
|---|---|---|---|---|
|agent_id|string|false|none|Agent identifier as reported by the plugin.|
|deployment_id|string|false|none|Deployment/team identifier.|
|deployment_name|string|false|none|Human-readable deployment name.|
|agent_type|string|false|none|Plugin type: claudecode or openclaw.|
|status|string|false|none|Derived activity status.|
|event_count|integer|false|none|Total hook events from this agent.|
|session_count|integer|false|none|Number of distinct sessions.|
|run_count|integer|false|none|Number of distinct LLM runs.|
|tool_call_count|integer|false|none|Total before_tool_call events.|
|total_tokens|integer|false|none|Sum of input + output + cache-read tokens.|
|llm_call_count|integer|false|none|Number of llm_output hook events (one per LLM turn).|
|last_seen|string(date-time)|false|none|Timestamp of the most recent hook event.|

#### Enumerated Values

|Property|Value|
|---|---|
|agent_type|claudecode|
|agent_type|openclaw|
|agent_type||
|status|active|
|status|idle|

<h2 id="tocS_AgentDetail">AgentDetail</h2>
<!-- backwards compatibility -->
<a id="schemaagentdetail"></a>
<a id="schema_AgentDetail"></a>
<a id="tocSagentdetail"></a>
<a id="tocsagentdetail"></a>

```json
{
  "agent_id": "string",
  "deployment_id": "string",
  "deployment_name": "string",
  "agent_type": "claudecode",
  "status": "active",
  "event_count": 0,
  "session_count": 0,
  "run_count": 0,
  "tool_call_count": 0,
  "total_tokens": 0,
  "llm_call_count": 0,
  "last_seen": "2019-08-24T14:15:22Z"
}

```

GET /agents/{id} returns the same Agent schema with all fields populated including run_count, tool_call_count, and status.

### Properties

*None*

<h2 id="tocS_Session">Session</h2>
<!-- backwards compatibility -->
<a id="schemasession"></a>
<a id="schema_Session"></a>
<a id="tocSsession"></a>
<a id="tocssession"></a>

```json
{
  "session_id": "string",
  "session_key": "string",
  "session_type": "Web UI",
  "status": "active",
  "model": "string",
  "provider": "string",
  "run_count": 0,
  "event_count": 0,
  "first_seen": "2019-08-24T14:15:22Z",
  "last_seen": "2019-08-24T14:15:22Z"
}

```

A single user conversation session within an agent.

### Properties

|Name|Type|Required|Restrictions|Description|
|---|---|---|---|---|
|session_id|string|false|none|UUID identifying the session.|
|session_key|string|false|none|Raw session key, e.g. announce:v1:agent:...:openai-user:abc.|
|session_type|string|false|none|Derived session type.|
|status|string|false|none|active when no session_end event exists; ended otherwise.|
|model|string|false|none|LLM model used in this session (from the most recent llm_output event).|
|provider|string|false|none|LLM provider, e.g. anthropic.|
|run_count|integer|false|none|Number of distinct LLM turns (runs) in this session.|
|event_count|integer|false|none|Total hook events in this session.|
|first_seen|string(date-time)|false|none|none|
|last_seen|string(date-time)|false|none|none|

#### Enumerated Values

|Property|Value|
|---|---|
|session_type|Web UI|
|session_type|HTTP API|
|session_type|Cron|
|session_type|Sub-agent|
|session_type|Unknown|
|status|active|
|status|ended|

<h2 id="tocS_SessionDetail">SessionDetail</h2>
<!-- backwards compatibility -->
<a id="schemasessiondetail"></a>
<a id="schema_SessionDetail"></a>
<a id="tocSsessiondetail"></a>
<a id="tocssessiondetail"></a>

```json
{
  "session_id": "string",
  "session_key": "string",
  "session_type": "string",
  "status": "active",
  "model": "string",
  "provider": "string",
  "total_tool_calls": 0,
  "runs": [
    {
      "run_id": "string",
      "start_time": "2019-08-24T14:15:22Z",
      "end_time": "2019-08-24T14:15:22Z",
      "event_count": 0,
      "model": "string",
      "tool_count": 0,
      "has_block": true
    }
  ]
}

```

Full session detail including ordered run list.

### Properties

|Name|Type|Required|Restrictions|Description|
|---|---|---|---|---|
|session_id|string|false|none|none|
|session_key|string|false|none|none|
|session_type|string|false|none|none|
|status|string|false|none|ended when a session_end hook event exists for this session.|
|model|string|false|none|LLM model used in this session.|
|provider|string|false|none|LLM provider, e.g. anthropic.|
|total_tool_calls|integer|false|none|Total before_tool_call events across all runs.|
|runs|[[Run](#schemarun)]|false|none|[A single LLM turn within a session.]|

#### Enumerated Values

|Property|Value|
|---|---|
|status|active|
|status|ended|

<h2 id="tocS_Run">Run</h2>
<!-- backwards compatibility -->
<a id="schemarun"></a>
<a id="schema_Run"></a>
<a id="tocSrun"></a>
<a id="tocsrun"></a>

```json
{
  "run_id": "string",
  "start_time": "2019-08-24T14:15:22Z",
  "end_time": "2019-08-24T14:15:22Z",
  "event_count": 0,
  "model": "string",
  "tool_count": 0,
  "has_block": true
}

```

A single LLM turn within a session.

### Properties

|Name|Type|Required|Restrictions|Description|
|---|---|---|---|---|
|run_id|string|false|none|Run identifier. Formats: chatcmpl_{uuid}, announce:v1:...:run:{uuid}.|
|start_time|string(date-time)|false|none|Timestamp of first hook event in this run.|
|end_time|string(date-time)|false|none|Timestamp of last hook event.|
|event_count|integer|false|none|Total hook events in this run.|
|model|string|false|none|LLM model name used, e.g. claude-sonnet-4-6.|
|tool_count|integer|false|none|Number of before_tool_call events.|
|has_block|boolean|false|none|True if any hook_decision=block event exists in this run.|

<h2 id="tocS_TraceResponse">TraceResponse</h2>
<!-- backwards compatibility -->
<a id="schematraceresponse"></a>
<a id="schema_TraceResponse"></a>
<a id="tocStraceresponse"></a>
<a id="tocstraceresponse"></a>

```json
{
  "spans": [
    {
      "span_id": "string",
      "parent_id": "string",
      "kind": "run",
      "name": "string",
      "start_ms": 0,
      "duration_ms": 0,
      "status": "ok",
      "attributes": {
        "property1": "string",
        "property2": "string"
      }
    }
  ]
}

```

Waterfall trace built from hook events for one run.

### Properties

|Name|Type|Required|Restrictions|Description|
|---|---|---|---|---|
|spans|[[TraceSpan](#schematracespan)]|false|none|[A single span in the waterfall. The root span (kind=run) covers the full run window; child spans correspond to individual hook events.]|

<h2 id="tocS_TraceSpan">TraceSpan</h2>
<!-- backwards compatibility -->
<a id="schematracespan"></a>
<a id="schema_TraceSpan"></a>
<a id="tocStracespan"></a>
<a id="tocstracespan"></a>

```json
{
  "span_id": "string",
  "parent_id": "string",
  "kind": "run",
  "name": "string",
  "start_ms": 0,
  "duration_ms": 0,
  "status": "ok",
  "attributes": {
    "property1": "string",
    "property2": "string"
  }
}

```

A single span in the waterfall. The root span (kind=run) covers the full run window; child spans correspond to individual hook events.

### Properties

|Name|Type|Required|Restrictions|Description|
|---|---|---|---|---|
|span_id|string|false|none|Unique span identifier. Root span uses the run_id; child spans use evt-{index}.|
|parent_id|string|false|none|Parent span_id. Empty string for the root span.|
|kind|string|false|none|Span category.|
|name|string|false|none|Hook name (e.g. before_tool_call) or 'run' for the root span.|
|start_ms|integer(int64)|false|none|Span start in Unix milliseconds.|
|duration_ms|integer(int64)|false|none|Duration in milliseconds. Gap to next event, or 0 for the last span.|
|status|string|false|none|ok | blocked | error.|
|attributes|object|false|none|Key-value metadata: tool_name, model, provider, hook_decision, block_reason, duration_ms, error.|
|» **additionalProperties**|string|false|none|none|

#### Enumerated Values

|Property|Value|
|---|---|
|kind|run|
|kind|llm|
|kind|tool|
|kind|lifecycle|
|status|ok|
|status|blocked|
|status|error|

<h2 id="tocS_HookEvent">HookEvent</h2>
<!-- backwards compatibility -->
<a id="schemahookevent"></a>
<a id="schema_HookEvent"></a>
<a id="tocShookevent"></a>
<a id="tocshookevent"></a>

```json
{
  "hook_name": "string",
  "event_time": "2019-08-24T14:15:22Z",
  "session_id": "string",
  "run_id": "string",
  "tool_name": "string",
  "provider": "string",
  "model": "string",
  "hook_decision": "observe",
  "block_reason": "string",
  "event_data": "string",
  "agent_type": "claudecode"
}

```

A raw hook event from agentguard_hook_events returned by GET /agents/{id}/events.

### Properties

|Name|Type|Required|Restrictions|Description|
|---|---|---|---|---|
|hook_name|string|false|none|Lifecycle hook name, e.g. before_tool_call, llm_output, session_end.|
|event_time|string(date-time)|false|none|none|
|session_id|string|false|none|none|
|run_id|string|false|none|none|
|tool_name|string|false|none|Tool name for tool-call hooks, empty otherwise.|
|provider|string|false|none|LLM provider, e.g. anthropic.|
|model|string|false|none|LLM model name.|
|hook_decision|string|false|none|Plugin decision: observe (pass-through) or block.|
|block_reason|string|false|none|Human-readable reason when hook_decision=block.|
|event_data|string|false|none|JSON payload with hook-specific fields: tokens, durationMs, toolCallId, error, etc.|
|agent_type|string|false|none|Agent plugin type: claudecode or openclaw.|

#### Enumerated Values

|Property|Value|
|---|---|
|hook_decision|observe|
|hook_decision|block|
|hook_decision||
|agent_type|claudecode|
|agent_type|openclaw|
|agent_type||

<h2 id="tocS_LLMMetrics">LLMMetrics</h2>
<!-- backwards compatibility -->
<a id="schemallmmetrics"></a>
<a id="schema_LLMMetrics"></a>
<a id="tocSllmmetrics"></a>
<a id="tocsllmmetrics"></a>

```json
{
  "period": {
    "since": "2019-08-24T14:15:22Z",
    "until": "2019-08-24T14:15:22Z"
  },
  "filters": {
    "agent_id": "string",
    "model": "string",
    "tool_name": "string"
  },
  "tokens": {
    "input": 0,
    "output": 0,
    "total": 0,
    "cache_read": 0,
    "cache_write": 0
  },
  "calls": {
    "total": 0,
    "success": 0,
    "failed": 0,
    "success_rate": 0
  },
  "stop_reasons": {
    "property1": 0,
    "property2": 0
  },
  "latency_ms": {
    "p50": 0,
    "p95": 0,
    "p99": 0,
    "avg": 0,
    "min": 0,
    "max": 0,
    "samples": 0
  },
  "throughput": [
    {
      "bucket": "2019-08-24T14:15:22Z",
      "llm_calls": 0,
      "input_tokens": 0,
      "output_tokens": 0,
      "total_tokens": 0
    }
  ]
}

```

LLM token usage and call statistics returned by GET /metrics/llm.

### Properties

|Name|Type|Required|Restrictions|Description|
|---|---|---|---|---|
|period|[MetricsPeriod](#schemametricsperiod)|false|none|The time range of a metrics query (both RFC 3339 strings, omitted when unbounded).|
|filters|[MetricsFilters](#schemametricsfilters)|false|none|Active dimension filters echoed back in metrics responses.|
|tokens|object|false|none|Cumulative token counts.|
|» input|integer(int64)|false|none|none|
|» output|integer(int64)|false|none|none|
|» total|integer(int64)|false|none|none|
|» cache_read|integer(int64)|false|none|none|
|» cache_write|integer(int64)|false|none|none|
|calls|object|false|none|LLM API call success/failure summary.|
|» total|integer|false|none|none|
|» success|integer|false|none|none|
|» failed|integer|false|none|none|
|» success_rate|number¦null|false|none|null when total=0.|
|stop_reasons|object|false|none|Count of each stop reason (end_turn, max_tokens, tool_use, etc.).|
|» **additionalProperties**|integer|false|none|none|
|latency_ms|[LatencyStats](#schemalatencystats)|false|none|Latency percentile statistics in milliseconds.|
|throughput|[object]|false|none|Time-bucketed LLM call and token throughput.|
|» bucket|string(date-time)|false|none|none|
|» llm_calls|integer|false|none|none|
|» input_tokens|integer(int64)|false|none|none|
|» output_tokens|integer(int64)|false|none|none|
|» total_tokens|integer(int64)|false|none|none|

<h2 id="tocS_MetricsPeriod">MetricsPeriod</h2>
<!-- backwards compatibility -->
<a id="schemametricsperiod"></a>
<a id="schema_MetricsPeriod"></a>
<a id="tocSmetricsperiod"></a>
<a id="tocsmetricsperiod"></a>

```json
{
  "since": "2019-08-24T14:15:22Z",
  "until": "2019-08-24T14:15:22Z"
}

```

The time range of a metrics query (both RFC 3339 strings, omitted when unbounded).

### Properties

|Name|Type|Required|Restrictions|Description|
|---|---|---|---|---|
|since|string(date-time)|false|none|none|
|until|string(date-time)|false|none|none|

<h2 id="tocS_MetricsFilters">MetricsFilters</h2>
<!-- backwards compatibility -->
<a id="schemametricsfilters"></a>
<a id="schema_MetricsFilters"></a>
<a id="tocSmetricsfilters"></a>
<a id="tocsmetricsfilters"></a>

```json
{
  "agent_id": "string",
  "model": "string",
  "tool_name": "string"
}

```

Active dimension filters echoed back in metrics responses.

### Properties

|Name|Type|Required|Restrictions|Description|
|---|---|---|---|---|
|agent_id|string|false|none|none|
|model|string|false|none|none|
|tool_name|string|false|none|none|

<h2 id="tocS_LatencyStats">LatencyStats</h2>
<!-- backwards compatibility -->
<a id="schemalatencystats"></a>
<a id="schema_LatencyStats"></a>
<a id="tocSlatencystats"></a>
<a id="tocslatencystats"></a>

```json
{
  "p50": 0,
  "p95": 0,
  "p99": 0,
  "avg": 0,
  "min": 0,
  "max": 0,
  "samples": 0
}

```

Latency percentile statistics in milliseconds.

### Properties

|Name|Type|Required|Restrictions|Description|
|---|---|---|---|---|
|p50|number|false|none|none|
|p95|number|false|none|none|
|p99|number|false|none|none|
|avg|number|false|none|none|
|min|number|false|none|none|
|max|number|false|none|none|
|samples|integer|false|none|none|

<h2 id="tocS_ToolMetrics">ToolMetrics</h2>
<!-- backwards compatibility -->
<a id="schematoolmetrics"></a>
<a id="schema_ToolMetrics"></a>
<a id="tocStoolmetrics"></a>
<a id="tocstoolmetrics"></a>

```json
{
  "period": {
    "since": "2019-08-24T14:15:22Z",
    "until": "2019-08-24T14:15:22Z"
  },
  "filters": {
    "agent_id": "string",
    "model": "string",
    "tool_name": "string"
  },
  "summary": {
    "total_calls": 0,
    "success_calls": 0,
    "failed_calls": 0,
    "success_rate": 0
  },
  "by_tool": [
    {
      "tool_name": "string",
      "total_calls": 0,
      "success_calls": 0,
      "failed_calls": 0,
      "success_rate": 0,
      "latency_ms": {
        "p50": 0,
        "p95": 0,
        "p99": 0,
        "avg": 0,
        "min": 0,
        "max": 0,
        "samples": 0
      }
    }
  ]
}

```

Tool-call statistics returned by GET /metrics/tools.

### Properties

|Name|Type|Required|Restrictions|Description|
|---|---|---|---|---|
|period|[MetricsPeriod](#schemametricsperiod)|false|none|The time range of a metrics query (both RFC 3339 strings, omitted when unbounded).|
|filters|[MetricsFilters](#schemametricsfilters)|false|none|Active dimension filters echoed back in metrics responses.|
|summary|object|false|none|Aggregate tool call counts.|
|» total_calls|integer|false|none|none|
|» success_calls|integer|false|none|none|
|» failed_calls|integer|false|none|none|
|» success_rate|number¦null|false|none|none|
|by_tool|[object]|false|none|Per-tool-name breakdown.|
|» tool_name|string|false|none|none|
|» total_calls|integer|false|none|none|
|» success_calls|integer|false|none|none|
|» failed_calls|integer|false|none|none|
|» success_rate|number¦null|false|none|none|
|» latency_ms|[LatencyStats](#schemalatencystats)|false|none|Latency percentile statistics in milliseconds.|

<h2 id="tocS_ThreatEvent">ThreatEvent</h2>
<!-- backwards compatibility -->
<a id="schemathreatevent"></a>
<a id="schema_ThreatEvent"></a>
<a id="tocSthreatevent"></a>
<a id="tocsthreatevent"></a>

```json
{
  "detected_at": "string",
  "run_id": "string",
  "hook_name": "string",
  "tool_name": "string",
  "message": "string",
  "event_data": "string"
}

```

A single raw security event that contributed to a deduplicated threat.

### Properties

|Name|Type|Required|Restrictions|Description|
|---|---|---|---|---|
|detected_at|string|false|none|Detection timestamp (Proton datetime64 string).|
|run_id|string|false|none|none|
|hook_name|string|false|none|none|
|tool_name|string|false|none|none|
|message|string|false|none|none|
|event_data|string|false|none|Raw JSON payload from the triggering hook event.|

<h2 id="tocS_Threat">Threat</h2>
<!-- backwards compatibility -->
<a id="schemathreat"></a>
<a id="schema_Threat"></a>
<a id="tocSthreat"></a>
<a id="tocsthreat"></a>

```json
{
  "threat_id": "string",
  "rule_id": "string",
  "rule_name": "string",
  "severity": "critical",
  "agent_id": "string",
  "deployment_id": "string",
  "session_id": "string",
  "event_count": 0,
  "first_seen": "string",
  "last_seen": "string",
  "status": "open",
  "status_updated_at": "string",
  "events": [
    {
      "detected_at": "string",
      "run_id": "string",
      "hook_name": "string",
      "tool_name": "string",
      "message": "string",
      "event_data": "string"
    }
  ]
}

```

A deduplicated security threat keyed by (agent_id, deployment_id, session_id, rule_id). Includes aggregated event counts and up to 50 individual detection events. Status lifecycle: open -> acknowledged -> cleared (auto-reopens on new events).

### Properties

|Name|Type|Required|Restrictions|Description|
|---|---|---|---|---|
|threat_id|string|false|none|MD5 hex string computed from (agent_id, deployment_id, session_id, rule_id). Used as the :id path parameter.|
|rule_id|string|false|none|none|
|rule_name|string|false|none|none|
|severity|string|false|none|none|
|agent_id|string|false|none|none|
|deployment_id|string|false|none|none|
|session_id|string|false|none|none|
|event_count|integer(int64)|false|none|Total number of security events for this threat key.|
|first_seen|string|false|none|Timestamp of the earliest detection (Proton datetime string).|
|last_seen|string|false|none|Timestamp of the most recent detection.|
|status|string|false|none|Lifecycle status. Reverts to open if new events arrive after acknowledged or cleared.|
|status_updated_at|string|false|none|When status was last changed.|
|events|[[ThreatEvent](#schemathreatevent)]|false|none|Up to 50 individual detection events, newest first.|

#### Enumerated Values

|Property|Value|
|---|---|
|severity|critical|
|severity|warning|
|severity|info|
|status|open|
|status|acknowledged|
|status|cleared|

<h2 id="tocS_ThreatDetailResponse">ThreatDetailResponse</h2>
<!-- backwards compatibility -->
<a id="schemathreatdetailresponse"></a>
<a id="schema_ThreatDetailResponse"></a>
<a id="tocSthreatdetailresponse"></a>
<a id="tocsthreatdetailresponse"></a>

```json
{
  "threat_id": "string",
  "rule_id": "string",
  "rule_name": "string",
  "severity": "critical",
  "agent_id": "string",
  "deployment_id": "string",
  "session_id": "string",
  "event_count": 0,
  "first_seen": "string",
  "last_seen": "string",
  "status": "open",
  "status_updated_at": "string",
  "events": [
    {
      "detected_at": "string",
      "run_id": "string",
      "hook_name": "string",
      "tool_name": "string",
      "message": "string",
      "event_data": "string"
    }
  ],
  "pending_hold": {
    "hold_id": "string",
    "tool_name": "string",
    "args_summary": "string",
    "created_at": "2019-08-24T14:15:22Z"
  }
}

```

Threat detail response from GET /threats/{id}. Extends Threat with an optional pending_hold field.

### Properties

allOf

|Name|Type|Required|Restrictions|Description|
|---|---|---|---|---|
|*anonymous*|[Threat](#schemathreat)|false|none|A deduplicated security threat keyed by (agent_id, deployment_id, session_id, rule_id). Includes aggregated event counts and up to 50 individual detection events. Status lifecycle: open -> acknowledged -> cleared (auto-reopens on new events).|

and

|Name|Type|Required|Restrictions|Description|
|---|---|---|---|---|
|*anonymous*|object|false|none|none|
|» pending_hold|any|false|none|Present when a hold for this threat's rule+session is currently pending human decision.|

oneOf

|Name|Type|Required|Restrictions|Description|
|---|---|---|---|---|
|»» *anonymous*|[PendingHold](#schemapendinghold)|false|none|A hold that is currently pending human review for this threat's rule+session.|

xor

|Name|Type|Required|Restrictions|Description|
|---|---|---|---|---|
|»» *anonymous*|null|false|none|none|

<h2 id="tocS_ThreatStatusRequest">ThreatStatusRequest</h2>
<!-- backwards compatibility -->
<a id="schemathreatstatusrequest"></a>
<a id="schema_ThreatStatusRequest"></a>
<a id="tocSthreatstatusrequest"></a>
<a id="tocsthreatstatusrequest"></a>

```json
{
  "agent_id": "string",
  "deployment_id": "string",
  "session_id": "string",
  "rule_id": "string"
}

```

Request body for POST /threats/{id}/acknowledge and POST /threats/{id}/clear. All four fields are required; the server verifies that MD5(agent_id, deployment_id, session_id, rule_id) matches the :id path parameter.

### Properties

|Name|Type|Required|Restrictions|Description|
|---|---|---|---|---|
|agent_id|string|true|none|none|
|deployment_id|string|true|none|none|
|session_id|string|true|none|none|
|rule_id|string|true|none|none|

<h2 id="tocS_PendingHold">PendingHold</h2>
<!-- backwards compatibility -->
<a id="schemapendinghold"></a>
<a id="schema_PendingHold"></a>
<a id="tocSpendinghold"></a>
<a id="tocspendinghold"></a>

```json
{
  "hold_id": "string",
  "tool_name": "string",
  "args_summary": "string",
  "created_at": "2019-08-24T14:15:22Z"
}

```

A hold that is currently pending human review for this threat's rule+session.

### Properties

|Name|Type|Required|Restrictions|Description|
|---|---|---|---|---|
|hold_id|string|false|none|none|
|tool_name|string|false|none|none|
|args_summary|string|false|none|Truncated JSON of the tool arguments (max 2048 bytes).|
|created_at|string(date-time)|false|none|none|

<h2 id="tocS_CostResponse">CostResponse</h2>
<!-- backwards compatibility -->
<a id="schemacostresponse"></a>
<a id="schema_CostResponse"></a>
<a id="tocScostresponse"></a>
<a id="tocscostresponse"></a>

```json
{
  "month_to_date": 0,
  "forecasted": 0,
  "avg_token_cost": 0,
  "cache_savings": 0,
  "wasted_spend": 0,
  "daily_spend": [
    {
      "day": "string",
      "amount": 0
    }
  ],
  "token_breakdown": [
    {
      "label": "string",
      "percentage": 0,
      "tokens": 0,
      "amount": 0
    }
  ],
  "agent_model_costs": [
    {
      "model": "string",
      "agent_name": "string",
      "cost": 0,
      "requests": "string",
      "provider": "string",
      "agent_id": "string",
      "input_tokens": 0,
      "output_tokens": 0,
      "cache_tokens": 0
    }
  ]
}

```

Cost governance summary returned by GET /cost.

### Properties

|Name|Type|Required|Restrictions|Description|
|---|---|---|---|---|
|month_to_date|number|false|none|Total estimated cost in USD for the queried period.|
|forecasted|number|false|none|Projected month-end cost based on current run rate.|
|avg_token_cost|number|false|none|Average cost per token (USD).|
|cache_savings|number|false|none|Estimated savings from prompt cache hits (USD).|
|wasted_spend|number|false|none|Estimated cost of failed/retried calls (USD).|
|daily_spend|[object]|false|none|Daily cost time-series.|
|» day|string|false|none|Calendar date, YYYY-MM-DD.|
|» amount|number|false|none|Cost in USD.|
|token_breakdown|[object]|false|none|Cost breakdown by token type (Input, Output, Cache Read).|
|» label|string|false|none|none|
|» percentage|integer|false|none|none|
|» tokens|integer(int64)|false|none|none|
|» amount|number|false|none|none|
|agent_model_costs|[object]|false|none|Cost breakdown by agent and model.|
|» model|string|false|none|none|
|» agent_name|string|false|none|none|
|» cost|number|false|none|none|
|» requests|string|false|none|none|
|» provider|string|false|none|none|
|» agent_id|string|false|none|none|
|» input_tokens|integer(int64)|false|none|none|
|» output_tokens|integer(int64)|false|none|none|
|» cache_tokens|integer(int64)|false|none|none|

<h2 id="tocS_PricingEntry">PricingEntry</h2>
<!-- backwards compatibility -->
<a id="schemapricingentry"></a>
<a id="schema_PricingEntry"></a>
<a id="tocSpricingentry"></a>
<a id="tocspricingentry"></a>

```json
{
  "provider": "anthropic",
  "model": "claude-sonnet-4-6",
  "input_cost_per_million": 3,
  "output_cost_per_million": 15,
  "cache_read_cost_per_million": 0.3
}

```

Per-million-token pricing for one provider/model pair.

### Properties

|Name|Type|Required|Restrictions|Description|
|---|---|---|---|---|
|provider|string|true|none|Provider name, e.g. anthropic, openai.|
|model|string|true|none|Model identifier as it appears in hook events.|
|input_cost_per_million|number|true|none|Cost in USD per 1 million input tokens.|
|output_cost_per_million|number|true|none|Cost in USD per 1 million output tokens.|
|cache_read_cost_per_million|number|false|none|Cost in USD per 1 million cache-read tokens.|

<h2 id="tocS_RulePack">RulePack</h2>
<!-- backwards compatibility -->
<a id="schemarulepack"></a>
<a id="schema_RulePack"></a>
<a id="tocSrulepack"></a>
<a id="tocsrulepack"></a>

```json
{
  "id": "rp-001",
  "name": "Prompt Injection Shield",
  "version": "1.0.0",
  "description": "string",
  "query_sql": "string",
  "enabled": true,
  "severity": "critical",
  "category": "string",
  "required_telemetry": "string",
  "fp_rate": "low",
  "hook_sources": "string",
  "block_policy": "log_only",
  "detections_24h": 0,
  "latest_version": "string"
}

```

An installed security rule and its current state.

### Properties

|Name|Type|Required|Restrictions|Description|
|---|---|---|---|---|
|id|string|false|none|Rule identifier: rp-{N} for builtins, custom-{hash} for user-created.|
|name|string|false|none|none|
|version|string|false|none|none|
|description|string|false|none|none|
|query_sql|string|false|none|The Proton SELECT used by this rule's materialized view.|
|enabled|boolean|false|none|Whether the rule's MV is currently active.|
|severity|string|false|none|none|
|category|string|false|none|Rule category: prompt_injection, dlp, privilege, supply_chain, custom.|
|required_telemetry|string|false|none|Telemetry prerequisite note (e.g. requires OTel logs).|
|fp_rate|string|false|none|Expected false-positive rate.|
|hook_sources|string|false|none|Comma-separated list of hook names this rule inspects.|
|block_policy|string|false|none|Enforcement mode for holds integration.|
|detections_24h|integer|false|none|Number of security events generated by this rule in the last 24 hours.|
|latest_version|string|false|none|Catalog version if different from installed version (omitted when up to date).|

#### Enumerated Values

|Property|Value|
|---|---|
|severity|critical|
|severity|warning|
|severity|info|
|fp_rate|low|
|fp_rate|medium|
|fp_rate|high|
|block_policy|log_only|
|block_policy|hold|
|block_policy|auto_block|

<h2 id="tocS_RulesResponse">RulesResponse</h2>
<!-- backwards compatibility -->
<a id="schemarulesresponse"></a>
<a id="schema_RulesResponse"></a>
<a id="tocSrulesresponse"></a>
<a id="tocsrulesresponse"></a>

```json
{
  "rule_packs": [
    {
      "id": "rp-001",
      "name": "Prompt Injection Shield",
      "version": "1.0.0",
      "description": "string",
      "query_sql": "string",
      "enabled": true,
      "severity": "critical",
      "category": "string",
      "required_telemetry": "string",
      "fp_rate": "low",
      "hook_sources": "string",
      "block_policy": "log_only",
      "detections_24h": 0,
      "latest_version": "string"
    }
  ],
  "total_detections_24h": 0,
  "active_rule_density": 0,
  "recent_detections": [
    {
      "rule_id": "string",
      "rule_name": "string",
      "severity": "critical",
      "agent_id": "string",
      "hook_name": "string",
      "message": "string",
      "detected_at": "string"
    }
  ]
}

```

Response from GET /rules.

### Properties

|Name|Type|Required|Restrictions|Description|
|---|---|---|---|---|
|rule_packs|[[RulePack](#schemarulepack)]|false|none|[An installed security rule and its current state.]|
|total_detections_24h|integer|false|none|Sum of detections_24h across all installed rules.|
|active_rule_density|integer|false|none|Percentage of catalog rules that are currently enabled.|
|recent_detections|[[SecurityEvent](#schemasecurityevent)]|false|none|Latest security events across all rules.|

<h2 id="tocS_RuleDetailResponse">RuleDetailResponse</h2>
<!-- backwards compatibility -->
<a id="schemaruledetailresponse"></a>
<a id="schema_RuleDetailResponse"></a>
<a id="tocSruledetailresponse"></a>
<a id="tocsruledetailresponse"></a>

```json
{
  "id": "rp-001",
  "name": "Prompt Injection Shield",
  "version": "1.0.0",
  "description": "string",
  "query_sql": "string",
  "enabled": true,
  "severity": "critical",
  "category": "string",
  "required_telemetry": "string",
  "fp_rate": "low",
  "hook_sources": "string",
  "block_policy": "log_only",
  "detections_24h": 0,
  "latest_version": "string",
  "recent_detections": [
    {
      "rule_id": "string",
      "rule_name": "string",
      "severity": "critical",
      "agent_id": "string",
      "hook_name": "string",
      "message": "string",
      "detected_at": "string"
    }
  ]
}

```

Response from GET /rules/{id}. Extends RulePack with recent detections.

### Properties

allOf

|Name|Type|Required|Restrictions|Description|
|---|---|---|---|---|
|*anonymous*|[RulePack](#schemarulepack)|false|none|An installed security rule and its current state.|

and

|Name|Type|Required|Restrictions|Description|
|---|---|---|---|---|
|*anonymous*|object|false|none|none|
|» recent_detections|[[SecurityEvent](#schemasecurityevent)]|false|none|Recent detections triggered by this rule.|

<h2 id="tocS_SecurityEvent">SecurityEvent</h2>
<!-- backwards compatibility -->
<a id="schemasecurityevent"></a>
<a id="schema_SecurityEvent"></a>
<a id="tocSsecurityevent"></a>
<a id="tocssecurityevent"></a>

```json
{
  "rule_id": "string",
  "rule_name": "string",
  "severity": "critical",
  "agent_id": "string",
  "hook_name": "string",
  "message": "string",
  "detected_at": "string"
}

```

A single security detection event from agentguard_security_events.

### Properties

|Name|Type|Required|Restrictions|Description|
|---|---|---|---|---|
|rule_id|string|false|none|none|
|rule_name|string|false|none|none|
|severity|string|false|none|none|
|agent_id|string|false|none|none|
|hook_name|string|false|none|none|
|message|string|false|none|none|
|detected_at|string|false|none|none|

#### Enumerated Values

|Property|Value|
|---|---|
|severity|critical|
|severity|warning|
|severity|info|

<h2 id="tocS_CatalogItem">CatalogItem</h2>
<!-- backwards compatibility -->
<a id="schemacatalogitem"></a>
<a id="schema_CatalogItem"></a>
<a id="tocScatalogitem"></a>
<a id="tocscatalogitem"></a>

```json
{
  "id": "rp-001",
  "name": "Prompt Injection Shield",
  "version": "1.0.0",
  "description": "string",
  "severity": "critical",
  "category": "string",
  "fp_rate": "low",
  "hook_sources": "string",
  "required_telemetry": "string",
  "author": "Timeplus",
  "tags": [
    "security",
    "prompt-injection"
  ],
  "installed": true,
  "enabled": true,
  "installed_version": "1.0.0",
  "upgradeable": true
}

```

A rule definition from the file-based catalog, annotated with its current install state in Proton.

### Properties

|Name|Type|Required|Restrictions|Description|
|---|---|---|---|---|
|id|string|false|none|Rule identifier, e.g. rp-001 or custom-my-rule.|
|name|string|false|none|none|
|version|string|false|none|Catalog version (semver X.Y.Z).|
|description|string|false|none|none|
|severity|string|false|none|none|
|category|string|false|none|Rule category, e.g. prompt_injection, dlp, privilege, supply_chain.|
|fp_rate|string|false|none|Expected false-positive rate.|
|hook_sources|string|false|none|Comma-separated hook names this rule inspects.|
|required_telemetry|string|false|none|Optional telemetry prerequisite note.|
|author|string|false|none|Rule author, e.g. Timeplus.|
|tags|[string]|false|none|Labels for filtering. Orphaned rules (installed but absent from catalog) carry the 'orphaned' tag.|
|installed|boolean|false|none|True when this rule exists in agentguard_rules and is not deleted.|
|enabled|boolean|false|none|True when the rule is installed and its MV is active.|
|installed_version|string|false|none|Version string of the installed rule (omitted when not installed).|
|upgradeable|boolean|false|none|True when the catalog version is newer than the installed version — call /rules/{id}/upgrade to update.|

#### Enumerated Values

|Property|Value|
|---|---|
|severity|critical|
|severity|warning|
|severity|info|
|fp_rate|low|
|fp_rate|medium|
|fp_rate|high|

<h2 id="tocS_RuleInput">RuleInput</h2>
<!-- backwards compatibility -->
<a id="schemaruleinput"></a>
<a id="schema_RuleInput"></a>
<a id="tocSruleinput"></a>
<a id="tocsruleinput"></a>

```json
{
  "id": "string",
  "name": "string",
  "description": "string",
  "query_sql": "string",
  "severity": "critical",
  "category": "string",
  "fp_rate": "low",
  "hook_sources": "string"
}

```

Request body for creating a custom rule.

### Properties

|Name|Type|Required|Restrictions|Description|
|---|---|---|---|---|
|id|string|false|none|Optional user-specified rule ID (letters, digits, hyphens, underscores; max 64 chars). Must be unique across the catalog. Auto-generated with a custom- prefix when omitted.|
|name|string|true|none|Display name for the rule.|
|description|string|false|none|none|
|query_sql|string|true|none|Proton SELECT targeting agentguard_hook_events. Must output all agentguard_security_events columns.|
|severity|string|true|none|none|
|category|string|true|none|Logical category for grouping, e.g. dlp, privilege.|
|fp_rate|string|false|none|Expected false-positive rate.|
|hook_sources|string|false|none|Comma-separated hook names for documentation purposes.|

#### Enumerated Values

|Property|Value|
|---|---|
|severity|critical|
|severity|warning|
|severity|info|
|fp_rate|low|
|fp_rate|medium|
|fp_rate|high|

<h2 id="tocS_ImportResult">ImportResult</h2>
<!-- backwards compatibility -->
<a id="schemaimportresult"></a>
<a id="schema_ImportResult"></a>
<a id="tocSimportresult"></a>
<a id="tocsimportresult"></a>

```json
{
  "added": [
    "string"
  ],
  "updated": [
    "string"
  ],
  "skipped": [
    "string"
  ],
  "errors": [
    "string"
  ]
}

```

Summary of a catalog import operation.

### Properties

|Name|Type|Required|Restrictions|Description|
|---|---|---|---|---|
|added|[string]|false|none|Rule IDs that were newly added to the catalog.|
|updated|[string]|false|none|Rule IDs whose catalog entry was overwritten (newer version or force=true).|
|skipped|[string]|false|none|Rule IDs that were skipped because the imported version was not newer than the existing entry.|
|errors|[string]|false|none|Parse or write errors encountered per file (non-fatal — other files are still processed).|

<h2 id="tocS_Alert">Alert</h2>
<!-- backwards compatibility -->
<a id="schemaalert"></a>
<a id="schema_Alert"></a>
<a id="tocSalert"></a>
<a id="tocsalert"></a>

```json
{
  "rule_id": "string",
  "rule_name": "string",
  "severity": "critical",
  "agent_id": "string",
  "deployment_id": "string",
  "message": "string",
  "detected_at": "string"
}

```

Alert payload posted by the Timeplus UDF and pushed to SSE clients.

### Properties

|Name|Type|Required|Restrictions|Description|
|---|---|---|---|---|
|rule_id|string|false|none|none|
|rule_name|string|false|none|none|
|severity|string|false|none|none|
|agent_id|string|false|none|none|
|deployment_id|string|false|none|none|
|message|string|false|none|none|
|detected_at|string|false|none|Detection timestamp (string, as sent by Timeplus UDF).|

#### Enumerated Values

|Property|Value|
|---|---|
|severity|critical|
|severity|warning|
|severity|info|

<h2 id="tocS_AuthStatus">AuthStatus</h2>
<!-- backwards compatibility -->
<a id="schemaauthstatus"></a>
<a id="schema_AuthStatus"></a>
<a id="tocSauthstatus"></a>
<a id="tocsauthstatus"></a>

```json
{
  "setup_complete": true,
  "logged_in": true,
  "username": "string"
}

```

Server setup and session state returned by GET /auth/status.

### Properties

|Name|Type|Required|Restrictions|Description|
|---|---|---|---|---|
|setup_complete|boolean|false|none|True when the setup wizard has been completed and config.yaml contains setup_complete=true.|
|logged_in|boolean|false|none|True when the request carries a valid agentguard_session cookie.|
|username|string|false|none|Timeplus username of the active session. Empty when logged_in=false.|

<h2 id="tocS_LoginRequest">LoginRequest</h2>
<!-- backwards compatibility -->
<a id="schemaloginrequest"></a>
<a id="schema_LoginRequest"></a>
<a id="tocSloginrequest"></a>
<a id="tocsloginrequest"></a>

```json
{
  "username": "default",
  "password": ""
}

```

Credentials for POST /auth/login.

### Properties

|Name|Type|Required|Restrictions|Description|
|---|---|---|---|---|
|username|string|true|none|Timeplus username.|
|password|string|true|none|Timeplus password. May be an empty string for password-less deployments.|

<h2 id="tocS_LoginResponse">LoginResponse</h2>
<!-- backwards compatibility -->
<a id="schemaloginresponse"></a>
<a id="schema_LoginResponse"></a>
<a id="tocSloginresponse"></a>
<a id="tocsloginresponse"></a>

```json
{
  "username": "default"
}

```

Response body for a successful POST /auth/login.

### Properties

|Name|Type|Required|Restrictions|Description|
|---|---|---|---|---|
|username|string|false|none|Timeplus username of the newly created session.|

<h2 id="tocS_TestConnectionRequest">TestConnectionRequest</h2>
<!-- backwards compatibility -->
<a id="schematestconnectionrequest"></a>
<a id="schema_TestConnectionRequest"></a>
<a id="tocStestconnectionrequest"></a>
<a id="tocstestconnectionrequest"></a>

```json
{
  "host": "localhost",
  "port": 3218,
  "username": "default",
  "password": ""
}

```

Connection parameters for POST /setup/test-connection.

### Properties

|Name|Type|Required|Restrictions|Description|
|---|---|---|---|---|
|host|string|true|none|Timeplus hostname or IP.|
|port|integer|true|none|Timeplus HTTP API port.|
|username|string|true|none|Timeplus username.|
|password|string|false|none|Timeplus password.|

<h2 id="tocS_ProvisionRequest">ProvisionRequest</h2>
<!-- backwards compatibility -->
<a id="schemaprovisionrequest"></a>
<a id="schema_ProvisionRequest"></a>
<a id="tocSprovisionrequest"></a>
<a id="tocsprovisionrequest"></a>

```json
{
  "host": "localhost",
  "port": 3218,
  "native_port": 8463,
  "username": "default",
  "password": "",
  "agentguard_url": "http://localhost:8080"
}

```

Full connection details for POST /setup/provision.

### Properties

|Name|Type|Required|Restrictions|Description|
|---|---|---|---|---|
|host|string|true|none|Timeplus hostname or IP.|
|port|integer|true|none|Timeplus HTTP API port.|
|native_port|integer|true|none|Timeplus native TCP port.|
|username|string|true|none|Timeplus username.|
|password|string|false|none|Timeplus password.|
|agentguard_url|string|false|none|URL where AgentGuard is reachable from Timeplus's network — used for alert webhook callbacks.|

<h2 id="tocS_ProvisionEvent">ProvisionEvent</h2>
<!-- backwards compatibility -->
<a id="schemaprovisionevent"></a>
<a id="schema_ProvisionEvent"></a>
<a id="tocSprovisionevent"></a>
<a id="tocsprovisionevent"></a>

```json
{
  "status": "running",
  "msg": "string"
}

```

A single SSE event from POST /setup/provision.

### Properties

|Name|Type|Required|Restrictions|Description|
|---|---|---|---|---|
|status|string|false|none|running=step in progress, ok=step succeeded, error=step failed (provisioning continues), done=all steps complete.|
|msg|string|false|none|Human-readable message describing the provisioning step.|

#### Enumerated Values

|Property|Value|
|---|---|
|status|running|
|status|ok|
|status|error|
|status|done|

<h2 id="tocS_Hold">Hold</h2>
<!-- backwards compatibility -->
<a id="schemahold"></a>
<a id="schema_Hold"></a>
<a id="tocShold"></a>
<a id="tocshold"></a>

```json
{
  "hold_id": "string",
  "deployment_id": "string",
  "agent_id": "string",
  "session_id": "string",
  "run_id": "string",
  "tool_name": "string",
  "rule_id": "string",
  "severity": "critical",
  "args_summary": "string",
  "message": "string",
  "created_at": "2019-08-24T14:15:22Z",
  "status": "pending",
  "decided_by": "string",
  "decided_at": "2019-08-24T14:15:22Z",
  "decided_reason": "string"
}

```

A hold record from agentguard_holds. Current state is computed inline via arg_max(field, updated_at) GROUP BY hold_id.

### Properties

|Name|Type|Required|Restrictions|Description|
|---|---|---|---|---|
|hold_id|string|false|none|UUID for this hold.|
|deployment_id|string|false|none|none|
|agent_id|string|false|none|none|
|session_id|string|false|none|none|
|run_id|string|false|none|none|
|tool_name|string|false|none|The tool call that was intercepted.|
|rule_id|string|false|none|The rule that triggered the hold.|
|severity|string|false|none|none|
|args_summary|string|false|none|Truncated JSON of tool_input (max 2048 bytes).|
|message|string|false|none|Human-readable reason the hold was created.|
|created_at|string(date-time)|false|none|none|
|status|string|false|none|none|
|decided_by|string|false|none|Email or identifier of the person who decided the hold. Empty for system decisions.|
|decided_at|string(date-time)|false|none|none|
|decided_reason|string|false|none|none|

#### Enumerated Values

|Property|Value|
|---|---|
|severity|critical|
|severity|warning|
|severity|info|
|status|pending|
|status|approved|
|status|denied|
|status|timeout|
|status|abandoned|

<h2 id="tocS_HoldRequest">HoldRequest</h2>
<!-- backwards compatibility -->
<a id="schemaholdrequest"></a>
<a id="schema_HoldRequest"></a>
<a id="tocSholdrequest"></a>
<a id="tocsholdrequest"></a>

```json
{
  "deployment_id": "string",
  "agent_id": "string",
  "session_id": "string",
  "run_id": "string",
  "tool_name": "string",
  "tool_input": {},
  "event_time": "2019-08-24T14:15:22Z",
  "hook_payload": {},
  "agent_type": "claudecode",
  "deployment_name": "string"
}

```

Request body sent by agent plugins to POST /holds.

### Properties

|Name|Type|Required|Restrictions|Description|
|---|---|---|---|---|
|deployment_id|string|false|none|none|
|agent_id|string|false|none|none|
|session_id|string|false|none|none|
|run_id|string|false|none|none|
|tool_name|string|false|none|none|
|tool_input|object|false|none|Full tool arguments as a JSON object.|
|event_time|string(date-time)|false|none|Timestamp when the PreToolUse hook fired.|
|hook_payload|object|false|none|Full raw hook payload from the plugin.|
|agent_type|string|false|none|Plugin type. Defaults to claudecode if empty.|
|deployment_name|string|false|none|Human-readable deployment name (optional).|

#### Enumerated Values

|Property|Value|
|---|---|
|agent_type|claudecode|
|agent_type|openclaw|

<h2 id="tocS_HoldResponse">HoldResponse</h2>
<!-- backwards compatibility -->
<a id="schemaholdresponse"></a>
<a id="schema_HoldResponse"></a>
<a id="tocSholdresponse"></a>
<a id="tocsholdresponse"></a>

```json
{
  "decision": "allow",
  "reason": "string",
  "hold_id": "string"
}

```

Response returned by POST /holds to the waiting plugin.

### Properties

|Name|Type|Required|Restrictions|Description|
|---|---|---|---|---|
|decision|string|false|none|Whether the tool call should proceed.|
|reason|string|false|none|Human-readable explanation (populated for block decisions).|
|hold_id|string|false|none|UUID of the hold record opened for human review (only present when a hold was created).|

#### Enumerated Values

|Property|Value|
|---|---|
|decision|allow|
|decision|block|

<h2 id="tocS_HoldDecideRequest">HoldDecideRequest</h2>
<!-- backwards compatibility -->
<a id="schemaholddeciderequest"></a>
<a id="schema_HoldDecideRequest"></a>
<a id="tocSholddeciderequest"></a>
<a id="tocsholddeciderequest"></a>

```json
{
  "decision": "approved",
  "decided_by": "string",
  "decided_reason": "string"
}

```

Request body for POST /holds/{hold_id}/decide.

### Properties

|Name|Type|Required|Restrictions|Description|
|---|---|---|---|---|
|decision|string|true|none|Human decision: approved allows the tool call, denied blocks it.|
|decided_by|string|false|none|Email or display name of the reviewer.|
|decided_reason|string|false|none|Optional explanation for audit trail.|

#### Enumerated Values

|Property|Value|
|---|---|
|decision|approved|
|decision|denied|

<h2 id="tocS_ActivityEvent">ActivityEvent</h2>
<!-- backwards compatibility -->
<a id="schemaactivityevent"></a>
<a id="schema_ActivityEvent"></a>
<a id="tocSactivityevent"></a>
<a id="tocsactivityevent"></a>

```json
{
  "id": "string",
  "event_time": "2019-08-24T14:15:22Z",
  "tool_name": "string",
  "status": "pending",
  "rule_id": "string",
  "rule_name": "string",
  "severity": "critical",
  "agent_id": "string",
  "session_id": "string",
  "run_id": "string",
  "args_summary": "string",
  "message": "string",
  "decided_by": "string",
  "decided_at": "2019-08-24T14:15:22Z",
  "decided_reason": "string",
  "hook_payload": "string",
  "is_hold_event": true
}

```

One event in the activity feed. Sourced from agentguard_holds (is_hold_event=true) or agentguard_hook_events when include_allowed=true (is_hold_event=false).

### Properties

|Name|Type|Required|Restrictions|Description|
|---|---|---|---|---|
|id|string|false|none|hold_id for hold events; synthetic hex hash for hook-only events.|
|event_time|string(date-time)|false|none|none|
|tool_name|string|false|none|none|
|status|string|false|none|For hold events: the hold lifecycle status. For hook-only events: always allowed.|
|rule_id|string|false|none|none|
|rule_name|string|false|none|none|
|severity|string|false|none|none|
|agent_id|string|false|none|none|
|session_id|string|false|none|none|
|run_id|string|false|none|none|
|args_summary|string|false|none|Truncated JSON of tool arguments.|
|message|string|false|none|none|
|decided_by|string|false|none|none|
|decided_at|string(date-time)¦null|false|none|none|
|decided_reason|string|false|none|none|
|hook_payload|string|false|none|Raw event_data JSON; only populated for hook-only (allowed) events.|
|is_hold_event|boolean|false|none|True when this event was sourced from agentguard_holds.|

#### Enumerated Values

|Property|Value|
|---|---|
|status|pending|
|status|approved|
|status|denied|
|status|timeout|
|status|abandoned|
|status|auto_block|
|status|allowed|
|severity|critical|
|severity|warning|
|severity|info|
|severity||

<h2 id="tocS_ActivityResponse">ActivityResponse</h2>
<!-- backwards compatibility -->
<a id="schemaactivityresponse"></a>
<a id="schema_ActivityResponse"></a>
<a id="tocSactivityresponse"></a>
<a id="tocsactivityresponse"></a>

```json
{
  "events": [
    {
      "id": "string",
      "event_time": "2019-08-24T14:15:22Z",
      "tool_name": "string",
      "status": "pending",
      "rule_id": "string",
      "rule_name": "string",
      "severity": "critical",
      "agent_id": "string",
      "session_id": "string",
      "run_id": "string",
      "args_summary": "string",
      "message": "string",
      "decided_by": "string",
      "decided_at": "2019-08-24T14:15:22Z",
      "decided_reason": "string",
      "hook_payload": "string",
      "is_hold_event": true
    }
  ],
  "total": 0
}

```

Response from GET /activity.

### Properties

|Name|Type|Required|Restrictions|Description|
|---|---|---|---|---|
|events|[[ActivityEvent](#schemaactivityevent)]|false|none|[One event in the activity feed. Sourced from agentguard_holds (is_hold_event=true) or agentguard_hook_events when include_allowed=true (is_hold_event=false).]|
|total|integer|false|none|none|

