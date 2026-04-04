import { useState, useEffect } from 'react'
import { useParams, Link } from 'react-router-dom'
import Sidebar from '../components/Sidebar'
import Footer from '../components/Footer'
import { api } from '../api/client'

const pulseHeights = [40, 60, 80, 40, 100, 70, 50, 90, 30, 100, 60, 80]

const staticTimeline = [
  {
    id: 1,
    color: 'bg-[#ffb0cf] shadow-[0_0_8px_#ffb0cf]',
    borderHover: 'hover:border-[#ffb0cf]',
    title: 'Security Policy Check',
    titleColor: 'text-[#e7dff0]',
    time: '14:22:01.442',
    desc: <>Verified request against <span className="text-[#ffb0cf]">Global-Inbound-Ruleset-V4</span>. No violations detected in prompt injection scan.</>,
    meta: [{ icon: 'timer', text: '12ms' }, { icon: 'database', text: 'metadata_ref: 0x44A' }],
    error: null,
  },
  {
    id: 2,
    color: 'bg-[#ffb4ab] shadow-[0_0_8px_#ffb4ab]',
    borderHover: 'hover:border-[#ffb4ab]',
    title: 'Model Hallucination Alert',
    titleColor: 'text-[#ffb4ab]',
    time: '14:18:55.201',
    desc: 'Output entropy exceeded defined threshold (0.84). Content flagged for RAG re-verification. High variance in token probability.',
    meta: [],
    error: '"Error: Confidence score below 0.7. Automated corrective prompt injected."',
  },
  {
    id: 3,
    color: 'bg-[#ffb0cf]/40 shadow-[0_0_4px_#ffb0cf66]',
    borderHover: 'hover:border-[#ffb0cf]',
    title: 'Context Window Compression',
    titleColor: 'text-[#e7dff0]',
    time: '14:05:12.889',
    desc: 'Successfully compressed 8k history tokens into 256 summarization tokens using recursive summarizer module.',
    meta: [],
    error: null,
  },
]

