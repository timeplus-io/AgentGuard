import { useState, useEffect, useRef, useCallback } from 'react'
import { Link } from 'react-router-dom'
import Sidebar from '../components/Sidebar'
import TopNav from '../components/TopNav'
import PageHeader from '../components/PageHeader'
import { api } from '../api/client'

const EXAMPLE_PROMPTS = [
  'Did any agent leak credentials or API keys in the last week?',
  'Show me conversations where customer email addresses appeared',
  'Which sessions discussed production database passwords?',
  'Was any source code exfiltrated to external services this month?',
]

const ROLES = ['user', 'assistant', 'tool']

function formatTs(ts) {
  if (!ts) return '—'
  try {
    const normalized = typeof ts === 'string'
      ? ts.replace(' ', 'T').replace(/\s+UTC$/, 'Z').replace(/\s+\+0000$/, 'Z').replace(/\s+[\w+\-:]+$/, 'Z')
      : ts
    const d = new Date(normalized)
    if (isNaN(d.getTime())) return String(ts)
    return d.toLocaleString()
  } catch { return String(ts) }
}

function truncId(id, n = 12) {
  if (!id) return '—'
  return id.length > n ? id.slice(0, n) + '…' : id
}

// The agent detail route keys agents by "deploymentId__agentId".
function sessionHref(m) {
  if (!m?.deployment_id || !m?.agent_id || !m?.session_id) return null
  return `/agents/${encodeURIComponent(`${m.deployment_id}__${m.agent_id}`)}?session=${encodeURIComponent(m.session_id)}`
}

function RiskScoreBadge({ score }) {
  const s = Number(score) || 0
  const cls = s >= 0.7 ? 'bg-red-900 text-red-400 border border-red-400'
    : s >= 0.4 ? 'bg-orange-900 text-orange-400 border border-orange-400'
    : s > 0 ? 'bg-yellow-900/60 text-yellow-400 border border-yellow-600'
    : 'bg-gray-700 text-gray-400 border border-gray-600'
  return (
    <span className={`text-[10px] font-bold rounded-full px-2 py-0.5 whitespace-nowrap ${cls}`}>
      {s.toFixed(2)}
    </span>
  )
}

function RiskTagChips({ tags, onClick }) {
  if (!tags?.length) return <span className="text-gray-400 text-xs">—</span>
  return (
    <div className="flex flex-wrap gap-1">
      {tags.map(tag => (
        <span
          key={tag}
          onClick={onClick ? (e) => { e.stopPropagation(); onClick(tag) } : undefined}
          className={`text-[10px] font-semibold rounded-full px-2 py-0.5 bg-[#ffb4ab]/10 text-[#ffb4ab] whitespace-nowrap ${onClick ? 'cursor-pointer hover:bg-[#ffb4ab]/20' : ''}`}
        >
          {tag}
        </span>
      ))}
    </div>
  )
}

function RoleBadge({ role }) {
  const styles = {
    user: 'bg-[#ffb0cf]/10 text-[#ffb0cf]',
    assistant: 'bg-sky-900/50 text-sky-300',
    tool: 'bg-gray-700 text-gray-300',
  }
  return (
    <span className={`text-[10px] font-semibold uppercase rounded-full px-2 py-0.5 ${styles[role] || styles.tool}`}>
      {role || '—'}
    </span>
  )
}

// ── Unprovisioned call-to-action ─────────────────────────────────────────────

