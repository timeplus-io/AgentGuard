import React, { useState, useEffect } from 'react'
import { Link, useNavigate } from 'react-router-dom'
import { SeverityBadge, ThreatStatusBadge } from './Badge'
import { api } from '../api/client'

function AgentTypeLogo({ agentType }) {
  if (agentType === 'claudecode') {
    return <img src="/static/icons/claude-seeklogo.svg" alt="Claude Code" title="Claude Code" className="h-4 w-4 shrink-0" />
  }
  if (agentType === 'openclaw') {
    return <img src="/static/icons/openclaw-dark.svg" alt="OpenClaw" title="OpenClaw" className="h-4 w-4 shrink-0" />
  }
  if (agentType === 'hermes') {
    return <img src="/static/icons/nousresearch-dark.png" alt="Hermes" title="Hermes" className="h-4 w-4 shrink-0" />
  }
  return null
}

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
  const [loadError, setLoadError] = useState(null)
  const [actionError, setActionError] = useState(null)
  const [severityFilter, setSeverityFilter] = useState('All')
  const [statusFilter, setStatusFilter] = useState('All')
  const [agentTypeMap, setAgentTypeMap] = useState({})
  const navigate = useNavigate()

  useEffect(() => {
    api.listAgents()
      .then(data => {
        const map = {}
        for (const a of (data.agents || [])) map[a.agent_id] = a.agent_type
        setAgentTypeMap(map)
      })
      .catch(err => {
        // Decorative — only affects the agent-type icon column. Log so the
        // failure is visible in DevTools rather than completely silent.
        console.warn('ThreatsList: listAgents failed (icons will be missing):', err)
      })
  }, [])

  useEffect(() => {
    let cancelled = false
    async function fetchThreats() {
      try {
        const data = await api.getThreats()
        if (!cancelled) { setThreats(data.threats || []); setLoadError(null) }
      } catch (err) {
        if (!cancelled) setLoadError(err?.message || 'Failed to load threats')
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
    setActionError(null)
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
    } catch (err) {
      console.error('ThreatsList: acknowledge failed:', err)
      setThreats(prev => prev.map(t =>
        t.threat_id === threat.threat_id ? { ...t, status: threat.status } : t
      ))
      setActionError(`Acknowledge failed: ${err?.message ?? String(err)}`)
    }
  }

  async function handleClear(threat) {
    setActionError(null)
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
    } catch (err) {
      console.error('ThreatsList: clear failed:', err)
      setThreats(prev => prev.map(t =>
        t.threat_id === threat.threat_id ? { ...t, status: threat.status } : t
      ))
      setActionError(`Clear failed: ${err?.message ?? String(err)}`)
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
              {['All', 'Critical', 'Warning', 'Info'].map(label => {
                const isActive = severityFilter === label
                return (
                  <button
                    key={label}
                    onClick={() => setSeverityFilter(prev => prev === label ? 'All' : label)}
                    className={`px-3 py-1.5 text-[11px] font-semibold rounded-full transition-colors ${
                      isActive
                        ? 'bg-gray-800 text-gray-200 border border-pink-500'
                        : 'bg-gray-800 text-gray-400 border border-gray-600 hover:text-gray-200 hover:border-pink-500'
                    }`}
                  >
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
                      ? 'bg-gray-800 text-gray-200 border border-pink-500'
                      : 'bg-gray-800 text-gray-400 border border-gray-600 hover:text-gray-200 hover:border-pink-500'
                  }`}
                >
                  {s === 'All' ? 'All' : s.charAt(0).toUpperCase() + s.slice(1)}
                </button>
              ))}
            </div>
          </div>
        </section>
      )}

      {actionError && (
        <div className="mb-3 flex items-center gap-2 px-4 py-2 bg-[#93000a]/20 border border-[#ffb4ab]/30 rounded-sm text-[#ffb4ab] text-xs">
          <span className="material-symbols-outlined text-base shrink-0">error</span>
          <span className="flex-1">{actionError}</span>
          <button onClick={() => setActionError(null)} className="text-[#ffb4ab] hover:text-white">
            <span className="material-symbols-outlined text-sm">close</span>
          </button>
        </div>
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
              ) : loadError ? (
                <tr>
                  <td colSpan={colSpan} className="px-6 py-12 text-center">
                    <div className="flex flex-col items-center gap-2">
                      <span className="material-symbols-outlined text-[#ffb4ab] text-3xl">error</span>
                      <p className="text-xs text-[#ffb4ab]">{loadError}</p>
                    </div>
                  </td>
                </tr>
              ) : filtered.length === 0 ? (
                <tr>
                  <td colSpan={colSpan} className="px-6 py-12 text-center text-zinc-500 text-xs">
                    {threats.length > 0 ? 'No threats match the current filters.' : 'No threats detected.'}
                  </td>
                </tr>
              ) : filtered.map(t => (
                <tr
                    key={t.threat_id}
                    onClick={() => navigate(`/threats/${t.threat_id}`)}
                    className="hover:bg-[#2c2834] transition-colors cursor-pointer"
                  >
                    <td className="px-6 py-4">
                      <SeverityBadge severity={t.severity} />
                    </td>
                    <td className="px-6 py-4">
                      <ThreatStatusBadge status={t.status} />
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
                      <div className="flex items-center gap-1.5">
                        <AgentTypeLogo agentType={agentTypeMap[t.agent_id]} />
                        <code className="text-xs font-mono text-[#ffb0cf] px-2 py-0.5 bg-[#ffb0cf]/5 rounded-sm">
                          {t.agent_id}
                        </code>
                      </div>
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
              ))}
            </tbody>
          </table>
        </div>
      </section>
    </>
  )
}
