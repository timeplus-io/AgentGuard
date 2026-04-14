import { useState, useEffect } from 'react'
import { BarChart, Bar, XAxis, YAxis, Tooltip, ResponsiveContainer, Cell } from 'recharts'
import Sidebar from '../components/Sidebar'
import TopNav from '../components/TopNav'
import PageHeader from '../components/PageHeader'
import { api } from '../api/client'

const CustomTooltip = ({ active, payload, label }) => {
  if (active && payload && payload.length) {
    return (
      <div className="bg-[#2c2834] border border-[#574149]/20 px-3 py-2 rounded-sm text-xs">
        <p className="text-zinc-400 font-semibold">{label}</p>
        <p className="text-[#ffb0cf] font-bold">${payload[0].value.toLocaleString(undefined, { minimumFractionDigits: 2, maximumFractionDigits: 2 })}</p>
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

export default function CostGovernance() {
  const [data, setData] = useState(null)
  const [loading, setLoading] = useState(true)
  const [pricing, setPricing] = useState([])
  const [showAddForm, setShowAddForm] = useState(false)
  const [form, setForm] = useState(EMPTY_FORM)
  const [saving, setSaving] = useState(false)
  const [saveError, setSaveError] = useState('')
  const [deletingKey, setDeletingKey] = useState(null)
  const [editingKey, setEditingKey] = useState(null)

  useEffect(() => {
    api.getCost()
      .then(setData)
      .catch(() => {})
      .finally(() => setLoading(false))
  }, [])

  const fetchPricing = () => {
    api.listPricing()
      .then(res => setPricing(res.pricing ?? []))
      .catch(() => {})
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
    } catch {
      // intentionally swallow error — UI handles state via finally
    } finally {
      setDeletingKey(null)
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
    { label: 'Month-to-Date Spend', value: formatDollar(data?.month_to_date), delta: '4.2% from last month', deltaColor: 'text-emerald-400', deltaIcon: 'trending_down' },
    { label: 'Forecasted Total',     value: formatDollar(data?.forecasted),    delta: 'Within 15% of budget', deltaColor: 'text-zinc-500',    deltaIcon: null },
    { label: 'Avg. Token Cost / 1M', value: formatDollar(data?.avg_token_cost), delta: 'Optimized by 12%',  deltaColor: 'text-[#ffb0cf]',   deltaIcon: null },
  ]

  return (
    <div className="min-h-screen bg-[#15121d] text-[#e7dff0]">
      <Sidebar />
      <TopNav />

      <main className="pl-sidebar pt-16 pb-8 min-h-screen">
        <div className="p-6 lg:p-10 space-y-8">
          <PageHeader
            title="Cost Governance"
            subtitle="Monitor token spend, enforce budget policies, and detect cost anomalies across all agents."
            actions={<>
              <button className="px-4 py-2 bg-[#2c2834] text-[#e7dff0] text-xs font-medium rounded-sm border border-[#574149]/10 hover:bg-[#37333f] transition-colors flex items-center gap-2">
                <span className="material-symbols-outlined text-[16px]">download</span>
                Export Report
              </button>
            </>}
          />

          {/* KPI Grid */}
          <div className="grid grid-cols-1 md:grid-cols-4 gap-6">
            {kpis.map((kpi, i) => (
              <div key={i} className="bg-[#1d1a25] p-6 rounded relative overflow-hidden group">
                <div className="absolute top-0 right-0 w-16 h-16 bg-[#ffb0cf]/5 rounded-full -mr-8 -mt-8 group-hover:scale-150 transition-transform duration-500"></div>
                <p className="text-zinc-500 text-xs font-medium uppercase tracking-wider">{kpi.label}</p>
                <h3 className={`text-2xl font-semibold mt-2 ${kpi.valueColor || 'text-[#e7dff0]'}`}>{loading ? '...' : kpi.value}</h3>
                <div className={`flex items-center gap-1 mt-4 text-xs font-semibold ${kpi.deltaColor}`}>
                  {kpi.deltaIcon && <span className="material-symbols-outlined text-xs">{kpi.deltaIcon}</span>}
                  {kpi.delta}
                </div>
              </div>
            ))}
          </div>

          {/* Charts Row */}
          <div className="grid grid-cols-1 lg:grid-cols-3 gap-6">
            {/* Daily Spend Trend */}
            <div className="lg:col-span-2 bg-[#2c2834] p-6 rounded">
              <div className="flex justify-between items-center mb-8">
                <h4 className="text-base font-semibold text-[#e7dff0]">Daily Spend Trend</h4>
                <div className="flex gap-2">
                  <span className="px-2 py-1 bg-[#37333f] text-[10px] rounded text-[#ffb0cf] border border-[#ffb0cf]/20">30 DAYS</span>
                </div>
              </div>
              <ResponsiveContainer width="100%" height={256}>
                <BarChart data={spendData} margin={{ top: 0, right: 0, bottom: 0, left: 0 }}>
                  <XAxis dataKey="day" axisLine={false} tickLine={false} tick={{ fill: '#52525b', fontSize: 10, fontWeight: 700 }} />
                  <YAxis hide />
                  <Tooltip content={<CustomTooltip />} cursor={{ fill: 'rgba(255,176,207,0.05)' }} />
                  <Bar dataKey="amount" radius={[2, 2, 0, 0]}>
                    {spendData.map((entry, index) => (
                      <Cell key={index} fill={entry.amount === maxSpend ? '#ffb0cf' : '#1d1a25'} />
                    ))}
                  </Bar>
                </BarChart>
              </ResponsiveContainer>
            </div>

            {/* Token Breakdown */}
            <div className="bg-[#2c2834] p-6 rounded">
              <h4 className="text-base font-semibold text-[#e7dff0] mb-8">Token Breakdown</h4>
              <div className="space-y-6">
                {tokenBreakdown.map(t => (
                  <div key={t.label}>
                    <div className="flex justify-between text-xs mb-2">
                      <span className="text-zinc-400">{t.label}</span>
                      <span className="text-[#e7dff0] font-semibold">{t.percentage}% ({(t.tokens ?? 0).toLocaleString()} tokens)</span>
                    </div>
                    <div className="h-1.5 w-full bg-[#0f0c17] rounded-full overflow-hidden">
                      <div className={`h-full ${TOKEN_COLORS[t.label] ?? 'bg-zinc-500'}`} style={{ width: `${t.percentage}%` }}></div>
                    </div>
                  </div>
                ))}
              </div>
              <div className="mt-8 pt-8 border-t border-[#574149]/10">
                <div className="flex items-center gap-4">
                  <div className="flex-1 bg-[#0f0c17] p-3 rounded text-center">
                    <p className="text-[10px] text-zinc-500 uppercase font-semibold">Cache Savings</p>
                    <p className="text-base font-semibold text-emerald-400">{formatDollar(cacheSavings)}</p>
                  </div>
                  <div className="flex-1 bg-[#0f0c17] p-3 rounded text-center">
                    <p className="text-[10px] text-zinc-500 uppercase font-semibold">Wasted Spend</p>
                    <p className="text-base font-semibold text-[#ffb4ab]">{formatDollar(wastedSpend)}</p>
                  </div>
                </div>
              </div>
            </div>
          </div>

          {/* Cost per Agent/Model */}
          <div className="bg-[#2c2834] p-6 rounded">
            <h4 className="text-base font-semibold text-[#e7dff0] mb-6">Cost per Agent / Model</h4>
            <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-4 gap-4">
              {agentModelCosts.map((am, i) => (
                <div key={`${am.agent_id}-${am.model}-${i}`} className="bg-[#211e29] p-4 rounded flex items-center justify-between group hover:bg-[#37333f] transition-colors">
                  <div>
                    <p className="text-[10px] text-zinc-500 uppercase font-semibold mb-1">{am.provider} / {am.model}</p>
                    <h5 className="font-semibold text-[#e7dff0]">{am.agent_name}</h5>
                  </div>
                  <div className="text-right">
                    <p className="font-semibold text-[#ffb0cf]">{formatDollar(am.cost)}</p>
                    <p className="text-[10px] text-zinc-500">{am.requests}</p>
                  </div>
                </div>
              ))}
              {agentModelCosts.length === 0 && !loading && (
                <div className="col-span-4 text-center text-zinc-500 text-sm py-4">No cost data available yet.</div>
              )}
            </div>
          </div>

          {/* Model Pricing */}
          <div className="bg-[#2c2834] p-6 rounded">
            <div className="flex justify-between items-center mb-6">
              <h4 className="text-base font-semibold text-[#e7dff0]">Model Pricing</h4>
              {!showAddForm && (
                <button
                  onClick={() => { setShowAddForm(true); setSaveError('') }}
                  className="text-xs text-[#ffb0cf] font-semibold flex items-center gap-1 hover:underline"
                >
                  <span className="material-symbols-outlined text-xs">add_circle</span>
                  Add Model
                </button>
              )}
            </div>
            <table className="w-full text-left text-xs">
              <thead>
                <tr className="text-zinc-500 border-b border-[#574149]/10">
                  <th className="pb-3 font-medium">Provider</th>
                  <th className="pb-3 font-medium">Model</th>
                  <th className="pb-3 font-medium">Input $/1M</th>
                  <th className="pb-3 font-medium">Output $/1M</th>
                  <th className="pb-3 font-medium">Cache-Read $/1M</th>
                  <th className="pb-3 font-medium">Updated</th>
                  <th className="pb-3"></th>
                </tr>
              </thead>
              <tbody className="divide-y divide-[#574149]/5">
                {pricing.map(p => {
                  const key = `${p.provider}/${p.model}`
                  return (
                    <tr key={key}>
                      <td className="py-3 font-medium text-[#e7dff0]">{p.provider}</td>
                      <td className="py-3 text-zinc-400">{p.model}</td>
                      <td className="py-3 text-zinc-400">{formatDollar(p.input_cost_per_million)}</td>
                      <td className="py-3 text-zinc-400">{formatDollar(p.output_cost_per_million)}</td>
                      <td className="py-3 text-zinc-400">{formatDollar(p.cache_read_cost_per_million)}</td>
                      <td className="py-3 text-zinc-500">{p.updated_at ? p.updated_at.slice(0, 10) : '—'}</td>
                      <td className="py-3 text-right">
                        <div className="flex items-center justify-end gap-2">
                          <button
                            onClick={() => handleEdit(p)}
                            disabled={showAddForm}
                            className="text-zinc-500 hover:text-[#ffb0cf] disabled:opacity-40"
                            title="Edit"
                          >
                            <span className="material-symbols-outlined text-base">edit</span>
                          </button>
                          <button
                            onClick={() => handleDelete(p.provider, p.model)}
                            disabled={deletingKey === key || showAddForm}
                            className="text-zinc-500 hover:text-[#ffb4ab] disabled:opacity-40"
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
                    <td colSpan={7} className="py-4 text-center text-zinc-500 text-xs">No pricing entries. Click "+ Add Model" to add one.</td>
                  </tr>
                )}
                {showAddForm && (
                  <tr>
                    <td className="py-2">
                      <input
                        className={`w-full bg-[#211e29] border border-[#574149]/30 rounded px-2 py-1 text-xs focus:outline-none ${editingKey ? 'text-zinc-500 cursor-not-allowed' : 'text-[#e7dff0] placeholder-zinc-600'}`}
                        placeholder="anthropic"
                        value={form.provider}
                        readOnly={!!editingKey}
                        onChange={e => setForm(f => ({ ...f, provider: e.target.value }))}
                      />
                    </td>
                    <td className="py-2">
                      <input
                        className={`w-full bg-[#211e29] border border-[#574149]/30 rounded px-2 py-1 text-xs focus:outline-none ${editingKey ? 'text-zinc-500 cursor-not-allowed' : 'text-[#e7dff0] placeholder-zinc-600'}`}
                        placeholder="claude-sonnet-4-6"
                        value={form.model}
                        readOnly={!!editingKey}
                        onChange={e => setForm(f => ({ ...f, model: e.target.value }))}
                      />
                    </td>
                    <td className="py-2">
                      <input
                        type="number" step="0.01"
                        className="w-full bg-[#211e29] border border-[#574149]/30 rounded px-2 py-1 text-[#e7dff0] placeholder-zinc-600 text-xs focus:outline-none"
                        placeholder="3.00"
                        value={form.input_cost_per_million}
                        onChange={e => setForm(f => ({ ...f, input_cost_per_million: e.target.value }))}
                      />
                    </td>
                    <td className="py-2">
                      <input
                        type="number" step="0.01"
                        className="w-full bg-[#211e29] border border-[#574149]/30 rounded px-2 py-1 text-[#e7dff0] placeholder-zinc-600 text-xs focus:outline-none"
                        placeholder="15.00"
                        value={form.output_cost_per_million}
                        onChange={e => setForm(f => ({ ...f, output_cost_per_million: e.target.value }))}
                      />
                    </td>
                    <td className="py-2">
                      <input
                        type="number" step="0.01"
                        className="w-full bg-[#211e29] border border-[#574149]/30 rounded px-2 py-1 text-[#e7dff0] placeholder-zinc-600 text-xs focus:outline-none"
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
                          className="px-2 py-1 bg-[#37333f] text-zinc-300 text-[10px] font-semibold rounded"
                        >
                          Cancel
                        </button>
                      </div>
                      {saveError && <p className="text-[10px] text-[#ffb4ab] mt-1 text-right">{saveError}</p>}
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
