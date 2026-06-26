import { useState } from 'react'
import { toggleMultiChoiceValue } from './paramValues'

// ParamField renders ONE editable rule parameter control, selected by the
// backend-declared parameter type. All parameter-type branching lives here so
// RuleDetail / ConfigurationTab stay type-agnostic (types: integer, float,
// bool, string, choice, list, multi_choice, regex, duration).
//
// Props:
//   param        – schema entry { key, label, description, type, default, min,
//                  max, options, min_items, max_items, unit }
//   value        – current edited value (canonical JS form: number | string |
//                  boolean | string[])
//   overridden   – true when value deviates from the definition default
//   error        – validation message for this field, or falsy
//   onChange(v)  – set the edited value
//   onReset()    – reset this field to its default
const labelCls = 'text-sm font-medium text-white'
const descCls = 'text-[11px] text-gray-400 mt-0.5'
const inputCls =
  'w-full bg-[#0f0c17] text-sm text-gray-100 rounded-sm px-3 py-2 outline-none ' +
  'focus:ring-1 focus:ring-[#ffb0cf]/50 placeholder:text-gray-500'

export default function ParamField({ param, value, defaultValue, overridden, error, onChange, onReset }) {
  return (
    <div className="bg-[#1d1a25] rounded-sm p-4">
      <div className="flex items-start justify-between gap-3">
        <div className="min-w-0">
          <div className="flex items-center gap-2 flex-wrap">
            <span className={labelCls}>{param.label || param.key}</span>
            <code className="text-[10px] text-gray-500 font-mono">{param.key}</code>
            {overridden && (
              <span className="text-[9px] uppercase tracking-wide px-1.5 py-0.5 rounded-full bg-[#ffb0cf]/10 text-[#ffb0cf] border border-[#ffb0cf]/20">
                overridden
              </span>
            )}
          </div>
          {param.description && <p className={descCls}>{param.description}</p>}
        </div>
        {overridden && (
          <button
            type="button"
            onClick={onReset}
            className="shrink-0 text-[10px] uppercase tracking-wide text-gray-400 hover:text-[#ffb0cf] transition-colors"
            title="Reset to default"
          >
            Reset
          </button>
        )}
      </div>

      <div className="mt-3">
        <TypedControl param={param} value={value} onChange={onChange} />
      </div>

      <div className="mt-2 flex items-center justify-between gap-3">
        <span className="text-[10px] text-gray-500">
          Default: <code className="text-gray-400">{formatScalar(param, defaultValue)}</code>
          {param.unit ? <span className="ml-1 text-gray-500">({param.unit})</span> : null}
        </span>
        {error && <span className="text-[10px] text-[#ffb4ab]">{error}</span>}
      </div>
    </div>
  )
}

function TypedControl({ param, value, onChange }) {
  switch (param.type) {
    case 'integer':
    case 'float':
      return (
        <input
          type="number"
          className={inputCls}
          value={value ?? ''}
          step={param.type === 'integer' ? '1' : 'any'}
          min={param.min ?? undefined}
          max={param.max ?? undefined}
          onChange={(e) => {
            const raw = e.target.value
            if (raw === '') return onChange('')
            const n = Number(raw)
            onChange(Number.isNaN(n) ? raw : n)
          }}
        />
      )

    case 'bool':
      return (
        <button
          type="button"
          role="switch"
          aria-checked={!!value}
          onClick={() => onChange(!value)}
          className={`relative inline-flex h-6 w-11 items-center rounded-full transition-colors ${
            value ? 'bg-[#f256a3]' : 'bg-zinc-700'
          }`}
        >
          <span
            className={`inline-block h-4 w-4 transform rounded-full bg-white transition-transform ${
              value ? 'translate-x-6' : 'translate-x-1'
            }`}
          />
        </button>
      )

    case 'choice':
      return (
        <select
          className={inputCls + ' appearance-none'}
          value={value ?? ''}
          onChange={(e) => onChange(e.target.value)}
        >
          {(param.options || []).map((opt) => (
            <option key={opt} value={opt}>
              {opt}
            </option>
          ))}
        </select>
      )

    case 'list':
      return <StringListControl param={param} value={Array.isArray(value) ? value : []} onChange={onChange} />

    case 'multi_choice':
      return <MultiChoiceControl param={param} value={Array.isArray(value) ? value : []} onChange={onChange} />

    case 'regex':
      return (
        <input
          type="text"
          className={inputCls + ' font-mono'}
          value={value ?? ''}
          placeholder="RE2 regular expression"
          onChange={(e) => onChange(e.target.value)}
        />
      )

    case 'duration':
      return (
        <div className="flex items-center gap-2">
          <input
            type="number"
            step="1"
            min={param.min ?? undefined}
            max={param.max ?? undefined}
            className={inputCls}
            value={value ?? ''}
            onChange={(e) => {
              const r = e.target.value
              if (r === '') return onChange('')
              const n = Number(r)
              onChange(Number.isNaN(n) ? r : n)
            }}
          />
          <span className="text-xs text-gray-400">{param.unit || 'units'}</span>
        </div>
      )

    case 'string':
    default:
      return (
        <input
          type="text"
          className={inputCls}
          value={value ?? ''}
          onChange={(e) => onChange(e.target.value)}
        />
      )
  }
}

