import { useState, useEffect } from 'react'
import { useTranslation } from 'react-i18next'
import { api } from '../api/client'
import { formatDate } from '../i18n/format'

function formatTs(ts) {
  if (!ts) return '—'
  try {
    const d = new Date(typeof ts === 'string' ? ts.replace(' ', 'T').replace(/\s+UTC$/, 'Z') : ts)
    if (isNaN(d)) return String(ts)
    return formatDate(d)
  } catch { return String(ts) }
}

function JsonBlock({ raw }) {
  if (!raw) return <span className="text-gray-400 text-[11px]">(empty)</span>
  try {
    return (
      <pre className="text-[11px] font-mono text-gray-100 whitespace-pre-wrap break-all leading-relaxed">
        {JSON.stringify(JSON.parse(raw), null, 2)}
      </pre>
    )
  } catch {
    return <pre className="text-[11px] font-mono text-gray-100 whitespace-pre-wrap break-all">{raw}</pre>
  }
}

export default function ActivityDrawer({ event: initialEvent, onClose, onDecided }) {
  const { t } = useTranslation()
  const [event, setEvent] = useState(initialEvent)
  const [timeline, setTimeline] = useState([])
  const [decideStatus, setDecideStatus] = useState('idle') // idle | pending | done | error
  const [decideReason, setDecideReason] = useState('')
  const [decideError, setDecideError] = useState('')

  // Fetch fresh hold state on open (handles stale pending rows)
  useEffect(() => {
    if (!initialEvent.is_hold_event) return
    api.getHold(initialEvent.id).then(data => {
      if (data?.hold) setEvent(prev => ({ ...prev, ...data.hold, id: data.hold.hold_id ?? prev.id }))
      if (data?.timeline) setTimeline(data.timeline)
    }).catch(() => { /* best-effort */ })
  }, [initialEvent.id, initialEvent.is_hold_event])

  async function handleDecide(decision) {
    setDecideStatus('pending')
    setDecideError('')
    try {
      await api.decideHold(event.id, decision, decideReason)
      setDecideStatus('done')
      onDecided?.()
      // Refresh hold state to show final decision
      const data = await api.getHold(event.id).catch(() => null)
      if (data?.hold) setEvent(prev => ({ ...prev, ...data.hold, id: data.hold.hold_id ?? prev.id }))
      if (data?.timeline) setTimeline(data.timeline)
    } catch (err) {
      setDecideStatus('error')
      setDecideError(err.message)
    }
  }

  const isPending = event.status === 'pending' && event.is_hold_event && decideStatus !== 'done'

  return (
    <>
      {/* Backdrop */}
      <div
        className="fixed inset-0 bg-black/40 z-40"
        onClick={onClose}
      />

      {/* Drawer */}
      <div className="fixed right-0 top-0 h-full w-[480px] max-w-full bg-surface border-l border-outline-variant/20 z-50 flex flex-col overflow-hidden">
        {/* Header */}
        <div className="flex items-center justify-between px-5 py-4 border-b border-outline-variant/20">
          <div>
            <h2 className="text-sm font-semibold text-gray-100">{t('activityDrawer.title')}</h2>
            <p className="text-[10px] font-mono text-gray-400 mt-0.5">{event.id}</p>
          </div>
          <button onClick={onClose} className="text-gray-400 hover:text-gray-100 transition-colors">
            <span className="material-symbols-outlined text-[18px]">close</span>
          </button>
        </div>

        {/* Body — scrollable */}
        <div className="flex-1 overflow-y-auto px-5 py-4 space-y-5">

          {/* Summary row */}
          <div className="grid grid-cols-2 gap-3 text-[11px]">
            <div>
              <p className="text-gray-400 text-[10px] uppercase tracking-widest mb-1">{t('activity.columns.tool')}</p>
              <p className="font-mono text-info">{event.tool_name || '—'}</p>
            </div>
            <div>
              <p className="text-gray-400 text-[10px] uppercase tracking-widest mb-1">{t('activity.columns.status')}</p>
              <p className="text-gray-100">{event.status}</p>
            </div>
            <div>
              <p className="text-gray-400 text-[10px] uppercase tracking-widest mb-1">{t('activity.columns.rule')}</p>
              <p className="text-gray-100">{event.rule_name || event.rule_id || '—'}</p>
            </div>
            <div>
              <p className="text-gray-400 text-[10px] uppercase tracking-widest mb-1">{t('activity.columns.severity')}</p>
              <p className={event.severity === 'critical' ? 'text-error' : event.severity === 'warning' ? 'text-amber-400' : 'text-gray-400'}>
                {event.severity ? t(`severity.${String(event.severity).toLowerCase()}`, { defaultValue: event.severity }) : '—'}
              </p>
            </div>
            <div>
              <p className="text-gray-400 text-[10px] uppercase tracking-widest mb-1">{t('activity.columns.agent')}</p>
              <p className="font-mono text-[10px] text-gray-400 truncate">{event.agent_id || '—'}</p>
            </div>
            <div>
              <p className="text-gray-400 text-[10px] uppercase tracking-widest mb-1">{t('activity.columns.time')}</p>
              <p className="text-gray-400">{formatTs(event.event_time)}</p>
            </div>
          </div>

          {/* Message */}
          {event.message && (
            <div>
              <p className="text-gray-400 text-[10px] uppercase tracking-widest mb-1.5">{t('activityDrawer.message')}</p>
              <p className="text-[11px] text-gray-100">{event.message}</p>
            </div>
          )}

          {/* Arguments */}
          <div>
            <p className="text-gray-400 text-[10px] uppercase tracking-widest mb-1.5">{t('activityDrawer.arguments')}</p>
            <div className="bg-surface-container-lowest rounded-sm p-3 max-h-64 overflow-y-auto">
              <JsonBlock raw={event.args_summary || event.hook_payload} />
            </div>
          </div>

          {/* Decision timeline (hold events only) */}
          {event.is_hold_event && (
            <div>
              <p className="text-gray-400 text-[10px] uppercase tracking-widest mb-2">{t('activityDrawer.decisionTimeline')}</p>
              {timeline.length > 0 ? (
                <div className="space-y-2">
                  {timeline.map((entry, i) => (
                    <div key={i} className="flex items-start gap-2.5 text-[11px]">
                      <span className="w-1.5 h-1.5 rounded-full bg-zinc-600 mt-1.5 shrink-0" />
                      <div>
                        <span className="text-gray-400 font-medium capitalize">{entry.status}</span>
                        {entry.decided_by && <span className="text-gray-400"> · {entry.decided_by}</span>}
                        {entry.decided_reason && <span className="text-gray-400 italic"> — {entry.decided_reason}</span>}
                        <p className="text-gray-400 text-[10px]">{formatTs(entry.updated_at)}</p>
                      </div>
                    </div>
                  ))}
                </div>
              ) : (
                <p className="text-[11px] text-gray-400">{t('activityDrawer.noTimelineEntries')}</p>
              )}
            </div>
          )}

          {/* Approve/Deny form — pending holds only */}
          {isPending && (
            <div className="bg-amber-900/20 border border-amber-700/30 rounded-sm p-4">
              <p className="text-[11px] font-semibold text-amber-300 mb-3">{t('activityDrawer.pausedWaiting')}</p>
              <textarea
                placeholder={t('activityDrawer.reasonPlaceholder')}
                value={decideReason}
                onChange={e => setDecideReason(e.target.value)}
                className="w-full bg-surface-container-lowest border border-outline-variant/30 rounded-sm p-2 text-xs text-gray-100 focus:border-zinc-500 focus:outline-none mb-3 resize-none"
                rows={2}
              />
              <div className="flex gap-2">
                <button
                  onClick={() => handleDecide('approved')}
                  disabled={decideStatus === 'pending'}
                  className="px-4 py-1.5 bg-emerald-500/20 text-emerald-400 text-xs font-semibold rounded-sm hover:bg-emerald-500/30 disabled:opacity-50 transition-colors"
                >
                  ✓ {t('actions.approve')}
                </button>
                <button
                  onClick={() => handleDecide('denied')}
                  disabled={decideStatus === 'pending'}
                  className="px-4 py-1.5 bg-[#ffb4ab]/20 text-error text-xs font-semibold rounded-sm hover:bg-[#ffb4ab]/30 disabled:opacity-50 transition-colors"
                >
                  ✗ {t('actions.deny')}
                </button>
              </div>
              {decideStatus === 'error' && (
                <p className="text-[11px] text-error mt-2">{decideError || t('activityDrawer.decideFailedDefault')}</p>
              )}
            </div>
          )}

          {/* Decision confirmed */}
          {decideStatus === 'done' && (
            <div className="bg-emerald-900/20 border border-emerald-700/30 rounded-sm p-3">
              <p className="text-xs text-emerald-400">{t('activityDrawer.decisionSubmitted')}</p>
            </div>
          )}
        </div>
      </div>
    </>
  )
}
