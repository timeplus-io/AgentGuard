import { useState, useEffect } from 'react'
import Sidebar from '../components/Sidebar'
import TopNav from '../components/TopNav'
import Footer from '../components/Footer'
import PageHeader from '../components/PageHeader'
import { SeverityBadge } from '../components/Badge'
import { api } from '../api/client'

const staticThreats = [
  { id: 1, severity: 'Critical', name: 'Prompt Injection Attempt', vector: 'Vector: System Message Override', agent: 'OpenClaw-Prod-1', session: 'sess_8f22x_991', date: 'Oct 24, 14:02:11', ago: '2 mins ago' },
  { id: 2, severity: 'Warning', name: 'Abnormal Token Consumption', vector: 'Vector: Rate Limit Near-Threshold', agent: 'SecureMind-Edge-7', session: 'sess_11a9q_420', date: 'Oct 24, 13:45:00', ago: '19 mins ago' },
  { id: 3, severity: 'Critical', name: 'Sensitive Data Leakage', vector: 'Vector: PII Detection in Output', agent: 'OpenClaw-Prod-3', session: 'sess_5d3r4_102', date: 'Oct 24, 13:12:44', ago: '51 mins ago' },
  { id: 4, severity: 'Info', name: 'System Patch Required', vector: 'Vector: New Security Definitions Available', agent: 'GLOBAL-ORCH', session: 'N/A', date: 'Oct 24, 12:00:01', ago: '2 hours ago' },
]

const liveStream = [
  { time: '14:05:22', text: <><span className="text-white font-semibold">OpenClaw-Prod-1</span> performed a recursive sanity check on session <span className="text-[#ffb2b8]">sess_8f22x_991</span>.</> },
  { time: '14:05:18', text: <><span className="text-white font-semibold">Firewall-01</span> blocked incoming traffic from <span className="text-[#ffb4ab]">192.168.1.12</span> due to malformed header.</> },
  { time: '14:04:59', text: <><span className="text-white font-semibold">System</span> successfully rotated encryption keys for region: <span className="text-[#ffb0cf]">US-EAST-1</span>.</> },
]

