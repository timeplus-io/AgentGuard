import { useCallback, useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { api } from '../api/client'
import Toggle from './Toggle'
import { formatNumber } from '../i18n/format'

// Chat budget knobs and the clamp ceilings the backend applies (llm.go
// applyDefaults) — shown as hints so an operator doesn't type a value that
// gets silently clamped. labelKey resolves against the sentry namespace.
const CHAT_NUMBER_FIELDS = [
  { key: 'max_tokens', labelKey: 'config.chat.fields.maxTokens', ceiling: 16384 },
  { key: 'max_iterations', labelKey: 'config.chat.fields.maxIterations', ceiling: 32 },
  { key: 'turn_token_budget', labelKey: 'config.chat.fields.turnTokenBudget', ceiling: 256000 },
  { key: 'sql_timeout_ms', labelKey: 'config.chat.fields.sqlTimeoutMs', ceiling: 55000 },
  { key: 'sql_max_rows', labelKey: 'config.chat.fields.sqlMaxRows', ceiling: 5000 },
  { key: 'tool_result_max_bytes', labelKey: 'config.chat.fields.toolResultMaxBytes', ceiling: 131072 },
]

const inputCls =
  'w-full rounded-sm bg-gray-700 text-gray-100 text-sm px-2 py-1.5 focus:outline-none focus:ring-1 focus:ring-[#f256a3]'
const labelCls = 'text-[11px] uppercase tracking-wide text-gray-400'

// buildDraft flattens the two self-contained agentguard_sentry_config rows
// (`chat`, `memory`) into editable form state. api_key is never echoed back
// (GetSentryConfig redacts it) — blank means "keep the stored key".
function buildDraft(byKey) {
  const chat = byKey.chat?.value || {}
  const memory = byKey.memory?.value || {}
  return {
    chat: {
      api_type: chat.api_type || 'openai',
      base_url: chat.base_url || '',
      api_key: '',
      model: chat.model || '',
      timeout: chat.timeout ?? 60,
      max_tokens: chat.max_tokens ?? 1024,
      max_iterations: chat.max_iterations ?? 12,
      turn_token_budget: chat.turn_token_budget ?? 32000,
      sql_timeout_ms: chat.sql_timeout_ms ?? 30000,
      sql_max_rows: chat.sql_max_rows ?? 1000,
      tool_result_max_bytes: chat.tool_result_max_bytes ?? 16384,
      // Opt-in switch for the AgentGuard interaction surface (skill +
      // read/write tools). Absent means DISABLED — generic SQL analyst.
      agentguard_tools_enabled: chat.agentguard_tools_enabled ?? false,
      // Opt-in switch for the data-audit-guide skill (A01–A27 audit playbook
      // over agentguard_cim_event). Independent of the surface above.
      data_audit_enabled: chat.data_audit_enabled ?? false,
      // Anthropic-only knobs (#212): ignored by the OpenAI provider.
      thinking: chat.thinking || '',
      effort: chat.effort || '',
      prompt_cache_disabled: chat.prompt_cache_disabled ?? false,
    },
    memory: {
      enabled: memory.enabled ?? false,
      base_url: memory.base_url || '',
      api_key: '',
      model: memory.model || '',
      embedding_dim: memory.embedding_dim ?? 1536,
    },
  }
}

export default function SentryConfigEditor({ reloadToken = 0 }) {
  const { t } = useTranslation('sentry')
  const [byKey, setByKey] = useState(null)
  const [draft, setDraft] = useState(null)
  const [dirty, setDirty] = useState(() => new Set())
  const [loadErr, setLoadErr] = useState(null)
  const [saving, setSaving] = useState(null) // 'chat' | 'memory' | null
  const [banner, setBanner] = useState(null)
  const [test, setTest] = useState(null)

  // Mirror `dirty` into a ref so load() can read the current dirty set without
  // taking it as a dependency (which would re-run the reloadToken effect on
  // every keystroke). Used to decide which panels a refetch may overwrite.
  const dirtyRef = useRef(dirty)
  useEffect(() => { dirtyRef.current = dirty }, [dirty])

  // load refetches the config and merges it into the form WITHOUT clobbering
  // in-progress edits: a panel that is currently dirty keeps its edits unless
  // it is the one we just saved (forceKey). Previously load() rebuilt the whole
  // draft and cleared all dirty flags, so saving one panel — or any unrelated
  // Settings save that bumps reloadToken — silently discarded the other panel's
  // unsaved edits while showing "Saved" (#237).
  const load = useCallback((forceKey = null) => {
    api.getSentryConfig()
      .then((res) => {
        const map = {}
        for (const r of res.config || []) map[r.key] = r
        const fresh = buildDraft(map)
        const keep = (k) => k !== forceKey && dirtyRef.current.has(k)
        setByKey(map)
        setDraft((prev) => (prev
          ? { chat: keep('chat') ? prev.chat : fresh.chat, memory: keep('memory') ? prev.memory : fresh.memory }
          : fresh))
        setDirty((prev) => {
          const next = new Set()
          for (const k of prev) if (keep(k)) next.add(k) // retain still-dirty panels; clear refreshed ones
          return next
        })
        setLoadErr(null)
      })
      .catch((e) => setLoadErr(e?.message || t('config.errors.loadFailed')))
  }, [t])

  // reloadToken changes when the Settings save bar writes config-file keys
  // (e.g. enabling Sentry, which provisions the streams, #226) — refetch so
  // a pre-provision 503 error doesn't stick around after provisioning. Dirty
  // panels are preserved (see load).
  useEffect(() => { load() }, [load, reloadToken])

  const mark = (key) => setDirty((prev) => new Set(prev).add(key))

  const setChat = (patch) => {
    setDraft((d) => ({ ...d, chat: { ...d.chat, ...patch } }))
    mark('chat')
  }
  const setMemory = (patch) => {
    setDraft((d) => ({ ...d, memory: { ...d.memory, ...patch } }))
    mark('memory')
  }

  function buildValue(key) {
    if (key === 'chat') {
      const c = draft.chat
      return {
        api_type: c.api_type,
        base_url: c.base_url,
        api_key: c.api_key, // blank → server preserves the stored key
        model: c.model,
        timeout: Number(c.timeout) || 0,
        max_tokens: Number(c.max_tokens) || 0,
        max_iterations: Number(c.max_iterations) || 0,
        turn_token_budget: Number(c.turn_token_budget) || 0,
        sql_timeout_ms: Number(c.sql_timeout_ms) || 0,
        sql_max_rows: Number(c.sql_max_rows) || 0,
        tool_result_max_bytes: Number(c.tool_result_max_bytes) || 0,
        agentguard_tools_enabled: !!c.agentguard_tools_enabled,
        data_audit_enabled: !!c.data_audit_enabled,
        thinking: c.thinking,
        effort: c.effort,
        prompt_cache_disabled: !!c.prompt_cache_disabled,
      }
    }
    const m = draft.memory
    // Always include endpoint config to preserve when toggling memory off.
    return {
      enabled: m.enabled,
      base_url: m.base_url,
      api_key: m.api_key, // blank → server preserves the stored key
      model: m.model,
      embedding_dim: Number(m.embedding_dim) || 0,
    }
  }

  async function savePanel(key) {
    setBanner(null)
    setSaving(key)
    try {
      await api.putSentryConfig(key, buildValue(key))
      setBanner({
        type: 'ok',
        msg: key === 'chat' ? t('config.banner.savedChat') : t('config.banner.savedMemory'),
      })
      load(key) // refresh only the saved panel; the other panel's unsaved edits survive (#237)
    } catch (e) {
      setBanner({ type: 'err', msg: e?.message || t('config.errors.saveFailed') })
    } finally {
      setSaving(null)
    }
  }

  // Test the chat + (if memory is enabled) embedding endpoints against the
  // values currently in the form (not the saved/config.yaml ones).
  async function testConnection() {
    setTest({ pending: true })
    try {
      const r = await api.testSentryConfig({
        chat_api_type: draft.chat.api_type,
        base_url: draft.chat.base_url,
        api_key: draft.chat.api_key,
        chat_model: draft.chat.model,
        memory_enabled: draft.memory.enabled,
        embedding_base_url: draft.memory.base_url,
        embedding_api_key: draft.memory.api_key,
        embedding_model: draft.memory.model,
        embedding_dim: Number(draft.memory.embedding_dim) || 0,
      })
      setTest(r)
    } catch (e) {
      setTest({ ok: false, error: e?.message || t('config.errors.testFailed') })
    }
  }

  if (loadErr) {
    return (
      <div className="flex items-center gap-3">
        <p className="text-xs text-error flex-1 min-w-0">
          {loadErr}{t('config.errors.loadErrSuffix')}
        </p>
        <button
          onClick={load}
          className="shrink-0 px-3 py-1.5 text-xs font-semibold rounded-sm bg-surface-container-high text-gray-100 hover:text-primary"
        >
          {t('config.errors.retry')}
        </button>
      </div>
    )
  }
  if (!draft) return <p className="text-xs text-gray-400">{t('config.loadingRuntime')}</p>

  return (
    <div className="space-y-6">
      <p className="text-xs text-gray-400">
        {t('config.intro.pre')}<code className="font-mono text-primary">agentguard_sentry_config</code>{t('config.intro.post')}
      </p>

      {/* Chat model */}
      <div>
        <h4 className="text-sm font-medium text-gray-100 mb-2">{t('config.chat.heading')}</h4>
        <label className="space-y-1 block mb-3"><span className={labelCls}>{t('config.chat.apiTypeLabel')}</span>
          <select
            className={inputCls}
            value={draft.chat.api_type}
            onChange={(e) => setChat({ api_type: e.target.value })}
          >
            <option value="openai">{t('config.chat.apiTypeOpenai')}</option>
            <option value="anthropic">{t('config.chat.apiTypeAnthropic')}</option>
          </select>
        </label>
        {draft.chat.api_type === 'anthropic' && (
          <p className="text-[11px] text-gray-500 mb-3">
            {t('config.chat.bedrockHint.part1')}<code>https://bedrock-mantle.&lt;region&gt;.api.aws/anthropic</code>{' '}
            {t('config.chat.bedrockHint.part2')}<code>/anthropic</code>{t('config.chat.bedrockHint.part3')}<code>/v1</code>
            {t('config.chat.bedrockHint.part4')}<code>anthropic.claude-opus-4-8</code>{t('config.chat.bedrockHint.part5')}
            <code>us-east-1</code>{t('config.chat.bedrockHint.part6')}
            <code>us.</code>{t('config.chat.bedrockHint.part7')}<code>global.</code>{t('config.chat.bedrockHint.part8')}
          </p>
        )}
        <div className="grid grid-cols-1 sm:grid-cols-3 gap-3 mb-3">
          <label className="space-y-1"><span className={labelCls}>{t('config.common.baseUrlLabel')}</span>
            <input className={inputCls} value={draft.chat.base_url} onChange={(e) => setChat({ base_url: e.target.value })} placeholder={t('config.chat.baseUrlPlaceholder')} />
          </label>
          <label className="space-y-1"><span className={labelCls}>{t('config.common.apiKeyLabel')} {byKey.chat?.api_key_set && <span className="text-emerald-400 normal-case">{t('config.common.apiKeySetSuffix')}</span>}</span>
            <input className={inputCls} type="password" value={draft.chat.api_key} onChange={(e) => setChat({ api_key: e.target.value })} placeholder={byKey.chat?.api_key_set ? t('config.common.apiKeyPlaceholderSet') : t('config.common.apiKeyPlaceholderUnset')} />
          </label>
          <label className="space-y-1"><span className={labelCls}>{t('config.common.modelLabel')}</span>
            <input className={inputCls} value={draft.chat.model} onChange={(e) => setChat({ model: e.target.value })} placeholder={t('config.common.modelPlaceholder')} />
          </label>
        </div>
        <div className="grid grid-cols-2 sm:grid-cols-3 gap-3">
          <label className="space-y-1"><span className={labelCls}>{t('config.chat.timeoutLabel')}</span>
            <input className={inputCls} type="number" value={draft.chat.timeout} onChange={(e) => setChat({ timeout: e.target.value })} />
          </label>
          {CHAT_NUMBER_FIELDS.map(({ key, labelKey, ceiling }) => (
            <label key={key} className="space-y-1">
              <span className={labelCls}>{t(labelKey)} <span className="normal-case text-gray-500">({t('config.chat.fields.ceilingSuffix', { value: formatNumber(ceiling) })})</span></span>
              <input
                className={inputCls}
                type="number"
                value={draft.chat[key]}
                onChange={(e) => setChat({ [key]: e.target.value })}
              />
            </label>
          ))}
        </div>

        {draft.chat.api_type === 'anthropic' && (
          <div className="mt-4 grid grid-cols-2 sm:grid-cols-3 gap-3 items-end">
            <label className="space-y-1"><span className={labelCls}>{t('config.chat.thinkingLabel')}</span>
              <select className={inputCls} value={draft.chat.thinking} onChange={(e) => setChat({ thinking: e.target.value })}>
                <option value="">{t('config.chat.providerDefault')}</option>
                <option value="adaptive">{t('config.chat.thinkingAdaptive')}</option>
              </select>
            </label>
            <label className="space-y-1"><span className={labelCls}>{t('config.chat.effortLabel')}</span>
              <select className={inputCls} value={draft.chat.effort} onChange={(e) => setChat({ effort: e.target.value })}>
                <option value="">{t('config.chat.providerDefault')}</option>
                <option value="low">low</option>
                <option value="medium">medium</option>
                <option value="high">high</option>
                <option value="xhigh">xhigh</option>
                <option value="max">max</option>
              </select>
            </label>
            <label className="flex items-center gap-2 text-xs text-gray-100 pb-1.5">
              <Toggle checked={!draft.chat.prompt_cache_disabled} onChange={(v) => setChat({ prompt_cache_disabled: !v })} />
              {t('config.chat.promptCachingLabel')}
            </label>
          </div>
        )}

        <div className="mt-4">
          <label className="flex items-start gap-3 text-xs text-gray-100 max-w-2xl">
            <span className="pt-0.5">
              <Toggle checked={draft.chat.agentguard_tools_enabled} onChange={(v) => setChat({ agentguard_tools_enabled: v })} />
            </span>
            <span>
              <span className="font-medium">{t('config.chat.agentguardToolsTitle')}</span>
              <span className="text-gray-500">{t('config.chat.agentguardToolsDescription')}</span>
            </span>
          </label>
        </div>

        <div className="mt-3">
          <label className="flex items-start gap-3 text-xs text-gray-100 max-w-2xl">
            <span className="pt-0.5">
              <Toggle checked={draft.chat.data_audit_enabled} onChange={(v) => setChat({ data_audit_enabled: v })} />
            </span>
            <span>
              <span className="font-medium">{t('config.chat.dataAuditTitle')}</span>
              <span className="text-gray-500">{t('config.chat.dataAuditDescription')}</span>
            </span>
          </label>
        </div>

        <div className="mt-4 flex items-center gap-3">
          <button onClick={() => savePanel('chat')} disabled={saving === 'chat' || !dirty.has('chat')}
            className="rounded-sm px-4 py-2 text-sm bg-gradient-to-br from-[#ffb0cf] to-[#f256a3] text-[#3d0023] font-medium disabled:opacity-50">
            {saving === 'chat' ? t('config.common.saving') : dirty.has('chat') ? t('config.chat.saveButton') : t('config.common.saved')}
          </button>
        </div>
      </div>

      {/* Memory (embedding) */}
      <div>
        <div className="flex items-center gap-3 mb-2">
          <h4 className="text-sm font-medium text-gray-100">
            {t('config.memory.heading')} <span className="text-[11px] font-normal text-gray-500 normal-case">{t('config.memory.subheading')}</span>
          </h4>
          <Toggle checked={draft.memory.enabled} onChange={(v) => setMemory({ enabled: v })} />
        </div>

        {draft.memory.enabled ? (
          <div className="grid grid-cols-1 sm:grid-cols-4 gap-3">
            <label className="space-y-1"><span className={labelCls}>{t('config.common.baseUrlLabel')}</span>
              <input className={inputCls} value={draft.memory.base_url} onChange={(e) => setMemory({ base_url: e.target.value })} placeholder={t('config.chat.baseUrlPlaceholder')} />
            </label>
            <label className="space-y-1"><span className={labelCls}>{t('config.common.apiKeyLabel')} {byKey.memory?.api_key_set && <span className="text-emerald-400 normal-case">{t('config.common.apiKeySetSuffix')}</span>}</span>
              <input className={inputCls} type="password" value={draft.memory.api_key} onChange={(e) => setMemory({ api_key: e.target.value })} placeholder={byKey.memory?.api_key_set ? t('config.common.apiKeyPlaceholderSet') : t('config.common.apiKeyPlaceholderUnset')} />
            </label>
            <label className="space-y-1"><span className={labelCls}>{t('config.common.modelLabel')}</span>
              <input className={inputCls} value={draft.memory.model} onChange={(e) => setMemory({ model: e.target.value })} placeholder={t('config.common.modelPlaceholder')} />
            </label>
            <label className="space-y-1"><span className={labelCls}>{t('config.memory.embeddingDimLabel')}</span>
              <input className={inputCls} type="number" value={draft.memory.embedding_dim} onChange={(e) => setMemory({ embedding_dim: e.target.value })} />
            </label>
          </div>
        ) : (
          <p className="text-[11px] text-gray-500">{t('config.memory.disabledHint')}</p>
        )}

        <div className="mt-4 flex items-center gap-3">
          <button onClick={() => savePanel('memory')} disabled={saving === 'memory' || !dirty.has('memory')}
            className="rounded-sm px-4 py-2 text-sm bg-gradient-to-br from-[#ffb0cf] to-[#f256a3] text-[#3d0023] font-medium disabled:opacity-50">
            {saving === 'memory' ? t('config.common.saving') : dirty.has('memory') ? t('config.memory.saveButton') : t('config.common.saved')}
          </button>
        </div>
      </div>

      {/* Test connection */}
      <div className="bg-gray-900/40 rounded-sm px-4 py-3">
        <div className="flex items-center gap-3 flex-wrap">
          <button onClick={testConnection} className="rounded-sm px-3 py-1.5 text-xs bg-gray-700 text-gray-100 hover:bg-gray-600">{t('config.test.button')}</button>
          <span className="text-[11px] text-gray-500">{t('config.test.hint')}</span>
        </div>
        {test?.pending && <p className="text-xs text-gray-400 mt-2">{t('config.test.testing')}</p>}
        {test && !test.pending && (
          <div className="text-xs space-y-0.5 mt-2">
            {test.checks ? (
              <>
                {test.checks.reachable?.skipped ? (
                  <div className="text-gray-500">{t('config.test.checks.reachableSkipped')}</div>
                ) : (
                  <div className={test.checks.reachable?.ok ? 'text-emerald-400' : 'text-error'}>
                    {test.checks.reachable?.ok ? t('config.test.checks.reachableOk') : t('config.test.checks.reachableFail', { error: test.checks.reachable?.error || '' })}
                  </div>
                )}
                {test.checks.chat && (
                  <div className={test.checks.chat.ok ? 'text-emerald-400' : 'text-error'}>
                    {test.checks.chat.ok
                      ? t('config.test.checks.chatOk', { toolCallName: test.checks.chat.tool_call_name || t('config.test.checks.chatOkDefault') })
                      : t('config.test.checks.chatFail', { error: test.checks.chat.error || t('config.test.checks.chatFailDefault') })}
                  </div>
                )}
                {test.checks.embedding && (
                  test.checks.embedding.skipped ? (
                    <div className="text-gray-500">{t('config.test.checks.embeddingSkipped')}</div>
                  ) : (
                    <div className={test.checks.embedding.ok ? 'text-emerald-400' : 'text-error'}>
                      {test.checks.embedding.ok
                        ? t('config.test.checks.embeddingOk', { dim: test.checks.embedding.dim })
                        : t('config.test.checks.embeddingFail', { error: test.checks.embedding.error || t('config.test.checks.embeddingFailDefault') })}
                    </div>
                  )
                )}
              </>
            ) : (
              <span className={test.ok ? 'text-emerald-400' : 'text-error'}>
                {test.ok ? t('config.test.simple.ok') : t('config.test.simple.fail', { error: test.error })}
              </span>
            )}
          </div>
        )}
      </div>

      {banner && <p className={`text-xs ${banner.type === 'ok' ? 'text-emerald-400' : 'text-error'}`}>{banner.msg}</p>}
    </div>
  )
}
