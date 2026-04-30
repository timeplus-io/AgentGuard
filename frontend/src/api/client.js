import { ndjsonStreamParser } from '@timeplus/proton-javascript-driver'

const BASE = '/api'

async function get(path) {
  const res = await fetch(`${BASE}${path}`, { credentials: 'include' })
  if (!res.ok) {
    let msg = `HTTP ${res.status}`
    try { const j = await res.json(); if (j?.error) msg = j.error } catch { /* use status */ }
    throw new Error(msg)
  }
  return res.json()
}

async function post(path, body) {
  const res = await fetch(`${BASE}${path}`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(body),
    credentials: 'include',
  })
  if (!res.ok) {
    let msg = `HTTP ${res.status}`
    try { const j = await res.json(); if (j?.error) msg = j.error } catch { /* use status */ }
    throw new Error(msg)
  }
  return res.json()
}

async function del(path) {
  const res = await fetch(`${BASE}${path}`, { method: 'DELETE', credentials: 'include' })
  if (!res.ok) throw new Error(`HTTP ${res.status}`)
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
  getRule: (id) => get(`/rules/${id}`),
  toggleRule: (id) => post(`/rules/${id}/toggle`, {}),
  upgradeRule: (id, querySql) => post(`/rules/${id}/upgrade`, { query_sql: querySql }),
  createRule: (body) => post('/rules', body),
  installRule: (id) => post(`/rules/${id}/install`, {}),
  uninstallRule: (id) => del(`/rules/${id}`),
  listTables: () => get('/investigate/tables'),
  generateConfig: (data) => post('/onboarding/generate', data),

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
