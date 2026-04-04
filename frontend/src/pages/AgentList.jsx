import { useState, useEffect } from 'react'
import { Link } from 'react-router-dom'
import Sidebar from '../components/Sidebar'
import TopNav from '../components/TopNav'
import Footer from '../components/Footer'
import { api } from '../api/client'

const staticAgents = [
  { id: 'AS-DELTA-9012', region: 'us-east-1a', instance: 't3.large', status: 'ACTIVE', healthLabel: 'OPTIMAL', healthPct: 98, throughput: '1,240', lastAction: 'Suspicious payload filtered • 2m ago' },
  { id: 'AS-SIGMA-4421', region: 'eu-central-1', instance: 'm5.xlarge', status: 'THROTTLED', healthLabel: 'STRESSED', healthPct: 64, throughput: '420', lastAction: 'Rate limiting applied • 14s ago' },
  { id: 'AS-OMEGA-1102', region: 'ap-southeast-1', instance: 't3.medium', status: 'SUSPENDED', healthLabel: 'CRITICAL', healthPct: 12, throughput: '0', lastAction: 'Manual killswitch engaged • 1h ago' },
  { id: 'AS-ALPHA-3030', region: 'us-west-2', instance: 'p3.2xlarge', status: 'ACTIVE', healthLabel: 'OPTIMAL', healthPct: 100, throughput: '3,892', lastAction: 'Vector database re-indexed • 5m ago' },
]

const statusConfig = {
  ACTIVE: { dot: 'bg-[#ffb0cf] shadow-[0_0_8px_rgba(255,176,207,0.4)]', badge: 'bg-[#ffb0cf]/10 text-[#ffb0cf] border border-[#ffb0cf]/20' },
  THROTTLED: { dot: 'bg-[#ffb2b8] shadow-[0_0_8px_rgba(255,178,184,0.4)]', badge: 'bg-[#ffb2b8]/10 text-[#ffb2b8] border border-[#ffb2b8]/20' },
  SUSPENDED: { dot: 'bg-[#93000a]', badge: 'bg-[#93000a]/20 text-[#ffb4ab] border border-[#ffb4ab]/20' },
}

const healthBarColor = {
  OPTIMAL: 'bg-gradient-to-r from-[#ffb0cf] to-[#f256a3]',
  STRESSED: 'bg-[#ffb2b8]',
  CRITICAL: 'bg-[#ffb4ab]',
}

const healthTextColor = {
  OPTIMAL: 'text-[#ffb0cf]',
  STRESSED: 'text-[#ffb2b8]',
  CRITICAL: 'text-[#ffb4ab] font-semibold',
}

