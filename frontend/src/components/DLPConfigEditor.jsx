import { useCallback, useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { api } from '../api/client'

// Chat UDFs edit a model reference, a token budget, and a prompt template.
// dlp_embed is handled separately (it has max_chars, no prompt).
// placeholders: the template variables that UDF substitutes at call time.
const CHAT_UDFS = [
  { key: 'dlp_understand_query', labelKey: 'config.udfs.understand', placeholders: ['{question}'], json: true },
  { key: 'dlp_rerank', labelKey: 'config.udfs.rerank', placeholders: ['{count}', '{question}', '{items}'], json: true },
  { key: 'dlp_risk', labelKey: 'config.udfs.risk', placeholders: ['{entity_types}', '{message}'], json: true },
  { key: 'dlp_summarize', labelKey: 'config.udfs.summarize', placeholders: ['{question}', '{evidence}'], json: false },
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
  const draft = {
    shared: {
      base_url: shared.base_url || '',
      api_key: '', // never echoed; blank = keep existing
      timeout: shared.timeout ?? 50,
      models: modelsToRows(shared.models),
    },
    udfs: {},
  }
  for (const { key } of CHAT_UDFS) {
    const v = byKey[key]?.value || {}
    draft.udfs[key] = { model: v.model || '', max_tokens: v.max_tokens ?? 512, prompt: v.prompt || '' }
  }
  const emb = byKey.dlp_embed?.value || {}
  draft.embed = { model: emb.model || '', max_chars: emb.max_chars ?? 8000 }
  return draft
}

export default function DLPConfigEditor() {
  const { t } = useTranslation('dlp')
  const [byKey, setByKey] = useState(null)
  const [draft, setDraft] = useState(null)
  const [dirty, setDirty] = useState(() => new Set())
  const [entitiesText, setEntitiesText] = useState('')
  const [entitiesErr, setEntitiesErr] = useState(null)
  const [loadErr, setLoadErr] = useState(null)
  const [saving, setSaving] = useState(false)
  const [banner, setBanner] = useState(null)
  const [test, setTest] = useState(null)

  const load = useCallback(() => {
    api.getDLPConfig()
      .then((res) => {
        const map = {}
        for (const r of res.config || []) map[r.key] = r
        setByKey(map)
        setDraft(buildDraft(map))
        setEntitiesText(JSON.stringify(map.dlp_extract_entities?.value ?? {}, null, 2))
        setDirty(new Set())
        setEntitiesErr(null)
        setLoadErr(null)
      })
      .catch((e) => setLoadErr(e?.message || t('config.errors.loadFailed')))
  }, [t])

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
  const setUdf = (key, patch) => {
    setDraft((d) => ({ ...d, udfs: { ...d.udfs, [key]: { ...d.udfs[key], ...patch } } }))
    mark(key)
  }
  const setEmbed = (patch) => {
    setDraft((d) => ({ ...d, embed: { ...d.embed, ...patch } }))
    mark('dlp_embed')
  }

  function buildValue(key) {
    const prev = byKey[key]?.value || {}
    if (key === 'shared') {
      const v = { ...prev, base_url: draft.shared.base_url, timeout: Number(draft.shared.timeout) || 0, models: rowsToModels(draft.shared.models) }
      v.api_key = draft.shared.api_key // blank → server preserves the stored key
      return v
    }
    if (key === 'dlp_embed') {
      return { ...prev, model: draft.embed.model, max_chars: Number(draft.embed.max_chars) || 0 }
    }
    if (key === 'dlp_extract_entities') {
      return JSON.parse(entitiesText)
    }
    const u = draft.udfs[key]
    // prompt: '' = use the UDF's built-in default (clearing the field resets it)
    return { ...prev, model: u.model, max_tokens: Number(u.max_tokens) || 0, prompt: u.prompt }
  }

  async function save() {
    setBanner(null)
    if (dirty.has('dlp_extract_entities')) {
      try { JSON.parse(entitiesText) } catch { setEntitiesErr(t('config.entityCatalog.invalidJson')); return }
    }
    setEntitiesErr(null)
    setSaving(true)
    try {
      for (const key of dirty) {
        await api.putDLPConfig(key, buildValue(key))
      }
      setBanner({ type: 'ok', msg: t('config.banner.saved') })
      load()
    } catch (e) {
      setBanner({ type: 'err', msg: e?.message || t('config.errors.saveFailed') })
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
  // saved/config.yaml ones), using the resolved embedding + risk model ids. A
  // blank api_key falls back to the server default. Per-model endpoint overrides
  // aren't individually tested here.
  async function testEndpoint() {
    setTest({ pending: true })
    try {
      const r = await api.testDLPConfig({
        base_url: draft.shared.base_url,
        api_key: draft.shared.api_key,
        embedding_model: resolveModelId(draft.embed.model),
        embedding_dim: 0, // backend uses the provisioned/config dimension
        risk_model: resolveModelId(draft.udfs.dlp_risk.model),
      })
      setTest(r)
    } catch (e) {
      setTest({ ok: false, error: e?.message || t('config.shared.errors.testFailed') })
    }
  }

  if (loadErr) {
    return (
      <p className="text-xs text-error">
        {loadErr}{t('config.errors.loadErrSuffix')}
      </p>
    )
  }
  if (!draft) return <p className="text-xs text-gray-400">{t('config.loadingRuntime')}</p>

  const modelNames = draft.shared.models.map((m) => m.name).filter(Boolean)
  const ModelSelect = ({ value, onChange }) => (
    <select className={inputCls} value={value} onChange={(e) => onChange(e.target.value)}>
      {value && !modelNames.includes(value) && <option value={value}>{t('config.catalog.literalSuffix', { value })}</option>}
      {modelNames.map((n) => <option key={n} value={n}>{n}</option>)}
    </select>
  )

  return (
    <div className="space-y-6">
      <p className="text-xs text-gray-400">
        {t('config.intro.pre')}<code className="font-mono text-primary">agentguard_dlp_config</code>{t('config.intro.post')}
      </p>

      {/* Shared endpoint */}
      <div>
        <h4 className="text-sm font-medium text-gray-100 mb-2">{t('config.shared.heading')}</h4>
        <p className="text-[11px] text-gray-400 mb-2">{t('config.shared.description')}</p>
        <div className="grid grid-cols-1 sm:grid-cols-3 gap-3">
          <label className="space-y-1"><span className={labelCls}>{t('config.shared.baseUrlLabel')}</span>
            <input className={inputCls} value={draft.shared.base_url} onChange={(e) => setShared({ base_url: e.target.value })} placeholder="http://localhost:4000/v1" />
          </label>
          <label className="space-y-1"><span className={labelCls}>{t('config.shared.apiKeyLabel')} {byKey.shared?.api_key_set && <span className="text-emerald-400 normal-case">{t('common.apiKeySet')}</span>}</span>
            <input className={inputCls} type="password" value={draft.shared.api_key} onChange={(e) => setShared({ api_key: e.target.value })} placeholder={byKey.shared?.api_key_set ? t('config.shared.apiKeyPlaceholderSet') : t('config.shared.apiKeyPlaceholderUnset')} />
          </label>
          <label className="space-y-1"><span className={labelCls}>{t('config.shared.timeoutLabel')}</span>
            <input className={inputCls} type="number" value={draft.shared.timeout} onChange={(e) => setShared({ timeout: e.target.value })} />
          </label>
        </div>
        <div className="mt-3 flex items-center gap-3 flex-wrap">
          <button onClick={testEndpoint} className="rounded-sm px-3 py-1.5 text-xs bg-gray-700 text-gray-100 hover:bg-gray-600">{t('config.shared.testButton')}</button>
          <span className="text-[11px] text-gray-500">{t('config.shared.testHint')}</span>
          {test?.pending && <span className="text-xs text-gray-400">{t('config.shared.testing')}</span>}
          {test && !test.pending && (test.checks ? (
            <div className="text-xs space-y-0.5 basis-full">
              <div className={test.checks.reachable?.ok ? 'text-emerald-400' : 'text-error'}>
                {test.checks.reachable?.ok ? t('config.shared.check.reachableOk') : t('config.shared.check.reachableFail', { error: test.checks.reachable?.error || '' })}
              </div>
              {test.checks.embedding && (
                <div className={test.checks.embedding.ok ? 'text-emerald-400' : 'text-error'}>
                  {test.checks.embedding.ok
                    ? t('config.shared.check.embeddingOk', { dim: test.checks.embedding.dim })
                    : t('config.shared.check.embeddingFail', { dim: test.checks.embedding.dim ?? '?', expected: test.checks.embedding.expected })}
                </div>
              )}
              {test.checks.chat && (
                <div className={test.checks.chat.ok ? 'text-emerald-400' : 'text-error'}>
                  {test.checks.chat.ok ? t('config.shared.check.chatOk') : t('config.shared.check.chatFail', { note: test.checks.chat.note || t('config.shared.check.chatFailDefault') })}
                </div>
              )}
            </div>
          ) : (
            <span className={`text-xs ${test.ok ? 'text-emerald-400' : 'text-error'}`}>
              {test.ok ? t('config.shared.check.reachableOkSimple') : t('config.shared.check.failedSimple', { error: test.error })}
            </span>
          ))}
        </div>
      </div>

      {/* Model catalog */}
      <div>
        <h4 className="text-sm font-medium text-gray-100 mb-2">{t('config.catalog.heading')}</h4>
        <p className="text-[11px] text-gray-400 mb-2">{t('config.catalog.description')}</p>
        <div className="space-y-2">
          <div className="hidden sm:grid grid-cols-[1fr_1.3fr_1.3fr_1fr_auto] gap-2 text-[11px] uppercase tracking-wide text-gray-500 px-1">
            <span>{t('config.catalog.nameHeader')}</span><span>{t('config.catalog.modelIdHeader')}</span><span>{t('config.catalog.baseUrlHeader')}</span><span>{t('config.catalog.apiKeyHeader')}</span><span></span>
          </div>
          {draft.shared.models.map((m, i) => (
            <div key={i} className="grid grid-cols-2 sm:grid-cols-[1fr_1.3fr_1.3fr_1fr_auto] gap-2 items-center">
              <input className={inputCls} value={m.name} onChange={(e) => setModelRow(i, { name: e.target.value })} placeholder={t('config.catalog.namePlaceholder')} />
              <input className={inputCls} value={m.model} onChange={(e) => setModelRow(i, { model: e.target.value })} placeholder={t('config.catalog.modelIdPlaceholder')} />
              <input className={inputCls} value={m.base_url} onChange={(e) => setModelRow(i, { base_url: e.target.value })} placeholder={t('config.catalog.baseUrlPlaceholder')} />
              <input className={inputCls} type="password" value={m.api_key} onChange={(e) => setModelRow(i, { api_key: e.target.value })} placeholder={t('config.catalog.apiKeyPlaceholder')} />
              <button onClick={() => removeModelRow(i)} className="text-gray-500 hover:text-error text-lg leading-none px-1" title={t('config.catalog.removeModelTitle')}>×</button>
            </div>
          ))}
          <button onClick={addModelRow} className="text-xs text-primary hover:underline">{t('config.catalog.addModel')}</button>
        </div>
      </div>

      {/* Per-UDF */}
      <div>
        <h4 className="text-sm font-medium text-gray-100 mb-2">{t('config.functions.heading')}</h4>
        <div className="space-y-2">
          <div className="grid grid-cols-[1.3fr_1fr_0.8fr] gap-3 items-center">
            <span className="text-sm text-gray-300">{t('config.functions.embeddingsLabel')}</span>
            <ModelSelect value={draft.embed.model} onChange={(v) => setEmbed({ model: v })} />
            <label className="flex items-center gap-1 text-[11px] text-gray-400">{t('config.functions.maxChars')}
              <input className={inputCls} type="number" value={draft.embed.max_chars} onChange={(e) => setEmbed({ max_chars: e.target.value })} />
            </label>
          </div>
          {CHAT_UDFS.map(({ key, labelKey, placeholders, json }) => (
            <div key={key} className="space-y-1">
              <div className="grid grid-cols-[1.3fr_1fr_0.8fr] gap-3 items-center">
                <span className="text-sm text-gray-300">{t(labelKey)}</span>
                <ModelSelect value={draft.udfs[key].model} onChange={(v) => setUdf(key, { model: v })} />
                <label className="flex items-center gap-1 text-[11px] text-gray-400">{t('config.functions.maxTokens')}
                  <input className={inputCls} type="number" value={draft.udfs[key].max_tokens} onChange={(e) => setUdf(key, { max_tokens: e.target.value })} />
                </label>
              </div>
              <details>
                <summary className="text-[11px] text-gray-500 cursor-pointer select-none hover:text-gray-400">
                  {t('config.functions.promptTemplate')} <span className="text-gray-600">{t('config.functions.variablesLabel')} {placeholders.join(' ')}</span>
                </summary>
                <textarea
                  className="mt-1 w-full h-28 rounded-sm bg-gray-900 text-gray-100 font-mono text-xs p-3 focus:outline-none focus:ring-1 focus:ring-[#f256a3]"
                  value={draft.udfs[key].prompt}
                  onChange={(e) => setUdf(key, { prompt: e.target.value })}
                  placeholder={t('config.functions.promptPlaceholder')}
                  spellCheck={false}
                />
                <p className="text-[11px] text-gray-500">
                  {json ? t('config.functions.jsonHint') : ''}{t('config.functions.resetHint')}
                </p>
              </details>
            </div>
          ))}
        </div>
      </div>

      {/* Entity catalog (advanced JSON) */}
      <details className="group">
        <summary className="text-sm font-medium text-gray-100 cursor-pointer select-none">{t('config.entityCatalog.summary')} <span className="text-[11px] text-gray-500">{t('config.entityCatalog.badge')}</span></summary>
        <p className="text-[11px] text-gray-400 my-2">
          {t('config.entityCatalog.description.pre')}<code className="font-mono">dlp_extract_entities</code>{t('config.entityCatalog.description.mid')}<code className="font-mono">{'{type, kind, pattern|terms, store, tag}'}</code>{t('config.entityCatalog.description.post')}
        </p>
        <textarea
          className="w-full h-64 rounded-sm bg-gray-900 text-gray-100 font-mono text-xs p-3 focus:outline-none focus:ring-1 focus:ring-[#f256a3]"
          value={entitiesText}
          onChange={(e) => { setEntitiesText(e.target.value); mark('dlp_extract_entities') }}
          spellCheck={false}
        />
        {entitiesErr && <p className="text-error text-xs">{entitiesErr}</p>}
      </details>

      <div className="flex items-center gap-3">
        <button onClick={save} disabled={saving || dirty.size === 0}
          className="rounded-sm px-4 py-2 text-sm bg-gradient-to-br from-[#ffb0cf] to-[#f256a3] text-[#3d0023] font-medium disabled:opacity-50">
          {saving ? t('config.actions.saving') : dirty.size ? t('config.actions.saveCount', { count: dirty.size }) : t('config.actions.saved')}
        </button>
        {dirty.size > 0 && !saving && (
          <button onClick={load} className="rounded-sm px-3 py-1.5 text-xs bg-gray-700 text-gray-100 hover:bg-gray-600">{t('config.actions.discard')}</button>
        )}
        {banner && <span className={`text-xs ${banner.type === 'ok' ? 'text-emerald-400' : 'text-error'}`}>{banner.msg}</span>}
      </div>
    </div>
  )
}
