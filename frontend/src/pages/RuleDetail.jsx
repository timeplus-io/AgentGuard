import { useState, useEffect } from 'react'
import { useParams, useNavigate, Link } from 'react-router-dom'
import ThreatsList from '../components/ThreatsList'
import { highlight, languages } from 'prismjs/components/prism-core'
import 'prismjs/components/prism-sql'
import 'prismjs/themes/prism-tomorrow.css'
import Sidebar from '../components/Sidebar'
import TopNav from '../components/TopNav'
import Footer from '../components/Footer'
import { api } from '../api/client'

const RULE_ICONS = {
  'rp-001': 'security_update_good',
  'rp-002': 'data_loss_prevention',
  'rp-003': 'admin_panel_settings',
  'rp-004': 'account_tree',
}

function severityBadge(severity) {
  if (severity === 'critical') return 'bg-[#ffb0cf]/10 text-[#ffb0cf] border border-[#ffb0cf]/20'
  if (severity === 'warning') return 'bg-amber-500/10 text-amber-400 border border-amber-500/20'
  return 'bg-zinc-700/40 text-zinc-400 border border-zinc-600/20'
}

export default function RuleDetail() {
  const { id } = useParams()
  const navigate = useNavigate()
  const [rule, setRule] = useState(null)
  const [detections24h, setDetections24h] = useState(0)
  const [loading, setLoading] = useState(true)
  const [copied, setCopied] = useState(false)
  const [uninstallConfirm, setUninstallConfirm] = useState(false)
  const [toggleConfirm, setToggleConfirm] = useState(false)
  const [actionLoading, setActionLoading] = useState(false)

  useEffect(() => {
    api.getRule(id)
      .then(data => {
        setRule(data)
        setDetections24h(data.detections_24h || 0)
      })
      .catch(() => {})
      .finally(() => setLoading(false))
  }, [id])

  const handleToggle = () => {
    if (!rule || actionLoading) return
    setToggleConfirm(true)
  }

  const confirmToggle = async () => {
    setToggleConfirm(false)
    setActionLoading(true)
    try {
      await api.toggleRule(id)
      // Refresh from API to get authoritative state
      const data = await api.getRule(id)
      setRule(data)
      setDetections24h(data.detections_24h || 0)
    } catch (e) {
      console.error('Toggle failed', e)
    } finally {
      setActionLoading(false)
    }
  }

  const handleUpgrade = async () => {
    if (!rule || actionLoading) return
    setActionLoading(true)
    try {
      await api.upgradeRule(id, rule.query_sql)
      setRule(prev => ({ ...prev, version: prev.latest_version, latest_version: undefined }))
    } catch (e) {
      console.error('Upgrade failed', e)
    } finally {
      setActionLoading(false)
    }
  }

  const handleUninstall = async () => {
    if (!rule || actionLoading) return
    setActionLoading(true)
    try {
      await api.uninstallRule(id)
      navigate('/rules')
    } catch (e) {
      console.error('Uninstall failed', e)
      setActionLoading(false)
      setUninstallConfirm(false)
    }
  }

  const copySQL = () => {
    if (!rule?.query_sql) return
    navigator.clipboard.writeText(rule.query_sql)
    setCopied(true)
    setTimeout(() => setCopied(false), 2000)
  }

  if (loading) {
    return (
      <div className="min-h-screen bg-[#15121d] text-[#e7dff0]">
        <Sidebar /><TopNav /><Footer />
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
      <div className="min-h-screen bg-[#15121d] text-[#e7dff0]">
        <Sidebar /><TopNav /><Footer />
        <main className="pl-sidebar pt-16 pb-8 min-h-screen flex items-center justify-center">
          <p className="text-zinc-500">Rule not found.</p>
        </main>
      </div>
    )
  }

  const icon = RULE_ICONS[id] || 'shield'

  return (
    <div className="min-h-screen bg-[#15121d] text-[#e7dff0]">
      <Sidebar /><TopNav /><Footer />

      {/* Toggle confirmation modal */}
      {toggleConfirm && rule && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 backdrop-blur-sm">
          <div className="bg-[#1d1a25] border border-[#574149]/20 rounded-sm p-8 max-w-sm w-full mx-4 space-y-6">
            <h3 className="text-base font-semibold text-white">
              {rule.enabled ? 'Disable' : 'Enable'} Rule Pack
            </h3>
            <p className="text-sm text-zinc-400">
              Are you sure you want to <span className="text-white font-medium">{rule.enabled ? 'disable' : 'enable'}</span> <span className="text-[#ffb0cf]">{rule.name}</span>?
              {rule.enabled
                ? <span className="block mt-2 text-xs text-zinc-500">This will pause the detection MV and stop new events from being detected.</span>
                : <span className="block mt-2 text-xs text-zinc-500">This will create or resume the detection MV and start monitoring.</span>
              }
            </p>
            <div className="flex gap-3 justify-end">
              <button
                onClick={() => setToggleConfirm(false)}
                className="px-4 py-2 text-xs font-semibold rounded-sm bg-zinc-700 text-white hover:bg-zinc-600 transition-colors"
              >
                Cancel
              </button>
              <button
                onClick={confirmToggle}
                className={`px-4 py-2 text-xs font-semibold rounded-sm transition-colors ${
                  !rule.enabled
                    ? 'bg-gradient-to-br from-[#ffb0cf] to-[#f256a3] text-[#3d0023]'
                    : 'bg-zinc-700 text-white hover:bg-zinc-600'
                }`}
              >
                {rule.enabled ? 'Disable' : 'Enable'}
              </button>
            </div>
          </div>
        </div>
      )}

      <main className="pl-sidebar pt-16 pb-8 min-h-screen">
        <div className="p-6 lg:p-10 space-y-8">

          {/* Back link */}
          <Link to="/rules" className="inline-flex items-center gap-1 text-xs text-zinc-500 hover:text-white transition-colors uppercase tracking-widest font-semibold">
            <span className="material-symbols-outlined text-[14px]">arrow_back</span>
            Rule Packs
          </Link>

          {/* Header row */}
          <div className="flex items-start justify-between gap-6 flex-wrap">
            <div className="flex items-center gap-4">
              <div className={`w-14 h-14 bg-[#0f0c17] flex items-center justify-center rounded-sm ${rule.enabled ? 'text-[#ffb0cf]' : 'text-zinc-500'}`}>
                <span className="material-symbols-outlined text-[28px]" style={rule.enabled ? { fontVariationSettings: "'FILL' 1" } : {}}>{icon}</span>
              </div>
              <div>
                <h1 className="text-2xl font-bold text-white tracking-tight">{rule.name}</h1>
                <div className="flex items-center gap-3 mt-1">
                  <span className="text-xs text-zinc-500">
                    <span className="material-symbols-outlined text-[12px] text-[#ffb0cf] align-middle" style={{ fontVariationSettings: "'FILL' 1" }}>bolt</span>
                    {' '}{rule.version}
                  </span>
                  <span className={`text-[10px] px-2 py-0.5 rounded-full font-semibold uppercase tracking-wide ${severityBadge(rule.severity)}`}>
                    {rule.severity}
                  </span>
                </div>
              </div>
            </div>

            {/* Action buttons */}
            <div className="flex items-center gap-3 flex-wrap">
              <button
                onClick={handleToggle}
                disabled={actionLoading}
                className={`px-4 py-2 text-xs font-semibold rounded-sm transition-colors disabled:opacity-50 ${
                  rule.enabled
                    ? 'bg-zinc-700 text-white hover:bg-zinc-600'
                    : 'bg-gradient-to-br from-[#ffb0cf] to-[#f256a3] text-[#3d0023]'
                }`}
              >
                {rule.enabled ? 'Disable' : 'Enable'}
              </button>

              {rule.latest_version && (
                <button
                  onClick={handleUpgrade}
                  disabled={actionLoading}
                  className="px-4 py-2 text-xs font-semibold rounded-sm border border-[#ffb0cf]/30 text-[#ffb0cf] hover:bg-[#ffb0cf]/10 transition-colors disabled:opacity-50"
                >
                  Upgrade to {rule.latest_version}
                </button>
              )}

              {!uninstallConfirm ? (
                <button
                  onClick={() => setUninstallConfirm(true)}
                  disabled={actionLoading}
                  className="px-4 py-2 text-xs font-semibold rounded-sm border border-red-500/30 text-red-400 hover:bg-red-500/10 transition-colors disabled:opacity-50"
                >
                  Uninstall
                </button>
              ) : (
                <div className="flex items-center gap-2">
                  <span className="text-xs text-red-400">Are you sure?</span>
                  <button
                    onClick={handleUninstall}
                    disabled={actionLoading}
                    className="px-3 py-1.5 text-xs font-semibold rounded-sm bg-red-500/20 text-red-400 hover:bg-red-500/30 transition-colors"
                  >
                    Confirm
                  </button>
                  <button
                    onClick={() => setUninstallConfirm(false)}
                    className="px-3 py-1.5 text-xs font-semibold rounded-sm bg-zinc-700 text-white hover:bg-zinc-600 transition-colors"
                  >
                    Cancel
                  </button>
                </div>
              )}
            </div>
          </div>

          {/* Rule Definition */}
          <div className="bg-[#1d1a25] p-8 rounded-sm">
            <h2 className="text-sm font-semibold text-zinc-400 uppercase tracking-widest mb-6">Rule Definition</h2>
            <div className="grid grid-cols-2 md:grid-cols-3 gap-6">
              <div>
                <p className="text-[10px] text-zinc-500 uppercase tracking-widest mb-1">Rule ID</p>
                <p className="text-sm font-mono text-white">{rule.id}</p>
              </div>
              <div>
                <p className="text-[10px] text-zinc-500 uppercase tracking-widest mb-1">Version</p>
                <p className="text-sm text-white">{rule.version}</p>
              </div>
              <div>
                <p className="text-[10px] text-zinc-500 uppercase tracking-widest mb-1">Category</p>
                <p className="text-sm text-white capitalize">{rule.category?.replace(/_/g, ' ')}</p>
              </div>
              <div>
                <p className="text-[10px] text-zinc-500 uppercase tracking-widest mb-1">Severity</p>
                <p className={`text-sm font-semibold capitalize ${rule.severity === 'critical' ? 'text-[#ffb0cf]' : 'text-amber-400'}`}>
                  {rule.severity}
                </p>
              </div>
              <div>
                <p className="text-[10px] text-zinc-500 uppercase tracking-widest mb-1">False Positive Rate</p>
                <p className="text-sm text-white capitalize">{rule.fp_rate}</p>
              </div>
              <div>
                <p className="text-[10px] text-zinc-500 uppercase tracking-widest mb-1">Hook Sources</p>
                <p className="text-xs font-mono text-zinc-300">{rule.hook_sources}</p>
              </div>
              {rule.required_telemetry && (
                <div className="col-span-2 md:col-span-3">
                  <p className="text-[10px] text-zinc-500 uppercase tracking-widest mb-1">Required Telemetry</p>
                  <span className="text-xs bg-amber-500/10 text-amber-400 border border-amber-500/20 px-2 py-0.5 rounded-full">
                    {rule.required_telemetry}
                  </span>
                </div>
              )}
              <div className="col-span-2 md:col-span-3">
                <p className="text-[10px] text-zinc-500 uppercase tracking-widest mb-1">Description</p>
                <p className="text-sm text-zinc-300">{rule.description}</p>
              </div>
            </div>
          </div>

          {/* Detection SQL */}
          <div className="bg-[#1d1a25] p-8 rounded-sm">
            <div className="flex items-center justify-between mb-4">
              <h2 className="text-sm font-semibold text-zinc-400 uppercase tracking-widest">Detection SQL</h2>
              <button
                onClick={copySQL}
                className="flex items-center gap-1.5 text-xs text-zinc-400 hover:text-white transition-colors"
              >
                <span className="material-symbols-outlined text-[14px]">{copied ? 'check' : 'content_copy'}</span>
                {copied ? 'Copied' : 'Copy'}
              </button>
            </div>
            <div className="bg-[#0f0c17] rounded-sm overflow-x-auto">
              <pre
                className="text-xs font-mono leading-relaxed p-6 m-0"
                dangerouslySetInnerHTML={{
                  __html: rule.query_sql
                    ? highlight(rule.query_sql, languages.sql, 'sql')
                    : ''
                }}
              />
            </div>
          </div>

          {/* Recent Detections */}
          <div>
            <h2 className="text-sm font-semibold text-zinc-400 uppercase tracking-widest mb-4">
              Recent Detections
              <span className="ml-2 text-[10px] text-zinc-500 normal-case">(24h: {detections24h})</span>
            </h2>
            <ThreatsList ruleId={id} hideRuleCol showFilters={false} />
          </div>

        </div>
      </main>
    </div>
  )
}
