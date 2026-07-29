import { useState, useEffect, useMemo } from 'react'
import { useNavigate } from 'react-router-dom'
import { useTranslation } from 'react-i18next'
import Sidebar from '../components/Sidebar'
import TopNav from '../components/TopNav'
import BlockPolicySelect from '../components/BlockPolicySelect'
import { api } from '../api/client'

const STEPS = [
  { key: 'selectAgent', icon: 'smart_toy' },
  { key: 'installPlugin', icon: 'extension' },
  { key: 'otelConfig', icon: 'monitoring' },
  { key: 'activateRules', icon: 'rule' },
]

const agentTypes = [
  {
    id: 'openclaw',
    key: 'openclaw',
    imgSrc: '/static/icons/openclaw-dark.svg',
    disabled: false,
  },
  {
    id: 'claude-code',
    key: 'claudeCode',
    imgSrc: '/static/icons/claude-seeklogo.svg',
    disabled: false,
  },
  {
    id: 'hermes',
    key: 'hermes',
    imgSrc: '/static/icons/nousresearch-dark.png',
    disabled: false,
  },
]

function CopyButton({ text }) {
  const { t } = useTranslation('onboarding')
  const [copied, setCopied] = useState(false)
  const handle = () => {
    navigator.clipboard.writeText(text)
    setCopied(true)
    setTimeout(() => setCopied(false), 2000)
  }
  return (
    <button onClick={handle} className="flex items-center gap-1.5 text-[10px] font-semibold uppercase text-primary hover:text-gray-100 transition-colors">
      <span className="material-symbols-outlined text-xs">{copied ? 'check' : 'content_copy'}</span>
      {copied ? t('wizard.copyButton.copied') : t('wizard.copyButton.copy')}
    </button>
  )
}

function CodeBlock({ label, code }) {
  return (
    <div className="bg-gray-800 rounded-sm overflow-hidden border border-outline-variant/10">
      <div className="bg-surface-container-highest/50 px-4 py-2 flex justify-between items-center">
        <span className="text-[10px] font-semibold text-gray-400 uppercase tracking-widest">{label}</span>
        <CopyButton text={code} />
      </div>
      <pre className="p-4 overflow-x-auto text-xs font-mono leading-relaxed text-gray-100 whitespace-pre-wrap break-all">{code}</pre>
    </div>
  )
}

