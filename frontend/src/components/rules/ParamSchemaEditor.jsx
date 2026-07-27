import { useState } from 'react'
import { toggleMultiChoiceValue } from './paramValues'

// ParamSchemaEditor edits the parameter SCHEMA of a custom rule definition —
// the typed declaration of each parameter (key / label / type / default /
// constraints), NOT a parameter's value. (ParamField edits a value; this edits
// the schema, so it deliberately does not import ParamField.)
//
// Controlled component:
//   config       – ordered array of schema entries
//                  { key, label, description, type, default, …constraints, unit, options }
//   onChange(arr) – emits the full, updated array
//
// Each row exposes: key (validated ^[a-z][a-z0-9_]{0,63}$ with inline error),
// label, description, a type <select> over all nine tokens, a default input
// appropriate to the type, and a type-switched constraints area. Rows can be
// added, removed and reordered.
//
// Stable React keys: each row carries a `_uid` field (client-side only) so
// that reordering rows does not cause React to reuse the wrong ParamRow
// instance (and thus the wrong expand/collapse state). Use `makeUid()` when
// creating a new row or seeding existing rows. Strip `_uid` before sending
// any config to the backend via `stripInternal(config)`.

// makeUid returns a stable, monotonically increasing id for a new row.
// It is exported so callers that seed an initial config (e.g. RuleCreate's
// normalizeDefinition) can assign _uid to pre-existing rows too.
let _uidSeq = 0
export const makeUid = () => `_p${++_uidSeq}`

// stripInternal removes client-only fields (_uid) from every config entry
// before the array is sent to the backend. Pure function, no side-effects.
export function stripInternal(config) {
  if (!Array.isArray(config)) return config
  return config.map(({ _uid, ...rest }) => rest)
}

export const PARAM_TYPES = [
  'integer',
  'float',
  'bool',
  'string',
  'choice',
  'list',
  'multi_choice',
  'regex',
  'duration',
]

const KEY_RE = /^[a-z][a-z0-9_]{0,63}$/

const labelCls = 'text-[10px] font-semibold text-gray-400 uppercase tracking-widest'
const inputCls =
  'w-full bg-surface-container-lowest text-xs text-gray-100 rounded-sm px-3 py-2 outline-none ' +
  'focus:ring-1 focus:ring-[#ffb0cf]/50 placeholder:text-gray-600'

export function validateKey(key) {
  if (!key) return 'key is required'
  if (!KEY_RE.test(key)) return 'lowercase letter, then letters/digits/underscore (max 64)'
  return null
}

export default function ParamSchemaEditor({ config, onChange }) {
  const rows = Array.isArray(config) ? config : []

  const emit = (next) => onChange(next)

  const update = (i, patch) => {
    const next = rows.map((row, idx) => (idx === i ? { ...row, ...patch } : row))
    emit(next)
  }

  const remove = (i) => emit(rows.filter((_, idx) => idx !== i))

  const move = (i, dir) => {
    const j = i + dir
    if (j < 0 || j >= rows.length) return
    const next = [...rows]
    ;[next[i], next[j]] = [next[j], next[i]]
    emit(next)
  }

  const add = () => emit([...rows, { key: '', label: '', type: 'string', default: '', _uid: makeUid() }])

  // Duplicate-key detection so two parameters can't share a key.
  const keyCounts = rows.reduce((acc, r) => {
    if (r.key) acc[r.key] = (acc[r.key] || 0) + 1
    return acc
  }, {})

  return (
    <div className="space-y-3">
      {rows.length === 0 && (
        <p className="text-xs text-gray-400">
          No parameters yet. Add one to expose a typed knob in the detection template via{' '}
          <code className="text-gray-300 font-mono">{'{{ .Config.<key> }}'}</code>.
        </p>
      )}

      {rows.map((row, i) => (
        <ParamRow
          key={row._uid ?? i}
          row={row}
          index={i}
          total={rows.length}
          duplicate={row.key && keyCounts[row.key] > 1}
          onUpdate={(patch) => update(i, patch)}
          onRemove={() => remove(i)}
          onMove={(dir) => move(i, dir)}
        />
      ))}

      <button
        type="button"
        onClick={add}
        className="flex items-center gap-1.5 px-3 py-2 text-xs font-semibold rounded-sm bg-surface-container-high text-gray-100 hover:bg-surface-variant transition-colors"
      >
        <span className="material-symbols-outlined text-[16px] align-middle">add</span>
        Add parameter
      </button>
    </div>
  )
}

