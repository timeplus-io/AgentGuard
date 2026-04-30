import { useState, useEffect } from 'react'
import { api } from '../api/client'

function formatTs(ts) {
  if (!ts) return '—'
  try {
    const d = new Date(typeof ts === 'string' ? ts.replace(' ', 'T').replace(/\s+UTC$/, 'Z') : ts)
    if (isNaN(d)) return String(ts)
    return d.toLocaleString()
  } catch { return String(ts) }
}

function JsonBlock({ raw }) {
  if (!raw) return <span className="text-zinc-600 text-[11px]">(empty)</span>
  try {
    return (
      <pre className="text-[11px] font-mono text-zinc-300 whitespace-pre-wrap break-all leading-relaxed">
        {JSON.stringify(JSON.parse(raw), null, 2)}
      </pre>
    )
  } catch {
    return <pre className="text-[11px] font-mono text-zinc-300 whitespace-pre-wrap break-all">{raw}</pre>
  }
}

export default function ActivityDrawer({ event: initialEvent, onClose, onDecided }) {
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
      <div className="fixed right-0 top-0 h-full w-[480px] max-w-full bg-[#15121d] border-l border-[#574149]/20 z-50 flex flex-col overflow-hidden">
        {/* Header */}
        <div className="flex items-center justify-between px-5 py-4 border-b border-[#574149]/20">
          <div>
            <h2 className="text-sm font-semibold text-[#e7dff0]">Tool Call Detail</h2>
            <p className="text-[10px] font-mono text-zinc-500 mt-0.5">{event.id}</p>
          </div>
          <button onClick={onClose} className="text-zinc-500 hover:text-zinc-300 transition-colors">
            <span className="material-symbols-outlined text-[18px]">close</span>
          </button>
        </div>

        {/* Body — scrollable */}
        <div className="flex-1 overflow-y-auto px-5 py-4 space-y-5">

          {/* Summary row */}
          <div className="grid grid-cols-2 gap-3 text-[11px]">
            <div>
              <p className="text-zinc-500 text-[10px] uppercase tracking-widest mb-1">Tool</p>
              <p className="font-mono text-[#93c5fd]">{event.tool_name || '—'}</p>
            </div>
            <div>
              <p className="text-zinc-500 text-[10px] uppercase tracking-widest mb-1">Status</p>
              <p className="text-[#e7dff0]">{event.status}</p>
            </div>
            <div>
              <p className="text-zinc-500 text-[10px] uppercase tracking-widest mb-1">Rule</p>
              <p className="text-[#e7dff0]">{event.rule_name || event.rule_id || '—'}</p>
            </div>
            <div>
              <p className="text-zinc-500 text-[10px] uppercase tracking-widest mb-1">Severity</p>
              <p className={event.severity === 'critical' ? 'text-[#ffb4ab]' : event.severity === 'warning' ? 'text-amber-400' : 'text-zinc-400'}>
                {event.severity || '—'}
              </p>
            </div>
            <div>
              <p className="text-zinc-500 text-[10px] uppercase tracking-widest mb-1">Agent</p>
              <p className="font-mono text-[10px] text-zinc-400 truncate">{event.agent_id || '—'}</p>
            </div>
            <div>
              <p className="text-zinc-500 text-[10px] uppercase tracking-widest mb-1">Time</p>
              <p className="text-zinc-400">{formatTs(event.event_time)}</p>
            </div>
          </div>

          {/* Message */}
          {event.message && (
            <div>
              <p className="text-zinc-500 text-[10px] uppercase tracking-widest mb-1.5">Message</p>
              <p className="text-[11px] text-zinc-300">{event.message}</p>
            </div>
          )}

          {/* Arguments */}
          <div>
            <p className="text-zinc-500 text-[10px] uppercase tracking-widest mb-1.5">Arguments</p>
            <div className="bg-[#0f0c17] rounded-sm p-3 max-h-64 overflow-y-auto">
              <JsonBlock raw={event.args_summary || event.hook_payload} />
            </div>
          </div>

          {/* Decision timeline (hold events only) */}
          {event.is_hold_event && (
            <div>
              <p className="text-zinc-500 text-[10px] uppercase tracking-widest mb-2">Decision Timeline</p>
              {timeline.length > 0 ? (
                <div className="space-y-2">
                  {timeline.map((entry, i) => (
                    <div key={i} className="flex items-start gap-2.5 text-[11px]">
                      <span className="w-1.5 h-1.5 rounded-full bg-zinc-600 mt-1.5 shrink-0" />
                      <div>
                        <span className="text-zinc-400 font-medium capitalize">{entry.status}</span>
                        {entry.decided_by && <span className="text-zinc-600"> · {entry.decided_by}</span>}
                        {entry.decided_reason && <span className="text-zinc-600 italic"> — {entry.decided_reason}</span>}
                        <p className="text-zinc-600 text-[10px]">{formatTs(entry.updated_at)}</p>
                      </div>
                    </div>
                  ))}
                </div>
              ) : (
                <p className="text-[11px] text-zinc-600">No timeline entries yet.</p>
              )}
            </div>
          )}

          {/* Approve/Deny form — pending holds only */}
          {isPending && (
            <div className="bg-amber-900/20 border border-amber-700/30 rounded-sm p-4">
              <p className="text-[11px] font-semibold text-amber-300 mb-3">Agent is paused — waiting for decision</p>
              <textarea
                placeholder="Optional reason…"
                value={decideReason}
                onChange={e => setDecideReason(e.target.value)}
                className="w-full bg-[#0f0c17] border border-[#574149]/30 rounded-sm p-2 text-xs text-[#e7dff0] focus:border-zinc-500 focus:outline-none mb-3 resize-none"
                rows={2}
              />
              <div className="flex gap-2">
                <button
                  onClick={() => handleDecide('approved')}
                  disabled={decideStatus === 'pending'}
                  className="px-4 py-1.5 bg-emerald-500/20 text-emerald-400 text-xs font-semibold rounded-sm hover:bg-emerald-500/30 disabled:opacity-50 transition-colors"
                >
                  ✓ Approve
                </button>
                <button
                  onClick={() => handleDecide('denied')}
                  disabled={decideStatus === 'pending'}
                  className="px-4 py-1.5 bg-[#ffb4ab]/20 text-[#ffb4ab] text-xs font-semibold rounded-sm hover:bg-[#ffb4ab]/30 disabled:opacity-50 transition-colors"
                >
                  ✗ Deny
                </button>
              </div>
              {decideStatus === 'error' && (
                <p className="text-[11px] text-[#ffb4ab] mt-2">{decideError || 'Failed to submit. Try again.'}</p>
              )}
            </div>
          )}

          {/* Decision confirmed */}
          {decideStatus === 'done' && (
            <div className="bg-emerald-900/20 border border-emerald-700/30 rounded-sm p-3">
              <p className="text-xs text-emerald-400">Decision submitted — agent has been notified.</p>
            </div>
          )}
        </div>
      </div>
    </>
  )
}
