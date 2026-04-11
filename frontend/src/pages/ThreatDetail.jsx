import { useState, useEffect } from 'react'
import { useParams, Link } from 'react-router-dom'
import Sidebar from '../components/Sidebar'
import { SeverityBadge, ThreatStatusBadge } from '../components/Badge'
import { api } from '../api/client'

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

function prettyJson(raw) {
  try {
    return JSON.stringify(JSON.parse(raw), null, 2)
  } catch {
    return raw
  }
}

// ── EventCard ─────────────────────────────────────────────────────────────────

function EventCard({ event }) {
  return (
    <div className="bg-[#211e29] border border-[#574149]/10 rounded p-5">
      {/* Header row */}
      <div className="flex flex-wrap items-center gap-3 mb-3">
        <span className="text-[10px] font-mono text-zinc-400 whitespace-nowrap">
          {formatTs(event.detected_at)}
        </span>
        {event.hook_name && (
          <span className="bg-[#2c2834] text-zinc-300 uppercase rounded-full text-[10px] font-semibold px-2.5 py-0.5 tracking-wide">
            {event.hook_name}
          </span>
        )}
        {event.tool_name && (
          <span className="text-xs font-mono text-blue-400">
            {event.tool_name}
          </span>
        )}
      </div>

      {/* Message */}
      {event.message && (
        <p className="text-sm text-[#e7dff0] mb-3 leading-relaxed">{event.message}</p>
      )}

      {/* Run ID */}
      {event.run_id && (
        <p className="text-[10px] font-mono text-zinc-500 mb-3">
          run: {event.run_id}
        </p>
      )}

      {/* event_data */}
      {event.event_data && (
        <pre className="bg-[#0f0c17] text-[11px] font-mono text-zinc-300 rounded p-3 overflow-x-auto whitespace-pre-wrap break-all leading-relaxed">
          {prettyJson(event.event_data)}
        </pre>
      )}
    </div>
  )
}

// ── Main page ─────────────────────────────────────────────────────────────────

