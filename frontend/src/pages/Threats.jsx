import { useState, useEffect } from 'react'
import Sidebar from '../components/Sidebar'
import TopNav from '../components/TopNav'
import Footer from '../components/Footer'
import PageHeader from '../components/PageHeader'
import { SeverityBadge } from '../components/Badge'
import { api } from '../api/client'

// Single streaming query per panel: Proton delivers historical backfill first,
// then continues streaming live events on the same connection.
const SQL_THREATS = `
SELECT detected_at, rule_id, rule_name, severity, agent_id,
       session_id, hook_name, tool_name, message
FROM agentshield_security_events
SETTINGS seek_to='-24h'`

const SQL_ACTIVITY = `
SELECT hook_name, event_time, agent_id, tool_name, model, hook_decision
FROM agentshield_hook_events
WHERE hook_name IN ('llm_input', 'llm_output', 'before_tool_call', 'after_tool_call')
SETTINGS seek_to='-1h'`

export default function Threats() {
  const [threats, setThreats] = useState([])
  const [liveActivity, setLiveActivity] = useState([])
  const [connected, setConnected] = useState(false)
  const [severity, setSeverity] = useState('All')
  const [category, setCategory] = useState('All Categories')

  useEffect(() => {
    const threatCtrl = new AbortController()
    const activityCtrl = new AbortController()

    async function streamThreats() {
      let first = true
      // seek_to='-24h' causes Proton to replay last 24h first, then continue live —
      // all on the same connection, no phase switch needed.
      for await (const row of api.protonQuery(SQL_THREATS, { signal: threatCtrl.signal })) {
        if (first) { setConnected(true); first = false }
        setThreats(prev => [row, ...prev].slice(0, 100))
      }
      setConnected(false)
    }

    async function streamActivity() {
      for await (const row of api.protonQuery(SQL_ACTIVITY, { signal: activityCtrl.signal })) {
        setLiveActivity(prev => [row, ...prev].slice(0, 20))
      }
    }

    streamThreats().catch(e => { if (e?.name !== 'AbortError') setConnected(false) })
    streamActivity().catch(() => {})

    return () => {
      threatCtrl.abort()
      activityCtrl.abort()
    }
  }, [])

  const filtered = threats.filter(t => {
    if (severity !== 'All' && t.severity?.toLowerCase() !== severity.toLowerCase()) return false
    if (category !== 'All Categories') {
      const catMap = { Injection: 'rp-001', Exfiltration: 'rp-002', 'Supply Chain': 'rp-004' }
      const mappedId = catMap[category]
      if (!mappedId || t.rule_id !== mappedId) return false
    }
    return true
  })

  return null // render added in Task 2
}
