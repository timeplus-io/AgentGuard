import { useCallback, useEffect, useState } from 'react'
import { api } from '../api/client'
import {
  formComplete,
  buildTestPayload,
  buildEnableChanges,
} from '../utils/dlpEnable.js'

const inputCls =
  'w-full rounded-sm bg-[#2c2834] text-gray-100 text-sm px-3 py-2 placeholder-gray-500 focus:outline-none focus:ring-1 focus:ring-[#ffb0cf]/50'
const labelCls = 'text-[11px] uppercase tracking-wide text-gray-400'

const EMPTY_FORM = {
  base_url: '', api_key: '', embedding_model: '', chat_model: '', summary_model: '',
}

// findConfigValue pulls a flat config key's value out of the /api/config shape.
function findConfigValue(cfg, key) {
  for (const s of cfg?.sections || []) {
    for (const k of s.keys || []) if (k.key === key) return k
  }
  return null
}

// missingPieces lists the not-yet-created Timeplus objects when DLP is enabled
// but partially provisioned.
function missingPieces(status) {
  const missing = []
  if (!status?.enabled) return missing
  if (!status.stream_exists) missing.push('agentguard_dlp_enriched stream')
  if (!status.mv_exists) missing.push('mv_dlp_enrich materialized view')
  for (const [fn, ok] of Object.entries(status.udfs || {})) {
    if (!ok) missing.push(`${fn} UDF`)
  }
  return missing
}

