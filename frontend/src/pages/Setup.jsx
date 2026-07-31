import { useState, useRef } from 'react'
import { useTranslation } from 'react-i18next'
import { useAuth } from '../context/AuthContext'
import { api } from '../api/client'

const STEPS = [
  { key: 'connection', icon: 'cable' },
  { key: 'credentials', icon: 'key' },
  { key: 'provision', icon: 'build' },
]

export default function Setup() {
  const { t } = useTranslation('onboarding')
  const { setAuth } = useAuth()
  const [step, setStep] = useState(0)

  // Step 0 — connection
  const [host, setHost] = useState('localhost')
  const [port, setPort] = useState(3218)
  const [nativePort, setNativePort] = useState(8463)

  // Step 1 — credentials
  const [username, setUsername] = useState('default')
  const [password, setPassword] = useState('')
  const [testResult, setTestResult] = useState(null) // null | {ok, error}
  const [testing, setTesting] = useState(false)

  // Step 2 — provision
  const [log, setLog] = useState([])
  const [provisioning, setProvisioning] = useState(false)
  const [done, setDone] = useState(false)
  const [provisionError, setProvisionError] = useState(null)
  const logEndRef = useRef(null)

  const handleTestConnection = async () => {
    setTesting(true)
    setTestResult(null)
    try {
      await api.setupTestConnection(host, port, username, password)
      setTestResult({ ok: true })
    } catch (err) {
      setTestResult({ ok: false, error: err.message })
    } finally {
      setTesting(false)
    }
  }

  const handleProvision = async () => {
    setProvisioning(true)
    setLog([])
    setProvisionError(null)
    try {
      for await (const event of api.setupProvision({ host, port, native_port: nativePort, username, password })) {
        setLog(prev => [...prev, event])
        logEndRef.current?.scrollIntoView({ behavior: 'smooth' })
        if (event.status === 'done') {
          // Provision succeeded — log the user in automatically.
          try {
            const data = await api.authLogin(username, password)
            localStorage.setItem('agentguard_username', data.username)
            setAuth(prev => ({ ...prev, setupComplete: true, loggedIn: true, username: data.username }))
            setDone(true)
          } catch (err) {
            console.error('setup: auto-login after provision failed:', err)
            setProvisionError(t('setup.provision.autoLoginFailed', { message: err?.message ?? String(err) }))
          }
        }
        if (event.status === 'error') {
          setProvisionError(event.msg)
        }
      }
    } catch (err) {
      setProvisionError(err.message)
    } finally {
      setProvisioning(false)
    }
  }

  return (
    <div className="min-h-screen bg-surface text-gray-100 flex flex-col">
      <nav className="bg-surface shadow-[0_0_12px_rgba(213,63,140,0.12)] sticky top-0 z-50 h-16 w-full flex items-center px-6 backdrop-blur-md">
        <span className="text-lg font-semibold bg-clip-text text-transparent bg-gradient-to-br from-[#ffb0cf] to-[#f256a3] tracking-tight">AgentGuard</span>
        <span className="ml-3 text-[10px] font-semibold text-gray-400 uppercase tracking-widest">{t('setup.nav.badge')}</span>
      </nav>

      <main className="flex-1 flex items-start justify-center p-6 md:p-12">
        <div className="w-full max-w-2xl space-y-8">
          <div>
            <h1 className="text-2xl font-bold text-gray-100 tracking-tight">{t('setup.header.title')}</h1>
            <p className="text-gray-400 text-xs mt-1 leading-relaxed">{t('setup.header.subtitle')}</p>
          </div>

          {/* Step indicator */}
          <div className="flex items-center gap-0">
            {STEPS.map((s, i) => (
              <div key={i} className="flex items-center">
                <div className={`flex items-center gap-2 px-4 py-2 rounded-sm text-xs font-semibold ${i === step ? 'bg-[#ffb0cf]/10 text-primary' : i < step ? 'text-gray-400' : 'text-gray-400'}`}>
                  <span className={`w-5 h-5 rounded-full flex items-center justify-center text-[10px] font-bold ${i < step ? 'bg-[#f256a3] text-[#570033]' : i === step ? 'bg-[#ffb0cf]/20 text-primary' : 'bg-surface-container-highest text-gray-400'}`}>
                    {i < step ? '✓' : i + 1}
                  </span>
                  {t(`setup.steps.${s.key}`)}
                </div>
                {i < STEPS.length - 1 && <div className="h-px w-8 bg-surface-container-highest" />}
              </div>
            ))}
          </div>

          <div className="bg-surface-container-high rounded-sm overflow-hidden">
            <div className="p-8 space-y-6">

              {/* Step 0: Connection */}
              {step === 0 && (
                <>
                  <div>
                    <h2 className="text-base font-semibold text-gray-100">{t('setup.connection.title')}</h2>
                    <p className="text-gray-400 text-xs mt-1">{t('setup.connection.desc')}</p>
                  </div>
                  <div className="space-y-3">
                    {[
                      { label: t('setup.connection.fields.host'), val: host, set: setHost, ph: t('setup.connection.placeholders.host'), type: 'text' },
                      { label: t('setup.connection.fields.httpPort'), val: port, set: v => setPort(Number(v)), ph: t('setup.connection.placeholders.httpPort'), type: 'number' },
                      { label: t('setup.connection.fields.nativePort'), val: nativePort, set: v => setNativePort(Number(v)), ph: t('setup.connection.placeholders.nativePort'), type: 'number' },
                    ].map(({ label, val, set, ph, type }) => (
                      <div key={label} className="flex items-center gap-4">
                        <label className="text-[10px] font-medium text-gray-400 uppercase tracking-widest w-36 shrink-0">{label}</label>
                        <input
                          type={type}
                          value={val}
                          onChange={e => { set(e.target.value); setTestResult(null) }}
                          placeholder={ph}
                          className="flex-1 bg-gray-800 border border-gray-600 text-xs py-2 px-3 rounded-sm focus:border-gray-400 focus:outline-none text-gray-100 font-mono"
                        />
                      </div>
                    ))}
                  </div>
                </>
              )}

              {/* Step 1: Credentials */}
              {step === 1 && (
                <>
                  <div>
                    <h2 className="text-base font-semibold text-gray-100">{t('setup.credentials.title')}</h2>
                    <p className="text-gray-400 text-xs mt-1">{t('setup.credentials.desc')}</p>
                  </div>
                  <div className="space-y-3">
                    {[
                      { label: t('setup.credentials.fields.username'), val: username, set: setUsername, ph: t('setup.credentials.placeholders.username'), type: 'text' },
                      { label: t('setup.credentials.fields.password'), val: password, set: setPassword, ph: t('setup.credentials.placeholders.password'), type: 'password' },
                    ].map(({ label, val, set, ph, type }) => (
                      <div key={label} className="flex items-center gap-4">
                        <label className="text-[10px] font-medium text-gray-400 uppercase tracking-widest w-36 shrink-0">{label}</label>
                        <input
                          type={type}
                          value={val}
                          onChange={e => { set(e.target.value); setTestResult(null) }}
                          placeholder={ph}
                          className="flex-1 bg-gray-800 border border-gray-600 text-xs py-2 px-3 rounded-sm focus:border-gray-400 focus:outline-none text-gray-100 font-mono"
                        />
                      </div>
                    ))}
                  </div>
                  <div className="flex items-center gap-4">
                    <button
                      onClick={handleTestConnection}
                      disabled={testing || !username}
                      className="px-4 py-2 text-xs font-semibold rounded-sm bg-surface-container-highest text-gray-100 hover:text-gray-100 hover:bg-secondary-container transition-colors disabled:opacity-50"
                    >
                      {testing ? t('setup.credentials.testing') : t('setup.credentials.testButton')}
                    </button>
                    {testResult?.ok && (
                      <span className="flex items-center gap-1.5 text-xs text-emerald-400">
                        <span className="material-symbols-outlined text-xs">check_circle</span>
                        {t('setup.credentials.connectedAs', { username })}
                      </span>
                    )}
                    {testResult && !testResult.ok && (
                      <span className="flex items-center gap-1.5 text-xs text-error">
                        <span className="material-symbols-outlined text-xs">error</span>
                        {testResult.error}
                      </span>
                    )}
                  </div>
                </>
              )}

              {/* Step 2: Provision */}
              {step === 2 && (
                <>
                  <div>
                    <h2 className="text-base font-semibold text-gray-100">{t('setup.provision.title')}</h2>
                    <p className="text-gray-400 text-xs mt-1">{t('setup.provision.desc')}</p>
                  </div>

                  {!provisioning && log.length === 0 && !done && (
                    <button
                      onClick={handleProvision}
                      className="bg-primary-button px-6 py-2.5 rounded-sm text-xs font-semibold text-primary-button-text hover:scale-[1.02] transition-transform"
                    >
                      <span className="flex items-center gap-2">
                        <span className="material-symbols-outlined text-xs">build</span>
                        {t('setup.provision.button')}
                      </span>
                    </button>
                  )}

                  {(log.length > 0 || provisioning) && (
                    <div className="bg-surface-container-lowest rounded-sm border border-outline-variant/10 p-4 font-mono text-[11px] leading-relaxed max-h-72 overflow-y-auto custom-scrollbar">
                      {log.map((entry, i) => (
                        <div key={i} className={
                          entry.status === 'error' ? 'text-error' :
                          entry.status === 'done'  ? 'text-emerald-400 font-semibold' :
                          entry.status === 'running' ? 'text-gray-400' :
                          'text-gray-100'
                        }>
                          {entry.msg}
                        </div>
                      ))}
                      {provisioning && <div className="text-primary animate-pulse">▌</div>}
                      <div ref={logEndRef} />
                    </div>
                  )}

                  {provisionError && (
                    <div className="flex items-start gap-2 text-xs text-error bg-[#93000a]/20 border border-[#ffb4ab]/20 rounded-sm p-3">
                      <span className="material-symbols-outlined text-xs mt-0.5 shrink-0">error</span>
                      {provisionError}
                    </div>
                  )}
                </>
              )}
            </div>

            {/* Footer */}
            <div className="bg-surface-container-highest/30 px-8 py-5 flex justify-between items-center">
              <button
                onClick={() => setStep(s => Math.max(0, s - 1))}
                disabled={step === 0 || provisioning || done}
                className="px-4 py-2 text-xs font-medium text-gray-400 hover:text-gray-100 transition-all flex items-center gap-2 disabled:opacity-40 disabled:cursor-not-allowed"
              >
                <span className="material-symbols-outlined text-xs">arrow_back</span>
                {t('setup.footer.back')}
              </button>

              {step < 2 ? (
                <button
                  onClick={() => setStep(s => s + 1)}
                  disabled={(step === 0 && !host) || (step === 1 && !testResult?.ok)}
                  className="bg-primary-button px-6 py-2.5 rounded-sm text-xs font-semibold text-primary-button-text hover:scale-[1.02] active:scale-95 transition-all flex items-center gap-2 disabled:opacity-50 disabled:cursor-not-allowed disabled:hover:scale-100"
                >
                  {t('setup.footer.next')}
                  <span className="material-symbols-outlined text-xs">arrow_forward</span>
                </button>
              ) : (
                <button
                  disabled={!done}
                  className="bg-primary-button px-6 py-2.5 rounded-sm text-xs font-semibold text-primary-button-text flex items-center gap-2 opacity-50 cursor-not-allowed"
                >
                  <span className="material-symbols-outlined text-xs">check_circle</span>
                  {done ? t('setup.footer.redirecting') : t('setup.footer.waiting')}
                </button>
              )}
            </div>
          </div>
        </div>
      </main>
    </div>
  )
}
