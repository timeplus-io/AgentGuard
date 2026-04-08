import { createContext, useContext, useEffect, useRef, useState, useCallback } from 'react'
import { Link } from 'react-router-dom'

const NotificationsContext = createContext(null)

export function useNotifications() {
  return useContext(NotificationsContext)
}

function severityStyles(severity) {
  if (severity === 'critical') return {
    bar: 'bg-[#ffb0cf]',
    icon: 'text-[#ffb0cf]',
    badge: 'bg-[#93000a] text-[#ffb4ab]',
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
  const s = severityStyles(n.severity)
  return (
    <div className="flex items-stretch bg-[#1d1a25] border border-[#574149]/20 rounded-sm shadow-2xl w-80 overflow-hidden animate-slide-in">
      {/* severity accent bar */}
      <div className={`w-1 flex-shrink-0 ${s.bar}`} />
      <div className="flex-1 p-4 min-w-0">
        <div className="flex items-start justify-between gap-2 mb-1">
          <div className="flex items-center gap-2 min-w-0">
            <span className={`material-symbols-outlined text-[16px] flex-shrink-0 ${s.icon}`}
              style={{ fontVariationSettings: "'FILL' 1" }}>
              {n.severity === 'critical' ? 'gpp_bad' : 'warning'}
            </span>
            <span className="text-xs font-semibold text-white truncate">{n.rule_name || 'Security Alert'}</span>
          </div>
          <button
            onClick={() => onDismiss(n.id)}
            className="text-zinc-500 hover:text-zinc-300 transition-colors flex-shrink-0"
          >
            <span className="material-symbols-outlined text-[14px]">close</span>
          </button>
        </div>
        <p className="text-[11px] text-zinc-400 leading-relaxed line-clamp-2 mb-2">{n.message}</p>
        <div className="flex items-center justify-between gap-2">
          {n.agent_id && (
            <code className="text-[10px] font-mono text-[#ffb0cf] bg-[#ffb0cf]/5 px-1.5 py-0.5 rounded-sm truncate max-w-[120px]">
              {n.agent_id}
            </code>
          )}
          <Link
            to="/threats"
            className="text-[10px] font-semibold text-[#ffb0cf] hover:underline underline-offset-2 ml-auto"
          >
            View threats →
          </Link>
        </div>
      </div>
    </div>
  )
}

export function NotificationsProvider({ children }) {
  const [toasts, setToasts] = useState([])
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

  useEffect(() => {
    const es = new EventSource('/api/notifications/stream')

    es.addEventListener('alert', (e) => {
      try {
        const n = JSON.parse(e.data)
        push(n)
      } catch { /* ignore malformed events */ }
    })

    es.onerror = () => {
      // EventSource reconnects automatically on error — no action needed
    }

    return () => {
      es.close()
      Object.values(timersRef.current).forEach(clearTimeout)
    }
  }, [push])

  return (
    <NotificationsContext.Provider value={{ push }}>
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