export default function DLPEnableWizard({ status, onProvisioned }) {
  const [form, setForm] = useState(EMPTY_FORM)
  const [apiKeySet, setApiKeySet] = useState(false)
  const [loadErr, setLoadErr] = useState(null)
  const [result, setResult] = useState(null)   // test-dlp response (invalidated on edit)
  const [validating, setValidating] = useState(false)
  const [enabling, setEnabling] = useState(false)
  const [error, setError] = useState(null)

  // Prefill from the config.yaml seed values.
  useEffect(() => {
    let cancelled = false
    api.getConfig()
      .then((cfg) => {
        if (cancelled) return
        const val = (k) => findConfigValue(cfg, k)?.value ?? ''
        setForm({
          base_url: String(val('dlp.base_url') || ''),
          api_key: '',
          embedding_model: String(val('dlp.embedding_model') || ''),
          chat_model: String(val('dlp.risk_model') || ''),
          summary_model: String(val('dlp.summary_model') || ''),
        })
        setApiKeySet(!!findConfigValue(cfg, 'dlp.api_key')?.set)
      })
      .catch((e) => setLoadErr(e?.message || 'Failed to load config'))
    return () => { cancelled = true }
  }, [])

  // Any edit invalidates a prior validation pass (closes the validate-then-swap hole).
  const setField = useCallback((patch) => {
    setForm((f) => ({ ...f, ...patch }))
    setResult(null)
    setError(null)
  }, [])

  const passed = result?.ok === true

  async function validate() {
    setValidating(true)
    setError(null)
    setResult(null)
    try {
      setResult(await api.testDLPConfig(buildTestPayload(form)))
    } catch (e) {
      setError(e?.message || 'Validation request failed')
    } finally {
      setValidating(false)
    }
  }

  async function enable() {
    setEnabling(true)
    setError(null)
    try {
      const changes = buildEnableChanges(form)
      if (status?.enabled) delete changes['dlp.enabled']
      await api.updateConfig(changes)
      await api.syncRules()
      onProvisioned?.()
    } catch (e) {
      if (e?.errors && typeof e.errors === 'object') {
        const details = Object.entries(e.errors).map(([k, v]) => `${k}: ${v}`).join('; ')
        setError(details || e?.message || 'Provisioning failed')
      } else {
        setError(e?.message || 'Provisioning failed')
      }
    } finally {
      setEnabling(false)
    }
  }

  const missing = missingPieces(status)

  return (
    <section className="bg-[#211e29] rounded-sm p-8 max-w-2xl mx-auto flex flex-col gap-6">
      <div className="flex flex-col items-center text-center gap-2">
        <span className="material-symbols-outlined text-5xl text-[#ffb0cf]">policy</span>
        <h3 className="text-lg font-semibold text-gray-100">
          {status?.enabled ? 'Finish provisioning Semantic DLP' : 'Enable Semantic DLP'}
        </h3>
        <p className="text-xs text-gray-400 max-w-md">
          Configure your model endpoint and models, then validate that every model responds.
          Provisioning creates the enrichment stream, UDFs, and materialized view only after
          validation passes — no restart required.
        </p>
        {missing.length > 0 && (
          <ul className="text-[11px] text-gray-400 font-mono mt-1">
            {missing.map((m) => <li key={m}>✗ {m}</li>)}
          </ul>
        )}
      </div>

      {loadErr && <p className="text-xs text-[#ffb4ab]">{loadErr}</p>}

      {/* Endpoint */}
      <div className="grid grid-cols-1 sm:grid-cols-2 gap-3">
        <label className="space-y-1 sm:col-span-2"><span className={labelCls}>Base URL</span>
          <input className={inputCls} value={form.base_url}
            onChange={(e) => setField({ base_url: e.target.value })}
            placeholder="http://host.docker.internal:11434/v1" />
        </label>
        <label className="space-y-1 sm:col-span-2"><span className={labelCls}>
          API key {apiKeySet && <span className="text-emerald-400 normal-case">· set</span>}</span>
          <input className={inputCls} type="password" value={form.api_key}
            onChange={(e) => setField({ api_key: e.target.value })}
            placeholder={apiKeySet ? '•••••• (unchanged)' : 'none'} />
        </label>
        <label className="space-y-1"><span className={labelCls}>Embedding model</span>
          <input className={inputCls} value={form.embedding_model}
            onChange={(e) => setField({ embedding_model: e.target.value })}
            placeholder="mxbai-embed-large" />
        </label>
        <label className="space-y-1"><span className={labelCls}>Chat model</span>
          <input className={inputCls} value={form.chat_model}
            onChange={(e) => setField({ chat_model: e.target.value })}
            placeholder="used by risk / understand / rerank" />
        </label>
        <label className="space-y-1 sm:col-span-2"><span className={labelCls}>
          Summary model <span className="normal-case text-gray-500">(optional — defaults to chat model)</span></span>
          <input className={inputCls} value={form.summary_model}
            onChange={(e) => setField({ summary_model: e.target.value })}
            placeholder="(chat model)" />
        </label>
      </div>

      {/* Actions */}
      <div className="flex items-center gap-3 flex-wrap">
        <button onClick={validate} disabled={validating || !formComplete(form)}
          className="rounded-sm px-4 py-2 text-xs bg-gray-700 text-gray-100 hover:bg-gray-600 disabled:opacity-50">
          {validating ? 'Validating…' : 'Validate models'}
        </button>
        <button onClick={enable} disabled={!passed || enabling}
          className="rounded-sm px-4 py-2 text-xs font-semibold bg-gradient-to-br from-[#ffb0cf] to-[#f256a3] text-[#3d0023] hover:scale-95 transition-transform disabled:opacity-50 disabled:hover:scale-100">
          {enabling ? 'Provisioning…' : 'Enable & provision'}
        </button>
        {!passed && !validating && (
          <span className="text-[11px] text-gray-500">Validate all models before provisioning.</span>
        )}
      </div>

      {error && <p className="text-xs text-[#ffb4ab]">{error}</p>}

      {/* Validation results */}
      {result?.checks && (
        <div className="bg-[#2c2834] rounded-sm p-4 text-xs space-y-1">
          <div className={result.checks.reachable?.ok ? 'text-emerald-400' : 'text-error'}>
            {result.checks.reachable?.ok ? '✓ endpoint reachable'
              : `✗ endpoint: ${result.checks.reachable?.error || 'unreachable'}`}
          </div>
          {result.checks.embedding && (
            <div className={result.checks.embedding.ok ? 'text-emerald-400' : 'text-error'}>
              {result.checks.embedding.ok
                ? `✓ embedding model — vector dim ${result.checks.embedding.dim}`
                : `✗ embedding model: ${result.checks.embedding.error || 'no vector returned'}`}
            </div>
          )}
          {(result.checks.chats || []).map((ch) => (
            <div key={ch.model} className={ch.ok ? 'text-emerald-400' : 'text-error'}>
              {ch.ok ? `✓ chat model ${ch.model}`
                : `✗ chat model ${ch.model}: ${ch.note || 'failed'}`}
            </div>
          ))}
        </div>
      )}

      {!status?.enabled && (
        <p className="text-[11px] text-gray-500 text-center">
          If <code className="font-mono">DLP_ENABLED</code> is set as an environment variable it takes
          precedence — unset it to control this from the UI. Endpoint and model settings become
          editable in <span className="text-gray-400">Settings</span> once provisioned.
        </p>
      )}
    </section>
  )
}
