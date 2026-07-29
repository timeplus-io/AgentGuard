import { useState, useEffect } from 'react'
import { useTranslation } from 'react-i18next'

function fmtTokens(n) {
  if (!n) return '—'
  return formatNumber(n, { notation: 'compact', maximumFractionDigits: 1 })
}
import { Link } from 'react-router-dom'
import Sidebar from '../components/Sidebar'
import TopNav from '../components/TopNav'
import { api } from '../api/client'
import { formatNumber, formatRelativeTime } from '../i18n/format'

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
        src="/static/icons/nousresearch-dark.png"
        alt="Hermes"
        title="Hermes"
        className="h-4 w-4 shrink-0"
      />
    )
  }
  return null
}

export default function AgentList() {
  const { t } = useTranslation('agents')
  const [agents, setAgents] = useState([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState(null)
  const [page, setPage] = useState(1)
  const [limit, setLimit] = useState(15)
  const [total, setTotal] = useState(0)

  useEffect(() => {
    setError(null)
    api.listAgents(page, limit)
      .then(data => {
        if (data.agents) {
          setAgents(data.agents)
          setTotal(data.total || 0)
        }
      })
      .catch(err => setError(err?.message || t('list.error.loadFailed')))
      .finally(() => setLoading(false))
  }, [page, limit, t])

  return (
    <div className="min-h-screen bg-surface text-gray-100">
      <Sidebar />
      <TopNav />

      <main className="pl-sidebar pt-16 pb-8 min-h-screen">
        <div className="p-8 max-w-7xl w-full mx-auto">
          {/* Header */}
          <div className="flex flex-col md:flex-row md:items-end justify-between mb-10 gap-4">
            <div>
              <h2 className="text-3xl font-medium text-gray-100 tracking-tight mb-2">{t('list.title')}</h2>
              <p className="text-gray-400 text-xs max-w-lg">
                {t('list.subtitle')}
              </p>
            </div>
            <div className="flex gap-3">
              <button className="px-4 py-2 bg-surface-container-high text-gray-100 text-xs font-medium rounded-sm border border-outline-variant/10 hover:bg-surface-container-highest transition-colors flex items-center gap-2">
                <span className="material-symbols-outlined text-xs">filter_list</span>
                {t('list.filters')}
              </button>
              <Link to="/onboarding" className="px-4 py-2 bg-primary-button text-primary-button-text text-xs font-semibold rounded-sm flex items-center gap-2 hover:scale-[0.98] transition-transform">
                <span className="material-symbols-outlined text-xs">cable</span>
                {t('list.connectAgents')}
              </Link>
            </div>
          </div>

          {/* Table */}
          <div className="bg-surface-container-low rounded-sm overflow-hidden">
            <table className="w-full text-left border-collapse">
              <thead>
                <tr className="bg-surface-container text-[10px] font-semibold uppercase tracking-[0.15em] text-gray-400">
                  <th className="px-6 py-4">{t('list.columns.agentId')}</th>
                  <th className="px-6 py-4">{t('list.columns.deployment')}</th>
                  <th className="px-6 py-4">{t('list.columns.status')}</th>
                  <th className="px-6 py-4">{t('list.columns.events')}</th>
                  <th className="px-6 py-4">{t('list.columns.sessions')}</th>
                  <th className="px-6 py-4">{t('list.columns.tokens')}</th>
                  <th className="px-6 py-4">{t('list.columns.llmCalls')}</th>
                  <th className="px-6 py-4">{t('list.columns.lastSeen')}</th>
                </tr>
              </thead>
              <tbody className="divide-y divide-outline-variant/10">
                {loading ? (
                  <tr>
                    <td colSpan={8} className="px-6 py-12 text-center text-gray-400">
                      <span className="material-symbols-outlined animate-spin text-primary">refresh</span>
                    </td>
                  </tr>
                ) : error ? (
                  <tr>
                    <td colSpan={8} className="px-6 py-12 text-center">
                      <div className="flex flex-col items-center gap-2">
                        <span className="material-symbols-outlined text-error text-3xl">error</span>
                        <p className="text-xs text-error">{error}</p>
                      </div>
                    </td>
                  </tr>
                ) : agents.length === 0 ? (
                  <tr>
                    <td colSpan={8} className="px-6 py-12 text-center text-gray-400 text-xs">
                      {t('list.empty')}
                    </td>
                  </tr>
                ) : agents.map(agent => {
                  const compositeId = `${agent.deployment_id}__${agent.agent_id}`
                  return (
                    <tr key={compositeId} className="group hover:bg-surface-container-high transition-colors">
                      <td className="px-6 py-5">
                        <div className="flex items-center gap-2">
                          <AgentTypeLogo agentType={agent.agent_type} />
                          <Link to={`/agents/${compositeId}`} className="text-xs font-medium text-gray-100 font-mono hover:text-primary transition-colors">{agent.agent_id}</Link>
                        </div>
                        <div className="text-[10px] text-gray-400 mt-0.5">{agent.deployment_name}</div>
                      </td>
                      <td className="px-6 py-5">
                        <div className="text-xs font-mono text-gray-100">{agent.deployment_id}</div>
                        <div className="text-[10px] text-gray-400">{agent.deployment_name}</div>
                      </td>
                      <td className="px-6 py-5">
                        <span className={`inline-flex items-center gap-1.5 px-2 py-0.5 rounded-full text-[10px] font-medium ${
                          agent.status === 'active'
                            ? 'bg-emerald-500/10 text-emerald-400'
                            : 'bg-zinc-700/30 text-gray-400'
                        }`}>
                          <span className={`w-1.5 h-1.5 rounded-full ${agent.status === 'active' ? 'bg-emerald-400' : 'bg-zinc-600'}`} />
                          {agent.status === 'active' ? t('list.status.active') : t('list.status.idle')}
                        </span>
                      </td>
                      <td className="px-6 py-5">
                        <div className="text-xs font-mono text-gray-100">{agent.event_count != null ? formatNumber(agent.event_count) : ''}</div>
                      </td>
                      <td className="px-6 py-5">
                        <div className="text-xs font-mono text-gray-100">{agent.session_count != null ? formatNumber(agent.session_count) : ''}</div>
                      </td>
                      <td className="px-6 py-5">
                        <div className="text-xs font-mono text-gray-100">{fmtTokens(agent.total_tokens)}</div>
                      </td>
                      <td className="px-6 py-5">
                        <div className="text-xs font-mono text-gray-100">{agent.llm_call_count != null ? formatNumber(agent.llm_call_count) : '—'}</div>
                      </td>
                      <td className="px-6 py-5 text-xs text-gray-400">
                        {agent.last_seen ? formatRelativeTime(agent.last_seen) : '—'}
                      </td>
                    </tr>
                  )
                })}
              </tbody>
            </table>
          </div>

          {/* Pagination */}
          <div className="mt-6 flex items-center justify-between text-[10px] text-gray-400 font-medium">
            <div className="flex items-center gap-4">
              <span>{t('list.pagination.showing', { shown: agents.length, total })}</span>
              <div className="flex items-center gap-2">
                <span>{t('list.pagination.show')}</span>
                <select value={limit} onChange={e => { setLimit(Number(e.target.value)); setPage(1) }} className="bg-gray-800 border border-gray-600 rounded-sm text-[10px] py-1 pl-2 pr-6 focus:border-gray-400 focus:outline-none text-gray-400">
                  <option value={15}>15</option>
                  <option value={50}>50</option>
                  <option value={100}>100</option>
                </select>
              </div>
            </div>
            <div className="flex gap-2">
              <button
                onClick={() => setPage(p => Math.max(1, p - 1))}
                disabled={page === 1}
                className="w-8 h-8 flex items-center justify-center bg-surface-container-low hover:bg-surface-container transition-colors rounded-sm border border-outline-variant/10 text-gray-400 disabled:cursor-not-allowed disabled:opacity-50"
              >
                <span className="material-symbols-outlined text-xs">chevron_left</span>
              </button>
              {Array.from({ length: Math.ceil(total / limit) }, (_, i) => i + 1).map(p => (
                <button
                  key={p}
                  onClick={() => setPage(p)}
                  className={`w-8 h-8 flex items-center justify-center rounded-sm border text-[10px] font-semibold transition-colors ${page === p
                      ? 'bg-surface-container-low border-outline-variant/10 text-gray-100'
                      : 'bg-transparent border-transparent text-gray-400 hover:bg-surface-container-high'
                    }`}
                >
                  {p}
                </button>
              ))}
              <button
                onClick={() => setPage(p => p + 1)}
                disabled={page >= Math.ceil(total / limit)}
                className="w-8 h-8 flex items-center justify-center bg-surface-container-low hover:bg-surface-container-high transition-colors rounded-sm border border-outline-variant/10 text-gray-100 disabled:cursor-not-allowed disabled:opacity-50"
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
