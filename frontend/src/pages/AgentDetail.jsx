import { useState, useEffect, useCallback } from 'react'
import { useParams, useSearchParams, Link } from 'react-router-dom'
import Sidebar from '../components/Sidebar'
import Footer from '../components/Footer'
import { api } from '../api/client'

// ── helpers ───────────────────────────────────────────────────────────────────

function fmtTokens(n) {
  if (n == null) return '—'
  if (n >= 1_000_000) return (n / 1_000_000).toFixed(1) + 'M'
  if (n >= 1_000) return (n / 1_000).toFixed(1) + 'K'
  return String(n)
}

function fmtLatency(ms) {
  if (ms == null || ms === 0) return '—'
  if (ms >= 1000) return (ms / 1000).toFixed(1) + 's'
  return Math.round(ms) + ' ms'
}

function fmtPct(rate) {
  if (rate == null) return '—'
  return (rate * 100).toFixed(1) + '%'
}

function fmtTime(iso) {
  if (!iso) return ''
  const d = new Date(iso)
  const yyyy = d.getUTCFullYear()
  const mo = String(d.getUTCMonth() + 1).padStart(2, '0')
  const dd = String(d.getUTCDate()).padStart(2, '0')
  const hh = String(d.getUTCHours()).padStart(2, '0')
  const mm = String(d.getUTCMinutes()).padStart(2, '0')
  const ss = String(d.getUTCSeconds()).padStart(2, '0')
  return `${yyyy}-${mo}-${dd} ${hh}:${mm}:${ss} UTC`
}

function dotColor(event) {
  if (event.hook_decision === 'block') return '#ffb4ab'
  const n = event.hook_name
  if (n === 'llm_input' || n === 'llm_output') return '#ffb0cf'
  if (n === 'before_tool_call' || n === 'after_tool_call' || n === 'tool_result_persist') return '#93c5fd'
  if (n === 'before_agent_start' || n === 'agent_end') return '#a1a1aa'
  return '#52525b'
}

function sessionTypeIcon(type) {
  switch (type) {
    case 'Web UI':    return 'language'
    case 'HTTP API':  return 'api'
    case 'Cron':      return 'schedule'
    case 'Sub-agent': return 'account_tree'
    default:          return 'help_outline'
  }
}

function safeParse(str) {
  try { return JSON.parse(str) } catch { return null }
}

const TIME_PRESETS = [
  { value: '1h',  label: 'Last 1h' },
  { value: '6h',  label: 'Last 6h' },
  { value: '24h', label: 'Last 24h' },
  { value: '7d',  label: 'Last 7 days' },
  { value: '30d', label: 'Last 30 days' },
  { value: 'all', label: 'All time' },
]

function presetToISO(preset) {
  if (!preset || preset === 'all') return null
  const m = preset.match(/^(\d+)([hd])$/)
  if (!m) return null
  const secs = parseInt(m[1]) * (m[2] === 'h' ? 3600 : 86400)
  return new Date(Date.now() - secs * 1000).toISOString()
}

// ── EventDetail: structured detail panel per hook type ────────────────────────

function KVRow({ label, value }) {
  if (value == null || value === '') return null
  return (
    <div className="grid grid-cols-[130px_1fr] gap-2 py-1 border-b border-[#574149]/10 last:border-0">
      <span className="text-[10px] text-zinc-500 uppercase tracking-wider">{label}</span>
      <span className="text-xs font-mono text-[#e7dff0] break-all">{String(value)}</span>
    </div>
  )
}

