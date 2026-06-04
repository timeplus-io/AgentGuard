import { useState, useEffect, useMemo } from 'react'
import { useNavigate } from 'react-router-dom'
import Sidebar from '../components/Sidebar'
import TopNav from '../components/TopNav'
import BlockPolicySelect from '../components/BlockPolicySelect'
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
    imgSrc: '/static/icons/openclaw-dark.svg',
    badge: 'Supported',
    disabled: false,
  },
  {
    id: 'claude-code',
    name: 'Claude Code',
    desc: "Anthropic's Claude Code CLI. Monitor tool use, LLM calls, cost, and security events in real time.",
    imgSrc: '/static/icons/claude-seeklogo.svg',
    badge: 'Supported',
    disabled: false,
  },
  {
    id: 'hermes',
    name: 'Hermes Agent',
    desc: 'NousResearch Hermes AI agent. Monitor sessions, LLM calls, and tool use via the AgentGuard Python plugin.',
    imgSrc: '/static/icons/nousresearch-dark.png',
    badge: 'Supported',
    disabled: false,
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
    <div className="bg-gray-800 rounded-sm overflow-hidden border border-[#574149]/10">
      <div className="bg-[#37333f]/50 px-4 py-2 flex justify-between items-center">
        <span className="text-[10px] font-semibold text-gray-400 uppercase tracking-widest">{label}</span>
        <CopyButton text={code} />
      </div>
      <pre className="p-4 overflow-x-auto text-xs font-mono leading-relaxed text-gray-100 whitespace-pre-wrap break-all">{code}</pre>
    </div>
  )
}

function DownloadScriptButton() {
  const [downloading, setDownloading] = useState(false)
  const [dlError, setDlError] = useState(null)

  const handle = async () => {
    setDownloading(true)
    setDlError(null)
    try {
      const res = await api.getInstallScript()
      if (!res.ok) {
        throw new Error(`Download failed (${res.status})`)
      }
      const blob = await res.blob()
      const url = URL.createObjectURL(blob)
      const a = document.createElement('a')
      a.href = url
      a.download = 'agentguard-install.sh'
      a.click()
      URL.revokeObjectURL(url)
    } catch (e) {
      setDlError(e.message || 'Download failed')
    } finally {
      setDownloading(false)
    }
  }

  return (
    <>
      <button
        onClick={handle}
        disabled={downloading}
        className="flex items-center gap-2 bg-primary-button px-5 py-2.5 rounded-sm text-xs font-semibold text-primary-button-text hover:scale-[1.02] active:scale-95 transition-all disabled:opacity-50 disabled:cursor-not-allowed"
      >
        <span className="material-symbols-outlined text-sm">download</span>
        {downloading ? 'Downloading…' : 'Download Install Script'}
      </button>
      {dlError && (
        <p className="text-xs text-red-400">{dlError}</p>
      )}
    </>
  )
}