function SetupCTA({ status, onProvision, provisioning, provisionError }) {
  const missing = []
  if (status.enabled) {
    if (!status.stream_exists) missing.push('agentguard_dlp_enriched stream')
    if (!status.mv_exists) missing.push('mv_dlp_enrich materialized view')
    for (const [fn, ok] of Object.entries(status.udfs || {})) {
      if (!ok) missing.push(`${fn} UDF`)
    }
  }
  return (
    <section className="bg-[#211e29] rounded-sm p-10 flex flex-col items-center text-center gap-4">
      <span className="material-symbols-outlined text-5xl text-[#ffb0cf]">policy</span>
      <h3 className="text-lg font-semibold text-gray-100">
        Semantic DLP is not {status.enabled ? 'provisioned' : 'enabled'}
      </h3>
      <p className="text-xs text-gray-400 max-w-md">
        {status.enabled
          ? 'The enrichment pipeline (stream, UDFs, and materialized view) has not been created in Timeplus yet. Provisioning creates the missing pieces without restarting the server.'
          : 'Enabling turns on the enrichment + query pipeline and provisions the stream, UDFs, and materialized view in Timeplus — no restart required. You can point it at your model endpoint afterwards in Settings or the DLP config.'}
      </p>
      {missing.length > 0 && (
        <ul className="text-[11px] text-gray-400 font-mono">
          {missing.map(m => <li key={m}>✗ {m}</li>)}
        </ul>
      )}
      <button
        onClick={onProvision}
        disabled={provisioning}
        className="px-5 py-2 text-xs font-semibold rounded bg-gradient-to-br from-[#ffb0cf] to-[#f256a3] text-[#3d0023] hover:scale-95 transition-transform disabled:opacity-50"
      >
        {provisioning
          ? (status.enabled ? 'Provisioning…' : 'Enabling…')
          : (status.enabled ? 'Provision now' : 'Enable Semantic DLP')}
      </button>
      {provisionError && <p className="text-xs text-[#ffb4ab]">{provisionError}</p>}
      {!status.enabled && (
        <p className="text-[11px] text-gray-500 max-w-md">
          If <code className="font-mono">DLP_ENABLED</code> is set as an environment variable it takes precedence — unset it to control this from the UI.
        </p>
      )}
    </section>
  )
}

// ── Ask box + progressive answer pane ────────────────────────────────────────

function StageDot({ done, active, label }) {
  return (
    <div className="flex items-center gap-1.5">
      <span className={`w-2 h-2 rounded-full ${done ? 'bg-[#ffb0cf]' : active ? 'bg-[#ffb0cf] animate-pulse' : 'bg-gray-600'}`} />
      <span className={`text-[10px] uppercase tracking-widest font-semibold ${done || active ? 'text-gray-100' : 'text-gray-400'}`}>
        {label}
      </span>
    </div>
  )
}

function EvidenceCard({ ev, onOpenMessage }) {
  return (
    <div className="bg-[#2c2834] rounded-sm p-4 flex flex-col gap-2">
      <div className="flex items-center gap-2 flex-wrap">
        <RoleBadge role={ev.role} />
        <code className="text-[10px] font-mono text-[#ffb0cf]">{ev.agent_id || ev.agent_type || '—'}</code>
        <span className="text-[10px] text-gray-400">{formatTs(ev.event_time)}</span>
        <span className="ml-auto flex items-center gap-2">
          {ev.rerank_score > 0 && (
            <span className="text-[10px] text-gray-400" title="Relevance score from reranker">
              rel {Number(ev.rerank_score).toFixed(2)}
            </span>
          )}
          <RiskScoreBadge score={ev.risk_score} />
        </span>
      </div>
      <RiskTagChips tags={ev.risk_tags} />
      <p className="text-xs text-gray-100 whitespace-pre-wrap break-words max-h-32 overflow-hidden">{ev.content}</p>
      <div className="flex items-center gap-3 mt-auto pt-1">
        <button
          onClick={() => onOpenMessage(ev.message_id)}
          className="text-[11px] font-semibold text-[#ffb0cf] hover:underline"
        >
          Message detail
        </button>
        {sessionHref(ev) && (
          <Link
            to={sessionHref(ev)}
            className="text-[11px] font-semibold text-gray-400 hover:text-gray-100"
          >
            Open session →
          </Link>
        )}
      </div>
    </div>
  )
}