function ParamRow({ row, index, total, duplicate, onUpdate, onRemove, onMove }) {
  const [open, setOpen] = useState(true)
  const keyError = validateKey(row.key) || (duplicate ? 'duplicate key' : null)

  // When the type changes, drop the previous type's constraints/options/unit and
  // reset the default so the schema stays internally consistent.
  const onTypeChange = (type) => {
    onUpdate({
      type,
      default: defaultForType(type),
      min: undefined,
      max: undefined,
      min_items: undefined,
      max_items: undefined,
      options: type === 'choice' || type === 'multi_choice' ? row.options || [] : undefined,
      unit: type === 'duration' ? row.unit || '' : undefined,
    })
  }

  return (
    <div className="bg-surface-container-high rounded-sm p-4 space-y-3">
      <div className="flex items-start gap-2">
        <button
          type="button"
          onClick={() => setOpen((o) => !o)}
          className="shrink-0 text-gray-400 hover:text-primary transition-colors"
          title={open ? 'Collapse' : 'Expand'}
        >
          <span className="material-symbols-outlined text-[18px] align-middle">
            {open ? 'expand_more' : 'chevron_right'}
          </span>
        </button>

        <div className="flex-1 min-w-0">
          <div className="flex items-center gap-2 flex-wrap">
            <code className="text-xs font-mono text-gray-100">{row.key || '(unnamed)'}</code>
            <span className="text-[10px] uppercase tracking-wide px-1.5 py-0.5 rounded-full bg-surface-container-lowest text-gray-400">
              {row.type}
            </span>
            {row.label && <span className="text-[11px] text-gray-400 truncate">{row.label}</span>}
          </div>
        </div>

        <div className="flex items-center gap-1 shrink-0">
          <button
            type="button"
            onClick={() => onMove(-1)}
            disabled={index === 0}
            className="text-gray-400 hover:text-gray-100 disabled:opacity-30 transition-colors"
            title="Move up"
          >
            <span className="material-symbols-outlined text-[16px] align-middle">arrow_upward</span>
          </button>
          <button
            type="button"
            onClick={() => onMove(1)}
            disabled={index === total - 1}
            className="text-gray-400 hover:text-gray-100 disabled:opacity-30 transition-colors"
            title="Move down"
          >
            <span className="material-symbols-outlined text-[16px] align-middle">arrow_downward</span>
          </button>
          <button
            type="button"
            onClick={onRemove}
            className="text-gray-400 hover:text-error transition-colors"
            title="Remove parameter"
          >
            <span className="material-symbols-outlined text-[16px] align-middle">delete</span>
          </button>
        </div>
      </div>

      {open && (
        <div className="space-y-3 pl-7">
          <div className="grid grid-cols-1 md:grid-cols-2 gap-3">
            <Field label="Key *">
              <input
                type="text"
                value={row.key ?? ''}
                onChange={(e) => onUpdate({ key: e.target.value })}
                placeholder="threshold"
                className={`${inputCls} font-mono ${keyError ? 'ring-1 ring-[#ffb4ab]/60' : ''}`}
              />
              {keyError && <p className="text-[10px] text-error mt-1">{keyError}</p>}
            </Field>

            <Field label="Type">
              <select
                value={row.type}
                onChange={(e) => onTypeChange(e.target.value)}
                className={`${inputCls} appearance-none`}
              >
                {PARAM_TYPES.map((t) => (
                  <option key={t} value={t}>
                    {t}
                  </option>
                ))}
              </select>
            </Field>

            <Field label="Label">
              <input
                type="text"
                value={row.label ?? ''}
                onChange={(e) => onUpdate({ label: e.target.value })}
                placeholder="Detection threshold"
                className={inputCls}
              />
            </Field>

            <Field label="Default">
              <DefaultInput row={row} onUpdate={onUpdate} />
            </Field>
          </div>

          <Field label="Description">
            <input
              type="text"
              value={row.description ?? ''}
              onChange={(e) => onUpdate({ description: e.target.value })}
              placeholder="What this parameter controls"
              className={inputCls}
            />
          </Field>

          <Constraints row={row} onUpdate={onUpdate} />
        </div>
      )}
    </div>
  )
}

