import { useState } from 'react'
import Sidebar from '../components/Sidebar'
import TopNav from '../components/TopNav'
import PageHeader from '../components/PageHeader'
import ThreatsList from '../components/ThreatsList'
import ActivityLog from '../components/ActivityLog'
import { useNotifications } from '../components/NotificationsProvider'
import { Link } from 'react-router-dom'

export default function Threats() {
  const [activeTab, setActiveTab] = useState('threats')
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

          {/* Pending holds banner — only on Threats tab */}
          {activeTab === 'threats' && pendingHoldsArr.length > 0 && (
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
                          h.severity === 'warning'  ? 'text-amber-400' : 'text-zinc-400'
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

          {/* Tab bar */}
          <div className="flex items-center gap-1 mb-6 border-b border-[#574149]/20 pb-0">
            {['threats', 'activity'].map(tab => (
              <button
                key={tab}
                onClick={() => setActiveTab(tab)}
                className={`px-4 py-2 text-xs font-semibold capitalize transition-colors border-b-2 -mb-px ${
                  activeTab === tab
                    ? 'text-[#ffb0cf] border-[#ffb0cf]'
                    : 'text-zinc-500 border-transparent hover:text-zinc-300'
                }`}
              >
                {tab}
                {tab === 'activity' && pendingHoldsArr.length > 0 && (
                  <span className="ml-1.5 px-1.5 py-0.5 rounded-full bg-amber-900/60 text-amber-300 text-[9px] font-bold">
                    {pendingHoldsArr.length}
                  </span>
                )}
              </button>
            ))}
          </div>

          {activeTab === 'threats' && <ThreatsList showFilters />}
          {activeTab === 'activity' && <ActivityLog mode="global" />}
        </div>
      </main>
    </div>
  )
}
