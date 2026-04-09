import { useState, useEffect } from 'react'
import Sidebar from '../components/Sidebar'
import TopNav from '../components/TopNav'
import Footer from '../components/Footer'
import PageHeader from '../components/PageHeader'
import { api } from '../api/client'
import LiveAgentActivity from '../components/LiveAgentActivity'

const sqlThreats = `
SELECT detected_at, rule_id, rule_name, severity, agent_id, hook_name, message
FROM agentshield_security_events
SETTINGS seek_to='-1h'`

const severityCard = {
  critical: { border: 'border-[#ffb4ab]', icon: 'security_update_warning', iconColor: 'text-[#ffb4ab]' },
  warning:  { border: 'border-[#ffb2b8]', icon: 'warning',                 iconColor: 'text-[#ffb2b8]' },
  info:     { border: 'border-zinc-700',   icon: 'info',                    iconColor: 'text-zinc-500'  },
}

function formatThreatTime(ts) {
  if (!ts) return '—'
  try {
    return new Date(typeof ts === 'string' ? ts.replace(' ', 'T') : ts)
      .toLocaleTimeString([], { hour: '2-digit', minute: '2-digit', second: '2-digit' })
  } catch { return String(ts) }
}

const categories = [
  { label: 'LLM01: Prompt Injection', pct: 32 },
  { label: 'LLM02: Insecure Output', pct: 24 },
  { label: 'LLM06: Sensitive Data Leak', pct: 18 },
  { label: 'LLM10: Model Theft', pct: 12 },
]

