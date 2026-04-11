import { ndjsonStreamParser } from '@timeplus/proton-javascript-driver'

const BASE = '/api'

async function get(path) {
  const res = await fetch(`${BASE}${path}`)
  if (!res.ok) throw new Error(`HTTP ${res.status}`)
  return res.json()
}

async function post(path, body) {
  const res = await fetch(`${BASE}${path}`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(body),
  })
  if (!res.ok) throw new Error(`HTTP ${res.status}`)
  return res.json()
}

async function put(path, body) {
  const res = await fetch(`${BASE}${path}`, {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(body),
  })
  if (!res.ok) throw new Error(`HTTP ${res.status}`)
  return res.json()
}

export const api = {
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
  deletePricing: (provider, model) =>
    fetch(`${BASE}/cost/pricing/${encodeURIComponent(provider)}/${encodeURIComponent(model)}`, { method: 'DELETE' })
      .then(res => { if (!res.ok) throw new Error(`HTTP ${res.status}`); return res.json() }),
  listRules: () => get('/rules'),
  getRulesCatalog: () => get('/rules/catalog'),
  importCatalog: (file, force = false) => {
    const fd = new FormData()
    fd.append('file', file)
    return fetch(`${BASE}/rules/catalog/import${force ? '?force=true' : ''}`, { method: 'POST', body: fd })
      .then(res => { if (!res.ok) throw new Error(`HTTP ${res.status}`); return res.json() })
  },
  exportCatalogUrl: () => `${BASE}/rules/catalog/export`,
  getRule: (id) => get(`/rules/${id}`),
  toggleRule: (id) => post(`/rules/${id}/toggle`, {}),
  upgradeRule: (id, querySql) => post(`/rules/${id}/upgrade`, { query_sql: querySql }),
  createRule: (body) => post('/rules', body),
  installRule: (id) => post(`/rules/${id}/install`, {}),
  uninstallRule: (id) =>
    fetch(`${BASE}/rules/${id}`, { method: 'DELETE' })
      .then(res => { if (!res.ok) throw new Error(`HTTP ${res.status}`); return res.json() }),
  listTables: () => get('/investigate/tables'),
  runQuery: (query) => post('/investigate/query', { query }),
  generateConfig: (data) => post('/onboarding/generate', data),

  // Streams rows from Proton/Timeplus via the backend proxy.
  // Connection config lives server-side (config.yaml / env vars).
  // Pass an AbortSignal via options.signal to cancel the stream.
  async *protonQuery(sql, { signal } = {}) {
    const res = await fetch(`${BASE}/proton/query`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ sql }),
      signal,
    })
    if (!res.ok) {
      const err = await res.json().catch(() => ({}))
      throw new Error(err.error || `HTTP ${res.status}`)
    }
    yield* ndjsonStreamParser(res.body.getReader())
  },
}
