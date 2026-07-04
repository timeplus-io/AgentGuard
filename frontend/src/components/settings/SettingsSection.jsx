import Toggle from '../Toggle'
import DLPConfigEditor from '../DLPConfigEditor'

// DLP keys that remain server-config (config.yaml): the enable switch and the
// embedding dimension (baked into the stream/index). Everything else — endpoint,
// models, timeouts, tokens, entity catalog — is edited live below via the config
// stream, so it's hidden from the flat key list to avoid two sources of truth.
const DLP_SERVER_KEYS = new Set(['dlp.enabled', 'dlp.embedding_dim'])

const SOURCE_LABEL = {
  default: 'default',
  file: 'config file',
  env: 'environment',
  flag: 'CLI flag',
}

function SourceChip({ k }) {
  const label = SOURCE_LABEL[k.source] || k.source
  const title =
    k.source === 'env' ? `Set by ${k.env_var} — file edits have no effect until the variable is removed`
    : k.source === 'flag' ? `Set by --${k.flag} — file edits have no effect`
    : undefined
  return (
    <span title={title} className="text-[10px] uppercase tracking-wide rounded-full px-2 py-0.5 bg-gray-700 text-gray-300">
      {label}
    </span>
  )
}

function KeyField({ k, draftValue, onChange }) {
  const disabled = !k.editable
  const value = draftValue
  const baseInput =
    'w-full bg-gray-900 text-gray-100 text-sm rounded-sm px-3 py-2 outline-none focus:bg-gray-800 disabled:opacity-50 disabled:cursor-not-allowed'

  let input
  if (k.type === 'bool') {
    input = <Toggle checked={!!value} onChange={(v) => onChange(v)} disabled={disabled} />
  } else if (k.type === 'enum') {
    input = (
      <select className={baseInput} value={value ?? ''} disabled={disabled} onChange={(e) => onChange(e.target.value)}>
        {k.enum_values?.map((opt) => <option key={opt} value={opt}>{opt}</option>)}
      </select>
    )
  } else if (k.type === 'secret') {
    input = (
      <input
        type="password"
        className={baseInput}
        disabled={disabled}
        placeholder={k.set ? '•••••••• (set)' : 'not set'}
        value={value ?? ''}
        onChange={(e) => onChange(e.target.value)}
      />
    )
  } else if (k.type === 'int') {
    input = (
      <input
        type="number"
        className={baseInput}
        disabled={disabled}
        value={value ?? ''}
        onChange={(e) => onChange(e.target.value === '' ? '' : Number(e.target.value))}
      />
    )
  } else {
    input = (
      <input
        type="text"
        className={baseInput}
        disabled={disabled}
        value={value ?? ''}
        onChange={(e) => onChange(e.target.value)}
      />
    )
  }

  return (
    <div className="py-3">
      <div className="flex items-center justify-between gap-3 mb-1">
        <div className="flex items-center gap-2">
          <span className="text-sm text-gray-100">{k.label}</span>
          <SourceChip k={k} />
          {k.restart && <span className="text-[10px] text-amber-400">requires restart</span>}
          {k.reprovision && <span className="text-[10px] text-amber-400">requires re-provision</span>}
        </div>
      </div>
      <p className="text-xs text-gray-400 mb-2">{k.description}</p>
      <div className="max-w-md">{input}</div>
    </div>
  )
}

// Renders one settings section card. Section-specific extras: the Timeplus
// test-connection row, and the DLP card's three tonal sub-groups
// (server config / runtime config / operations).
export default function SettingsSection({
  section,
  draftFor,
  setDraft,
  fieldErrors,
  tpTest,
  onTestTimeplus,
  reprovisioning,
  onReprovision,
}) {
  const keys = section.id === 'dlp'
    ? section.keys.filter((k) => DLP_SERVER_KEYS.has(k.key))
    : section.keys

  const keyList = (
    <div className="divide-y divide-gray-700/50">
      {keys.map((k) => (
        <div key={k.key}>
          <KeyField k={k} draftValue={draftFor(k)} onChange={(v) => setDraft(k.key, v)} />
          {fieldErrors[k.key] && <p className="text-error text-xs pb-2">{fieldErrors[k.key]}</p>}
        </div>
      ))}
    </div>
  )

  if (section.id !== 'dlp') {
    return (
      <section className="bg-gray-800 rounded-sm px-6 py-4">
        <h3 className="text-base font-medium text-gray-100 mb-2">{section.title}</h3>
        {keyList}
        {section.id === 'timeplus' && (
          <div className="mt-3 flex items-center gap-3">
            <button onClick={onTestTimeplus} className="rounded-sm px-3 py-1.5 text-xs bg-gray-700 text-gray-100 hover:bg-gray-600">Test connection</button>
            {tpTest?.pending && <span className="text-xs text-gray-400">Testing…</span>}
            {tpTest && !tpTest.pending && (
              <span className={`text-xs ${tpTest.ok ? 'text-emerald-400' : 'text-error'}`}>
                {tpTest.ok ? 'Connection OK' : `Failed: ${tpTest.error}`}
              </span>
            )}
          </div>
        )}
      </section>
    )
  }

  return (
    <section className="bg-gray-800 rounded-sm px-6 py-4">
      <h3 className="text-base font-medium text-gray-100 mb-3">{section.title}</h3>
      <div className="flex flex-col gap-4">
        <div className="bg-gray-900/40 rounded-sm px-4 py-3">
          <h4 className="text-sm font-semibold text-gray-100 mb-1">Server config</h4>
          <p className="text-xs text-gray-400">
            Boot defaults stored in config.yaml — saved with the Save bar below. All other DLP settings are edited live under Runtime configuration.
          </p>
          {keyList}
        </div>
        <div className="bg-gray-900/40 rounded-sm px-4 py-3">
          <h4 className="text-sm font-semibold text-gray-100 mb-1">Runtime configuration</h4>
          <p className="text-xs text-gray-400 mb-2">
            Live state in the agentguard_dlp_config stream — changes apply immediately, with no restart or re-provision, independent of the Save bar.
          </p>
          <DLPConfigEditor />
        </div>
        <div className="bg-gray-900/40 rounded-sm px-4 py-3">
          <h4 className="text-sm font-semibold text-gray-100 mb-1">Operations</h4>
          <div className="flex items-center gap-3">
            <button onClick={onReprovision} disabled={reprovisioning}
              className="rounded-sm px-3 py-1.5 text-xs bg-gray-700 text-gray-100 hover:bg-gray-600 disabled:opacity-50">
              {reprovisioning ? 'Re-provisioning…' : 'Re-provision DLP'}
            </button>
            <span className="text-[11px] text-gray-500">
              Re-detects the embedding dimension and rebuilds the pipeline. Run after switching the embedding model.
            </span>
          </div>
        </div>
      </div>
    </section>
  )
}
