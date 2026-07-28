import { useState, useEffect, useRef, useCallback } from 'react'
import { Link } from 'react-router-dom'
import Sidebar from '../components/Sidebar'
import TopNav from '../components/TopNav'
import PageHeader from '../components/PageHeader'
import Markdown from '../components/Markdown'
import { useAuth } from '../context/AuthContext'
import { writeKey } from '../lib/perm'
import { api } from '../api/client'

let localSeq = 0
function localId(prefix) {
  localSeq += 1
  return `${prefix}-${Date.now()}-${localSeq}`
}

// prettyJSON best-effort pretty-prints a JSON string for display; falls back
// to the raw string when it isn't valid JSON (tool args/results are opaque
// strings from the model/tool's point of view).
function prettyJSON(text) {
  if (!text) return ''
  try {
    return JSON.stringify(JSON.parse(text), null, 2)
  } catch {
    return text
  }
}

// toolArgsFromRow unwraps a replayed role="tool" SentryMessage's tool_args
// column, which the backend persists as a toolCallMeta JSON blob
// ({id,name,args}) rather than the raw args string (see loop.go's
// toolCallMeta) -- pull the nested `args` back out for display, falling back
// to the raw column when it isn't that shape.
function toolArgsFromRow(raw) {
  if (!raw) return ''
  try {
    const meta = JSON.parse(raw)
    if (meta && typeof meta === 'object' && typeof meta.args === 'string') return meta.args
  } catch { /* not toolCallMeta JSON, use as-is */ }
  return raw
}

// approvalFromRow pulls the persisted GatedWrite approval audit record
// ({decision, decided_by, reason}) out of a replayed tool row's meta, if any.
function approvalFromRow(raw) {
  if (!raw) return null
  try {
    const meta = JSON.parse(raw)
    if (meta && typeof meta === 'object' && meta.approval) return meta.approval
  } catch { /* not toolCallMeta JSON */ }
  return null
}

// approvalStatusLine renders one compact line describing a resolved
// GatedWrite decision (used in both the live card and replayed tool rows).
function approvalStatusLine(a) {
  if (!a || !a.decision) return null
  const label = {
    approved: `approved by ${a.decided_by || a.decidedBy || 'operator'}`,
    denied: `denied by ${a.decided_by || a.decidedBy || 'operator'}`,
    timeout: 'timed out — not executed',
    abandoned: 'abandoned — not executed',
  }[a.decision] || a.decision
  const reason = a.reason ? ` — ${a.reason}` : ''
  const cls = a.decision === 'approved' ? 'text-primary' : 'text-error'
  return <span className={`text-[10px] ${cls}`}>write {label}{reason}</span>
}

// ApprovalCard renders a GatedWrite proposal: the tool + verbatim args the
// model wants to run, with Approve / Deny while pending, then the decision
// state and (if approved) the execution result.
function ApprovalCard({ item, onDecide }) {
  const pending = !item.decision
  return (
    <div className="bg-surface-container rounded-sm text-xs max-w-2xl">
      <div className="px-4 py-2.5 flex items-center gap-2 text-gray-100">
        <span className="material-symbols-outlined text-sm shrink-0 text-primary">gavel</span>
        <span className="font-mono text-[11px]">{item.name || 'write action'}</span>
        {pending
          ? <span className="text-[10px] text-primary animate-pulse ml-1">awaiting your approval…</span>
          : <span className="ml-1">{approvalStatusLine(item)}</span>}
      </div>
      <div className="px-4 pb-3 flex flex-col gap-2">
        <div>
          <p className="text-[9px] font-semibold uppercase tracking-widest text-gray-400 mb-1">Proposed action</p>
          <code className="block bg-surface-container-high rounded-sm p-2.5 text-[11px] text-gray-100 whitespace-pre-wrap break-words font-mono">
            {prettyJSON(item.args) || '(no arguments)'}
          </code>
        </div>
        {pending && (
          <div className="flex items-center gap-2">
            <button
              onClick={() => onDecide(item, true)}
              disabled={item.deciding}
              className="px-4 py-1.5 text-xs font-semibold rounded-sm bg-gradient-to-br from-[#ffb0cf] to-[#f256a3] text-[#3d0023] hover:scale-[0.98] transition-transform disabled:opacity-50"
            >
              Approve
            </button>
            <button
              onClick={() => onDecide(item, false)}
              disabled={item.deciding}
              className="px-4 py-1.5 text-xs font-semibold rounded-sm bg-surface-container-high text-gray-100 hover:text-error disabled:opacity-50"
            >
              Deny
            </button>
          </div>
        )}
        {item.decision === 'approved' && item.result != null && (
          <div>
            <p className="text-[9px] font-semibold uppercase tracking-widest text-gray-400 mb-1">Result</p>
            <code className="block bg-surface-container-high rounded-sm p-2.5 text-[11px] text-gray-100 whitespace-pre-wrap break-words font-mono max-h-64 overflow-y-auto">
              {prettyJSON(item.result)}
            </code>
          </div>
        )}
      </div>
    </div>
  )
}

