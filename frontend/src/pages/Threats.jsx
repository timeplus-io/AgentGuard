import { useState, useEffect } from 'react'
import { Link } from 'react-router-dom'
import Sidebar from '../components/Sidebar'
import TopNav from '../components/TopNav'
import Footer from '../components/Footer'
import PageHeader from '../components/PageHeader'
import { SeverityBadge } from '../components/Badge'
import { api } from '../api/client'
import LiveAgentActivity from '../components/LiveAgentActivity'

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

function formatTs(ts) {
  if (!ts) return '—'
  try {
    return new Date(typeof ts === 'string' ? ts.replace(' ', 'T') : ts).toLocaleString()
  } catch { return String(ts) }
}

export default function Threats() {
  const [threats, setThreats] = useState([])
  const [connected, setConnected] = useState(false)
  const [severity, setSeverity] = useState('All')
  const [category, setCategory] = useState('All Categories')
  const [timeRange, setTimeRange] = useState('-24h')

  useEffect(() => {
    // Clear existing data when time range changes so old backfill doesn't linger
    setThreats([])
    setConnected(false)

    const threatCtrl = new AbortController()

    async function streamThreats() {
      let first = true
      for await (const row of api.protonQuery(sqlThreats(timeRange), { signal: threatCtrl.signal })) {
        if (first) { setConnected(true); first = false }
        setThreats(prev => [row, ...prev].slice(0, 100))
      }
      setConnected(false)
    }

    streamThreats().catch(e => { if (e?.name !== 'AbortError') setConnected(false) })

    return () => { threatCtrl.abort() }
  }, [timeRange])

  const filtered = threats
    .filter(t => {
      if (severity !== 'All' && t.severity?.toLowerCase() !== severity.toLowerCase()) return false
      if (category !== 'All Categories') {
        const catMap = { Injection: 'rp-001', Exfiltration: 'rp-002', 'Supply Chain': 'rp-004' }
        const mappedId = catMap[category]
        if (!mappedId || !t.rule_id?.startsWith(mappedId)) return false
      }
      return true
    })
    .sort((a, b) => {
      const ta = new Date(typeof a.detected_at === 'string' ? a.detected_at.replace(' ', 'T') : a.detected_at)
      const tb = new Date(typeof b.detected_at === 'string' ? b.detected_at.replace(' ', 'T') : b.detected_at)
      return tb - ta
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
                {[
                  { label: 'Critical', activeBg: 'bg-[#93000a]',      activeText: 'text-[#ffb4ab]', inactiveBg: 'bg-[#93000a]/10', inactiveText: 'text-[#ffb4ab]/50', dot: 'bg-[#ffb4ab]', pulse: true  },
                  { label: 'Warning',  activeBg: 'bg-[#37333f]',      activeText: 'text-[#ffb2b8]', inactiveBg: 'bg-[#37333f]/50', inactiveText: 'text-[#ffb2b8]/50', dot: 'bg-[#ffb2b8]', pulse: false },
                  { label: 'Info',     activeBg: 'bg-[#2c2834]',      activeText: 'text-zinc-400',  inactiveBg: 'bg-transparent',  inactiveText: 'text-zinc-600',     dot: 'bg-zinc-500',  pulse: false },
                ].map(({ label, activeBg, activeText, inactiveBg, inactiveText, dot, pulse }) => {
                  const isActive = severity === label
                  return (
                    <button
                      key={label}
                      onClick={() => setSeverity(prev => prev === label ? 'All' : label)}
                      className={`flex items-center gap-1.5 px-3 py-1.5 text-[11px] font-black uppercase rounded-full transition-colors border ${
                        isActive
                          ? `${activeBg} ${activeText} border-transparent`
                          : `${inactiveBg} ${inactiveText} border-[#574149]/20`
                      }`}
                    >
                      <span className={`w-1.5 h-1.5 rounded-full ${dot} ${isActive && pulse ? 'animate-pulse' : ''}`} />
                      {label}
                    </button>
                  )
                })}
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
            <div className="overflow-auto max-h-96">
              <table className="w-full text-left border-collapse">
                <thead className="bg-[#2c2834] sticky top-0 z-10">
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
                  ) : filtered.map((t) => (
                    <tr key={`${t.detected_at}-${t.rule_id}-${t.agent_id}`} className="hover:bg-[#2c2834] transition-colors">
                      <td className="px-6 py-4"><SeverityBadge severity={t.severity} /></td>
                      <td className="px-6 py-4">
                        {t.rule_id ? (
                          <Link to={`/rules/${t.rule_id}`} className="text-xs font-semibold text-[#e7dff0] hover:text-[#ffb0cf] transition-colors">
                            {t.rule_name}
                          </Link>
                        ) : (
                          <p className="text-xs font-semibold text-[#e7dff0]">{t.rule_name}</p>
                        )}
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

          <LiveAgentActivity timeRange={timeRange} />

        </div>
      </main>
    </div>
  )
}