function AskPanel({ onOpenMessage }) {
  const [question, setQuestion] = useState('')
  const [asking, setAsking] = useState(false)
  const [stages, setStages] = useState(null) // { intent, candidates, reranked, answer, error }
  const abortRef = useRef(null)

  useEffect(() => () => abortRef.current?.abort(), [])

  async function ask(q) {
    const trimmed = q.trim()
    if (!trimmed || asking) return
    abortRef.current?.abort()
    const ctrl = new AbortController()
    abortRef.current = ctrl
    setAsking(true)
    setStages({})
    try {
      for await (const event of api.dlpQuery(trimmed, { signal: ctrl.signal })) {
        if (!event?.stage) continue
        setStages(prev => ({ ...prev, [event.stage]: event }))
        if (event.stage === 'error') break
      }
    } catch (err) {
      if (err?.name !== 'AbortError') {
        setStages(prev => ({ ...prev, error: { error: err?.message || 'Query failed' } }))
      }
    } finally {
      setAsking(false)
    }
  }

  const intent = stages?.intent?.intent
  const candidates = stages?.candidates
  const reranked = stages?.reranked
  const answer = stages?.answer
  const error = stages?.error

  return (
    <section className="bg-[#211e29] rounded-sm p-6 mb-6">
      <h3 className="text-[11px] font-semibold uppercase tracking-widest text-gray-400 mb-3">
        Ask about data exposure
      </h3>
      <form
        onSubmit={e => { e.preventDefault(); ask(question) }}
        className="flex gap-2"
      >
        <input
          type="text"
          value={question}
          onChange={e => setQuestion(e.target.value)}
          placeholder="e.g. Did any agent leak credentials in the last week?"
          className="flex-1 bg-[#2c2834] text-gray-100 text-sm rounded-sm px-4 py-2.5 placeholder-gray-500 focus:outline-none focus:ring-1 focus:ring-[#ffb0cf]/50"
        />
        <button
          type="submit"
          disabled={asking || !question.trim()}
          className="px-5 py-2 text-xs font-semibold rounded bg-gradient-to-br from-[#ffb0cf] to-[#f256a3] text-[#3d0023] hover:scale-95 transition-transform disabled:opacity-50 whitespace-nowrap"
        >
          {asking ? 'Investigating…' : 'Investigate'}
        </button>
      </form>

      <div className="flex flex-wrap gap-2 mt-3">
        {EXAMPLE_PROMPTS.map(p => (
          <button
            key={p}
            onClick={() => { setQuestion(p); ask(p) }}
            disabled={asking}
            className="text-[11px] px-3 py-1.5 rounded-full bg-[#2c2834] text-gray-400 hover:text-gray-100 hover:bg-gray-700 transition-colors disabled:opacity-50"
          >
            {p}
          </button>
        ))}
      </div>

      {stages && (
        <div className="mt-6">
          {/* Stage progress */}
          <div className="flex items-center gap-6 mb-4">
            <StageDot done={!!intent} active={asking && !intent} label="Understand" />
            <StageDot done={!!candidates} active={asking && !!intent && !candidates} label="Recall" />
            <StageDot done={!!reranked} active={asking && !!candidates && !reranked} label="Rerank" />
            <StageDot done={!!answer} active={asking && !!reranked && !answer} label="Report" />
          </div>

          {intent && (
            <div className="bg-[#2c2834] rounded-sm p-4 mb-3 flex flex-wrap items-center gap-x-6 gap-y-2 text-[11px]">
              <span className="text-gray-400 uppercase tracking-widest font-semibold text-[10px]">Intent</span>
              {intent.keywords?.length > 0 && (
                <span className="text-gray-100">keywords: <span className="font-mono text-[#ffb0cf]">{intent.keywords.join(', ')}</span></span>
              )}
              {intent.entity_types?.length > 0 && (
                <span className="text-gray-100">entities: <span className="font-mono text-[#ffb0cf]">{intent.entity_types.join(', ')}</span></span>
              )}
              {intent.risk_tags?.length > 0 && (
                <span className="text-gray-100">risk tags: <span className="font-mono text-[#ffb4ab]">{intent.risk_tags.join(', ')}</span></span>
              )}
              <span className="text-gray-100">window: <span className="font-mono text-[#ffb0cf]">{intent.time_range_days}d</span></span>
            </div>
          )}

          {candidates && (
            <p className="text-[11px] text-gray-400 mb-3">
              {candidates.count} candidate message{candidates.count === 1 ? '' : 's'}
              {' '}(structured {candidates.structured ?? 0}, vector {candidates.vector ?? 0})
              {reranked && !reranked.skipped && <> → top {reranked.kept} after rerank</>}
            </p>
          )}

          {error && (
            <div className="flex items-center gap-2 px-4 py-2 bg-[#93000a]/20 rounded-sm text-[#ffb4ab] text-xs">
              <span className="material-symbols-outlined text-base shrink-0">error</span>
              {error.error}
            </div>
          )}

          {answer && (
            <>
              <div className="bg-[#2c2834] rounded-sm p-5 mb-4">
                <h4 className="text-[10px] font-semibold uppercase tracking-widest text-gray-400 mb-2">Audit report</h4>
                <p className="text-sm text-gray-100 whitespace-pre-wrap leading-relaxed">{answer.report}</p>
              </div>
              {answer.evidence?.length > 0 && (
                <>
                  <h4 className="text-[10px] font-semibold uppercase tracking-widest text-gray-400 mb-2">
                    Cited evidence ({answer.evidence.length})
                  </h4>
                  <div className="grid grid-cols-1 lg:grid-cols-2 gap-3">
                    {answer.evidence.map(ev => (
                      <EvidenceCard key={ev.message_id} ev={ev} onOpenMessage={onOpenMessage} />
                    ))}
                  </div>
                </>
              )}
            </>
          )}
        </div>
      )}
    </section>
  )
}

