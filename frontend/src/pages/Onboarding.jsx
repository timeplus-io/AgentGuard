import { useState } from 'react'
import { api } from '../api/client'

const STEPS = [
  { label: 'Select Agent', icon: 'smart_toy' },
  { label: 'Connect Telemetry', icon: 'cable' },
  { label: 'Setup Hooks', icon: 'webhook' },
  { label: 'Activate Rules', icon: 'rule' },
]

const agentTypes = [
  {
    id: 'openclaw',
    name: 'OpenClaw',
    desc: 'Open-source agent with full customization. Ideal for production workloads and custom security policies.',
    icon: 'terminal',
    badge: 'Popular',
  },
  {
    id: 'claude-code',
    name: 'Claude Code',
    desc: 'Anthropic\'s Claude integration. Best for agentic coding tasks with built-in safety guardrails.',
    icon: 'code',
    badge: 'Recommended',
  },
]

const hookRules = [
  { id: 'prompt-injection', label: 'Prompt Injection Shield', desc: 'Blocks jailbreak attempts', enabled: true },
  { id: 'dlp', label: 'DLP Sentinel', desc: 'Prevents PII leakage', enabled: true },
  { id: 'supply-chain', label: 'Supply Chain Watch', desc: 'Monitors external dependencies', enabled: false },
  { id: 'privilege', label: 'Privilege Guard', desc: 'Enforces least-privilege', enabled: false },
]

const defaultConfig = `{
  "agent_id": "AS-SENTINEL-01-V4",
  "telemetry_endpoint": "https://mesh.agentshield.io/v1/stream",
  "auth_mode": "OIDC_CHALLENGE",
  "security_hooks": [
    "prompt_injection_v4",
    "pii_leak_detection",
    "adversarial_probing"
  ],
  "reporting_interval": "100ms",
  "fail_safe": true
}`

