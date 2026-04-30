import { useState, useEffect, useRef, useCallback } from 'react'
import { api } from '../api/client'
import { useNotifications } from './NotificationsProvider'
import ActivityDrawer from './ActivityDrawer'

const STATUS_STYLES = {
  pending:    { bg: 'bg-amber-900/40',   text: 'text-amber-300',  label: 'Pending' },
  approved:   { bg: 'bg-emerald-900/40', text: 'text-emerald-400', label: 'Approved' },
  denied:     { bg: 'bg-[#93000a]/40',   text: 'text-[#ffb4ab]',  label: 'Denied' },
  timeout:    { bg: 'bg-zinc-800/60',    text: 'text-zinc-400',   label: 'Timed Out' },
  abandoned:  { bg: 'bg-zinc-800/60',    text: 'text-zinc-500',   label: 'Abandoned' },
  auto_block: { bg: 'bg-orange-900/40',  text: 'text-orange-300', label: 'Auto-blocked' },
  allowed:    { bg: 'bg-zinc-800/40',    text: 'text-zinc-500',   label: 'Allowed' },
}

function StatusBadge({ status }) {
  const s = STATUS_STYLES[status] ?? STATUS_STYLES.allowed
  return (
    <span className={`inline-flex items-center px-2 py-0.5 rounded-full text-[10px] font-semibold uppercase tracking-wide ${s.bg} ${s.text}`}>
      {s.label}
    </span>
  )
}

function SeverityChip({ severity }) {
  if (!severity) return <span className="text-zinc-600 text-[10px]">—</span>
  const color = severity === 'critical' ? 'text-[#ffb4ab]' : severity === 'warning' ? 'text-amber-400' : 'text-zinc-400'
  return <span className={`text-[10px] font-semibold uppercase ${color}`}>{severity}</span>
}

function formatTs(ts) {
  if (!ts) return '—'
  try {
    const d = new Date(typeof ts === 'string' ? ts.replace(' ', 'T').replace(/\s+UTC$/, 'Z') : ts)
    if (isNaN(d)) return String(ts)
    return d.toLocaleString()
  } catch { return String(ts) }
}

