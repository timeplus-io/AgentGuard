import { Link } from 'react-router-dom'
import { useNotifications } from './NotificationsProvider'

export default function HoldToastContainer() {
  const notifications = useNotifications()
  const arr = Array.from(notifications?.pendingHolds?.values() ?? []).slice(0, 3)

  if (arr.length === 0) return null

  return (
    <div className="fixed bottom-6 right-6 z-50 flex flex-col gap-2 max-w-sm">
      {arr.map(h => (
        <div key={h.hold_id} className="bg-[#1d1a25] border border-[#ffb4ab]/40 rounded-sm p-3 shadow-lg">
          <div className="flex items-center gap-2 mb-1">
            <span className="material-symbols-outlined text-[#ffb4ab] text-base">block</span>
            <span className={`text-[9px] uppercase font-semibold ${
              h.severity === 'critical' ? 'text-[#ffb4ab]' :
              h.severity === 'warning'  ? 'text-amber-400' : 'text-zinc-400'
            }`}>{h.severity}</span>
            <span className="text-[10px] text-zinc-400 truncate flex-1">
              Hold pending: <code className="font-mono">{h.tool_name}</code>
            </span>
          </div>
          <p className="text-[11px] text-zinc-300 mb-2 truncate">{h.message}</p>
          <Link
            to={`/threats/by-hold/${encodeURIComponent(h.hold_id)}`}
            className="inline-block px-3 py-1 bg-primary-button text-primary-button-text text-[10px] font-semibold rounded hover:scale-95 transition-transform"
          >
            Review →
          </Link>
        </div>
      ))}
    </div>
  )
}
