import { useCallback, useEffect, useState } from 'react'
import { api } from '../api/client'
import Toggle from './Toggle'

// Chat budget knobs and the clamp ceilings the backend applies (llm.go
// applyDefaults) — shown as hints so an operator doesn't type a value that
// gets silently clamped.
const CHAT_NUMBER_FIELDS = [
  { key: 'max_tokens', label: 'Max tokens', ceiling: 16384 },
  { key: 'max_iterations', label: 'Max iterations', ceiling: 32 },
  { key: 'turn_token_budget', label: 'Turn token budget', ceiling: 256000 },
  { key: 'sql_timeout_ms', label: 'SQL timeout (ms)', ceiling: 55000 },
  { key: 'sql_max_rows', label: 'SQL max rows', ceiling: 5000 },
  { key: 'tool_result_max_bytes', label: 'Tool result max bytes', ceiling: 131072 },
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
  const [byKey, setByKey] = useState(null)
  const [draft, setDraft] = useState(null)
  const [dirty, setDirty] = useState(() => new Set())
  const [loadErr, setLoadErr] = useState(null)
  const [saving, setSaving] = useState(null) // 'chat' | 'memory' | null
  const [banner, setBanner] = useState(null)
  const [test, setTest] = useState(null)

  const load = useCallback(() => {
    api.getSentryConfig()
      .then((res) => {
        const map = {}
        for (const r of res.config || []) map[r.key] = r
        setByKey(map)
        setDraft(buildDraft(map))
        setDirty(new Set())
        setLoadErr(null)
      })
      .catch((e) => setLoadErr(e?.message || 'Failed to load Sentry config'))
  }, [])

  // reloadToken changes when the Settings save bar writes config-file keys
  // (e.g. enabling Sentry, which provisions the streams, #226) — refetch so
  // a pre-provision 503 error doesn't stick around after provisioning.
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
        msg: `${key === 'chat' ? 'Chat' : 'Memory'} config saved. Changes take effect on the next Sentry turn — no re-provision.`,
      })
      load()
    } catch (e) {
      setBanner({ type: 'err', msg: e?.message || 'Save failed' })
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
      setTest({ ok: false, error: e?.message || 'test failed' })
    }
  }

  if (loadErr) {
    return (
      <div className="flex items-center gap-3">
        <p className="text-xs text-error flex-1 min-w-0">
          {loadErr} — runtime config is available only after Sentry is provisioned.
        </p>
        <button
          onClick={load}
          className="shrink-0 px-3 py-1.5 text-xs font-semibold rounded-sm bg-surface-container-high text-gray-100 hover:text-primary"
        >
          Retry
        </button>
      </div>
    )
  }
  if (!draft) return <p className="text-xs text-gray-400">Loading runtime config…</p>

  return (
    <div className="space-y-6">
      <p className="text-xs text-gray-400">
        Live configuration stored in the <code className="font-mono text-[#ffb0cf]">agentguard_sentry_config</code> stream.
        Edits apply immediately — no restart or re-provision. (Boot defaults in the server config only seed these on first provision.)
      </p>

      {/* Chat model */}
      <div>
        <h4 className="text-sm font-medium text-gray-100 mb-2">Chat model</h4>
        <label className="space-y-1 block mb-3"><span className={labelCls}>API type</span>
          <select
            className={inputCls}
            value={draft.chat.api_type}
            onChange={(e) => setChat({ api_type: e.target.value })}
          >
            <option value="openai">OpenAI-compatible (Chat Completions)</option>
            <option value="anthropic">Anthropic (Claude, incl. Bedrock)</option>
          </select>
        </label>
        {draft.chat.api_type === 'anthropic' && (
          <p className="text-[11px] text-gray-500 mb-3">
            For AWS Bedrock: base URL <code>https://bedrock-mantle.&lt;region&gt;.api.aws/anthropic</code>{' '}
            (note the <code>/anthropic</code> path, not <code>/v1</code>), API key = your Bedrock API key,
            model = a bare Bedrock Claude id (e.g. <code>anthropic.claude-opus-4-8</code>). Pick a region where the
            model is in-region available and access-enabled (<code>us-east-1</code> is a safe default; Mantle
            doesn't do <code>us.</code>/<code>global.</code> cross-region ids). Tool calling is required.
          </p>
        )}
        <div className="grid grid-cols-1 sm:grid-cols-3 gap-3 mb-3">
          <label className="space-y-1"><span className={labelCls}>Base URL</span>
            <input className={inputCls} value={draft.chat.base_url} onChange={(e) => setChat({ base_url: e.target.value })} placeholder="http://localhost:4000/v1" />
          </label>
          <label className="space-y-1"><span className={labelCls}>API key {byKey.chat?.api_key_set && <span className="text-emerald-400 normal-case">· •••• set</span>}</span>
            <input className={inputCls} type="password" value={draft.chat.api_key} onChange={(e) => setChat({ api_key: e.target.value })} placeholder={byKey.chat?.api_key_set ? '•••••• (unchanged)' : 'none'} />
          </label>
          <label className="space-y-1"><span className={labelCls}>Model</span>
            <input className={inputCls} value={draft.chat.model} onChange={(e) => setChat({ model: e.target.value })} placeholder="model id" />
          </label>
        </div>
        <div className="grid grid-cols-2 sm:grid-cols-3 gap-3">
          <label className="space-y-1"><span className={labelCls}>Timeout (s)</span>
            <input className={inputCls} type="number" value={draft.chat.timeout} onChange={(e) => setChat({ timeout: e.target.value })} />
          </label>
          {CHAT_NUMBER_FIELDS.map(({ key, label, ceiling }) => (
            <label key={key} className="space-y-1">
              <span className={labelCls}>{label} <span className="normal-case text-gray-500">(≤{ceiling.toLocaleString()})</span></span>
              <input
                className={inputCls}
                type="number"
                value={draft.chat[key]}
                onChange={(e) => setChat({ [key]: e.target.value })}
              />
            </label>
          ))}
        </div>

        <div className="mt-4">
          <label className="flex items-start gap-3 text-xs text-gray-100 max-w-2xl">
            <span className="pt-0.5">
              <Toggle checked={draft.chat.agentguard_tools_enabled} onChange={(v) => setChat({ agentguard_tools_enabled: v })} />
            </span>
            <span>
              <span className="font-medium">AgentGuard skills &amp; tools</span>
              <span className="text-gray-500"> — let Sentry read and act on AgentGuard features (threats, holds, rules, cost, DLP; write actions still require in-chat approval). Off: Sentry is a generic Timeplus SQL analyst.</span>
            </span>
          </label>
        </div>

        <div className="mt-4 flex items-center gap-3">
          <button onClick={() => savePanel('chat')} disabled={saving === 'chat' || !dirty.has('chat')}
            className="rounded-sm px-4 py-2 text-sm bg-gradient-to-br from-[#ffb0cf] to-[#f256a3] text-[#3d0023] font-medium disabled:opacity-50">
            {saving === 'chat' ? 'Saving…' : dirty.has('chat') ? 'Save chat config' : 'Saved'}
          </button>
        </div>
      </div>

      {/* Memory (embedding) */}
      <div>
        <div className="flex items-center gap-3 mb-2">
          <h4 className="text-sm font-medium text-gray-100">
            Memory (embedding) <span className="text-[11px] font-normal text-gray-500 normal-case">— used by Sentry memory (Phase 2)</span>
          </h4>
          <Toggle checked={draft.memory.enabled} onChange={(v) => setMemory({ enabled: v })} />
        </div>

        {draft.memory.enabled ? (
          <div className="grid grid-cols-1 sm:grid-cols-4 gap-3">
            <label className="space-y-1"><span className={labelCls}>Base URL</span>
              <input className={inputCls} value={draft.memory.base_url} onChange={(e) => setMemory({ base_url: e.target.value })} placeholder="http://localhost:4000/v1" />
            </label>
            <label className="space-y-1"><span className={labelCls}>API key {byKey.memory?.api_key_set && <span className="text-emerald-400 normal-case">· •••• set</span>}</span>
              <input className={inputCls} type="password" value={draft.memory.api_key} onChange={(e) => setMemory({ api_key: e.target.value })} placeholder={byKey.memory?.api_key_set ? '•••••• (unchanged)' : 'none'} />
            </label>
            <label className="space-y-1"><span className={labelCls}>Model</span>
              <input className={inputCls} value={draft.memory.model} onChange={(e) => setMemory({ model: e.target.value })} placeholder="model id" />
            </label>
            <label className="space-y-1"><span className={labelCls}>Embedding dimension</span>
              <input className={inputCls} type="number" value={draft.memory.embedding_dim} onChange={(e) => setMemory({ embedding_dim: e.target.value })} />
            </label>
          </div>
        ) : (
          <p className="text-[11px] text-gray-500">Disabled — Sentry memory (Phase 2) has no embedding endpoint configured.</p>
        )}

        <div className="mt-4 flex items-center gap-3">
          <button onClick={() => savePanel('memory')} disabled={saving === 'memory' || !dirty.has('memory')}
            className="rounded-sm px-4 py-2 text-sm bg-gradient-to-br from-[#ffb0cf] to-[#f256a3] text-[#3d0023] font-medium disabled:opacity-50">
            {saving === 'memory' ? 'Saving…' : dirty.has('memory') ? 'Save memory config' : 'Saved'}
          </button>
        </div>
      </div>

      {/* Test connection */}
      <div className="bg-gray-900/40 rounded-sm px-4 py-3">
        <div className="flex items-center gap-3 flex-wrap">
          <button onClick={testConnection} className="rounded-sm px-3 py-1.5 text-xs bg-gray-700 text-gray-100 hover:bg-gray-600">Test connection</button>
          <span className="text-[11px] text-gray-500">Tests the values above (not the saved ones) — chat endpoint always, embedding only when memory is enabled.</span>
        </div>
        {test?.pending && <p className="text-xs text-gray-400 mt-2">Testing…</p>}
        {test && !test.pending && (
          <div className="text-xs space-y-0.5 mt-2">
            {test.checks ? (
              <>
                {test.checks.reachable?.skipped ? (
                  <div className="text-gray-500">— reachable: checked via chat (Anthropic)</div>
                ) : (
                  <div className={test.checks.reachable?.ok ? 'text-emerald-400' : 'text-error'}>
                    {test.checks.reachable?.ok ? '✓ reachable' : `✗ reachable: ${test.checks.reachable?.error || ''}`}
                  </div>
                )}
                {test.checks.chat && (
                  <div className={test.checks.chat.ok ? 'text-emerald-400' : 'text-error'}>
                    {test.checks.chat.ok
                      ? `✓ chat (tool calling: ${test.checks.chat.tool_call_name || 'ok'})`
                      : `✗ chat: ${test.checks.chat.error || 'failed'}`}
                  </div>
                )}
                {test.checks.embedding && (
                  test.checks.embedding.skipped ? (
                    <div className="text-gray-500">— embedding: skipped (memory disabled)</div>
                  ) : (
                    <div className={test.checks.embedding.ok ? 'text-emerald-400' : 'text-error'}>
                      {test.checks.embedding.ok
                        ? `✓ embedding dim ${test.checks.embedding.dim}`
                        : `✗ embedding: ${test.checks.embedding.error || 'failed'}`}
                    </div>
                  )
                )}
              </>
            ) : (
              <span className={test.ok ? 'text-emerald-400' : 'text-error'}>
                {test.ok ? 'Endpoint reachable' : `Failed: ${test.error}`}
              </span>
            )}
          </div>
        )}
      </div>

      {banner && <p className={`text-xs ${banner.type === 'ok' ? 'text-emerald-400' : 'text-error'}`}>{banner.msg}</p>}
    </div>
  )
}