export default function Onboarding() {
  const navigate = useNavigate()
  const alreadyOnboarded = localStorage.getItem('agentguard_onboarded') === '1'

  const [step, setStep] = useState(0)
  const [selectedAgent, setSelectedAgent] = useState('openclaw')

  // Step 1 config
  const [agentId, setAgentId] = useState('')
  const [deploymentId, setDeploymentId] = useState('my-team')
  const [deploymentName, setDeploymentName] = useState('My Team')
  const [timeplusUrl, setTimeplusUrl] = useState('http://localhost:3218')
  const [timeplusUsername, setTimeplusUsername] = useState('proton')
  const [timeplusPassword, setTimeplusPassword] = useState('')
  const [agentguardUrl, setAgentguardUrl] = useState('http://localhost:8080')
  const [holdsEnabled, setHoldsEnabled] = useState(true)
  const [holdFailPolicy, setHoldFailPolicy] = useState('deny')

  const hermesEnvCode =
    `# Async event ingest (Hermes → Timeplus)\n` +
    `export AGENTGUARD_TIMEPLUS_URL="${timeplusUrl}"\n` +
    `export AGENTGUARD_USERNAME="${timeplusUsername}"\n` +
    `export AGENTGUARD_PASSWORD="${timeplusPassword || '<your-password>'}"\n` +
    `\n` +
    `# Identity\n` +
    `export AGENTGUARD_AGENT_ID="${agentId || '<your-hostname>'}"\n` +
    `export AGENTGUARD_DEPLOYMENT_ID="${deploymentId}"\n` +
    `export AGENTGUARD_DEPLOYMENT_NAME="${deploymentName}"\n` +
    `\n` +
    `# Synchronous tool-call blocking (pre_tool_call → AgentGuard /api/holds)\n` +
    `export AGENTGUARD_URL="${agentguardUrl}"\n` +
    `export AGENTGUARD_HOLDS_ENABLED="${holdsEnabled}"\n` +
    `export AGENTGUARD_HOLDS_FAIL_POLICY="${holdFailPolicy}"`

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


  const jsonConfigCode = JSON.stringify({
    plugins: {
      load: {
        paths: [
          '/absolute/path/to/agentguard-plugin',
        ],
      },
      entries: {
        agentguard: {
          enabled: true,
          config: {
            timeplusUrl,
            username: timeplusUsername,
            password: timeplusPassword || '<your-password>',
            deploymentId,
            deploymentName,
            stream: 'agentguard_hook_events',
            flushMs: 2000,
            batchSize: 50,
            agentguardUrl,
            holdsEnabled,
            holdFailPolicy,
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

  // ── Claude Code computed snippets ──────────────────────────────────────────
  // PreToolUse runs synchronously (no async:true) so the plugin can pause
  // the tool call while AgentGuard evaluates rules. Other hooks stay async.
  const ccAsyncHookEntry = { matcher: '*', hooks: [{ type: 'command', command: 'agentguard-hook', async: true }] }
  const ccSyncHookEntry  = { matcher: '*', hooks: [{ type: 'command', command: 'agentguard-hook', timeout: 600000 }] }
  const ccSettingsJsonCode = JSON.stringify({
    hooks: {
      PreToolUse:         [ccSyncHookEntry],
      SessionStart:       [ccAsyncHookEntry],
      SessionEnd:         [ccAsyncHookEntry],
      UserPromptSubmit:   [ccAsyncHookEntry],
      PostToolUse:        [ccAsyncHookEntry],
      PostToolUseFailure: [ccAsyncHookEntry],
      SubagentStart:      [ccAsyncHookEntry],
      SubagentStop:       [ccAsyncHookEntry],
      Stop:               [ccAsyncHookEntry],
      PermissionDenied:   [ccAsyncHookEntry],
    },
    env: {
      AGENTGUARD_AGENT_ID:         agentId || '<your-hostname>',
      AGENTGUARD_DEPLOYMENT_ID:    deploymentId,
      AGENTGUARD_DEPLOYMENT_NAME:  deploymentName,
      AGENTGUARD_TIMEPLUS_URL:     timeplusUrl,
      AGENTGUARD_USERNAME:         timeplusUsername,
      AGENTGUARD_PASSWORD:         timeplusPassword || '<your-password>',
      AGENTGUARD_URL:              agentguardUrl,
      AGENTGUARD_HOLDS_ENABLED:    String(holdsEnabled),
      AGENTGUARD_HOLDS_FAIL_POLICY: holdFailPolicy,
    },
  }, null, 2)

  const ccOtelSettingsCode = JSON.stringify({
    env: {
      CLAUDE_CODE_ENABLE_TELEMETRY:        '1',
      CLAUDE_CODE_ENHANCED_TELEMETRY_BETA: '1',
      OTEL_METRICS_EXPORTER:               'otlp',
      OTEL_LOGS_EXPORTER:                  'otlp',
      OTEL_TRACES_EXPORTER:                'otlp',
      OTEL_EXPORTER_OTLP_PROTOCOL:         'http/protobuf',
      OTEL_EXPORTER_OTLP_ENDPOINT:         otelEndpoint,
      OTEL_METRIC_EXPORT_INTERVAL:         '10000',
      OTEL_LOGS_EXPORT_INTERVAL:           '5000',
      OTEL_LOG_TOOL_DETAILS:               '1',
    },
  }, null, 2)

  // Step 4 (Activate Rules) — catalog + per-rule policy selection
  const [catalog, setCatalog] = useState([])
  const [catalogLoading, setCatalogLoading] = useState(false)
  const [catalogError, setCatalogError] = useState(null)
  const [selectedRules, setSelectedRules] = useState({}) // { ruleId: true }
  const [policies, setPolicies] = useState({})           // { ruleId: 'log_only' | 'auto_block' | 'hold' }
  const [activating, setActivating] = useState(false)
  const [activateError, setActivateError] = useState(null)

  useEffect(() => {
    if (step !== 3 || catalog.length > 0 || catalogLoading) return
    setCatalogLoading(true)
    setCatalogError(null)
    api.getRulesCatalog()
      .then(data => setCatalog(data.catalog || []))
      .catch(err => setCatalogError(err?.message || 'Failed to load catalog'))
      .finally(() => setCatalogLoading(false))
  }, [step, catalog.length, catalogLoading])

  const handleNext = () => setStep(s => Math.min(3, s + 1))
  const handleBack = () => setStep(s => Math.max(0, s - 1))

  const handleFinish = async () => {
    // If any rules are selected on Step 4, install them with their chosen policies.
    const ruleIds = Object.entries(selectedRules).filter(([, v]) => v).map(([k]) => k)
    if (ruleIds.length > 0) {
      setActivating(true)
      setActivateError(null)
      const failures = []
      try {
        for (const ruleId of ruleIds) {
          const entry = catalog.find(c => c.id === ruleId)
          // Install only if not already installed; record install failures so the
          // user is told which rules didn't activate instead of being silently skipped.
          if (entry && !entry.installed) {
            try {
              await api.installRule(ruleId)
            } catch (e) {
              console.warn('Install failed for', ruleId, e)
              failures.push(`${entry?.name || ruleId}: install failed (${e?.message ?? String(e)})`)
              continue  // skip the policy step for a rule we couldn't install
            }
          }
          const policy = policies[ruleId]
          if (policy && policy !== 'log_only') {
            try {
              await api.setRuleBlockPolicy(ruleId, policy)
            } catch (e) {
              console.warn('Set policy failed for', ruleId, e)
              failures.push(`${entry?.name || ruleId}: set ${policy} policy failed (${e?.message ?? String(e)})`)
            }
          }
        }
      } catch (e) {
        setActivateError(e?.message || 'Activation failed')
        setActivating(false)
        return
      }
      setActivating(false)
      if (failures.length > 0) {
        // Surface partial failure inline rather than navigating away silently.
        setActivateError(`${failures.length} of ${ruleIds.length} rule activation(s) failed:\n• ${failures.join('\n• ')}`)
        return
      }
    }
    localStorage.setItem('agentguard_onboarded', '1')
    navigate('/rules')
  }

  return (
    <div className="min-h-screen bg-gray-900 text-gray-100">
      <Sidebar />
      <TopNav />

      <main className="ml-16 xl:ml-64 pt-16 pb-8 min-h-screen px-6 md:px-12 pt-[120px]">
        <div className="w-full max-w-7xl grid grid-cols-1 lg:grid-cols-12 gap-6 lg:gap-12 items-start mr-auto">
          {/* Left: Stepper */}
          <div className="lg:col-span-5 space-y-10">
            <div>
              <h1 className="text-2xl font-bold tracking-tight text-white mb-2">Connect Your Agent</h1>
              {alreadyOnboarded && (
                <button
                  onClick={() => navigate('/dashboard')}
                  className="text-xs text-gray-400 hover:text-pink-400 transition-colors flex items-center gap-1.5 mb-2"
                >
                  <span className="material-symbols-outlined text-xs">arrow_back</span>
                  Back to Dashboard
                </button>
              )}
              <p className="text-gray-400 text-xs leading-relaxed">
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
                    <span className={`text-xs ${active ? 'font-semibold text-[#ffb0cf]' : done ? 'text-gray-400' : 'text-gray-400'}`}>
                      {i + 1}. {s.label}
                    </span>
                  </div>
                )
              })}
            </div>

            {alreadyOnboarded && (
              <div className="flex items-start gap-3 p-4 bg-[#1d1a25] rounded-sm">
                <span className="material-symbols-outlined text-amber-400 text-sm mt-0.5">info</span>
                <p className="text-xs text-gray-400 leading-relaxed">You've completed onboarding before. Rules already enabled will be shown with an "Already Enabled" badge.</p>
              </div>
            )}
          </div>

          {/* Right: Step Content */}
          <div className="lg:col-span-7 bg-[#2c2834] rounded-sm overflow-hidden flex flex-col min-h-[520px]">
            <div className="p-8 flex-1 space-y-6">

              {/* Step 0: Select Agent */}
              {step === 0 && (
                <>
                  <div>
                    <h2 className="text-xl font-semibold text-white tracking-tight">Select Agent Type</h2>
                    <p className="text-gray-400 mt-1.5 text-xs leading-relaxed">Choose the AI agent framework you want to protect with AgentGuard.</p>
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
                          <div className={`w-10 h-10 rounded-sm flex items-center justify-center ${!at.disabled && selectedAgent === at.id ? 'bg-[#ffb0cf]/10 text-[#ffb0cf]' : 'bg-[#211e29] text-gray-400'}`}>
                            {at.imgSrc
                              ? <img src={at.imgSrc} alt={at.name} className="h-6 w-6 object-contain" />
                              : <span className="material-symbols-outlined text-lg">{at.icon}</span>
                            }
                          </div>
                          <span className={`text-[10px] font-semibold px-2 py-0.5 rounded-full ${at.disabled ? 'bg-[#37333f] text-gray-400' : selectedAgent === at.id ? 'bg-[#ffb0cf]/10 text-[#ffb0cf]' : 'bg-[#37333f] text-gray-400'}`}>
                            {at.badge}
                          </span>
                        </div>
                        <div>
                          <h3 className="text-sm font-semibold text-white mb-1">{at.name}</h3>
                          <p className="text-xs text-gray-400 leading-relaxed">{at.desc}</p>
                        </div>
                      </button>
                    ))}
                  </div>
                </>
              )}

              {/* Step 1: Install Plugin */}
              {step === 1 && selectedAgent === 'openclaw' && (
                <>
                  <div>
                    <h2 className="text-xl font-semibold text-white tracking-tight">Install Plugin</h2>
                    <p className="text-gray-400 mt-1.5 text-xs leading-relaxed">
                      Download and run the interactive install script from your OpenClaw project directory. It will prompt for the values below and patch your <code className="text-gray-100">openclaw.json</code> automatically.
                    </p>
                  </div>

                  <div className="flex flex-col gap-3">
                    <DownloadScriptButton />
                    <CodeBlock label="Then run from your OpenClaw project directory" code="bash agentguard-install.sh" />
                  </div>

                  <div className="space-y-3">
                    <p className="text-[10px] font-semibold text-gray-400 uppercase tracking-widest">Values to have ready</p>
                    <div className="grid grid-cols-1 gap-3">
                      {[
                        { label: 'Deployment ID', val: deploymentId, set: setDeploymentId, ph: 'my-team' },
                        { label: 'Deployment Name', val: deploymentName, set: setDeploymentName, ph: 'My Team' },
                        { label: 'Timeplus URL', val: timeplusUrl, set: setTimeplusUrl, ph: 'http://localhost:3218' },
                        { label: 'Timeplus Username', val: timeplusUsername, set: setTimeplusUsername, ph: 'proton' },
                        { label: 'Timeplus Password', val: timeplusPassword, set: setTimeplusPassword, ph: 'your-password', type: 'password' },
                        { label: 'AgentGuard URL', val: agentguardUrl, set: setAgentguardUrl, ph: 'http://localhost:8080' },
                      ].map(({ label, val, set, ph, type }) => (
                        <div key={label} className="flex items-center gap-4">
                          <label className="text-[10px] font-medium text-gray-400 uppercase tracking-widest w-36 shrink-0">{label}</label>
                          <input
                            type={type || 'text'}
                            value={val}
                            onChange={e => set(e.target.value)}
                            placeholder={ph}
                            className="flex-1 bg-gray-800 border border-gray-600 text-xs py-2 px-3 rounded-sm focus:border-gray-400 focus:outline-none text-gray-100 font-mono"
                          />
                        </div>
                      ))}
                      <div className="flex items-center gap-4">
                        <label className="text-[10px] font-medium text-gray-400 uppercase tracking-widest w-36 shrink-0">Holds Enabled</label>
                        <label className="flex items-center gap-2 cursor-pointer">
                          <input type="checkbox" checked={holdsEnabled} onChange={e => setHoldsEnabled(e.target.checked)} className="accent-[#ffb0cf]" />
                          <span className="text-[10px] text-gray-400">{holdsEnabled ? 'Synchronous PreToolUse blocking on' : 'Observation-only — no blocking'}</span>
                        </label>
                      </div>
                      <div className="flex items-center gap-4">
                        <label className="text-[10px] font-medium text-gray-400 uppercase tracking-widest w-36 shrink-0">Hold Fail Policy</label>
                        <select value={holdFailPolicy} onChange={e => setHoldFailPolicy(e.target.value)} disabled={!holdsEnabled}
                          className="flex-1 bg-gray-800 border border-gray-600 text-xs py-2 px-3 rounded-sm focus:border-gray-400 focus:outline-none text-gray-100 font-mono disabled:opacity-50">
                          <option value="deny">deny — block on errors</option>
                          <option value="allow">allow — pass through on errors</option>
                        </select>
                      </div>
                    </div>
                  </div>
                </>
              )}

              {step === 1 && selectedAgent === 'claude-code' && (
                <>
                  <div>
                    <h2 className="text-xl font-semibold text-white tracking-tight">Install Plugin</h2>
                    <p className="text-gray-400 mt-1.5 text-xs leading-relaxed">
                      Download and run the interactive install script. It will prompt you for the values below and configure everything automatically.
                    </p>
                  </div>

                  <div className="flex flex-col gap-3">
                    <DownloadScriptButton />
                    <CodeBlock label="Then run" code="bash agentguard-install.sh" />
                  </div>

                  <div className="space-y-3">
                    <p className="text-[10px] font-semibold text-gray-400 uppercase tracking-widest">Values to have ready</p>
                    <div className="grid grid-cols-1 gap-3">
                      {[
                        { label: 'Agent ID *', val: agentId, set: setAgentId, ph: 'your-hostname' },
                        { label: 'Deployment ID', val: deploymentId, set: setDeploymentId, ph: 'local' },
                        { label: 'Deployment Name', val: deploymentName, set: setDeploymentName, ph: 'Local Dev' },
                        { label: 'Timeplus URL', val: timeplusUrl, set: setTimeplusUrl, ph: 'http://localhost:3218' },
                        { label: 'Timeplus Username', val: timeplusUsername, set: setTimeplusUsername, ph: 'proton' },
                        { label: 'Timeplus Password', val: timeplusPassword, set: setTimeplusPassword, ph: 'your-password', type: 'password' },
                        { label: 'AgentGuard URL', val: agentguardUrl, set: setAgentguardUrl, ph: 'http://localhost:8080' },
                      ].map(({ label, val, set, ph, type }) => (
                        <div key={label} className="flex items-center gap-4">
                          <label className="text-[10px] font-medium text-gray-400 uppercase tracking-widest w-36 shrink-0">{label}</label>
                          <input
                            type={type || 'text'}
                            value={val}
                            onChange={e => set(e.target.value)}
                            placeholder={ph}
                            className="flex-1 bg-gray-800 border border-gray-600 text-xs py-2 px-3 rounded-sm focus:border-gray-400 focus:outline-none text-gray-100 font-mono"
                          />
                        </div>
                      ))}
                      <div className="flex items-center gap-4">
                        <label className="text-[10px] font-medium text-gray-400 uppercase tracking-widest w-36 shrink-0">Holds Enabled</label>
                        <label className="flex items-center gap-2 cursor-pointer">
                          <input type="checkbox" checked={holdsEnabled} onChange={e => setHoldsEnabled(e.target.checked)} className="accent-[#ffb0cf]" />
                          <span className="text-[10px] text-gray-400">{holdsEnabled ? 'Synchronous PreToolUse blocking on' : 'Observation-only — no blocking'}</span>
                        </label>
                      </div>
                      <div className="flex items-center gap-4">
                        <label className="text-[10px] font-medium text-gray-400 uppercase tracking-widest w-36 shrink-0">Hold Fail Policy</label>
                        <select value={holdFailPolicy} onChange={e => setHoldFailPolicy(e.target.value)} disabled={!holdsEnabled}
                          className="flex-1 bg-gray-800 border border-gray-600 text-xs py-2 px-3 rounded-sm focus:border-gray-400 focus:outline-none text-gray-100 font-mono disabled:opacity-50">
                          <option value="deny">deny — block on errors</option>
                          <option value="allow">allow — pass through on errors</option>
                        </select>
                      </div>
                    </div>
                    <p className="text-[10px] text-gray-400 leading-relaxed">* Agent ID identifies this Claude Code instance — use your hostname or machine name.</p>
                  </div>
                </>
              )}

              {step === 1 && selectedAgent === 'hermes' && (
                <>
                  <div>
                    <h2 className="text-xl font-semibold text-white tracking-tight">Install Plugin</h2>
                    <p className="text-gray-400 mt-1.5 text-xs leading-relaxed">
                      Download and run the interactive install script. It will pip-install the plugin, run <code className="text-gray-100">agentguard-hermes install</code>, and offer to append the required env vars to your shell profile.
                    </p>
                  </div>

                  <div className="flex flex-col gap-3">
                    <DownloadScriptButton />
                    <CodeBlock label="Then run" code="bash agentguard-install.sh" />
                  </div>

                  <div className="space-y-3">
                    <p className="text-[10px] font-semibold text-gray-400 uppercase tracking-widest">Values to have ready</p>
                    <div className="grid grid-cols-1 gap-3">
                      {[
                        { label: 'Agent ID *', val: agentId, set: setAgentId, ph: 'your-hostname' },
                        { label: 'Deployment ID', val: deploymentId, set: setDeploymentId, ph: 'local' },
                        { label: 'Deployment Name', val: deploymentName, set: setDeploymentName, ph: 'Local Dev' },
                        { label: 'Timeplus URL', val: timeplusUrl, set: setTimeplusUrl, ph: 'http://localhost:3218' },
                        { label: 'Timeplus Username', val: timeplusUsername, set: setTimeplusUsername, ph: 'proton' },
                        { label: 'Timeplus Password', val: timeplusPassword, set: setTimeplusPassword, ph: 'your-password', type: 'password' },
                        { label: 'AgentGuard URL', val: agentguardUrl, set: setAgentguardUrl, ph: 'http://localhost:8080' },
                      ].map(({ label, val, set, ph, type }) => (
                        <div key={label} className="flex items-center gap-4">
                          <label className="text-[10px] font-medium text-gray-400 uppercase tracking-widest w-36 shrink-0">{label}</label>
                          <input
                            type={type || 'text'}
                            value={val}
                            onChange={e => set(e.target.value)}
                            placeholder={ph}
                            className="flex-1 bg-gray-800 border border-gray-600 text-xs py-2 px-3 rounded-sm focus:border-gray-400 focus:outline-none text-gray-100 font-mono"
                          />
                        </div>
                      ))}
                      <div className="flex items-center gap-4">
                        <label className="text-[10px] font-medium text-gray-400 uppercase tracking-widest w-36 shrink-0">Holds Enabled</label>
                        <label className="flex items-center gap-2 cursor-pointer">
                          <input type="checkbox" checked={holdsEnabled} onChange={e => setHoldsEnabled(e.target.checked)} className="accent-[#ffb0cf]" />
                          <span className="text-[10px] text-gray-400">{holdsEnabled ? 'Synchronous pre_tool_call blocking on' : 'Observation-only — no blocking'}</span>
                        </label>
                      </div>
                      <div className="flex items-center gap-4">
                        <label className="text-[10px] font-medium text-gray-400 uppercase tracking-widest w-36 shrink-0">Hold Fail Policy</label>
                        <select value={holdFailPolicy} onChange={e => setHoldFailPolicy(e.target.value)} disabled={!holdsEnabled}
                          className="flex-1 bg-gray-800 border border-gray-600 text-xs py-2 px-3 rounded-sm focus:border-gray-400 focus:outline-none text-gray-100 font-mono disabled:opacity-50">
                          <option value="deny">deny — block on errors</option>
                          <option value="allow">allow — pass through on errors</option>
                        </select>
                      </div>
                    </div>
                    <p className="text-[10px] text-gray-400 leading-relaxed">* Agent ID identifies this Hermes instance — use your hostname or machine name.</p>
                  </div>
                </>
              )}

              {/* Step 2: OTel Config */}
              {step === 2 && selectedAgent === 'openclaw' && (
                <>
                  <div>
                    <h2 className="text-xl font-semibold text-white tracking-tight">OpenTelemetry Config</h2>
                    <p className="text-gray-400 mt-1.5 text-xs leading-relaxed">
                      Enable the built-in OTel diagnostics plugin to export traces, metrics, and logs from OpenClaw to Timeplus.
                    </p>
                  </div>

                  <div className="space-y-3">
                    <p className="text-[10px] font-semibold text-gray-400 uppercase tracking-widest">Timeplus OTLP Endpoint</p>
                    <div className="flex items-center gap-4">
                      <label className="text-[10px] font-medium text-gray-400 uppercase tracking-widest w-36 shrink-0">Endpoint URL</label>
                      <input
                        value={otelEndpoint}
                        onChange={e => setOtelEndpoint(e.target.value)}
                        placeholder="http://host.docker.internal:4318"
                        className="flex-1 bg-gray-800 border border-gray-600 text-xs py-2 px-3 rounded-sm focus:border-gray-400 focus:outline-none text-gray-100 font-mono"
                      />
                    </div>
                    <p className="text-[10px] text-gray-400 leading-relaxed pl-40">
                      Use <code className="text-gray-400">host.docker.internal</code> when OpenClaw runs in Docker and Timeplus runs on the host. Replace with the actual host/IP if needed.
                    </p>
                  </div>

                  <CodeBlock label="openclaw.json — diagnostics section" code={otelConfigCode} />
                </>
              )}

              {step === 2 && selectedAgent === 'claude-code' && (
                <>
                  <div>
                    <h2 className="text-xl font-semibold text-white tracking-tight">OpenTelemetry Config</h2>
                    <p className="text-gray-400 mt-1.5 text-xs leading-relaxed">
                      Enable Claude Code's built-in OTel telemetry to export LLM metrics and logs to Timeplus. This unlocks token usage, cost, and latency data.
                    </p>
                  </div>

                  <div className="space-y-3">
                    <p className="text-[10px] font-semibold text-gray-400 uppercase tracking-widest">Timeplus OTLP Endpoint</p>
                    <div className="flex items-center gap-4">
                      <label className="text-[10px] font-medium text-gray-400 uppercase tracking-widest w-36 shrink-0">Endpoint URL</label>
                      <input
                        value={otelEndpoint}
                        onChange={e => setOtelEndpoint(e.target.value)}
                        placeholder="http://localhost:4318"
                        className="flex-1 bg-gray-800 border border-gray-600 text-xs py-2 px-3 rounded-sm focus:border-gray-400 focus:outline-none text-gray-100 font-mono"
                      />
                    </div>
                  </div>

                  <CodeBlock label="~/.claude/settings.json — merge this env block" code={ccOtelSettingsCode} />
                  <p className="text-[10px] text-gray-400 leading-relaxed">
                    Merge the <code className="text-gray-400">env</code> keys into your existing <code className="text-gray-400">~/.claude/settings.json</code>. The <code className="text-gray-400">http/protobuf</code> protocol is required — do not use gRPC.
                  </p>
                </>
              )}

              {step === 2 && selectedAgent === 'hermes' && (
                <>
                  <div>
                    <h2 className="text-xl font-semibold text-white tracking-tight">OpenTelemetry Config</h2>
                    <p className="text-gray-400 mt-1.5 text-xs leading-relaxed">
                      OTel telemetry export is not yet supported by Hermes Agent.
                    </p>
                  </div>
                  <div className="flex items-start gap-4 p-5 bg-[#1d1a25] rounded-sm border border-[#574149]/20">
                    <span className="material-symbols-outlined text-amber-400 text-xl mt-0.5 shrink-0">info</span>
                    <div className="space-y-1.5">
                      <p className="text-sm font-medium text-gray-100">OTel not yet available for Hermes</p>
                      <p className="text-xs text-gray-400 leading-relaxed">
                        Hermes Agent does not yet expose an OpenTelemetry exporter. Token usage and latency metrics are captured
                        via the AgentGuard plugin's <code className="text-gray-100">post_api_request</code> hook instead —
                        no additional configuration is required.
                      </p>
                      <p className="text-xs text-gray-400 leading-relaxed mt-1">
                        OTel support will be added here once Hermes ships an OTLP exporter.
                      </p>
                    </div>
                  </div>
                  <p className="text-xs text-gray-400">Click <span className="text-gray-100">Next</span> to proceed to rule activation.</p>
                </>
              )}

              {/* Step 3: Activate Rules */}
              {step === 3 && (
                <>
                  <div>
                    <h2 className="text-xl font-semibold text-white tracking-tight">Activate Security Rules</h2>
                    <p className="text-gray-400 mt-1.5 text-xs leading-relaxed">
                      Select the rules to enable now and pick a block policy for each. You can refine these any time from the Rules page.
                    </p>
                  </div>

                  {catalogLoading && (
                    <div className="text-xs text-gray-400">Loading rules…</div>
                  )}
                  {catalogError && (
                    <div className="bg-red-500/10 border border-red-500/20 rounded-sm px-4 py-3 text-xs text-red-400">{catalogError}</div>
                  )}

                  {!catalogLoading && !catalogError && (
                    <div className="flex flex-col gap-2 max-h-[420px] overflow-y-auto pr-2">
                      {catalog.length === 0 ? (
                        <p className="text-xs text-gray-400">No rules available in the catalog.</p>
                      ) : catalog.map(entry => {
                        const isSelected = !!selectedRules[entry.id]
                        return (
                          <div
                            key={entry.id}
                            className={`flex items-center justify-between gap-4 p-3 rounded-sm border transition-colors ${
                              isSelected ? 'bg-[#1d1a25] border-[#ffb0cf]/30' : 'bg-[#1d1a25] border-[#574149]/20'
                            }`}
                          >
                            <div className="flex items-start gap-3 min-w-0">
                              <input
                                type="checkbox"
                                checked={isSelected || entry.installed}
                                disabled={entry.installed}
                                onChange={e => setSelectedRules(prev => ({ ...prev, [entry.id]: e.target.checked }))}
                                className="mt-1 accent-[#ffb0cf] shrink-0"
                              />
                              <div className="min-w-0">
                                <p className="text-xs font-semibold text-gray-100 truncate">
                                  {entry.name}
                                  {entry.installed && (
                                    <span className="ml-2 text-[9px] uppercase tracking-wide text-gray-400">Already enabled</span>
                                  )}
                                </p>
                                <p className="text-[11px] text-gray-400 leading-relaxed line-clamp-2">{entry.description}</p>
                              </div>
                            </div>
                            <div className="shrink-0">
                              <BlockPolicySelect
                                value={policies[entry.id] ?? 'log_only'}
                                disabled={entry.installed || !isSelected}
                                onChange={(p) => setPolicies(prev => ({ ...prev, [entry.id]: p }))}
                              />
                            </div>
                          </div>
                        )
                      })}
                    </div>
                  )}

                  {activateError && (
                    <div className="bg-red-500/10 border border-red-500/20 rounded-sm px-4 py-3 text-xs text-red-400 whitespace-pre-line">{activateError}</div>
                  )}

                  <p className="text-xs text-gray-400 leading-relaxed">
                    Selected rules will be installed when you click <span className="text-gray-100 font-medium">Go to Rules</span>. You can browse the full catalog and adjust policies on the Rules page.
                  </p>
                </>
              )}
            </div>

            {/* Footer Actions */}
            <div className="bg-[#37333f]/30 px-8 py-5 flex justify-between items-center">
              <button
                onClick={handleBack}
                disabled={step === 0}
                className="px-4 py-2 text-xs font-medium text-gray-400 hover:text-white transition-all flex items-center gap-2 disabled:opacity-40 disabled:cursor-not-allowed"
              >
                <span className="material-symbols-outlined text-xs">arrow_back</span>
                Back
              </button>
              <div className="flex items-center gap-3">
                {step < 3 ? (
                  <button
                    onClick={handleNext}
                    className="bg-primary-button px-6 py-2.5 rounded-sm text-xs font-semibold text-primary-button-text hover:scale-[1.02] active:scale-95 transition-all flex items-center gap-2"
                  >
                    Next
                    <span className="material-symbols-outlined text-xs">arrow_forward</span>
                  </button>
                ) : (
                  <button
                    onClick={handleFinish}
                    disabled={activating}
                    className="bg-primary-button px-6 py-2.5 rounded-sm text-xs font-semibold text-primary-button-text hover:scale-[1.02] active:scale-95 transition-all flex items-center gap-2 disabled:opacity-50 disabled:cursor-not-allowed"
                  >
                    <span className="material-symbols-outlined text-xs">rule</span>
                    {activating ? 'Activating…' : 'Go to Rules'}
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