function DownloadScriptButton() {
  const { t } = useTranslation('onboarding')
  const [downloading, setDownloading] = useState(false)
  const [dlError, setDlError] = useState(null)

  const handle = async () => {
    setDownloading(true)
    setDlError(null)
    try {
      const res = await api.getInstallScript()
      if (!res.ok) {
        throw new Error(t('wizard.downloadScript.errorStatus', { status: res.status }))
      }
      const blob = await res.blob()
      const url = URL.createObjectURL(blob)
      const a = document.createElement('a')
      a.href = url
      a.download = 'agentguard-install.sh'
      a.click()
      URL.revokeObjectURL(url)
    } catch (e) {
      setDlError(e.message || t('wizard.downloadScript.errorFallback'))
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
        {downloading ? t('wizard.downloadScript.downloading') : t('wizard.downloadScript.button')}
      </button>
      {dlError && (
        <p className="text-xs text-red-400">{dlError}</p>
      )}
    </>
  )
}

export default function Onboarding() {
  const { t } = useTranslation('onboarding')
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
      .catch(err => setCatalogError(err?.message || t('wizard.step3.loadCatalogFailed')))
      .finally(() => setCatalogLoading(false))
  }, [step, catalog.length, catalogLoading, t])

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
              console.warn('Install failed for', ruleId, e)  // eslint-disable-line no-console
              failures.push(t('wizard.step3.installFailedLine', { name: entry?.name || ruleId, message: e?.message ?? String(e) }))
              continue  // skip the policy step for a rule we couldn't install
            }
          }
          const policy = policies[ruleId]
          if (policy && policy !== 'log_only') {
            try {
              await api.setRuleBlockPolicy(ruleId, policy)
            } catch (e) {
              console.warn('Set policy failed for', ruleId, e)  // eslint-disable-line no-console
              failures.push(t('wizard.step3.setPolicyFailedLine', { name: entry?.name || ruleId, policy, message: e?.message ?? String(e) }))
            }
          }
        }
      } catch (e) {
        setActivateError(e?.message || t('wizard.step3.activationFailedFallback'))
        setActivating(false)
        return
      }
      setActivating(false)
      if (failures.length > 0) {
        // Surface partial failure inline rather than navigating away silently.
        const summary = t('wizard.step3.activationFailedSummary', { count: failures.length, total: ruleIds.length })
        setActivateError(`${summary}\n• ${failures.join('\n• ')}`)
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
              <h1 className="text-2xl font-bold tracking-tight text-gray-100 mb-2">{t('wizard.header.title')}</h1>
              {alreadyOnboarded && (
                <button
                  onClick={() => navigate('/dashboard')}
                  className="text-xs text-gray-400 hover:text-pink-400 transition-colors flex items-center gap-1.5 mb-2"
                >
                  <span className="material-symbols-outlined text-xs">arrow_back</span>
                  {t('wizard.header.backToDashboard')}
                </button>
              )}
              <p className="text-gray-400 text-xs leading-relaxed">
                {alreadyOnboarded
                  ? t('wizard.header.descAlready')
                  : t('wizard.header.descNew')}
              </p>
            </div>

            <div className="space-y-6 relative">
              <div className="absolute left-[11px] top-2 bottom-2 w-[2px] bg-surface-container-highest"></div>
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
                      <div className="h-6 w-6 rounded-full bg-surface-container-highest flex items-center justify-center z-10">
                        <div className="h-1.5 w-1.5 rounded-full bg-zinc-600"></div>
                      </div>
                    )}
                    <span className={`text-xs ${active ? 'font-semibold text-primary' : done ? 'text-gray-400' : 'text-gray-400'}`}>
                      {i + 1}. {t(`wizard.steps.${s.key}`)}
                    </span>
                  </div>
                )
              })}
            </div>

            {alreadyOnboarded && (
              <div className="flex items-start gap-3 p-4 bg-surface-container-low rounded-sm">
                <span className="material-symbols-outlined text-amber-400 text-sm mt-0.5">info</span>
                <p className="text-xs text-gray-400 leading-relaxed">{t('wizard.header.alreadyOnboardedNote')}</p>
              </div>
            )}
          </div>

          {/* Right: Step Content */}
          <div className="lg:col-span-7 bg-surface-container-high rounded-sm overflow-hidden flex flex-col min-h-[520px]">
            <div className="p-8 flex-1 space-y-6">

              {/* Step 0: Select Agent */}
              {step === 0 && (
                <>
                  <div>
                    <h2 className="text-xl font-semibold text-gray-100 tracking-tight">{t('wizard.step0.title')}</h2>
                    <p className="text-gray-400 mt-1.5 text-xs leading-relaxed">{t('wizard.step0.desc')}</p>
                  </div>
                  <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
                    {agentTypes.map(at => (
                      <button
                        key={at.id}
                        onClick={() => !at.disabled && setSelectedAgent(at.id)}
                        disabled={at.disabled}
                        className={`p-6 rounded-sm text-left flex flex-col gap-4 border-2 transition-all ${
                          at.disabled
                            ? 'border-outline-variant/10 bg-surface-container-low opacity-50 cursor-not-allowed'
                            : selectedAgent === at.id
                              ? 'border-[#ffb0cf] bg-[#ffb0cf]/5'
                              : 'border-outline-variant/20 bg-surface-container-low hover:border-outline-variant/40'
                        }`}
                      >
                        <div className="flex items-center justify-between">
                          <div className={`w-10 h-10 rounded-sm flex items-center justify-center ${!at.disabled && selectedAgent === at.id ? 'bg-[#ffb0cf]/10 text-primary' : 'bg-surface-container text-gray-400'}`}>
                            {at.imgSrc
                              ? <img src={at.imgSrc} alt={t(`wizard.agentTypes.${at.key}.name`)} className="h-6 w-6 object-contain" />
                              : <span className="material-symbols-outlined text-lg">{at.icon}</span>
                            }
                          </div>
                          <span className={`text-[10px] font-semibold px-2 py-0.5 rounded-full ${at.disabled ? 'bg-surface-container-highest text-gray-400' : selectedAgent === at.id ? 'bg-[#ffb0cf]/10 text-primary' : 'bg-surface-container-highest text-gray-400'}`}>
                            {t('wizard.agentTypes.badge')}
                          </span>
                        </div>
                        <div>
                          <h3 className="text-sm font-semibold text-gray-100 mb-1">{t(`wizard.agentTypes.${at.key}.name`)}</h3>
                          <p className="text-xs text-gray-400 leading-relaxed">{t(`wizard.agentTypes.${at.key}.desc`)}</p>
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
                    <h2 className="text-xl font-semibold text-gray-100 tracking-tight">{t('wizard.step1.openclaw.title')}</h2>
                    <p className="text-gray-400 mt-1.5 text-xs leading-relaxed">
                      {t('wizard.step1.openclaw.descBefore')}<code className="text-gray-100">openclaw.json</code>{t('wizard.step1.openclaw.descAfter')}
                    </p>
                  </div>

                  <div className="flex flex-col gap-3">
                    <DownloadScriptButton />
                    <CodeBlock label={t('wizard.step1.openclaw.runLabel')} code="bash agentguard-install.sh" />
                  </div>

                  <div className="space-y-3">
                    <p className="text-[10px] font-semibold text-gray-400 uppercase tracking-widest">{t('wizard.common.valuesToHaveReady')}</p>
                    <div className="grid grid-cols-1 gap-3">
                      {[
                        { label: t('wizard.common.fields.deploymentId'), val: deploymentId, set: setDeploymentId, ph: t('wizard.common.placeholders.deploymentIdOpenclaw') },
                        { label: t('wizard.common.fields.deploymentName'), val: deploymentName, set: setDeploymentName, ph: t('wizard.common.placeholders.deploymentNameOpenclaw') },
                        { label: t('wizard.common.fields.timeplusUrl'), val: timeplusUrl, set: setTimeplusUrl, ph: t('wizard.common.placeholders.timeplusUrl') },
                        { label: t('wizard.common.fields.timeplusUsername'), val: timeplusUsername, set: setTimeplusUsername, ph: t('wizard.common.placeholders.timeplusUsername') },
                        { label: t('wizard.common.fields.timeplusPassword'), val: timeplusPassword, set: setTimeplusPassword, ph: t('wizard.common.placeholders.timeplusPassword'), type: 'password' },
                        { label: t('wizard.common.fields.agentguardUrl'), val: agentguardUrl, set: setAgentguardUrl, ph: t('wizard.common.placeholders.agentguardUrl') },
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
                        <label className="text-[10px] font-medium text-gray-400 uppercase tracking-widest w-36 shrink-0">{t('wizard.common.holdsEnabledLabel')}</label>
                        <label className="flex items-center gap-2 cursor-pointer">
                          <input type="checkbox" checked={holdsEnabled} onChange={e => setHoldsEnabled(e.target.checked)} className="accent-[#ffb0cf]" />
                          <span className="text-[10px] text-gray-400">{holdsEnabled ? t('wizard.common.holdsOn') : t('wizard.common.holdsOff')}</span>
                        </label>
                      </div>
                      <div className="flex items-center gap-4">
                        <label className="text-[10px] font-medium text-gray-400 uppercase tracking-widest w-36 shrink-0">{t('wizard.common.holdFailPolicyLabel')}</label>
                        <select value={holdFailPolicy} onChange={e => setHoldFailPolicy(e.target.value)} disabled={!holdsEnabled}
                          className="flex-1 bg-gray-800 border border-gray-600 text-xs py-2 px-3 rounded-sm focus:border-gray-400 focus:outline-none text-gray-100 font-mono disabled:opacity-50">
                          <option value="deny">{t('wizard.common.holdFailPolicyOptions.deny')}</option>
                          <option value="allow">{t('wizard.common.holdFailPolicyOptions.allow')}</option>
                        </select>
                      </div>
                    </div>
                  </div>
                </>
              )}

              {step === 1 && selectedAgent === 'claude-code' && (
                <>
                  <div>
                    <h2 className="text-xl font-semibold text-gray-100 tracking-tight">{t('wizard.step1.claudeCode.title')}</h2>
                    <p className="text-gray-400 mt-1.5 text-xs leading-relaxed">
                      {t('wizard.step1.claudeCode.desc')}
                    </p>
                  </div>

                  <div className="flex flex-col gap-3">
                    <DownloadScriptButton />
                    <CodeBlock label={t('wizard.common.runLabel')} code="bash agentguard-install.sh" />
                  </div>

                  <div className="space-y-3">
                    <p className="text-[10px] font-semibold text-gray-400 uppercase tracking-widest">{t('wizard.common.valuesToHaveReady')}</p>
                    <div className="grid grid-cols-1 gap-3">
                      {[
                        { label: t('wizard.common.fields.agentId'), val: agentId, set: setAgentId, ph: t('wizard.common.placeholders.agentId') },
                        { label: t('wizard.common.fields.deploymentId'), val: deploymentId, set: setDeploymentId, ph: t('wizard.common.placeholders.deploymentIdLocal') },
                        { label: t('wizard.common.fields.deploymentName'), val: deploymentName, set: setDeploymentName, ph: t('wizard.common.placeholders.deploymentNameLocal') },
                        { label: t('wizard.common.fields.timeplusUrl'), val: timeplusUrl, set: setTimeplusUrl, ph: t('wizard.common.placeholders.timeplusUrl') },
                        { label: t('wizard.common.fields.timeplusUsername'), val: timeplusUsername, set: setTimeplusUsername, ph: t('wizard.common.placeholders.timeplusUsername') },
                        { label: t('wizard.common.fields.timeplusPassword'), val: timeplusPassword, set: setTimeplusPassword, ph: t('wizard.common.placeholders.timeplusPassword'), type: 'password' },
                        { label: t('wizard.common.fields.agentguardUrl'), val: agentguardUrl, set: setAgentguardUrl, ph: t('wizard.common.placeholders.agentguardUrl') },
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
                        <label className="text-[10px] font-medium text-gray-400 uppercase tracking-widest w-36 shrink-0">{t('wizard.common.holdsEnabledLabel')}</label>
                        <label className="flex items-center gap-2 cursor-pointer">
                          <input type="checkbox" checked={holdsEnabled} onChange={e => setHoldsEnabled(e.target.checked)} className="accent-[#ffb0cf]" />
                          <span className="text-[10px] text-gray-400">{holdsEnabled ? t('wizard.common.holdsOn') : t('wizard.common.holdsOff')}</span>
                        </label>
                      </div>
                      <div className="flex items-center gap-4">
                        <label className="text-[10px] font-medium text-gray-400 uppercase tracking-widest w-36 shrink-0">{t('wizard.common.holdFailPolicyLabel')}</label>
                        <select value={holdFailPolicy} onChange={e => setHoldFailPolicy(e.target.value)} disabled={!holdsEnabled}
                          className="flex-1 bg-gray-800 border border-gray-600 text-xs py-2 px-3 rounded-sm focus:border-gray-400 focus:outline-none text-gray-100 font-mono disabled:opacity-50">
                          <option value="deny">{t('wizard.common.holdFailPolicyOptions.deny')}</option>
                          <option value="allow">{t('wizard.common.holdFailPolicyOptions.allow')}</option>
                        </select>
                      </div>
                    </div>
                    <p className="text-[10px] text-gray-400 leading-relaxed">{t('wizard.step1.claudeCode.agentIdNote')}</p>
                  </div>
                </>
              )}

              {step === 1 && selectedAgent === 'hermes' && (
                <>
                  <div>
                    <h2 className="text-xl font-semibold text-gray-100 tracking-tight">{t('wizard.step1.hermes.title')}</h2>
                    <p className="text-gray-400 mt-1.5 text-xs leading-relaxed">
                      {t('wizard.step1.hermes.descBefore')}<code className="text-gray-100">agentguard-hermes install</code>{t('wizard.step1.hermes.descAfter')}
                    </p>
                  </div>

                  <div className="flex flex-col gap-3">
                    <DownloadScriptButton />
                    <CodeBlock label={t('wizard.common.runLabel')} code="bash agentguard-install.sh" />
                  </div>

                  <div className="space-y-3">
                    <p className="text-[10px] font-semibold text-gray-400 uppercase tracking-widest">{t('wizard.common.valuesToHaveReady')}</p>
                    <div className="grid grid-cols-1 gap-3">
                      {[
                        { label: t('wizard.common.fields.agentId'), val: agentId, set: setAgentId, ph: t('wizard.common.placeholders.agentId') },
                        { label: t('wizard.common.fields.deploymentId'), val: deploymentId, set: setDeploymentId, ph: t('wizard.common.placeholders.deploymentIdLocal') },
                        { label: t('wizard.common.fields.deploymentName'), val: deploymentName, set: setDeploymentName, ph: t('wizard.common.placeholders.deploymentNameLocal') },
                        { label: t('wizard.common.fields.timeplusUrl'), val: timeplusUrl, set: setTimeplusUrl, ph: t('wizard.common.placeholders.timeplusUrl') },
                        { label: t('wizard.common.fields.timeplusUsername'), val: timeplusUsername, set: setTimeplusUsername, ph: t('wizard.common.placeholders.timeplusUsername') },
                        { label: t('wizard.common.fields.timeplusPassword'), val: timeplusPassword, set: setTimeplusPassword, ph: t('wizard.common.placeholders.timeplusPassword'), type: 'password' },
                        { label: t('wizard.common.fields.agentguardUrl'), val: agentguardUrl, set: setAgentguardUrl, ph: t('wizard.common.placeholders.agentguardUrl') },
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
                        <label className="text-[10px] font-medium text-gray-400 uppercase tracking-widest w-36 shrink-0">{t('wizard.common.holdsEnabledLabel')}</label>
                        <label className="flex items-center gap-2 cursor-pointer">
                          <input type="checkbox" checked={holdsEnabled} onChange={e => setHoldsEnabled(e.target.checked)} className="accent-[#ffb0cf]" />
                          <span className="text-[10px] text-gray-400">{holdsEnabled ? t('wizard.common.holdsOnHermes') : t('wizard.common.holdsOff')}</span>
                        </label>
                      </div>
                      <div className="flex items-center gap-4">
                        <label className="text-[10px] font-medium text-gray-400 uppercase tracking-widest w-36 shrink-0">{t('wizard.common.holdFailPolicyLabel')}</label>
                        <select value={holdFailPolicy} onChange={e => setHoldFailPolicy(e.target.value)} disabled={!holdsEnabled}
                          className="flex-1 bg-gray-800 border border-gray-600 text-xs py-2 px-3 rounded-sm focus:border-gray-400 focus:outline-none text-gray-100 font-mono disabled:opacity-50">
                          <option value="deny">{t('wizard.common.holdFailPolicyOptions.deny')}</option>
                          <option value="allow">{t('wizard.common.holdFailPolicyOptions.allow')}</option>
                        </select>
                      </div>
                    </div>
                    <p className="text-[10px] text-gray-400 leading-relaxed">{t('wizard.step1.hermes.agentIdNote')}</p>
                  </div>
                </>
              )}

              {/* Step 2: OTel Config */}
              {step === 2 && selectedAgent === 'openclaw' && (
                <>
                  <div>
                    <h2 className="text-xl font-semibold text-gray-100 tracking-tight">{t('wizard.step2.openclaw.title')}</h2>
                    <p className="text-gray-400 mt-1.5 text-xs leading-relaxed">
                      {t('wizard.step2.openclaw.desc')}
                    </p>
                  </div>

                  <div className="space-y-3">
                    <p className="text-[10px] font-semibold text-gray-400 uppercase tracking-widest">{t('wizard.step2.openclaw.endpointHeading')}</p>
                    <div className="flex items-center gap-4">
                      <label className="text-[10px] font-medium text-gray-400 uppercase tracking-widest w-36 shrink-0">{t('wizard.step2.openclaw.endpointLabel')}</label>
                      <input
                        value={otelEndpoint}
                        onChange={e => setOtelEndpoint(e.target.value)}
                        placeholder="http://host.docker.internal:4318"
                        className="flex-1 bg-gray-800 border border-gray-600 text-xs py-2 px-3 rounded-sm focus:border-gray-400 focus:outline-none text-gray-100 font-mono"
                      />
                    </div>
                    <p className="text-[10px] text-gray-400 leading-relaxed pl-40">
                      {t('wizard.step2.openclaw.hintBefore')}<code className="text-gray-400">host.docker.internal</code>{t('wizard.step2.openclaw.hintAfter')}
                    </p>
                  </div>

                  <CodeBlock label={t('wizard.step2.openclaw.codeBlockLabel')} code={otelConfigCode} />
                </>
              )}

              {step === 2 && selectedAgent === 'claude-code' && (
                <>
                  <div>
                    <h2 className="text-xl font-semibold text-gray-100 tracking-tight">{t('wizard.step2.claudeCode.title')}</h2>
                    <p className="text-gray-400 mt-1.5 text-xs leading-relaxed">
                      {t('wizard.step2.claudeCode.desc')}
                    </p>
                  </div>

                  <div className="space-y-3">
                    <p className="text-[10px] font-semibold text-gray-400 uppercase tracking-widest">{t('wizard.step2.claudeCode.endpointHeading')}</p>
                    <div className="flex items-center gap-4">
                      <label className="text-[10px] font-medium text-gray-400 uppercase tracking-widest w-36 shrink-0">{t('wizard.step2.claudeCode.endpointLabel')}</label>
                      <input
                        value={otelEndpoint}
                        onChange={e => setOtelEndpoint(e.target.value)}
                        placeholder="http://localhost:4318"
                        className="flex-1 bg-gray-800 border border-gray-600 text-xs py-2 px-3 rounded-sm focus:border-gray-400 focus:outline-none text-gray-100 font-mono"
                      />
                    </div>
                  </div>

                  <CodeBlock label={t('wizard.step2.claudeCode.codeBlockLabel')} code={ccOtelSettingsCode} />
                  <p className="text-[10px] text-gray-400 leading-relaxed">
                    {t('wizard.step2.claudeCode.mergeNoteBefore')}<code className="text-gray-400">env</code>{t('wizard.step2.claudeCode.mergeNoteMiddle')}<code className="text-gray-400">~/.claude/settings.json</code>{t('wizard.step2.claudeCode.mergeNoteBetween')}<code className="text-gray-400">http/protobuf</code>{t('wizard.step2.claudeCode.mergeNoteAfter')}
                  </p>
                </>
              )}

              {step === 2 && selectedAgent === 'hermes' && (
                <>
                  <div>
                    <h2 className="text-xl font-semibold text-gray-100 tracking-tight">{t('wizard.step2.hermes.title')}</h2>
                    <p className="text-gray-400 mt-1.5 text-xs leading-relaxed">
                      {t('wizard.step2.hermes.desc')}
                    </p>
                  </div>
                  <div className="flex items-start gap-4 p-5 bg-surface-container-low rounded-sm border border-outline-variant/20">
                    <span className="material-symbols-outlined text-amber-400 text-xl mt-0.5 shrink-0">info</span>
                    <div className="space-y-1.5">
                      <p className="text-sm font-medium text-gray-100">{t('wizard.step2.hermes.notAvailableTitle')}</p>
                      <p className="text-xs text-gray-400 leading-relaxed">
                        {t('wizard.step2.hermes.notAvailableBodyBefore')}<code className="text-gray-100">post_api_request</code>{t('wizard.step2.hermes.notAvailableBodyAfter')}
                      </p>
                      <p className="text-xs text-gray-400 leading-relaxed mt-1">
                        {t('wizard.step2.hermes.notAvailableBody2')}
                      </p>
                    </div>
                  </div>
                  <p className="text-xs text-gray-400">{t('wizard.step2.hermes.nextHintBefore')}<span className="text-gray-100">{t('wizard.footer.next')}</span>{t('wizard.step2.hermes.nextHintAfter')}</p>
                </>
              )}

              {/* Step 3: Activate Rules */}
              {step === 3 && (
                <>
                  <div>
                    <h2 className="text-xl font-semibold text-gray-100 tracking-tight">{t('wizard.step3.title')}</h2>
                    <p className="text-gray-400 mt-1.5 text-xs leading-relaxed">
                      {t('wizard.step3.desc')}
                    </p>
                  </div>

                  {catalogLoading && (
                    <div className="text-xs text-gray-400">{t('wizard.step3.loading')}</div>
                  )}
                  {catalogError && (
                    <div className="bg-red-500/10 border border-red-500/20 rounded-sm px-4 py-3 text-xs text-red-400">{catalogError}</div>
                  )}

                  {!catalogLoading && !catalogError && (
                    <div className="flex flex-col gap-2 max-h-[420px] overflow-y-auto pr-2">
                      {catalog.length === 0 ? (
                        <p className="text-xs text-gray-400">{t('wizard.step3.noRules')}</p>
                      ) : catalog.map(entry => {
                        const isSelected = !!selectedRules[entry.id]
                        return (
                          <div
                            key={entry.id}
                            className={`flex items-center justify-between gap-4 p-3 rounded-sm border transition-colors ${
                              isSelected ? 'bg-surface-container-low border-[#ffb0cf]/30' : 'bg-surface-container-low border-outline-variant/20'
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
                                    <span className="ml-2 text-[9px] uppercase tracking-wide text-gray-400">{t('wizard.step3.alreadyEnabled')}</span>
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
                    {t('wizard.step3.footerNoteBefore')}<span className="text-gray-100 font-medium">{t('wizard.step3.footerNoteGoToRules')}</span>{t('wizard.step3.footerNoteAfter')}
                  </p>
                </>
              )}
            </div>

            {/* Footer Actions */}
            <div className="bg-surface-container-highest/30 px-8 py-5 flex justify-between items-center">
              <button
                onClick={handleBack}
                disabled={step === 0}
                className="px-4 py-2 text-xs font-medium text-gray-400 hover:text-gray-100 transition-all flex items-center gap-2 disabled:opacity-40 disabled:cursor-not-allowed"
              >
                <span className="material-symbols-outlined text-xs">arrow_back</span>
                {t('wizard.footer.back')}
              </button>
              <div className="flex items-center gap-3">
                {step < 3 ? (
                  <button
                    onClick={handleNext}
                    className="bg-primary-button px-6 py-2.5 rounded-sm text-xs font-semibold text-primary-button-text hover:scale-[1.02] active:scale-95 transition-all flex items-center gap-2"
                  >
                    {t('wizard.footer.next')}
                    <span className="material-symbols-outlined text-xs">arrow_forward</span>
                  </button>
                ) : (
                  <button
                    onClick={handleFinish}
                    disabled={activating}
                    className="bg-primary-button px-6 py-2.5 rounded-sm text-xs font-semibold text-primary-button-text hover:scale-[1.02] active:scale-95 transition-all flex items-center gap-2 disabled:opacity-50 disabled:cursor-not-allowed"
                  >
                    <span className="material-symbols-outlined text-xs">rule</span>
                    {activating ? t('wizard.footer.activating') : t('wizard.footer.goToRules')}
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
