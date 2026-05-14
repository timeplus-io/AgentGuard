import { useState, useEffect } from 'react'
import Sidebar from '../components/Sidebar'
import TopNav from '../components/TopNav'
import PageHeader from '../components/PageHeader'
import { api } from '../api/client'
import LiveAgentActivity from '../components/LiveAgentActivity'
import { LiveThreatFeedList } from '../components/LiveThreatFeed'
import { useLiveThreatFeed, useThreatNavigation } from '../hooks/useLiveThreatFeed'


export default function Dashboard() {
  const [data, setData] = useState(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState(null)
  const {
    threats,
    connected: threatsConnected,
    error: threatsError,
    ruleCategory,
  } = useLiveThreatFeed(20)
  const handleThreatClick = useThreatNavigation()

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
      deltaColor: 'text-emerald-400', icon: 'smart_toy', iconColor: 'text-[#ffb0cf]', deltaIcon: 'trending_up',
    },
    {
      label: 'Active Threats (24h)', value: data.kpis.active_threats,
      delta: `${data.kpis.critical_escalations} Critical escalations`,
      deltaColor: data.kpis.critical_escalations > 0 ? 'text-[#ffb4ab]' : 'text-zinc-500',
      icon: 'warning', iconColor: 'text-[#ffb4ab]', deltaIcon: 'priority_high',
    },
    {
      label: 'Total Token Cost (24h)', value: data.kpis.total_cost_24h,
      delta: data.kpis.cost_status,
      deltaColor: 'text-zinc-500', icon: 'monetization_on', iconColor: 'text-[#ffb0cf]', deltaIcon: 'horizontal_rule',
    },
    {
      label: 'Active Rules', value: data.kpis.active_rules,
      delta: 'security policies enforced',
      deltaColor: 'text-emerald-400', icon: 'verified_user', iconColor: 'text-[#ffb2b8]', deltaIcon: 'check_circle',
    },
  ] : [
    { label: 'Total Agents', value: '—', delta: 'loading…', deltaColor: 'text-zinc-600', icon: 'smart_toy', iconColor: 'text-[#ffb0cf]', deltaIcon: 'trending_up' },
    { label: 'Active Threats (24h)', value: '—', delta: 'loading…', deltaColor: 'text-zinc-600', icon: 'warning', iconColor: 'text-[#ffb4ab]', deltaIcon: 'priority_high' },
    { label: 'Total Token Cost (24h)', value: '—', delta: 'loading…', deltaColor: 'text-zinc-600', icon: 'monetization_on', iconColor: 'text-[#ffb0cf]', deltaIcon: 'horizontal_rule' },
    { label: 'Active Rules', value: '—', delta: 'loading…', deltaColor: 'text-zinc-600', icon: 'verified_user', iconColor: 'text-[#ffb2b8]', deltaIcon: 'check_circle' },
  ]

  return (
    <div className="min-h-screen bg-gray-900 text-gray-100">
      <Sidebar />
      <TopNav />

      <main className="pl-sidebar pt-16 pb-8 min-h-screen">
        <div className="p-6 lg:p-10 space-y-8">
          <PageHeader
            title="Security Posture"
            subtitle="Real-time defensive intelligence for distributed AI agents."
          />

          {/* KPI Bento Grid */}
          {error && (
            <div className="flex items-center gap-3 px-4 py-3 bg-[#93000a]/20 border border-[#ffb4ab]/30 rounded-sm text-[#ffb4ab] text-xs mb-2">
              <span className="material-symbols-outlined text-base shrink-0">error</span>
              <span>{typeof error === 'string' ? error : error?.message || 'Failed to load dashboard data'}</span>
            </div>
          )}
          {loading ? (
            <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-4 gap-6">
              {[1,2,3,4].map(i => (
                <div key={i} className="bg-gray-800 border border-gray-700 p-6 rounded-sm animate-pulse h-32" />
              ))}
            </div>
          ) : (
            <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-4 gap-6">
              {kpis.map((kpi, i) => (
                <div key={i} className="bg-gray-800 border border-gray-700 p-6 rounded-sm flex flex-col justify-between group transition-all">
                  <div className="flex justify-between items-start">
                    <span className="text-xs font-semibold text-zinc-400 uppercase tracking-widest">{kpi.label}</span>
                    <span className={`material-symbols-outlined ${kpi.iconColor}`} style={{ fontVariationSettings: "'FILL' 1" }}>{kpi.icon}</span>
                  </div>
                  <div className="mt-4">
                    <p className="text-3xl font-bold text-[#e7dff0]">{kpi.value}</p>
                    <div className={`flex items-center gap-1 mt-1 ${kpi.deltaColor}`}>
                      <span className="material-symbols-outlined text-xs">{kpi.deltaIcon}</span>
                      <span className="text-[10px] font-semibold">{kpi.delta}</span>
                    </div>
                  </div>
                </div>
              ))}
            </div>
          )}

          {/* Main Area */}
          <div className="grid grid-cols-1 lg:grid-cols-3 gap-6">
            {/* Live Threat Feed */}
            <div className="lg:col-span-2 bg-gray-800 border border-gray-700 rounded-sm flex flex-col">
              <div className="p-6 flex justify-between items-center">
                <h3 className="text-base font-semibold text-[#e7dff0] flex items-center gap-2">
                  Live Threat Feed
                  <span className={`flex h-2 w-2 rounded-full ${threatsConnected ? 'bg-red-400 animate-pulse' : 'bg-zinc-600'}`}></span>
                </h3>
              </div>
              <div className="overflow-y-auto max-h-[500px] px-6 pb-6 space-y-4">
                <LiveThreatFeedList
                  threats={threats}
                  connected={threatsConnected}
                  error={threatsError}
                  ruleCategory={ruleCategory}
                  onThreatClick={handleThreatClick}
                />
              </div>
            </div>

            {/* Threat Categories */}
            <div className="bg-gray-800 border border-gray-700 rounded-sm p-6 flex flex-col">
              <h3 className="text-base font-semibold text-[#e7dff0] mb-6">Threat Categories</h3>
              <div className="space-y-6 flex-1">
                {(data?.threat_categories ?? []).length === 0 ? (
                  <p className="text-xs text-zinc-600">No detections in the last 24h.</p>
                ) : (data?.threat_categories ?? []).map(cat => (
                  <div key={cat.name}>
                    <div className="flex justify-between items-end mb-2">
                      <span className="text-xs font-semibold text-[#e7dff0] uppercase tracking-tighter">{cat.name}</span>
                      <span className="text-xs font-mono text-[#ffb0cf]">{cat.percentage}%</span>
                    </div>
                    <div className="h-1.5 w-full bg-[#1d1a25] rounded-full overflow-hidden">
                      <div className="h-full bg-gradient-to-r from-red-500 to-pink-400" style={{ width: `${cat.percentage}%` }}></div>
                    </div>
                  </div>
                ))}
              </div>
              <div className="mt-8 p-4 bg-[#0f0c17] rounded-sm">
                <p className="text-[10px] text-zinc-500 uppercase font-bold mb-2">Global Heatmap</p>
                <div className="h-32 w-full bg-[#0f0c17] rounded-sm relative overflow-hidden">
                  <div className="absolute top-1/2 left-1/3 w-3 h-3 bg-[#ffb0cf] rounded-full blur-[2px] animate-pulse"></div>
                  <div className="absolute top-1/4 right-1/4 w-2 h-2 bg-[#ffb4ab] rounded-full blur-[1px] animate-pulse"></div>
                  <div className="absolute bottom-1/3 left-1/2 w-2 h-2 bg-[#ffb0cf] rounded-full blur-[1px] animate-pulse"></div>
                  <div className="absolute inset-0 flex items-center justify-center">
                    <span className="material-symbols-outlined text-zinc-700 text-5xl">public</span>
                  </div>
                </div>
              </div>
            </div>
          </div>

          <LiveAgentActivity timeRange="-24h" limit={10} />
        </div>
      </main>
    </div>
  )
}
