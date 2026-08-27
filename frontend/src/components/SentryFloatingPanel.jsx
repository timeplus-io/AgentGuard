import { useEffect, useRef, useState } from 'react'
import { Link, useLocation } from 'react-router-dom'
import { useTranslation } from 'react-i18next'
import Markdown from './Markdown'
import { useAuth } from '../context/AuthContext'
import { uiKey, writeKey } from '../lib/perm'
import { api } from '../api/client'
import { OPEN_TABBY_EVENT, isTabbyPage, TABBY_USER_BUBBLE_CLASS, TABBY_USER_BUBBLE_HOVER_CLASS } from '../lib/tabby'
import TabbyAvatar from './TabbyAvatar'

let localSeq = 0
function localId(prefix) {
  localSeq += 1
  return `${prefix}-${Date.now()}-${localSeq}`
}

const PANEL_CHAT_STORAGE_KEY = 'agentguard:sentry-floating-chat'

function loadSavedChat() {
  if (typeof window === 'undefined') return { activeId: null, messages: [], input: '' }
  try {
    const raw = window.sessionStorage.getItem(PANEL_CHAT_STORAGE_KEY)
    if (!raw) return { activeId: null, messages: [], input: '' }
    const saved = JSON.parse(raw)
    return {
      activeId: typeof saved.activeId === 'string' ? saved.activeId : null,
      messages: Array.isArray(saved.messages)
        ? saved.messages.map(m => ({ ...m, pending: false, deciding: false }))
        : [],
      input: typeof saved.input === 'string' ? saved.input : '',
    }
  } catch {
    return { activeId: null, messages: [], input: '' }
  }
}

function saveChat(activeId, messages, input) {
  if (typeof window === 'undefined') return
  window.sessionStorage.setItem(PANEL_CHAT_STORAGE_KEY, JSON.stringify({ activeId, messages, input }))
}

function prettyJSON(text) {
  if (!text) return ''
  try {
    return JSON.stringify(JSON.parse(text), null, 2)
  } catch {
    return text
  }
}

const SUGGESTION_GROUPS = ['rules', 'agents', 'threats', 'cost', 'investigate', 'dlp', 'settings', 'access']

function suggestionGroupForPath(pathname) {
  return SUGGESTION_GROUPS.find(group => pathname.startsWith(`/${group}`)) || 'default'
}

function MessageBubble({ item }) {
  if (item.role === 'user') {
    return (
      <div className="flex justify-end">
        <div className={`max-w-[85%] ${TABBY_USER_BUBBLE_CLASS} rounded-sm px-3 py-2 text-sm whitespace-pre-wrap break-words`}>
          {item.content}
        </div>
      </div>
    )
  }
  if (item.role === 'error') {
    return (
      <div className="flex items-center gap-2 px-3 py-2 bg-error-container/20 rounded-sm text-error text-xs">
        <span className="material-symbols-outlined text-base shrink-0">error</span>
        {item.content}
      </div>
    )
  }
  return (
    <div className="text-sm text-gray-100">
      <Markdown>{item.content}</Markdown>
      {item.pending && <span className="inline-block w-1.5 h-3.5 bg-primary/70 ml-0.5 align-text-bottom animate-pulse" />}
    </div>
  )
}

function ToolCard({ item }) {
  const { t } = useTranslation('sentry')
  return (
    <details className="bg-surface-container rounded-sm text-xs border border-gray-700">
      <summary className="cursor-pointer select-none list-none px-3 py-2 flex items-center gap-2 text-gray-400 hover:text-gray-100">
        <span className="material-symbols-outlined text-sm shrink-0">settings</span>
        <span className="font-mono text-[11px] truncate">{item.name || t('card.toolFallbackName')}</span>
        {item.pending && <span className="text-[10px] text-gray-400 animate-pulse ml-auto">{t('card.running')}</span>}
      </summary>
      <div className="px-3 pb-3 flex flex-col gap-2">
        {item.args && (
          <code className="block sentry-code-surface rounded-sm p-2 text-[11px] text-gray-100 whitespace-pre-wrap break-words font-mono max-h-40 overflow-y-auto">
            {prettyJSON(item.args)}
          </code>
        )}
        {item.result != null && (
          <code className="block sentry-code-surface rounded-sm p-2 text-[11px] text-gray-100 whitespace-pre-wrap break-words font-mono max-h-40 overflow-y-auto">
            {prettyJSON(item.result)}
          </code>
        )}
      </div>
    </details>
  )
}

