import { useCallback, useEffect, useState } from 'react'
import { api } from '../api/client'

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

// modelsToRows turns the shared.models object into an editable array; back via
// rowsToModels. base_url/api_key blank on a row → inherit the shared endpoint.
function modelsToRows(models) {
  return Object.entries(models || {}).map(([name, v]) => {
    const e = typeof v === 'string' ? { model: v } : (v || {})
    return { name, model: e.model || '', base_url: e.base_url || '', api_key: e.api_key || '' }
  })
}

function rowsToModels(rows) {
  const out = {}
  for (const r of rows) {
    if (!r.name) continue
    const e = { model: r.model }
    if (r.base_url) e.base_url = r.base_url
    if (r.api_key) e.api_key = r.api_key // blank → omitted; server preserves any stored key
    out[r.name] = e
  }
  return out
}

function buildDraft(byKey) {
  const shared = byKey.shared?.value || {}
  const chat = byKey.chat?.value || {}
  const embed = byKey.embed?.value || {}
  return {
    shared: {
      base_url: shared.base_url || '',
      api_key: '', // never echoed; blank = keep existing
      timeout: shared.timeout ?? 60,
      models: modelsToRows(shared.models),
    },
    chat: {
      model: chat.model || '',
      system_prompt: chat.system_prompt || '',
      max_tokens: chat.max_tokens ?? 1024,
      max_iterations: chat.max_iterations ?? 12,
      turn_token_budget: chat.turn_token_budget ?? 32000,
      sql_timeout_ms: chat.sql_timeout_ms ?? 30000,
      sql_max_rows: chat.sql_max_rows ?? 1000,
      tool_result_max_bytes: chat.tool_result_max_bytes ?? 16384,
    },
    embed: {
      model: embed.model || '',
      embedding_dim: embed.embedding_dim ?? 1536,
    },
  }
}