export default function AgentList() {
  const [agents, setAgents] = useState(staticAgents)
  const [loading, setLoading] = useState(true)
  const [page, setPage] = useState(1)
  const [total, setTotal] = useState(124)

  useEffect(() => {
    api.listAgents(page, 15)
      .then(data => {
        if (data.agents && data.agents.length > 0) {
          setAgents(data.agents)
          setTotal(data.total || 124)
        }
      })
      .catch(() => {})
      .finally(() => setLoading(false))
  }, [page])

  return (
    <div className="min-h-screen bg-[#15121d] text-[#e7dff0]">
      <Sidebar />
      <TopNav />
      <Footer />
      <main className="pl-64 pt-16 pb-8 min-h-screen">
        <div className="p-8 max-w-7xl w-full mx-auto">
          {/* Header */}
          <div className="flex flex-col md:flex-row md:items-end justify-between mb-10 gap-4">
            <div>
              <h2 className="text-3xl font-medium text-[#e7dff0] tracking-tight mb-2">Agent Fleet</h2>
              <p className="text-zinc-500 text-xs max-w-lg">
                Manage and monitor your active sentinel agents. Currently monitoring{' '}
                <span className="text-[#ffb0cf] font-medium">12 active nodes</span> across 3 regions.
              </p>
            </div>
            <div className="flex gap-3">
              <button className="px-4 py-2 bg-[#2c2834] text-[#e7dff0] text-xs font-medium rounded-sm border border-[#574149]/10 hover:bg-[#37333f] transition-colors flex items-center gap-2">
                <span className="material-symbols-outlined text-xs">filter_list</span>
                Filters
              </button>
              <Link to="/onboarding" className="px-4 py-2 bg-gradient-to-br from-[#ffb0cf] to-[#f256a3] text-[#3d0023] text-xs font-semibold rounded-sm flex items-center gap-2 hover:scale-[0.98] transition-transform">
                <span className="material-symbols-outlined text-xs">cable</span>
                Connect Agents
              </Link>
            </div>
          </div>

          {/* Table */}
          <div className="bg-[#1d1a25] rounded-sm overflow-hidden">
            <table className="w-full text-left border-collapse">
              <thead>
                <tr className="bg-[#211e29] text-[10px] font-semibold uppercase tracking-[0.15em] text-zinc-500">
                  <th className="px-6 py-4">Agent ID</th>
                  <th className="px-6 py-4">Status</th>
                  <th className="px-6 py-4">Health Index</th>
                  <th className="px-6 py-4">Throughput</th>
                  <th className="px-6 py-4">Last Action</th>
                  <th className="px-6 py-4 text-right">Operations</th>
                </tr>
              </thead>
              <tbody className="divide-y divide-[#574149]/10">
                {loading ? (
                  <tr>
                    <td colSpan={6} className="px-6 py-12 text-center text-zinc-500">
                      <span className="material-symbols-outlined animate-spin text-[#ffb0cf]">refresh</span>
                    </td>
                  </tr>
                ) : agents.map(agent => {
                  const sc = statusConfig[agent.status] || statusConfig.ACTIVE
                  const hColor = healthBarColor[agent.healthLabel] || healthBarColor.OPTIMAL
                  const htColor = healthTextColor[agent.healthLabel] || healthTextColor.OPTIMAL
                  return (
                    <tr key={agent.id} className="group hover:bg-[#2c2834] transition-colors">
                      <td className="px-6 py-5">
                        <div className="flex items-center gap-3">
                          <div className={`w-2 h-2 rounded-full ${sc.dot}`}></div>
                          <div>
                            <Link to={`/agents/${agent.id}`} className="text-xs font-medium text-[#e7dff0] font-mono hover:text-[#ffb0cf] transition-colors">{agent.id}</Link>
                            <div className="text-[10px] text-zinc-500">{agent.region} {agent.instance ? `• ${agent.instance}` : ''}</div>
                          </div>
                        </div>
                      </td>
                      <td className="px-6 py-5">
                        <span className={`px-2 py-0.5 rounded-full text-[10px] font-semibold ${sc.badge}`}>{agent.status}</span>
                      </td>
                      <td className="px-6 py-5">
                        <div className="w-full max-w-[120px]">
                          <div className="flex justify-between text-[10px] font-mono mb-1">
                            <span className={`text-zinc-400 ${agent.healthLabel === 'CRITICAL' ? 'text-[#ffb4ab] font-semibold' : ''}`}>{agent.healthLabel}</span>
                            <span className={htColor}>{agent.healthPct}%</span>
                          </div>
                          <div className="h-1 w-full bg-[#37333f] rounded-full overflow-hidden">
                            <div className={`h-full ${hColor}`} style={{ width: `${agent.healthPct}%` }}></div>
                          </div>
                        </div>
                      </td>
                      <td className="px-6 py-5">
                        <div className="text-xs font-mono text-[#e7dff0]">{agent.throughput} <span className="text-[10px] text-zinc-500 font-sans">tokens/sec</span></div>
                      </td>
                      <td className={`px-6 py-5 text-xs ${agent.status === 'SUSPENDED' ? 'text-[#ffb4ab] font-medium' : 'text-zinc-400'}`}>
                        {agent.lastAction}
                      </td>
                      <td className="px-6 py-5 text-right">
                        <button className="text-zinc-500 hover:text-[#e7dff0] transition-colors">
                          <span className="material-symbols-outlined">more_vert</span>
                        </button>
                      </td>
                    </tr>
                  )
                })}
              </tbody>
            </table>
          </div>

          {/* Pagination */}
          <div className="mt-6 flex items-center justify-between text-[10px] text-zinc-500 font-medium">
            <div className="flex items-center gap-4">
              <span>Showing 1-{agents.length} of {total} agents</span>
              <div className="flex items-center gap-2">
                <span>Show:</span>
                <select className="bg-[#1d1a25] border-none rounded-sm text-[10px] py-1 pl-2 pr-6 focus:ring-0 text-zinc-400">
                  <option>15</option>
                  <option>50</option>
                  <option>100</option>
                </select>
              </div>
            </div>
            <div className="flex gap-2">
              <button
                onClick={() => setPage(p => Math.max(1, p - 1))}
                disabled={page === 1}
                className="w-8 h-8 flex items-center justify-center bg-[#1d1a25] hover:bg-[#211e29] transition-colors rounded-sm border border-[#574149]/10 text-zinc-400 disabled:cursor-not-allowed disabled:opacity-50"
              >
                <span className="material-symbols-outlined text-xs">chevron_left</span>
              </button>
              {[1, 2, 3].map(p => (
                <button
                  key={p}
                  onClick={() => setPage(p)}
                  className={`w-8 h-8 flex items-center justify-center rounded-sm border text-[10px] font-semibold transition-colors ${
                    page === p
                      ? 'bg-[#1d1a25] border-[#574149]/10 text-[#e7dff0]'
                      : 'bg-transparent border-transparent text-zinc-500 hover:bg-[#2c2834]'
                  }`}
                >
                  {p}
                </button>
              ))}
              <button
                onClick={() => setPage(p => p + 1)}
                className="w-8 h-8 flex items-center justify-center bg-[#1d1a25] hover:bg-[#2c2834] transition-colors rounded-sm border border-[#574149]/10 text-[#e7dff0]"
              >
                <span className="material-symbols-outlined text-xs">chevron_right</span>
              </button>
            </div>
          </div>
        </div>
      </main>
    </div>
  )
}