function ApprovalCard({ item, onDecide }) {
  const { t } = useTranslation('sentry')
  return (
    <div className="bg-surface-container rounded-sm text-xs border border-gray-700">
      <div className="px-3 py-2 flex items-center gap-2 text-gray-100">
        <span className="material-symbols-outlined text-sm shrink-0 text-primary">gavel</span>
        <span className="font-mono text-[11px] truncate">{item.name || t('approval.defaultName')}</span>
        {!item.decision && <span className="text-[10px] text-primary animate-pulse ml-auto">{t('approval.awaiting')}</span>}
      </div>
      <div className="px-3 pb-3 flex flex-col gap-2">
        <code className="block sentry-code-surface rounded-sm p-2 text-[11px] text-gray-100 whitespace-pre-wrap break-words font-mono max-h-40 overflow-y-auto">
          {prettyJSON(item.args) || t('approval.noArguments')}
        </code>
        {!item.decision ? (
          <div className="flex items-center gap-2">
            <button
              onClick={() => onDecide(item, true)}
              disabled={item.deciding}
              className="px-3 py-1.5 text-xs font-semibold rounded-sm bg-pink-500 text-white hover:bg-pink-400 disabled:opacity-50"
            >
              {t('approval.approve')}
            </button>
            <button
              onClick={() => onDecide(item, false)}
              disabled={item.deciding}
              className="px-3 py-1.5 text-xs font-semibold rounded-sm bg-surface-container-high text-gray-100 hover:text-error disabled:opacity-50"
            >
              {t('approval.deny')}
            </button>
          </div>
        ) : (
          <p className={item.decision === 'approved' ? 'text-primary' : 'text-error'}>{item.decision}</p>
        )}
      </div>
    </div>
  )
}

