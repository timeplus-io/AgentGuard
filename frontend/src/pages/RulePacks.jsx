import { useState, useEffect, useRef } from 'react'
import { Link, useNavigate } from 'react-router-dom'
import { useTranslation } from 'react-i18next'
import Sidebar from '../components/Sidebar'
import TopNav from '../components/TopNav'
import PageHeader from '../components/PageHeader'
import Toggle from '../components/Toggle'
import { api } from '../api/client'
import { rankBySearch } from '../utils/ruleSearch.js'
import { useAuth } from '../context/AuthContext'
import { writeKey } from '../lib/perm'

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


const RULES_PAGE_SIZE = 12

export default function RulePacks() {
  const { t } = useTranslation('rules')
  const navigate = useNavigate()
  const { can } = useAuth()
  const canWrite = can(writeKey('rules')) // gate install/toggle/import/create
  const [rules, setRules] = useState([])
  const [catalog, setCatalog] = useState([])
  const [loading, setLoading] = useState(true)
  const [loadError, setLoadError] = useState(null)
  const [importing, setImporting] = useState(false)
  const [importResult, setImportResult] = useState(null)
  const importInputRef = useRef(null)
  const [confirmToggle, setConfirmToggle] = useState(null) // { id, newVal, name }
  const [confirmInstall, setConfirmInstall] = useState(null) // catalog entry
  const [installing, setInstalling] = useState(null) // rule id being installed
  const [search, setSearch] = useState('')
  const [rulesPage, setRulesPage] = useState(1)
  const [catalogPage, setCatalogPage] = useState(1)

  // When the user types a query, rank by relevance (issue #124) so that an
  // exact / name match wins over a description-only match. Empty search keeps
  // the existing enabled-first + alphabetical ordering.
  const filteredRules = rankBySearch(
    rules,
    search,
    r => ({ name: r.name, description: r.desc, category: r.category }),
    (a, b) => (a.enabled !== b.enabled ? (a.enabled ? -1 : 1) : (a.name || '').localeCompare(b.name || '')),
  )
  const totalRulesPages = Math.ceil(filteredRules.length / RULES_PAGE_SIZE)
  const pagedRules = filteredRules.slice((rulesPage - 1) * RULES_PAGE_SIZE, rulesPage * RULES_PAGE_SIZE)

  const loadRules = () =>
    api.listRules().then(data => {
      if (data.rule_packs) {
        setRules(data.rule_packs.map(r => ({
          id: r.id,
          icon: CATEGORY_ICONS[r.category] || 'shield',
          name: r.name,
          desc: r.description,
          category: r.category,
          enabled: r.enabled,
          version: r.enabled ? (r.version || null) : null,
          active: r.enabled,
          detections24h: r.detections_24h || 0,
          block_policy: r.block_policy || 'log_only',
        })))
      }
    })

  useEffect(() => {
    Promise.all([
      loadRules(),
      api.getRulesCatalog().then(data => setCatalog(data.catalog || [])),
    ])
      .catch(() => { setLoadError(t('list.errors.loadFailed')) })
      .finally(() => setLoading(false))
  }, [t])

  useEffect(() => { setRulesPage(1); setCatalogPage(1) }, [search])

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
    } catch (err) {
      console.error('rules: toggle failed:', err)
      setRules(prev => prev.map(r => r.id === id ? { ...r, enabled: !newVal, active: !newVal } : r))
      setLoadError(t('list.errors.toggleFailed', {
        action: newVal ? t('actions.enable') : t('actions.disable'),
        message: err?.message ?? String(err),
      }))
    }
  }

  const confirmDoInstall = async () => {
    const entry = confirmInstall
    setConfirmInstall(null)
    await handleInstall(entry.id)
  }

  const handleInstall = async (id) => {
    setInstalling(id)
    // Optimistic update — same pattern as the toggle handler. A re-fetch
    // immediately after the INSERT races with Proton's table() commit lag and
    // returns stale "not installed" data, snapping the UI back.
    const entry = catalog.find(e => e.id === id)
    if (!entry) {
      setInstalling(null)
      return
    }
    const newRule = {
      id: entry.id,
      icon: CATEGORY_ICONS[entry.category] || 'shield',
      name: entry.name,
      desc: entry.description,
      category: entry.category,
      enabled: true,
      version: entry.version || '',
      active: true,
      detections24h: 0,
      block_policy: entry.block_policy || 'log_only',
    }
    setCatalog(prev => prev.map(e => e.id === id ? { ...e, installed: true } : e))
    setRules(prev => [newRule, ...prev])
    try {
      await api.installRule(id)
    } catch (err) {
      // Revert
      setCatalog(prev => prev.map(e => e.id === id ? { ...e, installed: false } : e))
      setRules(prev => prev.filter(r => r.id !== id))
      setLoadError(err?.message || t('list.errors.installFailed'))
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
      api.getRulesCatalog().then(data => setCatalog(data.catalog || [])).catch(err => setLoadError(err?.message || t('list.errors.reloadCatalogFailed')))
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

  const uninstalledCatalog = rankBySearch(
    catalog.filter(e => !e.installed),
    search,
    e => e,
    (a, b) => (a.name || '').localeCompare(b.name || ''),
  )
  const totalCatalogPages = Math.ceil(uninstalledCatalog.length / RULES_PAGE_SIZE)
  const pagedCatalog = uninstalledCatalog.slice((catalogPage - 1) * RULES_PAGE_SIZE, catalogPage * RULES_PAGE_SIZE)

  return (
    <div className="min-h-screen bg-surface text-gray-100">
      <Sidebar />
      <TopNav />


      {/* Toggle confirmation modal */}
      {confirmToggle && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 backdrop-blur-sm">
          <div className="bg-surface-container-low border border-outline-variant/20 rounded-sm p-8 max-w-sm w-full mx-4 space-y-6">
            <h3 className="text-base font-semibold text-gray-100">
              {t('list.confirmToggle.title', { action: confirmToggle.newVal ? t('actions.enable') : t('actions.disable') })}
            </h3>
            <p className="text-sm text-gray-400">
              {t('list.confirmToggle.bodyPrefix')} <span className="text-gray-100 font-medium">{confirmToggle.newVal ? t('actions.enable') : t('actions.disable')}</span> <span className="text-primary">{confirmToggle.name}</span>{t('list.confirmToggle.bodySuffix')}
              {!confirmToggle.newVal && <span className="block mt-2 text-xs text-gray-400">{t('list.confirmToggle.disableNote')}</span>}
              {confirmToggle.newVal && <span className="block mt-2 text-xs text-gray-400">{t('list.confirmToggle.enableNote')}</span>}
            </p>
            <div className="flex gap-3 justify-end">
              <button
                onClick={() => setConfirmToggle(null)}
                className="px-4 py-2 text-xs font-semibold rounded-sm bg-zinc-700 text-white hover:bg-zinc-600 transition-colors"
              >
                {t('actions.cancel')}
              </button>
              <button
                onClick={confirmDoToggle}
                className={`px-4 py-2 text-xs font-semibold rounded-sm transition-colors ${
                  confirmToggle.newVal
                    ? 'bg-primary-button text-primary-button-text border border-pink-500'
                    : 'bg-zinc-700 text-white hover:bg-zinc-600'
                }`}
              >
                {confirmToggle.newVal ? t('actions.enable') : t('actions.disable')}
              </button>
            </div>
          </div>
        </div>
      )}
      {/* Install confirmation modal */}
      {confirmInstall && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 backdrop-blur-sm">
          <div className="bg-surface-container-low border border-outline-variant/20 rounded-sm p-8 max-w-sm w-full mx-4 space-y-6">
            <h3 className="text-base font-semibold text-gray-100">{t('list.confirmInstall.title')}</h3>
            <p className="text-sm text-gray-400">
              {t('list.confirmInstall.bodyPrefix')} <span className="text-primary font-medium">{confirmInstall.name}</span>{t('list.confirmInstall.bodySuffix')}
              <span className="block mt-2 text-xs text-gray-400">{t('list.confirmInstall.note')}</span>
            </p>
            <div className="flex gap-3 justify-end">
              <button
                onClick={() => setConfirmInstall(null)}
                className="px-4 py-2 text-xs font-semibold rounded-sm bg-zinc-700 text-white hover:bg-zinc-600 transition-colors"
              >
                {t('actions.cancel')}
              </button>
              <button
                onClick={confirmDoInstall}
                className="px-4 py-2 text-xs font-semibold rounded-sm bg-primary-button text-primary-button-text border border-pink-500 transition-colors"
              >
                {t('actions.install')}
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
            title={t('list.title')}
            subtitle={t('list.subtitle')}
            actions={
              <div className="flex items-center gap-3">
                <button
                  onClick={handleExport}
                  className="px-4 py-2 text-xs font-semibold rounded-sm bg-gray-800 border border-gray-600 text-gray-100 hover:text-gray-100 hover:border-pink-500 transition-colors flex items-center gap-1.5"
                >
                  <span className="material-symbols-outlined text-[14px]">download</span>
                  {t('actions.exportCatalog')}
                </button>
                <button
                  onClick={() => importInputRef.current?.click()}
                  disabled={importing || !canWrite}
                  className="px-4 py-2 text-xs font-semibold rounded-sm bg-gray-800 border border-gray-600 text-gray-100 hover:text-gray-100 hover:border-pink-500 transition-colors flex items-center gap-1.5 disabled:opacity-50"
                >
                  <span className="material-symbols-outlined text-[14px]">upload</span>
                  {importing ? t('actions.importing') : t('actions.importCatalog')}
                </button>
                <button
                  onClick={() => navigate('/rules/new')}
                  disabled={!canWrite}
                  className="px-4 py-2 bg-primary-button text-primary-button-text border border-pink-500 text-xs font-semibold rounded-sm hover:scale-[0.98] transition-all"
                >
                  {t('actions.newCustomRule')}
                </button>
              </div>
            }
          />

          {loadError && (
            <div className="mb-6 px-4 py-3 bg-[#93000a]/20 border border-[#ffb4ab]/20 rounded text-sm text-error">
              {loadError}
            </div>
          )}

          {importResult && (
            <div className={`p-4 rounded-sm text-xs flex items-start gap-3 ${importResult.errors?.length ? 'bg-[#93000a]/20 border border-[#ffb4ab]/20 text-error' : 'bg-green-500/10 border border-green-700/30 text-green-400'}`}>
              <span className="material-symbols-outlined text-[14px] mt-0.5 shrink-0">
                {importResult.errors?.length ? 'error' : 'check_circle'}
              </span>
              <div className="space-y-1">
                {(importResult.added?.length > 0) && <p>{t('list.importResult.added')} <span className="font-mono">{importResult.added.join(', ')}</span></p>}
                {(importResult.updated?.length > 0) && <p>{t('list.importResult.updated')} <span className="font-mono">{importResult.updated.join(', ')}</span></p>}
                {(importResult.skipped?.length > 0) && <p className="text-gray-400">{t('list.importResult.skipped')} <span className="font-mono">{importResult.skipped.join(', ')}</span></p>}
                {(importResult.skipped_builtin?.length > 0) && <p className="text-gray-400">{t('list.importResult.skippedBuiltin')} <span className="font-mono">{importResult.skipped_builtin.join(', ')}</span></p>}
                {(importResult.skipped_custom?.length > 0) && <p className="text-gray-400">{t('list.importResult.skippedCustom')} <span className="font-mono">{importResult.skipped_custom.join(', ')}</span></p>}
                {(importResult.errors?.length > 0) && importResult.errors.map((e, i) => <p key={i}>{e}</p>)}
                {!importResult.errors?.length && !importResult.added?.length && !importResult.updated?.length
                  && !importResult.skipped_builtin?.length && !importResult.skipped_custom?.length
                  && <p>{t('list.importResult.noChanges')}</p>}
              </div>
              <button onClick={() => setImportResult(null)} className="ml-auto text-gray-400 hover:text-gray-100 shrink-0">
                <span className="material-symbols-outlined text-[14px]">close</span>
              </button>
            </div>
          )}

          <section className="space-y-4">
            {/* Search bar */}
            <div className="relative max-w-sm">
              <span className="material-symbols-outlined absolute left-3 top-1/2 -translate-y-1/2 text-gray-400 text-[16px] pointer-events-none">search</span>
              <input
                type="text"
                value={search}
                onChange={e => setSearch(e.target.value)}
                placeholder={t('list.search.placeholder')}
                className="w-full bg-gray-800 border border-gray-600 text-sm text-gray-100 placeholder-zinc-600 pl-9 pr-8 py-2 rounded-sm focus:border-gray-400 focus:outline-none"
              />
              {search && (
                <button onClick={() => setSearch('')} className="absolute right-2 top-1/2 -translate-y-1/2 text-gray-400 hover:text-gray-100">
                  <span className="material-symbols-outlined text-[14px]">close</span>
                </button>
              )}
            </div>

            {/* Rules grid */}
            <div className="grid grid-cols-1 md:grid-cols-2 xl:grid-cols-4 gap-6">
              {loading ? (
                [1,2,3,4].map(i => <div key={i} className="bg-surface-container-low p-6 rounded-sm h-64 animate-pulse" />)
              ) : pagedRules.length === 0 ? (
                <p className="text-sm text-gray-400 col-span-4 py-4">{t('list.grid.noMatch')}</p>
              ) : pagedRules.map(rule => (
                <div key={rule.id} className="group bg-gray-800 border border-gray-700 p-6 rounded-sm flex flex-col gap-6 transition-all duration-300 relative overflow-hidden">
                  {rule.active && (
                    <div className="absolute top-0 right-0 w-24 h-24 bg-[#ffb0cf]/5 blur-3xl rounded-full -translate-y-12 translate-x-12"></div>
                  )}
                  <div className="flex justify-between items-start z-10">
                    <div className={`w-12 h-12 bg-surface-container-lowest flex items-center justify-center rounded-sm ${rule.active ? 'text-pink-400' : 'text-gray-400'}`}>
                      <span className="material-symbols-outlined" style={rule.active ? { fontVariationSettings: "'FILL' 1" } : {}}>{rule.icon}</span>
                    </div>
                    <Toggle checked={rule.enabled} onChange={val => handleToggle(rule.id, val)} disabled={!canWrite} />
                  </div>
                  <div className="z-10">
                    <h3 className="text-lg font-semibold tracking-tight text-gray-100 mb-2">{rule.name}</h3>
                    <p className="text-xs text-gray-400 leading-relaxed line-clamp-3 min-h-[4.5rem]">{rule.desc}</p>
                    {rule.detections24h > 0 && (
                      <p className="text-[10px] font-mono text-pink-400 mt-2">{t('list.grid.detections', { count: rule.detections24h })}</p>
                    )}
                  </div>
                  <div className="mt-auto flex items-center justify-between z-10">
                    <Link to={`/rules/${rule.id}`} className="text-xs font-semibold text-pink-400 tracking-wider uppercase hover:underline decoration-2 underline-offset-4">
                      {t('list.grid.viewDetails')}
                    </Link>
                    <span className="text-[10px] text-gray-400 flex items-center gap-1">
                      {rule.version ? (
                        <>
                          <span className="material-symbols-outlined text-[12px] text-pink-400" style={{ fontVariationSettings: "'FILL' 1" }}>bolt</span>
                          {rule.version}
                        </>
                      ) : (
                        <>
                          <span className="material-symbols-outlined text-[12px]">pause_circle</span>
                          {t('list.grid.inactive')}
                        </>
                      )}
                    </span>
                  </div>
                </div>
              ))}
            </div>

            {/* Pagination */}
            {!loading && totalRulesPages > 1 && (
              <div className="flex items-center justify-between pt-2">
                <p className="text-xs text-gray-400">
                  {t('list.pagination.rulesCount', { count: filteredRules.length })}
                  {search ? t('list.pagination.matching', { query: search }) : ''}
                  {' · '}{t('list.pagination.pageOf', { page: rulesPage, total: totalRulesPages })}
                </p>
                <div className="flex items-center gap-1">
                  <button
                    onClick={() => setRulesPage(p => Math.max(1, p - 1))}
                    disabled={rulesPage === 1}
                    className="px-2 py-1 text-xs rounded-sm bg-surface-container-low text-gray-400 hover:text-gray-100 disabled:opacity-30 disabled:cursor-default transition-colors"
                  >
                    <span className="material-symbols-outlined text-[14px]">chevron_left</span>
                  </button>
                  {Array.from({ length: totalRulesPages }, (_, i) => i + 1).map(p => (
                    <button
                      key={p}
                      onClick={() => setRulesPage(p)}
                      className={`w-7 h-7 text-xs rounded-sm transition-colors ${
                        p === rulesPage
                          ? 'bg-[#ffb0cf]/20 text-primary font-semibold'
                          : 'bg-surface-container-low text-gray-400 hover:text-gray-100'
                      }`}
                    >
                      {p}
                    </button>
                  ))}
                  <button
                    onClick={() => setRulesPage(p => Math.min(totalRulesPages, p + 1))}
                    disabled={rulesPage === totalRulesPages}
                    className="px-2 py-1 text-xs rounded-sm bg-surface-container-low text-gray-400 hover:text-gray-100 disabled:opacity-30 disabled:cursor-default transition-colors"
                  >
                    <span className="material-symbols-outlined text-[14px]">chevron_right</span>
                  </button>
                </div>
              </div>
            )}
          </section>

          {uninstalledCatalog.length > 0 && (
            <section className="space-y-4">
              <h4 className="text-xs font-semibold text-gray-400 uppercase tracking-widest">{t('list.availableToInstall.heading')}</h4>
              <div className="grid grid-cols-1 md:grid-cols-2 xl:grid-cols-4 gap-4">
                {pagedCatalog.map(entry => (
                  <div key={entry.id} className="bg-surface-container-low/60 border border-dashed border-zinc-700/40 p-5 rounded-sm flex flex-col gap-4">
                    <div className="flex justify-between items-start">
                      <div className="w-10 h-10 bg-surface-container-lowest flex items-center justify-center rounded-sm text-gray-400">
                        <span className="material-symbols-outlined">{entry.is_custom ? 'tune' : (CATEGORY_ICONS[entry.category] || 'shield')}</span>
                      </div>
                      <div className="flex items-center gap-2">
                        {entry.is_custom && (
                          <span className="text-[10px] px-1.5 py-0.5 rounded-full bg-surface-container-high text-gray-400 border border-zinc-700/40 font-semibold uppercase tracking-wide">{t('list.grid.customBadge')}</span>
                        )}
                        <span className={`text-[10px] px-2 py-0.5 rounded-full font-semibold uppercase tracking-wide border ${
                          entry.severity === 'critical'
                            ? 'bg-[#ffb0cf]/10 text-primary border-[#ffb0cf]/20'
                            : 'bg-amber-500/10 text-amber-400 border-amber-500/20'
                        }`}>{entry.severity}</span>
                      </div>
                    </div>
                    <div>
                      <h3 className="text-sm font-semibold text-gray-400 mb-1">{entry.name}</h3>
                      <p className="text-xs text-gray-400 leading-relaxed line-clamp-2">{entry.description}</p>
                    </div>
                    <button
                      onClick={() => setConfirmInstall(entry)}
                      disabled={installing === entry.id || !canWrite}
                      className="mt-auto w-full px-3 py-2 text-xs font-semibold rounded-sm bg-primary-button text-primary-button-text border border-pink-500 disabled:opacity-50 transition-opacity"
                    >
                      {installing === entry.id ? t('actions.installing') : t('actions.install')}
                    </button>
                  </div>
                ))}
              </div>

              {totalCatalogPages > 1 && (
                <div className="flex items-center justify-between pt-2">
                  <p className="text-xs text-gray-400">
                    {t('list.pagination.available', { count: uninstalledCatalog.length })}
                    {search ? t('list.pagination.matching', { query: search }) : ''}
                    {' · '}{t('list.pagination.pageOf', { page: catalogPage, total: totalCatalogPages })}
                  </p>
                  <div className="flex items-center gap-1">
                    <button
                      onClick={() => setCatalogPage(p => Math.max(1, p - 1))}
                      disabled={catalogPage === 1}
                      className="px-2 py-1 text-xs rounded-sm bg-surface-container-low text-gray-400 hover:text-gray-100 disabled:opacity-30 disabled:cursor-default transition-colors"
                    >
                      <span className="material-symbols-outlined text-[14px]">chevron_left</span>
                    </button>
                    {Array.from({ length: totalCatalogPages }, (_, i) => i + 1).map(p => (
                      <button
                        key={p}
                        onClick={() => setCatalogPage(p)}
                        className={`w-7 h-7 text-xs rounded-sm transition-colors ${
                          p === catalogPage
                            ? 'bg-[#ffb0cf]/20 text-primary font-semibold'
                            : 'bg-surface-container-low text-gray-400 hover:text-gray-100'
                        }`}
                      >
                        {p}
                      </button>
                    ))}
                    <button
                      onClick={() => setCatalogPage(p => Math.min(totalCatalogPages, p + 1))}
                      disabled={catalogPage === totalCatalogPages}
                      className="px-2 py-1 text-xs rounded-sm bg-surface-container-low text-gray-400 hover:text-gray-100 disabled:opacity-30 disabled:cursor-default transition-colors"
                    >
                      <span className="material-symbols-outlined text-[14px]">chevron_right</span>
                    </button>
                  </div>
                </div>
              )}
            </section>
          )}
        </div>
      </main>
    </div>
  )
}
