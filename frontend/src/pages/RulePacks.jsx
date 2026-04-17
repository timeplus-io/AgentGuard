import { useState, useEffect, useRef } from 'react'
import { Link, useNavigate } from 'react-router-dom'
import { BarChart, Bar, XAxis, YAxis, Tooltip, Cell, ResponsiveContainer } from 'recharts'
import Sidebar from '../components/Sidebar'
import TopNav from '../components/TopNav'
import PageHeader from '../components/PageHeader'
import Toggle from '../components/Toggle'
import { api } from '../api/client'

const CATEGORY_ICONS = {
  prompt_injection_and_hijacking:    'phishing',
  sensitive_data_exposure:           'visibility_off',
  agent_privilege_escalation:        'admin_panel_settings',
  supply_chain_compromise:           'hub',
  resource_exhaustion_and_dos:       'speed',
  data_exfiltration_and_leakage:     'data_loss_prevention',
  credential_theft_and_harvesting:   'key_off',
  lateral_movement_and_pivoting:     'swap_horiz',
}


export default function RulePacks() {
  const navigate = useNavigate()
  const [rules, setRules] = useState([])
  const [catalog, setCatalog] = useState([])
  const [loading, setLoading] = useState(true)
  const [loadError, setLoadError] = useState(null)
  const [totalDetections, setTotalDetections] = useState(0)
  const density = rules.length > 0 ? Math.round(rules.filter(r => r.enabled).length * 100 / rules.length) : 0
  const [chartData, setChartData] = useState({ openThreats: [], totalEvents: [] })
  const [chartsLoading, setChartsLoading] = useState(true)
  const [importing, setImporting] = useState(false)
  const [importResult, setImportResult] = useState(null)
  const importInputRef = useRef(null)
  const [confirmToggle, setConfirmToggle] = useState(null) // { id, newVal, name }
  const [confirmInstall, setConfirmInstall] = useState(null) // catalog entry
  const [installing, setInstalling] = useState(null) // rule id being installed

  const loadRules = () =>
    api.listRules().then(data => {
      if (data.rule_packs) {
        setRules(data.rule_packs.map(r => ({
          id: r.id,
          icon: CATEGORY_ICONS[r.category] || 'shield',
          name: r.name,
          desc: r.description,
          enabled: r.enabled,
          version: r.enabled ? (r.version || null) : null,
          active: r.enabled,
          detections24h: r.detections_24h || 0,
        })))
      }
      if (data.total_detections_24h !== undefined) setTotalDetections(data.total_detections_24h)
    })

  useEffect(() => {
    Promise.all([
      loadRules(),
      api.getRulesCatalog().then(data => setCatalog(data.catalog || [])),
    ])
      .catch(() => { setLoadError('Failed to load rules. Please refresh.') })
      .finally(() => setLoading(false))
  }, [])

  useEffect(() => {
    let cancelled = false
    async function loadCharts() {
      try {
        // Fetch all threats via pagination
        const openByRule = {}
        let page = 1
        const pageSize = 200
        while (true) {
          const res = await api.getThreats({ page, limit: pageSize })
          for (const t of (res.threats || [])) {
            if (t.status === 'open') {
              const key = t.rule_id || 'unknown'
              openByRule[key] = { rule_id: key, rule_name: t.rule_name || key, cnt: (openByRule[key]?.cnt || 0) + 1 }
            }
          }
          if ((res.threats || []).length < pageSize) break
          page++
        }
        const openThreats = Object.values(openByRule).sort((a, b) => b.cnt - a.cnt)

        const evtMap = {}
        const sql = `SELECT rule_id, rule_name, count() AS cnt FROM table(agentguard_security_events) GROUP BY rule_id, rule_name ORDER BY cnt DESC`
        for await (const row of api.protonQuery(sql)) {
          evtMap[row.rule_id] = { rule_id: row.rule_id, rule_name: row.rule_name || row.rule_id, cnt: Number(row.cnt) }
        }
        const totalEvents = Object.values(evtMap).sort((a, b) => b.cnt - a.cnt)

        if (!cancelled) setChartData({ openThreats, totalEvents })
      } catch (e) {
        console.error('Chart load failed', e)
      } finally {
        if (!cancelled) setChartsLoading(false)
      }
    }
    loadCharts()
    return () => { cancelled = true }
  }, [])

  const handleToggle = (id, newVal) => {
    const rule = rules.find(r => r.id === id)
    setConfirmToggle({ id, newVal, name: rule?.name || id })
  }

  const confirmDoToggle = async () => {
    const { id, newVal } = confirmToggle
    setConfirmToggle(null)
    // Optimistic update — apply immediately and revert on error.
    // Do NOT re-fetch from the server here: Proton's table() scan has a
    // commit lag, so an immediate GET /rules would race with the INSERT
    // and return stale data, snapping the UI back to the old state.
    setRules(prev => prev.map(r => r.id === id ? { ...r, enabled: newVal, active: newVal, version: newVal ? (r.version || '') : null } : r))
    try {
      await api.toggleRule(id)
    } catch {
      setRules(prev => prev.map(r => r.id === id ? { ...r, enabled: !newVal, active: !newVal } : r))
    }
  }

  const confirmDoInstall = async () => {
    const entry = confirmInstall
    setConfirmInstall(null)
    await handleInstall(entry.id)
  }

  const handleInstall = async (id) => {
    setInstalling(id)
    try {
      await api.installRule(id)
      await Promise.all([
        loadRules(),
        api.getRulesCatalog().then(data => setCatalog(data.catalog || [])),
      ])
    } catch (err) {
      setLoadError(err?.message || 'Install failed — please try again')
    } finally {
      setInstalling(null)
    }
  }

  const handleImport = async (e) => {
    const file = e.target.files?.[0]
    if (!file) return
    e.target.value = ''
    setImporting(true)
    setImportResult(null)
    try {
      const result = await api.importCatalog(file)
      setImportResult(result)
      // Reload catalog to reflect imported entries.
      api.getRulesCatalog().then(data => setCatalog(data.catalog || []))
    } catch (err) {
      setImportResult({ errors: [err.message] })
    } finally {
      setImporting(false)
    }
  }

  const handleExport = () => {
    const a = document.createElement('a')
    a.href = api.exportCatalogUrl()
    a.download = 'agentguard-catalog.zip'
    a.click()
  }

  const uninstalledCatalog = catalog.filter(e => !e.installed)

  return (
    <div className="min-h-screen bg-[#15121d] text-[#e7dff0]">
      <Sidebar />
      <TopNav />


      {/* Toggle confirmation modal */}
      {confirmToggle && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 backdrop-blur-sm">
          <div className="bg-[#1d1a25] border border-[#574149]/20 rounded-sm p-8 max-w-sm w-full mx-4 space-y-6">
            <h3 className="text-base font-semibold text-white">
              {confirmToggle.newVal ? 'Enable' : 'Disable'} Rule Pack
            </h3>
            <p className="text-sm text-zinc-400">
              Are you sure you want to <span className="text-white font-medium">{confirmToggle.newVal ? 'enable' : 'disable'}</span> <span className="text-[#ffb0cf]">{confirmToggle.name}</span>?
              {!confirmToggle.newVal && <span className="block mt-2 text-xs text-zinc-500">This will pause the detection MV and stop new events from being detected.</span>}
              {confirmToggle.newVal && <span className="block mt-2 text-xs text-zinc-500">This will create or resume the detection MV and start monitoring.</span>}
            </p>
            <div className="flex gap-3 justify-end">
              <button
                onClick={() => setConfirmToggle(null)}
                className="px-4 py-2 text-xs font-semibold rounded-sm bg-zinc-700 text-white hover:bg-zinc-600 transition-colors"
              >
                Cancel
              </button>
              <button
                onClick={confirmDoToggle}
                className={`px-4 py-2 text-xs font-semibold rounded-sm transition-colors ${
                  confirmToggle.newVal
                    ? 'bg-primary-button text-primary-button-text'
                    : 'bg-zinc-700 text-white hover:bg-zinc-600'
                }`}
              >
                {confirmToggle.newVal ? 'Enable' : 'Disable'}
              </button>
            </div>
          </div>
        </div>
      )}
      {/* Install confirmation modal */}
      {confirmInstall && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 backdrop-blur-sm">
          <div className="bg-[#1d1a25] border border-[#574149]/20 rounded-sm p-8 max-w-sm w-full mx-4 space-y-6">
            <h3 className="text-base font-semibold text-white">Install Rule Pack</h3>
            <p className="text-sm text-zinc-400">
              Are you sure you want to install <span className="text-[#ffb0cf] font-medium">{confirmInstall.name}</span>?
              <span className="block mt-2 text-xs text-zinc-500">This will create a detection MV and start monitoring immediately.</span>
            </p>
            <div className="flex gap-3 justify-end">
              <button
                onClick={() => setConfirmInstall(null)}
                className="px-4 py-2 text-xs font-semibold rounded-sm bg-zinc-700 text-white hover:bg-zinc-600 transition-colors"
              >
                Cancel
              </button>
              <button
                onClick={confirmDoInstall}
                className="px-4 py-2 text-xs font-semibold rounded-sm bg-primary-button text-primary-button-text transition-colors"
              >
                Install
              </button>
            </div>
          </div>
        </div>
      )}

      <main className="pl-sidebar pt-16 pb-8 min-h-screen">
        <div className="p-6 lg:p-10 space-y-8">
          {/* Hidden file input for import */}
          <input
            ref={importInputRef}
            type="file"
            accept=".zip"
            className="hidden"
            onChange={handleImport}
          />

          <PageHeader
            title="Rule Packs"
            subtitle="Configure and manage security policies enforced across all monitored AI agents."
            actions={
              <div className="flex items-center gap-3">
                <button
                  onClick={handleExport}
                  className="px-4 py-2 text-xs font-semibold rounded-sm border border-zinc-700 text-zinc-300 hover:text-white hover:border-zinc-500 transition-colors flex items-center gap-1.5"
                >
                  <span className="material-symbols-outlined text-[14px]">download</span>
                  Export Catalog
                </button>
                <button
                  onClick={() => importInputRef.current?.click()}
                  disabled={importing}
                  className="px-4 py-2 text-xs font-semibold rounded-sm border border-zinc-700 text-zinc-300 hover:text-white hover:border-zinc-500 transition-colors flex items-center gap-1.5 disabled:opacity-50"
                >
                  <span className="material-symbols-outlined text-[14px]">upload</span>
                  {importing ? 'Importing…' : 'Import Catalog'}
                </button>
                <button
                  onClick={() => navigate('/rules/new')}
                  className="px-4 py-2 bg-primary-button text-primary-button-text text-xs font-semibold rounded-sm hover:scale-[0.98] transition-all"
                >
                  New Custom Rule
                </button>
              </div>
            }
          />

          {loadError && (
            <div className="mb-6 px-4 py-3 bg-[#93000a]/20 border border-[#ffb4ab]/20 rounded text-sm text-[#ffb4ab]">
              {loadError}
            </div>
          )}

          {importResult && (
            <div className={`p-4 rounded-sm text-xs flex items-start gap-3 ${importResult.errors?.length ? 'bg-[#93000a]/20 border border-[#ffb4ab]/20 text-[#ffb4ab]' : 'bg-[#1d2a1d] border border-green-700/30 text-green-400'}`}>
              <span className="material-symbols-outlined text-[14px] mt-0.5 shrink-0">
                {importResult.errors?.length ? 'error' : 'check_circle'}
              </span>
              <div className="space-y-1">
                {(importResult.added?.length > 0) && <p>Added: <span className="font-mono">{importResult.added.join(', ')}</span></p>}
                {(importResult.updated?.length > 0) && <p>Updated: <span className="font-mono">{importResult.updated.join(', ')}</span></p>}
                {(importResult.skipped?.length > 0) && <p className="text-zinc-500">Skipped: <span className="font-mono">{importResult.skipped.join(', ')}</span></p>}
                {(importResult.errors?.length > 0) && importResult.errors.map((e, i) => <p key={i}>{e}</p>)}
                {!importResult.errors?.length && !importResult.added?.length && !importResult.updated?.length && <p>No changes — all entries already up to date.</p>}
              </div>
              <button onClick={() => setImportResult(null)} className="ml-auto text-zinc-500 hover:text-white shrink-0">
                <span className="material-symbols-outlined text-[14px]">close</span>
              </button>
            </div>
          )}

          <section className="grid grid-cols-1 md:grid-cols-2 xl:grid-cols-4 gap-6">
            {loading ? (
              [1,2,3,4].map(i => <div key={i} className="bg-[#1d1a25] p-6 rounded-sm h-64 animate-pulse" />)
            ) : rules.map(rule => (
              <div key={rule.id} className="group bg-[#1d1a25] p-6 rounded-sm flex flex-col gap-6 hover:bg-[#2c2834] transition-all duration-300 relative overflow-hidden">
                {rule.active && (
                  <div className="absolute top-0 right-0 w-24 h-24 bg-[#ffb0cf]/5 blur-3xl rounded-full -translate-y-12 translate-x-12"></div>
                )}
                <div className="flex justify-between items-start z-10">
                  <div className={`w-12 h-12 bg-[#0f0c17] flex items-center justify-center rounded-sm ${rule.active ? 'text-[#ffb0cf]' : 'text-zinc-500'}`}>
                    <span className="material-symbols-outlined" style={rule.active ? { fontVariationSettings: "'FILL' 1" } : {}}>{rule.icon}</span>
                  </div>
                  <Toggle checked={rule.enabled} onChange={val => handleToggle(rule.id, val)} />
                </div>
                <div className="z-10">
                  <h3 className="text-lg font-semibold tracking-tight text-white mb-2">{rule.name}</h3>
                  <p className="text-xs text-zinc-400 leading-relaxed line-clamp-3 min-h-[4.5rem]">{rule.desc}</p>
                  {rule.detections24h > 0 && (
                    <p className="text-[10px] font-mono text-[#ffb0cf] mt-2">{rule.detections24h.toLocaleString()} events detected (24h)</p>
                  )}
                </div>
                <div className="mt-auto flex items-center justify-between z-10">
                  <Link to={`/rules/${rule.id}`} className="text-xs font-semibold text-[#ffb0cf] tracking-wider uppercase hover:underline decoration-2 underline-offset-4">
                    View Details
                  </Link>
                  <span className="text-[10px] text-zinc-500 flex items-center gap-1">
                    {rule.version ? (
                      <>
                        <span className="material-symbols-outlined text-[12px] text-[#ffb0cf]" style={{ fontVariationSettings: "'FILL' 1" }}>bolt</span>
                        {rule.version}
                      </>
                    ) : (
                      <>
                        <span className="material-symbols-outlined text-[12px]">pause_circle</span>
                        Inactive
                      </>
                    )}
                  </span>
                </div>
              </div>
            ))}
          </section>

          {uninstalledCatalog.length > 0 && (
            <section>
              <h4 className="text-xs font-semibold text-zinc-500 uppercase tracking-widest mb-4">Available to Install</h4>
              <div className="grid grid-cols-1 md:grid-cols-2 xl:grid-cols-4 gap-4">
                {uninstalledCatalog.map(entry => (
                  <div key={entry.id} className="bg-[#1d1a25]/60 border border-dashed border-zinc-700/40 p-5 rounded-sm flex flex-col gap-4">
                    <div className="flex justify-between items-start">
                      <div className="w-10 h-10 bg-[#0f0c17] flex items-center justify-center rounded-sm text-zinc-600">
                        <span className="material-symbols-outlined">{entry.is_custom ? 'tune' : (CATEGORY_ICONS[entry.category] || 'shield')}</span>
                      </div>
                      <div className="flex items-center gap-2">
                        {entry.is_custom && (
                          <span className="text-[10px] px-1.5 py-0.5 rounded-full bg-[#2c2834] text-zinc-400 border border-zinc-700/40 font-semibold uppercase tracking-wide">Custom</span>
                        )}
                        <span className={`text-[10px] px-2 py-0.5 rounded-full font-semibold uppercase tracking-wide border ${
                          entry.severity === 'critical'
                            ? 'bg-[#ffb0cf]/10 text-[#ffb0cf] border-[#ffb0cf]/20'
                            : 'bg-amber-500/10 text-amber-400 border-amber-500/20'
                        }`}>{entry.severity}</span>
                      </div>
                    </div>
                    <div>
                      <h3 className="text-sm font-semibold text-zinc-400 mb-1">{entry.name}</h3>
                      <p className="text-xs text-zinc-600 leading-relaxed line-clamp-2">{entry.description}</p>
                    </div>
                    <button
                      onClick={() => setConfirmInstall(entry)}
                      disabled={installing === entry.id}
                      className="mt-auto w-full px-3 py-2 text-xs font-semibold rounded-sm bg-primary-button text-primary-button-text disabled:opacity-50 transition-opacity"
                    >
                      {installing === entry.id ? 'Installing…' : 'Install'}
                    </button>
                  </div>
                ))}
              </div>
            </section>
          )}

          <section className="grid grid-cols-1 lg:grid-cols-3 gap-8">
            {/* Bar Charts */}
            <div className="lg:col-span-2 grid grid-cols-1 md:grid-cols-2 gap-6">
              {/* Open Threats by Rule */}
              <div className="bg-[#211e29] p-8 rounded-sm">
                <h4 className="text-sm font-semibold text-zinc-400 uppercase tracking-widest mb-6">Open Threats by Rule</h4>
                {chartsLoading ? (
                  <div className="h-40 animate-pulse bg-[#2c2834] rounded-sm" />
                ) : chartData.openThreats.length === 0 ? (
                  <p className="text-xs text-zinc-600 py-4">No open threats.</p>
                ) : (
                  <ResponsiveContainer width="100%" height={chartData.openThreats.length * 44 + 8}>
                    <BarChart data={chartData.openThreats} layout="vertical" margin={{ left: 0, right: 24, top: 0, bottom: 0 }}>
                      <XAxis type="number" hide />
                      <YAxis type="category" dataKey="rule_name" width={140} tick={{ fill: '#71717a', fontSize: 10 }} />
                      <Tooltip
                        contentStyle={{ background: '#15121d', border: '1px solid #2c2834', borderRadius: 4 }}
                        labelStyle={{ color: '#e7dff0', fontSize: 11 }}
                        itemStyle={{ color: '#ffb0cf', fontSize: 11 }}
                        formatter={(v) => [v, 'Open threats']}
                      />
                      <Bar dataKey="cnt" radius={[0, 2, 2, 0]}>
                        {chartData.openThreats.map(entry => (
                          <Cell key={entry.rule_id} fill="#f256a3" />
                        ))}
                      </Bar>
                    </BarChart>
                  </ResponsiveContainer>
                )}
              </div>

              {/* Total Security Events by Rule */}
              <div className="bg-[#211e29] p-8 rounded-sm">
                <h4 className="text-sm font-semibold text-zinc-400 uppercase tracking-widest mb-6">Security Events by Rule</h4>
                {chartsLoading ? (
                  <div className="h-40 animate-pulse bg-[#2c2834] rounded-sm" />
                ) : chartData.totalEvents.length === 0 ? (
                  <p className="text-xs text-zinc-600 py-4">No events recorded.</p>
                ) : (
                  <ResponsiveContainer width="100%" height={chartData.totalEvents.length * 44 + 8}>
                    <BarChart data={chartData.totalEvents} layout="vertical" margin={{ left: 0, right: 24, top: 0, bottom: 0 }}>
                      <XAxis type="number" hide />
                      <YAxis type="category" dataKey="rule_name" width={140} tick={{ fill: '#71717a', fontSize: 10 }} />
                      <Tooltip
                        contentStyle={{ background: '#15121d', border: '1px solid #2c2834', borderRadius: 4 }}
                        labelStyle={{ color: '#e7dff0', fontSize: 11 }}
                        itemStyle={{ color: '#ffb0cf', fontSize: 11 }}
                        formatter={(v) => [v, 'Total events']}
                      />
                      <Bar dataKey="cnt" radius={[0, 2, 2, 0]}>
                        {chartData.totalEvents.map(entry => (
                          <Cell key={entry.rule_id} fill="#574149" />
                        ))}
                      </Bar>
                    </BarChart>
                  </ResponsiveContainer>
                )}
              </div>
            </div>

            <div className="flex flex-col gap-6">
              <div className="bg-gradient-to-br from-[#2c2834] to-[#211e29] p-8 rounded-sm relative overflow-hidden group">
                <div className="absolute top-0 left-0 w-1 h-full bg-[#ffb0cf]"></div>
                <p className="text-[10px] text-zinc-500 uppercase tracking-widest font-semibold mb-4">Total Detections (24h)</p>
                <h5 className="text-3xl font-bold text-white">{totalDetections.toLocaleString()}</h5>
                <p className="text-xs text-zinc-500 mt-2 font-medium">from active rule MVs</p>
              </div>
              <div className="bg-[#2c2834] p-8 rounded-sm border-l border-[#574149]/20">
                <p className="text-[10px] text-zinc-500 uppercase tracking-widest font-semibold mb-4">Active Rule Density</p>
                <h5 className="text-3xl font-bold text-white">{density}<span className="text-base text-zinc-500">%</span></h5>
                <div className="w-full bg-[#0f0c17] h-1.5 mt-6 rounded-full overflow-hidden">
                  <div className="bg-[#ffb0cf] h-full transition-all duration-500" style={{ width: `${density}%` }}></div>
                </div>
              </div>
            </div>
          </section>
        </div>
      </main>
    </div>
  )
}