function EventDetail({ event }) {
  const data = safeParse(event.event_data) || {}
  const hook = event.hook_name
  const isBlock = event.hook_decision === 'block'

  if (isBlock) {
    const rule = data.rule ?? data.ruleName ?? data.rule_name ?? data.ruleId ?? ''
    return (
      <div className="mt-2 bg-[#0f0c17] p-3 rounded-sm border border-[#ffb4ab]/20 space-y-0.5">
        <KVRow label="Block Reason" value={event.block_reason} />
        <KVRow label="Rule" value={rule} />
      </div>
    )
  }

  if (hook === 'llm_input') {
    const msgCount = Array.isArray(data.messages) ? data.messages.length : (data.messageCount ?? data.message_count ?? '')
    return (
      <div className="mt-2 bg-[#0f0c17] p-3 rounded-sm border border-[#574149]/20 space-y-0.5">
        <KVRow label="Provider" value={event.provider} />
        <KVRow label="Model" value={event.model} />
        <KVRow label="Run ID" value={event.run_id} />
        <KVRow label="Session ID" value={event.session_id} />
        {msgCount !== '' && <KVRow label="Messages" value={msgCount} />}
      </div>
    )
  }

  if (hook === 'llm_output') {
    const finish = data.finishReason ?? data.finish_reason ?? data.stop_reason ?? ''
    return (
      <div className="mt-2 bg-[#0f0c17] p-3 rounded-sm border border-[#574149]/20 space-y-0.5">
        <KVRow label="Provider" value={event.provider} />
        <KVRow label="Model" value={event.model} />
        <KVRow label="Run ID" value={event.run_id} />
        <KVRow label="Finish Reason" value={finish} />
      </div>
    )
  }

  if (hook === 'before_tool_call' || hook === 'after_tool_call') {
    const input = data.input ?? data.tool_input ?? data.args ?? {}
    return (
      <div className="mt-2 bg-[#0f0c17] p-3 rounded-sm border border-[#574149]/20 space-y-0.5">
        <KVRow label="Tool Name" value={event.tool_name} />
        <KVRow label="Run ID" value={event.run_id} />
        {typeof input === 'object'
          ? Object.entries(input).map(([k, v]) => (
              <KVRow key={k} label={k} value={typeof v === 'object' ? JSON.stringify(v) : v} />
            ))
          : <KVRow label="Input" value={input} />
        }
      </div>
    )
  }

  if (hook === 'tool_result_persist') {
    const raw = data.result ?? data.output ?? data.content ?? JSON.stringify(data)
    const preview = String(raw).slice(0, 200)
    return (
      <div className="mt-2 bg-[#0f0c17] p-3 rounded-sm border border-[#574149]/20 space-y-0.5">
        <KVRow label="Tool Name" value={event.tool_name} />
        <KVRow label="Result" value={preview + (String(raw).length > 200 ? '…' : '')} />
      </div>
    )
  }

  if (hook === 'before_agent_start' || hook === 'agent_end') {
    const duration = data.duration_ms ?? data.duration ?? data.elapsed_ms ?? data.elapsed ?? ''
    return (
      <div className="mt-2 bg-[#0f0c17] p-3 rounded-sm border border-[#574149]/20 space-y-0.5">
        <KVRow label="Run ID" value={event.run_id} />
        <KVRow label="Session ID" value={event.session_id} />
        {duration !== '' && <KVRow label="Duration" value={`${duration}ms`} />}
      </div>
    )
  }

  // fallback: all non-empty top-level fields from event_data
  const entries = Object.entries(data).filter(([, v]) => v != null && v !== '' && v !== false)
  return (
    <div className="mt-2 bg-[#0f0c17] p-3 rounded-sm border border-[#574149]/20 space-y-0.5">
      {entries.length === 0
        ? <span className="text-[10px] text-zinc-600">No structured data</span>
        : entries.map(([k, v]) => (
            <KVRow key={k} label={k} value={typeof v === 'object' ? JSON.stringify(v) : v} />
          ))
      }
    </div>
  )
}

// ── EventRow ──────────────────────────────────────────────────────────────────

function EventRow({ event }) {
  const [expanded, setExpanded] = useState(false)
  const color = dotColor(event)
  const subtitle = [event.provider, event.model, event.tool_name, event.hook_decision]
    .filter(Boolean).join(' · ')

  return (
    <div className="group flex gap-4 px-6 py-4 hover:bg-[#2c2834]/50 transition-all border-l-2 border-transparent hover:border-[#574149]/40">
      <div className="flex flex-col items-center pt-1">
        <div className="w-2 h-2 rounded-full flex-shrink-0" style={{ backgroundColor: color }} />
        <div className="w-px flex-1 bg-[#574149]/20 my-2" />
      </div>
      <div className="flex-1 min-w-0">
        <div className="flex justify-between items-start gap-4">
          <span className="text-xs font-medium text-[#e7dff0]">{event.hook_name}</span>
          <span className="text-[10px] text-zinc-500 font-mono shrink-0">{fmtTime(event.event_time)}</span>
        </div>
        {subtitle && <p className="text-[11px] text-zinc-500 mt-0.5">{subtitle}</p>}
        <button
          onClick={() => setExpanded(e => !e)}
          className="mt-1 flex items-center gap-1 text-[10px] text-zinc-500 hover:text-[#ffb0cf] transition-colors"
        >
          <span className="material-symbols-outlined text-[12px]">
            {expanded ? 'expand_less' : 'expand_more'}
          </span>
          Event Details
        </button>
        {expanded && <EventDetail event={event} />}
      </div>
    </div>
  )
}

