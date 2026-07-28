import { useState, useEffect, useMemo } from 'react'
import { BarChart, Bar, XAxis, YAxis, Tooltip, ResponsiveContainer, Cell } from 'recharts'
import { useTheme } from '../context/ThemeContext'
import { useAuth } from '../context/AuthContext'
import { writeKey } from '../lib/perm'
import Sidebar from '../components/Sidebar'
import TopNav from '../components/TopNav'
import PageHeader from '../components/PageHeader'
import { api } from '../api/client'

const CustomTooltip = ({ active, payload, label }) => {
  if (active && payload && payload.length) {
    return (
      <div className="bg-surface-container-high border border-outline-variant/20 px-3 py-2 rounded-sm text-xs">
        <p className="text-gray-400 font-semibold">{label}</p>
        <p className="text-primary font-bold">${payload[0].value.toLocaleString(undefined, { minimumFractionDigits: 2, maximumFractionDigits: 2 })}</p>
      </div>
    )
  }
  return null
}

function formatDollar(n) {
  if (n == null) return '$0.00'
  return '$' + Number(n).toLocaleString(undefined, { minimumFractionDigits: 2, maximumFractionDigits: 2 })
}


const TOKEN_COLORS = {
  'Input Tokens':  'bg-[#D53F8C]',
  'Output Tokens': 'bg-[#ffb0cf]',
  'Cache Tokens':  'bg-[#ffb2b8]',
}

const EMPTY_FORM = { provider: '', model: '', input_cost_per_million: '', output_cost_per_million: '', cache_read_cost_per_million: '' }
const EMPTY_BUDGET = { deployment_id: '', monthly_budget: '', currency: 'USD' }

// Delta-flat band: when |pct| < 0.1, the change is noise — render no arrow icon
// and a neutral color so the tile doesn't claim a trend that isn't there.
const DELTA_FLAT_BAND_PCT = 0.1

// periodDeltaProps builds {delta, deltaColor, deltaIcon} from a prior-period
// delta percent. Returns an empty object when pct is null/undefined/NaN — the
// caller spreads it into the kpi object, so the delta row is omitted entirely.
// lowerIsBetter=true means a negative delta (spend went down) renders green.
function periodDeltaProps(pct, lowerIsBetter = true) {
  if (pct == null || !Number.isFinite(pct)) return {}
  const sign = pct > 0 ? '+' : ''
  const text = `${sign}${pct.toFixed(1)}% vs last month-to-date`
  if (Math.abs(pct) < DELTA_FLAT_BAND_PCT) {
    return { delta: text, deltaColor: 'text-gray-400', deltaIcon: null }
  }
  if (pct < 0) {
    return {
      delta: text,
      deltaColor: lowerIsBetter ? 'text-emerald-400' : 'text-error',
      deltaIcon: 'trending_down',
    }
  }
  return {
    delta: text,
    deltaColor: lowerIsBetter ? 'text-error' : 'text-emerald-400',
    deltaIcon: 'trending_up',
  }
}

// budgetDeltaProps renders the Forecasted Total tile's delta against the
// configured budget. status comes from the backend (over | under | on | "").
function budgetDeltaProps(status, pct) {
  if (!status || pct == null || !Number.isFinite(pct)) return {}
  if (status === 'on') {
    return { delta: 'on budget', deltaColor: 'text-gray-400', deltaIcon: null }
  }
  if (status === 'under') {
    return {
      delta: `${Math.abs(pct).toFixed(1)}% under budget`,
      deltaColor: 'text-emerald-400',
      deltaIcon: 'trending_down',
    }
  }
  return {
    delta: `${Math.abs(pct).toFixed(1)}% over budget`,
    deltaColor: 'text-error',
    deltaIcon: 'trending_up',
  }
}

