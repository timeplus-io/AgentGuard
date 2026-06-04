import { useEffect, useState, useCallback } from 'react'
import { api } from '../api/client'

const PHASES = ['starting', 'healthy', 'provisioning', 'ready']
const LABELS = {
  starting: 'Starting timeplusd…',
  healthy: 'Engine healthy — preparing…',
  provisioning: 'Provisioning streams & views…',
  ready: 'Ready',
}

export default function BootStatus({ status: initial, onReady }) {
  const [status, setStatus] = useState(initial)
  const [retrying, setRetrying] = useState(false)
  const [retryError, setRetryError] = useState('')

  const poll = useCallback(async () => {
    const s = await api.supervisorStatus()
    setStatus(s)
    if (s.state === 'ready' || s.state === 'disabled') onReady?.()
    return s
  }, [onReady])

  useEffect(() => {
    if (status.state === 'failed') return
    const id = setInterval(poll, 1500)
    return () => clearInterval(id)
  }, [poll, status.state])

  const handleRetry = async () => {
    setRetrying(true)
    setRetryError('')
    try {
      await api.supervisorRetry()
    } catch (e) {
      setRetryError(e?.message || 'Retry failed')
    } finally {
      setRetrying(false)
    }
    // Resync regardless of the retry call's outcome; the effect resumes polling
    // once the state leaves "failed".
    poll()
  }

  const failed = status.state === 'failed'

  return (
    <div className="min-h-screen bg-[#15121d] text-gray-100 flex flex-col items-center justify-center px-6">
      <div className="w-full max-w-md">
        <h1 className="text-lg font-semibold mb-6">AgentGuard is starting</h1>
        {!failed && (
          <ol className="space-y-2 mb-6">
            {PHASES.map((p) => {
              const reached = PHASES.indexOf(p) <= PHASES.indexOf(status.state)
              const active = p === status.state
              return (
                <li key={p} className={`flex items-center gap-3 text-sm ${reached ? 'text-gray-100' : 'text-gray-500'}`}>
                  <span className={`h-2 w-2 rounded-full ${active ? 'bg-[#f256a3] animate-pulse' : reached ? 'bg-[#f256a3]' : 'bg-gray-600'}`} />
                  {LABELS[p]}
                </li>
              )
            })}
          </ol>
        )}
        {failed && (
          <div className="bg-[#1f1a2b] rounded p-4">
            <p className="text-error text-sm font-medium mb-1">Startup failed: {status.reason}</p>
            {status.detail && (
              <pre className="text-xs text-gray-400 whitespace-pre-wrap max-h-48 overflow-auto mb-3">{status.detail}</pre>
            )}
            <button
              onClick={handleRetry}
              disabled={retrying}
              className="bg-gradient-to-br from-[#ffb0cf] to-[#f256a3] text-[#3d0023] text-sm font-medium rounded px-4 py-2 disabled:opacity-60"
            >
              {retrying ? 'Retrying…' : 'Retry'}
            </button>
            {retryError && <p className="text-error text-xs mt-3">{retryError}</p>}
          </div>
        )}
      </div>
    </div>
  )
}
