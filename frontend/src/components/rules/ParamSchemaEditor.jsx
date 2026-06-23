import { useState } from 'react'

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
// label, description, a type <select> over all eight tokens, a default input
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
  'number',
  'boolean',
  'string',
  'enum',
  'string_list',
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
      min_length: undefined,
      max_length: undefined,
      min_items: undefined,
      max_items: undefined,
      options: type === 'enum' ? row.options || [] : undefined,
      unit: type === 'duration' ? row.unit || '' : undefined,
    })
  }

  return (
    <div className="bg-surface-container-high rounded-sm p-4 space-y-3">
      <div className="flex items-start gap-2">
        <button
          type="button"
          onClick={() => setOpen((o) => !o)}
          className="shrink-0 text-gray-400 hover:text-[#ffb0cf] transition-colors"
          title={open ? 'Collapse' : 'Expand'}
        >
          <span className="material-symbols-outlined text-[18px] align-middle">
            {open ? 'expand_more' : 'chevron_right'}
          </span>
        </button>

        <div className="flex-1 min-w-0">
          <div className="flex items-center gap-2 flex-wrap">
            <code className="text-xs font-mono text-white">{row.key || '(unnamed)'}</code>
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
            className="text-gray-400 hover:text-white disabled:opacity-30 transition-colors"
            title="Move up"
          >
            <span className="material-symbols-outlined text-[16px] align-middle">arrow_upward</span>
          </button>
          <button
            type="button"
            onClick={() => onMove(1)}
            disabled={index === total - 1}
            className="text-gray-400 hover:text-white disabled:opacity-30 transition-colors"
            title="Move down"
          >
            <span className="material-symbols-outlined text-[16px] align-middle">arrow_downward</span>
          </button>
          <button
            type="button"
            onClick={onRemove}
            className="text-gray-400 hover:text-[#ffb4ab] transition-colors"
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
              {keyError && <p className="text-[10px] text-[#ffb4ab] mt-1">{keyError}</p>}
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
    case 'number':
    case 'duration':
      return (
        <input
          type="number"
          step={row.type === 'number' ? 'any' : '1'}
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
    case 'boolean':
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
    case 'enum':
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
    case 'string_list':
      return (
        <input
          type="text"
          value={Array.isArray(row.default) ? row.default.join(', ') : row.default ?? ''}
          onChange={(e) =>
            set(
              e.target.value
                .split(',')
                .map((s) => s.trim())
                .filter(Boolean),
            )
          }
          placeholder="comma, separated, values"
          className={`${inputCls} font-mono`}
        />
      )
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

// Constraints renders the type-switched constraint editors.
function Constraints({ row, onUpdate }) {
  const numField = (key, label) => (
    <Field label={label}>
      <input
        type="number"
        value={row[key] ?? ''}
        onChange={(e) => {
          const raw = e.target.value
          onUpdate({ [key]: raw === '' ? undefined : Number(raw) })
        }}
        className={inputCls}
      />
    </Field>
  )

  switch (row.type) {
    case 'integer':
    case 'number':
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
    case 'string':
    case 'regex':
      return (
        <div className="grid grid-cols-2 gap-3">
          {numField('min_length', 'Min length')}
          {numField('max_length', 'Max length')}
        </div>
      )
    case 'string_list':
      return (
        <div className="grid grid-cols-2 gap-3">
          {numField('min_items', 'Min items')}
          {numField('max_items', 'Max items')}
        </div>
      )
    case 'enum':
      return <OptionsEditor row={row} onUpdate={onUpdate} />
    case 'boolean':
    default:
      return null
  }
}

// OptionsEditor edits the allowed values of an enum parameter as removable chips.
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
    if (!next.includes(row.default)) patch.default = ''
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
              className="text-gray-500 hover:text-[#ffb4ab]"
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
          className="shrink-0 px-3 rounded-sm bg-surface-variant text-white hover:bg-surface-bright transition-colors"
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
    case 'number':
    case 'duration':
      return ''
    case 'boolean':
      return false
    case 'string_list':
      return []
    case 'enum':
    case 'string':
    case 'regex':
    default:
      return ''
  }
}