function Field({ label, children }) {
  return (
    <div className="flex flex-col gap-1.5">
      <span className={labelCls}>{label}</span>
      {children}
    </div>
  )
}

// DefaultInput renders a default-value control appropriate to the parameter type.
function DefaultInput({ row, onUpdate }) {
  const set = (v) => onUpdate({ default: v })
  switch (row.type) {
    case 'integer':
    case 'float':
    case 'duration':
      return (
        <input
          type="number"
          step={row.type === 'float' ? 'any' : '1'}
          value={row.default ?? ''}
          onChange={(e) => {
            const raw = e.target.value
            if (raw === '') return set('')
            const n = Number(raw)
            set(Number.isNaN(n) ? raw : n)
          }}
          className={inputCls}
        />
      )
    case 'bool':
      return (
        <select
          value={row.default ? 'true' : 'false'}
          onChange={(e) => set(e.target.value === 'true')}
          className={`${inputCls} appearance-none`}
        >
          <option value="false">false</option>
          <option value="true">true</option>
        </select>
      )
    case 'choice':
      return (
        <select
          value={row.default ?? ''}
          onChange={(e) => set(e.target.value)}
          className={`${inputCls} appearance-none`}
        >
          <option value="">(none)</option>
          {(row.options || []).map((opt) => (
            <option key={opt} value={opt}>
              {opt}
            </option>
          ))}
        </select>
      )
    case 'list':
      return <ListDefaultInput row={row} onUpdate={onUpdate} />
    case 'multi_choice':
      return <MultiChoiceDefaultEditor row={row} onUpdate={onUpdate} />
    case 'regex':
      return (
        <input
          type="text"
          value={row.default ?? ''}
          onChange={(e) => set(e.target.value)}
          placeholder="RE2 pattern"
          className={`${inputCls} font-mono`}
        />
      )
    case 'string':
    default:
      return (
        <input
          type="text"
          value={row.default ?? ''}
          onChange={(e) => set(e.target.value)}
          className={inputCls}
        />
      )
  }
}

// ListDefaultInput edits a free-form list default as comma-separated text. It
// keeps the raw text in local state (the source of truth for the input) and
// stores the parsed, trimmed, non-empty array underneath. A previous version
// derived the input value from `array.join(', ')` and re-parsed on every
// keystroke, so filter(Boolean) dropped the trailing empty segment the instant a
// comma was typed — the comma vanished and a second item could never be started.
// Seeded once on mount (a type switch remounts this via the DefaultInput branch,
// so there is no external reset to re-sync).
function ListDefaultInput({ row, onUpdate }) {
  const [text, setText] = useState(() =>
    Array.isArray(row.default) ? row.default.join(', ') : typeof row.default === 'string' ? row.default : '',
  )
  return (
    <input
      type="text"
      value={text}
      onChange={(e) => {
        setText(e.target.value)
        onUpdate({
          default: e.target.value
            .split(',')
            .map((s) => s.trim())
            .filter(Boolean),
        })
      }}
      placeholder="comma, separated, values"
      className={`${inputCls} font-mono`}
    />
  )
}

