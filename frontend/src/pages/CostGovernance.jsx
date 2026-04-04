import { useState, useEffect } from 'react'
import { BarChart, Bar, XAxis, YAxis, Tooltip, ResponsiveContainer, Cell } from 'recharts'
import Sidebar from '../components/Sidebar'
import TopNav from '../components/TopNav'
import Footer from '../components/Footer'
import PageHeader from '../components/PageHeader'
import { api } from '../api/client'

const staticSpendData = [
  { day: 'MON', amount: 1650 },
  { day: 'TUE', amount: 2100 },
  { day: 'WED', amount: 2800 },
  { day: 'THU', amount: 3200 },
  { day: 'FRI', amount: 1800 },
  { day: 'SAT', amount: 1100 },
  { day: 'SUN', amount: 1950 },
]

const agentModels = [
  { model: 'Claude-3.5-Sonnet', agent: 'Security_Bot_Alpha', cost: '$4,210', requests: '2.1M Requests' },
  { model: 'GPT-4o', agent: 'Investigator_Deep', cost: '$3,842', requests: '1.4M Requests' },
  { model: 'GPT-4-Turbo', agent: 'Legacy_Auditor', cost: '$2,105', requests: '0.8M Requests' },
  { model: 'Claude-3-Haiku', agent: 'Quick_Log_Parser', cost: '$685', requests: '8.2M Requests' },
]

const budgetPolicies = [
  { name: 'Daily Hard Cap', trigger: '$500/day', enforcement: 'KILL_AGENT', enfClass: 'bg-[#93000a] text-[#ffdad6]', status: 'Active', statusColor: 'text-emerald-400', dot: 'bg-emerald-400' },
  { name: 'Spike Warning', trigger: '200% Velocity', enforcement: 'NOTIFY_SOC', enfClass: 'bg-[#4b4754] text-[#bcb6c5]', status: 'Active', statusColor: 'text-emerald-400', dot: 'bg-emerald-400' },
  { name: 'Model Throttling', trigger: 'Budget > 80%', enforcement: 'SWITCH_HAIKU', enfClass: 'bg-[#4b4754] text-[#bcb6c5]', status: 'Paused', statusColor: 'text-zinc-500', dot: 'bg-zinc-600' },
]

const anomalies = [
  {
    border: 'border-[#ffb4ab]',
    icon: 'warning', iconBg: 'bg-[#93000a]/20 text-[#ffb4ab]',
    title: 'Runaway token loop detected',
    sub: 'Agent: Auditor_Bot_04 • Impact: $42.10/min',
    age: '2H AGO',
    actions: true,
  },
  {
    border: 'border-[#ffb0cf]',
    icon: 'trending_up', iconBg: 'bg-[#ffb0cf]/10 text-[#ffb0cf]',
    title: 'Unexpected model shift',
    sub: 'Legacy_Auditor routing 100% to GPT-4o',
    age: '1D AGO',
    actions: false,
  },
  {
    border: 'border-zinc-700',
    icon: 'receipt_long', iconBg: 'bg-[#37333f] text-zinc-500',
    title: 'Budget alert threshold reached',
    sub: 'Security_Cluster_Global at 80% of limit',
    age: '2D AGO',
    actions: false,
  },
]

const CustomTooltip = ({ active, payload, label }) => {
  if (active && payload && payload.length) {
    return (
      <div className="bg-[#2c2834] border border-[#574149]/20 px-3 py-2 rounded-sm text-xs">
        <p className="text-zinc-400 font-semibold">{label}</p>
        <p className="text-[#ffb0cf] font-bold">${payload[0].value.toLocaleString()}</p>
      </div>
    )
  }
  return null
}

