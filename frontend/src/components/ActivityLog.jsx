import { useState, useEffect, useRef, useCallback } from 'react'
import { useTranslation } from 'react-i18next'
import { api } from '../api/client'
import { formatDate } from '../i18n/format'
import { useNotifications } from './NotificationsProvider'
import ActivityDrawer from './ActivityDrawer'

const STATUS_STYLES = {
  pending:    { bg: 'bg-amber-900/40',   text: 'text-amber-300' },
  approved:   { bg: 'bg-emerald-900/40', text: 'text-emerald-400' },
  denied:     { bg: 'bg-[#93000a]/40',   text: 'text-error' },
  timeout:    { bg: 'bg-zinc-800/60',    text: 'text-gray-400' },
  abandoned:  { bg: 'bg-zinc-800/60',    text: 'text-gray-400' },
  auto_block: { bg: 'bg-orange-900/40',  text: 'text-orange-300' },
  allowed:    { bg: 'bg-zinc-800/40',    text: 'text-gray-400' },
}

const GLOBAL_COLUMNS = ['time', 'tool', 'status', 'rule', 'severity', 'agent', 'session', 'decidedBy']
const SESSION_COLUMNS = ['time', 'tool', 'status', 'rule', 'severity', 'decidedBy']

function StatusBadge({ status }) {
  const { t } = useTranslation()
  const key = STATUS_STYLES[status] ? status : 'allowed'
  const s = STATUS_STYLES[key]
  return (
    <span className={`inline-flex items-center px-2 py-0.5 rounded-full text-[10px] font-semibold uppercase tracking-wide ${s.bg} ${s.text}`}>
      {t(`activity.status.${key}`)}
    </span>
  )
}

function SeverityChip({ severity }) {
  const { t } = useTranslation()
  if (!severity) return <span className="text-gray-400 text-[10px]">—</span>
  const color = severity === 'critical' ? 'text-error' : severity === 'warning' ? 'text-amber-400' : 'text-gray-400'
  return (
    <span className={`text-[10px] font-semibold uppercase ${color}`}>
      {t(`severity.${String(severity).toLowerCase()}`, { defaultValue: severity })}
    </span>
  )
}

function formatTs(ts) {
  if (!ts) return '—'
  try {
    let s = typeof ts === 'string' ? ts.replace(' ', 'T').replace(/\s*UTC$/, 'Z') : ts
    if (typeof s === 'string' && !s.endsWith('Z') && !/[+-]\d{2}:\d{2}$/.test(s)) s = s + 'Z'
    const d = new Date(s)
    if (isNaN(d)) return String(ts)
    return formatDate(d)
  } catch { return String(ts) }
}

