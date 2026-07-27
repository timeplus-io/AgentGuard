import { useState, useEffect } from 'react'
import { useParams, useNavigate, useLocation, Link } from 'react-router-dom'
import Sidebar from '../components/Sidebar'
import TopNav from '../components/TopNav'
import { SeverityBadge, ThreatStatusBadge } from '../components/Badge'
import { api } from '../api/client'
import ActivityLog from '../components/ActivityLog'

// ── helpers ───────────────────────────────────────────────────────────────────

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

// ── JsonTree ──────────────────────────────────────────────────────────────────
// Recursive collapsible JSON tree. Nested objects/arrays start collapsed.

function JsonValue({ value, depth }) {
  const [open, setOpen] = useState(depth === 0)

  if (value === null) return <span className="text-gray-400">null</span>
  if (typeof value === 'boolean') return <span className="text-purple-400">{String(value)}</span>
  if (typeof value === 'number') return <span className="text-amber-400">{value}</span>
  if (typeof value === 'string') {
    return (
      <span className="text-green-400 break-all">
        "{value.length > 200 ? value.slice(0, 200) + '…' : value}"
      </span>
    )
  }

  const isArr = Array.isArray(value)
  const entries = isArr
    ? value.map((v, i) => [String(i), v])
    : Object.entries(value)
  const [ob, cb] = isArr ? ['[', ']'] : ['{', '}']

  if (entries.length === 0) return <span className="text-gray-400">{ob}{cb}</span>

  return (
    <span>
      <button
        onClick={() => setOpen(o => !o)}
        className="text-gray-400 hover:text-gray-100 transition-colors mr-1"
      >
        {open ? '▾' : '▸'}
      </button>
      {open ? (
        <>
          <span className="text-gray-400">{ob}</span>
          <div className="pl-4 border-l border-outline-variant/20 ml-0.5 mt-0.5 space-y-0.5">
            {entries.map(([k, v]) => (
              <div key={k} className="flex gap-1.5 items-start">
                <span className="text-info shrink-0 select-none">
                  {isArr ? k : `"${k}"`}:
                </span>
                <JsonValue value={v} depth={depth + 1} />
              </div>
            ))}
          </div>
          <span className="text-gray-400">{cb}</span>
        </>
      ) : (
        <button
          onClick={() => setOpen(true)}
          className="text-gray-400 hover:text-gray-400 transition-colors"
        >
          {ob}&hellip;{cb}
        </button>
      )}
    </span>
  )
}

// ── EventCard ─────────────────────────────────────────────────────────────────

function EventCard({ event }) {
  const [expanded, setExpanded] = useState(false)

  let parsedData = null
  if (event.event_data) {
    try { parsedData = JSON.parse(event.event_data) } catch { /* use raw */ }
  }

  const hasDetail = event.run_id || event.event_data

  return (
    <div className="bg-surface-container border border-outline-variant/10 rounded">
      {/* Summary row — always visible, click to expand */}
      <div
        className="flex items-center gap-3 px-4 py-2.5 cursor-pointer select-none"
        onClick={() => hasDetail && setExpanded(e => !e)}
      >
        <span className="material-symbols-outlined text-[14px] text-gray-400 shrink-0">
          {expanded ? 'expand_more' : 'chevron_right'}
        </span>
        <span className="text-[10px] font-mono text-gray-400 whitespace-nowrap shrink-0">
          {formatTs(event.detected_at)}
        </span>
        {event.hook_name && (
          <span className="bg-surface-container-high text-gray-400 uppercase rounded-full text-[10px] font-semibold px-2 py-0.5 tracking-wide shrink-0">
            {event.hook_name}
          </span>
        )}
        {event.tool_name && (
          <span className="text-[10px] font-mono text-info shrink-0">{event.tool_name}</span>
        )}
        {event.message && (
          <span className="text-xs text-gray-100 truncate" title={event.message}>{event.message}</span>
        )}
      </div>

      {/* Expanded detail — run_id + JSON tree */}
      {expanded && hasDetail && (
        <div className="px-4 pb-3 pt-1 border-t border-outline-variant/10">
          {event.run_id && (
            <p className="text-[10px] font-mono text-gray-400 mb-2">run: {event.run_id}</p>
          )}
          {parsedData !== null ? (
            <div className="text-[11px] font-mono text-gray-100 leading-relaxed">
              <JsonValue value={parsedData} depth={0} />
            </div>
          ) : event.event_data ? (
            <pre className="text-[11px] font-mono text-gray-400 whitespace-pre-wrap break-all">
              {event.event_data}
            </pre>
          ) : null}
        </div>
      )}
    </div>
  )
}