export default function Dashboard() {
  const [data, setData] = useState(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState(null)
  const [threats, setThreats] = useState([])
  const [threatsConnected, setThreatsConnected] = useState(false)

  useEffect(() => {
    api.getDashboard()
      .then(setData)
      .catch(setError)
      .finally(() => setLoading(false))
  }, [])

  useEffect(() => {
    const ctrl = new AbortController()
    async function stream() {
      let first = true
      for await (const row of api.protonQuery(sqlThreats, { signal: ctrl.signal })) {
        if (first) { setThreatsConnected(true); first = false }
        setThreats(prev => [row, ...prev].slice(0, 20))
      }
      setThreatsConnected(false)
    }
    stream().catch(e => { if (e?.name !== 'AbortError') setThreatsConnected(false) })
    return () => ctrl.abort()
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
      label: 'Compliance Coverage', value: `${data.kpis.compliance_coverage}%`,
      delta: data.kpis.compliance_label,
      deltaColor: 'text-emerald-400', icon: 'verified_user', iconColor: 'text-[#ffb2b8]', deltaIcon: 'check_circle',
    },
  ] : [
    { label: 'Total Agents', value: '—', delta: 'loading…', deltaColor: 'text-zinc-600', icon: 'smart_toy', iconColor: 'text-[#ffb0cf]', deltaIcon: 'trending_up' },
    { label: 'Active Threats (24h)', value: '—', delta: 'loading…', deltaColor: 'text-zinc-600', icon: 'warning', iconColor: 'text-[#ffb4ab]', deltaIcon: 'priority_high' },
    { label: 'Total Token Cost (24h)', value: '—', delta: 'loading…', deltaColor: 'text-zinc-600', icon: 'monetization_on', iconColor: 'text-[#ffb0cf]', deltaIcon: 'horizontal_rule' },
    { label: 'Compliance Coverage', value: '—', delta: 'loading…', deltaColor: 'text-zinc-600', icon: 'verified_user', iconColor: 'text-[#ffb2b8]', deltaIcon: 'check_circle' },
  ]

  return (
    <div className="min-h-screen bg-[#15121d] text-[#e7dff0]">
      <Sidebar />
      <TopNav />
      <Footer />
      <main className="pl-64 pt-16 pb-8 min-h-screen">
        <div className="p-6 lg:p-10 space-y-8">
          <PageHeader
            title="Security Posture"
            subtitle="Real-time defensive intelligence for distributed AI agents."
          />

          {/* KPI Bento Grid */}
          {loading ? (
            <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-4 gap-6">
              {[1,2,3,4].map(i => (
                <div key={i} className="bg-[#2c2834] p-6 rounded-sm animate-pulse h-32" />
              ))}
            </div>
          ) : (
            <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-4 gap-6">
              {kpis.map((kpi, i) => (
                <div key={i} className="bg-[#2c2834] p-6 rounded-sm flex flex-col justify-between group transition-all">
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
            <div className="lg:col-span-2 bg-[#2c2834] rounded-sm flex flex-col">
              <div className="p-6 flex justify-between items-center">
                <h3 className="text-base font-semibold text-[#e7dff0] flex items-center gap-2">
                  Live Threat Feed
                  <span className={`flex h-2 w-2 rounded-full ${threatsConnected ? 'bg-[#ffb0cf] animate-pulse' : 'bg-zinc-600'}`}></span>
                </h3>
              </div>
              <div className="overflow-y-auto max-h-[500px] px-6 pb-6 space-y-4">
                {threats.length === 0 ? (
                  <p className="text-xs text-zinc-600 py-4">{threatsConnected ? 'Waiting for threats…' : 'Connecting…'}</p>
                ) : [...threats].sort((a, b) => {
                  const ta = new Date(typeof a.detected_at === 'string' ? a.detected_at.replace(' ', 'T') : a.detected_at)
                  const tb = new Date(typeof b.detected_at === 'string' ? b.detected_at.replace(' ', 'T') : b.detected_at)
                  return tb - ta
                }).map((t, i) => {
                  const sev = t.severity?.toLowerCase() || 'info'
                  const { border, icon, iconColor } = severityCard[sev] || severityCard.info
                  return (
                    <div key={`${t.detected_at}-${t.rule_id}-${i}`} className={`flex items-start gap-4 p-4 bg-[#1d1a25] rounded-sm border-l-2 ${border}`}>
                      <div className="mt-1">
                        <span className={`material-symbols-outlined ${iconColor}`} style={{ fontVariationSettings: "'FILL' 1" }}>{icon}</span>
                      </div>
                      <div className="flex-1 min-w-0">
                        <div className="flex justify-between items-start gap-2">
                          <h4 className="text-xs font-semibold text-[#e7dff0] truncate">{t.rule_name}</h4>
                          <span className="text-[10px] font-mono text-zinc-500 whitespace-nowrap flex-shrink-0">{formatThreatTime(t.detected_at)}</span>
                        </div>
                        <p className="text-xs text-zinc-400 mt-1 line-clamp-1 italic">{t.message}</p>
                        <div className="flex gap-2 mt-3 flex-wrap">
                          {t.agent_id && <span className="px-2 py-0.5 bg-[#2c2834] text-[#bcb6c5] text-[9px] font-semibold uppercase rounded-full">{t.agent_id}</span>}
                          {t.hook_name && <span className="px-2 py-0.5 bg-[#2c2834] text-[#bcb6c5] text-[9px] font-semibold uppercase rounded-full">{t.hook_name}</span>}
                        </div>
                      </div>
                    </div>
                  )
                })}
              </div>
            </div>

            {/* Threat Categories */}
            <div className="bg-[#2c2834] rounded-sm p-6 flex flex-col">
              <h3 className="text-base font-semibold text-[#e7dff0] mb-6">Threat Categories</h3>
              <div className="space-y-6 flex-1">
                {categories.map(cat => (
                  <div key={cat.label}>
                    <div className="flex justify-between items-end mb-2">
                      <span className="text-xs font-semibold text-[#e7dff0] uppercase tracking-tighter">{cat.label}</span>
                      <span className="text-xs font-mono text-[#ffb0cf]">{cat.pct}%</span>
                    </div>
                    <div className="h-1.5 w-full bg-[#1d1a25] rounded-full overflow-hidden">
                      <div className="h-full bg-gradient-to-r from-[#ffb0cf] to-[#f256a3]" style={{ width: `${cat.pct}%` }}></div>
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

          <LiveAgentActivity timeRange="-1h" limit={10} />
        </div>
      </main>
    </div>
  )
}
