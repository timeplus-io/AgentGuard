import { useState, useEffect } from 'react'
import Sidebar from '../components/Sidebar'
import TopNav from '../components/TopNav'
import Footer from '../components/Footer'
import PageHeader from '../components/PageHeader'
import { SeverityBadge } from '../components/Badge'
import { api } from '../api/client'

const TIME_RANGES = [
  { label: '1h',  value: '-1h' },
  { label: '6h',  value: '-6h' },
  { label: '24h', value: '-24h' },
  { label: '7d',  value: '-7d' },
]

// Single streaming query per panel: Proton delivers historical backfill first,
// then continues streaming live events on the same connection.
const sqlThreats = (range) => `
SELECT detected_at, rule_id, rule_name, severity, agent_id,
       session_id, hook_name, tool_name, message
FROM agentshield_security_events
SETTINGS seek_to='${range}'`

const sqlActivity = (range) => `
SELECT hook_name, event_time, agent_id, tool_name, model, hook_decision
FROM agentshield_hook_events
WHERE hook_name IN ('llm_input', 'llm_output', 'before_tool_call', 'after_tool_call')
SETTINGS seek_to='${range}'`

function formatTs(ts) {
  if (!ts) return '—'
  try {
    return new Date(typeof ts === 'string' ? ts.replace(' ', 'T') : ts).toLocaleString()
  } catch { return String(ts) }
}

function formatEvtTime(ts) {
  if (!ts) return ''
  try {
    return new Date(typeof ts === 'string' ? ts.replace(' ', 'T') : ts)
      .toLocaleTimeString([], { hour: '2-digit', minute: '2-digit', second: '2-digit' })
  } catch { return String(ts) }
}

function formatActivity(evt) {
  if (evt.hook_name === 'llm_input')       return `called LLM${evt.model ? ` [${evt.model}]` : ''}`
  if (evt.hook_name === 'llm_output')      return `received LLM response${evt.model ? ` [${evt.model}]` : ''}`
  if (evt.hook_name === 'before_tool_call') return `invoked tool ${evt.tool_name || '(unknown)'}`
  if (evt.hook_name === 'after_tool_call')  return `completed tool ${evt.tool_name || '(unknown)'}`
  return evt.hook_name
}

