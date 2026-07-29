import { createContext, useContext, useEffect, useRef, useState, useCallback } from 'react'
import { Link } from 'react-router-dom'
import { useTranslation } from 'react-i18next'
import { api } from '../api/client'

const NotificationsContext = createContext(null)

function parseField(message, key) {
  for (const part of (message ?? '').split('|')) {
    const idx = part.indexOf(':')
    if (idx < 0) continue
    if (part.slice(0, idx) === key) return part.slice(idx + 1)
  }
  return ''
}

export function useNotifications() {
  return useContext(NotificationsContext)
}

function severityStyles(severity) {
  if (severity === 'critical') return {
    bar: 'bg-[#ffb0cf]',
    icon: 'text-primary',
    badge: 'bg-[#93000a] text-error',
    label: 'CRITICAL',
  }
  return {
    bar: 'bg-amber-400',
    icon: 'text-amber-400',
    badge: 'bg-amber-900/60 text-amber-300',
    label: 'WARNING',
  }
}

function Toast({ n, onDismiss }) {
  const { t } = useTranslation()
  const s = severityStyles(n.severity)
  return (
    <div className="flex items-stretch bg-surface-container-low border border-outline-variant/20 rounded-sm shadow-2xl w-80 overflow-hidden animate-slide-in">
      {/* severity accent bar */}
      <div className={`w-1 flex-shrink-0 ${s.bar}`} />
      <div className="flex-1 p-4 min-w-0">
        <div className="flex items-start justify-between gap-2 mb-1">
          <div className="flex items-center gap-2 min-w-0">
            <span className={`material-symbols-outlined text-[16px] flex-shrink-0 ${s.icon}`}
              style={{ fontVariationSettings: "'FILL' 1" }}>
              {n.severity === 'critical' ? 'gpp_bad' : 'warning'}
            </span>
            <span className="text-xs font-semibold text-gray-100 truncate">{n.rule_name || t('notifications.fallbackTitle')}</span>
          </div>
          <button
            onClick={() => onDismiss(n.id)}
            className="text-gray-400 hover:text-gray-100 transition-colors flex-shrink-0"
          >
            <span className="material-symbols-outlined text-[14px]">close</span>
          </button>
        </div>
        <p className="text-[11px] text-gray-400 leading-relaxed line-clamp-2 mb-2">{n.message}</p>
        <div className="flex items-center justify-between gap-2">
          {n.agent_id && (
            <code className="text-[10px] font-mono text-primary bg-[#ffb0cf]/5 px-1.5 py-0.5 rounded-sm truncate max-w-[120px]">
              {n.agent_id}
            </code>
          )}
          <Link
            to="/threats"
            className="text-[10px] font-semibold text-primary hover:underline underline-offset-2 ml-auto"
          >
            {t('notifications.viewThreats')}
          </Link>
        </div>
      </div>
    </div>
  )
}

export function NotificationsProvider({ children }) {
  const [toasts, setToasts] = useState([])
  const [pendingHolds, setPendingHolds] = useState(new Map())
  const timersRef = useRef({})

  const dismiss = useCallback((id) => {
    clearTimeout(timersRef.current[id])
    delete timersRef.current[id]
    setToasts(prev => prev.filter(t => t.id !== id))
  }, [])

  const push = useCallback((notification) => {
    setToasts(prev => [notification, ...prev].slice(0, 5))
    // Auto-dismiss after 6 seconds
    timersRef.current[notification.id] = setTimeout(() => dismiss(notification.id), 6000)
  }, [dismiss])

  const reconcileHolds = useCallback(async () => {
    try {
      const r = await api.listHolds('pending')
      const next = new Map()
      for (const h of r.holds ?? []) next.set(h.hold_id, h)
      setPendingHolds(next)
    } catch { /* best-effort */ }
  }, [])

  useEffect(() => {
    const es = new EventSource('/api/notifications/stream', { withCredentials: true })
    let cancelled = false

    es.onopen = () => { if (!cancelled) reconcileHolds() }

    es.addEventListener('alert', (e) => {
      try {
        const n = JSON.parse(e.data)
        if (n.rule_name === 'hold_pending') {
          const holdId = parseField(n.message, 'hold')
          const tool = parseField(n.message, 'tool')
          const reason = parseField(n.message, 'reason')
          setPendingHolds(prev => {
            const next = new Map(prev)
            next.set(holdId, {
              hold_id: holdId,
              severity: n.severity,
              agent_id: n.agent_id,
              tool_name: tool,
              message: reason,
              created_at: n.detected_at,
            })
            return next
          })
          return
        }
        if (n.rule_name === 'hold_decided') {
          const holdId = parseField(n.message, 'hold')
          setPendingHolds(prev => {
            if (!prev.has(holdId)) return prev
            const next = new Map(prev)
            next.delete(holdId)
            return next
          })
          return
        }
        push(n)
      } catch { /* ignore malformed events */ }
    })

    es.onerror = () => {
      // EventSource reconnects automatically on error — no action needed
    }

    return () => {
      cancelled = true
      es.close()
      Object.values(timersRef.current).forEach(clearTimeout)
    }
  }, [push, reconcileHolds])

  return (
    <NotificationsContext.Provider value={{ push, pendingHolds, reconcileHolds }}>
      {children}
      {/* Fixed toast stack — rendered outside the page layout so it appears on every page */}
      {toasts.length > 0 && (
        <div className="fixed top-20 right-4 z-50 flex flex-col gap-2 pointer-events-none">
          {toasts.map(n => (
            <div key={n.id} className="pointer-events-auto">
              <Toast n={n} onDismiss={dismiss} />
            </div>
          ))}
        </div>
      )}
    </NotificationsContext.Provider>
  )
}
