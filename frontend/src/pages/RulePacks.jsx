import { useState, useEffect } from 'react'
import { Link } from 'react-router-dom'
import Sidebar from '../components/Sidebar'
import TopNav from '../components/TopNav'
import Footer from '../components/Footer'
import PageHeader from '../components/PageHeader'
import Toggle from '../components/Toggle'
import { api } from '../api/client'

const RULE_ICONS = {
  'rp-001': 'security_update_good',
  'rp-002': 'data_loss_prevention',
  'rp-003': 'admin_panel_settings',
  'rp-004': 'account_tree',
  'rp-005': 'paid',
  'rp-006': 'description',
}

function severityDot(severity) {
  if (severity === 'critical') return 'bg-[#ffb0cf] shadow-[0_0_8px_rgba(255,176,207,0.6)]'
  if (severity === 'warning') return 'bg-[#ffb0cf]/30'
  return 'bg-zinc-500'
}

function formatTs(ts) {
  if (!ts) return ''
  try {
    return new Date(ts.replace(' ', 'T')).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })
  } catch { return ts }
}

const staticRules = [
  { id: 'rp-001', icon: 'security_update_good', name: 'Prompt Injection Shield', desc: 'Advanced heuristic filtering to prevent jailbreaking attempts and unauthorized behavioral overrides in LLM agents.', enabled: true, version: 'v2.4.1', active: true, detections24h: 0 },
  { id: 'rp-002', icon: 'data_loss_prevention', name: 'DLP Sentinel', desc: 'Scans outbound agent communication for PII, API keys, and sensitive internal documentation leaks.', enabled: true, version: 'v1.9.0', active: true, detections24h: 0 },
  { id: 'rp-003', icon: 'admin_panel_settings', name: 'Privilege Guard', desc: 'Enforces strict Least Privilege access for agents. Limits tool execution to predefined namespaces.', enabled: false, version: null, active: false, detections24h: 0 },
  { id: 'rp-004', icon: 'account_tree', name: 'Supply Chain Watch', desc: 'Monitors external model dependencies and third-party library calls for anomalous behavior.', enabled: true, version: 'v3.1.2', active: true, detections24h: 0 },
]

export default function RulePacks() {
  const [rules, setRules] = useState(staticRules)
  const [loading, setLoading] = useState(true)
  const [totalDetections, setTotalDetections] = useState(0)
  const [density, setDensity] = useState(0)
  const [recentDetections, setRecentDetections] = useState([])
  const [confirmToggle, setConfirmToggle] = useState(null) // { id, newVal, name }

  useEffect(() => {
    api.listRules()
      .then(data => {
        if (data.rule_packs && data.rule_packs.length > 0) {
          setRules(data.rule_packs.map(r => ({
            id: r.id,
            icon: RULE_ICONS[r.id] || 'shield',
            name: r.name,
            desc: r.description,
            enabled: r.enabled,
            version: r.enabled ? (r.version || null) : null,
            active: r.enabled,
            detections24h: r.detections_24h || 0,
          })))
        }
        if (data.total_detections_24h !== undefined) setTotalDetections(data.total_detections_24h)
        if (data.active_rule_density !== undefined) setDensity(data.active_rule_density)
        if (data.recent_detections) setRecentDetections(data.recent_detections)
      })
      .catch(() => {})
      .finally(() => setLoading(false))
  }, [])

  const handleToggle = (id, newVal) => {
    const rule = rules.find(r => r.id === id)
    setConfirmToggle({ id, newVal, name: rule?.name || id })
  }

  const confirmDoToggle = async () => {
    const { id, newVal } = confirmToggle
    setConfirmToggle(null)
    setRules(prev => prev.map(r => r.id === id ? { ...r, enabled: newVal, active: newVal, version: newVal ? (r.version || '') : null } : r))
    try {
      await api.toggleRule(id)
    } catch {
      setRules(prev => prev.map(r => r.id === id ? { ...r, enabled: !newVal, active: !newVal } : r))
    }
  }

  return (
    <div className="min-h-screen bg-[#15121d] text-[#e7dff0]">
      <Sidebar />
      <TopNav />
      <Footer />

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
                    ? 'bg-gradient-to-br from-[#ffb0cf] to-[#f256a3] text-[#3d0023]'
                    : 'bg-zinc-700 text-white hover:bg-zinc-600'
                }`}
              >
                {confirmToggle.newVal ? 'Enable' : 'Disable'}
              </button>
            </div>
          </div>
        </div>
      )}
      <main className="pl-64 pt-16 pb-8 min-h-screen">
        <div className="p-6 lg:p-10 space-y-8">
          <PageHeader
            title="Rule Packs"
            subtitle="Configure and manage security policies enforced across all monitored AI agents."
            actions={<>
              <button className="px-4 py-2 bg-[#2c2834] text-[#e7dff0] text-xs font-medium rounded-sm border border-[#574149]/10 hover:bg-[#37333f] transition-colors">
                Import Manifest
              </button>
              <button className="px-4 py-2 bg-gradient-to-br from-[#ffb0cf] to-[#f256a3] text-[#3d0023] text-xs font-semibold rounded-sm">
                New Custom Rule
              </button>
            </>}
          />

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
                  <p className="text-xs text-zinc-400 leading-relaxed min-h-[4.5rem]">{rule.desc}</p>
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

          <section className="grid grid-cols-1 lg:grid-cols-3 gap-8">
            <div className="lg:col-span-2 bg-[#211e29] p-8 rounded-sm">
              <div className="flex justify-between items-center mb-8">
                <h4 className="text-base font-semibold tracking-tight">Recent Detections</h4>
              </div>
              <div className="space-y-6">
                {recentDetections.length > 0 ? recentDetections.map((evt, i) => (
                  <div key={i} className="flex items-start gap-4">
                    <div className={`w-2 h-2 rounded-full mt-2 flex-shrink-0 ${severityDot(evt.severity)}`}></div>
                    <div className="flex-1 min-w-0">
                      <p className="text-xs text-[#e7dff0]">
                        <span className="font-semibold text-white">{evt.rule_name}</span>{' '}
                        {evt.message}
                      </p>
                      <p className="text-[10px] text-zinc-500 mt-1 uppercase tracking-tight">
                        {formatTs(evt.detected_at)} • Agent: {evt.agent_id}
                      </p>
                    </div>
                  </div>
                )) : (
                  <p className="text-xs text-zinc-500">No detections yet. Security events detected by rule MVs will appear here.</p>
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