export default function Threats() {
  const [threats, setThreats] = useState([])
  const [liveActivity, setLiveActivity] = useState([])
  const [connected, setConnected] = useState(false)
  const [severity, setSeverity] = useState('All')
  const [category, setCategory] = useState('All Categories')
  const [timeRange, setTimeRange] = useState('-24h')

  useEffect(() => {
    // Clear existing data when time range changes so old backfill doesn't linger
    setThreats([])
    setLiveActivity([])
    setConnected(false)

    const threatCtrl = new AbortController()
    const activityCtrl = new AbortController()

    async function streamThreats() {
      let first = true
      for await (const row of api.protonQuery(sqlThreats(timeRange), { signal: threatCtrl.signal })) {
        if (first) { setConnected(true); first = false }
        setThreats(prev => [row, ...prev].slice(0, 100))
      }
      setConnected(false)
    }

    async function streamActivity() {
      for await (const row of api.protonQuery(sqlActivity(timeRange), { signal: activityCtrl.signal })) {
        setLiveActivity(prev => [row, ...prev].slice(0, 20))
      }
    }

    streamThreats().catch(e => { if (e?.name !== 'AbortError') setConnected(false) })
    streamActivity().catch(() => {})

    return () => {
      threatCtrl.abort()
      activityCtrl.abort()
    }
  }, [timeRange])

  const filtered = threats.filter(t => {
    if (severity !== 'All' && t.severity?.toLowerCase() !== severity.toLowerCase()) return false
    if (category !== 'All Categories') {
      const catMap = { Injection: 'rp-001', Exfiltration: 'rp-002', 'Supply Chain': 'rp-004' }
      const mappedId = catMap[category]
      if (!mappedId || !t.rule_id?.startsWith(mappedId)) return false
    }
    return true
  })

  return (
    <div className="min-h-screen bg-[#15121d] text-[#e7dff0]">
      <Sidebar />
      <TopNav />
      <Footer />
      <main className="pl-64 pt-16 pb-8 min-h-screen">
        <div className="p-8">
          <PageHeader
            title="Threats"
            subtitle="Real-time surveillance of AI agent activity and anomalous patterns."
          />

          {/* Filter Bar */}
          <section className="bg-[#211e29] rounded p-6 mb-6 flex flex-wrap items-center gap-6">
            <div className="flex flex-col gap-2">
              <label className="text-[10px] font-semibold text-zinc-500 uppercase tracking-widest">Time Range</label>
              <div className="flex gap-1">
                {TIME_RANGES.map(r => (
                  <button
                    key={r.value}
                    onClick={() => setTimeRange(r.value)}
                    className={`px-3 py-1.5 text-[11px] font-semibold rounded-full transition-colors ${
                      timeRange === r.value
                        ? 'bg-[#ffb0cf]/20 text-[#ffb0cf] border border-[#ffb0cf]/30'
                        : 'bg-[#2c2834] text-zinc-400 border border-[#574149]/10 hover:text-white'
                    }`}
                  >
                    {r.label}
                  </button>
                ))}
              </div>
            </div>
            <div className="flex flex-col gap-2 flex-1 min-w-[200px]">
              <label className="text-[10px] font-semibold text-zinc-500 uppercase tracking-widest">Severity</label>
              <div className="flex gap-1">
                {['Critical', 'Warning', 'Info'].map(s => (
                  <button
                    key={s}
                    onClick={() => setSeverity(prev => prev === s ? 'All' : s)}
                    className={`px-3 py-1.5 text-[11px] font-semibold rounded-full transition-colors ${
                      severity === s
                        ? s === 'Critical' ? 'bg-[#93000a] text-[#ffb4ab]'
                          : s === 'Warning' ? 'bg-[#37333f] text-[#ffb2b8]'
                          : 'bg-[#2c2834] text-zinc-300'
                        : 'bg-[#2c2834] text-[#ddbfc8] border border-[#574149]/10'
                    }`}
                  >
                    {s}
                  </button>
                ))}
              </div>
            </div>
            <div className="flex flex-col gap-2 flex-1 min-w-[200px]">
              <label className="text-[10px] font-semibold text-zinc-500 uppercase tracking-widest">Threat Category</label>
              <select
                value={category}
                onChange={e => setCategory(e.target.value)}
                className="bg-[#0f0c17] border-none text-xs font-medium text-[#e7dff0] rounded-sm py-2 px-3 focus:ring-1 focus:ring-[#ffb0cf]/50"
              >
                <option>All Categories</option>
                <option>Injection</option>
                <option>Exfiltration</option>
                <option>Supply Chain</option>
              </select>
            </div>
          </section>

          {/* Threats Table */}
          <section className="bg-[#211e29] rounded-sm overflow-hidden mb-8">
            <div className="px-6 py-4 flex items-center justify-between border-b border-[#574149]/10">
              <span className="text-xs font-semibold text-zinc-400 uppercase tracking-widest">
                Security Events
                <span className="ml-2 text-zinc-600 font-normal normal-case">({filtered.length})</span>
              </span>
              {connected && (
                <span className="flex items-center gap-1.5 text-[10px] font-semibold text-[#ffb0cf] uppercase tracking-widest">
                  <span className="w-1.5 h-1.5 rounded-full bg-[#ffb0cf] animate-pulse" />
                  Live
                </span>
              )}
            </div>
            <div className="overflow-x-auto">
              <table className="w-full text-left border-collapse">
                <thead className="bg-[#2c2834]/50">
                  <tr>
                    {['Severity', 'Rule / Message', 'Agent', 'Session', 'Hook', 'Time'].map(h => (
                      <th key={h} className="px-6 py-4 text-[10px] font-bold text-zinc-400 uppercase tracking-widest">{h}</th>
                    ))}
                  </tr>
                </thead>
                <tbody className="divide-y divide-[#574149]/10">
                  {filtered.length === 0 ? (
                    <tr>
                      <td colSpan={6} className="px-6 py-12 text-center text-zinc-500 text-xs">
                        {threats.length > 0
                          ? 'No threats match the current filters.'
                          : connected ? 'Connecting to threat stream…' : 'No threats detected in the selected time range.'}
                      </td>
                    </tr>
                  ) : filtered.map((t, i) => (
                    <tr key={`${t.detected_at}-${t.rule_id}-${t.agent_id}-${i}`} className="hover:bg-[#2c2834] transition-colors">
                      <td className="px-6 py-4"><SeverityBadge severity={t.severity} /></td>
                      <td className="px-6 py-4">
                        <p className="text-xs font-semibold text-[#e7dff0]">{t.rule_name}</p>
                        <p className="text-[10px] text-zinc-500 font-medium max-w-xs truncate">{t.message}</p>
                      </td>
                      <td className="px-6 py-4">
                        <code className="text-xs font-mono text-[#ffb0cf] px-2 py-0.5 bg-[#ffb0cf]/5 rounded-sm">{t.agent_id}</code>
                      </td>
                      <td className="px-6 py-4">
                        <span className="text-xs text-zinc-400 font-mono">
                          {t.session_id
                            ? t.session_id.length > 12
                              ? t.session_id.slice(0, 12) + '…'
                              : t.session_id
                            : '—'}
                        </span>
                      </td>
                      <td className="px-6 py-4">
                        <span className="text-[10px] text-zinc-500">{t.hook_name}</span>
                      </td>
                      <td className="px-6 py-4">
                        <span className="text-xs text-zinc-400">{formatTs(t.detected_at)}</span>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          </section>

          {/* Live Activity Panel */}
          <section className="bg-[#2c2834] p-8 rounded-sm relative overflow-hidden">
            <div className="absolute top-0 right-0 p-4 opacity-5">
              <span className="material-symbols-outlined text-[120px]">shield_with_heart</span>
            </div>
            <h3 className="text-sm font-semibold text-zinc-400 uppercase tracking-widest mb-6 flex items-center gap-3">
              <span className="w-2 h-2 rounded-full bg-[#ffb0cf] shadow-[0_0_10px_#ffb0cf] animate-pulse" />
              Live Agent Activity
            </h3>
            <div className="space-y-3">
              {liveActivity.length === 0 ? (
                <p className="text-xs text-zinc-600">Waiting for agent activity…</p>
              ) : liveActivity.map((evt, i) => (
                <div key={`${evt.event_time}-${evt.agent_id}-${evt.hook_name}-${i}`} className="flex items-start gap-4">
                  <span className="text-[10px] font-mono text-[#ffb0cf] py-0.5 whitespace-nowrap flex-shrink-0">
                    {formatEvtTime(evt.event_time)}
                  </span>
                  <p className="text-xs text-zinc-300">
                    <span className="font-semibold text-white">{evt.agent_id}</span>
                    {' '}{formatActivity(evt)}
                    {evt.hook_decision && evt.hook_decision !== 'allow' && (
                      <span className={`ml-2 text-[10px] px-1.5 py-0.5 rounded-full font-semibold uppercase ${
                        evt.hook_decision === 'block'
                          ? 'bg-red-500/20 text-red-400'
                          : 'bg-amber-500/20 text-amber-400'
                      }`}>{evt.hook_decision}</span>
                    )}
                  </p>
                </div>
              ))}
            </div>
          </section>

        </div>
      </main>
    </div>
  )
}