function MultiChoiceControl({ param, value, onChange }) {
  const options = Array.isArray(param.options) ? param.options : []
  const selected = new Set(value)
  const toggle = (opt) => onChange(toggleMultiChoiceValue(options, value, opt))

  return (
    <div className="flex flex-wrap gap-1.5">
      {options.map((opt) => {
        const active = selected.has(opt)
        return (
          <button
            key={opt}
            type="button"
            onClick={() => toggle(opt)}
            className={`text-[11px] rounded-full px-2.5 py-1 transition-colors ${
              active
                ? 'bg-[#ffb0cf] text-[#3d0023]'
                : 'bg-[#0f0c17] text-gray-300 hover:bg-zinc-700'
            }`}
          >
            <code className="font-mono">{opt}</code>
          </button>
        )
      })}
      {options.length === 0 && <span className="text-[11px] text-gray-500 italic">no options</span>}
    </div>
  )
}

// StringListControl edits a unique, ordered list of strings as removable chips
// plus an add-input (Enter or the + button appends).
function StringListControl({ param, value, onChange }) {
  const [draft, setDraft] = useState('')

  const add = () => {
    const v = draft.trim()
    if (!v || value.includes(v)) {
      setDraft('')
      return
    }
    onChange([...value, v])
    setDraft('')
  }
  const removeAt = (i) => onChange(value.filter((_, idx) => idx !== i))

  return (
    <div>
      <div className="flex flex-wrap gap-1.5 mb-2">
        {value.map((item, i) => (
          <span
            key={item + i}
            className="inline-flex items-center gap-1 text-[11px] bg-[#0f0c17] text-gray-100 rounded-full pl-2.5 pr-1 py-0.5"
          >
            <code className="font-mono">{item}</code>
            <button
              type="button"
              onClick={() => removeAt(i)}
              className="text-gray-500 hover:text-[#ffb4ab]"
              title="Remove"
            >
              <span className="material-symbols-outlined text-[14px] align-middle">close</span>
            </button>
          </span>
        ))}
        {value.length === 0 && <span className="text-[11px] text-gray-500 italic">empty</span>}
      </div>
      <div className="flex gap-2">
        <input
          type="text"
          className={inputCls}
          placeholder="Add item…"
          value={draft}
          onChange={(e) => setDraft(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === 'Enter') {
              e.preventDefault()
              add()
            }
          }}
        />
        <button
          type="button"
          onClick={add}
          className="shrink-0 px-3 rounded-sm bg-zinc-700 text-white hover:bg-zinc-600 transition-colors"
          title="Add"
        >
          <span className="material-symbols-outlined text-[16px] align-middle">add</span>
        </button>
      </div>
      {(param.min_items || param.max_items) && (
        <p className="text-[10px] text-gray-500 mt-1">
          {param.min_items ? `min ${param.min_items}` : ''}
          {param.min_items && param.max_items ? ' · ' : ''}
          {param.max_items ? `max ${param.max_items}` : ''} items
        </p>
      )}
    </div>
  )
}

function formatScalar(param, v) {
  if (v == null) return '—'
  if (Array.isArray(v)) return `[${v.join(', ')}]`
  if (typeof v === 'boolean') return v ? 'true' : 'false'
  return String(v)
}
