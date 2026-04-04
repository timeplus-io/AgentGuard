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
  getAgent: (id) => get(`/agents/${id}`),
  listThreats: (params = {}) => {
    const q = new URLSearchParams(params).toString()
    return get(`/threats${q ? '?' + q : ''}`)
  },
  getCost: () => get('/cost'),
  listRules: () => get('/rules'),
  toggleRule: (id) => post(`/rules/${id}/toggle`, {}),
  listTables: () => get('/investigate/tables'),
  runQuery: (query) => post('/investigate/query', { query }),
  getSettings: () => get('/settings'),
  updateSettings: (data) => put('/settings', data),
  generateConfig: (data) => post('/onboarding/generate', data),
}