// ── Risk profile widgets ─────────────────────────────────────────────────────

function ProfileCard({ title, entries, renderKey }) {
  return (
    <div className="bg-[#211e29] rounded-sm p-5">
      <h4 className="text-[10px] font-semibold uppercase tracking-widest text-gray-400 mb-3">{title}</h4>
      {!entries?.length ? (
        <p className="text-xs text-gray-400">No flagged messages in window.</p>
      ) : (
        <ul className="flex flex-col gap-2">
          {entries.slice(0, 5).map(e => (
            <li key={e.key} className="flex items-center gap-2 text-xs">
              <span className="min-w-0 flex-1 truncate">{renderKey(e)}</span>
              <span className="text-gray-400 whitespace-nowrap">{e.flagged_count} flagged</span>
              <RiskScoreBadge score={e.max_risk_score} />
            </li>
          ))}
        </ul>
      )}
    </div>
  )
}

function RiskProfiles({ onTagClick, onAgentClick }) {
  const [profiles, setProfiles] = useState(null)
  const [error, setError] = useState(null)

  useEffect(() => {
    api.dlpProfiles(30)
      .then(setProfiles)
      .catch(err => setError(err?.message || 'Failed to load risk profiles'))
  }, [])

  if (error) {
    return (
      <section className="bg-[#211e29] rounded-sm p-4 mb-6 text-xs text-[#ffb4ab]">
        Risk profiles unavailable: {error}
      </section>
    )
  }
  return (
    <section className="grid grid-cols-1 md:grid-cols-3 gap-4 mb-6">
      <ProfileCard
        title={`Top risk agents (${profiles?.window_days ?? 30}d)`}
        entries={profiles?.top_agents}
        renderKey={e => (
          <button onClick={() => onAgentClick(e.key)} className="font-mono text-[#ffb0cf] hover:underline truncate" title={e.key}>
            {e.key || '(unknown)'}
          </button>
        )}
      />
      <ProfileCard
        title="Top risk sessions"
        entries={profiles?.top_sessions}
        renderKey={e => (
          <code className="font-mono text-gray-100 truncate" title={e.key}>{truncId(e.key, 20)}</code>
        )}
      />
      <ProfileCard
        title="Top risk tags"
        entries={profiles?.top_tags}
        renderKey={e => (
          <button
            onClick={() => onTagClick(e.key)}
            className="text-[10px] font-semibold rounded-full px-2 py-0.5 bg-[#ffb4ab]/10 text-[#ffb4ab] hover:bg-[#ffb4ab]/20"
          >
            {e.key}
          </button>
        )}
      />
    </section>
  )
}

// ── Message detail drawer ────────────────────────────────────────────────────