export default function ThreatDetail() {
  const { id } = useParams()
  const [threat, setThreat] = useState(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState(null)

  useEffect(() => {
    setLoading(true)
    setError(null)
    api.getThreat(id)
      .then(data => setThreat(data))
      .catch(err => setError(err.message))
      .finally(() => setLoading(false))
  }, [id])

  async function handleAcknowledge() {
    const prev = threat.status
    setThreat(t => ({ ...t, status: 'acknowledged' }))
    try {
      await api.acknowledgeThreat(threat.threat_id, {
        agent_id: threat.agent_id,
        deployment_id: threat.deployment_id,
        session_id: threat.session_id,
        rule_id: threat.rule_id,
      })
    } catch {
      setThreat(t => ({ ...t, status: prev }))
    }
  }

  async function handleClear() {
    const prev = threat.status
    setThreat(t => ({ ...t, status: 'cleared' }))
    try {
      await api.clearThreat(threat.threat_id, {
        agent_id: threat.agent_id,
        deployment_id: threat.deployment_id,
        session_id: threat.session_id,
        rule_id: threat.rule_id,
      })
    } catch {
      setThreat(t => ({ ...t, status: prev }))
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
    <div className="min-h-screen bg-[#15121d] text-[#e7dff0]">
      <Sidebar />

      {/* Fixed top header */}
      <header className="top-nav fixed top-0 right-0 h-16 bg-[#15121d]/95 z-50 flex justify-between items-center px-8 backdrop-blur-md shadow-[0_0_12px_rgba(213,63,140,0.12)]">
        {/* Left side */}
        <div className="flex items-center gap-3 min-w-0">
          <Link to="/threats" className="text-zinc-400 hover:text-[#ffb0cf] transition-colors shrink-0">
            <span className="material-symbols-outlined">arrow_back</span>
          </Link>

          {loading ? (
            <div className="animate-pulse h-4 w-40 bg-[#2c2834] rounded" />
          ) : threat ? (
            <>
              <Link
                to={`/rules/${threat.rule_id}`}
                className="font-medium tracking-tight text-lg text-[#e7dff0] hover:text-[#ffb0cf] transition-colors truncate max-w-[240px]"
              >
                {threat.rule_name || threat.rule_id}
              </Link>
              <SeverityBadge severity={threat.severity} />
              <ThreatStatusBadge status={threat.status} />
              {threat.agent_id && (
                <Link
                  to={`/agents/${agentCompoundId}`}
                  className="bg-[#2c2834] text-[#ffb0cf] px-2.5 py-1 rounded-full text-[10px] font-semibold font-mono hover:bg-[#38323f] transition-colors whitespace-nowrap"
                >
                  {threat.agent_id}
                </Link>
              )}
            </>
          ) : null}
        </div>

        {/* Right side — action buttons */}
        {!loading && threat && (
          <div className="flex items-center gap-2 shrink-0">
            {threat.status === 'open' && (
              <>
                <button
                  onClick={handleAcknowledge}
                  className="px-3 py-1.5 text-xs font-semibold rounded-sm bg-yellow-500/10 text-yellow-400 hover:bg-yellow-500/20 transition-colors whitespace-nowrap"
                >
                  Acknowledge
                </button>
                <button
                  onClick={handleClear}
                  className="px-3 py-1.5 text-xs font-semibold rounded-sm bg-green-500/10 text-green-400 hover:bg-green-500/20 transition-colors"
                >
                  Clear
                </button>
              </>
            )}
            {threat.status === 'acknowledged' && (
              <button
                onClick={handleClear}
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
      </header>

      <main className="pl-sidebar pt-16 pb-8 min-h-screen">
        {loading ? (
          /* Loading skeleton */
          <div className="p-8 space-y-4">
            <div className="animate-pulse h-20 bg-[#211e29] rounded" />
            <div className="animate-pulse h-48 bg-[#211e29] rounded" />
            <div className="animate-pulse h-32 bg-[#211e29] rounded" />
          </div>
        ) : error ? (
          /* Error state */
          <div className="flex flex-col items-center justify-center min-h-[60vh] gap-4">
            <span className="material-symbols-outlined text-5xl text-zinc-600">security</span>
            <p className="text-zinc-400 text-sm">
              {error === 'HTTP 404' ? 'Threat not found.' : error}
            </p>
            <Link to="/threats" className="text-[#ffb0cf] text-sm hover:underline">
              ← Back to Threats
            </Link>
          </div>
        ) : threat ? (
          <div className="p-8 space-y-8">

            {/* Stat strip */}
            <div className="grid grid-cols-4 gap-4">
              {/* Event Count */}
              <div className="bg-[#211e29] border border-[#574149]/10 rounded p-4">
                <p className="text-[10px] font-semibold text-zinc-500 uppercase tracking-widest mb-1">Event Count</p>
                <p className="text-2xl font-medium text-[#e7dff0]">{threat.event_count ?? events.length}</p>
              </div>

              {/* First Seen */}
              <div className="bg-[#211e29] border border-[#574149]/10 rounded p-4">
                <p className="text-[10px] font-semibold text-zinc-500 uppercase tracking-widest mb-1">First Seen</p>
                <p className="text-sm text-[#e7dff0]">{formatTs(threat.first_seen)}</p>
              </div>

              {/* Last Seen */}
              <div className="bg-[#211e29] border border-[#574149]/10 rounded p-4">
                <p className="text-[10px] font-semibold text-zinc-500 uppercase tracking-widest mb-1">Last Seen</p>
                <p className="text-sm text-[#e7dff0]">{formatTs(threat.last_seen)}</p>
              </div>

              {/* Session ID */}
              <div className="bg-[#211e29] border border-[#574149]/10 rounded p-4">
                <p className="text-[10px] font-semibold text-zinc-500 uppercase tracking-widest mb-1">Session ID</p>
                {threat.session_id ? (
                  <code
                    className="text-sm font-mono text-[#e7dff0]"
                    title={threat.session_id}
                  >
                    {threat.session_id.length > 12
                      ? threat.session_id.slice(0, 12) + '…'
                      : threat.session_id}
                  </code>
                ) : (
                  <span className="text-zinc-600 text-sm">—</span>
                )}
              </div>
            </div>

            {/* Event History */}
            <section>
              <h2 className="text-sm font-semibold text-zinc-400 uppercase tracking-widest mb-4">
                Event History ({events.length})
              </h2>
              {events.length === 0 ? (
                <p className="text-sm text-zinc-600">No event history available.</p>
              ) : (
                <div className="space-y-4">
                  {sortedEvents.map((ev, i) => (
                    <EventCard key={i} event={ev} />
                  ))}
                </div>
              )}
            </section>

          </div>
        ) : null}
      </main>
    </div>
  )
}