export default function CostGovernance() {
  const { theme } = useTheme()
  const { can } = useAuth()
  const canWrite = can(writeKey("cost"))
  // Recharts fill/stroke are SVG attributes and don't resolve CSS var(), so
  // read the resolved theme colors and recompute when the theme flips (#240).
  const chartColors = useMemo(() => {
    const s = getComputedStyle(document.documentElement)
    const v = (name, fallback) => s.getPropertyValue(name).trim() || fallback
    return {
      axis: v('--on-surface-variant', '#52525b'),
      barMax: v('--primary', '#ffb0cf'),
      barRest: v('--surface-container-highest', '#1d1a25'),
    }
  }, [theme])
  const [data, setData] = useState(null)
  const [loading, setLoading] = useState(true)
  const [loadError, setLoadError] = useState(null)
  const [pricing, setPricing] = useState([])
  const [showAddForm, setShowAddForm] = useState(false)
  const [form, setForm] = useState(EMPTY_FORM)
  const [saving, setSaving] = useState(false)
  const [saveError, setSaveError] = useState('')
  const [deletingKey, setDeletingKey] = useState(null)
  const [editingKey, setEditingKey] = useState(null)
  const [budgets, setBudgets] = useState([])
  const [showAddBudget, setShowAddBudget] = useState(false)
  const [budgetForm, setBudgetForm] = useState(EMPTY_BUDGET)
  const [savingBudget, setSavingBudget] = useState(false)
  const [budgetError, setBudgetError] = useState('')
  const [editingBudgetKey, setEditingBudgetKey] = useState(null)
  const [deletingBudgetKey, setDeletingBudgetKey] = useState(null)

  const refreshCost = () => api.getCost().then(setData).catch(() => {})

  useEffect(() => {
    api.getCost()
      .then(setData)
      .catch(err => setLoadError(err?.message || 'Failed to load cost data'))
      .finally(() => setLoading(false))
  }, [])

  const fetchBudgets = () => {
    api.getCostBudgets()
      .then(res => setBudgets(res.budgets ?? []))
      .catch(err => setLoadError(err?.message || 'Failed to load budgets'))
  }
  useEffect(() => { fetchBudgets() }, [])

  const fetchPricing = () => {
    api.listPricing()
      .then(res => setPricing(res.pricing ?? []))
      .catch(err => setLoadError(err?.message || 'Failed to load pricing'))
  }

  useEffect(() => { fetchPricing() }, [])

  const handleSave = async () => {
    if (!form.provider || !form.model) return
    setSaving(true)
    try {
      await api.upsertPricing({
        provider: form.provider,
        model: form.model,
        input_cost_per_million: parseFloat(form.input_cost_per_million) || 0,
        output_cost_per_million: parseFloat(form.output_cost_per_million) || 0,
        cache_read_cost_per_million: parseFloat(form.cache_read_cost_per_million) || 0,
      })
      setSaveError('')
      setShowAddForm(false)
      setForm(EMPTY_FORM)
      setEditingKey(null)
      fetchPricing()
    } catch {
      setSaveError('Save failed. Please try again.')
    } finally {
      setSaving(false)
    }
  }

  const handleEdit = (p) => {
    setForm({
      provider: p.provider,
      model: p.model,
      input_cost_per_million: String(p.input_cost_per_million),
      output_cost_per_million: String(p.output_cost_per_million),
      cache_read_cost_per_million: String(p.cache_read_cost_per_million),
    })
    setEditingKey(`${p.provider}/${p.model}`)
    setShowAddForm(true)
    setSaveError('')
  }

  const handleDelete = async (provider, model) => {
    const key = `${provider}/${model}`
    setDeletingKey(key)
    try {
      await api.deletePricing(provider, model)
      fetchPricing()
    } catch (err) {
      console.error('cost: delete pricing failed:', err)
      setSaveError(`Delete failed: ${err?.message ?? String(err)}`)
    } finally {
      setDeletingKey(null)
    }
  }

  const handleSaveBudget = async () => {
    if (!budgetForm.deployment_id || !budgetForm.monthly_budget) return
    setSavingBudget(true)
    try {
      await api.upsertCostBudget({
        deployment_id: budgetForm.deployment_id.trim(),
        monthly_budget: parseFloat(budgetForm.monthly_budget) || 0,
        currency: budgetForm.currency || 'USD',
      })
      setBudgetError('')
      setShowAddBudget(false)
      setBudgetForm(EMPTY_BUDGET)
      setEditingBudgetKey(null)
      fetchBudgets()
      refreshCost()
    } catch (err) {
      setBudgetError(err?.message || 'Save failed. Please try again.')
    } finally {
      setSavingBudget(false)
    }
  }

  const handleEditBudget = (b) => {
    setBudgetForm({
      deployment_id: b.deployment_id,
      monthly_budget: String(b.monthly_budget),
      currency: b.currency || 'USD',
    })
    setEditingBudgetKey(b.deployment_id)
    setShowAddBudget(true)
    setBudgetError('')
  }

  const handleDeleteBudget = async (deployment_id) => {
    setDeletingBudgetKey(deployment_id)
    try {
      await api.upsertCostBudget({
        deployment_id,
        monthly_budget: 0,
        currency: 'USD',
        deleted: true,
      })
      fetchBudgets()
      refreshCost()
    } catch (err) {
      setBudgetError(`Delete failed: ${err?.message ?? String(err)}`)
    } finally {
      setDeletingBudgetKey(null)
    }
  }

  const spendData = data?.daily_spend ?? []
  const maxSpend = spendData.length > 0 ? Math.max(...spendData.map(d => d.amount)) : 0

  const tokenBreakdown = data?.token_breakdown ?? [
    { label: 'Input Tokens', percentage: 0, amount: 0 },
    { label: 'Output Tokens', percentage: 0, amount: 0 },
    { label: 'Cache Tokens', percentage: 0, amount: 0 },
  ]

  const agentModelCosts = data?.agent_model_costs ?? []
  const cacheSavings = data?.cache_savings ?? 0
  const wastedSpend = data?.wasted_spend ?? 0

  const kpis = [
    {
      label: 'Month-to-Date Spend',
      value: formatDollar(data?.month_to_date),
      ...periodDeltaProps(data?.month_to_date_delta_pct, true),
    },
    {
      label: 'Forecasted Total',
      value: formatDollar(data?.forecasted),
      ...budgetDeltaProps(data?.budget_status, data?.forecast_budget_delta_pct),
    },
    {
      label: 'Avg. Token Cost / 1M (MTD)',
      value: formatDollar(data?.avg_token_cost),
      ...periodDeltaProps(data?.avg_token_cost_delta_pct, true),
    },
  ]

  return (
    <div className="min-h-screen bg-surface text-gray-100">
      <Sidebar />
      <TopNav />

      <main className="pl-sidebar pt-16 pb-8 min-h-screen">
        <div className="p-6 lg:p-10 space-y-8">
          {loadError && (
            <div className="flex items-center gap-3 px-4 py-3 bg-[#93000a]/20 border border-[#ffb4ab]/30 rounded-sm text-error text-xs">
              <span className="material-symbols-outlined text-base shrink-0">error</span>
              <span>{loadError}</span>
            </div>
          )}
          <PageHeader
            title="Cost Governance"
            subtitle="Monitor token spend, enforce budget policies, and detect cost anomalies across all agents."
            actions={<>
              <button className="px-4 py-2 bg-surface-container-high text-gray-100 text-xs font-medium rounded-sm border border-outline-variant/10 hover:bg-surface-container-highest transition-colors flex items-center gap-2">
                <span className="material-symbols-outlined text-[16px]">download</span>
                Export Report
              </button>
            </>}
          />

          {/* KPI Grid */}
          <div className="grid grid-cols-1 md:grid-cols-4 gap-6">
            {kpis.map((kpi, i) => (
              <div key={i} className="bg-surface-container-low p-6 rounded relative overflow-hidden group">
                <div className="absolute top-0 right-0 w-16 h-16 bg-[#ffb0cf]/5 rounded-full -mr-8 -mt-8 group-hover:scale-150 transition-transform duration-500"></div>
                <p className="text-gray-400 text-xs font-medium uppercase tracking-wider">{kpi.label}</p>
                <h3 className={`text-2xl font-semibold mt-2 ${kpi.valueColor || 'text-gray-100'}`}>{loading ? '...' : kpi.value}</h3>
                {kpi.delta && (
                  <div className={`flex items-center gap-1 mt-4 text-xs font-semibold ${kpi.deltaColor}`}>
                    {kpi.deltaIcon && <span className="material-symbols-outlined text-xs">{kpi.deltaIcon}</span>}
                    {kpi.delta}
                  </div>
                )}
              </div>
            ))}
          </div>

          {/* Charts Row */}
          <div className="grid grid-cols-1 lg:grid-cols-3 gap-6">
            {/* Daily Spend Trend */}
            <div className="lg:col-span-2 bg-surface-container-high p-6 rounded">
              <div className="flex justify-between items-center mb-8">
                <h4 className="text-base font-semibold text-gray-100">Daily Spend Trend</h4>
                <div className="flex gap-2">
                  <span className="px-2 py-1 bg-surface-container-highest text-[10px] rounded text-primary border border-[#ffb0cf]/20">30 DAYS</span>
                </div>
              </div>
              <ResponsiveContainer width="100%" height={256}>
                <BarChart data={spendData} margin={{ top: 0, right: 0, bottom: 0, left: 0 }}>
                  <XAxis dataKey="day" axisLine={false} tickLine={false} tick={{ fill: chartColors.axis, fontSize: 10, fontWeight: 700 }} />
                  <YAxis hide />
                  <Tooltip content={<CustomTooltip />} cursor={{ fill: 'rgba(255,176,207,0.05)' }} />
                  <Bar dataKey="amount" radius={[2, 2, 0, 0]}>
                    {spendData.map((entry, index) => (
                      <Cell key={index} fill={entry.amount === maxSpend ? chartColors.barMax : chartColors.barRest} />
                    ))}
                  </Bar>
                </BarChart>
              </ResponsiveContainer>
            </div>

            {/* Token Breakdown */}
            <div className="bg-surface-container-high p-6 rounded">
              <h4 className="text-base font-semibold text-gray-100 mb-8">Token Breakdown</h4>
              <div className="space-y-6">
                {tokenBreakdown.map(t => (
                  <div key={t.label}>
                    <div className="flex justify-between text-xs mb-2">
                      <span className="text-gray-400">{t.label}</span>
                      <span className="text-gray-100 font-semibold">{t.percentage}% ({(t.tokens ?? 0).toLocaleString()} tokens)</span>
                    </div>
                    <div className="h-1.5 w-full bg-surface-container-lowest rounded-full overflow-hidden">
                      <div className={`h-full ${TOKEN_COLORS[t.label] ?? 'bg-zinc-500'}`} style={{ width: `${t.percentage}%` }}></div>
                    </div>
                  </div>
                ))}
              </div>
              <div className="mt-8 pt-8 border-t border-outline-variant/10">
                <div className="flex items-center gap-4">
                  <div className="flex-1 bg-surface-container-lowest p-3 rounded text-center">
                    <p className="text-[10px] text-gray-400 uppercase font-semibold">Cache Savings</p>
                    <p className="text-base font-semibold text-emerald-400">{formatDollar(cacheSavings)}</p>
                  </div>
                  <div className="flex-1 bg-surface-container-lowest p-3 rounded text-center">
                    <p className="text-[10px] text-gray-400 uppercase font-semibold">Wasted Spend</p>
                    <p className="text-base font-semibold text-error">{formatDollar(wastedSpend)}</p>
                  </div>
                </div>
              </div>
            </div>
          </div>

          {/* Cost per Agent/Model */}
          <div className="bg-surface-container-high p-6 rounded">
            <h4 className="text-base font-semibold text-gray-100 mb-6">Cost per Agent / Model</h4>
            <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-4 gap-4">
              {agentModelCosts.map((am, i) => (
                <div key={`${am.agent_id}-${am.model}-${i}`} className="bg-gray-800 p-4 rounded flex items-center justify-between group hover:bg-surface-container-highest transition-colors">
                  <div>
                    <p className="text-[10px] text-gray-400 uppercase font-semibold mb-1">{am.provider} / {am.model}</p>
                    <h5 className="font-semibold text-gray-100">{am.agent_name}</h5>
                  </div>
                  <div className="text-right">
                    <p className="font-semibold text-primary">{formatDollar(am.cost)}</p>
                    <p className="text-[10px] text-gray-400">{am.requests}</p>
                  </div>
                </div>
              ))}
              {agentModelCosts.length === 0 && !loading && (
                <div className="col-span-4 text-center text-gray-400 text-sm py-4">No cost data available yet.</div>
              )}
            </div>
          </div>

          {/* Monthly Budgets — powers the Forecasted Total delta */}
          <div className="bg-surface-container-high p-6 rounded">
            <div className="flex justify-between items-center mb-6">
              <div>
                <h4 className="text-base font-semibold text-gray-100">Monthly Budgets</h4>
                <p className="text-xs text-gray-400 mt-1">Per-deployment monthly spend targets. The Forecasted Total KPI compares against the sum of active budgets.</p>
              </div>
              {!showAddBudget && canWrite && (
                <button
                  onClick={() => { setShowAddBudget(true); setBudgetError('') }}
                  className="text-xs text-primary font-semibold flex items-center gap-1 hover:underline"
                >
                  <span className="material-symbols-outlined text-xs">add_circle</span>
                  Add Budget
                </button>
              )}
            </div>
            <table className="w-full text-left text-xs">
              <thead>
                <tr className="text-gray-400 border-b border-outline-variant/10">
                  <th className="pb-3 font-medium">Deployment</th>
                  <th className="pb-3 font-medium">Monthly Budget</th>
                  <th className="pb-3 font-medium">Currency</th>
                  <th className="pb-3 font-medium">Updated By</th>
                  <th className="pb-3 font-medium">Updated</th>
                  <th className="pb-3"></th>
                </tr>
              </thead>
              <tbody className="divide-y divide-outline-variant/5">
                {budgets.map(b => (
                  <tr key={b.deployment_id}>
                    <td className="py-3 font-medium text-gray-100">{b.deployment_id}</td>
                    <td className="py-3 text-gray-400">{formatDollar(b.monthly_budget)}</td>
                    <td className="py-3 text-gray-400">{b.currency || 'USD'}</td>
                    <td className="py-3 text-gray-400">{b.updated_by || '—'}</td>
                    <td className="py-3 text-gray-400">{b.updated_at ? b.updated_at.slice(0, 10) : '—'}</td>
                    <td className="py-3 text-right">
                      <div className="flex items-center justify-end gap-2">
                        <button
                          onClick={() => handleEditBudget(b)}
                          disabled={showAddBudget}
                          className="text-gray-400 hover:text-primary disabled:opacity-40"
                          title="Edit"
                        >
                          <span className="material-symbols-outlined text-base">edit</span>
                        </button>
                        <button
                          onClick={() => handleDeleteBudget(b.deployment_id)}
                          disabled={!canWrite}
                          disabled={deletingBudgetKey === b.deployment_id || showAddBudget}
                          className="text-gray-400 hover:text-error disabled:opacity-40"
                          title="Delete"
                        >
                          <span className="material-symbols-outlined text-base">close</span>
                        </button>
                      </div>
                    </td>
                  </tr>
                ))}
                {budgets.length === 0 && !showAddBudget && (
                  <tr>
                    <td colSpan={6} className="py-4 text-center text-gray-400 text-xs">No budgets configured. The Forecasted Total KPI shows no delta until at least one is added.</td>
                  </tr>
                )}
                {showAddBudget && (
                  <tr>
                    <td className="py-2">
                      <input
                        className={`w-full bg-gray-800 border border-gray-600 rounded px-2 py-1 text-xs focus:outline-none focus:border-gray-400 ${editingBudgetKey ? 'text-gray-400 cursor-not-allowed' : 'text-gray-100 placeholder-zinc-600'}`}
                        placeholder="production"
                        value={budgetForm.deployment_id}
                        readOnly={!!editingBudgetKey}
                        onChange={e => setBudgetForm(f => ({ ...f, deployment_id: e.target.value }))}
                      />
                    </td>
                    <td className="py-2">
                      <input
                        type="number" step="0.01" min="0"
                        className="w-full bg-gray-800 border border-gray-600 rounded px-2 py-1 text-gray-100 placeholder-zinc-600 text-xs focus:outline-none focus:border-gray-400"
                        placeholder="1000.00"
                        value={budgetForm.monthly_budget}
                        onChange={e => setBudgetForm(f => ({ ...f, monthly_budget: e.target.value }))}
                      />
                    </td>
                    <td className="py-2">
                      <select
                        className="w-full bg-gray-800 border border-gray-600 rounded px-2 py-1 text-gray-100 text-xs focus:outline-none focus:border-gray-400"
                        value={budgetForm.currency}
                        onChange={e => setBudgetForm(f => ({ ...f, currency: e.target.value }))}
                      >
                        <option value="USD">USD</option>
                      </select>
                    </td>
                    <td className="py-2" colSpan={2}></td>
                    <td className="py-2 text-right">
                      <div className="flex items-center justify-end gap-2">
                        <button
                          onClick={handleSaveBudget}
                          disabled={savingBudget || !budgetForm.deployment_id || !budgetForm.monthly_budget}
                          className="px-2 py-1 bg-primary-button text-primary-button-text text-[10px] font-semibold rounded disabled:opacity-40"
                        >
                          {savingBudget ? '...' : 'Save'}
                        </button>
                        <button
                          onClick={() => { setShowAddBudget(false); setBudgetForm(EMPTY_BUDGET); setBudgetError(''); setEditingBudgetKey(null) }}
                          className="px-2 py-1 bg-surface-container-highest text-gray-100 text-[10px] font-semibold rounded"
                        >
                          Cancel
                        </button>
                      </div>
                      {budgetError && <p className="text-[10px] text-error mt-1 text-right">{budgetError}</p>}
                    </td>
                  </tr>
                )}
              </tbody>
            </table>
          </div>

          {/* Model Pricing */}
          <div className="bg-surface-container-high p-6 rounded">
            <div className="flex justify-between items-center mb-6">
              <h4 className="text-base font-semibold text-gray-100">Model Pricing</h4>
              {!showAddForm && canWrite && (
                <button
                  onClick={() => { setShowAddForm(true); setSaveError('') }}
                  className="text-xs text-primary font-semibold flex items-center gap-1 hover:underline"
                >
                  <span className="material-symbols-outlined text-xs">add_circle</span>
                  Add Model
                </button>
              )}
            </div>
            <table className="w-full text-left text-xs">
              <thead>
                <tr className="text-gray-400 border-b border-outline-variant/10">
                  <th className="pb-3 font-medium">Provider</th>
                  <th className="pb-3 font-medium">Model</th>
                  <th className="pb-3 font-medium">Input $/1M</th>
                  <th className="pb-3 font-medium">Output $/1M</th>
                  <th className="pb-3 font-medium">Cache-Read $/1M</th>
                  <th className="pb-3 font-medium">Updated</th>
                  <th className="pb-3"></th>
                </tr>
              </thead>
              <tbody className="divide-y divide-outline-variant/5">
                {pricing.map(p => {
                  const key = `${p.provider}/${p.model}`
                  return (
                    <tr key={key}>
                      <td className="py-3 font-medium text-gray-100">{p.provider}</td>
                      <td className="py-3 text-gray-400">{p.model}</td>
                      <td className="py-3 text-gray-400">{formatDollar(p.input_cost_per_million)}</td>
                      <td className="py-3 text-gray-400">{formatDollar(p.output_cost_per_million)}</td>
                      <td className="py-3 text-gray-400">{formatDollar(p.cache_read_cost_per_million)}</td>
                      <td className="py-3 text-gray-400">{p.updated_at ? p.updated_at.slice(0, 10) : '—'}</td>
                      <td className="py-3 text-right">
                        <div className="flex items-center justify-end gap-2">
                          <button
                            onClick={() => handleEdit(p)}
                            disabled={showAddForm}
                            className="text-gray-400 hover:text-primary disabled:opacity-40"
                            title="Edit"
                          >
                            <span className="material-symbols-outlined text-base">edit</span>
                          </button>
                          <button
                            onClick={() => handleDelete(p.provider, p.model)}
                            disabled={!canWrite}
                            disabled={deletingKey === key || showAddForm}
                            className="text-gray-400 hover:text-error disabled:opacity-40"
                            title="Delete"
                          >
                            <span className="material-symbols-outlined text-base">close</span>
                          </button>
                        </div>
                      </td>
                    </tr>
                  )
                })}
                {pricing.length === 0 && !showAddForm && (
                  <tr>
                    <td colSpan={7} className="py-4 text-center text-gray-400 text-xs">No pricing entries. Click "+ Add Model" to add one.</td>
                  </tr>
                )}
                {showAddForm && (
                  <tr>
                    <td className="py-2">
                      <input
                        className={`w-full bg-gray-800 border border-gray-600 rounded px-2 py-1 text-xs focus:outline-none focus:border-gray-400 ${editingKey ? 'text-gray-400 cursor-not-allowed' : 'text-gray-100 placeholder-zinc-600'}`}
                        placeholder="anthropic"
                        value={form.provider}
                        readOnly={!!editingKey}
                        onChange={e => setForm(f => ({ ...f, provider: e.target.value }))}
                      />
                    </td>
                    <td className="py-2">
                      <input
                        className={`w-full bg-gray-800 border border-gray-600 rounded px-2 py-1 text-xs focus:outline-none focus:border-gray-400 ${editingKey ? 'text-gray-400 cursor-not-allowed' : 'text-gray-100 placeholder-zinc-600'}`}
                        placeholder="claude-sonnet-4-6"
                        value={form.model}
                        readOnly={!!editingKey}
                        onChange={e => setForm(f => ({ ...f, model: e.target.value }))}
                      />
                    </td>
                    <td className="py-2">
                      <input
                        type="number" step="0.01"
                        className="w-full bg-gray-800 border border-gray-600 rounded px-2 py-1 text-gray-100 placeholder-zinc-600 text-xs focus:outline-none focus:border-gray-400"
                        placeholder="3.00"
                        value={form.input_cost_per_million}
                        onChange={e => setForm(f => ({ ...f, input_cost_per_million: e.target.value }))}
                      />
                    </td>
                    <td className="py-2">
                      <input
                        type="number" step="0.01"
                        className="w-full bg-gray-800 border border-gray-600 rounded px-2 py-1 text-gray-100 placeholder-zinc-600 text-xs focus:outline-none focus:border-gray-400"
                        placeholder="15.00"
                        value={form.output_cost_per_million}
                        onChange={e => setForm(f => ({ ...f, output_cost_per_million: e.target.value }))}
                      />
                    </td>
                    <td className="py-2">
                      <input
                        type="number" step="0.01"
                        className="w-full bg-gray-800 border border-gray-600 rounded px-2 py-1 text-gray-100 placeholder-zinc-600 text-xs focus:outline-none focus:border-gray-400"
                        placeholder="0.30"
                        value={form.cache_read_cost_per_million}
                        onChange={e => setForm(f => ({ ...f, cache_read_cost_per_million: e.target.value }))}
                      />
                    </td>
                    <td className="py-2"></td>
                    <td className="py-2 text-right">
                      <div className="flex items-center justify-end gap-2">
                        <button
                          onClick={handleSave}
                          disabled={saving || !form.provider || !form.model}
                          className="px-2 py-1 bg-primary-button text-primary-button-text text-[10px] font-semibold rounded disabled:opacity-40"
                        >
                          {saving ? '...' : 'Save'}
                        </button>
                        <button
                          onClick={() => { setShowAddForm(false); setForm(EMPTY_FORM); setSaveError(''); setEditingKey(null) }}
                          className="px-2 py-1 bg-surface-container-highest text-gray-100 text-[10px] font-semibold rounded"
                        >
                          Cancel
                        </button>
                      </div>
                      {saveError && <p className="text-[10px] text-error mt-1 text-right">{saveError}</p>}
                    </td>
                  </tr>
                )}
              </tbody>
            </table>
          </div>

        </div>
      </main>
    </div>
  )
}