// ── Main page ─────────────────────────────────────────────────────────────────

export default function AgentDetail() {
  const { id } = useParams()
  const [searchParams, setSearchParams] = useSearchParams()
  const selectedSession = searchParams.get('session')
  const selectedRun = searchParams.get('run_id')

  const [agent, setAgent] = useState(null)
  const [sessions, setSessions] = useState([])
  const [sessionDetail, setSessionDetail] = useState(null)
  const [events, setEvents] = useState([])
  const [eventsTotal, setEventsTotal] = useState(0)
  const [page, setPage] = useState(1)
  const [limit, setLimit] = useState(100)
  const [timePreset, setTimePreset] = useState('24h')

  const [llmMetrics, setLlmMetrics] = useState(null)
  const [toolMetrics, setToolMetrics] = useState(null)

  const [loadingAgent, setLoadingAgent] = useState(true)
  const [loadingSessions, setLoadingSessions] = useState(true)
  const [loadingEvents, setLoadingEvents] = useState(false)
  const [refreshTick, setRefreshTick] = useState(0)

  const refresh = useCallback(() => setRefreshTick(t => t + 1), [])

  useEffect(() => {
    setLoadingAgent(true)
    const params = {}
    const since = presetToISO(timePreset)
    if (since) params.since = since
    api.getAgent(id, params)
      .then(setAgent)
      .catch(() => {})
      .finally(() => setLoadingAgent(false))
  }, [id, timePreset, refreshTick])

  useEffect(() => {
    const params = { agent_id: id }
    const since = presetToISO(timePreset)
    if (since) params.since = since
    api.getLLMMetrics(params)
      .then(setLlmMetrics)
      .catch(() => setLlmMetrics(null))
  }, [id, timePreset, refreshTick])

  useEffect(() => {
    const params = { agent_id: id }
    const since = presetToISO(timePreset)
    if (since) params.since = since
    api.getToolMetrics(params)
      .then(setToolMetrics)
      .catch(() => setToolMetrics(null))
  }, [id, timePreset, refreshTick])

  useEffect(() => {
    setLoadingSessions(true)
    const params = {}
    const since = presetToISO(timePreset)
    if (since) params.since = since
    api.listSessions(id, params)
      .then(setSessions)
      .catch(() => setSessions([]))
      .finally(() => setLoadingSessions(false))
  }, [id, timePreset, refreshTick])

  useEffect(() => {
    if (!selectedSession) { setSessionDetail(null); return }
    api.getSession(id, selectedSession)
      .then(setSessionDetail)
      .catch(() => setSessionDetail(null))
  }, [id, selectedSession, refreshTick])

  useEffect(() => {
    if (!selectedSession && !selectedRun) { setEvents([]); setEventsTotal(0); return }
    const params = { page, limit }
    if (selectedSession) params.session_id = selectedSession
    if (selectedRun) params.run_id = selectedRun
    const since = presetToISO(timePreset)
    if (since) params.since = since

    setLoadingEvents(true)
    api.listEvents(id, params)
      .then(r => { setEvents(r.events ?? []); setEventsTotal(r.total ?? 0) })
      .catch(() => { setEvents([]); setEventsTotal(0) })
      .finally(() => setLoadingEvents(false))
  }, [id, selectedSession, selectedRun, page, limit, timePreset, refreshTick])

  const selectSession = (sid) => {
    setPage(1)
    setSearchParams({ session: sid })
  }

  const selectRun = (rid) => {
    setPage(1)
    const params = {}
    if (selectedSession) params.session = selectedSession
    params.run_id = rid
    setSearchParams(params)
  }

  const isRefreshing = loadingAgent || loadingSessions || loadingEvents
  const totalPages = Math.max(1, Math.ceil(eventsTotal / limit))
  const startEvent = eventsTotal === 0 ? 0 : (page - 1) * limit + 1
  const endEvent = Math.min(page * limit, eventsTotal)

  const volumeMetrics = [
    { label: 'Total Events',    value: agent ? agent.event_count     : '—', icon: 'bolt' },
    { label: 'Total Sessions',  value: agent ? agent.session_count   : '—', icon: 'layers' },
    { label: 'Total Runs',      value: agent ? agent.run_count       : '—', icon: 'sync_alt' },
    { label: 'Total Tool Calls',value: agent ? agent.tool_call_count : '—', icon: 'handyman' },
  ]

  const llmHasCalls = (llmMetrics?.calls?.total ?? 0) > 0
  const toolHasCalls = (toolMetrics?.summary?.total_calls ?? 0) > 0
  const llmSuccessRate = llmMetrics?.calls?.success_rate   // null from backend when no calls
  const toolSuccessRate = toolMetrics?.summary?.success_rate // null from backend when no calls
  const perfMetrics = [
    {
      label: 'Total Tokens',
      value: llmHasCalls ? fmtTokens(llmMetrics.tokens?.total) : '—',
      sub: llmHasCalls ? `↑ ${fmtTokens(llmMetrics.tokens?.input)}  ↓ ${fmtTokens(llmMetrics.tokens?.output)}` : null,
      icon: 'token',
    },
    {
      label: 'LLM Latency p50',
      value: llmHasCalls ? fmtLatency(llmMetrics.latency_ms?.p50) : '—',
      sub: llmHasCalls ? `p95 ${fmtLatency(llmMetrics.latency_ms?.p95)}` : null,
      icon: 'speed',
    },
    {
      label: 'LLM Success Rate',
      value: fmtPct(llmSuccessRate),  // fmtPct(null) → '—'
      sub: llmHasCalls ? `${llmMetrics.calls?.success ?? 0} ok · ${llmMetrics.calls?.failed ?? 0} failed` : null,
      icon: 'check_circle',
      accent: llmSuccessRate != null && llmSuccessRate < 1,
    },
    {
      label: 'Tool Success Rate',
      value: fmtPct(toolSuccessRate),  // fmtPct(null) → '—'
      sub: toolHasCalls ? `${toolMetrics.summary?.success_calls ?? 0} ok · ${toolMetrics.summary?.failed_calls ?? 0} failed` : null,
      icon: 'build_circle',
      accent: toolSuccessRate != null && toolSuccessRate < 1,
    },
  ]

  const agentName = agent?.agent_id ?? id
  const deploymentName = agent?.deployment_name ?? ''

  return (
    <div className="min-h-screen bg-[#15121d] text-[#e7dff0]">
      <Sidebar />
      <Footer />

      {/* TopNav */}
      <header className="fixed top-0 right-0 left-64 h-16 bg-[#15121d]/95 z-50 flex justify-between items-center px-8 backdrop-blur-md shadow-[0_0_12px_rgba(213,63,140,0.12)]">
        <div className="flex items-center gap-4">
          <Link to="/agents" className="text-zinc-400 hover:text-[#ffb0cf] transition-colors">
            <span className="material-symbols-outlined">arrow_back</span>
          </Link>
          <span className="font-medium tracking-tight text-lg text-[#e7dff0]">{agentName}</span>
          {deploymentName && (
            <span className="bg-[#2c2834] text-zinc-400 px-2 py-0.5 rounded-full text-[10px] font-semibold uppercase tracking-tighter">
              {deploymentName}
            </span>
          )}
        </div>
        <div className="flex items-center gap-3">
          <div className="flex items-center gap-2">
            <span className="material-symbols-outlined text-sm text-zinc-500">schedule</span>
            <select
              value={timePreset}
              onChange={e => { setTimePreset(e.target.value); setPage(1) }}
              className="bg-[#2c2834] border border-[#574149]/30 text-xs text-zinc-300 rounded-sm px-2 py-1.5 focus:outline-none focus:border-[#ffb0cf]/40 hover:border-[#574149]/60 transition-colors"
            >
              {TIME_PRESETS.map(p => (
                <option key={p.value} value={p.value}>{p.label}</option>
              ))}
            </select>
          </div>
          <button
            onClick={refresh}
            className="flex items-center gap-2 text-xs text-zinc-400 hover:text-[#ffb0cf] transition-colors px-3 py-1.5 rounded-sm hover:bg-[#2c2834]"
          >
            <span className={`material-symbols-outlined text-base ${isRefreshing ? 'animate-spin' : ''}`}>
              refresh
            </span>
            Refresh
          </button>
        </div>
      </header>

      <main className="pl-sidebar pt-16 h-screen overflow-hidden flex flex-col">
        {/* Volume metric cards */}
        <section className="grid grid-cols-4 gap-4 p-6 pb-3 shrink-0">
          {volumeMetrics.map((m) => (
            <div key={m.label} className="bg-[#2c2834] p-4 rounded relative overflow-hidden group">
              <div className="absolute top-0 right-0 p-2 opacity-10 group-hover:opacity-20 transition-opacity">
                <span className="material-symbols-outlined text-4xl">{m.icon}</span>
              </div>
              <p className="text-[10px] font-medium text-zinc-500 uppercase tracking-widest mb-1">{m.label}</p>
              <h2 className="text-2xl font-medium text-[#e7dff0]">{m.value}</h2>
            </div>
          ))}
        </section>

        {/* Performance metric cards */}
        <section className="grid grid-cols-4 gap-4 px-6 pb-4 shrink-0">
          {perfMetrics.map((m) => (
            <div key={m.label} className="bg-[#211e29] p-4 rounded relative overflow-hidden group border border-[#574149]/10">
              <div className="absolute top-0 right-0 p-2 opacity-10 group-hover:opacity-20 transition-opacity">
                <span className={`material-symbols-outlined text-4xl ${m.accent ? 'text-[#ffb4ab]' : ''}`}>{m.icon}</span>
              </div>
              <p className="text-[10px] font-medium text-zinc-500 uppercase tracking-widest mb-1">{m.label}</p>
              <h2 className={`text-2xl font-medium ${m.accent ? 'text-[#ffb4ab]' : 'text-[#e7dff0]'}`}>{m.value}</h2>
              {m.sub && <p className="text-[10px] text-zinc-600 mt-1 truncate">{m.sub}</p>}
            </div>
          ))}
        </section>

        {/* Sessions + Timeline split */}
        <div className="flex gap-4 px-6 pb-6 flex-1 min-h-0">

          {/* Sessions panel */}
          <aside className="w-52 shrink-0 bg-[#211e29] rounded-sm flex flex-col overflow-hidden">
            <div className="px-4 py-3 border-b border-[#574149]/20 shrink-0">
              <h3 className="text-[10px] font-semibold text-zinc-500 uppercase tracking-widest">Sessions</h3>
            </div>
            <div className="flex-1 overflow-y-auto">
              {loadingSessions ? (
                <div className="flex items-center justify-center py-8">
                  <span className="material-symbols-outlined animate-spin text-zinc-600">refresh</span>
                </div>
              ) : sessions.length === 0 ? (
                <p className="text-[11px] text-zinc-600 px-4 py-4">No sessions found</p>
              ) : (
                sessions.map(session => {
                  const isActive = selectedSession === session.session_id
                  const runs = isActive && sessionDetail?.runs ? sessionDetail.runs : []
                  return (
                    <div key={session.session_id}>
                      <button
                        onClick={() => selectSession(session.session_id)}
                        className={`w-full text-left px-4 py-3 flex items-start gap-2 border-l-2 transition-all hover:bg-[#2c2834]/50 ${
                          isActive ? 'border-[#ffb0cf] bg-[#2c2834]/40' : 'border-transparent'
                        }`}
                      >
                        <span className={`material-symbols-outlined text-base mt-0.5 shrink-0 ${isActive ? 'text-[#ffb0cf]' : 'text-zinc-500'}`}>
                          {sessionTypeIcon(session.session_type)}
                        </span>
                        <div className="min-w-0 flex-1">
                          <div className="flex items-center justify-between gap-1">
                            <span className={`text-xs font-medium truncate ${isActive ? 'text-[#ffb0cf]' : 'text-[#e7dff0]'}`}>
                              {session.session_type}
                            </span>
                            <span className="text-[9px] bg-[#574149]/30 text-zinc-400 px-1.5 py-0.5 rounded-full shrink-0">
                              {session.run_count}
                            </span>
                          </div>
                          <p className="text-[10px] text-zinc-600 truncate mt-0.5">{session.session_key}</p>
                          <p className="text-[9px] text-zinc-700 font-mono mt-0.5">{fmtTime(session.last_seen)}</p>
                        </div>
                      </button>

                      {/* Run list (accordion) */}
                      {isActive && runs.map((run, idx) => {
                        const isRunActive = selectedRun === run.run_id
                        return (
                          <button
                            key={run.run_id}
                            onClick={() => selectRun(run.run_id)}
                            className={`w-full text-left pl-10 pr-4 py-2 flex items-center justify-between border-l-2 transition-all hover:bg-[#2c2834]/60 ${
                              isRunActive ? 'border-[#ffb0cf]/60 bg-[#2c2834]/30' : 'border-transparent'
                            }`}
                          >
                            <div>
                              <span className={`text-[11px] ${isRunActive ? 'text-[#ffb0cf]' : 'text-zinc-400'}`}>
                                Run #{idx + 1}
                              </span>
                              <p className="text-[9px] text-zinc-600 font-mono">{fmtTime(run.start_time)}</p>
                            </div>
                            <div className="flex items-center gap-1">
                              {run.tool_count > 0 && (
                                <span className="text-[9px] bg-[#93c5fd]/10 text-[#93c5fd] px-1.5 py-0.5 rounded-full">
                                  {run.tool_count}
                                </span>
                              )}
                              {run.has_block && (
                                <span className="material-symbols-outlined text-[12px] text-[#ffb4ab]">block</span>
                              )}
                            </div>
                          </button>
                        )
                      })}
                    </div>
                  )
                })
              )}
            </div>
          </aside>

          {/* Timeline panel */}
          <section className="flex-1 bg-[#211e29] rounded-sm flex flex-col min-w-0 overflow-hidden">

            {/* Event list */}
            <div className="flex-1 overflow-y-auto">
              {!selectedSession && !selectedRun ? (
                <div className="flex flex-col items-center justify-center h-full text-zinc-600 gap-2">
                  <span className="material-symbols-outlined text-4xl">timeline</span>
                  <p className="text-sm">Select a session from the left to view its event timeline</p>
                </div>
              ) : loadingEvents ? (
                <div className="flex items-center justify-center h-full">
                  <span className="material-symbols-outlined animate-spin text-zinc-600 text-3xl">refresh</span>
                </div>
              ) : events.length === 0 ? (
                <div className="flex flex-col items-center justify-center h-full text-zinc-600 gap-2">
                  <span className="material-symbols-outlined text-4xl">inbox</span>
                  <p className="text-sm">No events recorded for this session</p>
                </div>
              ) : (
                events.map((event, i) => (
                  <EventRow key={`${event.run_id}-${event.hook_name}-${i}`} event={event} />
                ))
              )}
            </div>

            {/* Pagination */}
            {(selectedSession || selectedRun) && eventsTotal > 0 && (
              <div className="flex items-center justify-between px-6 py-3 border-t border-[#574149]/20 shrink-0">
                <span className="text-[10px] text-zinc-500">
                  Showing {startEvent}–{endEvent} of {eventsTotal} events
                </span>
                <div className="flex items-center gap-3">
                  <select
                    value={limit}
                    onChange={e => { setLimit(Number(e.target.value)); setPage(1) }}
                    className="bg-[#0f0c17] border border-[#574149]/20 text-xs text-zinc-400 rounded-sm px-2 py-1 focus:outline-none"
                  >
                    <option value={25}>25</option>
                    <option value={50}>50</option>
                    <option value={100}>100</option>
                  </select>
                  <button
                    disabled={page <= 1}
                    onClick={() => setPage(p => p - 1)}
                    className="flex items-center gap-1 text-[10px] text-zinc-400 hover:text-[#ffb0cf] disabled:opacity-30 disabled:cursor-not-allowed transition-colors"
                  >
                    <span className="material-symbols-outlined text-sm">chevron_left</span>
                    Prev
                  </button>
                  <span className="text-[10px] text-zinc-600">{page} / {totalPages}</span>
                  <button
                    disabled={page >= totalPages}
                    onClick={() => setPage(p => p + 1)}
                    className="flex items-center gap-1 text-[10px] text-zinc-400 hover:text-[#ffb0cf] disabled:opacity-30 disabled:cursor-not-allowed transition-colors"
                  >
                    Next
                    <span className="material-symbols-outlined text-sm">chevron_right</span>
                  </button>
                </div>
              </div>
            )}
          </section>
        </div>
      </main>
    </div>
  )
}