// mode: "global" | "session"
export default function ActivityLog({ mode = 'global', sessionId, _threatId }) {
  const { t } = useTranslation()
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

  const columns = mode === 'global' ? GLOBAL_COLUMNS : SESSION_COLUMNS

  return (
    <div>
      {/* Toolbar */}
      <div className="flex items-center gap-3 mb-4 flex-wrap">
        {/* Status filter */}
        <select
          value={statusFilter}
          onChange={e => setStatusFilter(e.target.value)}
          className="bg-surface-container-low border border-outline-variant/20 text-gray-100 text-xs rounded-sm px-2 py-1.5 focus:outline-none"
        >
          <option value="">{t('activity.filters.allStatuses')}</option>
          <option value="pending">{t('activity.status.pending')}</option>
          <option value="approved">{t('activity.status.approved')}</option>
          <option value="denied">{t('activity.status.denied')}</option>
          <option value="auto_block">{t('activity.status.auto_block')}</option>
          <option value="timeout">{t('activity.status.timeout')}</option>
          <option value="abandoned">{t('activity.status.abandoned')}</option>
        </select>

        {/* include_allowed toggle */}
        <label className="flex items-center gap-2 cursor-pointer select-none">
          <div
            onClick={() => setIncludeAllowed(v => !v)}
            className={`w-8 h-4 rounded-full transition-colors ${includeAllowed ? 'bg-[#f256a3]' : 'bg-zinc-700'} relative`}
          >
            <span className={`absolute top-0.5 left-0.5 w-3 h-3 bg-white rounded-full transition-transform ${includeAllowed ? 'translate-x-4' : ''}`} />
          </div>
          <span className="text-[11px] text-gray-400">{t('activity.filters.showAllowed')}</span>
        </label>

        <div className="ml-auto flex items-center gap-2">
          {/* Refresh button */}
          <button
            onClick={fetchEvents}
            className="text-[11px] text-gray-400 hover:text-gray-100 transition-colors px-2 py-1"
          >
            <span className="material-symbols-outlined text-[14px] align-middle">refresh</span>
          </button>

          {/* Live pill */}
          <button
            onClick={() => setAutoRefresh(v => !v)}
            className={`flex items-center gap-1.5 px-2.5 py-1 rounded-full text-[10px] font-semibold transition-colors ${
              autoRefresh ? 'bg-emerald-900/40 text-emerald-400' : 'bg-zinc-800 text-gray-400'
            }`}
          >
            <span className={`w-1.5 h-1.5 rounded-full ${autoRefresh ? 'bg-emerald-400 animate-pulse' : 'bg-zinc-600'}`} />
            {autoRefresh ? t('activity.live') : t('activity.paused')}
          </button>
        </div>
      </div>

      {/* High-volume warning */}
      {includeAllowed && (
        <div className="flex items-center gap-2 px-3 py-2 bg-amber-900/20 border border-amber-700/30 rounded-sm text-amber-300 text-[11px] mb-4">
          <span className="material-symbols-outlined text-[14px]">warning</span>
          {t('activity.highVolumeWarning')}
        </div>
      )}

      {/* Error banner */}
      {error && (
        <div className="flex items-center gap-2 px-3 py-2 bg-[#93000a]/20 border border-[#ffb4ab]/30 rounded-sm text-error text-[11px] mb-4">
          <span className="material-symbols-outlined text-[14px]">error</span>
          <span className="flex-1">{error}</span>
          <button onClick={() => { setError(null); setAutoRefresh(true); fetchEvents() }} className="font-semibold hover:underline">{t('actions.retry')}</button>
        </div>
      )}

      {/* Table */}
      {loading ? (
        <div className="text-gray-400 text-xs py-8 text-center">{t('app.loading')}</div>
      ) : displayEvents.length === 0 ? (
        <div className="text-gray-400 text-xs py-12 text-center">
          {sessionId
            ? t('activity.empty.session')
            : includeAllowed
              ? t('activity.empty.allowed')
              : t('activity.empty.holds')}
        </div>
      ) : (
        <div className="overflow-x-auto">
          <table className="w-full text-xs">
            <thead>
              <tr className="text-gray-400 text-[10px] uppercase tracking-widest border-b border-outline-variant/20">
                {columns.map(col => (
                  <th key={col} className="text-left py-2 px-3 font-semibold whitespace-nowrap">{t(`activity.columns.${col}`)}</th>
                ))}
                <th className="py-2 px-3" />
              </tr>
            </thead>
            <tbody>
              {displayEvents.map(ev => (
                <tr
                  key={ev.id}
                  onClick={() => setSelectedEvent(ev)}
                  className={`border-b border-outline-variant/10 cursor-pointer transition-colors hover:bg-surface-container-low ${
                    ev.status === 'pending' ? 'border-l-2 border-l-amber-500/60' : ''
                  }`}
                >
                  <td className="py-2.5 px-3 text-gray-400 whitespace-nowrap font-mono text-[10px]">{formatTs(ev.event_time)}</td>
                  <td className="py-2.5 px-3 font-mono text-info whitespace-nowrap">{ev.tool_name || '—'}</td>
                  <td className="py-2.5 px-3"><StatusBadge status={ev.status} /></td>
                  <td className="py-2.5 px-3 text-gray-400 max-w-[160px] truncate" title={ev.rule_name || ev.rule_id}>
                    {ev.rule_name || ev.rule_id || '—'}
                  </td>
                  <td className="py-2.5 px-3"><SeverityChip severity={ev.severity} /></td>
                  {mode === 'global' && (
                    <>
                      <td className="py-2.5 px-3 font-mono text-[10px] text-gray-400 max-w-[100px] truncate">{ev.agent_id || '—'}</td>
                      <td className="py-2.5 px-3 font-mono text-[10px] text-gray-400 max-w-[80px] truncate" title={ev.session_id}>
                        {ev.session_id ? ev.session_id.slice(0, 8) + '…' : '—'}
                      </td>
                    </>
                  )}
                  <td className="py-2.5 px-3 text-gray-400 text-[10px] whitespace-nowrap">{ev.decided_by || '—'}</td>
                  <td className="py-2.5 px-3">
                    {ev.status === 'pending' && ev.is_hold_event && (
                      <div className="flex items-center gap-1.5" onClick={e => e.stopPropagation()}>
                        <button
                          onClick={() => handleDecide(ev, 'approved')}
                          className="px-2.5 py-1 bg-emerald-900/40 text-emerald-400 text-[10px] font-semibold rounded-sm hover:bg-emerald-900/60 transition-colors"
                        >
                          {t('actions.approve')}
                        </button>
                        <button
                          onClick={() => handleDecide(ev, 'denied')}
                          className="px-2.5 py-1 bg-[#93000a]/40 text-error text-[10px] font-semibold rounded-sm hover:bg-[#93000a]/60 transition-colors"
                        >
                          {t('actions.deny')}
                        </button>
                        {inlineErrors[ev.id] && (
                          <span className="text-[10px] text-error" title={inlineErrors[ev.id]}>!</span>
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
