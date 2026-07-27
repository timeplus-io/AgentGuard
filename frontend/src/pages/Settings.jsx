import { useState, useEffect, useCallback } from 'react'
import { useLocation, useNavigate } from 'react-router-dom'
import Sidebar from '../components/Sidebar'
import TopNav from '../components/TopNav'
import PageHeader from '../components/PageHeader'
import SettingsNav from '../components/settings/SettingsNav'
import SettingsSection from '../components/settings/SettingsSection'
import {
  keyToSectionMap,
  dirtyCountsBySection,
  sectionIdsWithFlag,
  activeSectionFromHash,
} from '../utils/settingsNav'
import { api } from '../api/client'

export default function Settings() {
  const [data, setData] = useState(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState(null)
  const [drafts, setDrafts] = useState({}) // key -> new value
  const [fieldErrors, setFieldErrors] = useState({})
  const [saving, setSaving] = useState(false)
  const [configSavedAt, setConfigSavedAt] = useState(0)
  const [banner, setBanner] = useState(null) // {type, msg}
  const [tpTest, setTpTest] = useState(null)
  const [reprovisioning, setReprovisioning] = useState(false)
  const location = useLocation()
  const navigate = useNavigate()

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
      if (res.sentry_provision_error) {
        setBanner({ type: 'warn', msg: `Saved, but Sentry provisioning failed: ${res.sentry_provision_error}` })
      } else if (res.restart_required) {
        setBanner({ type: 'warn', msg: 'Saved. Some changes require an AgentGuard restart to take effect.' })
      } else if (res.sentry_provisioned) {
        setBanner({ type: 'ok', msg: 'Configuration saved. Sentry streams provisioned.' })
      } else {
        setBanner({ type: 'ok', msg: 'Configuration saved.' })
      }
      setConfigSavedAt(Date.now()) // tells live-config editors (Sentry/DLP) to refetch
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

  const sections = data?.sections || []
  const activeId = activeSectionFromHash(location.hash, sections)
  const dirtyCounts = dirtyCountsBySection(drafts, keyToSectionMap(sections))
  const restartIds = sectionIdsWithFlag(sections, 'restart')
  const reprovisionIds = sectionIdsWithFlag(sections, 'reprovision')

  return (
    <div className="min-h-screen bg-[#15121d] text-gray-100">
      <Sidebar />
      <TopNav />
      <main className="pl-sidebar pt-16 pb-24 min-h-screen">
        <div className="p-8 max-w-5xl">
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

          {sections.length > 0 && (
            <div className="flex gap-6 items-start">
              <SettingsNav
                sections={sections}
                activeId={activeId}
                dirtyCounts={dirtyCounts}
                restartIds={restartIds}
                reprovisionIds={reprovisionIds}
                showRestart={!!data?.restart_required}
                showReprovision={!!data?.reprovision_required}
                onSelect={(id) => { navigate(`#${id}`, { replace: true }); window.scrollTo(0, 0) }}
              />
              <div className="flex-1 min-w-0">
                {sections.map((s) => (
                  <div key={s.id} className={s.id === activeId ? '' : 'hidden'}>
                    <SettingsSection
                      section={s}
                      draftFor={draftFor}
                      setDraft={setDraft}
                      fieldErrors={fieldErrors}
                      tpTest={tpTest}
                      onTestTimeplus={testTimeplus}
                      reprovisioning={reprovisioning}
                      onReprovision={reprovisionDLP}
                      configSavedAt={configSavedAt}
                    />
                  </div>
                ))}
              </div>
            </div>
          )}
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