export default function CostGovernance() {
  const [data, setData] = useState(null)
  const [loading, setLoading] = useState(true)

  useEffect(() => {
    api.getCost()
      .then(setData)
      .catch(() => {})
      .finally(() => setLoading(false))
  }, [])

  const kpis = [
    { label: 'Month-to-Date Spend', value: data?.monthToDate ?? '$12,842.50', delta: '4.2% from last month', deltaColor: 'text-emerald-400', deltaIcon: 'trending_down' },
    { label: 'Forecasted Total', value: data?.forecasted ?? '$15,200.00', delta: 'Within 15% of budget', deltaColor: 'text-zinc-500', deltaIcon: null },
    { label: 'Avg. Token Cost / 1M', value: data?.avgTokenCost ?? '$0.82', delta: 'Optimized by 12%', deltaColor: 'text-[#ffb0cf]', deltaIcon: null },
    { label: 'Active Policy Breaches', value: data?.policyBreaches ?? '02', delta: 'Requires Attention', deltaColor: 'text-[#ffb4ab]', deltaIcon: null, valueColor: 'text-[#ffb4ab]' },
  ]

  const spendData = data?.dailySpend ?? staticSpendData
  const maxSpend = Math.max(...spendData.map(d => d.amount))

  return (
    <div className="min-h-screen bg-[#15121d] text-[#e7dff0]">
      <Sidebar />
      <TopNav />
      <Footer />
      <main className="pl-64 pt-16 pb-8 min-h-screen">
        <div className="p-6 lg:p-10 space-y-8">
          <PageHeader
            title="Cost Governance"
            subtitle="Monitor token spend, enforce budget policies, and detect cost anomalies across all agents."
            actions={<>
              <button className="px-4 py-2 bg-[#2c2834] text-[#e7dff0] text-xs font-medium rounded-sm border border-[#574149]/10 hover:bg-[#37333f] transition-colors flex items-center gap-2">
                <span className="material-symbols-outlined text-[16px]">download</span>
                Export Report
              </button>
              <button className="px-4 py-2 bg-gradient-to-br from-[#ffb0cf] to-[#f256a3] text-[#3d0023] text-xs font-semibold rounded-sm flex items-center gap-2">
                <span className="material-symbols-outlined text-[16px]">add</span>
                New Policy
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
                  <span className="px-2 py-1 bg-[#37333f] text-[10px] rounded text-[#ffb0cf] border border-[#ffb0cf]/20">7 DAYS</span>
                  <span className="px-2 py-1 bg-[#0f0c17] text-[10px] rounded text-zinc-500">30 DAYS</span>
                </div>
              </div>
              <ResponsiveContainer width="100%" height={256}>
                <BarChart data={spendData} margin={{ top: 0, right: 0, bottom: 0, left: 0 }}>
                  <XAxis dataKey="day" axisLine={false} tickLine={false} tick={{ fill: '#52525b', fontSize: 10, fontWeight: 700 }} />
                  <YAxis hide />
                  <Tooltip content={<CustomTooltip />} cursor={{ fill: 'rgba(255,176,207,0.05)' }} />
                  <Bar dataKey="amount" radius={[2, 2, 0, 0]}>
                    {spendData.map((entry, index) => (
                      <Cell
                        key={index}
                        fill={entry.amount === maxSpend ? '#ffb0cf' : '#1d1a25'}
                      />
                    ))}
                  </Bar>
                </BarChart>
              </ResponsiveContainer>
            </div>

            {/* Token Breakdown */}
            <div className="bg-[#2c2834] p-6 rounded">
              <h4 className="text-base font-semibold text-[#e7dff0] mb-8">Token Breakdown</h4>
              <div className="space-y-6">
                {[
                  { label: 'Input Tokens', pct: 42, amount: '$5,393', color: 'bg-[#D53F8C]' },
                  { label: 'Output Tokens', pct: 48, amount: '$6,164', color: 'bg-[#ffb0cf]' },
                  { label: 'Cache Hits', pct: 10, amount: '$1,284', color: 'bg-[#ffb2b8]' },
                ].map(t => (
                  <div key={t.label}>
                    <div className="flex justify-between text-xs mb-2">
                      <span className="text-zinc-400">{t.label}</span>
                      <span className="text-[#e7dff0] font-semibold">{t.pct}% ({t.amount})</span>
                    </div>
                    <div className="h-1.5 w-full bg-[#0f0c17] rounded-full overflow-hidden">
                      <div className={`h-full ${t.color}`} style={{ width: `${t.pct}%` }}></div>
                    </div>
                  </div>
                ))}
              </div>
              <div className="mt-8 pt-8 border-t border-[#574149]/10">
                <div className="flex items-center gap-4">
                  <div className="flex-1 bg-[#0f0c17] p-3 rounded text-center">
                    <p className="text-[10px] text-zinc-500 uppercase font-semibold">Cache Savings</p>
                    <p className="text-base font-semibold text-emerald-400">$2,410</p>
                  </div>
                  <div className="flex-1 bg-[#0f0c17] p-3 rounded text-center">
                    <p className="text-[10px] text-zinc-500 uppercase font-semibold">Wasted Spend</p>
                    <p className="text-base font-semibold text-[#ffb4ab]">$142</p>
                  </div>
                </div>
              </div>
            </div>
          </div>

          {/* Cost per Agent/Model */}
          <div className="bg-[#2c2834] p-6 rounded">
            <h4 className="text-base font-semibold text-[#e7dff0] mb-6">Cost per Agent / Model</h4>
            <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-4 gap-4">
              {agentModels.map(am => (
                <div key={am.agent} className="bg-[#211e29] p-4 rounded flex items-center justify-between group hover:bg-[#37333f] transition-colors">
                  <div>
                    <p className="text-[10px] text-zinc-500 uppercase font-semibold mb-1">{am.model}</p>
                    <h5 className="font-semibold text-[#e7dff0]">{am.agent}</h5>
                  </div>
                  <div className="text-right">
                    <p className="font-semibold text-[#ffb0cf]">{am.cost}</p>
                    <p className="text-[10px] text-zinc-500">{am.requests}</p>
                  </div>
                </div>
              ))}
            </div>
          </div>

          {/* Budget Policy + Anomalies */}
          <div className="grid grid-cols-1 lg:grid-cols-2 gap-6">
            <div className="bg-[#2c2834] p-6 rounded">
              <div className="flex justify-between items-center mb-6">
                <h4 className="text-base font-semibold text-[#e7dff0]">Budget Policy Table</h4>
                <button className="text-xs text-[#ffb0cf] font-semibold flex items-center gap-1 hover:underline">
                  <span className="material-symbols-outlined text-xs">add_circle</span>
                  New Policy
                </button>
              </div>
              <table className="w-full text-left text-xs">
                <thead>
                  <tr className="text-zinc-500 border-b border-[#574149]/10">
                    <th className="pb-3 font-medium">Policy Name</th>
                    <th className="pb-3 font-medium">Alert Trigger</th>
                    <th className="pb-3 font-medium">Enforcement</th>
                    <th className="pb-3 font-medium">Status</th>
                  </tr>
                </thead>
                <tbody className="divide-y divide-[#574149]/5">
                  {budgetPolicies.map(p => (
                    <tr key={p.name}>
                      <td className="py-4 font-medium text-[#e7dff0]">{p.name}</td>
                      <td className="py-4 text-zinc-400">{p.trigger}</td>
                      <td className="py-4"><span className={`px-2 py-0.5 text-[10px] rounded font-semibold ${p.enfClass}`}>{p.enforcement}</span></td>
                      <td className="py-4">
                        <div className={`flex items-center gap-1.5 ${p.statusColor}`}>
                          <div className={`w-1.5 h-1.5 rounded-full ${p.dot}`}></div>
                          {p.status}
                        </div>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>

            <div className="bg-[#2c2834] p-6 rounded">
              <h4 className="text-base font-semibold text-[#e7dff0] mb-6">Anomaly History</h4>
              <div className="space-y-4">
                {anomalies.map((a, i) => (
                  <div key={i} className={`bg-[#211e29] p-4 rounded border-l-2 ${a.border}`}>
                    <div className="flex justify-between items-start">
                      <div className="flex items-center gap-3">
                        <div className={`p-2 rounded ${a.iconBg}`}>
                          <span className="material-symbols-outlined">{a.icon}</span>
                        </div>
                        <div>
                          <p className="font-semibold text-[#e7dff0]">{a.title}</p>
                          <p className="text-xs text-zinc-500">{a.sub}</p>
                        </div>
                      </div>
                      <p className="text-[10px] text-zinc-500 font-semibold">{a.age}</p>
                    </div>
                    {a.actions && (
                      <div className="mt-4 flex gap-2">
                        <button className="px-3 py-1 bg-[#93000a] text-[#ffdad6] text-[10px] font-semibold rounded">QUARANTINE AGENT</button>
                        <button className="px-3 py-1 bg-[#37333f] text-zinc-300 text-[10px] font-semibold rounded">DISMISS</button>
                      </div>
                    )}
                  </div>
                ))}
              </div>
            </div>
          </div>
        </div>
      </main>
    </div>
  )
}
