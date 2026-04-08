import { useState, useEffect } from 'react'
import { useParams, useNavigate, Link } from 'react-router-dom'
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

function formatTs(ts) {
  if (!ts) return '—'
  try { return new Date(ts.replace(' ', 'T')).toLocaleString() } catch { return ts }
}

export default function RuleDetail() {
  const { id } = useParams()
  const navigate = useNavigate()
  const [rule, setRule] = useState(null)
  const [recentDetections, setRecentDetections] = useState([])
  const [detections24h, setDetections24h] = useState(0)
  const [loading, setLoading] = useState(true)
  const [copied, setCopied] = useState(false)
  const [uninstallConfirm, setUninstallConfirm] = useState(false)
  const [actionLoading, setActionLoading] = useState(false)

  useEffect(() => {
    api.getRule(id)
      .then(data => {
        setRule(data)
        setRecentDetections(data.recent_detections || [])
        setDetections24h(data.detections_24h || 0)
      })
      .catch(() => {})
      .finally(() => setLoading(false))
  }, [id])

  const handleToggle = async () => {
    if (!rule || actionLoading) return
    setActionLoading(true)
    try {
      await api.toggleRule(id)
      setRule(prev => ({ ...prev, enabled: !prev.enabled }))
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
        <main className="pl-64 pt-16 pb-8 min-h-screen">
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
        <main className="pl-64 pt-16 pb-8 min-h-screen flex items-center justify-center">
          <p className="text-zinc-500">Rule not found.</p>
        </main>
      </div>
    )
  }

  const icon = RULE_ICONS[id] || 'shield'

  return (
    <div className="min-h-screen bg-[#15121d] text-[#e7dff0]">
      <Sidebar /><TopNav /><Footer />
      <main className="pl-64 pt-16 pb-8 min-h-screen">
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
            <pre className="text-xs font-mono text-zinc-300 bg-[#0f0c17] p-6 rounded-sm overflow-x-auto whitespace-pre leading-relaxed">
              {rule.query_sql}
            </pre>
          </div>

          {/* Recent Detections */}
          <div className="bg-[#1d1a25] p-8 rounded-sm">
            <div className="flex items-center justify-between mb-6">
              <h2 className="text-sm font-semibold text-zinc-400 uppercase tracking-widest">
                Recent Detections
                <span className="ml-2 text-[10px] text-zinc-500 normal-case">(24h: {detections24h})</span>
              </h2>
            </div>
            {recentDetections.length === 0 ? (
              <p className="text-xs text-zinc-500">No detections for this rule yet.</p>
            ) : (
              <div className="overflow-x-auto">
                <table className="w-full text-xs">
                  <thead>
                    <tr className="text-[10px] text-zinc-500 uppercase tracking-widest border-b border-zinc-800">
                      <th className="text-left pb-3 pr-4 font-medium">Time</th>
                      <th className="text-left pb-3 pr-4 font-medium">Agent</th>
                      <th className="text-left pb-3 pr-4 font-medium">Hook</th>
                      <th className="text-left pb-3 pr-4 font-medium">Message</th>
                      <th className="text-left pb-3 font-medium">Severity</th>
                    </tr>
                  </thead>
                  <tbody className="divide-y divide-zinc-800/50">
                    {recentDetections.map((evt, i) => (
                      <tr key={i} className="hover:bg-[#2c2834]/50">
                        <td className="py-3 pr-4 text-zinc-400 whitespace-nowrap">{formatTs(evt.detected_at)}</td>
                        <td className="py-3 pr-4 font-mono text-zinc-300">{evt.agent_id}</td>
                        <td className="py-3 pr-4 text-zinc-400">{evt.hook_name}</td>
                        <td className="py-3 pr-4 text-zinc-300 max-w-xs truncate">{evt.message}</td>
                        <td className="py-3">
                          <span className={`text-[10px] px-2 py-0.5 rounded-full font-semibold uppercase tracking-wide ${severityBadge(evt.severity)}`}>
                            {evt.severity}
                          </span>
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            )}
          </div>

        </div>
      </main>
    </div>
  )
}
