import React, { useState, useEffect } from 'react'
import { Link } from 'react-router-dom'
import { SeverityBadge } from './Badge'
import { api } from '../api/client'

function formatTs(ts) {
  if (!ts) return '—'
  try {
    const normalized = typeof ts === 'string'
      ? ts.replace(' ', 'T').replace(/\s+UTC$/, 'Z').replace(/\s+\+0000$/, 'Z').replace(/\s+[\w+\-:]+$/, 'Z')
      : ts
    const d = new Date(normalized)
    if (isNaN(d.getTime())) return String(ts)
    return d.toLocaleString()
  } catch { return String(ts) }
}

function StatusBadge({ status }) {
  const map = {
    open:         { label: 'Open',         cls: 'bg-blue-500/10 text-blue-400 border border-blue-400/30' },
    acknowledged: { label: 'Acknowledged', cls: 'bg-yellow-500/10 text-yellow-400 border border-yellow-400/30' },
    cleared:      { label: 'Cleared',      cls: 'bg-green-500/10 text-green-400 border border-green-400/30' },
  }
  const { label, cls } = map[status] || { label: status, cls: 'bg-zinc-500/10 text-zinc-400 border border-zinc-400/30' }
  return (
    <span className={`text-[10px] font-bold uppercase rounded-full px-2 py-0.5 ${cls}`}>
      {label}
    </span>
  )
}

/**
 * ThreatsList — reusable deduplicated threats table.
 *
 * Props:
 *   ruleId      — if set, only shows threats for that rule (client-side filter)
 *   hideRuleCol — hides the "Rule / Message" column (useful when already on a rule page)
 *   showFilters — renders the severity + status filter bar (default true)
 */
