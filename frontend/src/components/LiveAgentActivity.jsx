import { useState, useEffect } from 'react'
import { api } from '../api/client'
import { formatLocalTime } from '../utils/time'

const sqlActivity = (range) => `
SELECT hook_name, event_time, agent_id, tool_name, model, hook_decision
FROM agentguard_hook_events
WHERE hook_name IN ('llm_input', 'llm_output', 'before_tool_call', 'after_tool_call')
SETTINGS seek_to='${range}'`

const formatEvtTime = (ts) => ts ? (formatLocalTime(ts) || String(ts)) : '—'

function actionLabel(evt) {
  if (evt.hook_name === 'llm_input')        return 'LLM call'
  if (evt.hook_name === 'llm_output')       return 'LLM response'
  if (evt.hook_name === 'before_tool_call') return 'Tool invoke'
  if (evt.hook_name === 'after_tool_call')  return 'Tool complete'
  return evt.hook_name
}

function toolOrModel(evt) {
  if (evt.hook_name === 'llm_input' || evt.hook_name === 'llm_output') return evt.model || '—'
  return evt.tool_name || '—'
}

export default function LiveAgentActivity({ timeRange = '-1h', limit = 20 }) {
  const [rows, setRows] = useState([])
  const [connected, setConnected] = useState(false)
  const [error, setError] = useState(null)

  useEffect(() => {
    setRows([])
    setConnected(false)
    setError(null)
    const ctrl = new AbortController()

    async function stream() {
      let first = true
      for await (const row of api.protonQuery(sqlActivity(timeRange), { signal: ctrl.signal })) {
        if (first) { setConnected(true); first = false }
        setRows(prev => [row, ...prev].slice(0, limit))
      }
      setConnected(false)
    }

    stream().catch(e => { if (e?.name !== 'AbortError') setError(e.message || 'Stream error') })
    return () => ctrl.abort()
  }, [timeRange, limit])

  const toMs = (v) => {
    let s = typeof v === 'string' ? v.replace(' ', 'T') : v
    if (typeof s === 'string' && !s.endsWith('Z') && !/[+-]\d{2}:\d{2}$/.test(s)) s = s + 'Z'
    return new Date(s)
  }
  const sorted = [...rows].sort((a, b) => toMs(b.event_time) - toMs(a.event_time))

  return (
    <div className="bg-[#2c2834] rounded-sm overflow-hidden">
      <div className="px-6 py-4 flex items-center justify-between border-b border-[#574149]/10">
        <h3 className="text-sm font-semibold text-zinc-400 uppercase tracking-widest flex items-center gap-3">
          <span className={`w-2 h-2 rounded-full ${connected ? 'bg-[#ffb0cf] shadow-[0_0_10px_#ffb0cf] animate-pulse' : 'bg-zinc-600'}`} />
          Live Agent Activity
        </h3>
        {connected && (
          <span className="text-[10px] font-semibold text-[#ffb0cf] uppercase tracking-widest">Live</span>
        )}
      </div>
      <div className="overflow-auto max-h-80">
        <table className="w-full text-left border-collapse">
          <thead className="bg-[#211e29] sticky top-0 z-10">
            <tr>
              {['Time', 'Agent', 'Action', 'Tool / Model', 'Decision'].map(h => (
                <th key={h} className="px-6 py-3 text-[10px] font-bold text-zinc-500 uppercase tracking-widest">{h}</th>
              ))}
            </tr>
          </thead>
          <tbody className="divide-y divide-[#574149]/10">
            {sorted.length === 0 ? (
              <tr>
                <td colSpan={5} className="px-6 py-10 text-center text-xs">
                  {error
                    ? <span className="text-red-400 font-mono flex items-center justify-center gap-2"><span className="material-symbols-outlined text-sm">error</span>{error}</span>
                    : <span className="text-zinc-600">{connected ? 'Waiting for agent activity…' : 'Connecting…'}</span>
                  }
                </td>
              </tr>
            ) : sorted.map((evt, i) => (
              <tr key={`${evt.event_time}-${evt.agent_id}-${evt.hook_name}-${i}`} className="hover:bg-[#37333f] transition-colors">
                <td className="px-6 py-3 text-[10px] font-mono text-[#ffb0cf] whitespace-nowrap">{formatEvtTime(evt.event_time)}</td>
                <td className="px-6 py-3">
                  <code className="text-xs font-mono text-[#e7dff0]">{evt.agent_id}</code>
                </td>
                <td className="px-6 py-3 text-xs text-zinc-400">{actionLabel(evt)}</td>
                <td className="px-6 py-3 text-xs text-zinc-500 font-mono">{toolOrModel(evt)}</td>
                <td className="px-6 py-3">
                  {evt.hook_decision && evt.hook_decision !== 'allow' ? (
                    <span className={`text-[10px] px-1.5 py-0.5 rounded-full font-semibold uppercase ${
                      evt.hook_decision === 'block'
                        ? 'bg-red-500/20 text-red-400'
                        : 'bg-amber-500/20 text-amber-400'
                    }`}>{evt.hook_decision}</span>
                  ) : (
                    <span className="text-[10px] text-zinc-600">—</span>
                  )}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </div>
  )
}