function MessageDetail({ messageId, onClose }) {
  const [msg, setMsg] = useState(null)
  const [error, setError] = useState(null)

  useEffect(() => {
    setMsg(null)
    setError(null)
    api.dlpMessage(messageId)
      .then(setMsg)
      .catch(err => setError(err?.message || 'Failed to load message'))
  }, [messageId])

  return (
    // z-[70] keeps the drawer above the TopNav header (z-[60])
    <div className="fixed inset-0 z-[70] flex justify-end" onClick={onClose}>
      <div className="absolute inset-0 bg-black/60" />
      <div
        className="relative w-full max-w-xl h-full bg-[#211e29] overflow-y-auto p-6 flex flex-col gap-4"
        onClick={e => e.stopPropagation()}
      >
        <div className="flex items-center justify-between">
          <h3 className="text-sm font-semibold text-gray-100">Enriched message</h3>
          <button onClick={onClose} className="text-gray-400 hover:text-gray-100">
            <span className="material-symbols-outlined text-base">close</span>
          </button>
        </div>

        {error && <p className="text-xs text-[#ffb4ab]">{error}</p>}
        {!msg && !error && <p className="text-xs text-gray-400">Loading…</p>}

        {msg && (
          <>
            <div className="flex items-center gap-2 flex-wrap">
              <RoleBadge role={msg.role} />
              <RiskScoreBadge score={msg.risk_score} />
              <span className="text-[11px] text-gray-400">{formatTs(msg.event_time)}</span>
            </div>

            <RiskTagChips tags={msg.risk_tags} />

            <dl className="grid grid-cols-[auto_1fr] gap-x-4 gap-y-1.5 text-[11px]">
              {[
                ['Message ID', msg.message_id],
                ['Agent', msg.agent_id],
                ['Agent type', msg.agent_type],
                ['Deployment', msg.deployment_id],
                ['Session', msg.session_id],
                ['Run', msg.run_id],
                ['Model', msg.model],
                ['Tool', msg.tool_name],
                ['Embedding dim', msg.embedding_dim > 0 ? String(msg.embedding_dim) : 'not embedded'],
              ].filter(([, v]) => v).map(([k, v]) => (
                <div key={k} className="contents">
                  <dt className="text-gray-400 uppercase tracking-widest text-[9px] font-semibold pt-0.5">{k}</dt>
                  <dd className="font-mono text-gray-100 break-all">{v}</dd>
                </div>
              ))}
            </dl>

            {sessionHref(msg) && (
              <Link
                to={sessionHref(msg)}
                className="text-[11px] font-semibold text-[#ffb0cf] hover:underline w-fit"
              >
                Open session →
              </Link>
            )}

            {Object.keys(msg.entities || {}).length > 0 && (
              <div>
                <h4 className="text-[10px] font-semibold uppercase tracking-widest text-gray-400 mb-2">Detected entities</h4>
                <div className="flex flex-col gap-1.5">
                  {Object.entries(msg.entities).map(([type, values]) => (
                    <div key={type} className="flex items-start gap-2 text-[11px]">
                      <span className="font-semibold text-gray-400 whitespace-nowrap">{type}:</span>
                      <span className="font-mono text-[#ffb4ab] break-all">{(values || []).join(', ')}</span>
                    </div>
                  ))}
                </div>
              </div>
            )}

            <div>
              <h4 className="text-[10px] font-semibold uppercase tracking-widest text-gray-400 mb-2">Content</h4>
              <pre className="text-xs text-gray-100 whitespace-pre-wrap break-words bg-[#2c2834] rounded-sm p-4 font-sans">
                {msg.content}
              </pre>
            </div>
          </>
        )}
      </div>
    </div>
  )
}

// ── Enriched message browser ─────────────────────────────────────────────────

const PAGE_SIZE = 25
const EMPTY_FILTERS = { risk_tag: '', role: '', min_score: '', agent_id: '' }