export default function AgentDetail() {
  const { id } = useParams()
  const [agent, setAgent] = useState(null)
  const [loading, setLoading] = useState(true)

  useEffect(() => {
    api.getAgent(id)
      .then(setAgent)
      .catch(() => {})
      .finally(() => setLoading(false))
  }, [id])

  const metrics = agent ? [
    { label: 'Total Turns', value: agent.totalTurns ?? '1,402', unit: '', delta: '+12% from last session', deltaColor: 'text-[#ffb0cf]', icon: 'sync_alt', deltaIcon: 'trending_up' },
    { label: 'Latency', value: agent.latency ?? '840', unit: 'ms', delta: 'Stable performance', deltaColor: 'text-[#ffb2b8]', icon: 'speed', deltaIcon: 'check_circle' },
    { label: 'Success Rate', value: agent.successRate ?? '99.8', unit: '%', delta: 'Target threshold met', deltaColor: 'text-[#ffb0cf]', icon: 'verified', deltaIcon: 'shield' },
    { label: 'Token Usage', value: agent.tokenUsage ?? '42.1', unit: 'k', delta: 'Next tier at 50k', deltaColor: 'text-zinc-400', icon: 'toll', deltaIcon: 'info' },
  ] : [
    { label: 'Total Turns', value: '1,402', unit: '', delta: '+12% from last session', deltaColor: 'text-[#ffb0cf]', icon: 'sync_alt', deltaIcon: 'trending_up' },
    { label: 'Latency', value: '840', unit: 'ms', delta: 'Stable performance', deltaColor: 'text-[#ffb2b8]', icon: 'speed', deltaIcon: 'check_circle' },
    { label: 'Success Rate', value: '99.8', unit: '%', delta: 'Target threshold met', deltaColor: 'text-[#ffb0cf]', icon: 'verified', deltaIcon: 'shield' },
    { label: 'Token Usage', value: '42.1', unit: 'k', delta: 'Next tier at 50k', deltaColor: 'text-zinc-400', icon: 'toll', deltaIcon: 'info' },
  ]

  return (
    <div className="min-h-screen bg-[#15121d] text-[#e7dff0]">
      <Sidebar />
      <Footer />

      {/* Custom TopNav for detail page */}
      <header className="fixed top-0 right-0 left-64 h-16 bg-[#15121d] z-50 flex justify-between items-center px-8 shadow-[0_0_12px_rgba(213,63,140,0.12)] backdrop-blur-md">
        <div className="flex items-center gap-4">
          <Link to="/agents" className="text-zinc-400 hover:text-[#ffb0cf] transition-colors">
            <span className="material-symbols-outlined">arrow_back</span>
          </Link>
          <span className="font-medium tracking-tight text-lg text-[#e7dff0]">{id || 'Shield-Core-01'}</span>
          <span className="bg-[#ffb2b8]/10 text-[#ffb2b8] px-2 py-0.5 rounded-full text-[10px] font-semibold uppercase tracking-tighter">Live Monitor</span>
        </div>
        <div className="flex items-center gap-6">
          <div className="flex gap-4">
            <button className="text-zinc-400 hover:text-[#ffb0cf] transition-colors">
              <span className="material-symbols-outlined">monitor_heart</span>
            </button>
            <button className="text-zinc-400 hover:text-[#ffb0cf] transition-colors relative">
              <span className="material-symbols-outlined">notifications</span>
              <span className="absolute top-0 right-0 w-2 h-2 bg-[#ffb0cf] rounded-full border-2 border-[#15121d]"></span>
            </button>
          </div>
          <div className="w-8 h-8 rounded-sm bg-[#2c2834] flex items-center justify-center text-[#ffb0cf] font-semibold text-xs">AS</div>
        </div>
      </header>

      <main className="pl-64 pt-16 pb-8 min-h-screen">
        <div className="p-8 space-y-8">
          {/* Metrics */}
          <section className="grid grid-cols-1 md:grid-cols-4 gap-6">
            {metrics.map((m, i) => (
              <div key={i} className="bg-[#2c2834] p-6 rounded relative overflow-hidden group">
                <div className="absolute top-0 right-0 p-2 opacity-10 group-hover:opacity-20 transition-opacity">
                  <span className="material-symbols-outlined text-5xl">{m.icon}</span>
                </div>
                <p className="text-xs font-medium text-zinc-500 uppercase tracking-widest mb-1">{m.label}</p>
                <h2 className="text-3xl font-medium text-[#e7dff0]">
                  {m.value}<span className="text-base text-zinc-500 ml-1">{m.unit}</span>
                </h2>
                <div className={`mt-4 flex items-center gap-2 text-[10px] ${m.deltaColor}`}>
                  <span className="material-symbols-outlined text-xs">{m.deltaIcon}</span>
                  <span>{m.delta}</span>
                </div>
              </div>
            ))}
          </section>

          {/* Main Split */}
          <div className="grid grid-cols-1 lg:grid-cols-3 gap-8">
            {/* Timeline */}
            <section className="lg:col-span-2 space-y-6">
              <div className="flex items-center justify-between">
                <h3 className="text-base font-medium text-[#e7dff0]">Real-Time Trace Timeline</h3>
                <div className="flex gap-2">
                  <span className="bg-[#0f0c17] text-zinc-400 text-[10px] px-3 py-1 rounded-full border border-[#574149]/10 cursor-pointer hover:border-[#ffb0cf]/40 transition-colors">Filter: Errors</span>
                  <span className="bg-[#0f0c17] text-zinc-400 text-[10px] px-3 py-1 rounded-full border border-[#574149]/10 cursor-pointer hover:border-[#ffb0cf]/40 transition-colors">Export Logs</span>
                </div>
              </div>
              <div className="bg-[#211e29] rounded-sm p-1">
                {staticTimeline.map(item => (
                  <div key={item.id} className={`group flex gap-6 p-6 hover:bg-[#2c2834] transition-all border-l-2 border-transparent ${item.borderHover}`}>
                    <div className="flex flex-col items-center">
                      <div className={`w-2 h-2 rounded-full ${item.color}`}></div>
                      <div className="w-px flex-1 bg-[#574149]/20 my-2"></div>
                    </div>
                    <div className="flex-1 space-y-2">
                      <div className="flex justify-between items-start">
                        <h4 className={`text-xs font-medium ${item.titleColor}`}>{item.title}</h4>
                        <span className="text-[10px] text-zinc-500 font-mono">{item.time}</span>
                      </div>
                      <p className="text-xs text-zinc-400 leading-relaxed max-w-2xl">{item.desc}</p>
                      {item.error && (
                        <div className="bg-[#0f0c17] p-3 rounded-sm border border-[#ffb4ab]/10 text-[11px] text-[#ffb4ab]/80 font-mono mt-2">{item.error}</div>
                      )}
                      {item.meta.length > 0 && (
                        <div className="flex gap-4 pt-2">
                          {item.meta.map(m => (
                            <span key={m.text} className="text-[10px] text-zinc-500 flex items-center gap-1">
                              <span className="material-symbols-outlined text-[14px]">{m.icon}</span> {m.text}
                            </span>
                          ))}
                        </div>
                      )}
                    </div>
                  </div>
                ))}
              </div>
            </section>

            {/* Identity Panel */}
            <aside className="space-y-6">
              <h3 className="text-base font-medium text-[#e7dff0]">Agent Identity</h3>
              <div className="bg-[#2c2834] rounded-sm p-6 space-y-6">
                <div className="space-y-4">
                  <div className="flex items-center gap-3">
                    <div className="w-12 h-12 bg-[#0f0c17] rounded-sm flex items-center justify-center border border-[#574149]/20">
                      <span className="material-symbols-outlined text-[#ffb0cf] text-xl">memory</span>
                    </div>
                    <div>
                      <h4 className="text-xs font-semibold text-[#ffb0cf] uppercase tracking-tighter">Primary Model</h4>
                      <p className="text-xs font-medium text-[#e7dff0]">GPT-4o Sentinel Edition</p>
                    </div>
                  </div>
                  <div className="grid grid-cols-2 gap-4">
                    <div className="bg-[#0f0c17] p-3 rounded-sm border border-[#574149]/10">
                      <p className="text-[9px] text-zinc-500 uppercase font-semibold tracking-widest">Version</p>
                      <p className="text-xs text-[#e7dff0] mt-1">v2.4.12-prod</p>
                    </div>
                    <div className="bg-[#0f0c17] p-3 rounded-sm border border-[#574149]/10">
                      <p className="text-[9px] text-zinc-500 uppercase font-semibold tracking-widest">Context</p>
                      <p className="text-xs text-[#e7dff0] mt-1">128k Tokens</p>
                    </div>
                  </div>
                </div>
                <div className="pt-6 border-t border-[#574149]/10 space-y-4">
                  <h5 className="text-[10px] font-semibold text-zinc-500 uppercase tracking-widest">Active Session</h5>
                  <div className="space-y-3">
                    {[
                      { label: 'Session ID', value: '#SH-8821-X99L', mono: true },
                      { label: 'Environment', value: 'Production (AWS-US-E1)', mono: false },
                      { label: 'Auto-Scaling', value: 'Enabled', mono: false, icon: 'check_circle' },
                    ].map(row => (
                      <div key={row.label} className="flex justify-between items-center text-xs">
                        <span className="text-zinc-400">{row.label}</span>
                        <span className={`text-[#e7dff0] ${row.mono ? 'font-mono text-[10px]' : ''} ${row.icon ? 'text-[#ffb0cf] flex items-center gap-1' : ''}`}>
                          {row.value}
                          {row.icon && <span className="material-symbols-outlined text-[14px]" style={{ fontVariationSettings: "'FILL' 1" }}>{row.icon}</span>}
                        </span>
                      </div>
                    ))}
                  </div>
                </div>
                <div className="pt-6 border-t border-[#574149]/10">
                  <h5 className="text-[10px] font-semibold text-zinc-500 uppercase tracking-widest mb-3">Security Tags</h5>
                  <div className="flex flex-wrap gap-2">
                    {['Data Isolation', 'No-PII Policy', 'Encrypted-Out'].map(tag => (
                      <span key={tag} className="bg-[#4b4754]/30 text-[#bcb6c5] px-2 py-1 rounded-full text-[10px] font-medium border border-[#574149]/20">{tag}</span>
                    ))}
                  </div>
                </div>
                <div className="pt-8 flex flex-col gap-3">
                  <button className="w-full py-3 bg-[#0f0c17] border border-[#ffb0cf]/30 text-[#ffb0cf] text-xs font-semibold uppercase tracking-widest rounded-sm hover:bg-[#ffb0cf]/10 transition-all">
                    Update Config
                  </button>
                  <button className="w-full py-3 bg-[#93000a] text-[#ffdad6] text-xs font-semibold uppercase tracking-widest rounded-sm hover:opacity-90 transition-all flex items-center justify-center gap-2">
                    <span className="material-symbols-outlined text-[18px]">power_settings_new</span>
                    Terminate Agent
                  </button>
                </div>
              </div>

              {/* Neural Health */}
              <div className="bg-gradient-to-br from-[#1d1a25] to-[#0f0c17] rounded-sm p-6 border border-[#574149]/10 overflow-hidden relative group">
                <div className="absolute -right-10 -bottom-10 w-40 h-40 bg-[#ffb0cf] opacity-5 blur-[60px] group-hover:opacity-10 transition-opacity"></div>
                <h5 className="text-[10px] font-semibold text-zinc-500 uppercase tracking-widest mb-4">Neural Health Pulse</h5>
                <div className="flex items-end gap-1 h-12 mb-2">
                  {pulseHeights.map((h, i) => (
                    <div key={i} className="w-1 bg-[#ffb0cf] rounded-t" style={{ height: `${h}%`, opacity: h / 100 }}></div>
                  ))}
                </div>
                <p className="text-[10px] text-zinc-500">Stability: <span className="text-[#ffb2b8]">Optimal</span></p>
              </div>
            </aside>
          </div>
        </div>
      </main>
    </div>
  )
}