export default function SentryConfigEditor() {
  const [byKey, setByKey] = useState(null)
  const [draft, setDraft] = useState(null)
  const [dirty, setDirty] = useState(() => new Set())
  const [loadErr, setLoadErr] = useState(null)
  const [saving, setSaving] = useState(false)
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

  useEffect(() => { load() }, [load])

  const mark = (key) => setDirty((prev) => new Set(prev).add(key))

  const setShared = (patch) => {
    setDraft((d) => ({ ...d, shared: { ...d.shared, ...patch } }))
    mark('shared')
  }
  const setModelRow = (i, patch) => {
    setDraft((d) => {
      const models = d.shared.models.map((m, j) => (j === i ? { ...m, ...patch } : m))
      return { ...d, shared: { ...d.shared, models } }
    })
    mark('shared')
  }
  const addModelRow = () => {
    setDraft((d) => ({ ...d, shared: { ...d.shared, models: [...d.shared.models, { name: '', model: '', base_url: '', api_key: '' }] } }))
    mark('shared')
  }
  const removeModelRow = (i) => {
    setDraft((d) => ({ ...d, shared: { ...d.shared, models: d.shared.models.filter((_, j) => j !== i) } }))
    mark('shared')
  }
  const setChat = (patch) => {
    setDraft((d) => ({ ...d, chat: { ...d.chat, ...patch } }))
    mark('chat')
  }
  const setEmbed = (patch) => {
    setDraft((d) => ({ ...d, embed: { ...d.embed, ...patch } }))
    mark('embed')
  }

  function buildValue(key) {
    const prev = byKey[key]?.value || {}
    if (key === 'shared') {
      const v = { ...prev, base_url: draft.shared.base_url, timeout: Number(draft.shared.timeout) || 0, models: rowsToModels(draft.shared.models) }
      v.api_key = draft.shared.api_key // blank → server preserves the stored key
      return v
    }
    if (key === 'embed') {
      return { ...prev, model: draft.embed.model, embedding_dim: Number(draft.embed.embedding_dim) || 0 }
    }
    const c = draft.chat
    return {
      ...prev,
      model: c.model,
      system_prompt: c.system_prompt,
      max_tokens: Number(c.max_tokens) || 0,
      max_iterations: Number(c.max_iterations) || 0,
      turn_token_budget: Number(c.turn_token_budget) || 0,
      sql_timeout_ms: Number(c.sql_timeout_ms) || 0,
      sql_max_rows: Number(c.sql_max_rows) || 0,
      tool_result_max_bytes: Number(c.tool_result_max_bytes) || 0,
    }
  }

  async function save() {
    setBanner(null)
    setSaving(true)
    try {
      for (const key of dirty) {
        await api.putSentryConfig(key, buildValue(key))
      }
      setBanner({ type: 'ok', msg: 'Saved. Changes take effect on the next Sentry turn — no re-provision.' })
      load()
    } catch (e) {
      setBanner({ type: 'err', msg: e?.message || 'Save failed' })
    } finally {
      setSaving(false)
    }
  }

  // resolve a model reference (catalog name) to its actual model id.
  const resolveModelId = (ref) => {
    const row = (draft?.shared.models || []).find((m) => m.name === ref)
    return (row && row.model) || ref
  }

  // Test the shared endpoint against the values currently in the form (not the
  // saved/config.yaml ones), using the resolved chat + embedding model ids. A
  // blank api_key falls back to the server default. Per-model endpoint overrides
  // aren't individually tested here.
  async function testConnection() {
    setTest({ pending: true })
    try {
      const r = await api.testSentryConfig({
        base_url: draft.shared.base_url,
        api_key: draft.shared.api_key,
        chat_model: resolveModelId(draft.chat.model),
        embedding_model: resolveModelId(draft.embed.model),
        embedding_dim: Number(draft.embed.embedding_dim) || 0,
      })
      setTest(r)
    } catch (e) {
      setTest({ ok: false, error: e?.message || 'test failed' })
    }
  }

  if (loadErr) {
    return (
      <p className="text-xs text-error">
        {loadErr} — runtime config is available only after Sentry is provisioned.
      </p>
    )
  }
  if (!draft) return <p className="text-xs text-gray-400">Loading runtime config…</p>

  const modelNames = draft.shared.models.map((m) => m.name).filter(Boolean)
  const ModelSelect = ({ value, onChange }) => (
    <select className={inputCls} value={value} onChange={(e) => onChange(e.target.value)}>
      {value && !modelNames.includes(value) && <option value={value}>{value} (literal)</option>}
      {modelNames.map((n) => <option key={n} value={n}>{n}</option>)}
    </select>
  )

  return (
    <div className="space-y-6">
      <p className="text-xs text-gray-400">
        Live configuration stored in the <code className="font-mono text-[#ffb0cf]">agentguard_sentry_config</code> stream.
        Edits apply immediately — no restart or re-provision. (Boot defaults in the server config only seed these on first provision.)
      </p>

      {/* Shared endpoint */}
      <div>
        <h4 className="text-sm font-medium text-gray-100 mb-2">Endpoint</h4>
        <p className="text-[11px] text-gray-400 mb-2">Default endpoint for every model that doesn’t set its own below.</p>
        <div className="grid grid-cols-1 sm:grid-cols-3 gap-3">
          <label className="space-y-1"><span className={labelCls}>Base URL</span>
            <input className={inputCls} value={draft.shared.base_url} onChange={(e) => setShared({ base_url: e.target.value })} placeholder="http://localhost:4000/v1" />
          </label>
          <label className="space-y-1"><span className={labelCls}>API key {byKey.shared?.api_key_set && <span className="text-emerald-400 normal-case">· set</span>}</span>
            <input className={inputCls} type="password" value={draft.shared.api_key} onChange={(e) => setShared({ api_key: e.target.value })} placeholder={byKey.shared?.api_key_set ? '•••••• (unchanged)' : 'none'} />
          </label>
          <label className="space-y-1"><span className={labelCls}>Timeout (s)</span>
            <input className={inputCls} type="number" value={draft.shared.timeout} onChange={(e) => setShared({ timeout: e.target.value })} />
          </label>
        </div>

        <div className="mt-4">
          <p className="text-[11px] text-gray-400 mb-2">Model catalog — named models referenced by chat/embedding below. Leave base URL / API key blank to inherit the shared endpoint.</p>
          <div className="space-y-2">
            <div className="hidden sm:grid grid-cols-[1fr_1.3fr_1.3fr_1fr_auto] gap-2 text-[11px] uppercase tracking-wide text-gray-500 px-1">
              <span>Name</span><span>Model id</span><span>Base URL (optional)</span><span>API key (optional)</span><span></span>
            </div>
            {draft.shared.models.map((m, i) => (
              <div key={i} className="grid grid-cols-2 sm:grid-cols-[1fr_1.3fr_1.3fr_1fr_auto] gap-2 items-center">
                <input className={inputCls} value={m.name} onChange={(e) => setModelRow(i, { name: e.target.value })} placeholder="name" />
                <input className={inputCls} value={m.model} onChange={(e) => setModelRow(i, { model: e.target.value })} placeholder="model id" />
                <input className={inputCls} value={m.base_url} onChange={(e) => setModelRow(i, { base_url: e.target.value })} placeholder="(shared)" />
                <input className={inputCls} type="password" value={m.api_key} onChange={(e) => setModelRow(i, { api_key: e.target.value })} placeholder="(shared / unchanged)" />
                <button onClick={() => removeModelRow(i)} className="text-gray-500 hover:text-error text-lg leading-none px-1" title="Remove model">×</button>
              </div>
            ))}
            <button onClick={addModelRow} className="text-xs text-[#ffb0cf] hover:underline">+ add model</button>
          </div>
        </div>

        <div className="mt-3 flex items-center gap-3 flex-wrap">
          <button onClick={testConnection} className="rounded-sm px-3 py-1.5 text-xs bg-gray-700 text-gray-100 hover:bg-gray-600">Test connection</button>
          <span className="text-[11px] text-gray-500">Tests the values above (not the saved ones) with the chat + embedding models.</span>
          {test?.pending && <span className="text-xs text-gray-400">Testing…</span>}
          {test && !test.pending && (
            <div className="text-xs space-y-0.5 basis-full">
              {test.checks ? (
                <>
                  <div className={test.checks.reachable?.ok ? 'text-emerald-400' : 'text-error'}>
                    {test.checks.reachable?.ok ? '✓ reachable' : `✗ reachable: ${test.checks.reachable?.error || ''}`}
                  </div>
                  {test.checks.chat && (
                    <div className={test.checks.chat.ok ? 'text-emerald-400' : 'text-error'}>
                      {test.checks.chat.ok
                        ? `✓ chat (tool calling: ${test.checks.chat.tool_call_name || 'ok'})`
                        : `✗ chat: ${test.checks.chat.error || 'failed'}`}
                    </div>
                  )}
                  {test.checks.embedding && (
                    <div className={test.checks.embedding.ok ? 'text-emerald-400' : 'text-error'}>
                      {test.checks.embedding.ok
                        ? `✓ embedding dim ${test.checks.embedding.dim}`
                        : `✗ embedding: ${test.checks.embedding.error || 'failed'}`}
                    </div>
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
      </div>

      {/* Chat */}
      <div>
        <h4 className="text-sm font-medium text-gray-100 mb-2">Chat</h4>
        <div className="grid grid-cols-1 sm:grid-cols-3 gap-3 mb-3">
          <label className="space-y-1"><span className={labelCls}>Model</span>
            <ModelSelect value={draft.chat.model} onChange={(v) => setChat({ model: v })} />
          </label>
        </div>
        <label className="space-y-1 block mb-3">
          <span className={labelCls}>System prompt</span>
          <textarea
            className="w-full h-40 rounded-sm bg-gray-900 text-gray-100 font-mono text-xs p-3 focus:outline-none focus:ring-1 focus:ring-[#f256a3]"
            value={draft.chat.system_prompt}
            onChange={(e) => setChat({ system_prompt: e.target.value })}
            placeholder="(empty = built-in default)"
            spellCheck={false}
          />
        </label>
        <div className="grid grid-cols-2 sm:grid-cols-3 gap-3">
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
      </div>

      {/* Embedding */}
      <div>
        <h4 className="text-sm font-medium text-gray-100 mb-2">Embedding <span className="text-[11px] font-normal text-gray-500 normal-case">— used by Sentry memory (Phase 2)</span></h4>
        <div className="grid grid-cols-1 sm:grid-cols-3 gap-3">
          <label className="space-y-1"><span className={labelCls}>Model</span>
            <ModelSelect value={draft.embed.model} onChange={(v) => setEmbed({ model: v })} />
          </label>
          <label className="space-y-1"><span className={labelCls}>Embedding dimension</span>
            <input className={inputCls} type="number" value={draft.embed.embedding_dim} onChange={(e) => setEmbed({ embedding_dim: e.target.value })} />
          </label>
        </div>
      </div>

      <div className="flex items-center gap-3">
        <button onClick={save} disabled={saving || dirty.size === 0}
          className="rounded-sm px-4 py-2 text-sm bg-gradient-to-br from-[#ffb0cf] to-[#f256a3] text-[#3d0023] font-medium disabled:opacity-50">
          {saving ? 'Saving…' : dirty.size ? `Save ${dirty.size} change${dirty.size > 1 ? 's' : ''}` : 'Saved'}
        </button>
        {dirty.size > 0 && !saving && (
          <button onClick={load} className="rounded-sm px-3 py-1.5 text-xs bg-gray-700 text-gray-100 hover:bg-gray-600">Discard</button>
        )}
        {banner && <span className={`text-xs ${banner.type === 'ok' ? 'text-emerald-400' : 'text-error'}`}>{banner.msg}</span>}
      </div>
    </div>
  )
}
