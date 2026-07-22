import { ndjsonStreamParser } from '@timeplus/proton-javascript-driver'

const BASE = '/api'

// throwResponseError builds an Error from a non-OK response, attaching the HTTP
// status and the parsed JSON body so callers can read structured detail (e.g. a
// 400's per-parameter `fields`, or a 409 upgrade's `conflicts`/`warnings`). It
// also surfaces `errors` (the config UI's per-key validation map) directly on the
// error for convenience.
async function throwResponseError(res) {
  let msg = `HTTP ${res.status}`
  let body = null
  try {
    body = await res.json()
    if (body?.error) msg = body.error
  } catch { /* use status */ }
  const err = new Error(msg)
  err.status = res.status
  err.body = body
  err.errors = body?.errors || null // per-key validation errors (config UI)
  throw err
}

async function get(path) {
  const res = await fetch(`${BASE}${path}`, { credentials: 'include' })
  if (!res.ok) await throwResponseError(res)
  return res.json()
}

async function send(method, path, body) {
  const res = await fetch(`${BASE}${path}`, {
    method,
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(body ?? {}),
    credentials: 'include',
  })
  if (!res.ok) await throwResponseError(res)
  return res.json()
}

const post = (path, body) => send('POST', path, body)
const put = (path, body) => send('PUT', path, body)

async function del(path) {
  const res = await fetch(`${BASE}${path}`, { method: 'DELETE', credentials: 'include' })
  if (!res.ok) await throwResponseError(res)
  return res.json()
}

