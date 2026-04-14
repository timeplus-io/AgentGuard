import { useState, useEffect, useMemo } from 'react'
import { useNavigate } from 'react-router-dom'
import { api } from '../api/client'

const STEPS = [
  { label: 'Select Agent', icon: 'smart_toy' },
  { label: 'Install Plugin', icon: 'extension' },
  { label: 'OTel Config', icon: 'monitoring' },
  { label: 'Activate Rules', icon: 'rule' },
]

const agentTypes = [
  {
    id: 'openclaw',
    name: 'OpenClaw',
    desc: 'Open-source AI agent gateway with full plugin support. Ideal for production workloads and custom security policies.',
    icon: 'terminal',
    badge: 'Supported',
    disabled: false,
  },
  {
    id: 'claude-code',
    name: 'Claude Code',
    desc: "Anthropic's Claude Code CLI. Native AgentGuard integration coming soon.",
    icon: 'code',
    badge: 'Coming Soon',
    disabled: true,
  },
]

function CopyButton({ text }) {
  const [copied, setCopied] = useState(false)
  const handle = () => {
    navigator.clipboard.writeText(text)
    setCopied(true)
    setTimeout(() => setCopied(false), 2000)
  }
  return (
    <button onClick={handle} className="flex items-center gap-1.5 text-[10px] font-semibold uppercase text-[#ffb0cf] hover:text-white transition-colors">
      <span className="material-symbols-outlined text-xs">{copied ? 'check' : 'content_copy'}</span>
      {copied ? 'Copied!' : 'Copy'}
    </button>
  )
}

function CodeBlock({ label, code }) {
  return (
    <div className="bg-[#0f0c17] rounded-sm overflow-hidden border border-[#574149]/10">
      <div className="bg-[#37333f]/50 px-4 py-2 flex justify-between items-center">
        <span className="text-[10px] font-semibold text-zinc-500 uppercase tracking-widest">{label}</span>
        <CopyButton text={code} />
      </div>
      <pre className="p-4 overflow-x-auto text-xs font-mono leading-relaxed text-zinc-300 whitespace-pre-wrap break-all">{code}</pre>
    </div>
  )
}

