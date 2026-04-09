import { useState, useEffect } from 'react'
import { api } from '../api/client'

const sqlActivity = (range) => `
SELECT hook_name, event_time, agent_id, tool_name, model, hook_decision
FROM agentshield_hook_events
WHERE hook_name IN ('llm_input', 'llm_output', 'before_tool_call', 'after_tool_call')
SETTINGS seek_to='${range}'`

function formatEvtTime(ts) {
  if (!ts) return '—'
  try {
    return new Date(typeof ts === 'string' ? ts.replace(' ', 'T') : ts)
      .toLocaleTimeString([], { hour: '2-digit', minute: '2-digit', second: '2-digit' })
  } catch { return String(ts) }
}

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

  useEffect(() => {
    setRows([])
    setConnected(false)
    const ctrl = new AbortController()

    async function stream() {
      let first = true
      for await (const row of api.protonQuery(sqlActivity(timeRange), { signal: ctrl.signal })) {
        if (first) { setConnected(true); first = false }
        setRows(prev => [row, ...prev].slice(0, limit))
      }
      setConnected(false)
    }

    stream().catch(e => { if (e?.name !== 'AbortError') setConnected(false) })
    return () => ctrl.abort()
  }, [timeRange, limit])

  const sorted = [...rows].sort((a, b) => {
    const ta = new Date(typeof a.event_time === 'string' ? a.event_time.replace(' ', 'T') : a.event_time)
    const tb = new Date(typeof b.event_time === 'string' ? b.event_time.replace(' ', 'T') : b.event_time)
    return tb - ta
  })

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
                <td colSpan={5} className="px-6 py-10 text-center text-zinc-600 text-xs">
                  {connected ? 'Waiting for agent activity…' : 'Connecting…'}
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