function MessageBrowser({ filters, setFilters, onOpenMessage }) {
  const [messages, setMessages] = useState([])
  const [total, setTotal] = useState(0)
  const [page, setPage] = useState(1)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState(null)

  useEffect(() => { setPage(1) }, [filters])

  useEffect(() => {
    let cancelled = false
    setLoading(true)
    api.dlpMessages({ ...filters, page, limit: PAGE_SIZE })
      .then(data => {
        if (cancelled) return
        setMessages(data.messages || [])
        setTotal(data.total || 0)
        setError(null)
      })
      .catch(err => { if (!cancelled) setError(err?.message || 'Failed to load messages') })
      .finally(() => { if (!cancelled) setLoading(false) })
    return () => { cancelled = true }
  }, [filters, page])

  const totalPages = Math.max(1, Math.ceil(total / PAGE_SIZE))
  const hasFilters = Object.values(filters).some(v => v !== '')
  const setFilter = (key, value) => setFilters(prev => ({ ...prev, [key]: value }))

  return (
    <section className="bg-[#211e29] rounded-sm overflow-hidden">
      <div className="px-6 py-4 flex flex-wrap items-center gap-4">
        <span className="text-xs font-semibold text-gray-400 uppercase tracking-widest">
          Enriched messages
          <span className="ml-2 text-gray-400 font-normal normal-case">({total})</span>
        </span>

        <div className="flex flex-wrap items-center gap-2 ml-auto">
          <input
            type="text"
            value={filters.risk_tag}
            onChange={e => setFilter('risk_tag', e.target.value)}
            placeholder="risk tag"
            className="w-32 bg-[#2c2834] text-gray-100 text-[11px] rounded-sm px-3 py-1.5 placeholder-gray-500 focus:outline-none focus:ring-1 focus:ring-[#ffb0cf]/50"
          />
          <input
            type="text"
            value={filters.agent_id}
            onChange={e => setFilter('agent_id', e.target.value)}
            placeholder="agent id"
            className="w-32 bg-[#2c2834] text-gray-100 text-[11px] rounded-sm px-3 py-1.5 placeholder-gray-500 focus:outline-none focus:ring-1 focus:ring-[#ffb0cf]/50"
          />
          <select
            value={filters.role}
            onChange={e => setFilter('role', e.target.value)}
            className="bg-[#2c2834] text-gray-100 text-[11px] rounded-sm px-2 py-1.5 focus:outline-none"
          >
            <option value="">All roles</option>
            {ROLES.map(r => <option key={r} value={r}>{r}</option>)}
          </select>
          <select
            value={filters.min_score}
            onChange={e => setFilter('min_score', e.target.value)}
            className="bg-[#2c2834] text-gray-100 text-[11px] rounded-sm px-2 py-1.5 focus:outline-none"
          >
            <option value="">Any risk</option>
            <option value="0.3">≥ 0.3</option>
            <option value="0.5">≥ 0.5</option>
            <option value="0.7">≥ 0.7</option>
          </select>
          {hasFilters && (
            <button
              onClick={() => setFilters(EMPTY_FILTERS)}
              className="text-[11px] text-gray-400 hover:text-gray-100"
            >
              Clear
            </button>
          )}
        </div>
      </div>

      <div className="overflow-auto">
        <table className="w-full text-left border-collapse">
          <thead className="bg-[#2c2834] sticky top-0 z-10">
            <tr>
              {['Time', 'Role', 'Agent', 'Risk', 'Tags', 'Content'].map(h => (
                <th key={h} className="px-6 py-3 text-[10px] font-bold text-gray-400 uppercase tracking-widest whitespace-nowrap">
                  {h}
                </th>
              ))}
            </tr>
          </thead>
          <tbody className="divide-y divide-[#574149]/10">
            {loading ? (
              <tr><td colSpan={6} className="px-6 py-12 text-center text-gray-400 text-xs">Loading messages…</td></tr>
            ) : error ? (
              <tr><td colSpan={6} className="px-6 py-12 text-center text-xs text-[#ffb4ab]">{error}</td></tr>
            ) : messages.length === 0 ? (
              <tr>
                <td colSpan={6} className="px-6 py-12 text-center text-gray-400 text-xs">
                  {hasFilters ? 'No messages match the current filters.' : 'No enriched messages yet.'}
                </td>
              </tr>
            ) : messages.map(m => (
              <tr
                key={m.message_id}
                onClick={() => onOpenMessage(m.message_id)}
                className="hover:bg-[#2c2834] transition-colors cursor-pointer"
              >
                <td className="px-6 py-3 text-[11px] text-gray-400 whitespace-nowrap">{formatTs(m.event_time)}</td>
                <td className="px-6 py-3"><RoleBadge role={m.role} /></td>
                <td className="px-6 py-3">
                  <code className="text-[11px] font-mono text-[#ffb0cf] truncate block max-w-[140px]" title={m.agent_id}>
                    {m.agent_id || '—'}
                  </code>
                </td>
                <td className="px-6 py-3"><RiskScoreBadge score={m.risk_score} /></td>
                <td className="px-6 py-3 max-w-[200px]">
                  <RiskTagChips tags={m.risk_tags} onClick={tag => setFilter('risk_tag', tag)} />
                </td>
                <td className="px-6 py-3 max-w-md">
                  <p className="text-xs text-gray-100 truncate" title={m.content}>{m.content}</p>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>

      {totalPages > 1 && (
        <div className="px-6 py-3 flex items-center justify-end gap-3 text-[11px] text-gray-400">
          <button
            onClick={() => setPage(p => Math.max(1, p - 1))}
            disabled={page <= 1}
            className="px-2 py-1 rounded-sm hover:bg-[#2c2834] disabled:opacity-40"
          >
            ← Prev
          </button>
          <span>Page {page} of {totalPages}</span>
          <button
            onClick={() => setPage(p => Math.min(totalPages, p + 1))}
            disabled={page >= totalPages}
            className="px-2 py-1 rounded-sm hover:bg-[#2c2834] disabled:opacity-40"
          >
            Next →
          </button>
        </div>
      )}
    </section>
  )
}

// ── Page ─────────────────────────────────────────────────────────────────────

export default function SemanticDLP() {
  const [status, setStatus] = useState(null)
  const [statusError, setStatusError] = useState(null)
  const [provisioning, setProvisioning] = useState(false)
  const [provisionError, setProvisionError] = useState(null)
  const [filters, setFilters] = useState(EMPTY_FILTERS)
  const [openMessageId, setOpenMessageId] = useState(null)

  const loadStatus = useCallback(() => {
    api.dlpStatus()
      .then(s => { setStatus(s); setStatusError(null) })
      .catch(err => setStatusError(err?.message || 'Failed to load DLP status'))
  }, [])

  useEffect(() => { loadStatus() }, [loadStatus])

  // Enable (if needed) then provision. dlp.enabled is a hot config key, so
  // flipping it applies to the live server without a restart; syncRules() then
  // runs InitDLP to create the stream, UDFs, and MV.
  async function provision() {
    setProvisioning(true)
    setProvisionError(null)
    try {
      if (!status?.enabled) {
        await api.updateConfig({ 'dlp.enabled': true })
      }
      await api.syncRules()
      loadStatus()
    } catch (err) {
      setProvisionError(err?.message || 'Provisioning failed')
    } finally {
      setProvisioning(false)
    }
  }

  const provisioned = status?.enabled && status?.stream_exists && status?.mv_exists
    && Object.values(status?.udfs || {}).every(Boolean)

  return (
    <div className="min-h-screen bg-[#15121d] text-gray-100">
      <Sidebar />
      <TopNav />

      <main className="pl-sidebar pt-16 pb-8 min-h-screen">
        <div className="p-8">
          <PageHeader
            title="Semantic DLP"
            subtitle="Ask natural-language questions about sensitive data exposure across agent conversations, backed by entity extraction, risk scoring, and vector search."
            actions={status && provisioned && (
              <span className="text-[11px] text-gray-400 self-end" title="Rows with embeddings / total enriched rows">
                {status.total_rows} messages · {Math.round((status.embedding_coverage || 0) * 100)}% embedded · {status.flagged_rows} flagged
              </span>
            )}
          />

          {statusError ? (
            <section className="bg-[#211e29] rounded-sm p-6 flex items-center gap-2 text-xs text-[#ffb4ab]">
              <span className="material-symbols-outlined text-base">error</span>
              {statusError}
            </section>
          ) : !status ? (
            <p className="text-xs text-gray-400">Loading…</p>
          ) : !provisioned ? (
            <SetupCTA
              status={status}
              onProvision={provision}
              provisioning={provisioning}
              provisionError={provisionError}
            />
          ) : (
            <>
              <AskPanel onOpenMessage={setOpenMessageId} />
              <RiskProfiles
                onTagClick={tag => setFilters(prev => ({ ...prev, risk_tag: tag }))}
                onAgentClick={agentId => setFilters(prev => ({ ...prev, agent_id: agentId }))}
              />
              <MessageBrowser
                filters={filters}
                setFilters={setFilters}
                onOpenMessage={setOpenMessageId}
              />
            </>
          )}
        </div>
      </main>

      {openMessageId && (
        <MessageDetail messageId={openMessageId} onClose={() => setOpenMessageId(null)} />
      )}
    </div>
  )
}
