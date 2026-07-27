import { useState, useEffect } from 'react'
import Sidebar from '../components/Sidebar'
import TopNav from '../components/TopNav'
import { api } from '../api/client'
import LiveAgentActivity from '../components/LiveAgentActivity'
import ThreatAnalytics from '../components/ThreatAnalytics'


export default function Dashboard() {
  const [data, setData] = useState(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState(null)

  useEffect(() => {
    api.getDashboard()
      .then(setData)
      .catch(setError)
      .finally(() => setLoading(false))
  }, [])

  const kpis = data ? [
    {
      label: 'Total Agents', value: data.kpis.total_agents,
      delta: data.kpis.agents_delta || 'all time',
      deltaColor: 'text-emerald-400', icon: 'smart_toy', iconColor: 'text-primary', deltaIcon: 'trending_up',
    },
    {
      label: 'Total Token Cost (24h)', value: data.kpis.total_cost_24h,
      delta: data.kpis.cost_status,
      deltaColor: 'text-gray-300', icon: 'monetization_on', iconColor: 'text-primary', deltaIcon: 'horizontal_rule',
    },
    {
      label: 'Active Rules', value: data.kpis.active_rules,
      delta: 'security policies enforced',
      deltaColor: 'text-emerald-400', icon: 'verified_user', iconColor: 'text-[#ffb2b8]', deltaIcon: 'check_circle',
    },
  ] : [
    { label: 'Total Agents', value: '—', delta: 'loading…', deltaColor: 'text-gray-300', icon: 'smart_toy', iconColor: 'text-primary', deltaIcon: 'trending_up' },
    { label: 'Total Token Cost (24h)', value: '—', delta: 'loading…', deltaColor: 'text-gray-300', icon: 'monetization_on', iconColor: 'text-primary', deltaIcon: 'horizontal_rule' },
    { label: 'Active Rules', value: '—', delta: 'loading…', deltaColor: 'text-gray-300', icon: 'verified_user', iconColor: 'text-[#ffb2b8]', deltaIcon: 'check_circle' },
  ]

  return (
    <div className="min-h-screen bg-gray-900 text-gray-100">
      <Sidebar />
      <TopNav />

      <main className="pl-sidebar pt-10 pb-8 min-h-screen">
        <div className="p-6 lg:p-10 space-y-8">
          {/* KPI Bento Grid */}
          {error && (
            <div className="flex items-center gap-3 px-4 py-3 bg-[#93000a]/20 border border-[#ffb4ab]/30 rounded-sm text-error text-xs mb-2">
              <span className="material-symbols-outlined text-base shrink-0">error</span>
              <span>{typeof error === 'string' ? error : error?.message || 'Failed to load dashboard data'}</span>
            </div>
          )}
          {loading ? (
            <div className="grid grid-cols-1 md:grid-cols-3 gap-6">
              {[1,2,3].map(i => (
                <div key={i} className="bg-gray-800 border border-gray-700 p-4 rounded-sm animate-pulse h-24" />
              ))}
            </div>
          ) : (
            <div className="grid grid-cols-1 md:grid-cols-3 gap-6">
              {kpis.map((kpi, i) => (
                <div key={i} className="bg-gray-800 border border-gray-700 p-4 rounded-sm flex flex-col justify-between group transition-all min-h-[96px]">
                  <div className="flex justify-between items-start">
                    <span className="text-xs font-semibold text-gray-300 uppercase tracking-widest">{kpi.label}</span>
                    <span className={`material-symbols-outlined ${kpi.iconColor}`} style={{ fontVariationSettings: "'FILL' 1" }}>{kpi.icon}</span>
                  </div>
                  <div className="mt-3">
                    <p className="text-2xl font-bold text-gray-100">{kpi.value}</p>
                    <div className={`flex items-center gap-1 mt-1 ${kpi.deltaColor}`}>
                      <span className="material-symbols-outlined text-xs">{kpi.deltaIcon}</span>
                      <span className="text-[10px] font-semibold">{kpi.delta}</span>
                    </div>
                  </div>
                </div>
              ))}
            </div>
          )}

          <ThreatAnalytics />

          <LiveAgentActivity timeRange="-24h" limit={10} />
        </div>
      </main>
    </div>
  )
}
