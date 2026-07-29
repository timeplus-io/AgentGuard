import { useEffect, useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'
import ParamField from './ParamField'
import { api } from '../../api/client'
import { formatNumber } from '../../i18n/format'

// ConfigurationTab renders the editable parameter form for an installed rule.
// It owns edit state, client-side validation (immediate feedback; the backend is
// authoritative and its 400 field errors are merged on save), reset-one/reset-all,
// dirty tracking with unsaved-change protection, and the PUT save.
//
// Props:
//   ruleId    – rule id
//   schema    – ordered array of parameter definitions
//   defaults  – { key: normalizedDefault }
//   effective – { key: effectiveValue } (defaults overlaid with current overrides)
//   enabled   – whether the rule has a live MV (drives the redeploy notice)
//   onSaved(updatedInstance) – called after a successful configuration save
export default function ConfigurationTab({ ruleId, schema, defaults, effective, enabled, onSaved }) {
  const { t } = useTranslation('rules')
  // baseline is the last-saved effective config; edits diff against it.
  const baseline = useMemo(() => buildInitial(schema, effective, defaults), [schema, effective, defaults])
  const [values, setValues] = useState(baseline)
  const [saving, setSaving] = useState(false)
  const [saveError, setSaveError] = useState(null)
  const [fieldErrorsFromServer, setFieldErrorsFromServer] = useState({})

  // Re-seed when the rule reloads (e.g. after a save elsewhere).
  useEffect(() => {
    setValues(baseline)
    setFieldErrorsFromServer({})
    setSaveError(null)
  }, [baseline])

  const clientErrors = useMemo(() => validateAll(schema, values, t), [schema, values, t])
  const errors = { ...clientErrors, ...fieldErrorsFromServer }
  const dirty = useMemo(() => !deepEqual(values, baseline), [values, baseline])
  const hasErrors = Object.keys(clientErrors).length > 0

  // Unsaved-change protection: warn on browser navigation away while dirty.
  useEffect(() => {
    if (!dirty) return undefined
    const handler = (e) => {
      e.preventDefault()
      e.returnValue = ''
    }
    window.addEventListener('beforeunload', handler)
    return () => window.removeEventListener('beforeunload', handler)
  }, [dirty])

  if (!schema || schema.length === 0) {
    return (
      <p className="text-xs text-gray-400">
        {t('config.noParams')}
      </p>
    )
  }

  const setOne = (id, v) => {
    setValues((prev) => ({ ...prev, [id]: v }))
    setFieldErrorsFromServer((prev) => (prev[id] ? { ...prev, [id]: undefined } : prev))
  }
  const resetOne = (id) => setOne(id, defaults[id])
  const resetAll = () => {
    const next = {}
    for (const p of schema) next[p.key] = defaults[p.key]
    setValues(next)
    setFieldErrorsFromServer({})
  }
  // Discard reverts to the last-saved baseline (effective config with its saved
  // overrides), NOT to defaults — discarding edits must not wipe saved overrides.
  // After this, values === baseline so the form is clean (dirty becomes false).
  const discard = () => {
    setValues(baseline)
    setFieldErrorsFromServer({})
    setSaveError(null)
  }

  const save = async () => {
    if (hasErrors || saving) return
    setSaving(true)
    setSaveError(null)
    setFieldErrorsFromServer({})
    try {
      // Send the full edited map; the backend canonicalizes (drops values equal
      // to the current default) and resets any omitted key to its default.
      const updated = await api.configureRule(ruleId, values, 'configure via UI')
      onSaved?.(updated)
    } catch (e) {
      // Surface per-parameter 400 field errors next to their controls.
      const fields = e?.body?.fields
      if (Array.isArray(fields)) {
        const m = {}
        for (const f of fields) m[f.parameter] = f.message
        setFieldErrorsFromServer(m)
      }
      // 409 means an upgrade became available since this tab opened (the page
      // normally gates configuration behind Upgrade). The 409 body carries
      // conflicts/warnings, not field errors — give a clear, actionable message
      // rather than a raw banner.
      if (e?.status === 409) {
        setSaveError(t('config.errors.pendingUpgrade'))
      } else {
        setSaveError(e?.message || t('config.errors.saveFailed'))
      }
    } finally {
      setSaving(false)
    }
  }

  return (
    <div className="space-y-4">
      <div className="flex items-center justify-between">
        <p className="text-[11px] text-gray-400">
          {t('config.helpText')}
        </p>
        <button
          type="button"
          onClick={resetAll}
          disabled={!dirty && !someOverridden(values, defaults)}
          className="text-[10px] uppercase tracking-wide text-gray-400 hover:text-primary transition-colors disabled:opacity-40"
        >
          {t('config.resetAll')}
        </button>
      </div>

      <div className="space-y-3">
        {schema.map((p) => (
          <ParamField
            key={p.key}
            param={p}
            value={values[p.key]}
            defaultValue={defaults[p.key]}
            overridden={!deepEqual(values[p.key], defaults[p.key])}
            error={errors[p.key]}
            onChange={(v) => setOne(p.key, v)}
            onReset={() => resetOne(p.key)}
          />
        ))}
      </div>

      {saveError && (
        <div className="bg-red-500/10 border border-red-500/20 rounded-sm px-4 py-3 flex items-start gap-3">
          <span className="material-symbols-outlined text-red-400 text-[18px] shrink-0 mt-0.5">error</span>
          <p className="text-xs text-red-400 break-all">{saveError}</p>
        </div>
      )}

      {/* A live rule's MV is dropped/recreated on save (Proton has no in-place
          ALTER), so any in-flight stateful window is lost. Only meaningful when
          the rule is enabled — a disabled rule has no MV to redeploy. */}
      {enabled && (
        <p className="text-[11px] text-gray-500">
          {t('config.redeployNote')}
        </p>
      )}

      {/* Save bar (sticky-ish): only actionable when there are unsaved, valid changes. */}
      <div className="flex items-center justify-end gap-3 pt-2">
        {dirty && <span className="text-[11px] text-amber-400 mr-auto">{t('config.unsavedChanges')}</span>}
        <button
          type="button"
          onClick={discard}
          disabled={!dirty}
          className="px-4 py-2 text-xs font-semibold rounded-sm bg-zinc-700 text-white hover:bg-zinc-600 transition-colors disabled:opacity-40"
        >
          {t('config.discard')}
        </button>
        <button
          type="button"
          onClick={save}
          disabled={!dirty || hasErrors || saving}
          className="px-4 py-2 text-xs font-semibold rounded-sm bg-primary-button text-primary-button-text transition-colors disabled:opacity-40"
        >
          {saving ? t('config.saving') : t('config.save')}
        </button>
      </div>
    </div>
  )
}

// buildInitial seeds the editable map from effective values, falling back to
// defaults; arrays are copied so edits don't mutate props.
function buildInitial(schema, effective, defaults) {
  const out = {}
  for (const p of schema || []) {
    const v = effective?.[p.key] ?? defaults?.[p.key]
    out[p.key] = Array.isArray(v) ? [...v] : v
  }
  return out
}

function someOverridden(values, defaults) {
  return Object.keys(values).some((key) => !deepEqual(values[key], defaults[key]))
}

// validateAll mirrors the backend's per-type constraints for immediate feedback.
// The backend remains authoritative (its field errors are merged on save).
function validateAll(schema, values, t) {
  const errs = {}
  for (const p of schema || []) {
    const e = validateOne(p, values[p.key], t)
    if (e) errs[p.key] = e
  }
  return errs
}

function validateOne(p, v, t) {
  switch (p.type) {
    case 'integer':
    case 'float': {
      if (v === '' || v == null) return t('config.validation.required')
      const n = Number(v)
      if (Number.isNaN(n) || !Number.isFinite(n)) return t('config.validation.mustBeFiniteNumber')
      if (p.type === 'integer' && !Number.isInteger(n)) return t('config.validation.mustBeWholeNumber')
      if (p.min != null && n < p.min) return t('config.validation.min', { min: formatNumber(p.min) })
      if (p.max != null && n > p.max) return t('config.validation.max', { max: formatNumber(p.max) })
      return null
    }
    case 'string': {
      const s = v ?? ''
      if (typeof s !== 'string') return t('config.validation.mustBeString')
      return null
    }
    case 'choice':
      if (!p.options?.includes(v)) return t('config.validation.mustBeAllowedOption')
      return null
    case 'list':
    case 'multi_choice': {
      const list = Array.isArray(v) ? v : []
      if (list.length === 0) return t('config.validation.minItems1')
      if (new Set(list).size !== list.length) return t('config.validation.uniqueItems')
      if (p.min_items != null && list.length < p.min_items) {
        return t('config.validation.minItemsN', { min: formatNumber(p.min_items) })
      }
      if (p.max_items != null && list.length > p.max_items) {
        return t('config.validation.maxItemsN', { max: formatNumber(p.max_items) })
      }
      if (p.type === 'multi_choice') {
        const options = Array.isArray(p.options) ? p.options : []
        const invalid = list.find((item) => !options.includes(item))
        if (invalid) return t('config.validation.notAllowedOption', { value: invalid })
      }
      return null
    }
    case 'regex': {
      const s = v ?? ''
      if (typeof s !== 'string') return t('config.validation.mustBeString')
      if (s === '') return t('config.validation.patternRequired')
      // Do NOT compile the pattern client-side: the backend validates it as RE2
      // (authoritative), and JS RegExp syntax diverges from RE2 (e.g. inline flags
      // like `(?m)`), so `new RegExp` would falsely block valid AgentGuard
      // patterns — and clientErrors gates the Save button. Let the server return
      // the authoritative field error on save.
      return null
    }
    case 'duration': {
      if (v === '' || v == null) return t('config.validation.required')
      const n = Number(v)
      if (!Number.isInteger(n)) return t('config.validation.mustBeWholeNumber')
      if (p.min != null && n < p.min) return t('config.validation.min', { min: formatNumber(p.min) })
      if (p.max != null && n > p.max) return t('config.validation.max', { max: formatNumber(p.max) })
      return null
    }
    case 'bool':
    default:
      return null
  }
}

function deepEqual(a, b) {
  if (a === b) return true
  if (Array.isArray(a) && Array.isArray(b)) {
    return a.length === b.length && a.every((x, i) => x === b[i])
  }
  // Numbers may arrive as json.Number-decoded strings vs JS numbers; compare loosely.
  if (typeof a !== typeof b) return String(a) === String(b)
  return false
}
