import { useState, useRef } from 'react'
import { useAuth } from '../context/AuthContext'
import { api } from '../api/client'

const STEPS = [
  { label: 'Connection', icon: 'cable' },
  { label: 'Credentials', icon: 'key' },
  { label: 'Provision', icon: 'build' },
]

export default function Setup() {
  const { setAuth } = useAuth()
  const [step, setStep] = useState(0)

  // Step 0 — connection
  const [host, setHost] = useState('localhost')
  const [port, setPort] = useState(3218)
  const [nativePort, setNativePort] = useState(8463)
  const [agentGuardURL, setAgentGuardURL] = useState('http://localhost:8080')

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
      for await (const event of api.setupProvision({ host, port, native_port: nativePort, username, password, agentguard_url: agentGuardURL })) {
        setLog(prev => [...prev, event])
        logEndRef.current?.scrollIntoView({ behavior: 'smooth' })
        if (event.status === 'done') {
          // Provision succeeded — log the user in automatically.
          try {
            const data = await api.authLogin(username, password)
            localStorage.setItem('agentguard_username', data.username)
            setAuth(prev => ({ ...prev, setupComplete: true, loggedIn: true, username: data.username }))
            setDone(true)
          } catch {
            setProvisionError('Provisioning succeeded but login failed — please reload and log in.')
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
    <div className="min-h-screen bg-[#15121d] text-[#e7dff0] flex flex-col">
      <nav className="bg-[#15121d] shadow-[0_0_12px_rgba(213,63,140,0.12)] sticky top-0 z-50 h-16 w-full flex items-center px-6 backdrop-blur-md">
        <span className="text-lg font-semibold bg-clip-text text-transparent bg-gradient-to-br from-[#ffb0cf] to-[#f256a3] tracking-tight">AgentGuard</span>
        <span className="ml-3 text-[10px] font-semibold text-zinc-500 uppercase tracking-widest">First-time Setup</span>
      </nav>

      <main className="flex-1 flex items-start justify-center p-6 md:p-12">
        <div className="w-full max-w-2xl space-y-8">
          <div>
            <h1 className="text-2xl font-bold text-white tracking-tight">Setup AgentGuard</h1>
            <p className="text-zinc-500 text-xs mt-1 leading-relaxed">Connect to Timeplus and provision all monitoring resources.</p>
          </div>

          {/* Step indicator */}
          <div className="flex items-center gap-0">
            {STEPS.map((s, i) => (
              <div key={i} className="flex items-center">
                <div className={`flex items-center gap-2 px-4 py-2 rounded-sm text-xs font-semibold ${i === step ? 'bg-[#ffb0cf]/10 text-[#ffb0cf]' : i < step ? 'text-zinc-400' : 'text-zinc-600'}`}>
                  <span className={`w-5 h-5 rounded-full flex items-center justify-center text-[10px] font-bold ${i < step ? 'bg-[#f256a3] text-[#570033]' : i === step ? 'bg-[#ffb0cf]/20 text-[#ffb0cf]' : 'bg-[#37333f] text-zinc-600'}`}>
                    {i < step ? '✓' : i + 1}
                  </span>
                  {s.label}
                </div>
                {i < STEPS.length - 1 && <div className="h-px w-8 bg-[#37333f]" />}
              </div>
            ))}
          </div>

          <div className="bg-[#2c2834] rounded-sm overflow-hidden">
            <div className="p-8 space-y-6">

              {/* Step 0: Connection */}
              {step === 0 && (
                <>
                  <div>
                    <h2 className="text-base font-semibold text-white">Timeplus Connection</h2>
                    <p className="text-zinc-400 text-xs mt-1">Enter the address of your Timeplus instance.</p>
                  </div>
                  <div className="space-y-3">
                    {[
                      { label: 'Timeplus Host', val: host, set: setHost, ph: 'localhost', type: 'text' },
                      { label: 'HTTP Port', val: port, set: v => setPort(Number(v)), ph: '3218', type: 'number' },
                      { label: 'Native Port', val: nativePort, set: v => setNativePort(Number(v)), ph: '8463', type: 'number' },
                      { label: 'AgentGuard URL', val: agentGuardURL, set: setAgentGuardURL, ph: 'http://localhost:8080', type: 'text' },
                    ].map(({ label, val, set, ph, type }) => (
                      <div key={label} className="flex items-center gap-4">
                        <label className="text-[10px] font-medium text-zinc-500 uppercase tracking-widest w-36 shrink-0">{label}</label>
                        <input
                          type={type}
                          value={val}
                          onChange={e => { set(e.target.value); setTestResult(null) }}
                          placeholder={ph}
                          className="flex-1 bg-[#0f0c17] text-xs py-2 px-3 rounded-sm focus:ring-1 focus:ring-[#ffb0cf] focus:outline-none text-[#e7dff0] font-mono"
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
                    <h2 className="text-base font-semibold text-white">Timeplus Credentials</h2>
                    <p className="text-zinc-400 text-xs mt-1">Enter your Timeplus username and password. These will not be stored.</p>
                  </div>
                  <div className="space-y-3">
                    {[
                      { label: 'Username', val: username, set: setUsername, ph: 'default', type: 'text' },
                      { label: 'Password', val: password, set: setPassword, ph: 'Leave blank if not set', type: 'password' },
                    ].map(({ label, val, set, ph, type }) => (
                      <div key={label} className="flex items-center gap-4">
                        <label className="text-[10px] font-medium text-zinc-500 uppercase tracking-widest w-36 shrink-0">{label}</label>
                        <input
                          type={type}
                          value={val}
                          onChange={e => { set(e.target.value); setTestResult(null) }}
                          placeholder={ph}
                          className="flex-1 bg-[#0f0c17] text-xs py-2 px-3 rounded-sm focus:ring-1 focus:ring-[#ffb0cf] focus:outline-none text-[#e7dff0] font-mono"
                        />
                      </div>
                    ))}
                  </div>
                  <div className="flex items-center gap-4">
                    <button
                      onClick={handleTestConnection}
                      disabled={testing || !username}
                      className="px-4 py-2 text-xs font-semibold rounded-sm bg-[#37333f] text-zinc-300 hover:text-white hover:bg-[#4b4754] transition-colors disabled:opacity-50"
                    >
                      {testing ? 'Testing…' : 'Test Connection'}
                    </button>
                    {testResult?.ok && (
                      <span className="flex items-center gap-1.5 text-xs text-emerald-400">
                        <span className="material-symbols-outlined text-xs">check_circle</span>
                        Connected as &quot;{username}&quot;
                      </span>
                    )}
                    {testResult && !testResult.ok && (
                      <span className="flex items-center gap-1.5 text-xs text-[#ffb4ab]">
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
                    <h2 className="text-base font-semibold text-white">Provision Resources</h2>
                    <p className="text-zinc-400 text-xs mt-1">Create all AgentGuard streams, materialized views, and rules in Timeplus.</p>
                  </div>

                  {!provisioning && log.length === 0 && !done && (
                    <button
                      onClick={handleProvision}
                      className="bg-primary-button px-6 py-2.5 rounded-sm text-xs font-semibold text-primary-button-text hover:scale-[1.02] transition-transform"
                    >
                      <span className="flex items-center gap-2">
                        <span className="material-symbols-outlined text-xs">build</span>
                        Provision Resources
                      </span>
                    </button>
                  )}

                  {(log.length > 0 || provisioning) && (
                    <div className="bg-[#0f0c17] rounded-sm border border-[#574149]/10 p-4 font-mono text-[11px] leading-relaxed max-h-72 overflow-y-auto custom-scrollbar">
                      {log.map((entry, i) => (
                        <div key={i} className={
                          entry.status === 'error' ? 'text-[#ffb4ab]' :
                          entry.status === 'done'  ? 'text-emerald-400 font-semibold' :
                          entry.status === 'running' ? 'text-zinc-400' :
                          'text-zinc-300'
                        }>
                          {entry.msg}
                        </div>
                      ))}
                      {provisioning && <div className="text-[#ffb0cf] animate-pulse">▌</div>}
                      <div ref={logEndRef} />
                    </div>
                  )}

                  {provisionError && (
                    <div className="flex items-start gap-2 text-xs text-[#ffb4ab] bg-[#93000a]/20 border border-[#ffb4ab]/20 rounded-sm p-3">
                      <span className="material-symbols-outlined text-xs mt-0.5 shrink-0">error</span>
                      {provisionError}
                    </div>
                  )}
                </>
              )}
            </div>

            {/* Footer */}
            <div className="bg-[#37333f]/30 px-8 py-5 flex justify-between items-center">
              <button
                onClick={() => setStep(s => Math.max(0, s - 1))}
                disabled={step === 0 || provisioning || done}
                className="px-4 py-2 text-xs font-medium text-zinc-400 hover:text-white transition-all flex items-center gap-2 disabled:opacity-40 disabled:cursor-not-allowed"
              >
                <span className="material-symbols-outlined text-xs">arrow_back</span>
                Back
              </button>

              {step < 2 ? (
                <button
                  onClick={() => setStep(s => s + 1)}
                  disabled={(step === 0 && !host) || (step === 1 && !testResult?.ok)}
                  className="bg-primary-button px-6 py-2.5 rounded-sm text-xs font-semibold text-primary-button-text hover:scale-[1.02] active:scale-95 transition-all flex items-center gap-2 disabled:opacity-50 disabled:cursor-not-allowed disabled:hover:scale-100"
                >
                  Next
                  <span className="material-symbols-outlined text-xs">arrow_forward</span>
                </button>
              ) : (
                <button
                  disabled={!done}
                  className="bg-primary-button px-6 py-2.5 rounded-sm text-xs font-semibold text-primary-button-text flex items-center gap-2 opacity-50 cursor-not-allowed"
                >
                  <span className="material-symbols-outlined text-xs">check_circle</span>
                  {done ? 'Redirecting…' : 'Waiting for provisioning…'}
                </button>
              )}
            </div>
          </div>
        </div>
      </main>
    </div>
  )
}
