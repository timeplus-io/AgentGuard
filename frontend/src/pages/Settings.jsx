import { useState, useEffect, useCallback } from 'react'
import Sidebar from '../components/Sidebar'
import TopNav from '../components/TopNav'
import PageHeader from '../components/PageHeader'
import Toggle from '../components/Toggle'
import DLPConfigEditor from '../components/DLPConfigEditor'
import { api } from '../api/client'

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

export default function Settings() {
  const [data, setData] = useState(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState(null)
  const [drafts, setDrafts] = useState({}) // key -> new value
  const [fieldErrors, setFieldErrors] = useState({})
  const [saving, setSaving] = useState(false)
  const [banner, setBanner] = useState(null) // {type, msg}
  const [tpTest, setTpTest] = useState(null)
  const [reprovisioning, setReprovisioning] = useState(false)

  const load = useCallback(() => {
    setLoading(true)
    api.getConfig()
      .then((d) => { setData(d); setError(null) })
      .catch((e) => setError(e.message))
      .finally(() => setLoading(false))
  }, [])

  useEffect(() => { load() }, [load])

  const setDraft = (key, value) => {
    setDrafts((d) => ({ ...d, [key]: value }))
    setFieldErrors((e) => { const n = { ...e }; delete n[key]; return n })
  }

  const dirtyKeys = Object.keys(drafts)

  const save = async () => {
    setSaving(true)
    setFieldErrors({})
    try {
      const res = await api.updateConfig(drafts)
      setDrafts({})
      setBanner(res.restart_required
        ? { type: 'warn', msg: 'Saved. Some changes require an AgentGuard restart to take effect.' }
        : { type: 'ok', msg: 'Configuration saved.' })
      load()
    } catch (e) {
      if (e.errors) setFieldErrors(e.errors)
      setBanner({ type: 'error', msg: e.message })
    } finally {
      setSaving(false)
    }
  }

  const discard = () => { setDrafts({}); setFieldErrors({}) }

  const draftFor = (k) => (k.key in drafts ? drafts[k.key] : (k.type === 'secret' ? '' : k.value))

  const testTimeplus = async () => {
    const host = drafts['timeplus.host'] ?? findValue('timeplus.host')
    const port = drafts['timeplus.port'] ?? findValue('timeplus.port')
    setTpTest({ pending: true })
    try {
      const r = await api.testTimeplusConfig(host, Number(port))
      setTpTest(r)
    } catch (e) { setTpTest({ ok: false, error: e.message }) }
  }

  const reprovisionDLP = async () => {
    setReprovisioning(true)
    try {
      await api.syncRules()
      setBanner({ type: 'ok', msg: 'DLP re-provision triggered.' })
      load()
    } catch (e) {
      setBanner({ type: 'error', msg: `Re-provision failed: ${e.message}` })
    } finally {
      setReprovisioning(false)
    }
  }

  const findValue = (key) => {
    for (const s of data?.sections || []) {
      for (const k of s.keys) if (k.key === key) return k.value
    }
    return ''
  }

  return (
    <div className="min-h-screen bg-[#15121d] text-gray-100">
      <Sidebar />
      <TopNav />
      <main className="pl-sidebar pt-16 pb-24 min-h-screen">
        <div className="p-8 max-w-4xl">
          <PageHeader title="Settings" subtitle="View and edit AgentGuard server configuration. Keys set by environment variables or CLI flags are read-only." />

          {loading && <p className="text-gray-400 text-sm">Loading…</p>}
          {error && <p className="text-error text-sm">{error}</p>}

          {banner && (
            <div className={`mb-6 rounded-sm px-4 py-3 text-sm ${
              banner.type === 'error' ? 'bg-red-900/40 text-red-300'
              : banner.type === 'warn' ? 'bg-amber-900/40 text-amber-200'
              : 'bg-emerald-900/40 text-emerald-200'}`}>
              {banner.msg}
            </div>
          )}

          {data?.restart_required && (
            <div className="mb-6 rounded-sm px-4 py-3 text-sm bg-amber-900/40 text-amber-200">
              Saved configuration differs from the running server — restart AgentGuard to apply.
            </div>
          )}

          {data?.reprovision_required && (
            <div className="mb-6 rounded-sm px-4 py-3 text-sm bg-amber-900/40 text-amber-200 flex items-center justify-between gap-4">
              <span>DLP model settings differ from what&apos;s provisioned. Re-provision to apply baked-in model/dimension changes. (Changing the embedding dimension does not recreate the existing stream automatically.)</span>
              <button onClick={reprovisionDLP} disabled={reprovisioning}
                className="shrink-0 rounded-sm px-3 py-1.5 text-xs bg-gradient-to-br from-[#ffb0cf] to-[#f256a3] text-[#3d0023] font-medium disabled:opacity-60">
                {reprovisioning ? 'Re-provisioning…' : 'Re-provision DLP now'}
              </button>
            </div>
          )}

          {data?.sections?.map((section) => (
            <section key={section.id} className="mb-6 bg-gray-800 rounded-sm px-6 py-4">
              <h3 className="text-base font-medium text-gray-100 mb-2">{section.title}</h3>
              <div className="divide-y divide-gray-700/50">
                {(section.id === 'dlp'
                  ? section.keys.filter((k) => DLP_SERVER_KEYS.has(k.key))
                  : section.keys
                ).map((k) => (
                  <div key={k.key}>
                    <KeyField k={k} draftValue={draftFor(k)} onChange={(v) => setDraft(k.key, v)} />
                    {fieldErrors[k.key] && <p className="text-error text-xs pb-2">{fieldErrors[k.key]}</p>}
                  </div>
                ))}
              </div>

              {section.id === 'timeplus' && (
                <div className="mt-3 flex items-center gap-3">
                  <button onClick={testTimeplus} className="rounded-sm px-3 py-1.5 text-xs bg-gray-700 text-gray-100 hover:bg-gray-600">Test connection</button>
                  {tpTest?.pending && <span className="text-xs text-gray-400">Testing…</span>}
                  {tpTest && !tpTest.pending && (
                    <span className={`text-xs ${tpTest.ok ? 'text-emerald-400' : 'text-error'}`}>
                      {tpTest.ok ? 'Connection OK' : `Failed: ${tpTest.error}`}
                    </span>
                  )}
                </div>
              )}

              {section.id === 'dlp' && (
                <div className="mt-6 border-t border-gray-700/50 pt-4">
                  <h4 className="text-sm font-semibold text-gray-100 mb-1">Runtime configuration</h4>
                  <DLPConfigEditor />
                </div>
              )}
            </section>
          ))}
        </div>

        {dirtyKeys.length > 0 && (
          <div className="fixed bottom-0 left-0 right-0 pl-sidebar bg-gray-800/95 backdrop-blur-md py-3 z-30 flex items-center justify-between [&>*:first-child]:pl-10 [&>*:last-child]:pr-10">
            <span className="text-sm text-gray-300">{dirtyKeys.length} unsaved change{dirtyKeys.length > 1 ? 's' : ''}</span>
            <div className="flex gap-3">
              <button onClick={discard} disabled={saving} className="rounded-sm px-4 py-2 text-sm bg-gray-700 text-gray-100 hover:bg-gray-600">Discard</button>
              <button onClick={save} disabled={saving}
                className="rounded-sm px-4 py-2 text-sm bg-gradient-to-br from-[#ffb0cf] to-[#f256a3] text-[#3d0023] font-medium disabled:opacity-60">
                {saving ? 'Saving…' : 'Save'}
              </button>
            </div>
          </div>
        )}
      </main>
    </div>
  )
}