export default function Threats() {
  const [threats, setThreats] = useState(staticThreats)
  const [loading, setLoading] = useState(true)
  const [severity, setSeverity] = useState('All')
  const [category, setCategory] = useState('All Categories')

  useEffect(() => {
    const params = {}
    if (severity !== 'All') params.severity = severity
    if (category !== 'All Categories') params.category = category
    api.listThreats(params)
      .then(data => {
        if (data.threats && data.threats.length > 0) setThreats(data.threats)
      })
      .catch(() => {})
      .finally(() => setLoading(false))
  }, [severity, category])

  const filtered = threats.filter(t => severity === 'All' || t.severity === severity)

  return (
    <div className="min-h-screen bg-[#15121d] text-[#e7dff0]">
      <Sidebar />
      <TopNav />
      <Footer />
      <main className="pl-64 pt-16 pb-8 min-h-screen">
        <div className="p-8">
          <PageHeader
            title="Threats"
            subtitle="Real-time surveillance of AI agent activity and anomalous patterns."
            actions={<>
              <button className="flex items-center gap-2 px-4 py-2 bg-[#2c2834] hover:bg-[#37333f] transition-colors text-xs font-medium text-[#e7dff0] rounded-sm border border-[#574149]/10">
                <span className="material-symbols-outlined text-[16px]">export_notes</span>
                Export to SIEM
              </button>
              <button className="flex items-center gap-2 px-4 py-2 bg-gradient-to-br from-[#ffb0cf] to-[#f256a3] hover:opacity-90 transition-opacity text-xs font-semibold text-[#3d0023] rounded-sm">
                <span className="material-symbols-outlined text-[16px]">block</span>
                Block Pattern
              </button>
            </>}
          />

          {/* Filter Bar */}
          <section className="bg-[#211e29] rounded p-6 mb-6 flex flex-wrap items-center gap-6">
            <div className="flex flex-col gap-2 flex-1 min-w-[200px]">
              <label className="text-[10px] font-semibold text-zinc-500 uppercase tracking-widest">Severity</label>
              <div className="flex gap-1">
                {['Critical', 'Warning', 'Info'].map(s => (
                  <button
                    key={s}
                    onClick={() => setSeverity(prev => prev === s ? 'All' : s)}
                    className={`px-3 py-1.5 text-[11px] font-semibold rounded-full transition-colors ${
                      severity === s
                        ? s === 'Critical' ? 'bg-[#93000a] text-[#ffb4ab]'
                          : s === 'Warning' ? 'bg-[#37333f] text-[#ffb2b8]'
                          : 'bg-[#2c2834] text-zinc-300'
                        : 'bg-[#2c2834] text-[#ddbfc8] border border-[#574149]/10'
                    }`}
                  >
                    {s}
                  </button>
                ))}
              </div>
            </div>
            <div className="flex flex-col gap-2 flex-1 min-w-[200px]">
              <label className="text-[10px] font-semibold text-zinc-500 uppercase tracking-widest">Threat Category</label>
              <select
                value={category}
                onChange={e => setCategory(e.target.value)}
                className="bg-[#0f0c17] border-none text-xs font-medium text-[#e7dff0] rounded-sm py-2 px-3 focus:ring-1 focus:ring-[#ffb0cf]/50"
              >
                <option>All Categories</option>
                <option>Injection</option>
                <option>Exfiltration</option>
                <option>Supply Chain</option>
              </select>
            </div>
            <div className="flex flex-col gap-2 flex-1 min-w-[200px]">
              <label className="text-[10px] font-semibold text-zinc-500 uppercase tracking-widest">Date Range</label>
              <div className="relative">
                <span className="material-symbols-outlined absolute left-3 top-1/2 -translate-y-1/2 text-zinc-500 text-xs">calendar_month</span>
                <input className="bg-[#0f0c17] border-none text-xs font-medium text-[#e7dff0] rounded-sm py-2 pl-9 pr-3 w-full cursor-pointer" readOnly type="text" defaultValue="Last 24 Hours" />
              </div>
            </div>
            <div className="flex items-end h-full">
              <button className="p-2.5 bg-[#ffb0cf]/10 text-[#ffb0cf] hover:bg-[#ffb0cf]/20 transition-colors rounded-sm">
                <span className="material-symbols-outlined">filter_list</span>
              </button>
            </div>
          </section>

          {/* Table */}
          <section className="bg-[#211e29] rounded-sm overflow-hidden">
            <div className="overflow-x-auto">
              <table className="w-full text-left border-collapse">
                <thead className="bg-[#2c2834]/50">
                  <tr>
                    {['Severity', 'Alert Name', 'Agent Identity', 'Session ID', 'Timestamp', 'Action'].map((h, i) => (
                      <th key={h} className={`px-6 py-4 text-[10px] font-bold text-zinc-400 uppercase tracking-widest ${i === 5 ? 'text-right' : ''}`}>{h}</th>
                    ))}
                  </tr>
                </thead>
                <tbody className="divide-y divide-[#574149]/10">
                  {loading ? (
                    <tr><td colSpan={6} className="px-6 py-12 text-center text-zinc-500">Loading...</td></tr>
                  ) : filtered.map(t => (
                    <tr key={t.id} className="hover:bg-[#2c2834] transition-colors group">
                      <td className="px-6 py-5"><SeverityBadge severity={t.severity} /></td>
                      <td className="px-6 py-5">
                        <p className="text-xs font-semibold text-[#e7dff0]">{t.name}</p>
                        <p className="text-[10px] text-zinc-500 font-medium">{t.vector}</p>
                      </td>
                      <td className="px-6 py-5">
                        <code className="text-xs font-mono text-[#ffb0cf] px-2 py-0.5 bg-[#ffb0cf]/5 rounded-sm">{t.agent}</code>
                      </td>
                      <td className="px-6 py-5">
                        <span className="text-xs text-zinc-400">{t.session}</span>
                      </td>
                      <td className="px-6 py-5">
                        <p className="text-xs text-[#e7dff0]">{t.date}</p>
                        <p className="text-[10px] text-zinc-500">{t.ago}</p>
                      </td>
                      <td className="px-6 py-5 text-right">
                        <button className="text-[#ffb0cf] hover:text-[#570033] text-xs font-semibold transition-colors">View Details</button>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
            <div className="p-6 bg-[#211e29] border-t border-[#574149]/10 flex justify-between items-center">
              <span className="text-[10px] font-semibold text-zinc-500 uppercase tracking-widest">Showing 1-12 of 284 Threats</span>
              <div className="flex gap-2">
                <button className="p-2 bg-[#2c2834] rounded text-zinc-500 hover:text-[#e7dff0]">
                  <span className="material-symbols-outlined text-xs">chevron_left</span>
                </button>
                {[1, 2, 3].map(p => (
                  <button key={p} className={`p-2 rounded text-[10px] font-semibold px-3 ${p === 1 ? 'bg-[#ffb0cf]/20 text-[#ffb0cf]' : 'bg-[#2c2834] text-zinc-500 hover:text-[#e7dff0]'}`}>{p}</button>
                ))}
                <button className="p-2 bg-[#2c2834] rounded text-zinc-500 hover:text-[#e7dff0]">
                  <span className="material-symbols-outlined text-xs">chevron_right</span>
                </button>
              </div>
            </div>
          </section>

          {/* Bottom */}
          <div className="mt-8 grid grid-cols-1 lg:grid-cols-3 gap-6">
            <div className="lg:col-span-2 bg-[#2c2834] p-8 rounded-sm relative overflow-hidden">
              <div className="absolute top-0 right-0 p-4 opacity-5">
                <span className="material-symbols-outlined text-[120px]">shield_with_heart</span>
              </div>
              <h3 className="text-lg font-medium mb-6 flex items-center gap-3">
                <span className="w-2 h-2 rounded-full bg-[#ffb0cf] shadow-[0_0_10px_#ffb0cf]"></span>
                Live Data Stream
              </h3>
              <div className="space-y-4">
                {liveStream.map((item, i) => (
                  <div key={i} className="flex items-start gap-4">
                    <span className="text-[10px] font-mono text-[#ffb0cf] py-1">{item.time}</span>
                    <p className="text-xs text-zinc-300">{item.text}</p>
                  </div>
                ))}
              </div>
            </div>
            <div className="bg-gradient-to-br from-[#211e29] to-[#15121d] p-8 rounded-sm flex flex-col justify-center items-center text-center border border-[#ffb0cf]/5">
              <div className="w-16 h-16 rounded-full bg-[#ffb0cf]/10 flex items-center justify-center mb-4">
                <span className="material-symbols-outlined text-[#ffb0cf] text-2xl">insights</span>
              </div>
              <h4 className="text-base font-semibold mb-2">Threat Intelligence</h4>
              <p className="text-xs text-zinc-400 mb-6 px-4">Automatic pattern analysis suggests an uptick in Injection attempts from APAC regions.</p>
              <button className="w-full py-3 bg-[#ffb0cf] text-[#63003b] font-semibold text-xs rounded hover:opacity-90 transition-opacity">
                Update Definitions
              </button>
            </div>
          </div>
        </div>
      </main>
    </div>
  )
}
