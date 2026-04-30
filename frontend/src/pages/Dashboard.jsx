import { useState, useEffect, useCallback } from 'react'
import { useNavigate } from 'react-router-dom'
import Sidebar from '../components/Sidebar'
import TopNav from '../components/TopNav'
import PageHeader from '../components/PageHeader'
import { api } from '../api/client'
import LiveAgentActivity from '../components/LiveAgentActivity'

const sqlNotifyEvents = `
SELECT detected_at, rule_id, rule_name, severity, agent_id, deployment_id, session_id, hook_name, message
FROM agentguard_notify_events
SETTINGS seek_to='-24h'`

const severityCard = {
  critical: { border: 'border-[#ffb4ab]', iconColor: 'text-[#ffb4ab]' },
  warning:  { border: 'border-[#ffb2b8]', iconColor: 'text-[#ffb2b8]' },
  info:     { border: 'border-zinc-700',  iconColor: 'text-zinc-500'  },
}

const CATEGORY_ICONS = {
  prompt_injection_and_hijacking:  'phishing',
  sensitive_data_exposure:         'visibility_off',
  agent_privilege_escalation:      'admin_panel_settings',
  supply_chain_compromise:         'hub',
  resource_exhaustion_and_dos:     'speed',
  data_exfiltration_and_leakage:   'data_loss_prevention',
  credential_theft_and_harvesting: 'key_off',
  lateral_movement_and_pivoting:   'swap_horiz',
}

function formatThreatTime(ts) {
  if (!ts) return '—'
  try {
    return new Date(typeof ts === 'string' ? ts.replace(' ', 'T') : ts)
      .toLocaleTimeString([], { hour: '2-digit', minute: '2-digit', second: '2-digit' })
  } catch { return String(ts) }
}


export default function Dashboard() {
  const navigate = useNavigate()
  const [data, setData] = useState(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState(null)
  const [threats, setThreats] = useState([])
  const [threatsConnected, setThreatsConnected] = useState(false)
  const [threatsError, setThreatsError] = useState(null)
  // rule_id → category, built once from the installed rules list
  const [ruleCategory, setRuleCategory] = useState({})

  useEffect(() => {
    api.getDashboard()
      .then(setData)
      .catch(setError)
      .finally(() => setLoading(false))

    api.listRules()
      .then(d => {
        const map = {}
        for (const r of (d.rule_packs || [])) map[r.id] = r.category
        setRuleCategory(map)
      })
      .catch(err => {
        // Decorative — only used to label rule categories in chart tooltips.
        // Log so the failure is at least visible in DevTools.
        console.warn('Dashboard: listRules failed (category labels will be missing):', err)
      })
  }, [])

  useEffect(() => {
    const ctrl = new AbortController()
    async function stream() {
      let first = true
      for await (const row of api.protonQuery(sqlNotifyEvents, { signal: ctrl.signal })) {
        if (first) { setThreatsConnected(true); first = false }
        setThreats(prev => {
          const key = r => `${r.deployment_id}|${r.agent_id}|${r.session_id}|${r.rule_id}`
          const next = [row, ...prev.filter(r => key(r) !== key(row))]
          return next.sort((a, b) => new Date(b.detected_at) - new Date(a.detected_at)).slice(0, 20)
        })
      }
      setThreatsConnected(false)
    }
    stream().catch(e => { if (e?.name !== 'AbortError') setThreatsError(e.message || 'Connection failed') })
    return () => ctrl.abort()
  }, [])

  const handleThreatClick = useCallback(async (t) => {
    try {
      const data = await api.getThreats({ agent_id: t.agent_id })
      const match = (data.threats || []).find(th =>
        th.rule_id === t.rule_id && th.session_id === t.session_id
      )
      navigate(match ? `/threats/${match.threat_id}` : '/threats')
    } catch (err) {
      console.error('dashboard: getThreats failed, falling back:', err)
      navigate('/threats')
    }
  }, [navigate])

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
                {threats.length === 0 ? (
                  <p className="text-xs py-4 flex items-center gap-2">
                    {threatsError
                      ? <><span className="material-symbols-outlined text-sm text-red-400">error</span><span className="text-red-400 font-mono">{threatsError}</span></>
                      : <span className="text-zinc-600">{threatsConnected ? 'Waiting for events…' : 'Monitoring…'}</span>
                    }
                  </p>
                ) : threats.map((t, i) => {
                  const sev = t.severity?.toLowerCase() || 'info'
                  const { border, iconColor } = severityCard[sev] || severityCard.info
                  const catIcon = CATEGORY_ICONS[ruleCategory[t.rule_id]] || 'shield'
                  return (
                    <div key={`${t.detected_at}-${t.rule_id}-${i}`} onClick={() => handleThreatClick(t)} className={`flex items-start gap-4 p-4 bg-gray-800 border border-gray-600 rounded-sm border-l-4 border-l-red-500 cursor-pointer transition-colors`}>
                      <div className="mt-1">
                        <span className={`material-symbols-outlined text-red-400`} style={{ fontVariationSettings: "'FILL' 1" }}>{catIcon}</span>
                      </div>
                      <div className="flex-1 min-w-0">
                        <div className="flex justify-between items-start gap-2">
                          <h4 className="text-xs font-semibold text-[#e7dff0] truncate">{t.rule_name}</h4>
                          <span className="text-[10px] font-mono text-zinc-500 whitespace-nowrap flex-shrink-0">{formatThreatTime(t.detected_at)}</span>
                        </div>
                        <p className="text-xs text-zinc-400 mt-1 line-clamp-1 italic">{t.message}</p>
                        <div className="flex gap-2 mt-3 flex-wrap">
                          {t.agent_id && <span className="px-2 py-0.5 bg-gray-600 text-gray-200 text-[9px] font-semibold uppercase rounded-full">{t.agent_id}</span>}
                          {t.hook_name && <span className="px-2 py-0.5 bg-gray-600 text-gray-200 text-[9px] font-semibold uppercase rounded-full">{t.hook_name}</span>}
                        </div>
                      </div>
                    </div>
                  )
                })}
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