export default function Onboarding() {
  const navigate = useNavigate()
  const alreadyOnboarded = localStorage.getItem('agentguard_onboarded') === '1'

  const [step, setStep] = useState(0)
  const [selectedAgent, setSelectedAgent] = useState('openclaw')

  // Step 1 config
  const [deploymentId, setDeploymentId] = useState('my-team')
  const [deploymentName, setDeploymentName] = useState('My Team')
  const [timeplusUrl, setTimeplusUrl] = useState('http://localhost:3218')
  const [configTab, setConfigTab] = useState('env')

  // Step 2 OTel config — derived from timeplusUrl host, port 4318; user can override
  const [otelEndpointOverride, setOtelEndpointOverride] = useState(null)
  const derivedOtelEndpoint = useMemo(() => {
    try {
      const u = new URL(timeplusUrl)
      return `${u.protocol}//${u.hostname}:4318`
    } catch { return 'http://localhost:4318' }
  }, [timeplusUrl])
  const otelEndpoint = otelEndpointOverride ?? derivedOtelEndpoint
  const setOtelEndpoint = val => setOtelEndpointOverride(val === derivedOtelEndpoint ? null : val)

  // Step 3 rules
  const [catalogRules, setCatalogRules] = useState([])
  const [enabledRuleIds, setEnabledRuleIds] = useState(new Set())
  const [loadingRules, setLoadingRules] = useState(false)
  const [selectedRules, setSelectedRules] = useState({})
  const [activating, setActivating] = useState(false)
  const [done, setDone] = useState(false)

  useEffect(() => {
    if (step !== 3) return
    setLoadingRules(true)
    Promise.all([api.getRulesCatalog(), api.listRules()])
      .then(([catalog, rules]) => {
        const items = catalog.catalog || []
        const enabled = new Set((rules.rule_packs || []).filter(r => r.enabled).map(r => r.id))
        setCatalogRules(items)
        setEnabledRuleIds(enabled)
        const sel = {}
        items.forEach(r => {
          if (!enabled.has(r.id)) sel[r.id] = false
        })
        setSelectedRules(sel)
      })
      .catch(() => {})
      .finally(() => setLoadingRules(false))
  }, [step])

  const envVarsCode =
    `export AGENTGUARD_TIMEPLUS_URL=${timeplusUrl}\nexport AGENTGUARD_DEPLOYMENT_ID=${deploymentId}\nexport AGENTGUARD_DEPLOYMENT_NAME="${deploymentName}"`

  const jsonConfigCode = JSON.stringify({
    plugins: {
      entries: {
        agentguard: {
          enabled: true,
          config: {
            timeplusUrl,
            deploymentId,
            deploymentName,
            stream: 'agentguard_hook_events',
            flushMs: 2000,
            batchSize: 50,
          },
        },
      },
    },
  }, null, 2)

  const otelConfigCode = JSON.stringify({
    diagnostics: {
      enabled: true,
      otel: {
        enabled: true,
        endpoint: otelEndpoint,
        traces: true,
        metrics: true,
        logs: true,
        serviceName: 'openclaw',
        sampleRate: 1,
        flushIntervalMs: 3000,
      },
    },
    plugins: {
      entries: {
        'diagnostics-otel': { enabled: true },
      },
    },
  }, null, 2)

  const handleNext = () => setStep(s => Math.min(3, s + 1))
  const handleBack = () => setStep(s => Math.max(0, s - 1))
  const toggleRuleSel = id => setSelectedRules(prev => ({ ...prev, [id]: !prev[id] }))

  const handleActivate = async () => {
    setActivating(true)
    const toEnable = Object.entries(selectedRules).filter(([, v]) => v).map(([id]) => id)
    for (const id of toEnable) {
      const rule = catalogRules.find(r => r.id === id)
      try {
        if (!rule?.installed) await api.installRule(id)
        if (!enabledRuleIds.has(id)) await api.toggleRule(id)
      } catch {
        // best effort
      }
    }
    localStorage.setItem('agentguard_onboarded', '1')
    setActivating(false)
    setDone(true)
  }

  if (done) {
    return (
      <div className="bg-[#15121d] text-[#e7dff0] min-h-screen flex flex-col items-center justify-center">
        <div className="text-center space-y-6 max-w-sm">
          <div className="w-16 h-16 mx-auto rounded-full bg-[#ffb0cf]/10 flex items-center justify-center">
            <span className="material-symbols-outlined text-3xl text-[#ffb0cf]">check_circle</span>
          </div>
          <h2 className="text-2xl font-semibold text-white">Agent Activated</h2>
          <p className="text-zinc-400 text-sm leading-relaxed">Your agent is now protected by AgentGuard. Head to the dashboard to monitor activity.</p>
          <button
            onClick={() => navigate('/dashboard')}
            className="bg-gradient-to-br from-[#ffb0cf] to-[#f256a3] px-8 py-2.5 rounded-sm text-xs font-semibold text-[#3d0023] hover:scale-[1.02] transition-transform"
          >
            Go to Dashboard
          </button>
        </div>
      </div>
    )
  }

  return (
    <div className="bg-[#15121d] text-[#e7dff0] min-h-screen flex flex-col">
      {/* TopNav */}
      <nav className="bg-[#15121d] shadow-[0_0_12px_rgba(213,63,140,0.12)] sticky top-0 z-50 h-16 w-full flex justify-between items-center px-6 backdrop-blur-md">
        <span className="text-lg font-semibold bg-clip-text text-transparent bg-gradient-to-br from-[#ffb0cf] to-[#f256a3] tracking-tight">AgentGuard</span>
        {alreadyOnboarded && (
          <button
            onClick={() => navigate('/dashboard')}
            className="text-xs text-zinc-400 hover:text-[#ffb0cf] transition-colors flex items-center gap-1.5"
          >
            <span className="material-symbols-outlined text-xs">arrow_back</span>
            Back to Dashboard
          </button>
        )}
      </nav>

      <main className="flex-1 flex items-start justify-center p-6 md:p-12">
        <div className="w-full max-w-5xl grid grid-cols-1 lg:grid-cols-12 gap-12 items-start">
          {/* Left: Stepper */}
          <div className="lg:col-span-4 space-y-10">
            <div>
              <h1 className="text-2xl font-bold tracking-tight text-white mb-2">Connect Your Agent</h1>
              <p className="text-zinc-500 text-xs leading-relaxed">
                {alreadyOnboarded
                  ? "You've already connected an agent. Update your configuration or enable additional rules."
                  : 'Follow these steps to connect your AI agent to AgentGuard.'}
              </p>
            </div>

            <div className="space-y-6 relative">
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
                    <span className={`text-xs ${active ? 'font-semibold text-[#ffb0cf]' : done ? 'text-zinc-500' : 'text-zinc-600'}`}>
                      {i + 1}. {s.label}
                    </span>
                  </div>
                )
              })}
            </div>

            {alreadyOnboarded && (
              <div className="flex items-start gap-3 p-4 bg-[#1d1a25] rounded-sm">
                <span className="material-symbols-outlined text-amber-400 text-sm mt-0.5">info</span>
                <p className="text-xs text-zinc-400 leading-relaxed">You've completed onboarding before. Rules already enabled will be shown with an "Already Enabled" badge.</p>
              </div>
            )}
          </div>

          {/* Right: Step Content */}
          <div className="lg:col-span-8 bg-[#2c2834] rounded-sm overflow-hidden flex flex-col min-h-[520px]">
            <div className="p-8 flex-1 space-y-6">

              {/* Step 0: Select Agent */}
              {step === 0 && (
                <>
                  <div>
                    <h2 className="text-xl font-semibold text-white tracking-tight">Select Agent Type</h2>
                    <p className="text-zinc-400 mt-1.5 text-xs leading-relaxed">Choose the AI agent framework you want to protect with AgentGuard.</p>
                  </div>
                  <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
                    {agentTypes.map(at => (
                      <button
                        key={at.id}
                        onClick={() => !at.disabled && setSelectedAgent(at.id)}
                        disabled={at.disabled}
                        className={`p-6 rounded-sm text-left flex flex-col gap-4 border-2 transition-all ${
                          at.disabled
                            ? 'border-[#574149]/10 bg-[#1d1a25] opacity-50 cursor-not-allowed'
                            : selectedAgent === at.id
                              ? 'border-[#ffb0cf] bg-[#ffb0cf]/5'
                              : 'border-[#574149]/20 bg-[#1d1a25] hover:border-[#574149]/40'
                        }`}
                      >
                        <div className="flex items-center justify-between">
                          <div className={`w-10 h-10 rounded-sm flex items-center justify-center ${!at.disabled && selectedAgent === at.id ? 'bg-[#ffb0cf]/10 text-[#ffb0cf]' : 'bg-[#211e29] text-zinc-500'}`}>
                            <span className="material-symbols-outlined text-lg">{at.icon}</span>
                          </div>
                          <span className={`text-[10px] font-semibold px-2 py-0.5 rounded-full ${at.disabled ? 'bg-[#37333f] text-zinc-600' : selectedAgent === at.id ? 'bg-[#ffb0cf]/10 text-[#ffb0cf]' : 'bg-[#37333f] text-zinc-500'}`}>
                            {at.badge}
                          </span>
                        </div>
                        <div>
                          <h3 className="text-sm font-semibold text-white mb-1">{at.name}</h3>
                          <p className="text-xs text-zinc-400 leading-relaxed">{at.desc}</p>
                        </div>
                      </button>
                    ))}
                  </div>
                </>
              )}

              {/* Step 1: Install Plugin */}
              {step === 1 && (
                <>
                  <div>
                    <h2 className="text-xl font-semibold text-white tracking-tight">Install Plugin</h2>
                    <p className="text-zinc-400 mt-1.5 text-xs leading-relaxed">Install the AgentGuard plugin into your OpenClaw project and configure it to point at your Timeplus instance.</p>
                  </div>

                  <CodeBlock label="1. Install via npm" code="npm install @timeplus/agentguard-openclaw-plugin" />

                  <div className="space-y-3">
                    <p className="text-[10px] font-semibold text-zinc-500 uppercase tracking-widest">2. Configuration</p>
                    <div className="grid grid-cols-1 gap-3">
                      {[
                        { label: 'Deployment ID', val: deploymentId, set: setDeploymentId, ph: 'my-team' },
                        { label: 'Deployment Name', val: deploymentName, set: setDeploymentName, ph: 'My Team' },
                        { label: 'Timeplus URL', val: timeplusUrl, set: setTimeplusUrl, ph: 'http://localhost:3218' },
                      ].map(({ label, val, set, ph }) => (
                        <div key={label} className="flex items-center gap-4">
                          <label className="text-[10px] font-medium text-zinc-500 uppercase tracking-widest w-36 shrink-0">{label}</label>
                          <input
                            value={val}
                            onChange={e => set(e.target.value)}
                            placeholder={ph}
                            className="flex-1 bg-[#0f0c17] text-xs py-2 px-3 rounded-sm focus:ring-1 focus:ring-[#ffb0cf] focus:outline-none text-[#e7dff0] font-mono"
                          />
                        </div>
                      ))}
                    </div>
                  </div>

                  <div className="space-y-2">
                    <p className="text-[10px] font-semibold text-zinc-500 uppercase tracking-widest">3. Apply configuration</p>
                    <div className="flex gap-2 mb-2">
                      {[{ id: 'env', label: 'Env Variables' }, { id: 'json', label: 'openclaw.json' }].map(tab => (
                        <button
                          key={tab.id}
                          onClick={() => setConfigTab(tab.id)}
                          className={`px-3 py-1 text-[10px] font-semibold rounded-full transition-colors ${configTab === tab.id ? 'bg-[#ffb0cf]/10 text-[#ffb0cf]' : 'text-zinc-500 hover:text-zinc-300'}`}
                        >
                          {tab.label}
                        </button>
                      ))}
                    </div>
                    <CodeBlock
                      label={configTab === 'env' ? '.env / shell export' : 'plugins.entries.agentguard (openclaw.json)'}
                      code={configTab === 'env' ? envVarsCode : jsonConfigCode}
                    />
                  </div>
                </>
              )}

              {/* Step 2: OTel Config */}
              {step === 2 && (
                <>
                  <div>
                    <h2 className="text-xl font-semibold text-white tracking-tight">
                      OpenTelemetry Config
                      <span className="ml-2 text-[10px] font-semibold px-2 py-0.5 rounded-full bg-[#37333f] text-zinc-500 align-middle">Optional</span>
                    </h2>
                    <p className="text-zinc-400 mt-1.5 text-xs leading-relaxed">
                      Enable the built-in OTel diagnostics plugin to export traces, metrics, and logs from OpenClaw to Timeplus. Recommended for full observability, but you can skip this step.
                    </p>
                  </div>

                  <div className="space-y-3">
                    <p className="text-[10px] font-semibold text-zinc-500 uppercase tracking-widest">Timeplus OTLP Endpoint</p>
                    <div className="flex items-center gap-4">
                      <label className="text-[10px] font-medium text-zinc-500 uppercase tracking-widest w-36 shrink-0">Endpoint URL</label>
                      <input
                        value={otelEndpoint}
                        onChange={e => setOtelEndpoint(e.target.value)}
                        placeholder="http://host.docker.internal:4318"
                        className="flex-1 bg-[#0f0c17] text-xs py-2 px-3 rounded-sm focus:ring-1 focus:ring-[#ffb0cf] focus:outline-none text-[#e7dff0] font-mono"
                      />
                    </div>
                    <p className="text-[10px] text-zinc-500 leading-relaxed pl-40">
                      Use <code className="text-zinc-400">host.docker.internal</code> when OpenClaw runs in Docker and Timeplus runs on the host. Replace with the actual host/IP if needed.
                    </p>
                  </div>

                  <CodeBlock label="openclaw.json — diagnostics section" code={otelConfigCode} />
                </>
              )}

              {/* Step 3: Activate Rules */}
              {step === 3 && (
                <>
                  <div>
                    <h2 className="text-xl font-semibold text-white tracking-tight">Activate Security Rules</h2>
                    <p className="text-zinc-400 mt-1.5 text-xs leading-relaxed">Select the security rules to enable for your agent. Toggle rules on to activate them when you click "Activate Agent".</p>
                  </div>

                  {loadingRules ? (
                    <div className="flex items-center justify-center py-12">
                      <span className="material-symbols-outlined animate-spin text-[#ffb0cf]">refresh</span>
                    </div>
                  ) : catalogRules.length === 0 ? (
                    <div className="py-8 text-center text-zinc-500 text-xs">No rules found. Check your backend connection.</div>
                  ) : (
                    <div className="space-y-2">
                      {catalogRules.map(rule => {
                        const alreadyEnabled = enabledRuleIds.has(rule.id)
                        const isSelected = selectedRules[rule.id] ?? false
                        return (
                          <div key={rule.id} className="flex items-center gap-4 p-4 bg-[#1d1a25] rounded-sm hover:bg-[#211e29] transition-colors">
                            <div className={`w-8 h-8 shrink-0 rounded-sm flex items-center justify-center ${alreadyEnabled || isSelected ? 'bg-[#ffb0cf]/10 text-[#ffb0cf]' : 'bg-[#37333f] text-zinc-500'}`}>
                              <span className="material-symbols-outlined text-xs">security</span>
                            </div>
                            <div className="flex-1 min-w-0">
                              <div className="flex flex-wrap items-center gap-2">
                                <p className="text-xs font-medium text-[#e7dff0]">{rule.name}</p>
                                {alreadyEnabled && (
                                  <span className="text-[10px] font-semibold px-2 py-0.5 rounded-full bg-emerald-900/30 text-emerald-400 shrink-0">Already Enabled</span>
                                )}
                                {rule.severity && (
                                  <span className={`text-[10px] font-semibold px-2 py-0.5 rounded-full shrink-0 ${rule.severity === 'critical' ? 'bg-red-900/30 text-red-400' : 'bg-amber-900/30 text-amber-400'}`}>
                                    {rule.severity}
                                  </span>
                                )}
                              </div>
                              <p className="text-[10px] text-zinc-500 mt-0.5">{rule.description}</p>
                            </div>
                            {alreadyEnabled ? (
                              <span className="material-symbols-outlined text-emerald-400 text-base shrink-0">check_circle</span>
                            ) : (
                              <button
                                onClick={() => toggleRuleSel(rule.id)}
                                className={`relative w-9 h-5 rounded-full transition-colors shrink-0 ${isSelected ? 'bg-[#D53F8C]' : 'bg-[#4b4754]'}`}
                              >
                                <span className={`absolute top-[3px] w-[14px] h-[14px] rounded-full bg-white transition-transform ${isSelected ? 'left-0 translate-x-[19px]' : 'left-[3px]'}`} />
                              </button>
                            )}
                          </div>
                        )
                      })}
                    </div>
                  )}
                </>
              )}
            </div>

            {/* Footer Actions */}
            <div className="bg-[#37333f]/30 px-8 py-5 flex justify-between items-center">
              <button
                onClick={handleBack}
                disabled={step === 0}
                className="px-4 py-2 text-xs font-medium text-zinc-400 hover:text-white transition-all flex items-center gap-2 disabled:opacity-40 disabled:cursor-not-allowed"
              >
                <span className="material-symbols-outlined text-xs">arrow_back</span>
                Back
              </button>
              <div className="flex items-center gap-3">
                {step === 2 && (
                  <button
                    onClick={() => setStep(3)}
                    className="px-4 py-2 text-xs font-medium text-zinc-400 hover:text-[#ffb0cf] transition-colors"
                  >
                    Skip
                  </button>
                )}
                {step < 3 ? (
                  <button
                    onClick={handleNext}
                    className="bg-gradient-to-br from-[#ffb0cf] to-[#f256a3] px-6 py-2.5 rounded-sm text-xs font-semibold text-[#3d0023] hover:scale-[1.02] active:scale-95 transition-all flex items-center gap-2"
                  >
                    Next
                    <span className="material-symbols-outlined text-xs">arrow_forward</span>
                  </button>
                ) : (
                  <button
                    onClick={handleActivate}
                    disabled={activating}
                    className="bg-gradient-to-br from-[#ffb0cf] to-[#f256a3] px-6 py-2.5 rounded-sm text-xs font-semibold text-[#3d0023] hover:scale-[1.02] active:scale-95 transition-all flex items-center gap-2 disabled:opacity-70"
                  >
                    {activating
                      ? <><span className="material-symbols-outlined animate-spin text-xs">refresh</span> Activating...</>
                      : <><span className="material-symbols-outlined text-xs">check_circle</span> Activate Agent</>
                    }
                  </button>
                )}
              </div>
            </div>
          </div>
        </div>
      </main>
    </div>
  )
}
