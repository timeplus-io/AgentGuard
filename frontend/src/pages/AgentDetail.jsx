import { useState, useEffect, useCallback } from 'react'
import { useTranslation } from 'react-i18next'
import { useParams, useSearchParams, Link } from 'react-router-dom'
import Sidebar from '../components/Sidebar'
import { api } from '../api/client'
import { formatLocalTime } from '../utils/time'
import { formatNumber } from '../i18n/format'

// ── helpers ───────────────────────────────────────────────────────────────────

function fmtTokens(n) {
  if (n == null) return '—'
  return formatNumber(n, { notation: 'compact', maximumFractionDigits: 1 })
}

function fmtLatency(ms) {
  if (ms == null || ms === 0) return '—'
  if (ms >= 1000) return formatNumber(ms / 1000, { minimumFractionDigits: 1, maximumFractionDigits: 1 }) + 's'
  return formatNumber(Math.round(ms)) + ' ms'
}

function fmtPct(rate) {
  if (rate == null) return '—'
  return formatNumber(rate, { style: 'percent', minimumFractionDigits: 1, maximumFractionDigits: 1 })
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

const fmtShortTime = formatLocalTime


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

function getTimePresets(t) {
  return [
    { value: '1h',  label: t('detail.timePresets.1h') },
    { value: '6h',  label: t('detail.timePresets.6h') },
    { value: '24h', label: t('detail.timePresets.24h') },
    { value: '7d',  label: t('detail.timePresets.7d') },
    { value: '30d', label: t('detail.timePresets.30d') },
    { value: 'all', label: t('detail.timePresets.all') },
  ]
}

function presetToISO(preset) {
  if (!preset || preset === 'all') return null
  const m = preset.match(/^(\d+)([hd])$/)
  if (!m) return null
  const secs = parseInt(m[1]) * (m[2] === 'h' ? 3600 : 86400)
  return new Date(Date.now() - secs * 1000).toISOString()
}

// ── KVRow ─────────────────────────────────────────────────────────────────────

function KVRow({ label, value }) {
  if (value == null || value === '') return null
  return (
    <div className="grid grid-cols-[130px_1fr] gap-2 py-1 border-b border-outline-variant/10 last:border-0">
      <span className="text-[10px] text-gray-400 uppercase tracking-wider">{label}</span>
      <span className="text-xs font-mono text-gray-100 break-all">{String(value)}</span>
    </div>
  )
}

// ── SessionSummary ────────────────────────────────────────────────────────────

function SessionSummary({ sessionDetail }) {
  const { t } = useTranslation('agents')
  const isEnded = sessionDetail.status === 'ended'
  const runs = sessionDetail.runs ?? []
  const startedAt = runs[0]?.start_time ?? null
  const endedAt = isEnded ? (runs[runs.length - 1]?.end_time ?? null) : null

  return (
    <div className="flex flex-col items-center justify-center h-full p-8">
      <div className="w-full max-w-lg bg-surface-container-low rounded-sm p-6 space-y-6">
        {/* Session ID */}
        <div>
          <p className="text-[10px] font-semibold text-gray-400 uppercase tracking-widest mb-1.5">{t('detail.session.id')}</p>
          <code className="text-[13px] font-mono text-gray-100 break-all leading-relaxed">{sessionDetail.session_id}</code>
        </div>

        {/* Status */}
        <div className="flex items-center gap-2">
          <span className={`w-2 h-2 rounded-full ${isEnded ? 'bg-zinc-500' : 'bg-emerald-400 animate-pulse'}`} />
          <span className={`text-sm font-semibold ${isEnded ? 'text-gray-400' : 'text-emerald-400'}`}>
            {isEnded ? t('detail.session.status.ended') : t('detail.session.status.active')}
          </span>
          {sessionDetail.session_type && (
            <span className="ml-2 text-[10px] text-gray-400 bg-surface-container-high px-2 py-0.5 rounded-full uppercase tracking-wider">
              {sessionDetail.session_type}
            </span>
          )}
        </div>

        {/* Model / Provider */}
        {(sessionDetail.model || sessionDetail.provider) && (
          <div className="flex items-center gap-3 px-3 py-2 bg-surface-container-lowest rounded-sm">
            <span className="material-symbols-outlined text-[13px] text-gray-400">smart_toy</span>
            {sessionDetail.provider && (
              <span className="text-[10px] text-gray-400 font-mono">{sessionDetail.provider}</span>
            )}
            {sessionDetail.model && (
              <span className="text-xs font-mono text-primary">{sessionDetail.model}</span>
            )}
          </div>
        )}

        {/* Stats grid */}
        <div className="grid grid-cols-2 gap-4">
          <div className="bg-surface-container-lowest rounded-sm px-4 py-3">
            <p className="text-[10px] text-gray-400 uppercase tracking-widest mb-1">{t('detail.session.started')}</p>
            <p className="text-xs font-mono text-gray-100">{startedAt ? fmtTime(startedAt) : '—'}</p>
          </div>
          <div className="bg-surface-container-lowest rounded-sm px-4 py-3">
            <p className="text-[10px] text-gray-400 uppercase tracking-widest mb-1">{t('detail.session.ended')}</p>
            <p className="text-xs font-mono text-gray-100">{endedAt ? fmtTime(endedAt) : '—'}</p>
          </div>
          <div className="bg-surface-container-lowest rounded-sm px-4 py-3">
            <p className="text-[10px] text-gray-400 uppercase tracking-widest mb-1">{t('detail.session.totalEvents')}</p>
            <p className="text-2xl font-bold text-gray-100">{sessionDetail.event_count ?? 0}</p>
          </div>
          <div className="bg-surface-container-lowest rounded-sm px-4 py-3">
            <p className="text-[10px] text-gray-400 uppercase tracking-widest mb-1">{t('detail.session.toolCalls')}</p>
            <p className="text-2xl font-bold text-gray-100">{sessionDetail.total_tool_calls ?? 0}</p>
          </div>
        </div>

        <p className="text-[11px] text-gray-400 text-center">{t('detail.session.selectRunHint')}</p>
      </div>
    </div>
  )
}

// ── SessionHeader ─────────────────────────────────────────────────────────────

function SessionHeader({ sessionDetail }) {
  const { t } = useTranslation('agents')
  const isEnded = sessionDetail.status === 'ended'
  const sid = sessionDetail.session_id ?? ''
  return (
    <div className="px-5 py-3 border-b border-outline-variant/20 flex flex-wrap items-center gap-x-4 gap-y-1 shrink-0 bg-surface-container-low">
      <code className="text-[11px] font-mono text-gray-100 break-all">{sid}</code>
      <span className={`flex items-center gap-1.5 text-[10px] font-semibold ${isEnded ? 'text-gray-400' : 'text-emerald-400'}`}>
        <span className={`w-1.5 h-1.5 rounded-full ${isEnded ? 'bg-zinc-500' : 'bg-emerald-400 animate-pulse'}`} />
        {isEnded ? t('detail.session.status.ended') : t('detail.session.status.active')}
      </span>
      <span className="text-[10px] text-gray-400">{t('detail.session.eventsCount', { count: sessionDetail.event_count ?? 0 })}</span>
      <span className="text-[10px] text-gray-400">{t('detail.session.toolCallsCount', { count: sessionDetail.total_tool_calls ?? 0 })}</span>
    </div>
  )
}

// ── JsonValue ─────────────────────────────────────────────────────────────────
// Recursive collapsible JSON tree. Nested objects/arrays start collapsed.

function JsonValue({ value, depth }) {
  const [open, setOpen] = useState(depth === 0)

  if (value === null) return <span className="text-gray-400">null</span>
  if (typeof value === 'boolean') return <span className="text-purple-400">{String(value)}</span>
  if (typeof value === 'number') return <span className="text-amber-400">{value}</span>
  if (typeof value === 'string') {
    return (
      <span className="text-green-400 break-all">
        &quot;{value.length > 300 ? value.slice(0, 300) + '…' : value}&quot;
      </span>
    )
  }

  const isArr = Array.isArray(value)
  const entries = isArr ? value.map((v, i) => [String(i), v]) : Object.entries(value)
  const [ob, cb] = isArr ? ['[', ']'] : ['{', '}']

  if (entries.length === 0) return <span className="text-gray-400">{ob}{cb}</span>

  return (
    <span>
      <button
        onClick={() => setOpen(o => !o)}
        className="text-gray-400 hover:text-gray-100 transition-colors mr-1"
      >
        {open ? '▾' : '▸'}
      </button>
      {open ? (
        <>
          <span className="text-gray-400">{ob}</span>
          <div className="pl-4 border-l border-outline-variant/20 ml-0.5 mt-0.5 space-y-0.5">
            {entries.map(([k, v]) => (
              <div key={k} className="flex gap-1.5 items-start">
                <span className="text-info shrink-0 select-none">
                  {isArr ? k : `"${k}"`}:
                </span>
                <JsonValue value={v} depth={depth + 1} />
              </div>
            ))}
          </div>
          <span className="text-gray-400">{cb}</span>
        </>
      ) : (
        <button
          onClick={() => setOpen(true)}
          className="text-gray-400 hover:text-gray-400 transition-colors"
        >
          {ob}&hellip;{cb}
        </button>
      )}
    </span>
  )
}

// ── RunEventCard ──────────────────────────────────────────────────────────────

const hookBadgeColor = {
  llm_input:            'text-primary bg-[#ffb0cf]/10',
  llm_output:           'text-primary bg-[#ffb0cf]/10',
  pre_llm_call:         'text-primary bg-[#ffb0cf]/10',
  post_llm_call:        'text-primary bg-[#ffb0cf]/10',
  pre_api_request:      'text-primary bg-[#ffb0cf]/10',
  post_api_request:     'text-primary bg-[#ffb0cf]/10',
  before_tool_call:     'text-info bg-info/10',
  after_tool_call:      'text-info bg-info/10',
  tool_result_persist:  'text-info bg-info/10',
  pre_tool_call:        'text-info bg-info/10',
  post_tool_call:       'text-info bg-info/10',
  session_start:        'text-gray-400  bg-zinc-700/30',
  session_end:          'text-gray-400  bg-zinc-700/30',
  on_session_start:     'text-gray-400  bg-zinc-700/30',
  on_session_end:       'text-gray-400  bg-zinc-700/30',
  on_session_finalize:  'text-gray-400  bg-zinc-700/30',
  on_session_reset:     'text-gray-400  bg-zinc-700/30',
}

function RunEventCard({ event }) {
  const { t } = useTranslation('agents')
  const [expanded, setExpanded] = useState(false)

  let parsedData = null
  if (event.event_data) {
    try { parsedData = JSON.parse(event.event_data) } catch { /* use raw */ }
  }

  const isBlocked = event.hook_decision === 'block'
  const badgeCls = hookBadgeColor[event.hook_name] ?? 'text-gray-400 bg-zinc-700/20'

  return (
    <div className={`rounded-sm border transition-colors ${isBlocked ? 'border-[#ffb4ab]/30 bg-[#93000a]/10' : 'border-outline-variant/10 bg-surface-container-low'}`}>
      {/* Summary row */}
      <div
        className="flex items-center gap-2.5 px-4 py-2.5 cursor-pointer select-none"
        onClick={() => setExpanded(e => !e)}
      >
        <span className={`material-symbols-outlined text-[13px] text-gray-400 shrink-0 transition-transform ${expanded ? 'rotate-90' : ''}`}>
          chevron_right
        </span>
        <span className="text-[10px] font-mono text-gray-400 whitespace-nowrap shrink-0 tabular-nums">
          {fmtShortTime(event.event_time)}
        </span>
        <span className={`text-[10px] font-semibold uppercase rounded-full px-2 py-0.5 tracking-wide shrink-0 ${badgeCls}`}>
          {event.hook_name}
        </span>
        {event.tool_name && (
          <span className="text-[11px] font-mono text-info shrink-0">{event.tool_name}</span>
        )}
        {(event.model || event.provider) && (
          <span className="text-[10px] text-gray-400 shrink-0 hidden sm:inline">
            {[event.provider, event.model].filter(Boolean).join(' · ')}
          </span>
        )}
        <span className="flex-1" />
        {isBlocked && (
          <span className="flex items-center gap-1 text-[10px] font-semibold text-error shrink-0">
            <span className="material-symbols-outlined text-[13px]">block</span>
            {t('detail.events.blocked')}
          </span>
        )}
        {event.event_data && (
          <span className="text-[10px] text-gray-400 shrink-0">{t('detail.events.jsonBadge')}</span>
        )}
      </div>

      {/* Expanded: JSON tree */}
      {expanded && (
        <div className="px-4 pb-3 pt-1 border-t border-outline-variant/10">
          {event.block_reason && (
            <p className="text-[11px] text-error mb-2 font-mono">{t('detail.events.blockReason', { reason: event.block_reason })}</p>
          )}
          {parsedData !== null ? (
            <div className="text-[11px] font-mono text-gray-100 leading-relaxed">
              <JsonValue value={parsedData} depth={0} />
            </div>
          ) : event.event_data ? (
            <pre className="text-[11px] font-mono text-gray-400 whitespace-pre-wrap break-all">
              {event.event_data}
            </pre>
          ) : (
            <p className="text-[11px] text-gray-400">{t('detail.events.noEventData')}</p>
          )}
        </div>
      )}
    </div>
  )
}

// ── SkillsTab ─────────────────────────────────────────────────────────────────

const SOURCE_TYPE_COLORS = {
  workspace: 'text-red-400',
  managed:   'text-emerald-400',
  personal:  'text-blue-400',
  bundled:   'text-gray-400',
  plugin:    'text-purple-400',
  user:      'text-gray-400',
  unknown:   'text-gray-400',
}

function SkillsTab({ sessionId }) {
  const { t } = useTranslation('agents')
  const [loaded, setLoaded] = useState([])
  const [invoked, setInvoked] = useState([])
  const [connecting, setConnecting] = useState(true)
  const [error, setError] = useState(null)
  const [expanded, setExpanded] = useState({})

  useEffect(() => {
    if (!sessionId) return
    if (!/^[a-zA-Z0-9_-]{1,128}$/.test(sessionId)) return
    setLoaded([])
    setInvoked([])
    setConnecting(true)
    setError(null)
    setExpanded({})
    const ctrl = new AbortController()
    const sql = `SELECT event_time, skill_name, skill_action, skill_namespace, skill_source_type, skill_location, skill_description, skill_args FROM agentguard_skills WHERE session_id = '${sessionId}' ORDER BY event_time ASC SETTINGS seek_to = 'earliest'`
    async function stream() {
      for await (const row of api.protonQuery(sql, { signal: ctrl.signal })) {
        if (ctrl.signal.aborted) break
        setConnecting(false)
        if (row.skill_action === 'loaded') {
          setLoaded(prev => prev.some(s => s.name === row.skill_name) ? prev : [...prev, {
            name: row.skill_name,
            firstSeen: row.event_time,
            sourceType: row.skill_source_type || 'unknown',
            location: row.skill_location || '',
            description: row.skill_description || '',
          }])
        } else if (row.skill_action === 'invoked') {
          setInvoked(prev => [...prev, {
            time: row.event_time,
            name: row.skill_name,
            args: row.skill_args || '',
          }])
        }
      }
      setConnecting(false)
    }
    stream().catch(e => { if (e?.name !== 'AbortError') setError(e.message || t('detail.skills.streamError')) })
    return () => ctrl.abort()
  }, [sessionId, t])

  if (connecting) {
    return (
      <div className="flex items-center justify-center h-full">
        <span className="material-symbols-outlined animate-spin text-gray-400 text-3xl">refresh</span>
      </div>
    )
  }

  if (error) {
    return (
      <div className="flex items-center gap-2 px-5 py-4 text-xs text-red-400 font-mono">
        <span className="material-symbols-outlined text-sm shrink-0">error</span>
        {error}
      </div>
    )
  }

  return (
    <div className="p-5 space-y-6">
      {/* Loaded section */}
      <div>
        <p className="text-[10px] font-bold text-gray-400 uppercase tracking-widest mb-3">
          {t('detail.skills.loadedHeading')}
        </p>
        {loaded.length === 0 ? (
          <p className="text-xs text-gray-400">{t('detail.skills.loadedEmpty')}</p>
        ) : (
          <div className="space-y-2">
            {loaded.map(({ name, firstSeen, sourceType, location, description }) => (
              <div key={name}>
                <div
                  className={`flex items-center gap-2 w-full ${(location || description) ? 'cursor-pointer' : ''}`}
                  onClick={() => (location || description) && setExpanded(prev => ({ ...prev, [name]: !prev[name] }))}
                >
                  <span className="material-symbols-outlined text-[14px] text-primary" style={{ fontVariationSettings: "'FILL' 0" }}>hexagon</span>
                  <span className="text-xs font-mono text-gray-100">{name}</span>
                  {sourceType && (
                    <span className={`text-[10px] font-mono px-1.5 py-0.5 rounded-full border border-current ${SOURCE_TYPE_COLORS[sourceType] || 'text-gray-400'}`}>
                      {sourceType}
                    </span>
                  )}
                  <span className="ml-auto text-[10px] font-mono text-gray-400">{t('detail.skills.firstSeen')}&nbsp;&nbsp;{fmtShortTime(firstSeen)}</span>
                </div>
                {expanded[name] && (location || description) && (
                  <div className="ml-6 mt-1 space-y-1">
                    {location && (
                      <p className="text-[10px] font-mono text-gray-400 break-all">
                        <span className="text-gray-400">{t('detail.skills.location')} </span>{location}
                      </p>
                    )}
                    {description && (
                      <p className="text-[10px] font-mono text-gray-400 break-words">
                        <span className="text-gray-400">{t('detail.skills.description')} </span>{description}
                      </p>
                    )}
                  </div>
                )}
              </div>
            ))}
          </div>
        )}
      </div>

      {/* Invoked section */}
      <div>
        <p className="text-[10px] font-bold text-gray-400 uppercase tracking-widest mb-3">
          {t('detail.skills.invokedHeading')}
        </p>
        {invoked.length === 0 ? (
          <p className="text-xs text-gray-400">{t('detail.skills.invokedEmpty')}</p>
        ) : (
          <div className="space-y-2">
            {invoked.map((item, i) => {
              const itemKey = `${item.time}-${item.name}-${i}`
              return (
                <div key={itemKey}>
                  <div
                    className={`flex items-center gap-3 ${item.args ? 'cursor-pointer' : ''}`}
                    onClick={() => item.args && setExpanded(prev => ({ ...prev, [itemKey]: !prev[itemKey] }))}
                  >
                    <span className="material-symbols-outlined text-[14px] text-[#ffb2b8]">play_arrow</span>
                    <span className="text-[10px] font-mono text-gray-400 w-16 shrink-0">{fmtShortTime(item.time)}</span>
                    <span className="text-xs font-mono text-gray-100">{item.name}</span>
                    {item.args && (
                      <span className="ml-auto text-[10px] font-mono text-gray-400">{t('detail.skills.args')} ▾</span>
                    )}
                  </div>
                  {expanded[itemKey] && item.args && (
                    <div className="ml-10 mt-1">
                      <p className="text-[10px] font-mono text-gray-400 break-words">
                        <span className="text-gray-400">{t('detail.skills.args')} </span>{item.args}
                      </p>
                    </div>
                  )}
                </div>
              )
            })}
          </div>
        )}
      </div>
    </div>
  )
}

// ── MemoryTab ─────────────────────────────────────────────────────────────────

const OP_COLORS = {
  save:   'text-amber-400',
  search: 'text-blue-400',
  read:   'text-emerald-400',
  delete: 'text-red-400',
  update: 'text-purple-400',
  other:  'text-gray-400',
}

function MemoryTab({ sessionId }) {
  const { t } = useTranslation('agents')
  const [ops, setOps] = useState([])
  const [connecting, setConnecting] = useState(true)
  const [error, setError] = useState(null)
  const [expanded, setExpanded] = useState({})

  useEffect(() => {
    if (!sessionId) return
    if (!/^[a-zA-Z0-9_-]{1,128}$/.test(sessionId)) return
    setOps([])
    setConnecting(true)
    setError(null)
    setExpanded({})
    const ctrl = new AbortController()
    const sql = `SELECT event_time, memory_op, memory_source, tool_name, server_name, memory_path, content_payload FROM agentguard_memory_ops WHERE session_id = '${sessionId}' ORDER BY event_time ASC SETTINGS seek_to = 'earliest'`
    async function stream() {
      for await (const row of api.protonQuery(sql, { signal: ctrl.signal })) {
        if (ctrl.signal.aborted) break
        setConnecting(false)
        setOps(prev => [...prev, row])
      }
      setConnecting(false)
    }
    stream().catch(e => { if (e?.name !== 'AbortError') setError(e.message || t('detail.memory.streamError')) })
    return () => ctrl.abort()
  }, [sessionId, t])

  const fmtTime = (ts) => ts ? (formatLocalTime(ts) || String(ts)) : '—'

  function displayLabel(op) {
    const server = op.server_name
    const path = op.memory_path
    if (server) return server
    if (path) return path.split('/').pop()
    return op.tool_name
  }

  if (connecting) {
    return (
      <div className="flex items-center justify-center h-full">
        <span className="material-symbols-outlined animate-spin text-gray-400 text-3xl">refresh</span>
      </div>
    )
  }

  if (error) {
    return (
      <div className="flex items-center gap-2 px-5 py-4 text-xs text-red-400 font-mono">
        <span className="material-symbols-outlined text-sm shrink-0">error</span>
        {error}
      </div>
    )
  }

  return (
    <div className="p-5 space-y-1">
      <p className="text-[10px] font-bold text-gray-400 uppercase tracking-widest mb-3">
        {t('detail.memory.heading')}
      </p>
      {ops.length === 0 ? (
        <p className="text-xs text-gray-400">{t('detail.memory.empty')}</p>
      ) : (
        ops.map((op, i) => (
          <div key={i}>
            <div
              className="flex items-center gap-3 cursor-pointer hover:bg-surface-container-high rounded px-2 py-1 -mx-2"
              onClick={() => setExpanded(prev => ({ ...prev, [i]: !prev[i] }))}
            >
              <span className="text-[10px] font-mono text-gray-400 w-16 shrink-0">{fmtTime(op.event_time)}</span>
              <span className={`text-[10px] font-bold uppercase w-14 shrink-0 ${OP_COLORS[op.memory_op] ?? 'text-gray-400'}`}>
                {op.memory_op}
              </span>
              <span className="text-[10px] text-gray-400 w-24 shrink-0">{op.memory_source}</span>
              <span className="text-xs font-mono text-gray-100 truncate">{displayLabel(op)}</span>
              <span className="material-symbols-outlined text-[12px] text-gray-400 ml-auto shrink-0">
                {expanded[i] ? 'expand_less' : 'expand_more'}
              </span>
            </div>
            {expanded[i] && (
              <div className="ml-2 mt-1 mb-2 pl-3 border-l border-outline-variant/40 space-y-1">
                {op.memory_path && (
                  <p className="text-[10px] font-mono text-gray-400 break-all">{op.memory_path}</p>
                )}
                {op.content_payload && (() => {
                  const parsed = safeParse(op.content_payload)
                  return parsed !== null
                    ? <div className="text-[10px] font-mono text-gray-100 leading-relaxed"><JsonValue value={parsed} depth={0} /></div>
                    : <pre className="text-[10px] font-mono text-gray-400 bg-surface rounded p-2 overflow-x-auto whitespace-pre-wrap break-all max-h-40">{op.content_payload}</pre>
                })()}
              </div>
            )}
          </div>
        ))
      )}
    </div>
  )
}

// ── AgentMemoryPanel ──────────────────────────────────────────────────────────

function AgentMemoryPanel({ agentId }) {
  const { t } = useTranslation('agents')
  const [stats, setStats] = useState(null)
  const [recentOps, setRecentOps] = useState([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState(null)
  const [expanded, setExpanded] = useState({})

  useEffect(() => {
    if (!agentId) return
    if (!/^[a-zA-Z0-9_-]{1,128}$/.test(agentId)) return
    setLoading(true)
    setError(null)
    setStats(null)
    setRecentOps([])
    setExpanded({})

    const statsSQL = `SELECT count() AS total_ops, count_if(memory_op = 'save') AS saves, count_if(memory_op = 'search') AS searches, count_if(memory_op = 'read') AS reads FROM table(agentguard_memory_ops) WHERE agent_id = '${agentId}'`
    const recentSQL = `SELECT event_time, session_id, memory_op, memory_source, tool_name, server_name, memory_path, content_payload FROM table(agentguard_memory_ops) WHERE agent_id = '${agentId}' ORDER BY event_time DESC LIMIT 50`

    Promise.all([
      (async () => {
        for await (const row of api.protonQuery(statsSQL)) {
          setStats(row)
          break
        }
      })(),
      (async () => {
        const rows = []
        for await (const row of api.protonQuery(recentSQL)) {
          rows.push(row)
        }
        setRecentOps(rows)
      })(),
    ]).catch(err => setError(err.message || t('detail.memory.loadError'))).finally(() => setLoading(false))
  }, [agentId, t])

  const fmtTime = (ts) => ts ? (formatLocalTime(ts) || String(ts)) : '—'

  function displayLabel(op) {
    if (op.server_name) return op.server_name
    if (op.memory_path) return op.memory_path.split('/').pop()
    return op.tool_name
  }

  if (loading) {
    return (
      <div className="flex items-center justify-center h-full">
        <span className="material-symbols-outlined animate-spin text-gray-400 text-3xl">refresh</span>
      </div>
    )
  }

  if (error) {
    return (
      <div className="flex items-center gap-2 p-5 text-xs text-red-400 font-mono">
        <span className="material-symbols-outlined text-sm shrink-0">error</span>
        {error}
      </div>
    )
  }

  const totalOps = Number(stats?.total_ops ?? 0)

  return (
    <div className="p-5 space-y-6">
      <div>
        <p className="text-[10px] font-bold text-gray-400 uppercase tracking-widest mb-3">{t('detail.memory.footprintHeading')}</p>
        {totalOps === 0 ? (
          <p className="text-xs text-gray-400">{t('detail.memory.footprintEmpty')}</p>
        ) : (
          <div className="grid grid-cols-4 gap-3 mb-6">
            {[
              { label: t('detail.memory.stats.total'),    value: stats?.total_ops ?? 0, color: 'text-gray-100' },
              { label: t('detail.memory.stats.saves'),    value: stats?.saves     ?? 0, color: 'text-amber-400' },
              { label: t('detail.memory.stats.searches'), value: stats?.searches  ?? 0, color: 'text-blue-400' },
              { label: t('detail.memory.stats.reads'),    value: stats?.reads     ?? 0, color: 'text-emerald-400' },
            ].map(({ label, value, color }) => (
              <div key={label} className="bg-surface rounded p-3 text-center">
                <p className={`text-lg font-bold font-mono ${color}`}>{value}</p>
                <p className="text-[10px] text-gray-400 uppercase tracking-wider mt-0.5">{label}</p>
              </div>
            ))}
          </div>
        )}
      </div>

      {recentOps.length > 0 && (
        <div>
          <p className="text-[10px] font-bold text-gray-400 uppercase tracking-widest mb-3">{t('detail.memory.recentHeading')}</p>
          <div className="space-y-1">
            {recentOps.map((op, i) => (
              <div key={i}>
                <div
                  className="flex items-center gap-3 cursor-pointer hover:bg-surface-container-high rounded px-2 py-1 -mx-2"
                  onClick={() => setExpanded(prev => ({ ...prev, [i]: !prev[i] }))}
                >
                  <span className="text-[10px] font-mono text-gray-400 w-16 shrink-0">{fmtTime(op.event_time)}</span>
                  <span className={`text-[10px] font-bold uppercase w-14 shrink-0 ${OP_COLORS[op.memory_op] ?? 'text-gray-400'}`}>
                    {op.memory_op}
                  </span>
                  <span className="text-[10px] text-gray-400 w-24 shrink-0">{op.memory_source}</span>
                  <span className="text-xs font-mono text-gray-100 truncate">{displayLabel(op)}</span>
                  <span className="material-symbols-outlined text-[12px] text-gray-400 ml-auto shrink-0">
                    {expanded[i] ? 'expand_less' : 'expand_more'}
                  </span>
                </div>
                {expanded[i] && (
                  <div className="ml-2 mt-1 mb-2 pl-3 border-l border-outline-variant/40 space-y-1">
                    {op.memory_path && (
                      <p className="text-[10px] font-mono text-gray-400 break-all">{op.memory_path}</p>
                    )}
                    {op.content_payload && (
                      safeParse(op.content_payload) !== null
                        ? <div className="text-[10px] font-mono text-gray-100 leading-relaxed"><JsonValue value={safeParse(op.content_payload)} depth={0} /></div>
                        : <pre className="text-[10px] font-mono text-gray-400 bg-surface rounded p-2 overflow-x-auto whitespace-pre-wrap break-all max-h-40">{op.content_payload}</pre>
                    )}
                  </div>
                )}
              </div>
            ))}
          </div>
        </div>
      )}
    </div>
  )
}

// ── RunWaterfall ──────────────────────────────────────────────────────────────

function RunWaterfall({ spans }) {
  const { t } = useTranslation('agents')
  const [expandedSpan, setExpandedSpan] = useState(null)

  if (!spans || spans.length === 0) {
    return (
      <div className="flex flex-col items-center justify-center h-full text-gray-400 gap-2">
        <span className="material-symbols-outlined text-4xl">account_tree</span>
        <p className="text-sm">{t('detail.trace.noData')}</p>
      </div>
    )
  }

  const root = spans.find(s => s.parent_id === '')
  if (!root) return null

  const rootStart = root.start_ms
  const totalMs = root.duration_ms || 1

  // Build a depth map so nested spans (run→llm→tool) render with correct indent.
  // Spans are sorted with parents before children, so a single forward pass works.
  const depthMap = {}
  spans.forEach(s => {
    const parentDepth = s.parent_id === '' ? -1 : (depthMap[s.parent_id] ?? 0)
    depthMap[s.span_id] = parentDepth + 1
  })

  function barColor(span) {
    if (span.kind === 'run') return '#ffb0cf'        // pink  — run root
    if (span.status === 'blocked' || span.status === 'error') return '#ffb4ab' // red
    if (span.kind === 'llm') return '#93c5fd'         // blue  — llm_input / llm_output
    if (span.kind === 'tool') return '#6ee7b7'        // green — tool calls
    if (span.kind === 'subagent') return '#c084fc'    // violet — subagent spans
    return '#c4b5fd'                                  // purple — lifecycle events
  }

  function fmtDuration(ms) {
    if (ms == null || ms === 0) return '—'
    if (ms >= 1000) return `${(ms / 1000).toFixed(1)}s`
    return `${Math.round(ms)}ms`
  }

  return (
    <div className="p-3 space-y-0.5">
      {spans.map(span => {
        const depth = depthMap[span.span_id] ?? 0
        const leftPct = Math.max(0, ((span.start_ms - rootStart) / totalMs) * 100)
        const widthPct = Math.max(0.5, (span.duration_ms / totalMs) * 100)
        const color = barColor(span)
        const isExpanded = expandedSpan === span.span_id
        const hasAttrs = span.attributes && Object.keys(span.attributes).length > 0

        return (
          <div key={span.span_id}>
            <button
              onClick={() => hasAttrs && setExpandedSpan(isExpanded ? null : span.span_id)}
              className={`w-full text-left flex items-center gap-3 px-2 py-1.5 rounded-sm transition-colors ${hasAttrs ? 'hover:bg-surface-container-high/50 cursor-pointer' : 'cursor-default'}`}
            >
              {/* Name column */}
              <div
                className="w-36 shrink-0 flex items-center gap-1"
                style={{ paddingLeft: depth * 16 }}
              >
                {span.status === 'blocked' && (
                  <span className="material-symbols-outlined text-[12px] text-error">block</span>
                )}
                {span.status === 'error' && (
                  <span className="material-symbols-outlined text-[12px] text-error">error</span>
                )}
                <span className="text-[11px] text-gray-100 truncate">{span.name}</span>
              </div>

              {/* Bar column */}
              <div className="flex-1 relative h-4 bg-surface-container-lowest rounded-sm overflow-hidden">
                <div
                  className="absolute top-0 h-full rounded-sm opacity-80"
                  style={{
                    left: `${leftPct}%`,
                    width: `${widthPct}%`,
                    backgroundColor: color,
                    minWidth: 2,
                  }}
                />
              </div>

              {/* Duration column */}
              <span className="text-[10px] text-gray-400 w-12 text-right shrink-0 tabular-nums">
                {fmtDuration(span.duration_ms)}
              </span>

              {/* Expand indicator */}
              {hasAttrs && (
                <span className={`material-symbols-outlined text-[12px] text-gray-400 transition-transform ${isExpanded ? 'rotate-180' : ''}`}>
                  expand_more
                </span>
              )}
            </button>

            {/* Attribute panel */}
            {isExpanded && hasAttrs && (
              <div
                className="mx-2 mb-1 bg-surface-container-lowest rounded-sm px-3 py-2"
                style={{ marginLeft: depth * 16 + 8 }}
              >
                {Object.entries(span.attributes).map(([k, v]) => (
                  <KVRow key={k} label={k} value={v} />
                ))}
              </div>
            )}
          </div>
        )
      })}
    </div>
  )
}

function getTimelineKindMeta(t) {
  return {
    user_input:    { label: t('detail.timeline.kinds.userInput'),    dot: '#a5b4fc' },
    llm_response:  { label: t('detail.timeline.kinds.llmResponse'),  dot: '#ffb0cf' },
    tool_request:  { label: t('detail.timeline.kinds.toolRequest'),  dot: '#93c5fd' },
    tool_result:   { label: t('detail.timeline.kinds.toolResult'),   dot: '#93c5fd' },
    session_start: { label: t('detail.timeline.kinds.sessionStart'), dot: '#6b7280' },
    session_end:   { label: t('detail.timeline.kinds.sessionEnd'),   dot: '#6b7280' },
  }
}

function TimelineBody({ kind, body }) {
  if (!body) return null
  const isCode = kind === 'tool_request' || kind === 'tool_result'
  const text = body.length > 800 ? body.slice(0, 800) + '…' : body
  if (isCode) {
    return (
      <pre className="mt-1.5 bg-surface-container-lowest rounded-sm px-2 py-1.5 text-[10px] font-mono text-gray-100 break-all whitespace-pre-wrap overflow-x-auto">
        {text}
      </pre>
    )
  }
  return (
    <p className="text-xs mt-0.5 break-words leading-relaxed whitespace-pre-wrap text-gray-100">
      {text}
    </p>
  )
}

function TurnTimeline({ turnIndex, events }) {
  const { t } = useTranslation('agents')
  const kindMeta = getTimelineKindMeta(t)
  return (
    <div className="bg-surface-container-low rounded-sm p-5 mb-3 last:mb-0">
      <div className="flex items-center gap-3 mb-3 flex-wrap">
        <span className="text-xs font-semibold text-gray-100">
          {turnIndex === 0 ? t('detail.timeline.sessionLabel') : t('detail.timeline.turnLabel', { index: turnIndex })}
        </span>
        <span className="text-[10px] text-gray-400">{t('detail.timeline.eventsCount', { count: events.length })}</span>
      </div>
      <div>
        {events.map((ev, i) => {
          const meta = kindMeta[ev.kind] ?? { label: ev.kind, dot: '#6b7280' }
          const alert = ev.status === 'blocked' || ev.status === 'error'
          return (
            <div key={`${ev.kind}-${i}`} className="flex gap-3">
              <div className="flex flex-col items-center">
                <div
                  className="w-2 h-2 rounded-full shrink-0 mt-1"
                  style={{ backgroundColor: alert ? '#ffb4ab' : meta.dot }}
                />
                {i < events.length - 1 && (
                  <div className="w-px flex-1 bg-outline-variant/20 my-1" style={{ minHeight: '12px' }} />
                )}
              </div>
              <div className="pb-3 flex-1 min-w-0">
                <div className="flex items-center gap-2 flex-wrap">
                  <span className={`text-[10px] font-semibold uppercase tracking-wide ${alert ? 'text-error' : 'text-gray-400'}`}>
                    {meta.label}
                  </span>
                  {ev.tool_name && <code className="text-[10px] font-mono text-info">{ev.tool_name}</code>}
                  {ev.provider && <span className="text-[10px] text-gray-500 font-mono">{ev.provider}</span>}
                  {ev.model && <span className="text-[10px] text-gray-400 font-mono">{ev.model}</span>}
                  {ev.status === 'blocked' && (
                    <span className="text-[10px] text-error">
                      {ev.attributes?.block_reason
                        ? t('detail.timeline.blockedWithReason', { reason: ev.attributes.block_reason })
                        : t('detail.timeline.blocked')}
                    </span>
                  )}
                  {ev.status === 'error' && <span className="text-[10px] text-error">{t('detail.timeline.error')}</span>}
                  <span className="ml-auto text-[10px] font-mono text-gray-400">{fmtShortTime(ev.event_time)}</span>
                </div>
                <TimelineBody kind={ev.kind} body={ev.body} />
              </div>
            </div>
          )
        })}
      </div>
    </div>
  )
}

// ── Main page ─────────────────────────────────────────────────────────────────

export default function AgentDetail() {
  const { t } = useTranslation('agents')
  const { id } = useParams()
  const [searchParams, setSearchParams] = useSearchParams()
  const selectedSession = searchParams.get('session')
  const selectedRun = searchParams.get('run_id')

  const [agent, setAgent] = useState(null)
  const [agentError, setAgentError] = useState(null)
  const [sessions, setSessions] = useState([])
  const [sessionsError, setSessionsError] = useState(null)
  const [sessionDetail, setSessionDetail] = useState(null)
  const [sessionDetailError, setSessionDetailError] = useState(null)
  const [events, setEvents] = useState([])
  const [eventsTruncated, setEventsTruncated] = useState(false)
  const [eventsError, setEventsError] = useState(null)
  const [timeline, setTimeline] = useState([])
  const [timelineError, setTimelineError] = useState(null)
  const [loadingTimeline, setLoadingTimeline] = useState(false)
  const [traceData, setTraceData] = useState(null)
  const [traceError, setTraceError] = useState(null)
  const [loadingTrace, setLoadingTrace] = useState(false)
  const [activeTab, setActiveTab] = useState('timeline')
  const [expandedSessions, setExpandedSessions] = useState(new Set())
  const [timePreset, setTimePreset] = useState('24h')

  const [llmMetrics, setLlmMetrics] = useState(null)
  const [llmMetricsError, setLlmMetricsError] = useState(null)
  const [toolMetrics, setToolMetrics] = useState(null)
  const [toolMetricsError, setToolMetricsError] = useState(null)
  const [activeThreatsError, setActiveThreatsError] = useState(null)
  const [activeThreats, setActiveThreats] = useState(null)

  const [loadingAgent, setLoadingAgent] = useState(true)
  const [loadingSessions, setLoadingSessions] = useState(true)
  const [loadingEvents, setLoadingEvents] = useState(false)
  const [refreshTick, setRefreshTick] = useState(0)

  const refresh = useCallback(() => setRefreshTick(prev => prev + 1), [])

  useEffect(() => {
    setLoadingAgent(true)
    setAgentError(null)
    const params = {}
    const since = presetToISO(timePreset)
    if (since) params.since = since
    api.getAgent(id, params)
      .then(data => { setAgent(data); setAgentError(null) })
      .catch(err => setAgentError(err?.message || t('detail.errors.loadAgent')))
      .finally(() => setLoadingAgent(false))
  }, [id, timePreset, refreshTick, t])

  useEffect(() => {
    const params = { agent_id: id }
    const since = presetToISO(timePreset)
    if (since) params.since = since
    api.getLLMMetrics(params)
      .then(d => { setLlmMetrics(d); setLlmMetricsError(null) })
      .catch(err => { setLlmMetrics(null); setLlmMetricsError(err?.message || t('detail.errors.loadLlmMetrics')) })
  }, [id, timePreset, refreshTick, t])

  useEffect(() => {
    const params = { agent_id: id }
    const since = presetToISO(timePreset)
    if (since) params.since = since
    api.getToolMetrics(params)
      .then(d => { setToolMetrics(d); setToolMetricsError(null) })
      .catch(err => { setToolMetrics(null); setToolMetricsError(err?.message || t('detail.errors.loadToolMetrics')) })
  }, [id, timePreset, refreshTick, t])

  useEffect(() => {
    // agent_id is the second segment of the compound "{deploymentId}__{agentId}" id
    const agentId = id.split('__')[1] ?? id
    api.getThreats({ agent_id: agentId, status: 'open', limit: 200 })
      .then(data => { setActiveThreats(data.total ?? (data.threats?.length ?? 0)); setActiveThreatsError(null) })
      .catch(err => { setActiveThreats(null); setActiveThreatsError(err?.message || t('detail.errors.loadActiveThreats')) })
  }, [id, refreshTick, t])

  useEffect(() => {
    setLoadingSessions(true)
    setSessionsError(null)
    const params = {}
    const since = presetToISO(timePreset)
    if (since) params.since = since
    api.listSessions(id, params)
      .then(data => { setSessions(data); setSessionsError(null) })
      .catch(err => { setSessions([]); setSessionsError(err?.message || t('detail.errors.loadSessions')) })
      .finally(() => setLoadingSessions(false))
  }, [id, timePreset, refreshTick, t])

  useEffect(() => {
    if (!selectedSession) { setSessionDetail(null); setSessionDetailError(null); return }
    api.getSession(id, selectedSession)
      .then(d => { setSessionDetail(d); setSessionDetailError(null) })
      .catch(err => { setSessionDetail(null); setSessionDetailError(err?.message || t('detail.errors.loadSessionDetails')) })
  }, [id, selectedSession, refreshTick, t])

  useEffect(() => {
    if (!selectedRun) { setEvents([]); setEventsTruncated(false); setEventsError(null); return }
    const params = { limit: 500 }
    if (selectedSession) params.session_id = selectedSession
    params.run_id = selectedRun
    const since = presetToISO(timePreset)
    if (since) params.since = since

    setLoadingEvents(true)
    api.listEvents(id, params)
      .then(r => {
        const evts = r.events ?? []
        setEvents(evts)
        setEventsTruncated((r.total ?? evts.length) > 500)
        setEventsError(null)
      })
      .catch(err => {
        setEvents([])
        setEventsTruncated(false)
        setEventsError(err?.message || t('detail.errors.loadEvents'))
      })
      .finally(() => setLoadingEvents(false))
  }, [id, selectedSession, selectedRun, timePreset, refreshTick, t])

  useEffect(() => {
    if (!selectedSession) { setTimeline([]); setTimelineError(null); setLoadingTimeline(false); return }
    let cancelled = false
    const params = {}
    if (selectedRun) params.run_id = selectedRun

    setTimeline([])
    setTimelineError(null)
    setLoadingTimeline(true)
    api.getSessionTimeline(id, selectedSession, params)
      .then(r => { if (!cancelled) { setTimeline(r.events ?? []); setTimelineError(null) } })
      .catch(err => { if (!cancelled) { setTimeline([]); setTimelineError(err?.message || t('detail.errors.loadTimeline')) } })
      .finally(() => { if (!cancelled) setLoadingTimeline(false) })
    return () => { cancelled = true }
  }, [id, selectedSession, selectedRun, refreshTick, t])

  useEffect(() => {
    setTraceData(null)
    setTraceError(null)
    if (!selectedRun || !selectedSession || activeTab !== 'trace') return
    setLoadingTrace(true)
    api.getRunTrace(id, selectedSession, selectedRun)
      .then(d => { setTraceData(d); setTraceError(null) })
      .catch(err => { setTraceData(null); setTraceError(err?.message || t('detail.errors.loadTrace')) })
      .finally(() => setLoadingTrace(false))
  }, [id, selectedSession, selectedRun, activeTab, t])

  const selectSession = (sid) => {
    setActiveTab('timeline')
    setSearchParams({ session: sid })
    setExpandedSessions(prev => {
      const next = new Set(prev)
      if (next.has(sid)) {
        next.delete(sid)
      } else {
        next.add(sid)
      }
      return next
    })
  }

  const selectRun = (rid) => {
    const params = {}
    if (selectedSession) params.session = selectedSession
    params.run_id = rid
    setSearchParams(params)
  }

  const isRefreshing = loadingAgent || loadingSessions || loadingEvents

  const volumeMetrics = [
    { label: t('detail.volumeMetrics.activeThreats'),  value: activeThreats ?? '—', icon: 'security', accent: activeThreats > 0, error: activeThreatsError },
    { label: t('detail.volumeMetrics.totalSessions'),  value: agent ? formatNumber(agent.session_count)   : '—', icon: 'layers' },
    { label: t('detail.volumeMetrics.totalEvents'),    value: agent ? formatNumber(agent.event_count)     : '—', icon: 'sync_alt' },
    { label: t('detail.volumeMetrics.totalToolCalls'), value: agent ? formatNumber(agent.tool_call_count) : '—', icon: 'handyman' },
  ]

  const llmHasCalls = (llmMetrics?.calls?.total ?? 0) > 0
  const toolHasCalls = (toolMetrics?.summary?.total_calls ?? 0) > 0
  const llmSuccessRate = llmMetrics?.calls?.success_rate   // null from backend when no calls
  const toolSuccessRate = toolMetrics?.summary?.success_rate // null from backend when no calls
  const perfMetrics = [
    {
      label: t('detail.perfMetrics.totalTokens'),
      value: llmHasCalls ? fmtTokens(llmMetrics.tokens?.total) : '—',
      sub: llmHasCalls ? t('detail.perfMetrics.tokensSub', { in: fmtTokens(llmMetrics.tokens?.input), out: fmtTokens(llmMetrics.tokens?.output) }) : null,
      icon: 'token',
      error: llmMetricsError,
    },
    {
      label: t('detail.perfMetrics.llmLatencyP50'),
      value: llmHasCalls ? fmtLatency(llmMetrics.latency_ms?.p50) : '—',
      sub: llmHasCalls ? t('detail.perfMetrics.latencySub', { p95: fmtLatency(llmMetrics.latency_ms?.p95) }) : null,
      icon: 'speed',
      error: llmMetricsError,
    },
    {
      label: t('detail.perfMetrics.llmSuccessRate'),
      value: fmtPct(llmSuccessRate),  // fmtPct(null) → '—'
      sub: llmHasCalls ? t('detail.perfMetrics.successSub', { success: llmMetrics.calls?.success ?? 0, failed: llmMetrics.calls?.failed ?? 0 }) : null,
      icon: 'check_circle',
      accent: llmSuccessRate != null && llmSuccessRate < 1,
      error: llmMetricsError,
    },
    {
      label: t('detail.perfMetrics.toolSuccessRate'),
      value: fmtPct(toolSuccessRate),  // fmtPct(null) → '—'
      sub: toolHasCalls ? t('detail.perfMetrics.successSub', { success: toolMetrics.summary?.success_calls ?? 0, failed: toolMetrics.summary?.failed_calls ?? 0 }) : null,
      icon: 'build_circle',
      accent: toolSuccessRate != null && toolSuccessRate < 1,
      error: toolMetricsError,
    },
  ]

  const agentName = agent?.agent_id ?? id
  const deploymentName = agent?.deployment_name ?? ''
  const agentType = agent?.agent_type ?? ''

  return (
    <div className="min-h-screen bg-surface text-gray-100">
      <Sidebar />


      {/* TopNav */}
      <header className="top-nav fixed top-0 right-0 h-16 bg-surface/95 z-50 flex justify-between items-center px-8 backdrop-blur-md shadow-[0_0_12px_rgba(213,63,140,0.12)]">
        <div className="flex items-center gap-4">
          <Link to="/agents" className="text-gray-400 hover:text-primary transition-colors">
            <span className="material-symbols-outlined">arrow_back</span>
          </Link>
          {agentType === 'claudecode' && (
            <img src="/static/icons/claude-seeklogo.svg" alt="Claude Code" title="Claude Code" className="h-5 w-5 shrink-0" />
          )}
          {agentType === 'openclaw' && (
            <img src="/static/icons/openclaw-dark.svg" alt="OpenClaw" title="OpenClaw" className="h-5 w-5 shrink-0" />
          )}
          {agentType === 'hermes' && (
            <img src="/static/icons/nousresearch-dark.png" alt="Hermes" title="Hermes" className="h-5 w-5 shrink-0" />
          )}
          <span className="font-medium tracking-tight text-lg text-gray-100">{agentName}</span>
          {deploymentName && (
            <span className="bg-surface-container-high text-gray-400 px-2 py-0.5 rounded-full text-[10px] font-semibold uppercase tracking-tighter">
              {deploymentName}
            </span>
          )}
        </div>
        <div className="flex items-center gap-3">
          <div className="flex items-center gap-2">
            <span className="material-symbols-outlined text-sm text-gray-400">schedule</span>
            <select
              value={timePreset}
              onChange={e => setTimePreset(e.target.value)}
              className="bg-surface-container-high border border-outline-variant/30 text-xs text-gray-100 rounded-sm px-2 py-1.5 focus:outline-none focus:border-[#ffb0cf]/40 hover:border-outline-variant/60 transition-colors"
            >
              {getTimePresets(t).map(p => (
                <option key={p.value} value={p.value}>{p.label}</option>
              ))}
            </select>
          </div>
          <button
            onClick={refresh}
            className="flex items-center gap-2 text-xs text-gray-400 hover:text-primary transition-colors px-3 py-1.5 rounded-sm hover:bg-surface-container-high"
          >
            <span className={`material-symbols-outlined text-base ${isRefreshing ? 'animate-spin' : ''}`}>
              refresh
            </span>
            {t('detail.refresh')}
          </button>
        </div>
      </header>

      <main className="pl-sidebar pt-16 h-screen overflow-hidden flex flex-col">
        {/* Agent load error */}
        {agentError && (
          <div className="flex items-center gap-3 mx-6 mt-4 px-4 py-3 bg-[#93000a]/20 border border-[#ffb4ab]/30 rounded-sm text-error text-xs shrink-0">
            <span className="material-symbols-outlined text-base shrink-0">error</span>
            <span>{agentError}</span>
          </div>
        )}
        {/* Volume metric cards */}
        <section className="grid grid-cols-4 gap-4 p-6 pb-3 shrink-0">
          {volumeMetrics.map((m) => (
            <div key={m.label} className={`p-4 rounded relative overflow-hidden group ${m.accent ? 'bg-[#93000a]/20 border border-[#ffb4ab]/20' : 'bg-surface-container-high'}`}>
              <div className="absolute top-0 right-0 p-2 opacity-10 group-hover:opacity-20 transition-opacity">
                <span className={`material-symbols-outlined text-4xl ${m.accent ? 'text-error' : ''}`}>{m.icon}</span>
              </div>
              <p className="text-[10px] font-medium text-gray-400 uppercase tracking-widest mb-1">{m.label}</p>
              <h2 className={`text-2xl font-medium ${m.accent ? 'text-error' : 'text-gray-100'}`}>{m.value}</h2>
              {m.error && (
                <p className="text-[10px] text-error mt-1 flex items-center gap-1 truncate" title={m.error}>
                  <span className="material-symbols-outlined text-[10px]">error</span>
                  {m.error}
                </p>
              )}
            </div>
          ))}
        </section>

        {/* Performance metric cards */}
        <section className="grid grid-cols-4 gap-4 px-6 pb-4 shrink-0">
          {perfMetrics.map((m) => (
            <div key={m.label} className="bg-surface-container p-4 rounded relative overflow-hidden group border border-outline-variant/10">
              <div className="absolute top-0 right-0 p-2 opacity-10 group-hover:opacity-20 transition-opacity">
                <span className={`material-symbols-outlined text-4xl ${m.accent ? 'text-error' : ''}`}>{m.icon}</span>
              </div>
              <p className="text-[10px] font-medium text-gray-400 uppercase tracking-widest mb-1">{m.label}</p>
              <h2 className={`text-2xl font-medium ${m.accent ? 'text-error' : 'text-gray-100'}`}>{m.value}</h2>
              {m.sub && <p className="text-[10px] text-gray-400 mt-1 truncate">{m.sub}</p>}
              {m.error && (
                <p className="text-[10px] text-error mt-1 flex items-center gap-1 truncate" title={m.error}>
                  <span className="material-symbols-outlined text-[10px]">error</span>
                  {m.error}
                </p>
              )}
            </div>
          ))}
        </section>

        {/* Sessions + Timeline split */}
        <div className="flex gap-4 px-6 pb-6 flex-1 min-h-0">

          {/* Sessions panel */}
          <aside className="w-52 shrink-0 bg-surface-container rounded-sm flex flex-col overflow-hidden">
            <div className="px-4 py-3 border-b border-outline-variant/20 shrink-0">
              <h3 className="text-[10px] font-semibold text-gray-400 uppercase tracking-widest">{t('detail.sessionsPanel.title')}</h3>
            </div>
            <div className="flex-1 overflow-y-auto">
              {loadingSessions ? (
                <div className="flex items-center justify-center py-8">
                  <span className="material-symbols-outlined animate-spin text-gray-400">refresh</span>
                </div>
              ) : sessionsError ? (
                <div className="px-4 py-4 flex flex-col gap-1">
                  <span className="material-symbols-outlined text-error text-base">error</span>
                  <p className="text-[10px] text-error">{sessionsError}</p>
                </div>
              ) : sessions.length === 0 ? (
                <p className="text-[11px] text-gray-400 px-4 py-4">{t('detail.sessionsPanel.empty')}</p>
              ) : (
                sessions.map(session => {
                  const isActive = selectedSession === session.session_id
                  const isExpanded = expandedSessions.has(session.session_id)
                  const runs = isExpanded && sessionDetail?.runs && isActive ? sessionDetail.runs : []
                  return (
                    <div key={session.session_id}>
                      <button
                        onClick={() => selectSession(session.session_id)}
                        className={`w-full text-left px-4 py-3 flex items-start gap-2 border-l-2 transition-all hover:bg-surface-container-high/50 ${
                          isActive ? 'border-[#ffb0cf] bg-surface-container-high/40' : 'border-transparent'
                        }`}
                      >
                        <span className={`material-symbols-outlined text-base mt-0.5 shrink-0 ${isActive ? 'text-primary' : 'text-gray-400'}`}>
                          {sessionTypeIcon(session.session_type)}
                        </span>
                        <div className="min-w-0 flex-1">
                          <div className="flex items-center justify-between gap-1">
                            <div className="flex items-center gap-1.5 min-w-0">
                              <span className={`w-1.5 h-1.5 rounded-full shrink-0 ${session.status === 'active' ? 'bg-emerald-400' : 'bg-zinc-600'}`} />
                              <span className={`text-xs font-medium truncate ${isActive ? 'text-primary' : 'text-gray-100'}`}>
                                {session.session_type === 'Unknown' ? session.session_id?.slice(0, 8) : session.session_type}
                              </span>
                            </div>
                            <div className="flex items-center gap-1 shrink-0">
                              <span className="text-[9px] bg-outline-variant/30 text-gray-400 px-1.5 py-0.5 rounded-full">
                                {session.run_count}
                              </span>
                              {session.run_count > 0 && (
                                <span className={`material-symbols-outlined text-[12px] transition-transform ${isExpanded && isActive ? 'text-primary rotate-180' : 'text-gray-400'}`}>
                                  expand_more
                                </span>
                              )}
                            </div>
                          </div>
                          <p className="text-[10px] text-gray-400 break-all mt-0.5">{session.session_key || session.session_id}</p>
                          {session.model && (
                            <p className="text-[9px] font-mono text-primary/70 mt-0.5">{session.model}</p>
                          )}
                          <p className="text-[9px] text-gray-400 font-mono mt-0.5">{fmtTime(session.last_seen)}</p>
                        </div>
                      </button>

                      {/* Run list (accordion) */}
                      {isExpanded && isActive && runs.map((run, idx) => {
                        const isRunActive = selectedRun === run.run_id
                        return (
                          <button
                            key={run.run_id}
                            onClick={() => selectRun(run.run_id)}
                            className={`w-full text-left pl-10 pr-4 py-2 flex items-center justify-between border-l-2 transition-all hover:bg-surface-container-high/60 ${
                              isRunActive ? 'border-[#ffb0cf]/60 bg-surface-container-high/30' : 'border-transparent'
                            }`}
                          >
                            <div>
                              <span className={`text-[11px] ${isRunActive ? 'text-primary' : 'text-gray-400'}`}>
                                {t('detail.sessionsPanel.run', { index: idx + 1 })}
                              </span>
                              <p className="text-[9px] text-gray-400 font-mono">{fmtTime(run.start_time)}</p>
                            </div>
                            <div className="flex items-center gap-1">
                              {run.tool_count > 0 && (
                                <span className="text-[9px] bg-info/10 text-info px-1.5 py-0.5 rounded-full">
                                  {run.tool_count}
                                </span>
                              )}
                              {run.has_block && (
                                <span className="material-symbols-outlined text-[12px] text-error">block</span>
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
          <section className="flex-1 bg-surface-container rounded-sm flex flex-col min-w-0 overflow-hidden">

            {/* Session header — only when a run is selected */}
            {selectedRun && sessionDetail && (
              <SessionHeader sessionDetail={sessionDetail} />
            )}

            {/* Tab bar — only when a run is selected */}
            {selectedSession && (
              <div role="tablist" className="flex border-b border-outline-variant/20 shrink-0">
                {[
                  { id: 'timeline', label: t('detail.tabs.timeline') },
                  { id: 'events',   label: t('detail.tabs.events') },
                  { id: 'trace',    label: t('detail.tabs.trace') },
                  { id: 'skills',   label: t('detail.tabs.skills') },
                  { id: 'memory',   label: t('detail.tabs.memory') },
                ].map(tab => (
                  <button
                    key={tab.id}
                    role="tab"
                    aria-selected={activeTab === tab.id}
                    onClick={() => setActiveTab(tab.id)}
                    className={`px-5 py-2.5 text-[11px] font-semibold uppercase tracking-wider transition-colors border-b-2 ${
                      activeTab === tab.id
                        ? 'border-[#ffb0cf] text-primary'
                        : 'border-transparent text-gray-400 hover:text-gray-100'
                    }`}
                  >
                    {tab.label}
                  </button>
                ))}
              </div>
            )}

            {/* Event list */}
            <div className="flex-1 overflow-y-auto">
              {!selectedSession ? (
                <AgentMemoryPanel agentId={agent?.agent_id} />
              ) : activeTab === 'skills' ? (
                <SkillsTab sessionId={selectedSession} />
              ) : activeTab === 'memory' ? (
                <MemoryTab sessionId={selectedSession} />
              ) : !selectedRun ? (
                sessionDetail ? (
                  <SessionSummary sessionDetail={sessionDetail} />
                ) : sessionDetailError ? (
                  <div className="flex flex-col items-center justify-center h-full text-error gap-2 p-6 text-center">
                    <span className="material-symbols-outlined text-4xl">error</span>
                    <p className="text-sm font-semibold">{t('detail.session.loadFailedHeading')}</p>
                    <p className="text-xs text-gray-400 max-w-md break-words">{sessionDetailError}</p>
                  </div>
                ) : (
                  <div className="flex items-center justify-center h-full">
                    <span className="material-symbols-outlined animate-spin text-gray-400 text-3xl">refresh</span>
                  </div>
                )
              ) : activeTab === 'trace' ? (
                loadingTrace ? (
                  <div className="flex items-center justify-center h-full">
                    <span className="material-symbols-outlined animate-spin text-gray-400 text-3xl">refresh</span>
                  </div>
                ) : traceError ? (
                  <div className="flex flex-col items-center justify-center h-full text-error gap-2 p-6 text-center">
                    <span className="material-symbols-outlined text-4xl">error</span>
                    <p className="text-sm font-semibold">{t('detail.trace.loadFailedHeading')}</p>
                    <p className="text-xs text-gray-400 max-w-md break-words">{traceError}</p>
                  </div>
                ) : (
                  <RunWaterfall spans={traceData?.spans ?? []} />
                )
              ) : activeTab === 'events' ? (
                loadingEvents ? (
                  <div className="flex items-center justify-center h-full">
                    <span className="material-symbols-outlined animate-spin text-gray-400 text-3xl">refresh</span>
                  </div>
                ) : eventsError ? (
                  <div className="flex flex-col items-center justify-center h-full text-error gap-2 p-6 text-center">
                    <span className="material-symbols-outlined text-4xl">error</span>
                    <p className="text-sm font-semibold">{t('detail.events.loadFailedHeading')}</p>
                    <p className="text-xs text-gray-400 max-w-md break-words">{eventsError}</p>
                  </div>
                ) : events.length === 0 ? (
                  <div className="flex flex-col items-center justify-center h-full text-gray-400 gap-2">
                    <span className="material-symbols-outlined text-4xl">inbox</span>
                    <p className="text-sm">{t('detail.events.noEvents')}</p>
                  </div>
                ) : (
                  <div className="p-4 space-y-1.5">
                    <p className="text-[10px] text-gray-400 mb-2">{t('detail.events.count', { count: events.length })}</p>
                    {events.map((evt, i) => (
                      <RunEventCard key={`${evt.hook_name}-${i}`} event={evt} />
                    ))}
                  </div>
                )
              ) : (
                // Timeline tab
                (() => {
                  if (timelineError) {
                    return (
                      <div className="flex flex-col items-center justify-center h-full text-error gap-2 p-6 text-center">
                        <span className="material-symbols-outlined text-4xl">error</span>
                        <p className="text-sm font-semibold">{t('detail.timeline.loadFailedHeading')}</p>
                        <p className="text-xs text-gray-400 max-w-md break-words">{timelineError}</p>
                      </div>
                    )
                  }
                  if (loadingTimeline && timeline.length === 0) {
                    return (
                      <div className="flex items-center justify-center h-full">
                        <span className="material-symbols-outlined animate-spin text-gray-400 text-3xl">refresh</span>
                      </div>
                    )
                  }
                  if (timeline.length === 0) {
                    return (
                      <div className="flex flex-col items-center justify-center h-full text-gray-400 gap-2">
                        <span className="material-symbols-outlined text-4xl">inbox</span>
                        <p className="text-sm">{t('detail.timeline.empty')}</p>
                      </div>
                    )
                  }
                  const turns = new Map()
                  for (const ev of timeline) {
                    if (!turns.has(ev.turn_index)) turns.set(ev.turn_index, [])
                    turns.get(ev.turn_index).push(ev)
                  }
                  const indexes = [...turns.keys()].sort((a, b) => a - b)
                  return (
                    <div className="p-4">
                      {indexes.map(idx => (
                        <TurnTimeline key={idx} turnIndex={idx} events={turns.get(idx)} />
                      ))}
                    </div>
                  )
                })()
              )}
            </div>

            {eventsTruncated && (
              <div className="px-6 py-2 border-t border-outline-variant/20 shrink-0 flex items-center gap-2">
                <span className="material-symbols-outlined text-[14px] text-amber-400">warning</span>
                <span className="text-[10px] text-amber-400">{t('detail.eventsTruncated')}</span>
              </div>
            )}

          </section>
        </div>
      </main>
    </div>
  )
}
