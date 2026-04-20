import { useState, useEffect } from 'react'

function fmtTokens(n) {
  if (!n) return '—'
  if (n >= 1_000_000) return (n / 1_000_000).toFixed(1) + 'M'
  if (n >= 1_000) return (n / 1_000).toFixed(1) + 'K'
  return String(n)
}
import { Link } from 'react-router-dom'
import Sidebar from '../components/Sidebar'
import TopNav from '../components/TopNav'
import { api } from '../api/client'

function timeAgo(iso) {
  if (!iso) return '—'
  const ms = Date.now() - new Date(iso).getTime()
  if (ms < 60000) return `${Math.floor(ms / 1000)}s ago`
  if (ms < 3600000) return `${Math.floor(ms / 60000)}m ago`
  if (ms < 86400000) return `${Math.floor(ms / 3600000)}h ago`
  return `${Math.floor(ms / 86400000)}d ago`
}

function AgentTypeLogo({ agentType }) {
  if (agentType === 'claudecode') {
    return (
      <img
        src="/static/icons/claude-seeklogo.svg"
        alt="Claude Code"
        title="Claude Code"
        className="h-4 w-4 shrink-0"
      />
    )
  }
  if (agentType === 'openclaw') {
    return (
      <img
        src="/static/icons/openclaw-dark.svg"
        alt="OpenClaw"
        title="OpenClaw"
        className="h-4 w-4 shrink-0"
      />
    )
  }
  if (agentType === 'hermes') {
    return (
      <img
        src="/static/icons/hermes-logo.png"
        alt="Hermes"
        title="Hermes"
        className="h-4 w-4 shrink-0"
      />
    )
  }
  return null
}

export default function AgentList() {
  const [agents, setAgents] = useState([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState(null)
  const [page, setPage] = useState(1)
  const [total, setTotal] = useState(0)

  useEffect(() => {
    setError(null)
    api.listAgents(page, 15)
      .then(data => {
        if (data.agents) {
          setAgents(data.agents)
          setTotal(data.total || 0)
        }
      })
      .catch(err => setError(err?.message || 'Failed to load agents'))
      .finally(() => setLoading(false))
  }, [page])

  return (
    <div className="min-h-screen bg-[#15121d] text-[#e7dff0]">
      <Sidebar />
      <TopNav />

      <main className="pl-sidebar pt-16 pb-8 min-h-screen">
        <div className="p-8 max-w-7xl w-full mx-auto">
          {/* Header */}
          <div className="flex flex-col md:flex-row md:items-end justify-between mb-10 gap-4">
            <div>
              <h2 className="text-3xl font-medium text-[#e7dff0] tracking-tight mb-2">Agent Fleet</h2>
              <p className="text-zinc-500 text-xs max-w-lg">
                Manage and monitor your active agents.
              </p>
            </div>
            <div className="flex gap-3">
              <button className="px-4 py-2 bg-[#2c2834] text-[#e7dff0] text-xs font-medium rounded-sm border border-[#574149]/10 hover:bg-[#37333f] transition-colors flex items-center gap-2">
                <span className="material-symbols-outlined text-xs">filter_list</span>
                Filters
              </button>
              <Link to="/onboarding" className="px-4 py-2 bg-primary-button text-primary-button-text text-xs font-semibold rounded-sm flex items-center gap-2 hover:scale-[0.98] transition-transform">
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
                  <th className="px-6 py-4">Deployment</th>
                  <th className="px-6 py-4">Status</th>
                  <th className="px-6 py-4">Events</th>
                  <th className="px-6 py-4">Sessions</th>
                  <th className="px-6 py-4">Tokens</th>
                  <th className="px-6 py-4">LLM Calls</th>
                  <th className="px-6 py-4">Last Seen</th>
                </tr>
              </thead>
              <tbody className="divide-y divide-[#574149]/10">
                {loading ? (
                  <tr>
                    <td colSpan={8} className="px-6 py-12 text-center text-zinc-500">
                      <span className="material-symbols-outlined animate-spin text-[#ffb0cf]">refresh</span>
                    </td>
                  </tr>
                ) : error ? (
                  <tr>
                    <td colSpan={8} className="px-6 py-12 text-center">
                      <div className="flex flex-col items-center gap-2">
                        <span className="material-symbols-outlined text-[#ffb4ab] text-3xl">error</span>
                        <p className="text-xs text-[#ffb4ab]">{error}</p>
                      </div>
                    </td>
                  </tr>
                ) : agents.length === 0 ? (
                  <tr>
                    <td colSpan={8} className="px-6 py-12 text-center text-zinc-500 text-xs">
                      No agents found. Connect an agent to start monitoring.
                    </td>
                  </tr>
                ) : agents.map(agent => {
                  const compositeId = `${agent.deployment_id}__${agent.agent_id}`
                  return (
                    <tr key={compositeId} className="group hover:bg-[#2c2834] transition-colors">
                      <td className="px-6 py-5">
                        <div className="flex items-center gap-2">
                          <AgentTypeLogo agentType={agent.agent_type} />
                          <Link to={`/agents/${compositeId}`} className="text-xs font-medium text-[#e7dff0] font-mono hover:text-[#ffb0cf] transition-colors">{agent.agent_id}</Link>
                        </div>
                        <div className="text-[10px] text-zinc-500 mt-0.5">{agent.deployment_name}</div>
                      </td>
                      <td className="px-6 py-5">
                        <div className="text-xs font-mono text-[#e7dff0]">{agent.deployment_id}</div>
                        <div className="text-[10px] text-zinc-500">{agent.deployment_name}</div>
                      </td>
                      <td className="px-6 py-5">
                        <span className={`inline-flex items-center gap-1.5 px-2 py-0.5 rounded-full text-[10px] font-medium ${
                          agent.status === 'active'
                            ? 'bg-emerald-500/10 text-emerald-400'
                            : 'bg-zinc-700/30 text-zinc-500'
                        }`}>
                          <span className={`w-1.5 h-1.5 rounded-full ${agent.status === 'active' ? 'bg-emerald-400' : 'bg-zinc-600'}`} />
                          {agent.status === 'active' ? 'Active' : 'Idle'}
                        </span>
                      </td>
                      <td className="px-6 py-5">
                        <div className="text-xs font-mono text-[#e7dff0]">{agent.event_count?.toLocaleString()}</div>
                      </td>
                      <td className="px-6 py-5">
                        <div className="text-xs font-mono text-[#e7dff0]">{agent.session_count?.toLocaleString()}</div>
                      </td>
                      <td className="px-6 py-5">
                        <div className="text-xs font-mono text-[#e7dff0]">{fmtTokens(agent.total_tokens)}</div>
                      </td>
                      <td className="px-6 py-5">
                        <div className="text-xs font-mono text-[#e7dff0]">{agent.llm_call_count?.toLocaleString() ?? '—'}</div>
                      </td>
                      <td className="px-6 py-5 text-xs text-zinc-400">
                        {timeAgo(agent.last_seen)}
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
              <span>Showing {agents.length} of {total} agents</span>
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
                  className={`w-8 h-8 flex items-center justify-center rounded-sm border text-[10px] font-semibold transition-colors ${page === p
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