// mode: "global" | "session"
export default function ActivityLog({ mode = 'global', sessionId, _threatId }) {
  const [events, setEvents] = useState([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState(null)
  const [includeAllowed, setIncludeAllowed] = useState(false)
  const [statusFilter, setStatusFilter] = useState('')
  const [autoRefresh, setAutoRefresh] = useState(mode === 'global')
  const [selectedEvent, setSelectedEvent] = useState(null)
  const [optimisticUpdates, setOptimisticUpdates] = useState({}) // holdId → status
  const [inlineErrors, setInlineErrors] = useState({})
  const timerRef = useRef(null)
  const notifications = useNotifications()

  const fetchEvents = useCallback(async () => {
    try {
      const params = {}
      if (sessionId) params.session_id = sessionId
      if (statusFilter) params.status = statusFilter
      if (includeAllowed) params.include_allowed = 'true'
      const data = await api.getActivity(params)
      setEvents(data.events ?? [])
      setError(null)
    } catch (err) {
      setError(err.message)
      setAutoRefresh(false)
    } finally {
      setLoading(false)
    }
  }, [sessionId, statusFilter, includeAllowed])

  // Initial fetch + polling
  useEffect(() => {
    setLoading(true)
    fetchEvents()
  }, [fetchEvents])

  useEffect(() => {
    if (!autoRefresh || selectedEvent) return
    timerRef.current = setInterval(fetchEvents, 5000)
    return () => clearInterval(timerRef.current)
  }, [autoRefresh, selectedEvent, fetchEvents])

  // SSE trigger: immediate refresh on hold_pending / hold_decided
  useEffect(() => {
    if (!notifications) return
    // notifications.pendingHolds changes when SSE fires — use it as a trigger
    fetchEvents()
  }, [notifications?.pendingHolds, fetchEvents])

  async function handleDecide(event, decision) {
    setOptimisticUpdates(prev => ({ ...prev, [event.id]: decision === 'approved' ? 'approved' : 'denied' }))
    setInlineErrors(prev => { const next = { ...prev }; delete next[event.id]; return next })
    try {
      await api.decideHold(event.id, decision)
      await fetchEvents()
    } catch (err) {
      setOptimisticUpdates(prev => { const next = { ...prev }; delete next[event.id]; return next })
      setInlineErrors(prev => ({ ...prev, [event.id]: err.message }))
    }
  }

  const displayEvents = events.map(ev => ({
    ...ev,
    status: optimisticUpdates[ev.id] ?? ev.status,
  }))

  const globalColumns = ['Time', 'Tool', 'Status', 'Rule', 'Severity', 'Agent', 'Session', 'Decided By']
  const sessionColumns = ['Time', 'Tool', 'Status', 'Rule', 'Severity', 'Decided By']
  const columns = mode === 'global' ? globalColumns : sessionColumns

  return (
    <div>
      {/* Toolbar */}
      <div className="flex items-center gap-3 mb-4 flex-wrap">
        {/* Status filter */}
        <select
          value={statusFilter}
          onChange={e => setStatusFilter(e.target.value)}
          className="bg-[#1d1a25] border border-[#574149]/20 text-[#e7dff0] text-xs rounded-sm px-2 py-1.5 focus:outline-none"
        >
          <option value="">All statuses</option>
          <option value="pending">Pending</option>
          <option value="approved">Approved</option>
          <option value="denied">Denied</option>
          <option value="auto_block">Auto-blocked</option>
          <option value="timeout">Timed Out</option>
          <option value="abandoned">Abandoned</option>
        </select>

        {/* include_allowed toggle */}
        <label className="flex items-center gap-2 cursor-pointer select-none">
          <div
            onClick={() => setIncludeAllowed(v => !v)}
            className={`w-8 h-4 rounded-full transition-colors ${includeAllowed ? 'bg-[#f256a3]' : 'bg-zinc-700'} relative`}
          >
            <span className={`absolute top-0.5 left-0.5 w-3 h-3 bg-white rounded-full transition-transform ${includeAllowed ? 'translate-x-4' : ''}`} />
          </div>
          <span className="text-[11px] text-zinc-400">Show allowed</span>
        </label>

        <div className="ml-auto flex items-center gap-2">
          {/* Refresh button */}
          <button
            onClick={fetchEvents}
            className="text-[11px] text-zinc-400 hover:text-[#e7dff0] transition-colors px-2 py-1"
          >
            <span className="material-symbols-outlined text-[14px] align-middle">refresh</span>
          </button>

          {/* Live pill */}
          <button
            onClick={() => setAutoRefresh(v => !v)}
            className={`flex items-center gap-1.5 px-2.5 py-1 rounded-full text-[10px] font-semibold transition-colors ${
              autoRefresh ? 'bg-emerald-900/40 text-emerald-400' : 'bg-zinc-800 text-zinc-500'
            }`}
          >
            <span className={`w-1.5 h-1.5 rounded-full ${autoRefresh ? 'bg-emerald-400 animate-pulse' : 'bg-zinc-600'}`} />
            {autoRefresh ? 'Live' : 'Paused'}
          </button>
        </div>
      </div>

      {/* High-volume warning */}
      {includeAllowed && (
        <div className="flex items-center gap-2 px-3 py-2 bg-amber-900/20 border border-amber-700/30 rounded-sm text-amber-300 text-[11px] mb-4">
          <span className="material-symbols-outlined text-[14px]">warning</span>
          Showing all tool call events including allowed — may be high volume
        </div>
      )}

      {/* Error banner */}
      {error && (
        <div className="flex items-center gap-2 px-3 py-2 bg-[#93000a]/20 border border-[#ffb4ab]/30 rounded-sm text-[#ffb4ab] text-[11px] mb-4">
          <span className="material-symbols-outlined text-[14px]">error</span>
          <span className="flex-1">{error}</span>
          <button onClick={() => { setError(null); setAutoRefresh(true); fetchEvents() }} className="font-semibold hover:underline">Retry</button>
        </div>
      )}

      {/* Table */}
      {loading ? (
        <div className="text-zinc-500 text-xs py-8 text-center">Loading…</div>
      ) : displayEvents.length === 0 ? (
        <div className="text-zinc-500 text-xs py-12 text-center">
          {sessionId
            ? 'No tool call activity for this session.'
            : includeAllowed
              ? 'No tool call events recorded yet.'
              : 'No tool call holds recorded yet. Holds appear here when a security rule triggers on an agent tool call.'}
        </div>
      ) : (
        <div className="overflow-x-auto">
          <table className="w-full text-xs">
            <thead>
              <tr className="text-zinc-500 text-[10px] uppercase tracking-widest border-b border-[#574149]/20">
                {columns.map(col => (
                  <th key={col} className="text-left py-2 px-3 font-semibold whitespace-nowrap">{col}</th>
                ))}
                <th className="py-2 px-3" />
              </tr>
            </thead>
            <tbody>
              {displayEvents.map(ev => (
                <tr
                  key={ev.id}
                  onClick={() => setSelectedEvent(ev)}
                  className={`border-b border-[#574149]/10 cursor-pointer transition-colors hover:bg-[#1d1a25] ${
                    ev.status === 'pending' ? 'border-l-2 border-l-amber-500/60' : ''
                  }`}
                >
                  <td className="py-2.5 px-3 text-zinc-500 whitespace-nowrap font-mono text-[10px]">{formatTs(ev.event_time)}</td>
                  <td className="py-2.5 px-3 font-mono text-[#93c5fd] whitespace-nowrap">{ev.tool_name || '—'}</td>
                  <td className="py-2.5 px-3"><StatusBadge status={ev.status} /></td>
                  <td className="py-2.5 px-3 text-zinc-400 max-w-[160px] truncate" title={ev.rule_name || ev.rule_id}>
                    {ev.rule_name || ev.rule_id || '—'}
                  </td>
                  <td className="py-2.5 px-3"><SeverityChip severity={ev.severity} /></td>
                  {mode === 'global' && (
                    <>
                      <td className="py-2.5 px-3 font-mono text-[10px] text-zinc-400 max-w-[100px] truncate">{ev.agent_id || '—'}</td>
                      <td className="py-2.5 px-3 font-mono text-[10px] text-zinc-600 max-w-[80px] truncate" title={ev.session_id}>
                        {ev.session_id ? ev.session_id.slice(0, 8) + '…' : '—'}
                      </td>
                    </>
                  )}
                  <td className="py-2.5 px-3 text-zinc-500 text-[10px] whitespace-nowrap">{ev.decided_by || '—'}</td>
                  <td className="py-2.5 px-3">
                    {ev.status === 'pending' && ev.is_hold_event && (
                      <div className="flex items-center gap-1.5" onClick={e => e.stopPropagation()}>
                        <button
                          onClick={() => handleDecide(ev, 'approved')}
                          className="px-2.5 py-1 bg-emerald-900/40 text-emerald-400 text-[10px] font-semibold rounded-sm hover:bg-emerald-900/60 transition-colors"
                        >
                          Approve
                        </button>
                        <button
                          onClick={() => handleDecide(ev, 'denied')}
                          className="px-2.5 py-1 bg-[#93000a]/40 text-[#ffb4ab] text-[10px] font-semibold rounded-sm hover:bg-[#93000a]/60 transition-colors"
                        >
                          Deny
                        </button>
                        {inlineErrors[ev.id] && (
                          <span className="text-[10px] text-[#ffb4ab]" title={inlineErrors[ev.id]}>!</span>
                        )}
                      </div>
                    )}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}

      {/* Detail drawer */}
      {selectedEvent && (
        <ActivityDrawer
          event={selectedEvent}
          onClose={() => setSelectedEvent(null)}
          onDecided={fetchEvents}
        />
      )}
    </div>
  )
}