function ToolCard({ item }) {
  return (
    <details className="bg-surface-container rounded-sm text-xs max-w-2xl">
      <summary className="cursor-pointer select-none list-none px-4 py-2.5 flex items-center gap-2 text-gray-400 hover:text-gray-100">
        <span className="material-symbols-outlined text-sm shrink-0">settings</span>
        <span className="font-mono text-[11px]">{item.name || 'tool'}</span>
        {item.pending && <span className="text-[10px] text-gray-400 animate-pulse ml-1">running…</span>}
        {item.approval && <span className="ml-1">{approvalStatusLine(item.approval)}</span>}
        {item.truncated && (
          <span className="ml-auto text-[9px] font-semibold uppercase tracking-widest rounded-full px-2 py-0.5 bg-error/10 text-error whitespace-nowrap">
            truncated
          </span>
        )}
      </summary>
      <div className="px-4 pb-3 flex flex-col gap-2">
        {item.args && (
          <div>
            <p className="text-[9px] font-semibold uppercase tracking-widest text-gray-400 mb-1">Args</p>
            <code className="block bg-surface-container-high rounded-sm p-2.5 text-[11px] text-gray-100 whitespace-pre-wrap break-words font-mono">
              {prettyJSON(item.args)}
            </code>
          </div>
        )}
        {item.result != null && (
          <div>
            <p className="text-[9px] font-semibold uppercase tracking-widest text-gray-400 mb-1">Result</p>
            <code className="block bg-surface-container-high rounded-sm p-2.5 text-[11px] text-gray-100 whitespace-pre-wrap break-words font-mono max-h-64 overflow-y-auto">
              {prettyJSON(item.result)}
            </code>
          </div>
        )}
      </div>
    </details>
  )
}

// ThinkingCard renders streamed model reasoning (anthropic adaptive
// thinking, summarized display) as a dim, collapsed-by-default block.
function ThinkingCard({ item }) {
  return (
    <details className="max-w-2xl">
      <summary className="cursor-pointer select-none list-none text-[11px] text-gray-500 flex items-center gap-1.5">
        <span className="material-symbols-outlined text-sm">psychology</span>
        Reasoning{item.pending && <span className="animate-pulse">…</span>}
      </summary>
      <p className="mt-1 pl-5 text-[11px] text-gray-500 italic whitespace-pre-wrap break-words">{item.content}</p>
    </details>
  )
}

