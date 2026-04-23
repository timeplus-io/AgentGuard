import { Link } from 'react-router-dom'
import Sidebar from '../components/Sidebar'
import TopNav from '../components/TopNav'
import PageHeader from '../components/PageHeader'
import ThreatsList from '../components/ThreatsList'
import { useNotifications } from '../components/NotificationsProvider'

export default function Threats() {
  const notifications = useNotifications()
  const pendingHoldsArr = Array.from(notifications?.pendingHolds?.values() ?? []).sort(
    (a, b) => new Date(b.created_at) - new Date(a.created_at)
  )

  return (
    <div className="min-h-screen bg-[#15121d] text-[#e7dff0]">
      <Sidebar />
      <TopNav />

      <main className="pl-sidebar pt-16 pb-8 min-h-screen">
        <div className="p-8">
          <PageHeader
            title="Threats"
            subtitle="Deduplicated security threats detected across AI agents."
          />

          {pendingHoldsArr.length > 0 && (
            <section className="bg-[#1d1a25] rounded-sm p-4 mb-6">
              <h3 className="text-[11px] font-semibold uppercase tracking-widest text-[#ffb4ab] mb-3">
                ⚠ Pending Holds ({pendingHoldsArr.length})
              </h3>
              <ul className="divide-y divide-[#574149]/20">
                {pendingHoldsArr.map(h => (
                  <li key={h.hold_id} className="flex items-center justify-between py-2.5">
                    <div className="min-w-0 flex-1">
                      <div className="flex items-center gap-2 mb-0.5">
                        <span className={`text-[10px] uppercase font-semibold ${
                          h.severity === 'critical' ? 'text-[#ffb4ab]' :
                          h.severity === 'warning'  ? 'text-amber-400'  : 'text-zinc-400'
                        }`}>
                          {h.severity}
                        </span>
                        <span className="text-xs text-[#e7dff0] font-mono">{h.agent_id}</span>
                        <span className="text-zinc-700">·</span>
                        <span className="text-xs text-[#e7dff0] font-mono">{h.tool_name}</span>
                      </div>
                      <p className="text-[11px] text-zinc-400 truncate">{h.message}</p>
                    </div>
                    <Link
                      to={`/threats/by-hold/${encodeURIComponent(h.hold_id)}`}
                      className="ml-4 px-3 py-1.5 text-[11px] font-semibold rounded bg-primary-button text-primary-button-text hover:scale-95 transition-transform"
                    >
                      Review →
                    </Link>
                  </li>
                ))}
              </ul>
            </section>
          )}

          <ThreatsList showFilters />
        </div>
      </main>
    </div>
  )
}
