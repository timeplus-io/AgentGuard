import { useCallback, useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { api } from '../api/client'
import {
  formComplete,
  buildTestPayload,
  buildEnableChanges,
} from '../utils/dlpEnable.js'

const inputCls =
  'w-full rounded-sm bg-surface-container-high text-gray-100 text-sm px-3 py-2 placeholder-gray-500 focus:outline-none focus:ring-1 focus:ring-[#ffb0cf]/50'
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
function missingPieces(status, t) {
  const missing = []
  if (!status?.enabled) return missing
  if (!status.stream_exists) missing.push(t('wizard.missing.stream', { name: 'agentguard_dlp_enriched' }))
  if (!status.mv_exists) missing.push(t('wizard.missing.mv', { name: 'mv_dlp_enrich' }))
  for (const [fn, ok] of Object.entries(status.udfs || {})) {
    if (!ok) missing.push(t('wizard.missing.udf', { name: fn }))
  }
  return missing
}

export default function DLPEnableWizard({ status, onProvisioned }) {
  const { t } = useTranslation('dlp')
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
      .catch((e) => setLoadErr(e?.message || t('wizard.errors.loadConfigFailed')))
    return () => { cancelled = true }
  }, [t])

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
      setError(e?.message || t('wizard.errors.validateFailed'))
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
        setError(details || e?.message || t('wizard.errors.provisionFailed'))
      } else {
        setError(e?.message || t('wizard.errors.provisionFailed'))
      }
    } finally {
      setEnabling(false)
    }
  }

  const missing = missingPieces(status, t)

  return (
    <section className="bg-surface-container rounded-sm p-8 max-w-2xl mx-auto flex flex-col gap-6">
      <div className="flex flex-col items-center text-center gap-2">
        <span className="material-symbols-outlined text-5xl text-primary">policy</span>
        <h3 className="text-lg font-semibold text-gray-100">
          {status?.enabled ? t('wizard.headingResume') : t('wizard.headingNew')}
        </h3>
        <p className="text-xs text-gray-400 max-w-md">
          {t('wizard.description')}
        </p>
        {missing.length > 0 && (
          <ul className="text-[11px] text-gray-400 font-mono mt-1">
            {missing.map((m) => <li key={m}>✗ {m}</li>)}
          </ul>
        )}
      </div>

      {loadErr && <p className="text-xs text-error">{loadErr}</p>}

      {/* Endpoint */}
      <div className="grid grid-cols-1 sm:grid-cols-2 gap-3">
        <label className="space-y-1 sm:col-span-2"><span className={labelCls}>{t('wizard.fields.baseUrlLabel')}</span>
          <input className={inputCls} value={form.base_url}
            onChange={(e) => setField({ base_url: e.target.value })}
            placeholder={t('wizard.fields.baseUrlPlaceholder')} />
        </label>
        <label className="space-y-1 sm:col-span-2"><span className={labelCls}>
          {t('wizard.fields.apiKeyLabel')} {apiKeySet && <span className="text-emerald-400 normal-case">{t('common.apiKeySet')}</span>}</span>
          <input className={inputCls} type="password" value={form.api_key}
            onChange={(e) => setField({ api_key: e.target.value })}
            placeholder={apiKeySet ? t('wizard.fields.apiKeyPlaceholderSet') : t('wizard.fields.apiKeyPlaceholderUnset')} />
        </label>
        <label className="space-y-1"><span className={labelCls}>{t('wizard.fields.embeddingModelLabel')}</span>
          <input className={inputCls} value={form.embedding_model}
            onChange={(e) => setField({ embedding_model: e.target.value })}
            placeholder={t('wizard.fields.embeddingModelPlaceholder')} />
        </label>
        <label className="space-y-1"><span className={labelCls}>{t('wizard.fields.chatModelLabel')}</span>
          <input className={inputCls} value={form.chat_model}
            onChange={(e) => setField({ chat_model: e.target.value })}
            placeholder={t('wizard.fields.chatModelPlaceholder')} />
        </label>
        <label className="space-y-1 sm:col-span-2"><span className={labelCls}>
          {t('wizard.fields.summaryModelLabel')} <span className="normal-case text-gray-500">{t('wizard.fields.summaryModelHint')}</span></span>
          <input className={inputCls} value={form.summary_model}
            onChange={(e) => setField({ summary_model: e.target.value })}
            placeholder={t('wizard.fields.summaryModelPlaceholder')} />
        </label>
      </div>

      {/* Actions */}
      <div className="flex items-center gap-3 flex-wrap">
        <button onClick={validate} disabled={validating || !formComplete(form)}
          className="rounded-sm px-4 py-2 text-xs bg-gray-700 text-gray-100 hover:bg-gray-600 disabled:opacity-50">
          {validating ? t('wizard.actions.validating') : t('wizard.actions.validate')}
        </button>
        <button onClick={enable} disabled={!passed || enabling}
          className="rounded-sm px-4 py-2 text-xs font-semibold bg-gradient-to-br from-[#ffb0cf] to-[#f256a3] text-[#3d0023] hover:scale-95 transition-transform disabled:opacity-50 disabled:hover:scale-100">
          {enabling ? t('wizard.actions.enabling') : t('wizard.actions.enable')}
        </button>
        {!passed && !validating && (
          <span className="text-[11px] text-gray-500">{t('wizard.actions.validateHint')}</span>
        )}
      </div>

      {error && <p className="text-xs text-error">{error}</p>}

      {/* Validation results */}
      {result?.checks && (
        <div className="bg-surface-container-high rounded-sm p-4 text-xs space-y-1">
          <div className={result.checks.reachable?.ok ? 'text-emerald-400' : 'text-error'}>
            {result.checks.reachable?.ok ? t('wizard.validation.reachableOk')
              : t('wizard.validation.reachableFail', { error: result.checks.reachable?.error || t('wizard.validation.reachableFailDefault') })}
          </div>
          {result.checks.embedding && (
            <div className={result.checks.embedding.ok ? 'text-emerald-400' : 'text-error'}>
              {result.checks.embedding.ok
                ? t('wizard.validation.embeddingOk', { dim: result.checks.embedding.dim })
                : t('wizard.validation.embeddingFail', { error: result.checks.embedding.error || t('wizard.validation.embeddingFailDefault') })}
            </div>
          )}
          {(result.checks.chats || []).map((ch) => (
            <div key={ch.model} className={ch.ok ? 'text-emerald-400' : 'text-error'}>
              {ch.ok ? t('wizard.validation.chatOk', { model: ch.model })
                : t('wizard.validation.chatFail', { model: ch.model, note: ch.note || t('wizard.validation.chatFailDefault') })}
            </div>
          ))}
        </div>
      )}

      {!status?.enabled && (
        <p className="text-[11px] text-gray-500 text-center">
          {t('wizard.footer.pre')}<code className="font-mono">DLP_ENABLED</code>{t('wizard.footer.mid')}<span className="text-gray-400">{t('wizard.footer.settingsLabel')}</span>{t('wizard.footer.post')}
        </p>
      )}
    </section>
  )
}
