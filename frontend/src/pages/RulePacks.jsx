import { useState, useEffect } from 'react'
import Sidebar from '../components/Sidebar'
import TopNav from '../components/TopNav'
import Footer from '../components/Footer'
import PageHeader from '../components/PageHeader'
import Toggle from '../components/Toggle'
import { api } from '../api/client'

const staticRules = [
  {
    id: 'prompt-injection',
    icon: 'security_update_good',
    name: 'Prompt Injection Shield',
    desc: 'Advanced heuristic filtering to prevent jailbreaking attempts and unauthorized behavioral overrides in LLM agents.',
    enabled: true,
    version: 'v2.4.1',
    active: true,
  },
  {
    id: 'dlp-sentinel',
    icon: 'data_loss_prevention',
    name: 'DLP Sentinel',
    desc: 'Scans outbound agent communication for PII, API keys, and sensitive internal documentation leaks.',
    enabled: true,
    version: 'v1.9.0',
    active: true,
  },
  {
    id: 'privilege-guard',
    icon: 'admin_panel_settings',
    name: 'Privilege Guard',
    desc: 'Enforces strict Least Privilege access for agents. Limits tool execution to predefined namespaces.',
    enabled: false,
    version: null,
    active: false,
  },
  {
    id: 'supply-chain',
    icon: 'account_tree',
    name: 'Supply Chain Watch',
    desc: 'Monitors external model dependencies and third-party library calls for anomalous behavior.',
    enabled: true,
    version: 'v3.1.2',
    active: true,
  },
]

const enforcementLog = [
  { dot: 'bg-[#ffb0cf] shadow-[0_0_8px_rgba(255,176,207,0.6)]', text: <><span className="font-semibold text-white">Prompt Injection Shield</span> blocked a malicious payload in <span className="text-[#f256a3]">Customer-Service-Agent-04</span></>, time: '2 minutes ago • Source: 192.168.1.45' },
  { dot: 'bg-[#ffb0cf]/30', text: <><span className="font-semibold text-white">DLP Sentinel</span> redacted a credit card number in outbound response</>, time: '14 minutes ago • Source: Internal SQL Hook' },
  { dot: 'bg-[#ffb4ab]', highlight: true, text: <><span className="font-semibold text-white">Rule Pack v3.1.2</span> update successfully deployed to all production clusters</>, time: '1 hour ago • Auto-sync' },
]

export default function RulePacks() {
  const [rules, setRules] = useState(staticRules)
  const [loading, setLoading] = useState(true)

  useEffect(() => {
    api.listRules()
      .then(data => {
        if (data.rules && data.rules.length > 0) setRules(data.rules)
      })
      .catch(() => {})
      .finally(() => setLoading(false))
  }, [])

  const handleToggle = async (id, newVal) => {
    setRules(prev => prev.map(r => r.id === id ? { ...r, enabled: newVal } : r))
    try {
      await api.toggleRule(id)
    } catch {
      setRules(prev => prev.map(r => r.id === id ? { ...r, enabled: !newVal } : r))
    }
  }

  return (
    <div className="min-h-screen bg-[#15121d] text-[#e7dff0]">
      <Sidebar />
      <TopNav />
      <Footer />
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

          {/* Card Grid */}
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
                </div>
                <div className="mt-auto flex items-center justify-between z-10">
                  <a className="text-xs font-semibold text-[#ffb0cf] tracking-wider uppercase hover:underline decoration-2 underline-offset-4 cursor-pointer">View Details</a>
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

          {/* Bottom Area */}
          <section className="grid grid-cols-1 lg:grid-cols-3 gap-8">
            <div className="lg:col-span-2 bg-[#211e29] p-8 rounded-sm">
              <div className="flex justify-between items-center mb-8">
                <h4 className="text-base font-semibold tracking-tight">Recent Policy Enforcement</h4>
                <button className="text-xs text-zinc-500 hover:text-white transition-colors uppercase tracking-widest font-semibold">Clear Logs</button>
              </div>
              <div className="space-y-6">
                {enforcementLog.map((log, i) => (
                  <div key={i} className={`flex items-start gap-4 ${log.highlight ? 'border-l-2 border-[#ffb0cf]/20 pl-4 py-1' : ''}`}>
                    <div className={`w-2 h-2 rounded-full mt-2 ${log.dot}`}></div>
                    <div className="flex-1">
                      <p className="text-xs text-[#e7dff0]">{log.text}</p>
                      <p className="text-[10px] text-zinc-500 mt-1 uppercase tracking-tight">{log.time}</p>
                    </div>
                  </div>
                ))}
              </div>
            </div>

            <div className="flex flex-col gap-6">
              <div className="bg-gradient-to-br from-[#2c2834] to-[#211e29] p-8 rounded-sm relative overflow-hidden group">
                <div className="absolute top-0 left-0 w-1 h-full bg-[#ffb0cf]"></div>
                <p className="text-[10px] text-zinc-500 uppercase tracking-widest font-semibold mb-4">Total Blocks (24h)</p>
                <h5 className="text-3xl font-bold text-white">1,248</h5>
                <p className="text-xs text-[#ffb0cf] mt-2 font-medium">+12.4% from yesterday</p>
              </div>
              <div className="bg-[#2c2834] p-8 rounded-sm border-l border-[#574149]/20">
                <p className="text-[10px] text-zinc-500 uppercase tracking-widest font-semibold mb-4">Active Rule Density</p>
                <h5 className="text-3xl font-bold text-white">84<span className="text-base text-zinc-500">%</span></h5>
                <div className="w-full bg-[#0f0c17] h-1.5 mt-6 rounded-full overflow-hidden">
                  <div className="bg-[#ffb0cf] h-full w-[84%]"></div>
                </div>
              </div>
            </div>
          </section>
        </div>
      </main>
    </div>
  )
}