export default function ThreatsList({ ruleId, hideRuleCol = false, showFilters = true }) {
  const [threats, setThreats] = useState([])
  const [loading, setLoading] = useState(true)
  const [severityFilter, setSeverityFilter] = useState('All')
  const [statusFilter, setStatusFilter] = useState('All')
  const [expandedId, setExpandedId] = useState(null)

  useEffect(() => {
    let cancelled = false
    async function fetchThreats() {
      try {
        const data = await api.getThreats()
        if (!cancelled) setThreats(data.threats || [])
      } catch {
        // silently ignore, keep stale data
      } finally {
        if (!cancelled) setLoading(false)
      }
    }
    fetchThreats()
    const timer = setInterval(fetchThreats, 30_000)
    return () => { cancelled = true; clearInterval(timer) }
  }, [])

  const filtered = threats.filter(t => {
    if (ruleId && t.rule_id !== ruleId) return false
    if (severityFilter !== 'All' && t.severity?.toLowerCase() !== severityFilter.toLowerCase()) return false
    if (statusFilter !== 'All' && t.status !== statusFilter.toLowerCase()) return false
    return true
  })

  async function handleAcknowledge(threat) {
    setThreats(prev => prev.map(t =>
      t.threat_id === threat.threat_id ? { ...t, status: 'acknowledged' } : t
    ))
    try {
      await api.acknowledgeThreat(threat.threat_id, {
        agent_id: threat.agent_id,
        deployment_id: threat.deployment_id,
        session_id: threat.session_id,
        rule_id: threat.rule_id,
      })
    } catch {
      setThreats(prev => prev.map(t =>
        t.threat_id === threat.threat_id ? { ...t, status: threat.status } : t
      ))
    }
  }

  async function handleClear(threat) {
    setThreats(prev => prev.map(t =>
      t.threat_id === threat.threat_id ? { ...t, status: 'cleared' } : t
    ))
    try {
      await api.clearThreat(threat.threat_id, {
        agent_id: threat.agent_id,
        deployment_id: threat.deployment_id,
        session_id: threat.session_id,
        rule_id: threat.rule_id,
      })
    } catch {
      setThreats(prev => prev.map(t =>
        t.threat_id === threat.threat_id ? { ...t, status: threat.status } : t
      ))
    }
  }

  const colSpan = hideRuleCol ? 7 : 8
  const headers = ['Severity', 'Status', ...(!hideRuleCol ? ['Rule / Message'] : []), 'Agent', 'Session', 'Events', 'Last Seen', 'Actions']

  return (
    <>
      {showFilters && (
        <section className="bg-[#211e29] rounded p-6 mb-6 flex flex-wrap items-center gap-6">
          <div className="flex flex-col gap-2">
            <label className="text-[10px] font-semibold text-zinc-500 uppercase tracking-widest">Severity</label>
            <div className="flex gap-1">
              {[
                { label: 'Critical', bg: 'bg-[#93000a]', text: 'text-[#ffb4ab]', dot: 'bg-[#ffb4ab]', ring: 'ring-[#ffb4ab]/50', pulse: true  },
                { label: 'Warning',  bg: 'bg-[#37333f]', text: 'text-[#ffb2b8]', dot: 'bg-[#ffb2b8]', ring: 'ring-[#ffb2b8]/50', pulse: false },
                { label: 'Info',     bg: 'bg-[#2c2834]', text: 'text-zinc-400',  dot: 'bg-zinc-400',  ring: 'ring-zinc-400/50',   pulse: false },
              ].map(({ label, bg, text, dot, ring, pulse }) => {
                const isActive = severityFilter === label
                return (
                  <button
                    key={label}
                    onClick={() => setSeverityFilter(prev => prev === label ? 'All' : label)}
                    className={`flex items-center gap-1.5 px-2.5 py-1 text-[10px] font-black uppercase rounded-full transition-all ${bg} ${text} ${
                      isActive ? `ring-2 ${ring} ring-offset-1 ring-offset-[#211e29]` : 'opacity-50 hover:opacity-80'
                    }`}
                  >
                    <span className={`w-1.5 h-1.5 rounded-full ${dot} ${isActive && pulse ? 'animate-pulse' : ''}`} />
                    {label}
                  </button>
                )
              })}
            </div>
          </div>

          <div className="flex flex-col gap-2">
            <label className="text-[10px] font-semibold text-zinc-500 uppercase tracking-widest">Status</label>
            <div className="flex gap-1">
              {['All', 'open', 'acknowledged', 'cleared'].map(s => (
                <button
                  key={s}
                  onClick={() => setStatusFilter(s)}
                  className={`px-3 py-1.5 text-[11px] font-semibold rounded-full transition-colors capitalize ${
                    statusFilter === s
                      ? 'bg-[#ffb0cf]/20 text-[#ffb0cf] border border-[#ffb0cf]/30'
                      : 'bg-[#2c2834] text-zinc-400 border border-[#574149]/10 hover:text-white'
                  }`}
                >
                  {s === 'All' ? 'All' : s.charAt(0).toUpperCase() + s.slice(1)}
                </button>
              ))}
            </div>
          </div>
        </section>
      )}

      <section className="bg-[#211e29] rounded-sm overflow-hidden mb-8">
        <div className="px-6 py-4 flex items-center justify-between border-b border-[#574149]/10">
          <span className="text-xs font-semibold text-zinc-400 uppercase tracking-widest">
            Threats
            <span className="ml-2 text-zinc-600 font-normal normal-case">({filtered.length})</span>
          </span>
        </div>
        <div className="overflow-auto">
          <table className="w-full text-left border-collapse">
            <thead className="bg-[#2c2834] sticky top-0 z-10">
              <tr>
                {headers.map(h => (
                  <th key={h} className="px-6 py-4 text-[10px] font-bold text-zinc-400 uppercase tracking-widest whitespace-nowrap">
                    {h}
                  </th>
                ))}
              </tr>
            </thead>
            <tbody className="divide-y divide-[#574149]/10">
              {loading ? (
                <tr>
                  <td colSpan={colSpan} className="px-6 py-12 text-center text-zinc-500 text-xs">
                    Loading threats…
                  </td>
                </tr>
              ) : filtered.length === 0 ? (
                <tr>
                  <td colSpan={colSpan} className="px-6 py-12 text-center text-zinc-500 text-xs">
                    {threats.length > 0 ? 'No threats match the current filters.' : 'No threats detected.'}
                  </td>
                </tr>
              ) : filtered.map(t => (
                <React.Fragment key={t.threat_id}>
                  <tr
                    onClick={() => setExpandedId(prev => prev === t.threat_id ? null : t.threat_id)}
                    className="hover:bg-[#2c2834] transition-colors cursor-pointer"
                  >
                    <td className="px-6 py-4">
                      <SeverityBadge severity={t.severity} />
                    </td>
                    <td className="px-6 py-4">
                      <StatusBadge status={t.status} />
                    </td>
                    {!hideRuleCol && (
                      <td className="px-6 py-4 max-w-xs">
                        {t.rule_id ? (
                          <Link
                            to={`/rules/${t.rule_id}`}
                            onClick={e => e.stopPropagation()}
                            className="text-xs font-semibold text-[#e7dff0] hover:text-[#ffb0cf] transition-colors"
                          >
                            {t.rule_name}
                          </Link>
                        ) : (
                          <p className="text-xs font-semibold text-[#e7dff0]">{t.rule_name}</p>
                        )}
                        {t.events && t.events[0]?.message && (
                          <p className="text-[10px] text-zinc-500 font-medium truncate">{t.events[0].message}</p>
                        )}
                      </td>
                    )}
                    <td className="px-6 py-4">
                      <code className="text-xs font-mono text-[#ffb0cf] px-2 py-0.5 bg-[#ffb0cf]/5 rounded-sm">
                        {t.agent_id}
                      </code>
                      {t.deployment_id && (
                        <p className="text-[10px] text-zinc-500 font-medium mt-0.5 truncate max-w-[160px]">
                          {t.deployment_id}
                        </p>
                      )}
                    </td>
                    <td className="px-6 py-4">
                      {t.session_id ? (
                        <code className="text-[10px] font-mono text-zinc-400 px-1.5 py-0.5 bg-[#2c2834] rounded-sm truncate block max-w-[120px]" title={t.session_id}>
                          {t.session_id.length > 12 ? t.session_id.slice(0, 12) + '…' : t.session_id}
                        </code>
                      ) : (
                        <span className="text-zinc-600 text-xs">—</span>
                      )}
                    </td>
                    <td className="px-6 py-4">
                      <span className="text-xs text-zinc-300 font-semibold">{t.event_count} events</span>
                      <p className="text-[10px] text-zinc-600 whitespace-nowrap">{formatTs(t.first_seen)}</p>
                    </td>
                    <td className="px-6 py-4">
                      <span className="text-xs text-zinc-400 whitespace-nowrap">{formatTs(t.last_seen)}</span>
                    </td>
                    <td className="px-6 py-4" onClick={e => e.stopPropagation()}>
                      <div className="flex items-center gap-1.5">
                        {t.status === 'open' && (
                          <>
                            <button
                              onClick={() => handleAcknowledge(t)}
                              className="px-2.5 py-1 text-[10px] font-semibold rounded-sm bg-yellow-500/10 text-yellow-400 hover:bg-yellow-500/20 transition-colors whitespace-nowrap"
                            >
                              Acknowledge
                            </button>
                            <button
                              onClick={() => handleClear(t)}
                              className="px-2.5 py-1 text-[10px] font-semibold rounded-sm bg-green-500/10 text-green-400 hover:bg-green-500/20 transition-colors"
                            >
                              Clear
                            </button>
                          </>
                        )}
                        {t.status === 'acknowledged' && (
                          <button
                            onClick={() => handleClear(t)}
                            className="px-2.5 py-1 text-[10px] font-semibold rounded-sm bg-green-500/10 text-green-400 hover:bg-green-500/20 transition-colors"
                          >
                            Clear
                          </button>
                        )}
                      </div>
                    </td>
                  </tr>

                  {expandedId === t.threat_id && (
                    <tr key={`${t.threat_id}-expanded`} className="bg-[#0f0c17]">
                      <td colSpan={colSpan} className="px-6 py-4">
                        <p className="text-[10px] font-semibold text-zinc-500 uppercase tracking-widest mb-3">
                          Event History
                        </p>
                        <div className="space-y-2">
                          {t.events && t.events.length > 0 ? (
                            t.events.map((ev, i) => (
                              <div key={i} className="flex items-start gap-4 text-xs text-zinc-400">
                                <span className="text-zinc-600 font-mono text-[10px] whitespace-nowrap">
                                  {formatTs(ev.detected_at)}
                                </span>
                                <span className="text-zinc-400">{ev.hook_name}</span>
                                <span className="text-zinc-300 flex-1">{ev.message}</span>
                              </div>
                            ))
                          ) : (
                            <p className="text-xs text-zinc-600">No event history available.</p>
                          )}
                        </div>
                      </td>
                    </tr>
                  )}
                </React.Fragment>
              ))}
            </tbody>
          </table>
        </div>
      </section>
    </>
  )
}