export default function Onboarding() {
  const [step, setStep] = useState(0)
  const [selectedAgent, setSelectedAgent] = useState('openclaw')
  const [endpoint, setEndpoint] = useState('https://mesh.agentshield.io/v1/stream')
  const [captureContent, setCaptureContent] = useState(false)
  const [generatedConfig, setGeneratedConfig] = useState(defaultConfig)
  const [rules, setRules] = useState(hookRules)
  const [copied, setCopied] = useState(false)
  const [generating, setGenerating] = useState(false)

  const handleNext = async () => {
    if (step === 1) {
      setGenerating(true)
      try {
        const data = await api.generateConfig({ agentType: selectedAgent, endpoint, captureContent })
        if (data.config) setGeneratedConfig(JSON.stringify(data.config, null, 2))
      } catch {
        // keep default
      } finally {
        setGenerating(false)
      }
    }
    setStep(s => Math.min(3, s + 1))
  }

  const handleBack = () => setStep(s => Math.max(0, s - 1))

  const handleCopy = () => {
    navigator.clipboard.writeText(generatedConfig)
    setCopied(true)
    setTimeout(() => setCopied(false), 2000)
  }

  const toggleRule = id => setRules(prev => prev.map(r => r.id === id ? { ...r, enabled: !r.enabled } : r))

  return (
    <div className="bg-[#15121d] text-[#e7dff0] min-h-screen flex flex-col">
      {/* TopNav */}
      <nav className="bg-[#15121d] border-none shadow-[0_0_12px_rgba(213,63,140,0.12)] sticky top-0 z-50 h-16 w-full flex justify-between items-center px-6 backdrop-blur-md">
        <div className="flex items-center gap-4">
          <span className="text-lg font-semibold bg-clip-text text-transparent bg-gradient-to-br from-[#ffb0cf] to-[#f256a3] tracking-tight">AgentShield</span>
        </div>
        <div className="flex items-center gap-6">
          <div className="flex items-center gap-4 text-zinc-400">
            <span className="material-symbols-outlined hover:text-[#ffb0cf] transition-colors cursor-pointer">monitor_heart</span>
            <span className="material-symbols-outlined hover:text-[#ffb0cf] transition-colors cursor-pointer">notifications</span>
          </div>
          <div className="w-8 h-8 rounded-full bg-[#37333f] flex items-center justify-center border border-[#574149]/20 text-[#ffb0cf] font-semibold text-xs">AS</div>
        </div>
      </nav>

      <main className="flex-1 flex items-center justify-center p-6 md:p-12">
        <div className="w-full max-w-5xl grid grid-cols-1 lg:grid-cols-12 gap-12 items-start">
          {/* Left: Stepper */}
          <div className="lg:col-span-4 space-y-12">
            <div>
              <h1 className="text-3xl font-extrabold tracking-tighter text-white mb-2">Deploying Sentinel-01</h1>
              <p className="text-[#ddbfc8] text-xs leading-relaxed">System integration phase. Establish a secure telemetry handshake between your AI agent and the global security mesh.</p>
            </div>

            <div className="space-y-8 relative">
              <div className="absolute left-[11px] top-2 bottom-2 w-[2px] bg-[#37333f]"></div>
              {STEPS.map((s, i) => {
                const done = i < step
                const active = i === step
                return (
                  <div key={i} className="flex items-center gap-4 relative">
                    {done ? (
                      <div className="h-6 w-6 rounded-full bg-[#f256a3] flex items-center justify-center z-10 shadow-[0_0_8px_rgba(242,86,163,0.4)]">
                        <span className="material-symbols-outlined text-[14px] text-[#570033] font-semibold">check</span>
                      </div>
                    ) : active ? (
                      <div className="h-6 w-6 rounded-full bg-gradient-to-br from-[#ffb0cf] to-[#f256a3] flex items-center justify-center z-10 ring-4 ring-[#ffb0cf]/10">
                        <div className="h-2 w-2 rounded-full bg-[#3d0023]"></div>
                      </div>
                    ) : (
                      <div className="h-6 w-6 rounded-full bg-[#37333f] flex items-center justify-center z-10">
                        <div className="h-1.5 w-1.5 rounded-full bg-zinc-600"></div>
                      </div>
                    )}
                    <span className={`text-xs ${active ? 'font-semibold text-[#ffb0cf] tracking-wide' : done ? 'font-medium text-zinc-500' : 'font-medium text-zinc-600'}`}>
                      {i + 1}. {s.label}
                    </span>
                  </div>
                )
              })}
            </div>

            <div className="bg-[#211e29] rounded-lg p-6 flex items-start gap-4">
              <span className="material-symbols-outlined text-[#ffb2b8]">verified_user</span>
              <div>
                <h4 className="text-xs font-semibold text-[#e7dff0] uppercase tracking-widest mb-1">Encrypted Tunnel</h4>
                <p className="text-xs text-[#ddbfc8] leading-tight">All telemetry data is AES-256 encrypted at rest and in transit.</p>
              </div>
            </div>
          </div>

          {/* Right: Step Content */}
          <div className="lg:col-span-8 bg-[#2c2834] rounded-lg overflow-hidden flex flex-col min-h-[500px]">
            <div className="p-8 flex-1 space-y-8">
              {/* Step 0: Select Agent */}
              {step === 0 && (
                <>
                  <div>
                    <h2 className="text-xl font-semibold text-white tracking-tight">Select Agent Type</h2>
                    <p className="text-[#ddbfc8] mt-2 leading-relaxed">Choose the AI agent framework you want to protect with AgentShield.</p>
                  </div>
                  <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
                    {agentTypes.map(at => (
                      <button
                        key={at.id}
                        onClick={() => setSelectedAgent(at.id)}
                        className={`p-6 rounded-lg text-left flex flex-col gap-4 border-2 transition-all ${
                          selectedAgent === at.id
                            ? 'border-[#ffb0cf] bg-[#ffb0cf]/5'
                            : 'border-[#574149]/20 bg-[#1d1a25] hover:border-[#574149]/40'
                        }`}
                      >
                        <div className="flex items-center justify-between">
                          <div className={`w-12 h-12 rounded-sm flex items-center justify-center ${selectedAgent === at.id ? 'bg-[#ffb0cf]/10 text-[#ffb0cf]' : 'bg-[#211e29] text-zinc-500'}`}>
                            <span className="material-symbols-outlined text-xl">{at.icon}</span>
                          </div>
                          <span className={`text-[10px] font-semibold px-2 py-0.5 rounded-full ${selectedAgent === at.id ? 'bg-[#ffb0cf]/10 text-[#ffb0cf]' : 'bg-[#37333f] text-zinc-500'}`}>{at.badge}</span>
                        </div>
                        <div>
                          <h3 className="text-base font-semibold text-white mb-1">{at.name}</h3>
                          <p className="text-xs text-zinc-400">{at.desc}</p>
                        </div>
                      </button>
                    ))}
                  </div>
                </>
              )}

              {/* Step 1: Telemetry */}
              {step === 1 && (
                <>
                  <div>
                    <h2 className="text-xl font-semibold text-white tracking-tight">Telemetry Handshake</h2>
                    <p className="text-[#ddbfc8] leading-relaxed mt-2">Configure your telemetry endpoint to enable real-time security monitoring.</p>
                  </div>
                  <div className="space-y-6">
                    <div className="space-y-2">
                      <label className="text-xs font-semibold text-zinc-400 uppercase tracking-widest">OTel Endpoint</label>
                      <input
                        value={endpoint}
                        onChange={e => setEndpoint(e.target.value)}
                        className="w-full bg-[#0f0c17] border-none text-xs py-3 px-4 rounded-sm focus:ring-1 focus:ring-[#ffb0cf] focus:outline-none text-[#e7dff0] font-mono"
                        type="text"
                      />
                    </div>
                    <div className="flex items-center justify-between p-4 bg-[#1d1a25] rounded-sm">
                      <div>
                        <p className="text-xs font-medium text-[#e7dff0]">Capture Content</p>
                        <p className="text-xs text-zinc-500 mt-1">Enable full prompt/response capture for forensic analysis</p>
                      </div>
                      <button
                        onClick={() => setCaptureContent(v => !v)}
                        className={`relative w-10 h-5 rounded-full transition-colors ${captureContent ? 'bg-[#D53F8C]' : 'bg-[#4b4754]'}`}
                      >
                        <span className={`absolute top-[3px] w-[14px] h-[14px] rounded-full bg-white transition-transform ${captureContent ? 'left-0 translate-x-[23px]' : 'left-[3px]'}`} />
                      </button>
                    </div>
                  </div>
                </>
              )}

              {/* Step 2: Deploy Hooks */}
              {step === 2 && (
                <>
                  <div>
                    <h2 className="text-xl font-semibold text-white tracking-tight">Deploy Security Hooks</h2>
                    <p className="text-[#ddbfc8] leading-relaxed mt-2">Place this configuration in your agent's runtime directory to activate protection.</p>
                  </div>
                  <div className="group relative bg-[#0f0c17] rounded-lg overflow-hidden border border-[#574149]/10">
                    <div className="bg-[#37333f]/50 px-4 py-2 flex justify-between items-center">
                      <span className="text-[10px] font-semibold text-zinc-500 uppercase tracking-widest">agentshield_config.json</span>
                      <button onClick={handleCopy} className="flex items-center gap-2 text-[#ffb0cf] hover:text-white transition-colors">
                        <span className="text-[10px] font-semibold uppercase">{copied ? 'Copied!' : 'Copy Code'}</span>
                        <span className="material-symbols-outlined text-xs">content_copy</span>
                      </button>
                    </div>
                    <pre className="p-6 overflow-x-auto text-xs font-mono leading-relaxed text-zinc-300 custom-scrollbar">
                      {generatedConfig}
                    </pre>
                  </div>
                  <div className="flex items-center gap-3 text-xs text-zinc-400">
                    <span className="material-symbols-outlined text-[#ffb0cf] text-sm">info</span>
                    <span>Place this within the <code className="text-[#ffb0cf]">/config/security/</code> directory of your runtime environment.</span>
                  </div>
                </>
              )}

              {/* Step 3: Activate Rules */}
              {step === 3 && (
                <>
                  <div>
                    <h2 className="text-xl font-semibold text-white tracking-tight">Activate Rule Packs</h2>
                    <p className="text-[#ddbfc8] leading-relaxed mt-2">Enable the security rules you want to apply to your agent.</p>
                  </div>
                  <div className="space-y-3">
                    {rules.map(rule => (
                      <div key={rule.id} className="flex items-center justify-between p-4 bg-[#1d1a25] rounded-sm hover:bg-[#211e29] transition-colors">
                        <div className="flex items-center gap-4">
                          <div className={`w-8 h-8 rounded-sm flex items-center justify-center ${rule.enabled ? 'bg-[#ffb0cf]/10 text-[#ffb0cf]' : 'bg-[#37333f] text-zinc-500'}`}>
                            <span className="material-symbols-outlined text-xs">security</span>
                          </div>
                          <div>
                            <p className="text-xs font-medium text-[#e7dff0]">{rule.label}</p>
                            <p className="text-xs text-zinc-500">{rule.desc}</p>
                          </div>
                        </div>
                        <button
                          onClick={() => toggleRule(rule.id)}
                          className={`relative w-9 h-5 rounded-full transition-colors ${rule.enabled ? 'bg-[#D53F8C]' : 'bg-[#4b4754]'}`}
                        >
                          <span className={`absolute top-[3px] w-[14px] h-[14px] rounded-full bg-white transition-transform ${rule.enabled ? 'left-0 translate-x-[19px]' : 'left-[3px]'}`} />
                        </button>
                      </div>
                    ))}
                  </div>
                </>
              )}
            </div>

            {/* Footer Actions */}
            <div className="bg-[#37333f]/30 p-8 flex justify-between items-center">
              <button
                onClick={handleBack}
                disabled={step === 0}
                className="px-6 py-2.5 text-xs font-medium text-zinc-400 hover:text-white transition-all flex items-center gap-2 disabled:opacity-40 disabled:cursor-not-allowed"
              >
                <span className="material-symbols-outlined text-xs">arrow_back</span>
                Back
              </button>
              <div className="flex items-center gap-4">
                <button className="px-6 py-2.5 text-xs font-medium text-[#ffb0cf] hover:bg-[#ffb0cf]/5 transition-all">
                  Save Draft
                </button>
                {step < 3 ? (
                  <button
                    onClick={handleNext}
                    disabled={generating}
                    className="bg-gradient-to-br from-[#ffb0cf] to-[#f256a3] px-8 py-2.5 rounded-lg text-xs font-semibold text-[#3d0023] shadow-lg shadow-[#ffb0cf]/20 hover:scale-[1.02] active:scale-95 transition-all flex items-center gap-2 disabled:opacity-70"
                  >
                    {generating ? 'Generating...' : 'Next Step'}
                    <span className="material-symbols-outlined text-xs">arrow_forward</span>
                  </button>
                ) : (
                  <button className="bg-gradient-to-br from-[#ffb0cf] to-[#f256a3] px-8 py-2.5 rounded-lg text-xs font-semibold text-[#3d0023] shadow-lg shadow-[#ffb0cf]/20 hover:scale-[1.02] active:scale-95 transition-all flex items-center gap-2">
                    <span className="material-symbols-outlined text-xs">check_circle</span>
                    Activate Agent
                  </button>
                )}
              </div>
            </div>
          </div>
        </div>
      </main>

      {/* Activity Indicator */}
      <aside className="fixed bottom-8 left-8 hidden xl:block">
        <div className="flex items-center gap-4 bg-[#2c2834]/40 backdrop-blur-md p-3 px-4 rounded-full border border-[#574149]/10">
          <div className="h-2 w-2 rounded-full bg-[#ffb0cf] animate-pulse shadow-[0_0_8px_#ffb0cf]"></div>
          <span className="text-[10px] font-semibold uppercase tracking-widest text-zinc-400">Waiting for handshake...</span>
        </div>
      </aside>
    </div>
  )
}
