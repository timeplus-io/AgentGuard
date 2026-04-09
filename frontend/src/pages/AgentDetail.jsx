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

function fmtShortTime(iso) {
  if (!iso) return ''
  try {
    return new Date(typeof iso === 'string' ? iso.replace(' ', 'T') : iso)
      .toLocaleTimeString([], { hour: '2-digit', minute: '2-digit', second: '2-digit' })
  } catch { return '' }
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

// ── SessionSummary ────────────────────────────────────────────────────────────

function SessionSummary({ sessionDetail }) {
  const isEnded = sessionDetail.status === 'ended'
  const runs = sessionDetail.runs ?? []
  const startedAt = runs[0]?.start_time ?? null
  const endedAt = isEnded ? (runs[runs.length - 1]?.end_time ?? null) : null

  return (
    <div className="flex flex-col items-center justify-center h-full p-8">
      <div className="w-full max-w-lg bg-[#1d1a25] rounded-sm p-6 space-y-6">
        {/* Session ID */}
        <div>
          <p className="text-[10px] font-semibold text-zinc-500 uppercase tracking-widest mb-1.5">Session ID</p>
          <code className="text-[13px] font-mono text-[#e7dff0] break-all leading-relaxed">{sessionDetail.session_id}</code>
        </div>

        {/* Status */}
        <div className="flex items-center gap-2">
          <span className={`w-2 h-2 rounded-full ${isEnded ? 'bg-zinc-500' : 'bg-emerald-400 animate-pulse'}`} />
          <span className={`text-sm font-semibold ${isEnded ? 'text-zinc-500' : 'text-emerald-400'}`}>
            {isEnded ? 'Ended' : 'Active'}
          </span>
          {sessionDetail.session_type && (
            <span className="ml-2 text-[10px] text-zinc-600 bg-[#2c2834] px-2 py-0.5 rounded-full uppercase tracking-wider">
              {sessionDetail.session_type}
            </span>
          )}
        </div>

        {/* Stats grid */}
        <div className="grid grid-cols-2 gap-4">
          <div className="bg-[#0f0c17] rounded-sm px-4 py-3">
            <p className="text-[10px] text-zinc-500 uppercase tracking-widest mb-1">Started</p>
            <p className="text-xs font-mono text-[#e7dff0]">{startedAt ? fmtTime(startedAt) : '—'}</p>
          </div>
          <div className="bg-[#0f0c17] rounded-sm px-4 py-3">
            <p className="text-[10px] text-zinc-500 uppercase tracking-widest mb-1">Ended</p>
            <p className="text-xs font-mono text-[#e7dff0]">{endedAt ? fmtTime(endedAt) : '—'}</p>
          </div>
          <div className="bg-[#0f0c17] rounded-sm px-4 py-3">
            <p className="text-[10px] text-zinc-500 uppercase tracking-widest mb-1">Total Runs</p>
            <p className="text-2xl font-bold text-[#e7dff0]">{runs.length}</p>
          </div>
          <div className="bg-[#0f0c17] rounded-sm px-4 py-3">
            <p className="text-[10px] text-zinc-500 uppercase tracking-widest mb-1">Tool Calls</p>
            <p className="text-2xl font-bold text-[#e7dff0]">{sessionDetail.total_tool_calls ?? 0}</p>
          </div>
        </div>

        <p className="text-[11px] text-zinc-600 text-center">Select a run from the left to view its timeline</p>
      </div>
    </div>
  )
}

// ── SessionHeader ─────────────────────────────────────────────────────────────

function SessionHeader({ sessionDetail }) {
  const isEnded = sessionDetail.status === 'ended'
  const sid = sessionDetail.session_id ?? ''
  return (
    <div className="px-5 py-3 border-b border-[#574149]/20 flex flex-wrap items-center gap-x-4 gap-y-1 shrink-0 bg-[#1d1a25]">
      <code className="text-[11px] font-mono text-[#e7dff0] break-all">{sid}</code>
      <span className={`flex items-center gap-1.5 text-[10px] font-semibold ${isEnded ? 'text-zinc-500' : 'text-emerald-400'}`}>
        <span className={`w-1.5 h-1.5 rounded-full ${isEnded ? 'bg-zinc-500' : 'bg-emerald-400 animate-pulse'}`} />
        {isEnded ? 'Ended' : 'Active'}
      </span>
      <span className="text-[10px] text-zinc-500">{sessionDetail.runs?.length ?? 0} Runs</span>
      <span className="text-[10px] text-zinc-500">{sessionDetail.total_tool_calls ?? 0} Tool Calls</span>
    </div>
  )
}

// ── buildRunData ──────────────────────────────────────────────────────────────
// Returns { runInfo, steps } for a single run's events.
// runInfo: general info (model, provider, systemPrompt) shown at top of card.
// steps: time-ordered events (user input → tool calls → response).

function parseEvtData(evt) {
  const raw = safeParse(evt?.event_data)
  return (raw && typeof raw === 'object' && !Array.isArray(raw)) ? raw : {}
}

function buildRunData(events) {
  const llmIn       = events.find(e => e.hook_name === 'llm_input')
  const llmOut      = events.find(e => e.hook_name === 'llm_output')
  const beforeTools = events.filter(e => e.hook_name === 'before_tool_call')
  const afterTools  = events.filter(e => e.hook_name === 'after_tool_call')

  const llmInData = llmIn ? parseEvtData(llmIn) : {}

  // General run info — not time-ordered, shown above the timeline
  const runInfo = {
    model:        llmIn?.model    || llmInData.model    || null,
    provider:     llmIn?.provider || llmInData.provider || null,
    systemPrompt: llmInData.systemPrompt || null,
    historyCount: Array.isArray(llmInData.historyMessages) ? llmInData.historyMessages.length : null,
    imagesCount:  (llmInData.imagesCount > 0) ? llmInData.imagesCount : null,
  }

  const steps = []

  // ── User Input ────────────────────────────────────────────────────────────
  // Use event_data.prompt (the direct user message) rather than parsing historyMessages.
  if (llmIn) {
    const prompt = llmInData.prompt
    if (prompt && String(prompt).trim()) {
      const t = String(prompt).trim()
      steps.push({
        type:    'user_input',
        label:   'User Input',
        content: t.slice(0, 400) + (t.length > 400 ? '…' : ''),
      })
    }
  }

  // ── Tool Calls (before + after paired by toolCallId, then position) ───────
  for (let i = 0; i < beforeTools.length; i++) {
    const before     = beforeTools[i]
    const beforeData = parseEvtData(before)
    const blocked    = before.hook_decision === 'block'
    const toolCallId = beforeData.toolCallId || null
    const params     = (beforeData.params && typeof beforeData.params === 'object' && Object.keys(beforeData.params).length > 0)
      ? beforeData.params : null

    // Find matching after_tool_call
    const after = toolCallId
      ? afterTools.find(a => parseEvtData(a).toolCallId === toolCallId)
      : afterTools[i]

    let resultError = null, duration = null
    if (after) {
      const afterData = parseEvtData(after)
      resultError = afterData.error || null
      duration    = afterData.durationMs ?? null
    }

    steps.push({
      type:        'tool_call',
      label:       blocked ? 'Tool Blocked' : 'Tool Called',
      content:     before.tool_name || beforeData.toolName || '—',
      blocked,
      params,
      // after_tool_call present means tool completed (success unless resultError set)
      hasResult:   after != null,
      resultError,
      duration,
    })
  }

  // ── LLM Response ─────────────────────────────────────────────────────────
  if (llmOut) {
    const llmOutData = parseEvtData(llmOut)
    const usage      = llmOutData.usage ?? llmOutData.usageTotals ?? {}
    const tokenParts = [
      usage.input  != null              ? `${fmtTokens(usage.input)} in`           : null,
      usage.output != null              ? `${fmtTokens(usage.output)} out`          : null,
      usage.cacheRead != null && usage.cacheRead > 0 ? `${fmtTokens(usage.cacheRead)} cached` : null,
      usage.total  != null              ? `${fmtTokens(usage.total)} total`         : null,
    ].filter(Boolean)

    const texts = llmOutData.assistantTexts ?? llmOutData.assistant_texts ?? []
    const responseText = Array.isArray(texts) ? texts.join('\n').trim() : ''

    steps.push({
      type:    'llm_response',
      label:   'Response',
      tokens:  tokenParts.length > 0 ? tokenParts.join(' · ') : null,
      content: responseText ? responseText.slice(0, 500) + (responseText.length > 500 ? '…' : '') : null,
    })
  }

  return { runInfo, steps }
}

// ── RunWaterfall ──────────────────────────────────────────────────────────────

function RunWaterfall({ spans }) {
  const [expandedSpan, setExpandedSpan] = useState(null)

  if (!spans || spans.length === 0) {
    return (
      <div className="flex flex-col items-center justify-center h-full text-zinc-600 gap-2">
        <span className="material-symbols-outlined text-4xl">account_tree</span>
        <p className="text-sm">No trace data available for this run</p>
      </div>
    )
  }

  const root = spans.find(s => s.parent_id === '')
  if (!root) return null

  const rootStart = root.start_ms
  const totalMs = root.duration_ms || 1

  function barColor(span) {
    if (span.kind === 'run') return '#ffb0cf'
    if (span.kind === 'llm') return '#93c5fd'
    if (span.status === 'ok') return '#6ee7b7'
    return '#ffb4ab' // blocked or error
  }

  function fmtDuration(ms) {
    if (ms == null || ms === 0) return '—'
    if (ms >= 1000) return `${(ms / 1000).toFixed(1)}s`
    return `${Math.round(ms)}ms`
  }

  return (
    <div className="p-3 space-y-0.5">
      {spans.map(span => {
        const depth = span.parent_id === '' ? 0 : 1
        const leftPct = Math.max(0, ((span.start_ms - rootStart) / totalMs) * 100)
        const widthPct = Math.max(0.5, (span.duration_ms / totalMs) * 100)
        const color = barColor(span)
        const isExpanded = expandedSpan === span.span_id
        const hasAttrs = span.attributes && Object.keys(span.attributes).length > 0

        return (
          <div key={span.span_id}>
            <button
              onClick={() => hasAttrs && setExpandedSpan(isExpanded ? null : span.span_id)}
              className={`w-full text-left flex items-center gap-3 px-2 py-1.5 rounded-sm transition-colors ${hasAttrs ? 'hover:bg-[#2c2834]/50 cursor-pointer' : 'cursor-default'}`}
            >
              {/* Name column */}
              <div
                className="w-36 shrink-0 flex items-center gap-1"
                style={{ paddingLeft: depth * 16 }}
              >
                {span.kind === 'tool' && span.status === 'blocked' && (
                  <span className="material-symbols-outlined text-[12px] text-[#ffb4ab]">block</span>
                )}
                {span.kind === 'tool' && span.status === 'error' && (
                  <span className="material-symbols-outlined text-[12px] text-[#ffb4ab]">error</span>
                )}
                <span className="text-[11px] text-[#e7dff0] truncate">{span.name}</span>
              </div>

              {/* Bar column */}
              <div className="flex-1 relative h-4 bg-[#0f0c17] rounded-sm overflow-hidden">
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
              <span className="text-[10px] text-zinc-500 w-12 text-right shrink-0 tabular-nums">
                {fmtDuration(span.duration_ms)}
              </span>

              {/* Expand indicator */}
              {hasAttrs && (
                <span className={`material-symbols-outlined text-[12px] text-zinc-600 transition-transform ${isExpanded ? 'rotate-180' : ''}`}>
                  expand_more
                </span>
              )}
            </button>

            {/* Attribute panel */}
            {isExpanded && hasAttrs && (
              <div
                className="mx-2 mb-1 bg-[#0f0c17] rounded-sm px-3 py-2"
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

// ── RunTimeline ───────────────────────────────────────────────────────────────

function stepDot(step) {
  if (step.type === 'tool_call') return step.blocked ? '#ffb4ab' : '#93c5fd'
  return '#ffb0cf'
}

function RunTimeline({ runIndex, run, events }) {
  const [sysExpanded, setSysExpanded] = useState(false)
  const rid = run.run_id ?? ''
  const { runInfo, steps } = buildRunData(events)
  const start = fmtShortTime(run.start_time)
  const end   = fmtShortTime(run.end_time)
  const timeRange = start && end ? `${start} → ${end}` : start || ''

  return (
    <div className="bg-[#1d1a25] rounded-sm p-5 mb-3 last:mb-0">
      {/* Run header */}
      <div className="flex items-center gap-3 mb-3 flex-wrap">
        <span className="text-xs font-semibold text-[#e7dff0]">Run #{runIndex + 1}</span>
        <code className="text-[10px] font-mono text-zinc-500 break-all">
          {rid || '—'}
        </code>
        {timeRange && <span className="text-[10px] text-zinc-600">{timeRange}</span>}
        {run.tool_count > 0 && (
          <span className="text-[9px] bg-[#93c5fd]/10 text-[#93c5fd] px-1.5 py-0.5 rounded-full">
            {run.tool_count} tools
          </span>
        )}
        {run.has_block && (
          <span className="material-symbols-outlined text-[12px] text-[#ffb4ab]">block</span>
        )}
      </div>

      {/* General run info — model, system prompt (not time-ordered) */}
      {(runInfo.model || runInfo.systemPrompt) && (
        <div className="mb-4 bg-[#0f0c17] rounded-sm px-3 py-2.5 space-y-2">
          {runInfo.model && (
            <div className="flex items-center gap-2 flex-wrap">
              <span className="material-symbols-outlined text-[13px] text-zinc-500">smart_toy</span>
              <span className="text-[10px] text-zinc-300 font-mono">
                {[runInfo.model, runInfo.provider].filter(Boolean).join(' · ')}
              </span>
              {runInfo.historyCount != null && (
                <span className="text-[10px] text-zinc-500">{runInfo.historyCount} messages in history</span>
              )}
              {runInfo.imagesCount != null && (
                <span className="text-[10px] text-zinc-500">{runInfo.imagesCount} images</span>
              )}
            </div>
          )}
          {runInfo.systemPrompt && (
            <div>
              <div className="flex items-center gap-1.5 mb-1">
                <span className="material-symbols-outlined text-[13px] text-zinc-500">tune</span>
                <span className="text-[10px] font-semibold text-zinc-500 uppercase tracking-wide">System Prompt</span>
                <button
                  onClick={() => setSysExpanded(e => !e)}
                  className="text-[10px] text-zinc-600 hover:text-[#ffb0cf] transition-colors"
                >
                  {sysExpanded ? '▲ collapse' : '▼ expand'}
                </button>
              </div>
              <p className="text-[10px] text-zinc-400 break-words leading-relaxed font-mono whitespace-pre-wrap">
                {sysExpanded
                  ? runInfo.systemPrompt
                  : runInfo.systemPrompt.slice(0, 150) + (runInfo.systemPrompt.length > 150 ? '…' : '')}
              </p>
            </div>
          )}
        </div>
      )}

      {/* Timeline steps */}
      {steps.length === 0 ? (
        <p className="text-[11px] text-zinc-600">No structured events for this run.</p>
      ) : (
        <div>
          {steps.map((step, i) => (
            <div key={`${step.type}-${i}`} className="flex gap-3">
              <div className="flex flex-col items-center">
                <div
                  className="w-2 h-2 rounded-full shrink-0 mt-1"
                  style={{ backgroundColor: stepDot(step) }}
                />
                {i < steps.length - 1 && (
                  <div className="w-px flex-1 bg-[#574149]/20 my-1" style={{ minHeight: '12px' }} />
                )}
              </div>
              <div className="pb-3 flex-1 min-w-0">
                <span className={`text-[10px] font-semibold uppercase tracking-wide ${
                  step.blocked ? 'text-[#ffb4ab]' : 'text-zinc-400'
                }`}>
                  {step.label}
                </span>

                {/* Main content: tool name or prompt text or response text */}
                {step.content && (
                  <p className="text-xs text-[#e7dff0] mt-0.5 break-words leading-relaxed whitespace-pre-wrap">
                    {step.content}
                  </p>
                )}

                {/* Tool params (key-value) */}
                {step.params && (
                  <div className="mt-1.5 bg-[#0f0c17] rounded-sm px-2 py-1.5 space-y-0.5">
                    {Object.entries(step.params).map(([k, v]) => (
                      <div key={k} className="grid grid-cols-[90px_1fr] gap-1.5">
                        <span className="text-[10px] text-zinc-500 truncate">{k}</span>
                        <span className="text-[10px] font-mono text-zinc-300 break-all whitespace-pre-wrap">
                          {typeof v === 'object' ? JSON.stringify(v) : String(v ?? '—')}
                        </span>
                      </div>
                    ))}
                  </div>
                )}

                {/* Tool result: success/fail indicator + error reason */}
                {step.type === 'tool_call' && step.hasResult && (
                  <div className="mt-1.5 flex items-start gap-2">
                    {step.resultError ? (
                      <>
                        <span className="material-symbols-outlined text-[13px] text-[#ffb4ab] shrink-0 mt-0.5">cancel</span>
                        <span className="text-[10px] text-[#ffb4ab] break-words">{step.resultError}</span>
                      </>
                    ) : (
                      <span className="flex items-center gap-1.5 text-[10px] text-emerald-400">
                        <span className="material-symbols-outlined text-[13px]">check_circle</span>
                        Success{step.duration != null ? ` · ${step.duration} ms` : ''}
                      </span>
                    )}
                  </div>
                )}

                {/* Token usage (llm_response) */}
                {step.tokens && (
                  <p className="text-[10px] text-zinc-500 mt-0.5">{step.tokens}</p>
                )}
              </div>
            </div>
          ))}
        </div>
      )}
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
  const [eventsTruncated, setEventsTruncated] = useState(false)
  const [traceData, setTraceData] = useState(null)
  const [loadingTrace, setLoadingTrace] = useState(false)
  const [activeTab, setActiveTab] = useState('timeline')
  const [expandedSessions, setExpandedSessions] = useState(new Set())
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
    if (!selectedRun) { setEvents([]); setEventsTruncated(false); return }
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
      })
      .catch(() => { setEvents([]); setEventsTruncated(false) })
      .finally(() => setLoadingEvents(false))
  }, [id, selectedSession, selectedRun, timePreset, refreshTick])

  useEffect(() => {
    setTraceData(null)
    if (!selectedRun || !selectedSession || activeTab !== 'trace') return
    setLoadingTrace(true)
    api.getRunTrace(id, selectedSession, selectedRun)
      .then(setTraceData)
      .catch(() => setTraceData(null))
      .finally(() => setLoadingTrace(false))
  }, [id, selectedSession, selectedRun, activeTab])

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
              onChange={e => setTimePreset(e.target.value)}
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
                  const isExpanded = expandedSessions.has(session.session_id)
                  const runs = isExpanded && sessionDetail?.runs && isActive ? sessionDetail.runs : []
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
                            <div className="flex items-center gap-1 shrink-0">
                              <span className="text-[9px] bg-[#574149]/30 text-zinc-400 px-1.5 py-0.5 rounded-full">
                                {session.run_count}
                              </span>
                              {session.run_count > 0 && (
                                <span className={`material-symbols-outlined text-[12px] transition-transform ${isExpanded && isActive ? 'text-[#ffb0cf] rotate-180' : 'text-zinc-600'}`}>
                                  expand_more
                                </span>
                              )}
                            </div>
                          </div>
                          <p className="text-[10px] text-zinc-600 break-all mt-0.5">{session.session_key}</p>
                          <p className="text-[9px] text-zinc-700 font-mono mt-0.5">{fmtTime(session.last_seen)}</p>
                        </div>
                      </button>

                      {/* Run list (accordion) */}
                      {isExpanded && isActive && runs.map((run, idx) => {
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

            {/* Session header — only when a run is selected */}
            {selectedRun && sessionDetail && (
              <SessionHeader sessionDetail={sessionDetail} />
            )}

            {/* Tab bar — only when a run is selected */}
            {selectedRun && (
              <div role="tablist" className="flex border-b border-[#574149]/20 shrink-0">
                {[
                  { id: 'timeline', label: 'Timeline' },
                  { id: 'trace',    label: 'Trace' },
                  { id: 'raw', label: 'Raw Events' },
                ].map(tab => (
                  <button
                    key={tab.id}
                    role="tab"
                    aria-selected={activeTab === tab.id}
                    onClick={() => setActiveTab(tab.id)}
                    className={`px-5 py-2.5 text-[11px] font-semibold uppercase tracking-wider transition-colors border-b-2 ${
                      activeTab === tab.id
                        ? 'border-[#ffb0cf] text-[#ffb0cf]'
                        : 'border-transparent text-zinc-500 hover:text-zinc-300'
                    }`}
                  >
                    {tab.label}
                  </button>
                ))}
              </div>
            )}

            {/* Event list */}
            <div className="flex-1 overflow-y-auto">
              {!selectedRun ? (
                selectedSession && sessionDetail ? (
                  <SessionSummary sessionDetail={sessionDetail} />
                ) : (
                  <div className="flex flex-col items-center justify-center h-full text-zinc-600 gap-2">
                    <span className="material-symbols-outlined text-4xl">timeline</span>
                    <p className="text-sm">Select a session from the left to begin</p>
                  </div>
                )
              ) : loadingEvents ? (
                <div className="flex items-center justify-center h-full">
                  <span className="material-symbols-outlined animate-spin text-zinc-600 text-3xl">refresh</span>
                </div>
              ) : activeTab === 'trace' ? (
                loadingTrace ? (
                  <div className="flex items-center justify-center h-full">
                    <span className="material-symbols-outlined animate-spin text-zinc-600 text-3xl">refresh</span>
                  </div>
                ) : (
                  <RunWaterfall spans={traceData?.spans ?? []} />
                )
              ) : activeTab === 'raw' ? (
                events.length === 0 ? (
                  <div className="flex flex-col items-center justify-center h-full text-zinc-600 gap-2">
                    <span className="material-symbols-outlined text-4xl">inbox</span>
                    <p className="text-sm">No events recorded for this session</p>
                  </div>
                ) : (
                  events.map((event, i) => (
                    <EventRow key={`${event.run_id}-${event.hook_name}-${i}`} event={event} />
                  ))
                )
              ) : (
                // Timeline tab
                (() => {
                  // Still loading session detail — don't show "No runs found" yet
                  if (!sessionDetail) {
                    return (
                      <div className="flex items-center justify-center h-full">
                        <span className="material-symbols-outlined animate-spin text-zinc-600 text-3xl">refresh</span>
                      </div>
                    )
                  }
                  const runs = selectedRun
                    ? (sessionDetail?.runs ?? []).filter(r => r.run_id === selectedRun)
                    : (sessionDetail?.runs ?? [])
                  if (runs.length === 0) {
                    return (
                      <div className="flex flex-col items-center justify-center h-full text-zinc-600 gap-2">
                        <span className="material-symbols-outlined text-4xl">inbox</span>
                        <p className="text-sm">No runs found for this session</p>
                      </div>
                    )
                  }
                  // Group events by run_id
                  const byRun = {}
                  for (const e of events) {
                    if (!e.run_id) continue
                    if (!byRun[e.run_id]) byRun[e.run_id] = []
                    byRun[e.run_id].push(e)
                  }
                  // Sort each run's events by event_time ASC
                  for (const rid of Object.keys(byRun)) {
                    byRun[rid].sort((a, b) => {
                      const ta = new Date(typeof a.event_time === 'string' ? a.event_time.replace(' ', 'T') : a.event_time)
                      const tb = new Date(typeof b.event_time === 'string' ? b.event_time.replace(' ', 'T') : b.event_time)
                      return ta - tb
                    })
                  }
                  return (
                    <div className="p-4">
                      {runs.map((run) => (
                        <RunTimeline
                          key={run.run_id}
                          runIndex={sessionDetail.runs.findIndex(r => r.run_id === run.run_id)}
                          run={run}
                          events={byRun[run.run_id] ?? []}
                        />
                      ))}
                    </div>
                  )
                })()
              )}
            </div>

            {eventsTruncated && (
              <div className="px-6 py-2 border-t border-[#574149]/20 shrink-0 flex items-center gap-2">
                <span className="material-symbols-outlined text-[14px] text-amber-400">warning</span>
                <span className="text-[10px] text-amber-400">Showing first 500 events — session has more. Select a specific run from the sidebar to narrow the view.</span>
              </div>
            )}

          </section>
        </div>
      </main>
    </div>
  )
}
