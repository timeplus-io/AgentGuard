import React, { useState, useEffect } from 'react'
import { Link, useNavigate } from 'react-router-dom'
import { useTranslation } from 'react-i18next'
import { SeverityBadge, ThreatStatusBadge } from './Badge'
import { api } from '../api/client'
import { formatDate } from '../i18n/format'

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
    return formatDate(d)
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
  const { t, i18n } = useTranslation('threats')
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
        if (!cancelled) setLoadError(err?.message || t('list.errors.loadFailed'))
      } finally {
        if (!cancelled) setLoading(false)
      }
    }
    fetchThreats()
    const timer = setInterval(fetchThreats, 30_000)
    return () => { cancelled = true; clearInterval(timer) }
  }, [i18n.language])

  const filtered = threats.filter(th => {
    if (ruleId && th.rule_id !== ruleId) return false
    if (severityFilter !== 'All' && th.severity?.toLowerCase() !== severityFilter.toLowerCase()) return false
    if (statusFilter !== 'All' && th.status !== statusFilter.toLowerCase()) return false
    return true
  })

  async function handleAcknowledge(threat) {
    setActionError(null)
    setThreats(prev => prev.map(th =>
      th.threat_id === threat.threat_id ? { ...th, status: 'acknowledged' } : th
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
      setThreats(prev => prev.map(th =>
        th.threat_id === threat.threat_id ? { ...th, status: threat.status } : th
      ))
      setActionError(t('errors.acknowledgeFailed', { message: err?.message ?? String(err) }))
    }
  }

  async function handleClear(threat) {
    setActionError(null)
    setThreats(prev => prev.map(th =>
      th.threat_id === threat.threat_id ? { ...th, status: 'cleared' } : th
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
      setThreats(prev => prev.map(th =>
        th.threat_id === threat.threat_id ? { ...th, status: threat.status } : th
      ))
      setActionError(t('errors.clearFailed', { message: err?.message ?? String(err) }))
    }
  }

  const colSpan = hideRuleCol ? 7 : 8
  const headers = [
    t('list.headers.severity'),
    t('list.headers.status'),
    ...(!hideRuleCol ? [t('list.headers.ruleMessage')] : []),
    t('list.headers.agent'),
    t('list.headers.session'),
    t('list.headers.events'),
    t('list.headers.lastSeen'),
    t('list.headers.actions'),
  ]

  return (
    <>
      {showFilters && (
        <div className="mb-6 flex flex-wrap items-center gap-6">
          <div className="flex flex-col gap-2">
            <label className="text-[10px] font-semibold text-gray-400 uppercase tracking-widest">{t('list.filters.severityLabel')}</label>
            <div className="flex gap-1">
              {['All', 'Critical', 'Warning', 'Info'].map(label => {
                const isActive = severityFilter === label
                const displayLabel = label === 'All' ? t('list.filters.all') : t(`severity.${label.toLowerCase()}`)
                return (
                  <button
                    key={label}
                    onClick={() => setSeverityFilter(prev => prev === label ? 'All' : label)}
                    className={`px-3 py-1.5 text-[11px] font-semibold rounded-full transition-colors ${
                      isActive
                        ? 'bg-gray-800 text-gray-100 border border-pink-500'
                        : 'bg-gray-800 text-gray-400 border border-gray-600 hover:text-gray-100 hover:border-pink-500'
                    }`}
                  >
                    {displayLabel}
                  </button>
                )
              })}
            </div>
          </div>

          <div className="flex flex-col gap-2">
            <label className="text-[10px] font-semibold text-gray-400 uppercase tracking-widest">{t('list.filters.statusLabel')}</label>
            <div className="flex gap-1">
              {['All', 'open', 'acknowledged', 'cleared'].map(s => (
                <button
                  key={s}
                  onClick={() => setStatusFilter(s)}
                  className={`px-3 py-1.5 text-[11px] font-semibold rounded-full transition-colors capitalize ${
                    statusFilter === s
                      ? 'bg-gray-800 text-gray-100 border border-pink-500'
                      : 'bg-gray-800 text-gray-400 border border-gray-600 hover:text-gray-100 hover:border-pink-500'
                  }`}
                >
                  {s === 'All' ? t('list.filters.all') : t(`threatStatus.${s}`)}
                </button>
              ))}
            </div>
          </div>
        </div>
      )}

      {actionError && (
        <div className="mb-3 flex items-center gap-2 px-4 py-2 bg-[#93000a]/20 border border-[#ffb4ab]/30 rounded-sm text-error text-xs">
          <span className="material-symbols-outlined text-base shrink-0">error</span>
          <span className="flex-1">{actionError}</span>
          <button onClick={() => setActionError(null)} className="text-error hover:text-gray-100">
            <span className="material-symbols-outlined text-sm">close</span>
          </button>
        </div>
      )}

      <section className="bg-surface-container rounded-sm overflow-hidden mb-8">
        <div className="px-6 py-4 flex items-center justify-between border-b border-outline-variant/10">
          <span className="text-xs font-semibold text-gray-400 uppercase tracking-widest">
            {t('list.heading', { count: filtered.length })}
          </span>
        </div>
        <div className="overflow-auto">
          <table className="w-full text-left border-collapse">
            <thead className="bg-surface-container-high sticky top-0 z-10">
              <tr>
                {headers.map(h => (
                  <th key={h} className="px-6 py-4 text-[10px] font-bold text-gray-400 uppercase tracking-widest whitespace-nowrap">
                    {h}
                  </th>
                ))}
              </tr>
            </thead>
            <tbody className="divide-y divide-outline-variant/10">
              {loading ? (
                <tr>
                  <td colSpan={colSpan} className="px-6 py-12 text-center text-gray-400 text-xs">
                    {t('list.loading')}
                  </td>
                </tr>
              ) : loadError ? (
                <tr>
                  <td colSpan={colSpan} className="px-6 py-12 text-center">
                    <div className="flex flex-col items-center gap-2">
                      <span className="material-symbols-outlined text-error text-3xl">error</span>
                      <p className="text-xs text-error">{loadError}</p>
                    </div>
                  </td>
                </tr>
              ) : filtered.length === 0 ? (
                <tr>
                  <td colSpan={colSpan} className="px-6 py-12 text-center text-gray-400 text-xs">
                    {threats.length > 0 ? t('list.emptyFiltered') : t('list.empty')}
                  </td>
                </tr>
              ) : filtered.map(th => (
                <tr
                    key={th.threat_id}
                    onClick={() => navigate(`/threats/${th.threat_id}`)}
                    className="hover:bg-surface-container-high transition-colors cursor-pointer"
                  >
                    <td className="px-6 py-4">
                      <SeverityBadge severity={th.severity} />
                    </td>
                    <td className="px-6 py-4">
                      <ThreatStatusBadge status={th.status} />
                    </td>
                    {!hideRuleCol && (
                      <td className="px-6 py-4 max-w-xs">
                        {th.rule_id ? (
                          <Link
                            to={`/rules/${th.rule_id}`}
                            onClick={e => e.stopPropagation()}
                            className="text-xs font-semibold text-gray-100 hover:text-primary transition-colors"
                          >
                            {th.rule_name}
                          </Link>
                        ) : (
                          <p className="text-xs font-semibold text-gray-100">{th.rule_name}</p>
                        )}
                        {th.events && th.events[0]?.message && (
                          <p className="text-[10px] text-gray-400 font-medium truncate">{th.events[0].message}</p>
                        )}
                      </td>
                    )}
                    <td className="px-6 py-4">
                      <div className="flex items-center gap-1.5">
                        <AgentTypeLogo agentType={agentTypeMap[th.agent_id]} />
                        <code className="text-xs font-mono text-primary px-2 py-0.5 bg-[#ffb0cf]/5 rounded-sm">
                          {th.agent_id}
                        </code>
                      </div>
                      {th.deployment_id && (
                        <p className="text-[10px] text-gray-400 font-medium mt-0.5 truncate max-w-[160px]">
                          {th.deployment_id}
                        </p>
                      )}
                    </td>
                    <td className="px-6 py-4">
                      {th.session_id ? (
                        <code className="text-[10px] font-mono text-gray-400 px-1.5 py-0.5 bg-surface-container-high rounded-sm truncate block max-w-[120px]" title={th.session_id}>
                          {th.session_id.length > 12 ? th.session_id.slice(0, 12) + '…' : th.session_id}
                        </code>
                      ) : (
                        <span className="text-gray-400 text-xs">—</span>
                      )}
                    </td>
                    <td className="px-6 py-4">
                      <span className="text-xs text-gray-100 font-semibold">{t('list.eventsCount', { count: th.event_count })}</span>
                      <p className="text-[10px] text-gray-400 whitespace-nowrap">{formatTs(th.first_seen)}</p>
                    </td>
                    <td className="px-6 py-4">
                      <span className="text-xs text-gray-400 whitespace-nowrap">{formatTs(th.last_seen)}</span>
                    </td>
                    <td className="px-6 py-4" onClick={e => e.stopPropagation()}>
                      <div className="flex items-center gap-1.5">
                        {th.status === 'open' && (
                          <>
                            <button
                              onClick={() => handleAcknowledge(th)}
                              className="px-2.5 py-1 text-[10px] font-semibold rounded-sm bg-yellow-500/10 text-yellow-400 hover:bg-yellow-500/20 transition-colors whitespace-nowrap"
                            >
                              {t('actions.acknowledge')}
                            </button>
                            <button
                              onClick={() => handleClear(th)}
                              className="px-2.5 py-1 text-[10px] font-semibold rounded-sm bg-green-500/10 text-green-400 hover:bg-green-500/20 transition-colors"
                            >
                              {t('actions.clear')}
                            </button>
                          </>
                        )}
                        {th.status === 'acknowledged' && (
                          <button
                            onClick={() => handleClear(th)}
                            className="px-2.5 py-1 text-[10px] font-semibold rounded-sm bg-green-500/10 text-green-400 hover:bg-green-500/20 transition-colors"
                          >
                            {t('actions.clear')}
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