export const api = {
  // ── Auth ────────────────────────────────────────────────────────────────────
  authStatus: () => get('/auth/status'),
  authLogin: (username, password) => post('/auth/login', { username, password }),
  authLogout: () => post('/auth/logout', {}),

  // ── Setup ───────────────────────────────────────────────────────────────────
  setupTestConnection: (host, port, username, password) =>
    post('/setup/test-connection', { host, port, username, password }),

  // setupProvision returns an async generator consuming the SSE stream.
  // Usage: for await (const event of api.setupProvision(body)) { ... }
  async *setupProvision(body) {
    const res = await fetch(`${BASE}/setup/provision`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(body),
      credentials: 'include',
    })
    if (!res.ok) throw new Error(`HTTP ${res.status}`)
    const reader = res.body.getReader()
    const decoder = new TextDecoder()
    let buffer = ''
    while (true) {
      const { value, done } = await reader.read()
      if (done) break
      buffer += decoder.decode(value, { stream: true })
      const parts = buffer.split('\n\n')
      buffer = parts.pop() // keep incomplete chunk
      for (const part of parts) {
        const line = part.trim()
        if (line.startsWith('data: ')) {
          try { yield JSON.parse(line.slice(6)) } catch { /* skip malformed */ }
        }
      }
    }
  },

  // ── Existing endpoints ────────────────────────────────────────────────────
  getDashboard: () => get('/dashboard'),
  listAgents: (page = 1, limit = 15) => get(`/agents?page=${page}&limit=${limit}`),
  getAgent: (id, params = {}) => {
    const q = new URLSearchParams(params).toString()
    return get(`/agents/${id}${q ? '?' + q : ''}`)
  },
  listSessions: (agentId, params = {}) => {
    const q = new URLSearchParams(params).toString()
    return get(`/agents/${agentId}/sessions${q ? '?' + q : ''}`)
  },
  getSession: (agentId, sessionId) => get(`/agents/${agentId}/sessions/${sessionId}`),
  getRunTrace: (agentId, sessionId, runId) =>
    get(`/agents/${agentId}/sessions/${sessionId}/runs/${encodeURIComponent(runId)}/trace`),
  getSessionTimeline: (agentId, sessionId, params = {}) => {
    const q = new URLSearchParams(params).toString()
    return get(`/agents/${agentId}/sessions/${sessionId}/timeline${q ? '?' + q : ''}`)
  },
  listEvents: (agentId, params = {}) => {
    const q = new URLSearchParams(params).toString()
    return get(`/agents/${agentId}/events${q ? '?' + q : ''}`)
  },
  getLLMMetrics: (params = {}) => {
    const q = new URLSearchParams(params).toString()
    return get(`/metrics/llm${q ? '?' + q : ''}`)
  },
  getToolMetrics: (params = {}) => {
    const q = new URLSearchParams(params).toString()
    return get(`/metrics/tools${q ? '?' + q : ''}`)
  },
  getThreats: (params = {}) => {
    const q = new URLSearchParams(params).toString()
    return get(`/threats${q ? '?' + q : ''}`)
  },
  acknowledgeThreat: (id, body) => post(`/threats/${id}/acknowledge`, body),
  clearThreat: (id, body) => post(`/threats/${id}/clear`, body),
  getThreat: (id) => get(`/threats/${id}`),
  getCost: (params = {}) => {
    const q = new URLSearchParams(params).toString()
    return get(`/cost${q ? '?' + q : ''}`)
  },
  listPricing: () => get('/cost/pricing'),
  upsertPricing: (entry) => post('/cost/pricing', entry),
  deletePricing: (provider, model) => del(`/cost/pricing/${encodeURIComponent(provider)}/${encodeURIComponent(model)}`),
  getCostBudgets: () => get('/cost/budget'),
  upsertCostBudget: (entry) => post('/cost/budget', entry),
  listRules: () => get('/rules'),
  getRulesCatalog: () => get('/rules/catalog'),
  importCatalog: (file, force = false) => {
    const fd = new FormData()
    fd.append('file', file)
    return fetch(`${BASE}/rules/catalog/import${force ? '?force=true' : ''}`, {
      method: 'POST',
      body: fd,
      credentials: 'include',
    }).then(async res => {
      if (!res.ok) {
        let msg = `HTTP ${res.status}`
        try { const j = await res.json(); if (j?.error) msg = j.error } catch { /* use status */ }
        throw new Error(msg)
      }
      return res.json()
    })
  },
  exportCatalogUrl: () => `${BASE}/rules/catalog/export`,
  // Clone a catalog definition as a new custom definition with rescoped UDFs.
  cloneCatalogRule: (srcId, body) => post(`/rules/catalog/${encodeURIComponent(srcId)}/clone`, body),
  // Read one catalog definition (full schema, including config[] parameters).
  getCatalogRule: (id) => get(`/rules/catalog/${encodeURIComponent(id)}`),
  // Pure validate+compile preview: returns { rendered_sql, definition_hash,
  // effective, overrides }. No catalog write, no MV, no Timeplus.
  compileCatalogRule: (definition, overrides) => post('/rules/catalog/compile', { definition, overrides }),
  // Create a custom definition (origin forced to custom). Does not install.
  createCatalogRule: (definition) => post('/rules/catalog', definition),
  // Edit a custom definition; atomically republishes + redeploys an installed MV.
  updateCatalogRule: (id, definition) => put(`/rules/catalog/${encodeURIComponent(id)}`, definition),
  // Delete a LOCAL (custom/imported) catalog definition that is not installed.
  // Rejects builtins (409) and installed rules (409 — uninstall first); the
  // thrown error's .status/.message carry the reason.
  deleteCatalogRule: (id) => del(`/rules/catalog/${encodeURIComponent(id)}`),
  getRule: (id) => get(`/rules/${id}`),
  toggleRule: (id) => post(`/rules/${id}/toggle`, {}),
  // Upgrade is bodyless and catalog-only; on 409 the thrown error's .body carries
  // { conflicts, warnings }.
  upgradeRule: (id) => post(`/rules/${id}/upgrade`, {}),
  createRule: (body) => post('/rules', body),
  // Install with optional typed parameter overrides (omit for defaults).
  installRule: (id, overrides) => post(`/rules/${id}/install`, overrides ? { overrides } : {}),
  // Replace the full parameter override map; omitted keys reset to defaults.
  configureRule: (id, overrides, reason) =>
    put(`/rules/${id}/configuration`, { overrides, reason }),
  uninstallRule: (id) => del(`/rules/${id}`),
  listTables: () => get('/investigate/tables'),
  generateConfig: (data) => post('/onboarding/generate', data),
  getInstallScript: () => fetch('/api/onboarding/install-script'),

  // ── Holds (tool-call blocking) ─────────────────────────────────────────────
  listHolds: (status = 'pending', limit = 100) =>
    get(`/holds?status=${encodeURIComponent(status)}&limit=${limit}`),
  getHold: (holdId) => get(`/holds/${encodeURIComponent(holdId)}`),
  decideHold: (holdId, decision, reason = '') =>
    post(`/holds/${encodeURIComponent(holdId)}/decide`, {
      decision,
      decided_by: 'web-ui',
      decided_reason: reason,
    }),
  getActivity: (params = {}) => {
    const q = new URLSearchParams(
      Object.fromEntries(Object.entries(params).filter(([, v]) => v !== undefined && v !== '' && v !== false))
    ).toString()
    return get(`/activity${q ? '?' + q : ''}`)
  },
  setRuleBlockPolicy: (ruleId, blockPolicy) =>
    post(`/rules/${encodeURIComponent(ruleId)}/policy`, { block_policy: blockPolicy }),
  getThreatDetail: (threatId) => get(`/threats/${encodeURIComponent(threatId)}`),

  // ── Semantic DLP ───────────────────────────────────────────────────────────
  dlpStatus: () => get('/dlp/status'),
  dlpProfiles: (days = 30) => get(`/dlp/profiles?days=${days}`),
  dlpMessages: (params = {}) => {
    const q = new URLSearchParams(
      Object.fromEntries(Object.entries(params).filter(([, v]) => v !== undefined && v !== null && v !== ''))
    ).toString()
    return get(`/dlp/messages${q ? '?' + q : ''}`)
  },
  dlpMessage: (id) => get(`/dlp/messages/${encodeURIComponent(id)}`),
  syncRules: () => post('/rules/sync', {}),

  // Runtime DLP config stream (agentguard_dlp_config): per-UDF rows + shared row.
  getDLPConfig: () => get('/dlp/config'),
  putDLPConfig: (key, value) => put(`/dlp/config/${encodeURIComponent(key)}`, { value }),

  // ── Configuration (Settings page) ───────────────────────────────────────────
  getConfig: () => get('/config'),
  updateConfig: (changes) => put('/config', changes),
  testTimeplusConfig: (host, port) => post('/config/test-timeplus', { host, port }),
  testDLPConfig: (payload) => post('/config/test-dlp', payload),

  // dlpQuery streams NDJSON stage events from the forensic funnel:
  // {stage:"intent"} → {stage:"candidates"} → {stage:"reranked"} → {stage:"answer"}
  // or a terminal {stage:"error"}.
  async *dlpQuery(question, { topN, signal } = {}) {
    const res = await fetch(`${BASE}/dlp/query`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(topN ? { question, top_n: topN } : { question }),
      signal,
      credentials: 'include',
    })
    if (!res.ok) {
      const err = await res.json().catch(() => ({}))
      throw new Error(err.error || `HTTP ${res.status}`)
    }
    yield* ndjsonStreamParser(res.body.getReader())
  },

  // ── Sentry (agent chat) ────────────────────────────────────────────────────
  sentryStatus: () => get('/sentry/status'),
  sentryConversations: () => get('/sentry/conversations'),
  sentryConversation: (id) => get(`/sentry/conversations/${encodeURIComponent(id)}`),
  sentryArchive: (id) => del(`/sentry/conversations/${encodeURIComponent(id)}`),

  // sentryChat streams NDJSON events from the agent loop:
  // {event:"meta"} → {event:"token"|"tool_call"|"tool_result"}* → {event:"done"} | {event:"error"}
  async *sentryChat(message, { conversationId, signal } = {}) {
    const res = await fetch(`${BASE}/sentry/chat`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(conversationId ? { conversation_id: conversationId, message } : { message }),
      signal,
      credentials: 'include',
    })
    if (!res.ok) {
      const err = await res.json().catch(() => ({}))
      throw new Error(err.error || `HTTP ${res.status}`)
    }
    yield* ndjsonStreamParser(res.body.getReader())
  },

  async *protonQuery(sql, { signal } = {}) {
    const res = await fetch(`${BASE}/proton/query`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ sql }),
      signal,
      credentials: 'include',
    })
    if (!res.ok) {
      const err = await res.json().catch(() => ({}))
      throw new Error(err.error || `HTTP ${res.status}`)
    }
    yield* ndjsonStreamParser(res.body.getReader())
  },
}