export default function SentryFloatingPanel() {
  const { t } = useTranslation('sentry')
  const location = useLocation()
  const { can } = useAuth()
  const canRead = can(uiKey('sentry'))
  const canWrite = can(writeKey('sentry'))
  const hiddenOnPage = isTabbyPage(location.pathname)
  const suggestionItems = t(`suggestions.${suggestionGroupForPath(location.pathname)}`, { returnObjects: true })
  const suggestions = Array.isArray(suggestionItems) ? suggestionItems : []
  const savedChatRef = useRef(null)
  if (!savedChatRef.current) savedChatRef.current = loadSavedChat()

  const [open, setOpen] = useState(false)
  const [status, setStatus] = useState(null)
  const [statusError, setStatusError] = useState(null)
  const [activeId, setActiveId] = useState(savedChatRef.current.activeId)
  const [messages, setMessages] = useState(savedChatRef.current.messages)
  const [input, setInput] = useState(savedChatRef.current.input)
  const [streaming, setStreaming] = useState(false)

  const abortRef = useRef(null)
  const scrollRef = useRef(null)
  const activeIdRef = useRef(null)
  const openAssistantIdRef = useRef(null)
  useEffect(() => { activeIdRef.current = activeId }, [activeId])

  useEffect(() => {
    if (!open || status) return
    api.sentryStatus()
      .then(s => { setStatus(s); setStatusError(null) })
      .catch(err => setStatusError(err?.message || t('errors.loadStatusFailed')))
  }, [open, status, t])

  useEffect(() => {
    const el = scrollRef.current
    if (el) el.scrollTop = el.scrollHeight
  }, [messages, open])

  useEffect(() => {
    saveChat(activeId, messages, input)
  }, [activeId, messages, input])

  useEffect(() => () => abortRef.current?.abort(), [])

  useEffect(() => {
    const handleOpenTabby = () => setOpen(true)
    window.addEventListener(OPEN_TABBY_EVENT, handleOpenTabby)
    return () => window.removeEventListener(OPEN_TABBY_EVENT, handleOpenTabby)
  }, [])

  if (!canRead || hiddenOnPage) return null

  function newConversation() {
    if (streaming) return
    abortRef.current?.abort()
    setActiveId(null)
    setMessages([])
  }

  async function decideApproval(item, approve) {
    setMessages(prev => prev.map(m => m.id === item.id ? { ...m, deciding: true } : m))
    try {
      await api.sentryDecideApproval(item.proposalId, approve)
    } catch (err) {
      setMessages(prev => prev.map(m => m.id === item.id ? { ...m, deciding: false } : m))
      setMessages(prev => [...prev, { id: localId('error'), role: 'error', content: err?.message || t('errors.decisionFailed') }])
    }
  }

  function finalizeOpenBubble() {
    const openId = openAssistantIdRef.current
    if (!openId) return
    setMessages(prev => prev.map(m => m.id === openId ? { ...m, pending: false } : m))
    openAssistantIdRef.current = null
  }

  async function send(messageText) {
    const text = (messageText ?? input).trim()
    if (!text || streaming) return
    setInput('')
    setMessages(prev => [...prev, { id: localId('user'), role: 'user', content: text }])
    openAssistantIdRef.current = null

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
          case 'token':
            setMessages(prev => {
              const openId = openAssistantIdRef.current
              if (openId) return prev.map(m => m.id === openId ? { ...m, content: (m.content || '') + (ev.text || '') } : m)
              const id = localId('assistant')
              openAssistantIdRef.current = id
              return [...prev, { id, role: 'assistant', content: ev.text || '', pending: true }]
            })
            break
          case 'tool_call':
            finalizeOpenBubble()
            setMessages(prev => prev.some(m => m.id === ev.id)
              ? prev
              : [...prev, { id: ev.id, role: 'tool', name: ev.name, args: ev.args, result: null, pending: true }])
            break
          case 'approval_request':
            finalizeOpenBubble()
            setMessages(prev => prev.some(m => m.id === ev.id)
              ? prev
              : [...prev, { id: ev.id, role: 'approval', proposalId: ev.proposal_id, name: ev.name, args: ev.args, decision: null, deciding: false }])
            break
          case 'approval_result':
            setMessages(prev => prev.map(m => m.id === ev.id
              ? { ...m, decision: ev.decision, deciding: false }
              : m))
            break
          case 'tool_result':
            setMessages(prev => prev.map(m => m.id === ev.id
              ? { ...m, result: ev.result, pending: false }
              : m))
            break
          case 'done':
            finalizeOpenBubble()
            break
          case 'error':
            finalizeOpenBubble()
            setMessages(prev => [...prev, { id: localId('error'), role: 'error', content: ev.message || t('errors.genericChatError') }])
            break
          default:
            break
        }
      }
    } catch (err) {
      finalizeOpenBubble()
      if (err?.name !== 'AbortError') {
        setMessages(prev => [...prev, { id: localId('error'), role: 'error', content: err?.message || t('errors.chatRequestFailed') }])
      }
    } finally {
      setStreaming(false)
    }
  }

  function handleKeyDown(e) {
    if (e.key === 'Enter' && (e.ctrlKey || e.metaKey) && !e.nativeEvent?.isComposing) {
      e.preventDefault()
      send()
    }
  }

  return (
    <div className="fixed inset-0 z-[90] pointer-events-none">
      {open && (
        <aside className="sentry-floating-drawer pointer-events-auto fixed right-0 top-[41px] h-[calc(100vh-41px)] w-[min(460px,100vw)] bg-surface-container border-l border-gray-700 flex flex-col">
          <header className="h-14 px-4 border-b border-gray-700 flex items-center gap-3 shrink-0">
            <TabbyAvatar className="h-8 w-8" />
            <div className="min-w-0 flex-1">
              <h2 className="text-sm font-semibold text-gray-100">{t('floating.title')}</h2>
              {status?.model && <p className="text-[10px] font-mono text-gray-400 truncate">{status.model}</p>}
            </div>
            <button type="button" onClick={newConversation} disabled={streaming} className="h-8 w-8 rounded-sm text-pink-500 hover:text-pink-400 hover:bg-gray-800 transition-colors disabled:opacity-40" title={t('floating.newChat')}>
              <span className="material-symbols-outlined text-xl">add</span>
            </button>
            <Link to="/sentry" className="h-8 w-8 rounded-sm text-gray-400 hover:text-gray-100 hover:bg-gray-800 transition-colors flex items-center justify-center" title={t('floating.openFull')}>
              <span className="material-symbols-outlined text-xl">open_in_full</span>
            </Link>
            <button type="button" onClick={() => setOpen(false)} className="h-8 w-8 rounded-sm text-gray-400 hover:text-gray-100 hover:bg-gray-800 transition-colors" title={t('floating.close')}>
              <span className="material-symbols-outlined text-xl">close</span>
            </button>
          </header>

          <div ref={scrollRef} className="flex-1 min-h-0 overflow-y-auto p-4 flex flex-col gap-4 custom-scrollbar">
            {statusError ? (
              <MessageBubble item={{ role: 'error', content: statusError }} />
            ) : !status ? (
              <p className="text-xs text-gray-400">{t('loading')}</p>
            ) : !status.enabled ? (
              <div className="text-xs text-gray-400">
                {t('disabled.hintPre')}<code className="font-mono text-primary">sentry.enabled</code>{t('disabled.hintPost')}
              </div>
            ) : messages.length === 0 ? (
              <div className="mt-auto mb-auto space-y-4">
                <TabbyAvatar className="h-12 w-12" />
                <p className="text-xl font-semibold text-gray-100 leading-snug">{t('chat.emptyState')}</p>
                <div className="flex flex-col items-end gap-2">
                  {suggestions.map(suggestion => (
                    <button
                      key={suggestion}
                      type="button"
                      onClick={() => send(suggestion)}
                      disabled={!canWrite || streaming}
                      className={`max-w-[85%] text-left ${TABBY_USER_BUBBLE_CLASS} ${TABBY_USER_BUBBLE_HOVER_CLASS} rounded-sm px-3 py-2 text-sm transition-colors disabled:opacity-60`}
                    >
                      {suggestion}
                    </button>
                  ))}
                </div>
              </div>
            ) : (
              messages.map(item => item.role === 'tool'
                ? <ToolCard key={item.id} item={item} />
                : item.role === 'approval'
                  ? <ApprovalCard key={item.id} item={item} onDecide={decideApproval} />
                  : <MessageBubble key={item.id} item={item} />
              )
            )}
          </div>

          <div className="p-4 border-t border-gray-700 shrink-0 bg-surface-container">
            {canWrite && status?.enabled ? (
              <>
                <textarea
                  value={input}
                  onChange={e => setInput(e.target.value)}
                  onKeyDown={handleKeyDown}
                  disabled={streaming}
                  rows={3}
                  placeholder={t('chat.composerPlaceholder')}
                  className="w-full bg-surface-container-high border border-gray-700 text-gray-100 text-sm rounded-sm px-3 py-2 placeholder-gray-500 resize-none focus:outline-none focus:ring-2 focus:ring-pink-500/30 disabled:opacity-60"
                />
                <div className="mt-2 flex items-center justify-between gap-3">
                  <p className="text-[10px] text-gray-400">{t('chat.sendHint')}</p>
                  <button
                    type="button"
                    onClick={() => send()}
                    disabled={streaming || !input.trim()}
                    className="px-4 py-2 text-xs font-semibold rounded-sm bg-pink-500 text-white hover:bg-pink-400 transition-colors disabled:opacity-50"
                  >
                    {streaming ? t('chat.sending') : t('chat.send')}
                  </button>
                </div>
              </>
            ) : (
              <p className="text-xs text-gray-400 text-center py-2">{t('chat.readOnlyHint')}</p>
            )}
          </div>
        </aside>
      )}
    </div>
  )
}