function MultiChoiceDefaultEditor({ row, onUpdate }) {
  const options = Array.isArray(row.options) ? row.options : []
  const values = Array.isArray(row.default) ? row.default : []
  const selected = new Set(values)
  const toggle = (opt) => onUpdate({ default: toggleMultiChoiceValue(options, values, opt) })

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
                ? 'bg-primary text-[#3d0023]'
                : 'bg-surface-container-lowest text-gray-300 hover:bg-surface-variant'
            }`}
          >
            <code className="font-mono">{opt}</code>
          </button>
        )
      })}
      {options.length === 0 && <span className="text-[11px] text-gray-500 italic">add options first</span>}
    </div>
  )
}

// Constraints renders the type-switched constraint editors.
function Constraints({ row, onUpdate }) {
  // intOnly fields back Go *int constraints (min_items/max_items); a fractional
  // value there fails JSON decode with an opaque 400, so
  // force whole numbers (step 1 + truncate on input). min/max back *float64 and
  // may be fractional, so they stay unconstrained.
  const numField = (key, label, { intOnly = false } = {}) => (
    <Field label={label}>
      <input
        type="number"
        step={intOnly ? '1' : 'any'}
        value={row[key] ?? ''}
        onChange={(e) => {
          const raw = e.target.value
          if (raw === '') {
            onUpdate({ [key]: undefined })
            return
          }
          const n = Number(raw)
          if (Number.isNaN(n)) {
            onUpdate({ [key]: undefined })
            return
          }
          onUpdate({ [key]: intOnly ? Math.trunc(n) : n })
        }}
        className={inputCls}
      />
    </Field>
  )

  switch (row.type) {
    case 'integer':
    case 'float':
      return (
        <div className="grid grid-cols-2 gap-3">
          {numField('min', 'Min')}
          {numField('max', 'Max')}
        </div>
      )
    case 'duration':
      return (
        <div className="grid grid-cols-1 md:grid-cols-3 gap-3">
          {numField('min', 'Min')}
          {numField('max', 'Max')}
          <Field label="Unit">
            <input
              type="text"
              value={row.unit ?? ''}
              onChange={(e) => onUpdate({ unit: e.target.value })}
              placeholder="seconds"
              className={inputCls}
            />
          </Field>
        </div>
      )
    case 'list':
      return (
        <div className="grid grid-cols-2 gap-3">
          {numField('min_items', 'Min items', { intOnly: true })}
          {numField('max_items', 'Max items', { intOnly: true })}
        </div>
      )
    case 'multi_choice':
      return (
        <div className="space-y-3">
          <div className="grid grid-cols-2 gap-3">
            {numField('min_items', 'Min items', { intOnly: true })}
            {numField('max_items', 'Max items', { intOnly: true })}
          </div>
          <OptionsEditor row={row} onUpdate={onUpdate} />
        </div>
      )
    case 'choice':
      return <OptionsEditor row={row} onUpdate={onUpdate} />
    case 'bool':
    default:
      return null
  }
}

// OptionsEditor edits the allowed values of a choice/multi_choice parameter as removable chips.
function OptionsEditor({ row, onUpdate }) {
  const [draft, setDraft] = useState('')
  const options = Array.isArray(row.options) ? row.options : []

  const add = () => {
    const v = draft.trim()
    if (!v || options.includes(v)) {
      setDraft('')
      return
    }
    onUpdate({ options: [...options, v] })
    setDraft('')
  }
  const removeAt = (i) => {
    const next = options.filter((_, idx) => idx !== i)
    // Clear the default if it pointed at the removed option.
    const patch = { options: next }
    if (Array.isArray(row.default)) {
      patch.default = row.default.filter((item) => next.includes(item))
    } else if (!next.includes(row.default)) {
      patch.default = ''
    }
    onUpdate(patch)
  }

  return (
    <Field label="Options">
      <div className="flex flex-wrap gap-1.5 mb-2">
        {options.map((opt, i) => (
          <span
            key={opt + i}
            className="inline-flex items-center gap-1 text-[11px] bg-surface-container-lowest text-gray-100 rounded-full pl-2.5 pr-1 py-0.5"
          >
            <code className="font-mono">{opt}</code>
            <button
              type="button"
              onClick={() => removeAt(i)}
              className="text-gray-500 hover:text-error"
              title="Remove option"
            >
              <span className="material-symbols-outlined text-[14px] align-middle">close</span>
            </button>
          </span>
        ))}
        {options.length === 0 && <span className="text-[11px] text-gray-500 italic">no options</span>}
      </div>
      <div className="flex gap-2">
        <input
          type="text"
          value={draft}
          onChange={(e) => setDraft(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === 'Enter') {
              e.preventDefault()
              add()
            }
          }}
          placeholder="Add option…"
          className={inputCls}
        />
        <button
          type="button"
          onClick={add}
          className="shrink-0 px-3 rounded-sm bg-surface-variant text-gray-100 hover:bg-surface-bright transition-colors"
          title="Add option"
        >
          <span className="material-symbols-outlined text-[16px] align-middle">add</span>
        </button>
      </div>
    </Field>
  )
}

function defaultForType(type) {
  switch (type) {
    case 'integer':
    case 'float':
    case 'duration':
      return ''
    case 'bool':
      return false
    case 'list':
    case 'multi_choice':
      return []
    case 'choice':
    case 'string':
    case 'regex':
    default:
      return ''
  }
}
