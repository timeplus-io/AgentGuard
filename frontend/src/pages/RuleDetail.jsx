import { useState, useEffect } from 'react'
import { useParams, useNavigate, Link } from 'react-router-dom'
import { highlight, languages } from 'prismjs/components/prism-core'
import 'prismjs/components/prism-sql'
import 'prismjs/themes/prism-tomorrow.css'
import ThreatsList from '../components/ThreatsList'
import Sidebar from '../components/Sidebar'
import TopNav from '../components/TopNav'
import BlockPolicySelect from '../components/BlockPolicySelect'
import ConfigurationTab from '../components/rules/ConfigurationTab'
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

function severityBadge(severity) {
  if (severity === 'critical') return 'bg-[#ffb0cf]/10 text-[#ffb0cf] border border-[#ffb0cf]/20'
  if (severity === 'warning') return 'bg-amber-500/10 text-amber-400 border border-amber-500/20'
  return 'bg-zinc-700/40 text-gray-400 border border-zinc-600/20'
}

export default function RuleDetail() {
  const { id } = useParams()
  const navigate = useNavigate()
  // `rule` is the GET /rules/:id read model: { instance, schema, defaults,
  // effective, overrides, rendered_sql, upgrade, mv, definition, detections_24h }.
  const [rule, setRule] = useState(null)
  const [loadError, setLoadError] = useState(null)
  const [detections24h, setDetections24h] = useState(0)
  const [loading, setLoading] = useState(true)
  const [copied, setCopied] = useState(false)
  const [copiedUDFs, setCopiedUDFs] = useState({})
  const [uninstallConfirm, setUninstallConfirm] = useState(false)
  const [toggleConfirm, setToggleConfirm] = useState(false)
  const [upgradeModal, setUpgradeModal] = useState(false)
  const [upgradeConflicts, setUpgradeConflicts] = useState(null) // { conflicts, warnings } from a blocked upgrade
  const [actionLoading, setActionLoading] = useState(false)
  const [actionError, setActionError] = useState(null)
  // Content tabs (left → right): configuration | description | sql | udfs | detections
  const [contentTab, setContentTab] = useState('configuration')

  const reload = async () => {
    const data = await api.getRule(id)
    setRule(data)
    setDetections24h(data.detections_24h || 0)
    return data
  }

  useEffect(() => {
    api.getRule(id)
      .then(data => {
        setRule(data)
        setDetections24h(data.detections_24h || 0)
      })
      .catch(err => setLoadError(err?.message || 'Failed to load rule'))
      .finally(() => setLoading(false))
  }, [id])

  const inst = rule?.instance || {}
  const upgradeAvailable = !!rule?.upgrade
  const targetVersion = rule?.definition?.version

  const handleToggle = () => {
    if (!rule || actionLoading) return
    setToggleConfirm(true)
  }

  const confirmToggle = async () => {
    setToggleConfirm(false)
    setActionLoading(true)
    setActionError(null)
    try {
      await api.toggleRule(id)
      await reload()
    } catch (e) {
      setActionError(e?.message || 'Toggle failed')
    } finally {
      setActionLoading(false)
    }
  }

  const confirmUpgrade = async () => {
    setActionLoading(true)
    setActionError(null)
    setUpgradeConflicts(null)
    try {
      await api.upgradeRule(id)
      setUpgradeModal(false)
      await reload()
    } catch (e) {
      // 409 → incompatible overrides; surface the structured conflicts/warnings.
      if (e?.status === 409 && e?.body) {
        setUpgradeConflicts({ conflicts: e.body.conflicts || [], warnings: e.body.warnings || [] })
      } else {
        setActionError(e?.message || 'Upgrade failed')
        setUpgradeModal(false)
      }
    } finally {
      setActionLoading(false)
    }
  }

  const handleUninstall = async () => {
    if (!rule || actionLoading) return
    setActionLoading(true)
    setActionError(null)
    try {
      await api.uninstallRule(id)
      navigate('/rules')
    } catch (e) {
      setActionLoading(false)
      setUninstallConfirm(false)
      setActionError(e?.message || 'Uninstall failed')
    }
  }

  const copySQL = () => {
    if (!inst.rendered_sql) return
    navigator.clipboard.writeText(inst.rendered_sql)
    setCopied(true)
    setTimeout(() => setCopied(false), 2000)
  }

  const copyUDF = (i, text) => {
    navigator.clipboard.writeText(text)
    setCopiedUDFs(prev => ({ ...prev, [i]: true }))
    setTimeout(() => setCopiedUDFs(prev => ({ ...prev, [i]: false })), 2000)
  }

  const udfFuncName = udf => {
    const m = udf.match(/FUNCTION\s+(\w+)\s*\(/i)
    return m ? m[1] : `UDF`
  }

  if (loading) {
    return (
      <div className="min-h-screen bg-[#15121d] text-gray-100">
        <Sidebar /><TopNav />
        <main className="pl-sidebar pt-16 pb-8 min-h-screen">
          <div className="p-6 lg:p-10 space-y-6">
            {[1,2,3].map(i => <div key={i} className="bg-[#1d1a25] rounded-sm h-32 animate-pulse" />)}
          </div>
        </main>
      </div>
    )
  }

  if (!rule) {
    return (
      <div className="min-h-screen bg-[#15121d] text-gray-100">
        <Sidebar /><TopNav />
        <main className="pl-sidebar pt-16 pb-8 min-h-screen flex items-center justify-center">
          {loadError ? (
            <div className="flex flex-col items-center gap-3">
              <span className="material-symbols-outlined text-[#ffb4ab] text-4xl">error</span>
              <p className="text-sm text-[#ffb4ab]">{loadError}</p>
            </div>
          ) : (
            <p className="text-gray-400">Rule not found.</p>
          )}
        </main>
      </div>
    )
  }

  const icon = CATEGORY_ICONS[inst.category] || 'shield'
  const udfs = inst.deployed_udfs || []

  return (
    <div className="min-h-screen bg-[#15121d] text-gray-100">
      <Sidebar /><TopNav />

      {/* Toggle confirmation modal */}
      {toggleConfirm && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 backdrop-blur-sm">
          <div className="bg-[#1d1a25] border border-[#574149]/20 rounded-sm p-8 max-w-sm w-full mx-4 space-y-6">
            <h3 className="text-base font-semibold text-white">
              {inst.enabled ? 'Disable' : 'Enable'} Rule
            </h3>
            <p className="text-sm text-gray-400">
              Are you sure you want to <span className="text-white font-medium">{inst.enabled ? 'disable' : 'enable'}</span> <span className="text-[#ffb0cf]">{inst.name}</span>?
              {inst.enabled
                ? <span className="block mt-2 text-xs text-gray-400">This drops the detection materialized view; re-enabling redeploys it from the current configuration.</span>
                : <span className="block mt-2 text-xs text-gray-400">This deploys the detection materialized view and starts monitoring.</span>
              }
            </p>
            <div className="flex gap-3 justify-end">
              <button onClick={() => setToggleConfirm(false)} className="px-4 py-2 text-xs font-semibold rounded-sm bg-zinc-700 text-white hover:bg-zinc-600 transition-colors">Cancel</button>
              <button
                onClick={confirmToggle}
                className={`px-4 py-2 text-xs font-semibold rounded-sm transition-colors ${!inst.enabled ? 'bg-primary-button text-primary-button-text' : 'bg-zinc-700 text-white hover:bg-zinc-600'}`}
              >
                {inst.enabled ? 'Disable' : 'Enable'}
              </button>
            </div>
          </div>
        </div>
      )}

      {/* Upgrade modal: shows the upgrade plan (warnings/conflicts) before confirming. */}
      {upgradeModal && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 backdrop-blur-sm">
          <div className="bg-[#1d1a25] border border-[#574149]/20 rounded-sm p-8 max-w-lg w-full mx-4 space-y-5">
            <h3 className="text-base font-semibold text-white">
              Upgrade {inst.name} → {targetVersion}
            </h3>

            {(() => {
              const plan = upgradeConflicts || rule.upgrade || {}
              const conflicts = plan.conflicts || []
              const warnings = plan.warnings || []
              const blocked = conflicts.length > 0
              return (
                <>
                  {conflicts.length > 0 && (
                    <div className="space-y-2">
                      <p className="text-xs font-semibold text-[#ffb4ab] uppercase tracking-widest">Conflicts (block upgrade)</p>
                      {conflicts.map((c, i) => (
                        <div key={i} className="text-xs bg-red-500/10 border border-red-500/20 rounded-sm px-3 py-2">
                          <code className="text-[#ffb4ab]">{c.parameter}</code>: <span className="text-gray-300">{c.message}</span>
                        </div>
                      ))}
                    </div>
                  )}
                  {warnings.length > 0 && (
                    <div className="space-y-2">
                      <p className="text-xs font-semibold text-amber-400 uppercase tracking-widest">Warnings</p>
                      {warnings.map((w, i) => (
                        <div key={i} className="text-xs bg-amber-500/10 border border-amber-500/20 rounded-sm px-3 py-2">
                          <code className="text-amber-300">{w.parameter}</code>: <span className="text-gray-300">{w.message}</span>
                        </div>
                      ))}
                    </div>
                  )}
                  {conflicts.length === 0 && warnings.length === 0 && (
                    <p className="text-sm text-gray-400">Compatible upgrade — all current overrides are preserved.</p>
                  )}
                  <div className="flex gap-3 justify-end pt-2">
                    <button onClick={() => { setUpgradeModal(false); setUpgradeConflicts(null) }} className="px-4 py-2 text-xs font-semibold rounded-sm bg-zinc-700 text-white hover:bg-zinc-600 transition-colors">
                      {blocked ? 'Close' : 'Cancel'}
                    </button>
                    {!blocked && (
                      <button onClick={confirmUpgrade} disabled={actionLoading} className="px-4 py-2 text-xs font-semibold rounded-sm bg-primary-button text-primary-button-text transition-colors disabled:opacity-50">
                        {actionLoading ? 'Upgrading…' : `Upgrade to ${targetVersion}`}
                      </button>
                    )}
                  </div>
                </>
              )
            })()}
          </div>
        </div>
      )}

      <main className="pl-sidebar pt-16 pb-8 min-h-screen">
        <div className="p-6 lg:p-10 space-y-8">

          <Link to="/rules" className="inline-flex items-center gap-1 text-xs text-gray-400 hover:text-white transition-colors uppercase tracking-widest font-semibold">
            <span className="material-symbols-outlined text-[14px]">arrow_back</span>
            Rule Packs
          </Link>

          {/* Header row */}
          <div className="flex items-start justify-between gap-6 flex-wrap">
            <div className="flex items-center gap-4">
              <div className={`w-14 h-14 bg-[#0f0c17] flex items-center justify-center rounded-sm ${inst.enabled ? 'text-[#ffb0cf]' : 'text-gray-400'}`}>
                <span className="material-symbols-outlined text-[28px]" style={inst.enabled ? { fontVariationSettings: "'FILL' 1" } : {}}>{icon}</span>
              </div>
              <div>
                <h1 className="text-2xl font-bold text-white tracking-tight">{inst.name}</h1>
                <div className="flex items-center gap-3 mt-1">
                  <span className="text-xs text-gray-400">
                    <span className="material-symbols-outlined text-[12px] text-[#ffb0cf] align-middle" style={{ fontVariationSettings: "'FILL' 1" }}>bolt</span>
                    {' '}{inst.definition_version}
                  </span>
                  <span className={`text-[10px] px-2 py-0.5 rounded-full font-semibold uppercase tracking-wide ${severityBadge(inst.severity)}`}>
                    {inst.severity}
                  </span>
                  {rule.mv && inst.enabled && rule.mv.failed_state && (
                    <span className="text-[10px] px-2 py-0.5 rounded-full font-semibold uppercase tracking-wide bg-red-500/10 text-red-400 border border-red-500/20" title={rule.mv.failed_state}>
                      MV {rule.mv.failed_state}
                    </span>
                  )}
                </div>
              </div>
            </div>

            <div className="flex items-center gap-3 flex-wrap">
              <button
                onClick={handleToggle}
                disabled={actionLoading}
                className={`px-4 py-2 text-xs font-semibold rounded-sm transition-colors disabled:opacity-50 ${inst.enabled ? 'bg-zinc-700 text-white hover:bg-zinc-600' : 'bg-primary-button text-primary-button-text'}`}
              >
                {inst.enabled ? 'Disable' : 'Enable'}
              </button>

              {upgradeAvailable && (
                <button
                  onClick={() => { setUpgradeConflicts(null); setUpgradeModal(true) }}
                  disabled={actionLoading}
                  className="px-4 py-2 text-xs font-semibold rounded-sm border border-[#ffb0cf]/30 text-[#ffb0cf] hover:bg-[#ffb0cf]/10 transition-colors disabled:opacity-50"
                >
                  Upgrade{targetVersion ? ` to ${targetVersion}` : ''}
                  {rule.upgrade?.conflicts?.length > 0 && (
                    <span className="ml-1.5 text-[9px] align-middle">⚠</span>
                  )}
                </button>
              )}

              {!uninstallConfirm ? (
                <button onClick={() => setUninstallConfirm(true)} disabled={actionLoading} className="px-4 py-2 text-xs font-semibold rounded-sm border border-red-500/30 text-red-400 hover:bg-red-500/10 transition-colors disabled:opacity-50">
                  Uninstall
                </button>
              ) : (
                <div className="flex items-center gap-2">
                  <span className="text-xs text-red-400">Are you sure?</span>
                  <button onClick={handleUninstall} disabled={actionLoading} className="px-3 py-1.5 text-xs font-semibold rounded-sm bg-red-500/20 text-red-400 hover:bg-red-500/30 transition-colors">Confirm</button>
                  <button onClick={() => setUninstallConfirm(false)} className="px-3 py-1.5 text-xs font-semibold rounded-sm bg-zinc-700 text-white hover:bg-zinc-600 transition-colors">Cancel</button>
                </div>
              )}
            </div>
          </div>

          {actionError && (
            <div className="bg-red-500/10 border border-red-500/20 rounded-sm px-4 py-3 flex items-start gap-3">
              <span className="material-symbols-outlined text-red-400 text-[18px] shrink-0 mt-0.5">error</span>
              <p className="text-xs text-red-400 font-mono break-all">{actionError}</p>
              <button onClick={() => setActionError(null)} className="ml-auto text-gray-400 hover:text-white shrink-0">
                <span className="material-symbols-outlined text-[16px]">close</span>
              </button>
            </div>
          )}

          {/* Block Policy */}
          <div className="bg-[#1d1a25] rounded-sm p-4">
            <div className="flex items-center justify-between mb-2">
              <span className="text-[11px] font-semibold uppercase tracking-widest text-gray-400">Block Policy</span>
              <BlockPolicySelect
                value={inst.block_policy}
                onChange={async (newPolicy) => {
                  const prevPolicy = inst.block_policy
                  setRule(prev => prev ? { ...prev, instance: { ...prev.instance, block_policy: newPolicy } } : prev)
                  try {
                    await api.setRuleBlockPolicy(inst.rule_id || id, newPolicy)
                  } catch (e) {
                    setRule(prev => prev ? { ...prev, instance: { ...prev.instance, block_policy: prevPolicy } } : prev)
                    setActionError(e?.message || 'Failed to update policy')
                  }
                }}
              />
            </div>
            <p className="text-[11px] text-gray-400">
              {inst.block_policy === 'hold' && 'When this rule fires, AgentGuard will pause the agent and wait for human approval.'}
              {inst.block_policy === 'auto_block' && 'When this rule fires, AgentGuard will block the tool call immediately without prompting.'}
              {(!inst.block_policy || inst.block_policy === 'log_only') && 'When this rule fires, the threat is logged and surfaced in the UI but the agent is not blocked.'}
            </p>
          </div>

          {/* Rule Definition metadata */}
          <div className="bg-[#1d1a25] p-8 rounded-sm">
            <h2 className="text-sm font-semibold text-gray-400 uppercase tracking-widest mb-6">Rule Definition</h2>
            <div className="grid grid-cols-2 md:grid-cols-3 gap-6">
              <div>
                <p className="text-[10px] text-gray-400 uppercase tracking-widest mb-1">Rule ID</p>
                <p className="text-sm font-mono text-white">{inst.rule_id}</p>
              </div>
              <div>
                <p className="text-[10px] text-gray-400 uppercase tracking-widest mb-1">Version</p>
                <p className="text-sm text-white">{inst.definition_version}</p>
              </div>
              <div>
                <p className="text-[10px] text-gray-400 uppercase tracking-widest mb-1">Category</p>
                <p className="text-sm text-white capitalize">{inst.category?.replace(/_/g, ' ')}</p>
              </div>
              <div>
                <p className="text-[10px] text-gray-400 uppercase tracking-widest mb-1">Severity</p>
                <p className={`text-sm font-semibold capitalize ${inst.severity === 'critical' ? 'text-[#ffb0cf]' : 'text-amber-400'}`}>{inst.severity}</p>
              </div>
              <div>
                <p className="text-[10px] text-gray-400 uppercase tracking-widest mb-1">False Positive Rate</p>
                <p className="text-sm text-white capitalize">{inst.fp_rate}</p>
              </div>
              <div>
                <p className="text-[10px] text-gray-400 uppercase tracking-widest mb-1">Hook Sources</p>
                <p className="text-xs font-mono text-gray-100">{inst.hook_sources}</p>
              </div>
              {inst.required_telemetry && (
                <div className="col-span-2 md:col-span-3">
                  <p className="text-[10px] text-gray-400 uppercase tracking-widest mb-1">Required Telemetry</p>
                  <span className="text-xs bg-amber-500/10 text-amber-400 border border-amber-500/20 px-2 py-0.5 rounded-full">{inst.required_telemetry}</span>
                </div>
              )}
            </div>
          </div>

          {/* Tabbed content: Configuration / Description / Detection SQL / UDFs / Recent Detections */}
          <div className="bg-[#1d1a25] rounded-sm overflow-hidden">
            <div className="flex border-b border-[#574149]/10 px-2 flex-wrap">
              {[
                { id: 'configuration', label: 'Configuration', badge: rule.schema?.length || 0 },
                { id: 'description',   label: 'Description' },
                { id: 'sql',           label: 'Detection SQL' },
                { id: 'udfs',          label: 'UDFs', badge: udfs.length },
                { id: 'detections',    label: 'Recent Detections', badge: detections24h, badgeLabel: '24h' },
              ].map(t => {
                const active = contentTab === t.id
                return (
                  <button
                    key={t.id}
                    onClick={() => setContentTab(t.id)}
                    className={`px-4 py-3 text-xs font-semibold uppercase tracking-widest transition-colors border-b-2 ${active ? 'text-[#ffb0cf] border-[#ffb0cf]' : 'text-gray-400 hover:text-gray-100 border-transparent'}`}
                  >
                    {t.label}
                    {t.badge != null && t.badge > 0 && (
                      <span className={`ml-2 text-[10px] normal-case font-normal ${active ? 'text-[#ffb0cf]/70' : 'text-gray-400'}`}>
                        {t.badgeLabel ? `(${t.badgeLabel}: ${t.badge})` : `(${t.badge})`}
                      </span>
                    )}
                  </button>
                )
              })}
            </div>

            <div className="p-8">
              {contentTab === 'configuration' && (
                <ConfigurationTab
                  ruleId={id}
                  schema={rule.schema}
                  defaults={rule.defaults}
                  effective={rule.effective}
                  onSaved={() => reload()}
                />
              )}

              {contentTab === 'description' && (
                <p className="text-sm text-gray-100 whitespace-pre-wrap leading-relaxed">{inst.description}</p>
              )}

              {contentTab === 'sql' && (
                <>
                  <div className="flex items-center justify-between mb-3">
                    <span className="text-[11px] text-gray-400">Read-only — rendered from the definition template and current parameter values.</span>
                    <button onClick={copySQL} className="flex items-center gap-1.5 text-xs text-gray-400 hover:text-white transition-colors">
                      <span className="material-symbols-outlined text-[14px]">{copied ? 'check' : 'content_copy'}</span>
                      {copied ? 'Copied' : 'Copy'}
                    </button>
                  </div>
                  <pre className="bg-[#0f0c17] text-[11px] font-mono rounded p-4 overflow-x-auto whitespace-pre-wrap break-all leading-relaxed"
                    dangerouslySetInnerHTML={{ __html: highlight(inst.rendered_sql || '', languages.sql, 'sql') }}
                  />
                </>
              )}

              {contentTab === 'udfs' && (
                udfs.length > 0 ? (
                  <div className="space-y-6">
                    {udfs.map((udf, i) => (
                      <div key={i}>
                        <div className="flex items-center justify-between mb-2">
                          <h3 className="text-sm font-semibold text-gray-400">
                            {udfFuncName(udf)}
                            <span className="ml-2 text-[10px] text-gray-400 normal-case font-normal">UDF</span>
                          </h3>
                          <button onClick={() => copyUDF(i, udf)} className="flex items-center gap-1.5 text-xs text-gray-400 hover:text-white transition-colors">
                            <span className="material-symbols-outlined text-[14px]">{copiedUDFs[i] ? 'check' : 'content_copy'}</span>
                            {copiedUDFs[i] ? 'Copied' : 'Copy'}
                          </button>
                        </div>
                        <pre className="bg-[#0f0c17] text-[11px] font-mono rounded p-4 overflow-x-auto whitespace-pre-wrap break-all leading-relaxed"
                          dangerouslySetInnerHTML={{ __html: highlight(udf || '', languages.sql, 'sql') }}
                        />
                      </div>
                    ))}
                  </div>
                ) : (
                  <p className="text-xs text-gray-400">This rule does not define any UDFs.</p>
                )
              )}

              {contentTab === 'detections' && (
                <ThreatsList ruleId={id} hideRuleCol showFilters={false} />
              )}
            </div>
          </div>

        </div>
      </main>
    </div>
  )
}