function MessageBubble({ item }) {
  if (item.role === 'user') {
    return (
      <div className="flex justify-end">
        <div className="max-w-2xl bg-surface-container-high rounded-sm px-4 py-2.5 text-sm text-gray-100 whitespace-pre-wrap break-words">
          {item.content}
        </div>
      </div>
    )
  }
  if (item.role === 'error') {
    return (
      <div className="flex items-center gap-2 px-4 py-2.5 bg-error-container/20 rounded-sm text-error text-xs max-w-2xl">
        <span className="material-symbols-outlined text-base shrink-0">error</span>
        {item.content}
      </div>
    )
  }
  // assistant — render Markdown (headings, GFM tables, lists, code); the
  // pending cursor follows the rendered content while streaming.
  return (
    <div className="max-w-2xl">
      <Markdown>{item.content}</Markdown>
      {item.pending && <span className="inline-block w-1.5 h-3.5 bg-primary/70 ml-0.5 align-text-bottom animate-pulse" />}
    </div>
  )
}

const EMPTY_STATE_TEXT = 'Ask Sentry about your agents, threats, cost, rules, or DLP findings.'

export default function Sentry() {
  const { can } = useAuth()
  const canWrite = can(writeKey('sentry')) // false for a view-only role
  const [status, setStatus] = useState(null)
  const [statusError, setStatusError] = useState(null)

  const [conversations, setConversations] = useState([])
  const [conversationsError, setConversationsError] = useState(null)

  const [activeId, setActiveId] = useState(null)
  const [messages, setMessages] = useState([])
  const [loadingConv, setLoadingConv] = useState(false)
  const [convError, setConvError] = useState(null)

  const [input, setInput] = useState('')
  const [streaming, setStreaming] = useState(false)
  const [provisioning, setProvisioning] = useState(false)

  const [editingId, setEditingId] = useState(null)
  const [editTitle, setEditTitle] = useState('')

  const abortRef = useRef(null)
  const titleRefreshRef = useRef(null)
  const openAssistantIdRef = useRef(null)
  const openThinkingIdRef = useRef(null)
  const scrollRef = useRef(null)
  const activeIdRef = useRef(null)
  useEffect(() => { activeIdRef.current = activeId }, [activeId])
  // Monotonic token bumped on every conversation switch / new conversation, so
  // a slow conversation-fetch that resolves out of order can be ignored (#237).
  const convGenRef = useRef(0)

  const loadConversations = useCallback(() => {
    api.sentryConversations()
      .then(d => { setConversations(d.conversations || []); setConversationsError(null) })
      .catch(err => setConversationsError(err?.message || 'Failed to load conversations'))
  }, [])

  useEffect(() => {
    api.sentryStatus()
      .then(s => { setStatus(s); setStatusError(null) })
      .catch(err => setStatusError(err?.message || 'Failed to load Sentry status'))
  }, [])

  useEffect(() => {
    if (status?.enabled) loadConversations()
  }, [status, loadConversations])

  useEffect(() => () => {
    abortRef.current?.abort()
    if (titleRefreshRef.current) clearTimeout(titleRefreshRef.current)
  }, [])

  useEffect(() => {
    const el = scrollRef.current
    if (el) el.scrollTop = el.scrollHeight
  }, [messages])

  function openConversation(id) {
    if (streaming || id === activeId) return
    abortRef.current?.abort()
    const gen = ++convGenRef.current
    setActiveId(id)
    setLoadingConv(true)
    setConvError(null)
    api.sentryConversation(id)
      .then(d => {
        if (gen !== convGenRef.current) return // a newer switch superseded this fetch
        const msgs = (d.messages || [])
          .slice()
          .sort((a, b) => (a.seq ?? 0) - (b.seq ?? 0))
          .flatMap(m => {
            if (m.role === 'tool') {
              return [{
                id: `s${m.seq}`, role: 'tool', name: m.tool_name,
                args: toolArgsFromRow(m.tool_args), result: m.tool_result,
                approval: approvalFromRow(m.tool_args),
                truncated: false, pending: false,
              }]
            }
            if (m.role === 'assistant' && !m.content) return [] // tool-only turn, nothing to show as text
            return [{ id: `s${m.seq}`, role: m.role, content: m.content }]
          })
        setMessages(msgs)
      })
      .catch(err => {
        if (gen !== convGenRef.current) return // superseded — swallow the stale error too
        setConvError(err?.message || 'Failed to load conversation')
      })
      .finally(() => {
        if (gen === convGenRef.current) setLoadingConv(false)
      })
  }

  function newConversation() {
    if (streaming) return
    abortRef.current?.abort()
    convGenRef.current++ // invalidate any in-flight conversation fetch
    setActiveId(null)
    setMessages([])
    setConvError(null)
  }

  async function deleteConversation(id, e) {
    e.stopPropagation()
    if (!window.confirm('Delete this conversation? It will be removed from your list.')) return
    try {
      await api.sentryDelete(id)
      if (activeId === id) newConversation()
      loadConversations()
    } catch (err) {
      setConversationsError(err?.message || 'Failed to delete conversation')
    }
  }

  function startRename(c, e) {
    e.stopPropagation()
    setEditingId(c.conversation_id)
    setEditTitle(c.title || '')
  }

  async function commitRename() {
    const id = editingId
    const title = editTitle.trim()
    setEditingId(null)
    if (!id || !title) return
    try {
      await api.sentryRename(id, title)
      loadConversations()
    } catch (err) {
      setConversationsError(err?.message || 'Failed to rename conversation')
    }
  }

  // decideApproval resolves one pending GatedWrite proposal. The card's
  // state flips when the loop's approval_result event echoes back — the
  // local `deciding` flag only debounces the buttons.
  async function decideApproval(item, approve) {
    setMessages(prev => prev.map(m => m.id === item.id ? { ...m, deciding: true } : m))
    try {
      await api.sentryDecideApproval(item.proposalId, approve)
    } catch (err) {
      setMessages(prev => prev.map(m => m.id === item.id ? { ...m, deciding: false } : m))
      setConvError(err?.message || 'Failed to submit decision')
    }
  }

  // finalizeOpenBubble clears pending:false on the currently-open assistant
  // bubble (if any) and nulls the ref. Call this everywhere the open
  // assistant bubble is being closed/abandoned -- otherwise its pending
  // cursor is left dangling (permanent pulsing cursor).
  function finalizeOpenBubble() {
    const openId = openAssistantIdRef.current
    if (!openId) return
    setMessages(prev => prev.map(m => m.id === openId ? { ...m, pending: false } : m))
    openAssistantIdRef.current = null
  }

  function finalizeOpenThinking() {
    const openId = openThinkingIdRef.current
    if (!openId) return
    setMessages(prev => prev.map(m => m.id === openId ? { ...m, pending: false } : m))
    openThinkingIdRef.current = null
  }

  async function send() {
    const text = input.trim()
    if (!text || streaming) return
    setInput('')
    setConvError(null)
    setMessages(prev => [...prev, { id: localId('user'), role: 'user', content: text }])
    openAssistantIdRef.current = null
    openThinkingIdRef.current = null // clear any dangling ref from a prior aborted turn (#237)

    const ctrl = new AbortController()
    abortRef.current = ctrl
    setStreaming(true)
    try {
      for await (const ev of api.sentryChat(text, { conversationId: activeIdRef.current, signal: ctrl.signal })) {
        if (!ev?.event) continue
        switch (ev.event) {
          case 'meta':
            if (ev.conversation_id) setActiveId(ev.conversation_id)
            break

          case 'thinking':
            setMessages(prev => {
              const openId = openThinkingIdRef.current
              if (openId) {
                return prev.map(m => m.id === openId ? { ...m, content: (m.content || '') + (ev.text || '') } : m)
              }
              const id = localId('thinking')
              openThinkingIdRef.current = id
              return [...prev, { id, role: 'thinking', content: ev.text || '', pending: true }]
            })
            break

          case 'token':
            finalizeOpenThinking() // reasoning ended; answer text begins
            setMessages(prev => {
              const openId = openAssistantIdRef.current
              if (openId) {
                return prev.map(m => m.id === openId ? { ...m, content: (m.content || '') + (ev.text || '') } : m)
              }
              const id = localId('assistant')
              openAssistantIdRef.current = id
              return [...prev, { id, role: 'assistant', content: ev.text || '', pending: true }]
            })
            break

          case 'tool_call':
            finalizeOpenBubble() // a fresh assistant bubble starts after this
            finalizeOpenThinking()
            setMessages(prev => prev.some(m => m.id === ev.id)
              ? prev
              : [...prev, { id: ev.id, role: 'tool', name: ev.name, args: ev.args, result: null, truncated: false, pending: true }])
            break

          case 'approval_request':
            finalizeOpenBubble()
            finalizeOpenThinking() // stop the reasoning ellipsis during the (possibly long) approval wait
            setMessages(prev => prev.some(m => m.id === ev.id)
              ? prev
              : [...prev, {
                  id: ev.id, role: 'approval', proposalId: ev.proposal_id,
                  name: ev.name, args: ev.args, decision: null, result: null,
                  truncated: false, deciding: false,
                }])
            break

          case 'approval_result':
            setMessages(prev => prev.map(m => m.id === ev.id
              ? { ...m, decision: ev.decision, decidedBy: ev.decided_by, reason: ev.message, deciding: false }
              : m))
            break

          case 'tool_result':
            setMessages(prev => prev.map(m => m.id === ev.id
              ? { ...m, result: ev.result, truncated: !!ev.truncated, pending: false }
              : m))
            break

          case 'done':
            finalizeOpenBubble()
            finalizeOpenThinking()
            break

          case 'error':
            finalizeOpenBubble()
            finalizeOpenThinking()
            setMessages(prev => [...prev, { id: localId('error'), role: 'error', content: ev.message || 'Sentry error' }])
            break

          default:
            break
        }
      }
    } catch (err) {
      finalizeOpenBubble()
      finalizeOpenThinking() // never leave the reasoning card pulsing after an error/abort (#237)
      if (err?.name !== 'AbortError') {
        setMessages(prev => [...prev, { id: localId('error'), role: 'error', content: err?.message || 'Chat request failed' }])
      }
    } finally {
      setStreaming(false)
      loadConversations() // picks up the new/updated conversation in the rail
      // The server may auto-title the conversation via the LLM shortly after
      // the turn completes; refresh once more to pick the new name up.
      if (titleRefreshRef.current) clearTimeout(titleRefreshRef.current)
      titleRefreshRef.current = setTimeout(loadConversations, 5000)
    }
  }

  function handleKeyDown(e) {
    // Plain Enter must NOT send: it confirms IME composition (e.g. Chinese
    // input) and inserts newlines. Send via the button or Ctrl/Cmd+Enter (#238).
    if (e.key === 'Enter' && (e.ctrlKey || e.metaKey) && !e.nativeEvent?.isComposing) {
      e.preventDefault()
      send()
    }
  }

  const provisionWarning = status?.enabled && status.provisioned === false
    ? (status.config_error || status.tables_check_error || 'Sentry streams are not provisioned yet.')
    : null

  // provisionNow recovers a deployment where Sentry was enabled without its
  // streams being provisioned (#226): POST /api/rules/sync reconciles all
  // provisioning (incl. InitSentry), then status + rail are refreshed.
  async function provisionNow() {
    setProvisioning(true)
    try {
      await api.syncRules()
      const s = await api.sentryStatus()
      setStatus(s)
      setStatusError(null)
      loadConversations()
    } catch (err) {
      setStatusError(err?.message || 'Provisioning failed')
    } finally {
      setProvisioning(false)
    }
  }

  // needsChatModelHint: Sentry is enabled but has no usable chat endpoint yet
  // (no base_url configured, or the config row failed to load) — point the
  // operator at the runtime editor rather than leaving a silent empty chat.
  const needsChatModelHint = status?.enabled && (status.base_url_set === false || !!status.config_error)

  return (
    <div className="min-h-screen bg-surface text-gray-100">
      <Sidebar />
      <TopNav />

      <main className="pl-sidebar pt-16 h-screen overflow-hidden flex flex-col">
        <div className="px-8 pt-8 shrink-0">
          <PageHeader
            title="Sentry"
            subtitle="Chat with AgentGuard's own security analyst agent — ask about threats, rules, cost, and DLP findings across your fleet."
            actions={status?.enabled && status?.model && (
              <span className="text-[11px] text-gray-400 self-end font-mono">{status.model}</span>
            )}
          />
        </div>

        {statusError ? (
          <section className="mx-8 mb-8 bg-surface-container rounded-sm p-6 flex items-center gap-2 text-xs text-error shrink-0">
            <span className="material-symbols-outlined text-base">error</span>
            {statusError}
          </section>
        ) : !status ? (
          <p className="mx-8 text-xs text-gray-400">Loading…</p>
        ) : !status.enabled ? (
          <div className="flex-1 flex items-center justify-center px-8 pb-8">
            <div className="max-w-md text-center bg-surface-container rounded-sm p-8 flex flex-col items-center gap-3">
              <span className="material-symbols-outlined text-4xl text-gray-400">shield_person</span>
              <h3 className="text-sm font-semibold text-gray-100">Sentry is disabled</h3>
              <p className="text-xs text-gray-400">
                Enable <code className="font-mono text-primary">sentry.enabled</code> in Settings and provision to start chatting with Sentry.
              </p>
            </div>
          </div>
        ) : (
          <div className="flex-1 flex flex-col min-h-0 px-8 pb-8 gap-3">
            {provisionWarning && (
              <div className="flex items-center gap-3 px-4 py-2.5 bg-error-container/20 rounded-sm text-error text-xs shrink-0">
                <span className="material-symbols-outlined text-base shrink-0">warning</span>
                <span className="flex-1 min-w-0">{provisionWarning}</span>
                <button
                  onClick={provisionNow}
                  disabled={provisioning}
                  className="shrink-0 px-3 py-1.5 text-xs font-semibold rounded-sm bg-gradient-to-br from-[#ffb0cf] to-[#f256a3] text-[#3d0023] hover:scale-[0.98] transition-transform disabled:opacity-50"
                >
                  {provisioning ? 'Provisioning…' : 'Provision now'}
                </button>
              </div>
            )}
            <div className="flex-1 flex min-h-0 gap-4">
              {/* Conversation rail */}
              <aside className="w-64 shrink-0 bg-surface-container rounded-sm flex flex-col overflow-hidden">
                <div className="p-3 shrink-0">
                  <button
                    onClick={newConversation}
                    disabled={streaming}
                    className="w-full px-4 py-2.5 text-xs font-semibold rounded-sm bg-gradient-to-br from-[#ffb0cf] to-[#f256a3] text-[#3d0023] hover:scale-[0.98] transition-transform disabled:opacity-50 flex items-center justify-center gap-2"
                  >
                    <span className="material-symbols-outlined text-base">add</span>
                    New conversation
                  </button>
                </div>
                <div className="flex-1 overflow-y-auto px-2 pb-3 flex flex-col gap-1 custom-scrollbar">
                  {conversationsError ? (
                    <p className="px-2 py-2 text-[11px] text-error">{conversationsError}</p>
                  ) : conversations.length === 0 ? (
                    <p className="px-2 py-2 text-[11px] text-gray-400">No conversations yet.</p>
                  ) : conversations.map(c => (
                    <div
                      key={c.conversation_id}
                      onClick={() => openConversation(c.conversation_id)}
                      className={`group flex items-center gap-2 cursor-pointer px-3 py-2.5 rounded-sm text-xs transition-colors ${
                        c.conversation_id === activeId
                          ? 'bg-surface-container-high text-gray-100'
                          : 'text-gray-400 hover:bg-surface-container-high hover:text-gray-100'
                      }`}
                    >
                      {editingId === c.conversation_id ? (
                        <input
                          autoFocus
                          value={editTitle}
                          onChange={e => setEditTitle(e.target.value)}
                          onClick={e => e.stopPropagation()}
                          onBlur={commitRename}
                          onKeyDown={e => {
                            if (e.key === 'Enter') { e.preventDefault(); commitRename() }
                            if (e.key === 'Escape') setEditingId(null)
                          }}
                          maxLength={80}
                          className="flex-1 min-w-0 bg-surface-container-highest text-gray-100 text-xs rounded-sm px-1.5 py-0.5 outline-none"
                        />
                      ) : (
                        <>
                          <span className="flex-1 min-w-0 truncate">{c.title || 'Untitled conversation'}</span>
                          <button
                            onClick={e => startRename(c, e)}
                            title="Rename conversation"
                            className="opacity-0 group-hover:opacity-100 text-gray-400 hover:text-gray-100 shrink-0"
                          >
                            <span className="material-symbols-outlined text-sm">edit</span>
                          </button>
                          <button
                            onClick={e => deleteConversation(c.conversation_id, e)}
                            title="Delete conversation"
                            className="opacity-0 group-hover:opacity-100 text-gray-400 hover:text-error shrink-0"
                          >
                            <span className="material-symbols-outlined text-sm">delete</span>
                          </button>
                        </>
                      )}
                    </div>
                  ))}
                </div>
              </aside>

              {/* Chat pane */}
              <section className="flex-1 flex flex-col min-w-0 bg-surface-container-high rounded-sm overflow-hidden">
                <div ref={scrollRef} className="flex-1 overflow-y-auto px-6 py-6 flex flex-col gap-4 custom-scrollbar">
                  {loadingConv ? (
                    <p className="text-xs text-gray-400">Loading conversation…</p>
                  ) : convError ? (
                    <p className="text-xs text-error">{convError}</p>
                  ) : messages.length === 0 ? (
                    <div className="flex flex-col gap-1.5">
                      <p className="text-xs text-gray-400">{EMPTY_STATE_TEXT}</p>
                      {needsChatModelHint && (
                        <p className="text-[11px] text-amber-400">
                          Sentry has no chat model configured — set it in{' '}
                          <Link to="/settings" className="underline hover:text-amber-300">Settings → Sentry Agent</Link>.
                        </p>
                      )}
                    </div>
                  ) : (
                    messages.map(item => item.role === 'tool'
                      ? <ToolCard key={item.id} item={item} />
                      : item.role === 'approval'
                        ? <ApprovalCard key={item.id} item={item} onDecide={decideApproval} />
                        : item.role === 'thinking'
                          ? <ThinkingCard key={item.id} item={item} />
                          : <MessageBubble key={item.id} item={item} />
                    )
                  )}
                </div>

                <div className="p-4 bg-surface-container shrink-0">
                  {canWrite ? (
                    <>
                      <div className="flex items-end gap-3">
                        <textarea
                          value={input}
                          onChange={e => setInput(e.target.value)}
                          onKeyDown={handleKeyDown}
                          disabled={streaming}
                          rows={2}
                          placeholder="Ask Sentry…"
                          className="flex-1 bg-surface-container-high text-gray-100 text-sm rounded-sm px-4 py-2.5 placeholder-gray-500 resize-none focus:outline-none focus:ring-1 focus:ring-primary/50 disabled:opacity-60"
                        />
                        <button
                          onClick={send}
                          disabled={streaming || !input.trim()}
                          className="px-5 py-2.5 text-xs font-semibold rounded bg-gradient-to-br from-[#ffb0cf] to-[#f256a3] text-[#3d0023] hover:scale-95 transition-transform disabled:opacity-50 whitespace-nowrap"
                        >
                          {streaming ? 'Sending…' : 'Send'}
                        </button>
                      </div>
                      <p className="text-[10px] text-gray-400 mt-1.5">Ctrl/Cmd+Enter to send · Enter for a new line</p>
                    </>
                  ) : (
                    <p className="text-xs text-gray-400 text-center py-2">
                      You have read-only access to Sentry. Ask an administrator for write access to start conversations.
                    </p>
                  )}
                </div>
              </section>
            </div>
          </div>
        )}
      </main>
    </div>
  )
}