// ── Main page ─────────────────────────────────────────────────────────────────

export default function ThreatDetail() {
  const { id } = useParams()
  const navigate = useNavigate()
  const location = useLocation()
  const [threat, setThreat] = useState(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState(null)
  const [actionLoading, setActionLoading] = useState(false)
  const [actionError, setActionError] = useState('')
  // Hold decision state: null | 'pending' | 'approved' | 'denied' | 'error'
  const [decideStatus, setDecideStatus] = useState(null)
  const [decideError, setDecideError] = useState('')
  const [decideReason, setDecideReason] = useState('')
  const [activeTab, setActiveTab] = useState('overview')

  // Support /threats/by-hold/:holdId — resolve hold to underlying threat and redirect.
  const isByHold = location.pathname.startsWith('/threats/by-hold/')

  useEffect(() => {
    let cancelled = false
    setLoading(true)
    setError(null)
    setDecideStatus(null)
    setDecideReason('')

    async function load() {
      try {
        if (isByHold) {
          const holdResp = await api.getHold(id)
          const h = holdResp.hold
          // Try to find a matching threat row so we can show full context.
          // The match can fail for legitimate reasons: the alert webhook
          // hasn't yet upserted to agentguard_threats; the hold's rule was
          // disabled before a threat row was written; etc. In all those cases
          // we still need to let the user approve/deny — so fall back to a
          // synthetic threat object built from the hold and render the approval
          // UI directly. This page is the operator's only way out of a stuck hold.
          let matched = null
          try {
            const list = await api.getThreats({ agent_id: h.agent_id, limit: 200 })
            matched = (list.threats ?? []).find(
              t => t.agent_id === h.agent_id
                && t.deployment_id === h.deployment_id
                && t.session_id === h.session_id
                && t.rule_id === h.rule_id
            )
          } catch (e) {
            // listThreats failure is non-fatal here — render hold-only fallback below.
            console.warn('threat lookup failed for by-hold path; rendering hold-only UI:', e)
          }
          if (matched && !cancelled) {
            navigate(`/threats/${encodeURIComponent(matched.threat_id)}`, { replace: true })
            return
          }
          // Hold-only fallback: synthesize a threat-shaped object so the existing
          // render path works. threat_id is empty — the render branches below
          // suppress acknowledge/clear (which need a real threat_id) but keep
          // approve/deny enabled (which only need the hold_id).
          if (!cancelled) {
            setThreat({
              threat_id: '',
              agent_id: h.agent_id,
              deployment_id: h.deployment_id,
              session_id: h.session_id,
              rule_id: h.rule_id,
              rule_name: h.rule_id || 'Pending Hold',
              severity: h.severity,
              status: 'open',
              status_updated_at: h.created_at,
              first_seen: h.created_at,
              last_seen: h.created_at,
              event_count: 1,
              events: [],
              pending_hold: {
                hold_id: h.hold_id,
                tool_name: h.tool_name,
                args_summary: h.args_summary,
                created_at: h.created_at,
              },
            })
          }
          return
        }
        const data = await api.getThreatDetail(id)
        if (!cancelled) setThreat(data)
      } catch (e) {
        if (!cancelled) setError(e.message ?? String(e))
      } finally {
        if (!cancelled) setLoading(false)
      }
    }
    load()
    return () => { cancelled = true }
  }, [id, isByHold, navigate])

  async function decide(decision) {
    if (!threat?.pending_hold) return
    setDecideStatus('pending')
    setDecideError('')
    try {
      await api.decideHold(threat.pending_hold.hold_id, decision, decideReason)
      setDecideStatus(decision)
      const fresh = await api.getThreatDetail(id)
      setThreat(fresh)
    } catch (err) {
      console.error('threat: decide hold failed:', err)
      setDecideStatus('error')
      setDecideError(err?.message ?? String(err))
    }
  }

  async function handleAcknowledge() {
    if (actionLoading) return
    const prev = threat.status
    setActionLoading(true)
    setActionError('')
    setThreat(t => ({ ...t, status: 'acknowledged' }))
    try {
      await api.acknowledgeThreat(threat.threat_id, {
        agent_id: threat.agent_id,
        deployment_id: threat.deployment_id,
        session_id: threat.session_id,
        rule_id: threat.rule_id,
      })
    } catch (err) {
      console.error('threat: acknowledge failed:', err)
      setThreat(t => ({ ...t, status: prev }))
      setActionError(`Acknowledge failed: ${err?.message ?? String(err)}`)
    } finally {
      setActionLoading(false)
    }
  }

  async function handleClear() {
    if (actionLoading) return
    const prev = threat.status
    setActionLoading(true)
    setActionError('')
    setThreat(t => ({ ...t, status: 'cleared' }))
    try {
      await api.clearThreat(threat.threat_id, {
        agent_id: threat.agent_id,
        deployment_id: threat.deployment_id,
        session_id: threat.session_id,
        rule_id: threat.rule_id,
      })
    } catch (err) {
      console.error('threat: clear failed:', err)
      setThreat(t => ({ ...t, status: prev }))
      setActionError(`Clear failed: ${err?.message ?? String(err)}`)
    } finally {
      setActionLoading(false)
    }
  }

  const events = threat?.events ?? []
  const sortedEvents = [...events].sort(
    (a, b) => {
      const ta = a.detected_at ? new Date(String(a.detected_at).replace(' ', 'T')) : 0
      const tb = b.detected_at ? new Date(String(b.detected_at).replace(' ', 'T')) : 0
      return tb - ta
    }
  )
  const agentCompoundId = threat
    ? `${threat.deployment_id}__${threat.agent_id}`
    : null

  return (
    <div className="min-h-screen bg-surface text-gray-100">
      <Sidebar />
      <TopNav />

      <main className="pl-sidebar pt-16 pb-8 min-h-screen">
        {loading ? (
          /* Loading skeleton */
          <div className="p-8 space-y-4">
            <div className="animate-pulse h-20 bg-surface-container rounded" />
            <div className="animate-pulse h-48 bg-surface-container rounded" />
            <div className="animate-pulse h-32 bg-surface-container rounded" />
          </div>
        ) : error ? (
          /* Error state */
          <div className="flex flex-col items-center justify-center min-h-[60vh] gap-4">
            <span className="material-symbols-outlined text-5xl text-gray-400">security</span>
            <p className="text-gray-400 text-sm">
              {error === 'HTTP 404' ? 'Threat not found.' : error}
            </p>
            <Link to="/threats" className="text-primary text-sm hover:underline">
              ← Back to Threats
            </Link>
          </div>
        ) : threat ? (
          <div className="p-8">
            <section className="mb-8 flex flex-col gap-4 lg:flex-row lg:items-center lg:justify-between">
              <div className="flex items-center gap-3 min-w-0">
                <Link to="/threats" className="text-gray-400 hover:text-primary transition-colors shrink-0">
                  <span className="material-symbols-outlined">arrow_back</span>
                </Link>

                <div className="flex flex-wrap items-center gap-3 min-w-0">
                  <Link
                    to={`/rules/${threat.rule_id}`}
                    className="font-medium tracking-tight text-lg text-gray-100 hover:text-primary transition-colors truncate max-w-[520px]"
                  >
                    {threat.rule_name || threat.rule_id}
                  </Link>
                  <SeverityBadge severity={threat.severity} />
                  <ThreatStatusBadge status={threat.status} />
                  {threat.agent_id && (
                    <Link
                      to={`/agents/${agentCompoundId}`}
                      className="bg-surface-container-high text-primary px-2.5 py-1 rounded-full text-[10px] font-semibold font-mono hover:bg-surface-container-highest transition-colors whitespace-nowrap"
                    >
                      {threat.agent_id}
                    </Link>
                  )}
                </div>
              </div>

              {/* Acknowledge/Clear require a real threat_id. The by-hold fallback
                  renders without one, so suppress those controls there. */}
              {threat.threat_id && (
                <div className="flex items-center gap-2 shrink-0">
                  {threat.status === 'open' && (
                    <>
                      <button
                        onClick={handleAcknowledge}
                        disabled={actionLoading}
                        className="px-3 py-1.5 text-xs font-semibold rounded-sm bg-yellow-500/10 text-yellow-400 hover:bg-yellow-500/20 transition-colors whitespace-nowrap"
                      >
                        Acknowledge
                      </button>
                      <button
                        onClick={handleClear}
                        disabled={actionLoading}
                        className="px-3 py-1.5 text-xs font-semibold rounded-sm bg-green-500/10 text-green-400 hover:bg-green-500/20 transition-colors"
                      >
                        Clear
                      </button>
                    </>
                  )}
                  {threat.status === 'acknowledged' && (
                    <button
                      onClick={handleClear}
                      disabled={actionLoading}
                      className="px-3 py-1.5 text-xs font-semibold rounded-sm bg-green-500/10 text-green-400 hover:bg-green-500/20 transition-colors"
                    >
                      Clear
                    </button>
                  )}
                  {threat.status === 'cleared' && (
                    <span className="px-3 py-1.5 text-xs font-semibold rounded-full bg-green-500/10 text-green-400">
                      Cleared
                    </span>
                  )}
                </div>
              )}
            </section>

            {/* Tab bar */}
            <div className="flex items-center gap-1 mb-8 border-b border-outline-variant/20 pb-0">
              {['overview', 'activity'].map(tab => (
                <button
                  key={tab}
                  onClick={() => setActiveTab(tab)}
                  className={`px-4 py-2 text-xs font-semibold capitalize transition-colors border-b-2 -mb-px ${
                    activeTab === tab
                      ? 'text-primary border-[#ffb0cf]'
                      : 'text-gray-400 border-transparent hover:text-gray-100'
                  }`}
                >
                  {tab}
                </button>
              ))}
            </div>

            {activeTab === 'activity' && (
              <ActivityLog mode="session" sessionId={threat.session_id} threatId={threat.threat_id} />
            )}

            {activeTab === 'overview' && (
              <div className="space-y-8">

            {actionError && (
              <div className="flex items-start gap-2 px-4 py-3 bg-[#93000a]/20 border border-[#ffb4ab]/30 rounded-sm text-error text-xs">
                <span className="material-symbols-outlined text-base shrink-0">error</span>
                <span className="flex-1">{actionError}</span>
                <button
                  onClick={() => setActionError('')}
                  className="text-error/70 hover:text-error shrink-0"
                  aria-label="Dismiss"
                >
                  <span className="material-symbols-outlined text-[14px]">close</span>
                </button>
              </div>
            )}

            {/* Pending Hold banner — Approve / Deny */}
            {threat.pending_hold && decideStatus !== 'approved' && decideStatus !== 'denied' && (
              <section className="bg-[#93000a]/20 border border-[#ffb4ab]/30 rounded-sm p-5">
                <div className="flex items-center gap-2 mb-3">
                  <span className="material-symbols-outlined text-error">block</span>
                  <h2 className="text-sm font-semibold text-error">Pending Hold — Agent paused</h2>
                </div>
                <p className="text-xs text-gray-100 mb-3">
                  Tool: <code className="font-mono text-gray-100">{threat.pending_hold.tool_name}</code>
                </p>
                <pre className="bg-surface-container-lowest rounded-sm p-3 mb-4 text-[11px] font-mono text-gray-100 whitespace-pre-wrap break-all max-h-48 overflow-y-auto">
                  {threat.pending_hold.args_summary || '(no arguments)'}
                </pre>
                <textarea
                  placeholder="Optional reason for your decision..."
                  value={decideReason}
                  onChange={e => setDecideReason(e.target.value)}
                  className="w-full bg-gray-800 border border-gray-600 rounded-sm p-2 text-xs text-gray-100 focus:border-gray-400 focus:outline-none mb-3"
                  rows={2}
                />
                <div className="flex gap-2">
                  <button
                    onClick={() => decide('approved')}
                    disabled={decideStatus === 'pending'}
                    className="px-4 py-2 bg-emerald-500/20 text-emerald-400 text-xs font-semibold rounded-sm hover:bg-emerald-500/30 disabled:opacity-50"
                  >
                    ✓ Approve
                  </button>
                  <button
                    onClick={() => decide('denied')}
                    disabled={decideStatus === 'pending'}
                    className="px-4 py-2 bg-[#ffb4ab]/20 text-error text-xs font-semibold rounded-sm hover:bg-[#ffb4ab]/30 disabled:opacity-50"
                  >
                    ✗ Deny
                  </button>
                </div>
                {decideStatus === 'error' && (
                  <p className="text-[11px] text-error mt-2">
                    Failed to submit decision{decideError ? `: ${decideError}` : '. Try again.'}
                  </p>
                )}
              </section>
            )}

            {(decideStatus === 'approved' || decideStatus === 'denied') && (
              <section className="bg-surface-container-low rounded-sm p-4">
                <p className="text-xs text-emerald-400">
                  ✓ {decideStatus === 'approved' ? 'Approved' : 'Denied'} — agent has been notified.
                </p>
              </section>
            )}

            {/* Stat strip */}
            <div className="grid grid-cols-4 gap-4">
              {/* Event Count */}
              <div className="bg-surface-container border border-outline-variant/10 rounded p-4">
                <p className="text-[10px] font-semibold text-gray-400 uppercase tracking-widest mb-1">Event Count</p>
                <p className="text-2xl font-medium text-gray-100">{threat.event_count ?? events.length}</p>
              </div>

              {/* First Seen */}
              <div className="bg-surface-container border border-outline-variant/10 rounded p-4">
                <p className="text-[10px] font-semibold text-gray-400 uppercase tracking-widest mb-1">First Seen</p>
                <p className="text-sm text-gray-100">{formatTs(threat.first_seen)}</p>
              </div>

              {/* Last Seen */}
              <div className="bg-surface-container border border-outline-variant/10 rounded p-4">
                <p className="text-[10px] font-semibold text-gray-400 uppercase tracking-widest mb-1">Last Seen</p>
                <p className="text-sm text-gray-100">{formatTs(threat.last_seen)}</p>
              </div>

              {/* Session ID */}
              <div className="bg-surface-container border border-outline-variant/10 rounded p-4">
                <p className="text-[10px] font-semibold text-gray-400 uppercase tracking-widest mb-1">Session ID</p>
                {threat.session_id ? (
                  <code
                    className="text-sm font-mono text-gray-100"
                    title={threat.session_id}
                  >
                    {threat.session_id.length > 12
                      ? threat.session_id.slice(0, 12) + '…'
                      : threat.session_id}
                  </code>
                ) : (
                  <span className="text-gray-400 text-sm">—</span>
                )}
              </div>
            </div>

            {/* Event History */}
            <section>
              <h2 className="text-sm font-semibold text-gray-400 uppercase tracking-widest mb-4">
                Event History ({events.length})
              </h2>
              {events.length === 0 ? (
                <p className="text-sm text-gray-400">No event history available.</p>
              ) : (
                <div className="space-y-2">
                  {sortedEvents.map((ev) => (
                    <EventCard key={`${ev.run_id ?? ''}-${ev.detected_at ?? ''}`} event={ev} />
                  ))}
                </div>
              )}
            </section>

              </div>
            )}
          </div>
        ) : null}
      </main>
    </div>
  )
}
